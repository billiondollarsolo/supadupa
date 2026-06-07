#!/usr/bin/env node
const WebSocket = require("ws");

const realtimeUrl = process.env.SUPADUPA_REALTIME_URL || "";
const key = process.env.SUPADUPA_REALTIME_KEY || "";
const expect = process.env.SUPADUPA_REALTIME_EXPECT || "accept";
const timeoutMs = Number(process.env.SUPADUPA_REALTIME_TIMEOUT_MS || "7000");
const insecureSkipVerify = /^(1|true|yes|on)$/i.test(process.env.SUPADUPA_REALTIME_INSECURE_SKIP_VERIFY || "");
const resolveIP = process.env.SUPADUPA_REALTIME_RESOLVE_IP || "";

if (!realtimeUrl) {
  console.error("SUPADUPA_REALTIME_URL is required");
  process.exit(2);
}

if (!key && expect !== "reject") {
  console.error("SUPADUPA_REALTIME_KEY is required unless expecting rejection");
  process.exit(2);
}

const url = new URL(realtimeUrl);
url.protocol = url.protocol === "https:" ? "wss:" : url.protocol === "http:" ? "ws:" : url.protocol;
url.pathname = url.pathname.replace(/\/$/, "") + "/websocket";
const search = new URLSearchParams({ vsn: "1.0.0" });
if (key) search.set("apikey", key);
url.search = search.toString();

let finished = false;
const finish = (code, message) => {
  if (finished) return;
  finished = true;
  if (message) {
    if (code === 0) console.log(message);
    else console.error(message);
  }
  process.exit(code);
};

const timer = setTimeout(() => finish(1, "websocket timeout"), timeoutMs);
const headers = key ? { apikey: key } : {};
const options = { headers };
if (insecureSkipVerify) {
  options.rejectUnauthorized = false;
}
if (resolveIP) {
  options.servername = url.hostname;
  options.headers = { ...options.headers, Host: url.hostname };
  options.lookup = (hostname, lookupOptions, callback) => {
    const family = resolveIP.includes(":") ? 6 : 4;
    if (hostname === url.hostname) {
      if (lookupOptions?.all) {
        callback(null, [{ address: resolveIP, family }]);
        return;
      }
      callback(null, resolveIP, family);
      return;
    }
    require("dns").lookup(hostname, lookupOptions, callback);
  };
}
const ws = new WebSocket(url.toString(), options);

ws.on("open", () => {
  clearTimeout(timer);
  ws.close();
  if (expect === "accept") finish(0, "101");
  else finish(1, "unexpected websocket acceptance");
});

ws.on("unexpected-response", (_req, res) => {
  clearTimeout(timer);
  const status = res.statusCode || 0;
  if (expect === "reject" && (status === 401 || status === 403)) finish(0, String(status));
  finish(1, `unexpected websocket response ${status}`);
});

ws.on("error", (error) => {
  clearTimeout(timer);
  finish(1, error.message);
});
