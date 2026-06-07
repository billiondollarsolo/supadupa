import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createClient } from "@supabase/supabase-js";

const execFileAsync = promisify(execFile);

const required = [
  "SUPABASE_URL",
  "SUPABASE_ANON_KEY",
  "SUPADUPA_COMPAT_RUN_ID",
  "SUPADUPA_REALTIME_RESTART_CONTAINER",
];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const timeoutMs = Number(process.env.SUPADUPA_REALTIME_RECONNECT_TIMEOUT_MS || "60000");
const runId = process.env.SUPADUPA_COMPAT_RUN_ID;
const container = process.env.SUPADUPA_REALTIME_RESTART_CONTAINER;
const event = `compat-reconnect-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
const channelName = `compat-reconnect-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
const progress = [];

const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
  realtime: { timeout: timeoutMs },
});

let finished = false;
let restartStarted = false;
let restartFinished = false;
let reconnectObserved = false;
let initialSubscribed = false;
let broadcastSent = false;

const record = (name, detail = {}) => {
  progress.push({ name, ...detail, at: new Date().toISOString() });
};

const finish = async (code, payload) => {
  if (finished) return;
  finished = true;
  clearTimeout(timer);
  await supabase.removeAllChannels().catch(() => {});
  supabase.realtime.disconnect();
  if (code === 0) console.log(JSON.stringify(payload));
  else console.error(JSON.stringify(payload, null, 2));
  process.exit(code);
};

const timer = setTimeout(() => {
  finish(1, {
    error: "realtime reconnect probe timeout",
    channel: channelName,
    restart_started: restartStarted,
    restart_finished: restartFinished,
    reconnect_observed: reconnectObserved,
    broadcast_sent: broadcastSent,
    progress,
  });
}, timeoutMs);

async function restartRealtime() {
  restartStarted = true;
  record("restart.started", { container });
  try {
    const { stdout, stderr } = await execFileAsync("docker", ["restart", container], { timeout: timeoutMs - 5000 });
    restartFinished = true;
    record("restart.finished", { stdout: stdout.trim(), stderr: stderr.trim() });
  } catch (error) {
    await finish(1, {
      error: `docker restart failed: ${error?.message || String(error)}`,
      stdout: error?.stdout,
      stderr: error?.stderr,
      progress,
    });
  }
}

function maybeSendAfterReconnect(channel) {
  if (!restartFinished || !reconnectObserved || broadcastSent) {
    return;
  }
  broadcastSent = true;
  record("broadcast.send.started", { channel: channelName, event });
  channel
    .send({
      type: "broadcast",
      event,
      payload: { run_id: runId, after_restart: true },
    })
    .then((response) => {
      record("broadcast.send.finished", { response });
      if (response !== "ok") {
        finish(1, { error: `broadcast send after reconnect failed: ${response}`, progress });
      }
    })
    .catch((error) => finish(1, { error: error?.message || String(error), progress }));
}

const channel = supabase.channel(channelName, {
  config: {
    broadcast: { self: true },
  },
});

channel.on("broadcast", { event }, (message) => {
  record("broadcast.received", { payload: message?.payload });
  if (message?.payload?.run_id === runId && message?.payload?.after_restart === true) {
    finish(0, {
      ok: true,
      channel: channelName,
      event,
      restart_container: container,
      progress,
    });
  }
});

channel.subscribe((status, error) => {
  record("channel.status", { status, error: error?.message });
  if (status === "SUBSCRIBED" && !initialSubscribed) {
    initialSubscribed = true;
    void restartRealtime();
    return;
  }
  if (status === "SUBSCRIBED" && restartStarted) {
    reconnectObserved = true;
    maybeSendAfterReconnect(channel);
    return;
  }
  if ((status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") && !restartStarted) {
    void finish(1, { error: `subscription failed before restart: ${status}${error ? ` ${error.message}` : ""}`, progress });
  }
});
