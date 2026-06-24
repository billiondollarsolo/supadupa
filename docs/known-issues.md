# Known Issues & Operational Notes

Supadupa is an early MVP (see "MVP Status" in the README). These are the rough
edges and operational gotchas worth knowing before you run it for real.

## Resource sizing is the #1 first-run surprise

Each project runs its **own** full Supabase stack. The `full` profile includes
**Logflare/analytics** (Elixir/BEAM), which alone wants ~1 GB and is **OOM-killed
on small hosts** — the project then reports `degraded` with
`analytics: expected service is missing from Docker Compose state`. That is a
host-memory limit, not a bug.

- Budget **~4 GB RAM per full-profile project** plus ~0.5 GB for the control
  plane. `setup-compose.sh` prints a warning when the host has < 4 GB.
- On a small host, disable the analytics service (or use a leaner profile).
- See [Resource Requirements](install.md#resource-requirements).

## Resource sizes are reservations unless limits are enforced

Tier/custom sizes (CPU/RAM/disk) are used for **placement and quota accounting**.
They are **not** enforced container limits unless you enable "Enforce limits".
When enabled, Supadupa distributes the selected CPU/RAM budget across enabled
service containers and writes per-container Docker Compose limits or Kubernetes
requests/limits. Docker Compose does not provide a true project-wide aggregate
cap, so an unenforced project can burst above its size on a host with free
capacity, and telemetry bars can read over 100%.

## Edge-router restarts have a brief self-heal window

The shared edge-router (Traefik) attaches to each project's isolated edge
network so it can route to that project. **Recreating only the edge-router**
(e.g. a Traefik upgrade or a static-config change) detaches it from every
project network and `502`s those projects until it re-attaches. The reconciler
re-attaches automatically within one cycle (~30 s); a normal full-stack
`up -d --build` restarts the control plane too, which also recovers it. Avoid
recreating *just* the edge-router on a busy host.

## Route manifests: don't delete the platform route

`runtime/routes/` holds Traefik's dynamic config:

- `00-platform.yaml` — the control-plane routes (`admin.` / `api.` hosts). **Do
  not delete this.** If it is lost, the API and admin UI return `404`; restore
  the file or re-run `setup-compose.sh` (which rewrites `.env`).
- `<ref>.yaml` — per-project routes, managed by the control plane.

When clearing rendered state, remove only the per-project files.

## Behavior under resource pressure

On an undersized or overloaded host, the provisioner's status check
(`docker compose ps`) can hiccup, and the reconciler may re-apply a project's
compose stack (recreating Kong → a brief `502` burst). Size the host for the
control plane **plus** every concurrent project's reservation, and avoid heavy
work (e.g. image builds) on the host while projects are under load.

## Not hosted-grade yet

Carried over from the README MVP status:

- Off-host PITR / durable recovery is not yet proven end-to-end.
- The Kubernetes provisioner is a renderer/operator contract, not the MVP
  runtime, and does not yet enforce namespace-per-project isolation.
- Platform SSO is a normalized-JSON adapter, not full SAML XML validation.

## Identifying the running build

`GET /v1/health` returns `{ "status", "version", "build" }`, and the admin
**About** page shows both. `build` is the git commit stamped into the
control-plane image at build time from `SUPADUPA_BUILD_SHA`. `setup-compose.sh`
records the current commit; to re-stamp after pulling an update, set it on the
build command:

```bash
git pull
SUPADUPA_BUILD_SHA="$(git rev-parse --short HEAD)" \
  docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```
