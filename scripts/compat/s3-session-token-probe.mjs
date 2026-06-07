import crypto from "node:crypto";

const required = [
  "SUPABASE_S3_ENDPOINT",
  "SUPABASE_S3_ACCESS_KEY",
  "SUPABASE_S3_SECRET_KEY",
  "SUPABASE_S3_SESSION_TOKEN",
  "SUPABASE_S3_OTHER_SESSION_TOKEN",
  "SUPADUPA_S3_BUCKET",
  "SUPADUPA_S3_USER_OBJECT_KEY",
  "SUPADUPA_S3_OTHER_OBJECT_KEY",
  "SUPADUPA_S3_USER_OBJECT_BODY",
  "SUPADUPA_S3_OTHER_OBJECT_BODY",
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
const userSessionToken = process.env.SUPABASE_S3_SESSION_TOKEN;
const otherSessionToken = process.env.SUPABASE_S3_OTHER_SESSION_TOKEN;
const bucket = process.env.SUPADUPA_S3_BUCKET;
const userObjectKey = process.env.SUPADUPA_S3_USER_OBJECT_KEY;
const otherObjectKey = process.env.SUPADUPA_S3_OTHER_OBJECT_KEY;
const userObjectBody = process.env.SUPADUPA_S3_USER_OBJECT_BODY;
const otherObjectBody = process.env.SUPADUPA_S3_OTHER_OBJECT_BODY;

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

function awsEncode(value) {
  return encodeURIComponent(value).replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`);
}

function canonicalQuery(searchParams) {
  return [...searchParams.entries()]
    .sort(([aKey, aValue], [bKey, bValue]) => (aKey === bKey ? aValue.localeCompare(bValue) : aKey.localeCompare(bKey)))
    .map(([key, value]) => `${awsEncode(key)}=${awsEncode(value)}`)
    .join("&");
}

function signingKeyForDate(shortDate) {
  const dateKey = hmac(`AWS4${secretKey}`, shortDate);
  const regionKey = hmac(dateKey, region);
  const serviceKey = hmac(regionKey, "s3");
  return hmac(serviceKey, "aws4_request");
}

async function s3Request(method, path, body = "", sessionToken = userSessionToken, extraHeaders = {}) {
  const url = new URL(endpoint + path);
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:-]|\.\d{3}/g, "");
  const shortDate = amzDate.slice(0, 8);
  const payloadHash = sha256(body);
  const headers = {
    host: url.host,
    "x-amz-content-sha256": payloadHash,
    "x-amz-date": amzDate,
    "x-amz-security-token": sessionToken,
  };
  for (const [key, value] of Object.entries(extraHeaders)) {
    headers[key.toLowerCase()] = String(value);
  }
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
  const credentialScope = `${shortDate}/${region}/s3/aws4_request`;
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256(canonicalRequest)].join("\n");
  const signature = hmac(signingKeyForDate(shortDate), stringToSign, "hex");
  headers.authorization = `AWS4-HMAC-SHA256 Credential=${accessKey}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;

  const response = await fetch(url, { method, headers, body: body || undefined });
  const text = method === "HEAD" ? "" : await response.text();
  return { status: response.status, text, headers: response.headers };
}

function assertStatus(name, result, allowed) {
  if (!allowed.includes(result.status)) {
    throw new Error(`${name} expected HTTP ${allowed.join("/")}, got ${result.status}: ${result.text.slice(0, 500)}`);
  }
}

function assertRejected(name, result) {
  if (![400, 401, 403, 404].includes(result.status)) {
    throw new Error(`${name} expected rejection, got HTTP ${result.status}: ${result.text.slice(0, 500)}`);
  }
}

const userPath = `/${bucket}/${userObjectKey}`;
const otherPath = `/${bucket}/${otherObjectKey}`;

const userPut = await s3Request("PUT", userPath, userObjectBody, userSessionToken, { "content-type": "text/plain" });
assertStatus("user put object", userPut, [200, 201]);

const userGet = await s3Request("GET", userPath, "", userSessionToken);
assertStatus("user get own object", userGet, [200]);
if (userGet.text !== userObjectBody) {
  throw new Error(`user get body mismatch: ${JSON.stringify(userGet.text)}`);
}

const otherGetUser = await s3Request("GET", userPath, "", otherSessionToken);
assertRejected("other user get first user object", otherGetUser);

const otherPut = await s3Request("PUT", otherPath, otherObjectBody, otherSessionToken, { "content-type": "text/plain" });
assertStatus("other user put object", otherPut, [200, 201]);

const userGetOther = await s3Request("GET", otherPath, "", userSessionToken);
assertRejected("first user get other user object", userGetOther);

const otherGetOwn = await s3Request("GET", otherPath, "", otherSessionToken);
assertStatus("other user get own object", otherGetOwn, [200]);
if (otherGetOwn.text !== otherObjectBody) {
  throw new Error(`other user get body mismatch: ${JSON.stringify(otherGetOwn.text)}`);
}

console.log(JSON.stringify({
  ok: true,
  bucket,
  operations: [
    "session-user-put",
    "session-user-get-own",
    "session-other-denied-user-object",
    "session-other-put",
    "session-user-denied-other-object",
    "session-other-get-own",
  ],
}));
