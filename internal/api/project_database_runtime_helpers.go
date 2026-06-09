package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"supadupa2026/internal/control"
	"supadupa2026/internal/env"
	"time"
)

func databaseRuntimeApplyEnabled() bool {
	if value := strings.TrimSpace(os.Getenv("SUPADUPA_DATABASE_APPLY")); value != "" {
		return env.BoolValue(value)
	}
	return env.BoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY"))
}

// projectSystemRLSSQL enables row-level security on platform-created internal
// tables that land in the API-exposed public schema (Realtime's Oban tables).
// Idempotent and guarded: it no-ops when a table isn't present yet. The owning
// service role bypasses RLS, so Realtime is unaffected; only the public API is
// denied access.
const projectSystemRLSSQL = `do $$
begin
  if to_regclass('public.oban_jobs')  is not null then execute 'alter table public.oban_jobs  enable row level security'; end if;
  if to_regclass('public.oban_peers') is not null then execute 'alter table public.oban_peers enable row level security'; end if;
end $$;`

// applyProjectSystemRLS enforces RLS on internal tables when the project's
// database config opts in (rls_enforce_system_tables). Best-effort.
func applyProjectSystemRLS(ctx context.Context, store control.Store, project control.Project) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	cfg, err := store.GetProjectConfig(ctx, project.Ref, "database")
	if err != nil {
		return err
	}
	if !env.BoolValue(cfg.Config["rls_enforce_system_tables"]) {
		return nil
	}
	return execProjectDatabaseSQL(ctx, project, projectSystemRLSSQL)
}

func execProjectDatabaseSQL(ctx context.Context, project control.Project, sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("database schema sql is required")
	}
	timeout := databaseApplyTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	projectDir := filepath.Join(env.OrDefault("SUPADUPA_PROJECT_ROOT", "./runtime/projects"), project.Ref)
	composeFile := filepath.Join(projectDir, "compose.yaml")
	commandParts := strings.Fields(env.OrDefault("SUPADUPA_COMPOSE_COMMAND", "docker compose"))
	if len(commandParts) == 0 {
		return fmt.Errorf("SUPADUPA_COMPOSE_COMMAND is empty")
	}
	args := append(commandParts[1:], "-p", project.Ref, "-f", composeFile, "exec", "-T", "db", "sh", "-c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql -v ON_ERROR_STOP=1 -U supabase_admin -d postgres`)
	cmd := exec.CommandContext(ctx, commandParts[0], args...)
	cmd.Dir = projectDir
	cmd.Stdin = strings.NewReader(sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("database SQL apply failed: %s", detail)
	}
	return nil
}

func quoteDatabaseIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteDatabaseLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func databaseApplyTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("SUPADUPA_DATABASE_APPLY_TIMEOUT"))
	if value == "" {
		return time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return time.Minute
}
