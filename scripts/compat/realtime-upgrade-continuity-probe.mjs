import fs from "node:fs";
import { createClient } from "@supabase/supabase-js";

const required = [
  "SUPABASE_URL",
  "SUPABASE_ANON_KEY",
  "SUPADUPA_COMPAT_RUN_ID",
  "SUPADUPA_REALTIME_READY_FILE",
  "SUPADUPA_REALTIME_CHECK_FILE",
];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const timeoutMs = Number(process.env.SUPADUPA_REALTIME_UPGRADE_TIMEOUT_MS || "180000");
const pollMs = Number(process.env.SUPADUPA_REALTIME_UPGRADE_POLL_MS || "1000");
const runId = process.env.SUPADUPA_COMPAT_RUN_ID;
const readyFile = process.env.SUPADUPA_REALTIME_READY_FILE;
const checkFile = process.env.SUPADUPA_REALTIME_CHECK_FILE;
const event = `compat-upgrade-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
const channelName = `compat-upgrade-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
const progress = [];

const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
  realtime: { timeout: timeoutMs },
});

let finished = false;
let ready = false;
let checkRequested = false;
let reconnectObserved = false;
let broadcastSent = false;
let currentlySubscribed = false;

const record = (name, detail = {}) => {
  progress.push({ name, ...detail, at: new Date().toISOString() });
};

const finish = async (code, payload) => {
  if (finished) return;
  finished = true;
  clearTimeout(timer);
  clearInterval(checkTimer);
  await supabase.removeAllChannels().catch(() => {});
  supabase.realtime.disconnect();
  const body = JSON.stringify(payload, null, code === 0 ? 0 : 2);
  if (code === 0) console.log(body);
  else console.error(body);
  process.exit(code);
};

const timer = setTimeout(() => {
  void finish(1, {
    error: "realtime upgrade continuity probe timeout",
    channel: channelName,
    ready,
    check_requested: checkRequested,
    reconnect_observed: reconnectObserved,
    broadcast_sent: broadcastSent,
    progress,
  });
}, timeoutMs);

const channel = supabase.channel(channelName, {
  config: {
    broadcast: { self: true },
  },
});

function requestPostUpgradeBroadcast() {
  if (!checkRequested || !reconnectObserved || !currentlySubscribed || broadcastSent) {
    return;
  }
  broadcastSent = true;
  record("broadcast.send.started", { channel: channelName, event });
  channel
    .send({
      type: "broadcast",
      event,
      payload: { run_id: runId, after_upgrade: true },
    })
    .then((response) => {
      record("broadcast.send.finished", { response });
      if (response !== "ok") {
        void finish(1, { error: `broadcast send after upgrade failed: ${response}`, progress });
      }
    })
    .catch((error) => void finish(1, { error: error?.message || String(error), progress }));
}

const checkTimer = setInterval(() => {
  if (checkRequested || !fs.existsSync(checkFile)) {
    return;
  }
  checkRequested = true;
  record("check.requested", { file: checkFile });
  requestPostUpgradeBroadcast();
}, pollMs);

channel.on("broadcast", { event }, (message) => {
  record("broadcast.received", { payload: message?.payload });
  if (message?.payload?.run_id === runId && message?.payload?.after_upgrade === true) {
    void finish(0, {
      ok: true,
      channel: channelName,
      event,
      reconnect_observed: reconnectObserved,
      progress,
    });
  }
});

channel.subscribe((status, error) => {
  record("channel.status", { status, error: error?.message });
  if (status === "SUBSCRIBED") {
    currentlySubscribed = true;
  } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") {
    currentlySubscribed = false;
  }
  if (status === "SUBSCRIBED" && !ready) {
    ready = true;
    fs.writeFileSync(readyFile, JSON.stringify({ channel: channelName, at: new Date().toISOString() }));
    return;
  }
  if (status === "SUBSCRIBED" && ready) {
    reconnectObserved = true;
    requestPostUpgradeBroadcast();
    return;
  }
  if ((status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") && checkRequested && !reconnectObserved) {
    record("channel.disrupted_after_check", { status });
  }
});
