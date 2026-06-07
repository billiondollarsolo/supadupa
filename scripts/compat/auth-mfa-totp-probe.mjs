import { createHmac } from "node:crypto";
import { createClient } from "@supabase/supabase-js";

const required = [
  "SUPABASE_URL",
  "SUPABASE_ANON_KEY",
  "SUPABASE_SERVICE_ROLE_KEY",
  "SUPADUPA_COMPAT_RUN_ID",
];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const runId = process.env.SUPADUPA_COMPAT_RUN_ID.replace(/[^a-zA-Z0-9_-]/g, "-").slice(0, 48);
const email = `compat-auth-mfa-${runId}@example.test`;
const password = `CompatMfa2026-${runId}!`;
const supabaseUrl = process.env.SUPABASE_URL;
const anonKey = process.env.SUPABASE_ANON_KEY;
const serviceRoleKey = process.env.SUPABASE_SERVICE_ROLE_KEY;

const admin = createClient(supabaseUrl, serviceRoleKey, {
  auth: { persistSession: false, autoRefreshToken: false },
});
const client = createClient(supabaseUrl, anonKey, {
  auth: { persistSession: false, autoRefreshToken: false },
});

function base32Decode(value) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const normalized = String(value).toUpperCase().replace(/=+$/g, "").replace(/[^A-Z2-7]/g, "");
  let bits = "";
  for (const char of normalized) {
    const index = alphabet.indexOf(char);
    if (index === -1) throw new Error(`invalid base32 character ${char}`);
    bits += index.toString(2).padStart(5, "0");
  }
  const bytes = [];
  for (let offset = 0; offset + 8 <= bits.length; offset += 8) {
    bytes.push(Number.parseInt(bits.slice(offset, offset + 8), 2));
  }
  return Buffer.from(bytes);
}

function totp(secret, now = Date.now()) {
  const key = base32Decode(secret);
  const counter = Math.floor(now / 1000 / 30);
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(counter));
  const digest = createHmac("sha1", key).update(message).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const binary =
    ((digest[offset] & 0x7f) << 24) |
    ((digest[offset + 1] & 0xff) << 16) |
    ((digest[offset + 2] & 0xff) << 8) |
    (digest[offset + 3] & 0xff);
  return String(binary % 1_000_000).padStart(6, "0");
}

function extractSecret(enrollData) {
  const direct = enrollData?.totp?.secret || enrollData?.secret;
  if (direct) return direct;
  const uri = enrollData?.totp?.uri || enrollData?.totp?.qr_code || enrollData?.qr_code || "";
  if (uri) {
    const parsed = new URL(uri);
    const secret = parsed.searchParams.get("secret");
    if (secret) return secret;
  }
  throw new Error(`MFA enroll response did not include a TOTP secret: ${JSON.stringify(enrollData)}`);
}

let userId = "";
let factorId = "";
try {
  const created = await admin.auth.admin.createUser({
    email,
    password,
    email_confirm: true,
  });
  if (created.error) throw created.error;
  userId = created.data.user?.id || "";
  if (!userId) throw new Error("admin createUser did not return user id");

  const signedIn = await client.auth.signInWithPassword({ email, password });
  if (signedIn.error) throw signedIn.error;
  if (!signedIn.data.session?.access_token) throw new Error("signInWithPassword did not return a session");

  const enrolled = await client.auth.mfa.enroll({
    factorType: "totp",
    friendlyName: `compat-${runId}`,
  });
  if (enrolled.error) throw enrolled.error;
  factorId = enrolled.data?.id || enrolled.data?.factorId || "";
  if (!factorId) throw new Error(`MFA enroll did not return a factor id: ${JSON.stringify(enrolled.data)}`);

  const secret = extractSecret(enrolled.data);
  const challenged = await client.auth.mfa.challenge({ factorId });
  if (challenged.error) throw challenged.error;
  const challengeId = challenged.data?.id;
  if (!challengeId) throw new Error(`MFA challenge did not return an id: ${JSON.stringify(challenged.data)}`);

  const verified = await client.auth.mfa.verify({
    factorId,
    challengeId,
    code: totp(secret),
  });
  if (verified.error) throw verified.error;
  if (!verified.data?.access_token && !verified.data?.session?.access_token) {
    throw new Error("MFA verify did not return an access token or session");
  }

  const aal = await client.auth.mfa.getAuthenticatorAssuranceLevel();
  if (aal.error) throw aal.error;
  if (aal.data?.currentLevel !== "aal2") {
    throw new Error(`expected currentLevel aal2, got ${aal.data?.currentLevel}`);
  }

  const factors = await client.auth.mfa.listFactors();
  if (factors.error) throw factors.error;
  const totpFactors = factors.data?.totp || [];
  const factor = totpFactors.find((entry) => entry.id === factorId);
  if (!factor) throw new Error("verified TOTP factor missing from listFactors response");
  if (factor.status && factor.status !== "verified") {
    throw new Error(`expected verified factor status, got ${factor.status}`);
  }

  console.log(JSON.stringify({
    ok: true,
    email,
    factor_id: factorId,
    aal: aal.data,
    factor_status: factor.status || "verified",
  }));
} finally {
  if (factorId) {
    await client.auth.mfa.unenroll({ factorId }).catch(() => {});
  }
  if (userId) {
    await admin.auth.admin.deleteUser(userId).catch(() => {});
  }
}
