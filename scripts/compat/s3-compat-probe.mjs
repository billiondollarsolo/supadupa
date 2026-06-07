import crypto from "node:crypto";

const required = [
  "SUPABASE_S3_ENDPOINT",
  "SUPABASE_S3_ACCESS_KEY",
  "SUPABASE_S3_SECRET_KEY",
  "SUPADUPA_S3_BUCKET",
  "SUPADUPA_S3_OBJECT_KEY",
  "SUPADUPA_S3_OBJECT_BODY",
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
const objectKey = process.env.SUPADUPA_S3_OBJECT_KEY;
const objectBody = process.env.SUPADUPA_S3_OBJECT_BODY;

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

async function s3Request(method, path, body = "", extraHeaders = {}) {
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
  const credentialScope = `${shortDate}/${region}/${service}/aws4_request`;
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256(canonicalRequest)].join("\n");
  const signingKey = signingKeyForDate(shortDate);
  const signature = hmac(signingKey, stringToSign, "hex");
  headers.authorization = `AWS4-HMAC-SHA256 Credential=${accessKey}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;

  const response = await fetch(url, { method, headers, body: body || undefined });
  const text = method === "HEAD" ? "" : await response.text();
  return { status: response.status, text, headers: response.headers };
}

function presignedUrl(method, path, expiresSeconds = 120) {
  const url = new URL(endpoint + path);
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:-]|\.\d{3}/g, "");
  const shortDate = amzDate.slice(0, 8);
  const credentialScope = `${shortDate}/${region}/s3/aws4_request`;
  url.searchParams.set("X-Amz-Algorithm", "AWS4-HMAC-SHA256");
  url.searchParams.set("X-Amz-Credential", `${accessKey}/${credentialScope}`);
  url.searchParams.set("X-Amz-Date", amzDate);
  url.searchParams.set("X-Amz-Expires", String(expiresSeconds));
  url.searchParams.set("X-Amz-SignedHeaders", "host");
  const canonicalRequest = [
    method,
    encodePath(url.pathname),
    canonicalQuery(url.searchParams),
    `host:${url.host}\n`,
    "host",
    "UNSIGNED-PAYLOAD",
  ].join("\n");
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256(canonicalRequest)].join("\n");
  url.searchParams.set("X-Amz-Signature", hmac(signingKeyForDate(shortDate), stringToSign, "hex"));
  return url;
}

function assertStatus(name, result, allowed) {
  if (!allowed.includes(result.status)) {
    throw new Error(`${name} expected HTTP ${allowed.join("/")}, got ${result.status}: ${result.text.slice(0, 500)}`);
  }
}

const listBuckets = await s3Request("GET", "");
assertStatus("list buckets", listBuckets, [200]);
if (!listBuckets.text.includes(bucket)) {
  throw new Error(`list buckets did not include ${bucket}: ${listBuckets.text.slice(0, 500)}`);
}

const objectPath = `/${bucket}/${objectKey}`;
const metadataValue = `run-${sha256(objectBody).slice(0, 12)}`;
const putObject = await s3Request("PUT", objectPath, objectBody, {
  "content-type": "text/plain; charset=utf-8",
  "x-amz-meta-supadupa-compat": metadataValue,
});
assertStatus("put object", putObject, [200, 201]);

const headObject = await s3Request("HEAD", objectPath);
assertStatus("head object", headObject, [200]);
if ((headObject.headers.get("x-amz-meta-supadupa-compat") || "") !== metadataValue) {
  throw new Error(`head object metadata mismatch: ${headObject.headers.get("x-amz-meta-supadupa-compat")}`);
}

const getObject = await s3Request("GET", objectPath);
assertStatus("get object", getObject, [200]);
if (getObject.text !== objectBody) {
  throw new Error(`get object body mismatch: ${JSON.stringify(getObject.text)}`);
}

const rangeObject = await s3Request("GET", objectPath, "", { range: "bytes=0-7" });
assertStatus("range get object", rangeObject, [206]);
if (rangeObject.text !== objectBody.slice(0, 8)) {
  throw new Error(`range object body mismatch: ${JSON.stringify(rangeObject.text)}`);
}

const signedGet = await fetch(presignedUrl("GET", objectPath));
const signedText = await signedGet.text();
if (signedGet.status !== 200 || signedText !== objectBody) {
  throw new Error(`presigned get expected HTTP 200 body match, got ${signedGet.status}: ${signedText.slice(0, 500)}`);
}

const listObjects = await s3Request("GET", `/${bucket}?list-type=2`);
assertStatus("list objects", listObjects, [200]);
if (!listObjects.text.includes(objectKey)) {
  throw new Error(`list objects did not include ${objectKey}: ${listObjects.text.slice(0, 500)}`);
}

const copiedKey = `copy-${objectKey}`;
const copyObject = await s3Request("PUT", `/${bucket}/${copiedKey}`, "", {
  "x-amz-copy-source": `/${bucket}/${objectKey}`,
});
assertStatus("copy object", copyObject, [200, 201]);
const getCopiedObject = await s3Request("GET", `/${bucket}/${copiedKey}`);
assertStatus("get copied object", getCopiedObject, [200]);
if (getCopiedObject.text !== objectBody) {
  throw new Error(`copied object body mismatch: ${JSON.stringify(getCopiedObject.text)}`);
}

const deleteCopiedObject = await s3Request("DELETE", `/${bucket}/${copiedKey}`);
assertStatus("delete copied object", deleteCopiedObject, [200, 204]);

const deleteObject = await s3Request("DELETE", objectPath);
assertStatus("delete object", deleteObject, [200, 204]);

console.log(JSON.stringify({
  ok: true,
  bucket,
  object_key: objectKey,
  operations: ["list-buckets", "put-object", "head-object", "get-object", "range-get-object", "presigned-get-object", "list-objects", "copy-object", "delete-copied-object", "delete-object"],
}));
