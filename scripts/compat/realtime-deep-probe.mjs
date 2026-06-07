import { createClient } from "@supabase/supabase-js";

const required = [
  "SUPABASE_URL",
  "SUPABASE_ANON_KEY",
  "SUPABASE_SERVICE_ROLE_KEY",
  "SUPADUPA_REALTIME_TABLE",
  "SUPADUPA_REALTIME_DB_BROADCAST_CHANNEL",
  "SUPADUPA_REALTIME_DB_BROADCAST_TOPIC",
  "SUPADUPA_COMPAT_RUN_ID",
];
for (const name of required) {
  if (!process.env[name]) {
    console.error(`${name} is required`);
    process.exit(2);
  }
}

const timeoutMs = Number(process.env.SUPADUPA_REALTIME_TIMEOUT_MS || "60000");
const postgresChangesSettleMs = Number(process.env.SUPADUPA_REALTIME_POSTGRES_CHANGES_SETTLE_MS || "1500");
const postgresChangesRetryMs = Number(process.env.SUPADUPA_REALTIME_POSTGRES_CHANGES_RETRY_MS || "5000");
const runId = process.env.SUPADUPA_COMPAT_RUN_ID;
const table = process.env.SUPADUPA_REALTIME_TABLE;
const dbBroadcastChannel = process.env.SUPADUPA_REALTIME_DB_BROADCAST_CHANNEL;
const dbBroadcastTopic = process.env.SUPADUPA_REALTIME_DB_BROADCAST_TOPIC;
const dbBroadcastEvent = "compat-db-broadcast";
const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
  realtime: { timeout: timeoutMs },
});
const admin = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_SERVICE_ROLE_KEY, {
  auth: { persistSession: false, autoRefreshToken: false },
});

let finished = false;
const channels = [];
const realtimeUserIds = [];
const clients = [supabase];
const progress = [];

const record = (name, detail = {}) => {
  progress.push({ name, ...detail, at: new Date().toISOString() });
};

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const finish = async (code, payload) => {
  if (finished) return;
  finished = true;
  clearTimeout(timer);
  await Promise.all(channels.map(({ client, channel }) => client.removeChannel(channel).catch(() => {})));
  await Promise.all(clients.map((client) => client.removeAllChannels?.().catch(() => {})));
  await Promise.all(realtimeUserIds.map((userId) => admin.auth.admin.deleteUser(userId).catch(() => {})));
  if (code === 0) console.log(JSON.stringify(payload));
  else console.error(JSON.stringify(payload, null, 2));
  process.exit(code);
};

const timer = setTimeout(() => {
  finish(1, { error: "realtime deep probe timeout", table, run_id: runId, progress });
}, timeoutMs);

function trackChannel(client, channel) {
  channels.push({ client, channel });
  return channel;
}

function newSupabaseClient() {
  const client = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY, {
    auth: { persistSession: false, autoRefreshToken: false },
    realtime: { timeout: timeoutMs },
  });
  clients.push(client);
  return client;
}

async function runPresenceProbe() {
  return new Promise((resolve, reject) => {
    const channelName = `compat-presence-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
    const channel = trackChannel(supabase, supabase.channel(channelName, {
      config: {
        presence: { key: `probe-${runId}` },
      },
    }));

    channel.on("presence", { event: "sync" }, () => {
      const state = channel.presenceState();
      const flattened = Object.values(state).flat();
      if (flattened.some((entry) => entry?.run_id === runId && entry?.project_ref === process.env.SUPADUPA_TEST_REF)) {
        resolve({ channel: channelName, entries: flattened.length });
      }
    });

    channel.subscribe(async (status) => {
      record("presence.status", { status });
      if (status === "SUBSCRIBED") {
        const response = await channel.track({ run_id: runId, project_ref: process.env.SUPADUPA_TEST_REF, online_at: new Date().toISOString() });
        if (response !== "ok") {
          reject(new Error(`presence track failed: ${response}`));
        }
      } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT") {
        reject(new Error(`presence subscription failed: ${status}`));
      }
    });
  });
}

async function runPostgresChangesProbe() {
  return new Promise((resolve, reject) => {
    const channelName = `compat-pg-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-");
    const channel = trackChannel(supabase, supabase.channel(channelName));
    let settled = false;
    let retryTimer;
    let insertInFlight = false;
    let insertAttempts = 0;

    const clearRetry = () => {
      if (retryTimer) clearInterval(retryTimer);
      retryTimer = undefined;
    };

    const settle = (handler, payload) => {
      if (settled) return;
      settled = true;
      clearRetry();
      handler(payload);
    };

    const insertProbeRow = async () => {
      if (settled || insertInFlight) return;
      insertInFlight = true;
      insertAttempts += 1;
      record("postgres_changes.insert.started", { table, run_id: runId, attempt: insertAttempts });
      const { error } = await supabase.from(table).insert({ run_id: runId, body: `realtime deep ${runId}` });
      insertInFlight = false;
      if (error) {
        record("postgres_changes.insert.error", { message: error.message, details: error.details, hint: error.hint, code: error.code, attempt: insertAttempts });
        settle(reject, error);
        return;
      }
      record("postgres_changes.insert.finished", { table, run_id: runId, attempt: insertAttempts });
    };

    channel.on("postgres_changes", { event: "INSERT", schema: "public", table }, (payload) => {
      record("postgres_changes.message", { run_id: payload?.new?.run_id });
      if (payload?.new?.run_id === runId) {
        settle(resolve, { channel: channelName, row: payload.new, insert_attempts: insertAttempts });
      }
    });

    channel.subscribe(async (status) => {
      record("postgres_changes.status", { status });
      if (status === "SUBSCRIBED") {
        if (postgresChangesSettleMs > 0) {
          record("postgres_changes.settle.started", { delay_ms: postgresChangesSettleMs });
          await delay(postgresChangesSettleMs);
          record("postgres_changes.settle.finished", { delay_ms: postgresChangesSettleMs });
        }
        await insertProbeRow();
        if (!settled && postgresChangesRetryMs > 0) {
          retryTimer = setInterval(insertProbeRow, postgresChangesRetryMs);
        }
      } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT") {
        settle(reject, new Error(`postgres changes subscription failed: ${status}`));
      }
    });
  });
}

function expectDatabaseBroadcastPayload(message, expectedReplayState) {
  const payload = message?.payload;
  const record = payload?.record || payload?.new || payload?.data?.record || payload?.data?.new || {};
  if (record?.run_id !== runId) return false;
  if (record?.body !== `database broadcast ${runId}`) return false;
  const operation = payload?.operation || payload?.type || payload?.data?.operation;
  const payloadTable = payload?.table || payload?.data?.table;
  const schema = payload?.schema || payload?.data?.schema;
  if (operation && operation !== "INSERT") return false;
  if (payloadTable && payloadTable !== table) return false;
  if (schema && schema !== "public") return false;
  if (expectedReplayState === "live" && message?.meta?.replayed) return false;
  if (expectedReplayState === "replayed" && !message?.meta?.replayed) return false;
  return true;
}

async function createRealtimeUserSession(label, client = supabase) {
  const email = `compat-realtime-${label}-${runId}`.replace(/[^a-zA-Z0-9_-]/g, "-").slice(0, 48) + "@example.test";
  const password = `compat-${runId}-Supadupa2026!`;
  const { data: created, error: createError } = await admin.auth.admin.createUser({
    email,
    password,
    email_confirm: true,
  });
  if (createError) throw createError;
  const realtimeUserId = created?.user?.id;
  if (!realtimeUserId) throw new Error(`realtime test user ${label} was not created`);
  realtimeUserIds.push(realtimeUserId);

  const { data: sessionData, error: signInError } = await client.auth.signInWithPassword({ email, password });
  if (signInError) throw signInError;
  const accessToken = sessionData?.session?.access_token;
  if (!accessToken) throw new Error(`realtime test user ${label} did not receive an access token`);
  await client.realtime.setAuth(accessToken);
  record("auth.signed_in", { label, user_id: realtimeUserId });
  return { label, user_id: realtimeUserId, email };
}

function waitForPrivateChannelStatus(client, topic, expectSubscribed, label) {
  return new Promise((resolve, reject) => {
    let settled = false;
    let channel;
    const settle = (handler, payload) => {
      settled = true;
      clearTimeout(timeout);
      if (channel) {
        client.removeChannel(channel).catch(() => {});
      }
      handler(payload);
    };
    channel = trackChannel(client, client.channel(topic, {
      config: {
        private: true,
      },
    }));
    const timeout = setTimeout(() => {
      if (settled) return;
      settled = true;
      record("private_user_topic.timeout", { label, topic, expect_subscribed: expectSubscribed });
      if (expectSubscribed) {
        if (channel) {
          client.removeChannel(channel).catch(() => {});
        }
        reject(new Error(`${label} did not subscribe to ${topic}`));
      } else {
        if (channel) {
          client.removeChannel(channel).catch(() => {});
        }
        resolve({ label, topic, status: "NO_SUBSCRIBE" });
      }
    }, Math.min(10000, Math.max(3000, Math.floor(timeoutMs / 6))));

    channel.subscribe((status, error) => {
      record("private_user_topic.status", { label, topic, status, error: error?.message });
      if (settled) return;
      if (status === "SUBSCRIBED") {
        if (expectSubscribed) {
          settle(resolve, { label, topic, status });
        } else {
          settle(reject, new Error(`${label} unexpectedly subscribed to ${topic}`));
        }
      } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") {
        if (expectSubscribed) {
          settle(reject, new Error(`${label} failed to subscribe to ${topic}: ${status}${error ? ` ${error.message}` : ""}`));
        } else {
          settle(resolve, { label, topic, status, error: error?.message });
        }
      }
    });
  });
}

async function runPrivateUserTopicIsolationProbe(userA, userB, userBClient, anonClient) {
  const userATopic = `compat-user-${userA.user_id}`;
  const userBTopic = `compat-user-${userB.user_id}`;
  const owner = await waitForPrivateChannelStatus(supabase, userATopic, true, "user_a_own_topic");
  const crossUserDenied = await waitForPrivateChannelStatus(userBClient, userATopic, false, "user_b_user_a_topic");
  const otherOwner = await waitForPrivateChannelStatus(userBClient, userBTopic, true, "user_b_own_topic");
  const anonDenied = await waitForPrivateChannelStatus(anonClient, userATopic, false, "anon_user_a_topic");
  return { owner, cross_user_denied: crossUserDenied, other_owner: otherOwner, anon_denied: anonDenied };
}

async function runDatabaseBroadcastProbe() {
  return new Promise((resolve, reject) => {
    let delivered = false;
    const channel = trackChannel(supabase, supabase.channel(dbBroadcastChannel, {
      config: {
        private: true,
      },
    }));

    channel.on("broadcast", { event: dbBroadcastEvent }, (message) => {
      record("database_broadcast.message", { meta: message?.meta, payload: message?.payload });
      if (expectDatabaseBroadcastPayload(message, "live")) {
        delivered = true;
        supabase.removeChannel(channel).catch(() => {});
        resolve({ channel: dbBroadcastChannel, topic: dbBroadcastTopic, event: dbBroadcastEvent, row: message.payload.record || message.payload.new || message.payload.data?.record || message.payload.data?.new });
      }
    });
    channel.on("broadcast", { event: "*" }, (message) => {
      record("database_broadcast.any_message", { meta: message?.meta, payload: message?.payload });
    });

    channel.subscribe(async (status, error) => {
      record("database_broadcast.status", { status, error: error?.message });
      if (delivered && status === "CLOSED") {
        return;
      }
      if (status === "SUBSCRIBED") {
        const { error } = await supabase.from(table).insert({ run_id: runId, body: `database broadcast ${runId}` });
        if (error) {
          reject(error);
        }
      } else if (status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") {
        reject(new Error(`database broadcast subscription failed: ${status}${error ? ` ${error.message}` : ""}`));
      }
    });
  });
}

async function runBroadcastReplayProbe() {
  return new Promise((resolve, reject) => {
    const channel = trackChannel(supabase, supabase.channel(dbBroadcastChannel, {
      config: {
        private: true,
        broadcast: { replay: { since: Date.now() - 5 * 60 * 1000, limit: 10 } },
      },
    }));

    channel.on("broadcast", { event: dbBroadcastEvent }, (message) => {
      record("broadcast_replay.message", { meta: message?.meta, payload: message?.payload });
      if (expectDatabaseBroadcastPayload(message, "replayed")) {
        resolve({ channel: dbBroadcastChannel, topic: dbBroadcastTopic, event: dbBroadcastEvent, replayed: true, id: message.meta?.id });
      }
    });
    channel.on("broadcast", { event: "*" }, (message) => {
      record("broadcast_replay.any_message", { meta: message?.meta, payload: message?.payload });
    });

    channel.subscribe((status, error) => {
      record("broadcast_replay.status", { status, error: error?.message });
      if (status === "CHANNEL_ERROR" || status === "TIMED_OUT" || status === "CLOSED") {
        reject(new Error(`broadcast replay subscription failed: ${status}${error ? ` ${error.message}` : ""}`));
      }
    });
  });
}

try {
  const realtimeUser = await createRealtimeUserSession("a", supabase);
  const presence = await runPresenceProbe();
  record("presence.done", presence);
  const postgresChanges = await runPostgresChangesProbe();
  record("postgres_changes.done", postgresChanges);
  const databaseBroadcast = await runDatabaseBroadcastProbe();
  record("database_broadcast.done", databaseBroadcast);
  const broadcastReplay = await runBroadcastReplayProbe();
  record("broadcast_replay.done", broadcastReplay);
  const userBClient = newSupabaseClient();
  const realtimeUserB = await createRealtimeUserSession("b", userBClient);
  const anonClient = newSupabaseClient();
  const privateUserTopics = await runPrivateUserTopicIsolationProbe(realtimeUser, realtimeUserB, userBClient, anonClient);
  record("private_user_topics.done", privateUserTopics);
  await finish(0, { ok: true, realtime_user: realtimeUser, realtime_user_b: realtimeUserB, private_user_topics: privateUserTopics, presence, postgres_changes: postgresChanges, database_broadcast: databaseBroadcast, broadcast_replay: broadcastReplay });
} catch (error) {
  await finish(1, { error: error?.message || String(error), table, run_id: runId, progress });
}
