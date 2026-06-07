# Projects

A Supadupa project is an isolated Supabase-style runtime rendered and applied through Docker Compose. Each project gets its own project directory, Compose stack, network, secrets, route manifest, and generated public hosts.

## Create A Project In The UI

1. Open `Projects`.
2. Click the add project action.
3. Choose the organization.
4. Enter a project ref, such as `smoke`.
5. Enter a project name.
6. Choose stack version, profile, and resource tier.
7. Create the project.
8. Wait for runtime status to become healthy.

The project ref becomes part of generated hostnames. Use lowercase letters, numbers, and hyphens.

## Create A Project With The CLI

Login:

```bash
go run ./cmd/supadupa-cli --api https://api.example.com login \
  --email admin@example.com \
  --password 'change-this-password'
```

Create:

```bash
go run ./cmd/supadupa-cli --api https://api.example.com projects create \
  --org-id <org-id> \
  --ref smoke \
  --name "Smoke"
```

## Project Routes

For `smoke` on `apps.example.com`:

```text
https://smoke.apps.example.com             API
https://studio-smoke.apps.example.com      Studio
https://storage-smoke.apps.example.com     Storage S3
db-smoke.apps.example.com:5432             direct Postgres
pooler-smoke.apps.example.com:6543         pooler
```

The route manifest is written under:

```text
runtime/routes/smoke.yaml
```

The project Compose stack is written under:

```text
runtime/projects/smoke
```

## Connect Page

Use the project `Connect` page for canonical runtime details:

- Supabase API URL.
- Anon key.
- Service role key.
- Direct Postgres URL.
- Pooler URLs.
- CLI profile and helper commands.

Typical values:

```text
SUPABASE_URL=https://smoke.apps.example.com
SUPABASE_ANON_KEY=<anon key>
SUPABASE_SERVICE_ROLE_KEY=<service role key>
DATABASE_URL=postgres://postgres:<password>@db-smoke.apps.example.com:5432/postgres?sslmode=require
POOLER_TRANSACTION_URL=postgres://postgres.<ref>:<password>@pooler-smoke.apps.example.com:6543/postgres?sslmode=require
```

## Studio

Open Studio from the project page. Studio is exposed through the project route:

```text
https://studio-smoke.apps.example.com
```

Studio access is mediated through the Supadupa control-plane login. Studio and the project admin page can coexist:

- Supadupa project page: lifecycle, routes, backups, upgrade actions, operational status, logs, and access.
- Studio: database, auth, storage, SQL editor, table editor, and Supabase project-level tools.

## Supabase CLI

Official Supabase CLI DB workflows can run against the public database route. Use the project `Connect` page for the DB URL.

If `supabase gen types --db-url` hits the known public TLS/CA caveat, use Supadupa's tunnel/wrapper flow:

```bash
go run ./cmd/supadupa-cli projects db-tunnel --ref smoke
go run ./cmd/supadupa-cli projects gen-types --ref smoke
```

## Project Config

The project config pages manage runtime settings such as:

- Auth and OAuth provider settings.
- Email templates and SMTP-related project config.
- Database settings, extension toggles, pooler settings, and network allowlist.
- Storage settings.
- Edge Functions settings and secrets.
- Realtime settings.
- AI/vector-related settings where available.

Secret values should be stored as secret handles, not copied into visible config fields.

## Project Backups

Open `Projects -> <project> -> Backups` to:

- Run logical backups.
- View backup status and timestamps.
- Bind project backup policy to a target.
- Inspect recovery posture.

For production-like validation, use a tested off-host S3/R2/remote-MinIO target.

## Pause, Resume, And Destroy

Project lifecycle actions are available in the project UI and CLI. Destructive actions should be paired with a recent backup and a tested recovery target.

Rendered project state lives under `runtime/projects/<ref>`. Do not manually edit generated files unless debugging; normal changes should go through the control plane so route manifests, audit events, and persisted state stay aligned.
