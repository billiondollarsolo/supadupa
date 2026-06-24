import { Boxes, Copy, Database, ExternalLink, FileCode2, Globe2, KeyRound, Server, Shield, Terminal, type LucideIcon } from "lucide-react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { RuntimeLink } from "../../components/runtime-link";
import { AppPanel } from "../../components/app/app-panel";
import { Badge } from "../../components/ui/badge";
import { Button, buttonVariants } from "../../components/ui/button";
import { CardButton } from "../../components/ui/card-button";
import { RevealField } from "../../components/ui/reveal-field";
import { Segmented } from "../../components/ui/segmented";
import { StatusPill } from "../../components/ui/status-pill";
import { auditProjectSecretCopy, revealProjectSecret } from "../../api";
import { useUIStore } from "../../lib/ui-store";
import { formatBytes, formatTime } from "../../lib/format";
import { connectSections, type ConnectSection as ProjectConnectSection } from "../../lib/project-config";
import { projectPath, projectSectionFromPathname } from "../../lib/routes";
import type { ConnectPayload, Project, ProjectCLIProfile, ProjectMetrics, ProjectRoute, ProjectRouteManifest, ProjectTCPRoute } from "../../types";

// Sections whose values are credentials (keys, signing secrets, connection
// strings with embedded passwords). These render masked-by-default RevealFields
// and audit every copy; non-secret sections (URLs, links, snippets) stay plain.
function isSensitiveSection(section: ProjectConnectSection): boolean {
  return section === "keys" || section === "jwt" || section === "database" || section === "storage" || section === "secrets";
}

// Map a connect entry (section prefix + key) to the canonical managed secret
// kind the audit endpoint understands (POST /secrets/{kind}/copy calls
// RevealProjectSecret, which only accepts these). Returns null when the entry
// has no managed secret material behind it.
function canonicalSecretKind(prefix: string, key: string, value: string): string | null {
  const k = key.toLowerCase();
  switch (prefix) {
    case "api_key":
    case "secret":
      return { anon: "anon_key", service_role: "service_role", publishable: "publishable_key", secret: "secret_key" }[k] ?? (knownManagedSecretKinds.has(k) ? k : null);
    case "jwt":
      if (k.includes("current")) return "jwt_signing_key_current";
      if (k.includes("next")) return "jwt_signing_key_next";
      if (k.includes("secret")) return "jwt_secret";
      return null;
    case "postgres":
    case "pooler":
      // Connection URIs embed the database password placeholder. Pooler config
      // entries such as pool_mode and ports are not secret-bearing.
      return value.includes("${DB_PASSWORD}") ? "db_password" : null;
    case "storage":
      if (k.includes("access")) return "s3_access_key";
      if (k.includes("secret")) return "s3_secret_key";
      return null;
    default:
      return null;
  }
}

const knownManagedSecretKinds = new Set([
  "anon_key",
  "db_password",
  "jwt_secret",
  "jwt_signing_key_current",
  "jwt_signing_key_next",
  "publishable_key",
  "s3_access_key",
  "s3_secret_key",
  "secret_key",
  "service_role",
]);

function materializeConnectValue(value: string, kind: string, secretValue: string): string {
  if (kind === "db_password") {
    return value.split("${DB_PASSWORD}").join(encodeURIComponent(secretValue));
  }
  if (value.startsWith("secret://")) {
    return secretValue;
  }
  return secretValue;
}

function secretHint(value: string, kind: string | null): string | undefined {
  if (!kind) return undefined;
  if (value.startsWith("secret://")) return value;
  if (kind === "db_password" && value.includes("${DB_PASSWORD}")) return "Password is materialized on reveal/copy.";
  return undefined;
}

export function ConnectPanel({
  cliProfile,
  cliProfileLoading,
  project,
  payload,
  loading,
}: {
  cliProfile?: ProjectCLIProfile;
  cliProfileLoading: boolean;
  project?: Project;
  payload?: ConnectPayload;
  loading: boolean;
}) {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = projectSectionFromPathname(pathname, "connect");
  const activeSection: ProjectConnectSection = connectSections.some((section) => section.id === selectedSection) ? selectedSection as ProjectConnectSection : "overview";
  if (!project) {
    return (
      <AppPanel>
        <p className="text-sm text-muted">Create a project to view connection details.</p>
      </AppPanel>
    );
  }
  const selectSection = (section: ProjectConnectSection) =>
    void navigate({ to: section === "overview" ? projectPath(project.ref, "connect") : projectPath(project.ref, "connect", section) });

  return (
    <AppPanel
      eyebrow="Connect"
      title={project.name}
      actions={payload?.studio_url ? <RuntimeLink className={buttonVariants({ variant: "secondary", size: "sm" })} label="Studio" projectRef={project.ref} url={payload.studio_url} /> : undefined}
    >
      <p className="-mt-1 truncate font-mono text-xs text-faint">{project.ref}</p>
      <SectionNav activeSection={activeSection} onSelect={selectSection} />
      {loading || !payload ? (
        <p className="mt-4 text-sm text-muted">Loading connection payload...</p>
      ) : (
        <div className="mt-4 grid gap-3">
          <ConnectSectionBody
            activeSection={activeSection}
            cliProfile={cliProfile}
            cliProfileLoading={cliProfileLoading}
            payload={payload}
            onSelect={selectSection}
          />
        </div>
      )}
    </AppPanel>
  );
}

// In-page section navigation so users switch Connect subsections without leaving
// the content area for the global sidebar.
function SectionNav({ activeSection, onSelect }: { activeSection: ProjectConnectSection; onSelect: (section: ProjectConnectSection) => void }) {
  return (
    <Segmented
      className="mt-3"
      onChange={onSelect}
      options={connectSections.map((section) => ({ value: section.id, label: section.label }))}
      value={activeSection}
    />
  );
}

function ConnectSectionBody({
  activeSection,
  cliProfile,
  cliProfileLoading,
  onSelect,
  payload,
}: {
  activeSection: ProjectConnectSection;
  cliProfile?: ProjectCLIProfile;
  cliProfileLoading: boolean;
  payload: ConnectPayload;
  onSelect: (section: ProjectConnectSection) => void;
}) {
  if (activeSection === "overview") {
    const endpointCount = Object.keys(endpointEntries(payload)).length;
    const customCount = payload.custom_api_urls?.length ?? 0;
    // The section rail above already navigates between subsections, so the
    // overview no longer restates it as a card grid. Instead it shows a compact
    // real-count summary of the inventory available to connect with, each still
    // clickable through to the section that acts on it. Constant-string tiles
    // ("Postgres", "S3 compatible", "Studio + docs", "Compatibility") are gone.
    const stats: Array<{ section: ProjectConnectSection; label: string; value: string; detail: string }> = [
      { section: "endpoints", label: "Endpoints", value: `${endpointCount}`, detail: customCount ? `${customCount} custom API URLs` : "URLs" },
      { section: "keys", label: "API keys", value: `${Object.keys(payload.api_keys).length}`, detail: "handles" },
      { section: "jwt", label: "JWT signing keys", value: `${payload.jwt_signing_keys?.length ?? 0}`, detail: `${Object.keys(payload.jwt).length} JWT items` },
      { section: "snippets", label: "SDK snippets", value: `${Object.keys(payload.sdk_snippets).length}`, detail: "SDKs" },
      { section: "secrets", label: "Secret handles", value: `${Object.keys(payload.secret_handles).length}`, detail: "audited references" },
    ];
    return (
      <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
        {stats.map((stat) => (
          <CardButton key={stat.section} onClick={() => onSelect(stat.section)}>
            <p className="label">{stat.label}</p>
            <p className="mt-2 truncate text-base font-medium tabular-nums">{stat.value}</p>
            <p className="mt-1 truncate text-xs text-muted">{stat.detail}</p>
          </CardButton>
        ))}
      </div>
    );
  }

  const projectRef = cliProfile?.project_ref ?? "";
  if (activeSection === "endpoints") {
    return <ConnectSection defaultOpen title="Endpoints" icon={Globe2} entries={endpointEntries(payload)} />;
  }
  if (activeSection === "keys") {
    return <ConnectSection defaultOpen sensitive auditRef={projectRef} auditPrefix="api_key" title="API keys" icon={KeyRound} entries={payload.api_keys} />;
  }
  if (activeSection === "jwt") {
    return (
      <>
        <ConnectSection defaultOpen sensitive auditRef={projectRef} auditPrefix="jwt" title="JWT" icon={Shield} entries={payload.jwt} />
        <JWTSigningKeysSection keys={payload.jwt_signing_keys ?? []} />
      </>
    );
  }
  if (activeSection === "database") {
    return (
      <>
        <ConnectSection defaultOpen sensitive auditRef={projectRef} auditPrefix="postgres" title="Postgres URIs" icon={Database} entries={payload.postgres} />
        <ConnectSection sensitive auditRef={projectRef} auditPrefix="pooler" title="Pooler" icon={Server} entries={payload.pooler} />
        {Object.entries(payload.postgres_parts).map(([mode, values]) => (
          <ConnectSection compact key={mode} title={`Postgres ${mode}`} icon={Database} entries={values} />
        ))}
      </>
    );
  }
  if (activeSection === "storage") {
    return <ConnectSection defaultOpen sensitive auditRef={projectRef} auditPrefix="storage" title="Storage" icon={Boxes} entries={payload.storage} />;
  }
  if (activeSection === "links") {
    return <ConnectSection defaultOpen title="Links" icon={ExternalLink} entries={payload.links} />;
  }
  if (activeSection === "cli") {
    return <CLIProfileSection defaultOpen profile={cliProfile} loading={cliProfileLoading} />;
  }
  if (activeSection === "snippets") {
    return (
      <>
        <ConnectSection defaultOpen title="Connection snippets" icon={Copy} entries={payload.connection_snippets} />
        <ConnectSection title="SDK snippets" icon={Copy} entries={payload.sdk_snippets} />
      </>
    );
  }
  return <ConnectSection defaultOpen sensitive auditRef={projectRef} auditPrefix="secret" title="Secret handles" icon={KeyRound} entries={payload.secret_handles} />;
}

function endpointEntries(payload: ConnectPayload) {
  const entries = filterEntries({
    api: payload.api_url,
    rest: payload.rest_url,
    auth: payload.auth_url,
    graphql: payload.graphql_url,
    realtime: payload.realtime_url,
    functions: payload.functions_url,
    storage: payload.storage_url,
    storage_s3: payload.storage_s3_url,
  });
  (payload.custom_api_urls ?? []).forEach((url, index) => {
    entries[`custom_api_${index + 1}`] = url;
  });
  if (payload.local_api_url) {
    entries.api_local = payload.local_api_url;
    if (payload.services.rest) entries.rest_local = `${payload.local_api_url}/rest/v1`;
    if (payload.services.auth) entries.auth_local = `${payload.local_api_url}/auth/v1`;
    if (payload.services.graphql) entries.graphql_local = `${payload.local_api_url}/graphql/v1`;
    if (payload.services.realtime) entries.realtime_local = `${payload.local_api_url}/realtime/v1`;
    if (payload.services.functions) entries.functions_local = `${payload.local_api_url}/functions/v1`;
    if (payload.services.storage) entries.storage_local = `${payload.local_api_url}/storage/v1`;
  }
  return entries;
}

function filterEntries(entries: Record<string, string | undefined>) {
  return Object.fromEntries(Object.entries(entries).filter(([, value]) => Boolean(value))) as Record<string, string>;
}

function JWTSigningKeysSection({ keys }: { keys: ConnectPayload["jwt_signing_keys"] }) {
  const addToast = useUIStore((state) => state.addToast);
  async function copyPublicKey(publicKey: string, label: string) {
    await navigator.clipboard?.writeText(publicKey);
    addToast({ title: "Copied public key", detail: label });
  }

  return (
    <details className="rounded-md border border-border bg-bg p-3" open>
      <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
        <Shield size={14} />
        <p className="label">JWT signing keys</p>
        <span className="ml-auto text-xs text-faint">{keys.length} keys</span>
      </summary>
      <div className="mt-3 grid gap-2">
        {keys.length === 0 ? <p className="text-sm text-muted">No asymmetric signing keys generated.</p> : null}
        {keys.map((key) => (
          <div className="copy-row" key={key.kind}>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <p className="truncate font-mono text-xs text-muted">{key.kid || key.kind}</p>
                <StatusPill label={key.status} tone={key.status === "current" ? "success" : key.status === "next" ? "info" : "neutral"} />
              </div>
              <p className="mt-1 truncate font-mono text-xs text-faint">{key.handle}</p>
              <p className="mt-1 truncate font-mono text-xs text-faint">{key.public_key.replace(/\n/g, " ")}</p>
            </div>
            <Button onClick={() => void copyPublicKey(key.public_key, key.kid || key.kind)} size="icon" type="button" variant="ghost">
              <Copy size={14} />
            </Button>
          </div>
        ))}
      </div>
    </details>
  );
}

function CLIProfileSection({ defaultOpen = false, profile, loading }: { profile?: ProjectCLIProfile; loading: boolean; defaultOpen?: boolean }) {
  if (loading) {
    return (
      <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
        <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
          <Terminal size={14} />
          <p className="label">Supabase CLI profile</p>
        </summary>
        <p className="mt-3 text-sm text-muted">Loading CLI profile...</p>
      </details>
    );
  }

  if (!profile) {
    return (
      <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
        <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
          <Terminal size={14} />
          <p className="label">Supabase CLI profile</p>
        </summary>
        <p className="mt-3 text-sm text-muted">No CLI profile available.</p>
      </details>
    );
  }

  const json = JSON.stringify(profile, null, 2);
  const env = envExport(profile.env);
  const customAPIURLs = profile.custom_api_urls ?? [];

  return (
    <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-faint">
          <Terminal size={14} />
          <p className="label">Supabase CLI profile</p>
        </div>
        <StatusPill label="compat" tone="success" />
      </summary>
      <div className="mt-3 grid gap-2">
        <p className="label text-faint">Commands</p>
        <CopyRow compact label="supadupa-cli env export" value={`supadupa-cli projects cli-profile --ref ${profile.project_ref} --format env`} />
        <CopyRow compact label="supadupa-cli toml export" value={`supadupa-cli projects cli-profile --ref ${profile.project_ref} --format toml`} />
        {profile.commands.supadupa_env_reveal ? <CopyRow compact label="materialized env" value={profile.commands.supadupa_env_reveal} /> : null}
        {profile.commands.supadupa_link_reveal ? <CopyRow compact label="link with secrets" value={profile.commands.supadupa_link_reveal} /> : null}
        <CopyRow compact label="supabase db push" value={profile.commands.supabase_db_push ?? ""} />
        {profile.commands.supabase_db_push_env ? <CopyRow compact label="supabase db push with env" value={profile.commands.supabase_db_push_env} /> : null}
        <CopyRow compact label="supabase db pull" value={profile.commands.supabase_db_pull ?? ""} />
        {profile.commands.supabase_db_pull_env ? <CopyRow compact label="supabase db pull with env" value={profile.commands.supabase_db_pull_env} /> : null}
        <CopyRow compact label="supadupa gen types" value={profile.commands.supadupa_gen_types ?? ""} />
        {profile.commands.supadupa_db_tunnel ? <CopyRow compact label="db tunnel" value={profile.commands.supadupa_db_tunnel} /> : null}
        {profile.commands.supabase_local_env ? <CopyRow compact label="local Supabase env" value={profile.commands.supabase_local_env} /> : null}
        {customAPIURLs.map((url, index) => <CopyRow compact key={url} label={`custom API ${index + 1}`} value={url} />)}
      </div>
      <details className="mt-3 grid gap-2 border-t border-border pt-3">
        <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
          <FileCode2 size={14} />
          <span className="label">Full env / config.toml / JSON dump</span>
        </summary>
        <div className="mt-3 grid gap-2">
          <CopyRow compact label="env" value={env} />
          <CopyRow compact label="supabase/config.toml" value={profile.supabase_config_toml} />
          <CopyRow compact label="json" value={json} />
        </div>
      </details>
      <div className="mt-3 grid gap-2 border-t border-border pt-3">
        <div className="flex items-center gap-2 text-faint">
          <FileCode2 size={14} />
          <p className="label">Contract</p>
        </div>
        {Object.entries(profile.compatibility_contracts).map(([key, value]) => (
          <p className="text-xs text-muted" key={key}>
            <span className="font-mono text-faint">{key}</span> · {value}
          </p>
        ))}
      </div>
    </details>
  );
}

function ConnectSection({
  title,
  icon: Icon,
  entries,
  compact = false,
  defaultOpen = false,
  sensitive = false,
  auditRef = "",
  auditPrefix = "",
}: {
  title: string;
  icon: LucideIcon;
  entries: Record<string, string>;
  compact?: boolean;
  defaultOpen?: boolean;
  sensitive?: boolean;
  auditRef?: string;
  auditPrefix?: string;
}) {
  return (
    <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
      <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
        <Icon size={14} />
        <span className="label">{title}</span>
        <span className="ml-auto text-xs text-faint">{Object.keys(entries).length} items</span>
      </summary>
      <div className="mt-3 grid gap-2">
        {Object.entries(entries).map(([key, value]) => {
          const auditKind = auditRef ? canonicalSecretKind(auditPrefix, key, value) : null;
          return sensitive ? (
            <RevealField
              key={key}
              label={key}
              sensitive
              hint={secretHint(value, auditKind)}
              value={value}
              onReveal={auditKind ? async () => {
                const secret = await revealProjectSecret(auditRef, auditKind);
                return materializeConnectValue(value, auditKind, secret.value);
              } : undefined}
              onCopy={auditKind ? () => auditProjectSecretCopy(auditRef, auditKind) : undefined}
            />
          ) : (
            <CopyRow compact={compact} key={key} label={key} value={value} />
          );
        })}
      </div>
    </details>
  );
}

export function ProjectMetricsPanel({ metrics, loading }: { metrics?: ProjectMetrics; loading: boolean }) {
  return (
    <AppPanel
      actions={metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : undefined}
      eyebrow="Reports"
      title="Project metrics"
    >
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading project metrics...</p> : null}
        {metrics ? (
          <>
	            <div className="metric-grid">
	              <Metric label="Status" value={metrics.status} />
	              <Metric label="Reserved CPU" value={metrics.resources.cpu.toString()} />
	              <Metric label="Reserved RAM" value={formatBytes(metrics.resources.ram_mb * 1024 * 1024)} />
	              <Metric label="Reserved disk" value={`${metrics.resources.disk_gb} GB`} />
	            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Operational surface</p>
                <p className="truncate text-xs text-muted">{metrics.routes} routes · {metrics.custom_domains} domains · {metrics.replication_pipelines} pipelines · {metrics.embedding_jobs} embeddings</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{metrics.database_extensions} extensions · {metrics.database_cron_jobs} cron jobs · {metrics.database_queues} queues · {metrics.database_webhooks} webhooks · {metrics.database_schemas} schemas · {metrics.auth_clients} auth clients · {metrics.auth_hooks} auth hooks · {metrics.database_roles} database roles</p>
                <p>{metrics.storage_buckets} storage buckets · {metrics.vector_buckets} vector buckets · {metrics.analytics_buckets} analytics buckets · {metrics.log_drains} drains · {metrics.network_connections} networks</p>
                <p>{metrics.function_deployments} functions · {metrics.function_regions} regions · {metrics.function_storage_mounts} mounts · {metrics.read_replicas} replicas</p>
                <p>{metrics.cdn_enabled ? "CDN on" : "CDN off"} · {metrics.cdn_invalidations} invalidations</p>
                <p>{metrics.secrets} secrets · {metrics.activity_events} activity events</p>
              </div>
            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Retention and data-plane counters</p>
                <p className="truncate text-xs text-muted">{metrics.backups} backups · {metrics.wal_archives} WAL archives · {metrics.project_log_events} logs</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{formatBytes(metrics.storage_bytes)} retained</p>
                <p>{formatBytes(metrics.db_allocated_bytes)} DB alloc · {formatBytes(metrics.egress_bytes)} egress</p>
              </div>
            </div>
          </>
        ) : null}
      </div>
    </AppPanel>
  );
}

export function RoutesPanel({ manifest, loading }: { manifest?: ProjectRouteManifest; loading: boolean }) {
  const addToast = useUIStore((state) => state.addToast);
  const httpRoutes = manifest?.http_routes ?? [];
  const tcpRoutes = manifest?.tcp_routes ?? [];
  async function copyRouteURL(route: ProjectRoute) {
    await navigator.clipboard?.writeText(routeURL(route));
    addToast({ title: "Copied route URL", detail: route.name });
  }
  async function copyTCPRoute(route: ProjectTCPRoute) {
    await navigator.clipboard?.writeText(tcpRouteAddress(route));
    addToast({ title: "Copied TCP route", detail: route.name });
  }
  return (
    <AppPanel eyebrow="Routing" title="Route manifest">
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading routes...</p> : null}
        {!loading && httpRoutes.length === 0 && tcpRoutes.length === 0 ? <p className="text-sm text-muted">No routes registered yet.</p> : null}
        {httpRoutes.map((route) => (
          <div className="route-row" key={route.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{route.name}</p>
              <p className="truncate font-mono text-xs text-muted">{routeURL(route)}</p>
              {route.path_prefix ? <p className="truncate font-mono text-xs text-faint">path {route.path_prefix}</p> : null}
              {route.strip_prefix ? <p className="truncate font-mono text-xs text-faint">strip {route.strip_prefix}</p> : null}
            </div>
            <div className="min-w-0 text-right">
              <div className="flex justify-end gap-1">
                <StatusPill label={route.tls ? "tls" : "plain"} tone={route.tls ? "success" : "info"} />
                <StatusPill label={route.ssl_enforced ? "ssl enforced" : "ssl optional"} tone={route.ssl_enforced ? "success" : "info"} />
                {route.cache_control ? <StatusPill label={route.smart_cdn ? "smart cdn" : "cdn"} tone={route.smart_cdn ? "success" : "info"} /> : null}
              </div>
              {route.ip_allowlist && route.ip_allowlist.length > 0 ? (
                <p className="mt-1 truncate font-mono text-xs text-muted">{route.ip_allowlist.join(", ")}</p>
              ) : null}
              {route.cache_control ? <p className="mt-1 truncate font-mono text-xs text-muted">{route.cache_control}</p> : null}
              <p className="mt-1 truncate font-mono text-xs text-faint">{route.upstream_url}</p>
              <div className="mt-2 flex justify-end gap-1">
                <a className={buttonVariants({ variant: "ghost", size: "icon" })} href={routeURL(route)} rel="noreferrer" target="_blank" title={`Open ${route.name}`}>
                  <ExternalLink size={14} />
                </a>
                <Button onClick={() => void copyRouteURL(route)} size="icon" title={`Copy ${route.name}`} type="button" variant="ghost">
                  <Copy size={14} />
                </Button>
              </div>
            </div>
          </div>
        ))}
        {tcpRoutes.map((route) => (
          <div className="route-row" key={`tcp-${route.name}`}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{route.name}</p>
              <p className="truncate font-mono text-xs text-muted">{tcpRouteAddress(route)}</p>
              <p className="truncate font-mono text-xs text-faint">HostSNI {route.fqdn}</p>
            </div>
            <div className="min-w-0 text-right">
              <div className="flex justify-end gap-1">
                <StatusPill label={route.protocol} tone="success" />
                <StatusPill label={route.tls ? "tls" : "plain"} tone={route.tls ? "success" : "info"} />
                <Badge variant="muted">{route.entrypoint}</Badge>
              </div>
              <p className="mt-1 truncate font-mono text-xs text-faint">{route.upstream_address}</p>
              <div className="mt-2 flex justify-end gap-1">
                <Button onClick={() => void copyTCPRoute(route)} size="icon" title={`Copy ${route.name}`} type="button" variant="ghost">
                  <Copy size={14} />
                </Button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </AppPanel>
  );
}

function routeURL(route: ProjectRoute) {
  const scheme = route.tls ? "https" : "http";
  return `${scheme}://${route.fqdn}${route.path_prefix ?? ""}`;
}

function tcpRouteAddress(route: ProjectTCPRoute) {
  return `${route.fqdn}:${route.public_port}`;
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric-cell">
      <p className="label">{label}</p>
      <p className="truncate text-sm font-medium">{value}</p>
    </div>
  );
}

function envExport(values: Record<string, string>) {
  return Object.keys(values)
    .sort()
    .map((key) => `${key}=${shellEnvValue(values[key])}`)
    .join("\n");
}

function shellEnvValue(value: string) {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}

function CopyRow({ label, value, compact = false }: { label: string; value: string; compact?: boolean }) {
  const addToast = useUIStore((state) => state.addToast);
  const url = httpURL(value);
  async function copy() {
    await navigator.clipboard?.writeText(value);
    addToast({ title: "Copied", detail: label });
  }
  return (
    <div className="copy-row">
      <div className="min-w-0">
        <p className="label">{label}</p>
        <p className={compact ? "truncate font-mono text-xs text-muted" : "truncate font-mono text-sm"}>{value}</p>
      </div>
      <div className="copy-row-actions">
        {url ? (
          <a className={buttonVariants({ variant: "ghost", size: "icon" })} href={url} rel="noreferrer" target="_blank" title={`Open ${label}`}>
            <ExternalLink size={14} />
          </a>
        ) : null}
        <Button onClick={() => void copy()} size="icon" title={`Copy ${label}`} type="button" variant="ghost">
          <Copy size={14} />
        </Button>
      </div>
    </div>
  );
}

function httpURL(value: string) {
  const trimmed = value.trim();
  if (!trimmed.startsWith("http://") && !trimmed.startsWith("https://")) {
    return "";
  }
  try {
    return new URL(trimmed).toString();
  } catch {
    return "";
  }
}
