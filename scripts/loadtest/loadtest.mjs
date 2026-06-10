// Repeatable real-world load/soak test for a Supadupa-hosted Supabase project.
//
// Exercises every public surface continuously for a configurable duration using
// the official @supabase/supabase-js SDK: REST/tables (insert/select/update/
// delete), Storage (buckets + file upload/download/list/remove), Edge Functions,
// Auth (admin create / sign-in / get-user), GraphQL, and Realtime subscriptions.
//
// Usage:
//   SUPABASE_URL=... SUPABASE_ANON_KEY=... SUPABASE_SERVICE_ROLE_KEY=... \
//   [SUPADUPA_API_URL=... SUPADUPA_TOKEN=... SUPADUPA_REF=...] \
//   node loadtest.mjs --minutes 10 --concurrency 8
//
// SUPADUPA_API_URL/TOKEN/REF are only needed for one-time setup (creating tables
// and deploying edge functions via the management API). Pass --skip-setup to
// reuse an already-seeded project with just the project keys.
import { createClient } from "@supabase/supabase-js";
import { WebSocket as WsWebSocket } from "ws";

// supabase-js realtime needs a WebSocket implementation in Node.
if (!globalThis.WebSocket) globalThis.WebSocket = WsWebSocket;

// ---- config ----------------------------------------------------------------
function arg(name, def) {
  const i = process.argv.indexOf(`--${name}`);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : def;
}
const flag = (name) => process.argv.includes(`--${name}`);

const URL = process.env.SUPABASE_URL;
const ANON = process.env.SUPABASE_ANON_KEY;
const SERVICE = process.env.SUPABASE_SERVICE_ROLE_KEY;
const MIN = parseFloat(arg("minutes", process.env.DURATION_MIN || "10"));
const CONC = parseInt(arg("concurrency", process.env.CONCURRENCY || "8"), 10);
const SKIP_SETUP = flag("skip-setup");
const MGMT = { api: process.env.SUPADUPA_API_URL, token: process.env.SUPADUPA_TOKEN, ref: process.env.SUPADUPA_REF };

if (!URL || !ANON || !SERVICE) {
  console.error("Missing SUPABASE_URL / SUPABASE_ANON_KEY / SUPABASE_SERVICE_ROLE_KEY");
  process.exit(2);
}

const TABLES = ["lt_events", "lt_items", "lt_metrics", "lt_notes", "lt_profiles"];
const BUCKETS = ["lt-public", "lt-assets", "lt-private"];
const FUNCTIONS = ["lt-echo", "lt-compute"];

const admin = createClient(URL, SERVICE, { auth: { persistSession: false, autoRefreshToken: false } });
const anon = createClient(URL, ANON, { auth: { persistSession: false, autoRefreshToken: false } });

// ---- stats -----------------------------------------------------------------
const stats = {}; // category -> {ok, err, ms:[], lastErr}
function record(cat, ok, ms, err) {
  const s = (stats[cat] ||= { ok: 0, err: 0, ms: [], lastErr: "" });
  if (ok) { s.ok++; s.ms.push(ms); if (s.ms.length > 5000) s.ms.shift(); }
  else { s.err++; if (err) s.lastErr = String(err).slice(0, 120); }
}
async function timed(cat, fn) {
  const t = Date.now();
  try { const r = await fn(); record(cat, true, Date.now() - t); return r; }
  catch (e) { record(cat, false, Date.now() - t, e?.message || e); return null; }
}
function pct(arr, p) {
  if (!arr.length) return 0;
  const a = [...arr].sort((x, y) => x - y);
  return a[Math.min(a.length - 1, Math.floor((p / 100) * a.length))];
}
const rint = (n) => Math.floor(Math.random() * n);
const pick = (a) => a[rint(a.length)];
let realtimeEvents = 0;

// ---- management-API setup (tables + functions) -----------------------------
async function mgmtPost(path, body) {
  const res = await fetch(`${MGMT.api}${path}`, {
    method: "POST",
    headers: { authorization: `Bearer ${MGMT.token}`, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: res.status, text: await res.text() };
}

function tableSQL(t) {
  return `
create table if not exists public.${t} (
  id bigint generated always as identity primary key,
  worker text, kind text, label text, n integer, flag boolean default false,
  payload jsonb default '{}'::jsonb, created_at timestamptz not null default now()
);
alter table public.${t} enable row level security;
drop policy if exists "${t}_all" on public.${t};
create policy "${t}_all" on public.${t} for all to anon, authenticated using (true) with check (true);
grant all on public.${t} to anon, authenticated;
do $$ begin
  alter publication supabase_realtime add table public.${t};
exception when duplicate_object then null; when undefined_object then null; end $$;
notify pgrst, 'reload schema';`;
}

async function setup() {
  console.log("== setup ==");
  // Buckets (service key, no mgmt needed)
  for (const b of BUCKETS) {
    const isPublic = b !== "lt-private";
    const { error } = await admin.storage.createBucket(b, { public: isPublic });
    console.log(`  bucket ${b}${isPublic ? " (public)" : ""}: ${error ? (error.message.includes("exist") ? "exists" : "ERR " + error.message) : "created"}`);
  }
  if (SKIP_SETUP || !MGMT.api || !MGMT.token || !MGMT.ref) {
    console.log("  (skipping table/function setup — no mgmt creds or --skip-setup)");
    return;
  }
  // Tables via management schema endpoint (applies SQL to the project DB)
  for (const t of TABLES) {
    const r = await mgmtPost(`/v1/projects/${MGMT.ref}/database/schemas`,
      { name: `lt-${t.replace(/_/g, "-")}`, version: "1", schema: "public", sql: tableSQL(t), active: true, apply_order: 1 });
    console.log(`  table ${t}: HTTP ${r.status}${r.status >= 400 ? " " + r.text.slice(0, 100) : ""}`);
  }
  // Edge functions
  const fns = {
    "lt-echo": `import { serve } from "https://deno.land/std@0.177.0/http/server.ts";
serve(async (req) => { let b={}; try{b=await req.json()}catch{}; return new Response(JSON.stringify({ ok:true, echo:b, ts:Date.now() }), { headers:{"content-type":"application/json"} }); });`,
    "lt-compute": `import { serve } from "https://deno.land/std@0.177.0/http/server.ts";
serve(async (req) => { let n=1; try{n=(await req.json()).n||1}catch{}; let s=0; for(let i=0;i<n;i++)s+=i; return new Response(JSON.stringify({ ok:true, n, sum:s }), { headers:{"content-type":"application/json"} }); });`,
  };
  for (const [name, source] of Object.entries(fns)) {
    const r = await mgmtPost(`/v1/projects/${MGMT.ref}/functions`, { name, entrypoint: "index.ts", verify_jwt: false, source });
    console.log(`  function ${name}: HTTP ${r.status}`);
  }
}

// ---- operations (weighted real-world mix) ----------------------------------
async function opRestInsert() {
  const t = pick(TABLES);
  return timed("rest.insert", async () => {
    const { error } = await anon.from(t).insert({ worker: "lt", kind: pick(["a", "b", "c"]), label: `row-${rint(1e6)}`, n: rint(1000), flag: Math.random() < 0.5, payload: { v: rint(1e6), tags: [pick(["x", "y", "z"])] } });
    if (error) throw error;
  });
}
async function opRestSelect() {
  const t = pick(TABLES);
  return timed("rest.select", async () => {
    const { error } = await anon.from(t).select("id,label,n,created_at").order("id", { ascending: false }).limit(20);
    if (error) throw error;
  });
}
async function opRestUpdate() {
  const t = pick(TABLES);
  return timed("rest.update", async () => {
    const { data } = await anon.from(t).select("id").order("id", { ascending: false }).limit(1);
    if (!data?.length) return;
    const { error } = await anon.from(t).update({ flag: true, n: rint(1000) }).eq("id", data[0].id);
    if (error) throw error;
  });
}
async function opRestDelete() {
  const t = pick(TABLES);
  return timed("rest.delete", async () => {
    const { data } = await anon.from(t).select("id").order("id", { ascending: true }).limit(1);
    if (!data?.length) return;
    const { error } = await anon.from(t).delete().eq("id", data[0].id);
    if (error) throw error;
  });
}
async function opStorageUpload() {
  const b = pick(BUCKETS);
  const key = `w/${rint(50)}/file-${rint(1e6)}.txt`;
  const body = new Blob([`payload ${Date.now()} ${"x".repeat(rint(2048))}`]);
  return timed("storage.upload", async () => {
    const { error } = await admin.storage.from(b).upload(key, body, { upsert: true, contentType: "text/plain" });
    if (error) throw error;
    return { b, key };
  });
}
async function opStorageDownload() {
  const b = pick(BUCKETS);
  return timed("storage.download", async () => {
    const { data: list } = await admin.storage.from(b).list("w/" + rint(50), { limit: 5 });
    if (!list?.length) return;
    const { error } = await admin.storage.from(b).download(`w/${rint(50)}/${list[0].name}`);
    // a miss is fine under churn; only count hard errors
    if (error && !/not.*found|exist/i.test(error.message)) throw error;
  });
}
async function opStorageList() {
  const b = pick(BUCKETS);
  return timed("storage.list", async () => {
    const { error } = await admin.storage.from(b).list("w/" + rint(50), { limit: 20 });
    if (error) throw error;
  });
}
async function opStorageRemove() {
  const b = pick(BUCKETS);
  return timed("storage.remove", async () => {
    const { data: list } = await admin.storage.from(b).list("w/" + rint(50), { limit: 3 });
    if (!list?.length) return;
    const { error } = await admin.storage.from(b).remove([`w/${rint(50)}/${list[0].name}`]);
    if (error) throw error;
  });
}
async function opEdge() {
  const fn = pick(FUNCTIONS);
  return timed("edge.invoke", async () => {
    const { error } = await anon.functions.invoke(fn, { body: { n: rint(500), ping: Date.now() } });
    if (error) throw error;
  });
}
let userSeq = 0;
async function opAuthSignupLogin() {
  const email = `lt+${Date.now()}-${userSeq++}@example.com`;
  const pw = "Load-Test-Pw-123!";
  return timed("auth.signup_login", async () => {
    const { error: ce } = await admin.auth.admin.createUser({ email, password: pw, email_confirm: true });
    if (ce && !/already|exist/i.test(ce.message)) throw ce;
    const { data, error } = await anon.auth.signInWithPassword({ email, password: pw });
    if (error) throw error;
    return data.session?.access_token;
  });
}
async function opGraphQL() {
  // pg_graphql names the collection after the raw table name + "Collection".
  const t = pick(TABLES);
  return timed("graphql.query", async () => {
    const res = await fetch(`${URL}/graphql/v1`, {
      method: "POST",
      headers: { apikey: ANON, authorization: `Bearer ${ANON}`, "content-type": "application/json" },
      body: JSON.stringify({ query: `{ ${t}Collection(first: 5) { edges { node { id n } } } }` }),
    });
    const j = await res.json();
    if (j.errors) throw new Error(j.errors[0]?.message || "graphql error");
  });
}

// Weighted mix resembling a chatty app: reads dominate, writes frequent.
const WEIGHTS = [
  [opRestSelect, 28], [opRestInsert, 18], [opRestUpdate, 8], [opRestDelete, 4],
  [opStorageUpload, 8], [opStorageList, 6], [opStorageDownload, 5], [opStorageRemove, 3],
  [opEdge, 8], [opGraphQL, 6], [opAuthSignupLogin, 3],
];
const BAG = WEIGHTS.flatMap(([fn, w]) => Array(w).fill(fn));

// ---- realtime subscribers --------------------------------------------------
async function startRealtime() {
  const channels = [];
  for (const t of TABLES.slice(0, 3)) {
    const ch = anon.channel(`lt-${t}`)
      .on("postgres_changes", { event: "*", schema: "public", table: t }, () => { realtimeEvents++; })
      .subscribe((status) => { if (status === "SUBSCRIBED") record("realtime.subscribe", true, 0); else if (status === "CHANNEL_ERROR") record("realtime.subscribe", false, 0, "channel error"); });
    channels.push(ch);
  }
  return channels;
}

// ---- worker loop -----------------------------------------------------------
async function worker(deadline) {
  while (Date.now() < deadline) {
    await pick(BAG)();
  }
}

function printSummary(title) {
  const cats = Object.keys(stats).sort();
  let totOk = 0, totErr = 0;
  console.log(`\n${title}`);
  console.log("  " + "category".padEnd(20) + "ok".padStart(9) + "err".padStart(7) + "p50ms".padStart(8) + "p95ms".padStart(8) + "  lastErr");
  for (const c of cats) {
    const s = stats[c]; totOk += s.ok; totErr += s.err;
    console.log("  " + c.padEnd(20) + String(s.ok).padStart(9) + String(s.err).padStart(7) + String(pct(s.ms, 50)).padStart(8) + String(pct(s.ms, 95)).padStart(8) + "  " + (s.err ? s.lastErr : ""));
  }
  console.log("  " + "-".repeat(60));
  console.log(`  TOTAL ok=${totOk} err=${totErr} realtimeEvents=${realtimeEvents}`);
  return { totOk, totErr };
}

// ---- main ------------------------------------------------------------------
async function main() {
  console.log(`Supadupa load test → ${URL}`);
  console.log(`duration=${MIN}min concurrency=${CONC} tables=${TABLES.length} buckets=${BUCKETS.length} functions=${FUNCTIONS.length}\n`);
  await setup();

  console.log(`\n== load (${MIN} min, ${CONC} workers) ==`);
  const channels = await startRealtime();
  const start = Date.now();
  const deadline = start + MIN * 60 * 1000;

  const ticker = setInterval(() => {
    const ok = Object.values(stats).reduce((a, s) => a + s.ok, 0);
    const err = Object.values(stats).reduce((a, s) => a + s.err, 0);
    const elapsed = (Date.now() - start) / 1000;
    console.log(`  t+${elapsed.toFixed(0)}s  ops=${ok}  err=${err}  rate=${(ok / elapsed).toFixed(1)}/s  rtEvents=${realtimeEvents}`);
  }, 15000);

  await Promise.all(Array.from({ length: CONC }, () => worker(deadline)));
  clearInterval(ticker);
  for (const ch of channels) { try { await anon.removeChannel(ch); } catch {} }

  const { totOk, totErr } = printSummary("== FINAL SUMMARY ==");
  const rate = totErr / Math.max(1, totOk + totErr);
  console.log(`\n  error rate: ${(rate * 100).toFixed(2)}%  over ${((Date.now() - start) / 1000).toFixed(0)}s`);
  process.exit(rate > 0.02 ? 1 : 0); // fail if >2% errors
}
main().catch((e) => { console.error("fatal:", e); process.exit(1); });
