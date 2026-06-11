import { expect, test, type Page, type Route } from "@playwright/test";

const adminUser = {
  id: "user-admin",
  email: "admin@example.com",
  role: "admin",
  mfa_enabled: false,
  created_at: "2026-06-08T00:00:00Z",
};

test("bootstraps the first admin and reaches the fleet dashboard", async ({ page }) => {
  let authenticated = false;
  await mockManagementAPI(page, () => authenticated, () => {
    authenticated = true;
  });

  await page.goto("/login");

  await expect(page.getByRole("heading", { name: "Create first admin" })).toBeVisible();
  await expect(page.getByText("No admin detected. Bootstrap is available below for first-run installs.")).toBeVisible();

  await page.getByPlaceholder("admin@example.com").fill(adminUser.email);
  await page.getByPlaceholder("Create password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create first admin" }).click();

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expect(page.getByText("SUPADUPA")).toBeVisible();
  await expect(page.getByRole("heading", { name: "At a glance" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Reservations" })).toBeVisible();
});

async function mockManagementAPI(page: Page, isAuthenticated: () => boolean, markAuthenticated: () => void) {
  await page.route("http://127.0.0.1:8080/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === "POST" && path === "/v1/auth/bootstrap") {
      markAuthenticated();
      await json(route, { user: adminUser });
      return;
    }
    switch (path) {
      case "/v1/health":
        await json(route, { status: "ok" });
        return;
      case "/v1/auth/state":
        await json(route, {
          auth_required: true,
          authenticated: isAuthenticated(),
          bootstrapped: isAuthenticated(),
          sso_enabled: false,
          sso_provider: "saml",
          ...(isAuthenticated() ? { user: adminUser } : {}),
        });
        return;
      case "/v1/metrics":
        await json(route, fleetMetrics());
        return;
      case "/v1/runtime-config":
        await json(route, runtimeConfig());
        return;
      case "/v1/advisor":
      case "/v1/hosts":
      case "/v1/orgs":
      case "/v1/projects":
        await json(route, []);
        return;
      case "/v1/compliance/report":
        await json(route, {
          generated_at: "2026-06-08T00:00:00Z",
          frameworks: [],
          summary: { passed: 0, action_needed: 0, manual_review: 0, total: 0 },
          controls: [],
          dpa_posture: "not_configured",
          certification: "self_managed",
        });
        return;
      case "/v1/provisioner":
        await json(route, { provisioner: "compose" });
        return;
      default:
        await json(route, {});
    }
  });
}

async function json(route: Route, body: unknown) {
  await route.fulfill({
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function fleetMetrics() {
  return {
    orgs: 0,
    users: 1,
    hosts: 0,
    projects: 0,
    read_replicas: 0,
    projects_by_status: {},
    host_capacity: { cpu: 0, ram_mb: 0, disk_gb: 0 },
    host_used: { cpu: 0, ram_mb: 0, disk_gb: 0 },
    observed: { projects_sampled: 0, active_projects: 0, latest_sampled_at: "" },
    routes: 0,
    custom_domains: 0,
    log_drains: 0,
    function_deployments: 0,
    function_regions: 0,
    function_storage_mounts: 0,
    replication_pipelines: 0,
    embedding_jobs: 0,
    database_extensions: 0,
    database_cron_jobs: 0,
    database_queues: 0,
    database_webhooks: 0,
    database_schemas: 0,
    auth_clients: 0,
    auth_hooks: 0,
    database_roles: 0,
    storage_buckets: 0,
    vector_buckets: 0,
    analytics_buckets: 0,
    cdn_enabled_projects: 0,
    cdn_invalidations: 0,
    network_connections: 0,
    backups: 0,
    backup_storage_bytes: 0,
    wal_archives: 0,
    wal_archive_bytes: 0,
    project_log_events: 0,
    audit_events: 0,
    audit_verified: true,
    sampled_at: "2026-06-08T00:00:00Z",
  };
}

function runtimeConfig() {
  return {
    provisioner: "compose",
    apply: {
      compose: false,
      kubernetes: false,
      storage_data_plane: false,
    },
    backup: {
      compose_defaults: true,
      logical_configured: false,
      physical_configured: false,
      wal_archive_configured: false,
      logical_restore_configured: false,
      pitr_restore_configured: false,
      backup_dry_run: true,
      restore_dry_run: true,
      wal_archive_dry_run: true,
    },
    recovery: {
      require_recovery_ready_targets: false,
    },
    upgrade: {
      require_durable_backup: false,
      failure_auto_restore: false,
    },
  };
}
