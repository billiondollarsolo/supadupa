import { createClient } from "@supabase/supabase-js";

const required = ["SUPABASE_URL", "SUPABASE_ANON_KEY", "SUPADUPA_TEST_REF"];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const timeoutMs = Number(process.env.SUPADUPA_REALTIME_TIMEOUT_MS || "10000");
const runId = process.env.SUPADUPA_COMPAT_RUN_ID || `${Date.now()}`;
const channelName = `compat-${process.env.SUPADUPA_TEST_REF}-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
const eventName = "compat-broadcast";

const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
  realtime: { timeout: timeoutMs },
});

let finished = false;
let timer;
let channel;

const finish = async (code, payload) => {
  if (finished) return;
  finished = true;
  clearTimeout(timer);
  if (channel) {
    await supabase.removeChannel(channel).catch(() => {});
  }
  if (code === 0) console.log(JSON.stringify(payload));
  else console.error(JSON.stringify(payload, null, 2));
  process.exit(code);
};

timer = setTimeout(() => {
  finish(1, { error: "realtime broadcast timeout", channel: channelName });
}, timeoutMs);

channel = supabase.channel(channelName, {
  config: {
    broadcast: { self: true },
  },
});

channel.on("broadcast", { event: eventName }, (message) => {
  if (message?.payload?.run_id === runId && message?.payload?.project_ref === process.env.SUPADUPA_TEST_REF) {
    finish(0, {
      ok: true,
      channel: channelName,
      event: eventName,
      run_id: runId,
    });
  }
});

channel.subscribe(async (status) => {
  if (status === "SUBSCRIBED") {
    const response = await channel.send({
      type: "broadcast",
      event: eventName,
      payload: {
        run_id: runId,
        project_ref: process.env.SUPADUPA_TEST_REF,
      },
    });
    if (response !== "ok") {
      await finish(1, { error: "broadcast send failed", response });
    }
  } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") {
    await finish(1, { error: "channel subscription failed", status });
  }
});
