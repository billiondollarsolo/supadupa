#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function parseArgs(argv) {
  const args = {
    envFile: ".env",
    refs: ["hosteq-alpha", "hosteq-beta"],
    appsRoot: "runtime/fake-projects",
    dbHostaddr: "",
    appDir: "",
  };
  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = argv[i + 1];
    if (arg === "--env-file") {
      args.envFile = next;
      i += 1;
    } else if (arg === "--refs") {
      args.refs = next.split(",").map((value) => value.trim()).filter(Boolean);
      i += 1;
    } else if (arg === "--apps-root") {
      args.appsRoot = next;
      i += 1;
    } else if (arg === "--db-hostaddr") {
      args.dbHostaddr = next;
      i += 1;
    } else if (arg === "--app-dir") {
      args.appDir = next;
      i += 1;
    } else {
      throw new Error(`unknown argument: ${arg}`);
    }
  }
  return args;
}

function loadEnv(path) {
  const env = {};
  const payload = readFileSync(path, "utf8");
  for (const line of payload.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const separator = trimmed.indexOf("=");
    if (separator <= 0) continue;
    env[trimmed.slice(0, separator)] = trimmed.slice(separator + 1);
  }
  return env;
}

function managementURL(env) {
  if (env.SUPADUPA_API_URL) return env.SUPADUPA_API_URL.replace(/\/+$/, "");
  if (env.VITE_API_BASE_URL) return env.VITE_API_BASE_URL.replace(/\/+$/, "");
  if (env.SUPADUPA_API_HOST) return `https://${env.SUPADUPA_API_HOST}`;
  return "http://localhost:8080";
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, options);
  const text = await response.text();
  let body = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = { raw: text.slice(0, 500) };
    }
  }
  if (!response.ok) {
    throw new Error(`${options.method ?? "GET"} ${url} -> HTTP ${response.status}: ${JSON.stringify(body)}`);
  }
  return { response, body };
}

async function login(apiURL, env) {
  const email = env.SUPADUPA_BOOTSTRAP_EMAIL;
  const password = env.SUPADUPA_BOOTSTRAP_PASSWORD;
  if (!email || !password) {
    throw new Error("SUPADUPA_BOOTSTRAP_EMAIL and SUPADUPA_BOOTSTRAP_PASSWORD are required");
  }
  const { body } = await fetchJSON(`${apiURL}/v1/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!body?.token) throw new Error("login did not return a token");
  return body.token;
}

async function authedJSON(apiURL, token, path) {
  const { body } = await fetchJSON(`${apiURL}${path}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  return body;
}

async function reveal(apiURL, token, ref, kind) {
  const body = await authedJSON(apiURL, token, `/v1/projects/${encodeURIComponent(ref)}/secrets/${encodeURIComponent(kind)}/reveal`);
  if (!body?.value) throw new Error(`${kind} reveal returned no value`);
  return body.value;
}

function writeFakeApp(appDir, config) {
  mkdirSync(join(appDir, "src"), { recursive: true });
  writeFileSync(join(appDir, "app.config.json"), `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
  writeFileSync(join(appDir, "package.json"), `${JSON.stringify({
    private: true,
    type: "module",
    scripts: {
      smoke: "node ../../../scripts/hosted-equivalence-smoke.mjs --app-dir . --env-file ../../../.env",
    },
  }, null, 2)}\n`);
  writeFileSync(join(appDir, "README.md"), `# ${config.name}\n\nFake hosted-equivalence client for Supadupa project \`${config.ref}\`.\n\nRun from this directory with:\n\n\`\`\`sh\nnpm run smoke\n\`\`\`\n\nThe app reads public connection metadata from Supadupa and reveals secrets at runtime through audited Management API routes. No keys are stored here.\n`);
  writeFileSync(join(appDir, "src", "app.mjs"), `import { spawnSync } from "node:child_process";\nconst result = spawnSync("node", ["../../../scripts/hosted-equivalence-smoke.mjs", "--app-dir", ".", "--env-file", "../../../.env"], { stdio: "inherit" });\nprocess.exit(result.status ?? 1);\n`);
}

function safeBucketName(ref, runID) {
  return `fake-${ref}-${runID}`.toLowerCase().replace(/[^a-z0-9-]/g, "-").replace(/-+/g, "-").slice(0, 62).replace(/-$/, "");
}

function psqlURL(template, hostaddr = "") {
  let value = template.replace(":${DB_PASSWORD}@", "@").replace(":$DB_PASSWORD@", "@");
  const url = new URL(value);
  if (hostaddr) url.searchParams.set("hostaddr", hostaddr);
  return url.toString();
}

function runPSQLOnce(url, password, sql) {
  return spawnSync("psql", ["-v", "ON_ERROR_STOP=1", "-At", url], {
    input: sql,
    encoding: "utf8",
    env: { ...process.env, PGPASSWORD: password },
  });
}

async function runPSQL(label, url, password, sql, results, options = {}) {
  const attempts = options.attempts ?? 1;
  const delayMs = options.delayMs ?? 1000;
  let command = null;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    command = runPSQLOnce(url, password, sql);
    if (command.status === 0) {
      const output = command.stdout.trim();
      results.push({ name: label, ok: true, detail: output.split("\n").slice(-1)[0] ?? "ok", attempts: attempt });
      return output;
    }
    if (attempt < attempts) {
      await new Promise((resolve) => setTimeout(resolve, delayMs));
    }
  }
  const detail = command ? (command.stderr || command.stdout).slice(0, 700) : "psql did not run";
  results.push({ name: label, ok: false, detail, attempts });
  return "";
}

function runPSQLSync(label, url, password, sql, results) {
  const command = runPSQLOnce(url, password, sql);
  if (command.status !== 0) {
    results.push({ name: label, ok: false, detail: (command.stderr || command.stdout).slice(0, 700) });
    return "";
  }
  const output = command.stdout.trim();
  results.push({ name: label, ok: true, detail: output.split("\n").slice(-1)[0] ?? "ok" });
  return output;
}

async function expectHTTP(label, url, options, expected, results) {
  const response = await fetch(url, options);
  const text = await response.text();
  const ok = typeof expected === "function" ? expected(response, text) : expected.includes(response.status);
  results.push({ name: label, ok, status: response.status, detail: ok ? "ok" : text.slice(0, 700) });
  return { response, text };
}

async function realtimeProbe(url, key, results) {
  if (typeof WebSocket === "undefined") {
    results.push({ name: "realtime.websocket", ok: false, detail: "WebSocket global unavailable in this Node runtime" });
    return;
  }
  const wsURL = new URL(url);
  wsURL.protocol = wsURL.protocol === "https:" ? "wss:" : "ws:";
  wsURL.pathname = wsURL.pathname.replace(/\/$/, "") + "/websocket";
  wsURL.search = new URLSearchParams({ apikey: key, vsn: "1.0.0" }).toString();
  await new Promise((resolve) => {
    let done = false;
    const finish = (ok, detail) => {
      if (done) return;
      done = true;
      results.push({ name: "realtime.websocket", ok, detail });
      resolve();
    };
    const timeout = setTimeout(() => finish(false, "timeout"), 7000);
    const ws = new WebSocket(wsURL);
    ws.addEventListener("open", () => {
      clearTimeout(timeout);
      ws.close();
      finish(true, "101");
    });
    ws.addEventListener("error", () => {
      clearTimeout(timeout);
      finish(false, "websocket error");
    });
  });
}

async function runFakeApp(appDir, envFile) {
  const config = JSON.parse(readFileSync(join(appDir, "app.config.json"), "utf8"));
  const env = loadEnv(resolve(appDir, envFile));
  const apiURL = managementURL(env);
  const token = await login(apiURL, env);
  const connect = await authedJSON(apiURL, token, `/v1/projects/${encodeURIComponent(config.ref)}/connect`);
  const [anon, serviceRole, dbPassword] = await Promise.all([
    reveal(apiURL, token, config.ref, "anon_key"),
    reveal(apiURL, token, config.ref, "service_role"),
    reveal(apiURL, token, config.ref, "db_password"),
  ]);

  const runID = `${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
  const table = "fake_app_events";
  const bucket = safeBucketName(config.ref, runID);
  const userEmail = `fake-${config.ref}-${runID}@example.test`;
  const userPassword = `FakeHosted2026-${runID}!`;
  const results = [];
  const serviceHeaders = {
    apikey: serviceRole,
    authorization: `Bearer ${serviceRole}`,
    "content-type": "application/json",
  };
  const anonHeaders = {
    apikey: anon,
    authorization: `Bearer ${anon}`,
  };

  const directURL = psqlURL(connect.postgres.public_direct, config.dbHostaddr);
  const transactionURL = psqlURL(connect.postgres.public_transaction, config.dbHostaddr);
  const sessionURL = psqlURL(connect.postgres.public_session, config.dbHostaddr);

  runPSQLSync("db.public_direct.bootstrap", directURL, dbPassword, `
create table if not exists public.${table} (
  id text primary key,
  app_ref text not null,
  note text not null,
  created_at timestamptz not null default now()
);
alter table public.${table} enable row level security;
grant select, insert, update, delete on public.${table} to anon, authenticated, service_role;
drop policy if exists fake_app_events_select on public.${table};
drop policy if exists fake_app_events_insert on public.${table};
create policy fake_app_events_select on public.${table} for select using (true);
create policy fake_app_events_insert on public.${table} for insert with check (true);
insert into public.${table} (id, app_ref, note) values ('${runID}', '${config.ref}', 'db seed') on conflict (id) do update set note = excluded.note;
notify pgrst, 'reload schema';
select id || ':' || app_ref from public.${table} where id = '${runID}';
`, results);
  await runPSQL("db.pooler_transaction", transactionURL, dbPassword, "select current_database() || ':' || current_user;", results, { attempts: 20, delayMs: 3000 });
  await runPSQL("db.pooler_session", sessionURL, dbPassword, "select current_database() || ':' || current_user;", results, { attempts: 20, delayMs: 3000 });

  await expectHTTP("auth.health", `${connect.auth_url}/health`, {}, [200], results);
  let createdUserID = "";
  try {
    const created = await fetchJSON(`${connect.auth_url}/admin/users`, {
      method: "POST",
      headers: serviceHeaders,
      body: JSON.stringify({ email: userEmail, password: userPassword, email_confirm: true }),
    });
    createdUserID = created.body?.id || created.body?.user?.id || "";
    results.push({ name: "auth.admin_create_user", ok: Boolean(createdUserID), status: created.response.status, detail: createdUserID ? "ok" : "missing user id" });
    const signedIn = await expectHTTP("auth.password_sign_in", `${connect.auth_url}/token?grant_type=password`, {
      method: "POST",
      headers: { apikey: anon, "content-type": "application/json" },
      body: JSON.stringify({ email: userEmail, password: userPassword }),
    }, [200], results);
    const accessToken = JSON.parse(signedIn.text).access_token;
    await expectHTTP("auth.get_user", `${connect.auth_url}/user`, {
      headers: { apikey: anon, authorization: `Bearer ${accessToken}` },
    }, [200], results);
  } finally {
    if (createdUserID) {
      await fetch(`${connect.auth_url}/admin/users/${createdUserID}`, {
        method: "DELETE",
        headers: serviceHeaders,
      });
    }
  }

  for (let attempt = 0; attempt < 30; attempt += 1) {
    const selected = await fetch(`${connect.rest_url}/${table}?id=eq.${encodeURIComponent(runID)}&select=id,app_ref,note`, {
      headers: serviceHeaders,
    });
    const text = await selected.text();
    if (selected.ok && text.includes(runID)) {
      results.push({ name: "rest.select_seeded_row", ok: true, status: selected.status, detail: "ok" });
      break;
    }
    if (attempt === 29) {
      results.push({ name: "rest.select_seeded_row", ok: false, status: selected.status, detail: text.slice(0, 700) });
    }
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }
  await expectHTTP("rest.insert_row", `${connect.rest_url}/${table}`, {
    method: "POST",
    headers: { ...serviceHeaders, prefer: "return=representation" },
    body: JSON.stringify({ id: `${runID}-rest`, app_ref: config.ref, note: "rest insert" }),
  }, [200, 201], results);
  await expectHTTP("graphql.introspection", `${connect.graphql_url}`, {
    method: "POST",
    headers: serviceHeaders,
    body: JSON.stringify({ query: "{ __typename }" }),
  }, [200], results);

  try {
    await expectHTTP("storage.create_bucket", `${connect.storage_url}/bucket`, {
      method: "POST",
      headers: serviceHeaders,
      body: JSON.stringify({ id: bucket, name: bucket, public: false }),
    }, [200, 201], results);
    await expectHTTP("storage.upload_object", `${connect.storage_url}/object/${bucket}/hello.txt`, {
      method: "POST",
      headers: { apikey: serviceRole, authorization: `Bearer ${serviceRole}`, "content-type": "text/plain", "x-upsert": "true" },
      body: `hello from ${config.ref} ${runID}\n`,
    }, [200, 201], results);
    await expectHTTP("storage.download_object", `${connect.storage_url}/object/${bucket}/hello.txt`, {
      headers: { apikey: serviceRole, authorization: `Bearer ${serviceRole}` },
    }, (response, text) => response.status === 200 && text.includes(`hello from ${config.ref}`), results);
  } finally {
    await fetch(`${connect.storage_url}/object/${bucket}`, {
      method: "DELETE",
      headers: serviceHeaders,
      body: JSON.stringify({ prefixes: ["hello.txt"] }),
    });
    await fetch(`${connect.storage_url}/bucket/${bucket}`, {
      method: "DELETE",
      headers: serviceHeaders,
    });
  }

  await expectHTTP("functions.main", `${connect.functions_url}/main`, {
    headers: anonHeaders,
  }, [200], results);
  await realtimeProbe(connect.realtime_url, anon, results);
  await expectHTTP("studio.forward_auth_without_session", connect.studio_url, { method: "HEAD" }, [401], results);

  runPSQLSync("db.cleanup", directURL, dbPassword, `delete from public.${table} where id in ('${runID}', '${runID}-rest');`, results);

  const failed = results.filter((result) => !result.ok);
  const redacted = {
    ref: config.ref,
    app: config.name,
    api_url: connect.api_url,
    run_id: runID,
    passed: results.length - failed.length,
    failed: failed.length,
    results,
  };
  writeFileSync(join(appDir, "last-run.json"), `${JSON.stringify(redacted, null, 2)}\n`, { mode: 0o600 });
  for (const result of results) {
    console.log(`${result.ok ? "PASS" : "FAIL"} ${config.ref}.${result.name}${result.status ? ` HTTP ${result.status}` : ""} ${result.detail ?? ""}`);
  }
  if (failed.length > 0) {
    throw new Error(`${config.ref} fake app smoke failed ${failed.length}/${results.length} checks`);
  }
}

async function orchestrate(args) {
  const envPath = resolve(repoRoot, args.envFile);
  const env = loadEnv(envPath);
  const apiURL = managementURL(env);
  const token = await login(apiURL, env);
  const appsRoot = resolve(repoRoot, args.appsRoot);
  mkdirSync(appsRoot, { recursive: true });

  for (const ref of args.refs) {
    const connect = await authedJSON(apiURL, token, `/v1/projects/${encodeURIComponent(ref)}/connect`);
    const appDir = join(appsRoot, `${ref}-app`);
    writeFakeApp(appDir, {
      name: `${ref}-fake-client`,
      ref,
      managementApiUrl: apiURL,
      apiUrl: connect.api_url,
      authUrl: connect.auth_url,
      restUrl: connect.rest_url,
      graphqlUrl: connect.graphql_url,
      realtimeUrl: connect.realtime_url,
      storageUrl: connect.storage_url,
      functionsUrl: connect.functions_url,
      studioUrl: connect.studio_url,
      publicDbHost: connect.postgres_parts?.public_direct?.host ?? "",
      publicPoolerHost: connect.postgres_parts?.public_transaction?.host ?? "",
      dbHostaddr: args.dbHostaddr,
    });
    console.log(`scaffolded ${appDir}`);
    await runFakeApp(appDir, resolve(repoRoot, args.envFile));
  }
}

const args = parseArgs(process.argv);
try {
  if (args.appDir) {
    await runFakeApp(resolve(process.cwd(), args.appDir), resolve(process.cwd(), args.envFile));
  } else {
    await orchestrate(args);
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
