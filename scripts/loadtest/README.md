# Supadupa load test

A repeatable real-world load/soak test for a Supadupa-hosted Supabase project,
driven by the official `@supabase/supabase-js` SDK over the project's **public
DNS** endpoints. It exercises every surface continuously for a configurable
duration: REST/tables (insert/select/update/delete), Storage (buckets +
file upload/download/list/remove), Edge Functions, Auth (admin create / sign-in /
get-user), GraphQL, and Realtime subscriptions.

## Install

```bash
cd scripts/loadtest
npm install
```

## Run

```bash
export SUPABASE_URL="https://<ref>.apps.<domain>"
export SUPABASE_ANON_KEY="<anon jwt>"
export SUPABASE_SERVICE_ROLE_KEY="<service_role jwt>"

# Optional — only needed the first time, to create tables + deploy functions
# via the management API (buckets are created with the service key regardless):
export SUPADUPA_API_URL="https://api.<domain>"
export SUPADUPA_TOKEN="<control-plane bearer token>"
export SUPADUPA_REF="<project ref>"

node loadtest.mjs --minutes 10 --concurrency 8
```

Get the anon/service_role JWTs from `GET /v1/projects/<ref>/secrets/<kind>/reveal`
(kinds `anon_key`, `service_role`) or the project Connect page.

## Flags / env

| Flag | Env | Default | Meaning |
|------|-----|---------|---------|
| `--minutes N` | `DURATION_MIN` | `10` | How long the load phase runs (fractional ok, e.g. `0.5`). |
| `--concurrency N` | `CONCURRENCY` | `8` | Number of concurrent worker loops. |
| `--skip-setup` | | off | Reuse an already-seeded project; skip table/function/bucket creation. |

## What it creates

Idempotent setup: tables `lt_events, lt_items, lt_metrics, lt_notes, lt_profiles`
(RLS open to anon for the test), buckets `lt-public, lt-assets, lt-private`, and
edge functions `lt-echo, lt-compute`. Safe to re-run.

## Output

Progress every 15s (ops, errors, ops/sec, realtime events), then a final
per-category summary with p50/p95 latency. Exits non-zero if the error rate
exceeds 2%, so it doubles as a CI smoke/soak gate.
