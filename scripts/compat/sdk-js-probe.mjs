import { createClient } from "@supabase/supabase-js";

const required = ["SUPABASE_URL", "SUPABASE_ANON_KEY", "SUPADUPA_TEST_REF"];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
});

let query = supabase
  .from("compat_runner_probe")
  .select("id,project_ref,created_at")
  .eq("project_ref", process.env.SUPADUPA_TEST_REF)
  .limit(1);

if (process.env.SUPADUPA_COMPAT_RUN_ID) {
  query = query.eq("id", process.env.SUPADUPA_COMPAT_RUN_ID);
}

const { data, error } = await query;
if (error) {
  console.error(JSON.stringify({ error }, null, 2));
  process.exit(1);
}

if (!Array.isArray(data) || data.length !== 1) {
  console.error(JSON.stringify({ error: "expected exactly one SDK row", data }, null, 2));
  process.exit(1);
}

if (data[0].project_ref !== process.env.SUPADUPA_TEST_REF) {
  console.error(JSON.stringify({ error: "row project_ref mismatch", row: data[0] }, null, 2));
  process.exit(1);
}

let authResult = null;
if (process.env.SUPABASE_SERVICE_ROLE_KEY) {
  const admin = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_SERVICE_ROLE_KEY, {
    auth: { persistSession: false, autoRefreshToken: false },
  });
  const suffix = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  const email = `compat-sdk-${suffix}@example.test`;
  const password = `CompatSdk2026-${suffix}!`;
  let userId = "";
  try {
    const created = await admin.auth.admin.createUser({
      email,
      password,
      email_confirm: true,
    });
    if (created.error) throw created.error;
    userId = created.data.user?.id || "";
    if (!userId) throw new Error("admin createUser did not return user id");

    const signedIn = await supabase.auth.signInWithPassword({ email, password });
    if (signedIn.error) throw signedIn.error;
    if (!signedIn.data.session?.access_token) throw new Error("signInWithPassword did not return a session");

    const currentUser = await supabase.auth.getUser(signedIn.data.session.access_token);
    if (currentUser.error) throw currentUser.error;
    if (currentUser.data.user?.email !== email) {
      throw new Error(`getUser email mismatch: ${currentUser.data.user?.email}`);
    }
    authResult = { ok: true, email };
  } finally {
    if (userId) {
      await admin.auth.admin.deleteUser(userId);
    }
  }
}

console.log(JSON.stringify({ ok: true, rows: data.length, row: data[0], auth: authResult }));
