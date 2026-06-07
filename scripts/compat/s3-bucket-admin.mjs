import crypto from "node:crypto";

const required = [
  "SUPABASE_S3_ENDPOINT",
  "SUPABASE_S3_ACCESS_KEY",
  "SUPABASE_S3_SECRET_KEY",
  "SUPADUPA_S3_BUCKET",
  "SUPADUPA_S3_ACTION",
];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const endpoint = process.env.SUPABASE_S3_ENDPOINT.replace(/\/+$/, "");
const accessKey = process.env.SUPABASE_S3_ACCESS_KEY;
const secretKey = process.env.SUPABASE_S3_SECRET_KEY;
const region = process.env.SUPABASE_S3_REGION || "us-east-1";
const bucket = process.env.SUPADUPA_S3_BUCKET;
const action = process.env.SUPADUPA_S3_ACTION;

function hmac(key, data, encoding) {
  return crypto.createHmac("sha256", key).update(data).digest(encoding);
}

function sha256(data, encoding = "hex") {
  return crypto.createHash("sha256").update(data).digest(encoding);
}

function encodePath(path) {
  return path
    .split("/")
    .map((part) => encodeURIComponent(part).replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`))
    .join("/");
}

function canonicalQuery(searchParams) {
  return [...searchParams.entries()]
    .sort(([aKey, aValue], [bKey, bValue]) => (aKey === bKey ? aValue.localeCompare(bValue) : aKey.localeCompare(bKey)))
    .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
    .join("&");
}

async function s3Request(method, path, body = "") {
  const url = new URL(endpoint + path);
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:-]|\.\d{3}/g, "");
  const shortDate = amzDate.slice(0, 8);
  const service = "s3";
  const payloadHash = sha256(body);
  const headers = {
    host: url.host,
    "x-amz-content-sha256": payloadHash,
    "x-amz-date": amzDate,
  };
  const headerNames = Object.keys(headers).sort();
  const canonicalHeaders = headerNames.map((name) => `${name}:${headers[name]}\n`).join("");
  const signedHeaders = headerNames.join(";");
  const canonicalRequest = [
    method,
    encodePath(url.pathname),
    canonicalQuery(url.searchParams),
    canonicalHeaders,
    signedHeaders,
    payloadHash,
  ].join("\n");
  const credentialScope = `${shortDate}/${region}/${service}/aws4_request`;
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256(canonicalRequest)].join("\n");
  const dateKey = hmac(`AWS4${secretKey}`, shortDate);
  const regionKey = hmac(dateKey, region);
  const serviceKey = hmac(regionKey, service);
  const signingKey = hmac(serviceKey, "aws4_request");
  const signature = hmac(signingKey, stringToSign, "hex");
  headers.authorization = `AWS4-HMAC-SHA256 Credential=${accessKey}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;

  const response = await fetch(url, { method, headers, body: body || undefined });
  const text = await response.text();
  return { status: response.status, text };
}

function assertStatus(name, result, allowed) {
  if (!allowed.includes(result.status)) {
    throw new Error(`${name} expected HTTP ${allowed.join("/")}, got ${result.status}: ${result.text.slice(0, 500)}`);
  }
}

if (action === "create") {
  const create = await s3Request("PUT", `/${bucket}`);
  assertStatus("create bucket", create, [200, 201, 409]);
  const list = await s3Request("GET", "");
  assertStatus("list buckets", list, [200]);
  if (!list.text.includes(bucket)) {
    throw new Error(`list buckets did not include ${bucket}: ${list.text.slice(0, 500)}`);
  }
  console.log(JSON.stringify({ ok: true, action, bucket }));
} else if (action === "delete") {
  const result = await s3Request("DELETE", `/${bucket}`);
  assertStatus("delete bucket", result, [200, 204, 404]);
  console.log(JSON.stringify({ ok: true, action, bucket, status: result.status }));
} else {
  throw new Error(`unsupported SUPADUPA_S3_ACTION ${action}`);
}
