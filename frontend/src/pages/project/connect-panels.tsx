import { Activity, Boxes, Command, Copy, Database, ExternalLink, FileCode2, Globe2, KeyRound, Mail, RotateCcw, Server, Shield, SlidersHorizontal, Terminal, type LucideIcon } from "lucide-react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { RuntimeLink } from "../../components/runtime-link";
import { useUIStore } from "../../lib/ui-store";
import { formatBytes, formatTime } from "../../lib/format";
import { connectSections, type ConfigArea, type ConnectSection as ProjectConnectSection, type ProjectTab } from "../../lib/project-config";
import type { ConnectPayload, Project, ProjectCLIProfile, ProjectMetrics, ProjectRoute, ProjectRouteManifest, ProjectTCPRoute } from "../../types";

export function ConnectPanel({
  cliProfile,
  cliProfileLoading,
  project,
  payload,
  loading,
  onOpenProjectTab,
  onOpenConfigArea,
}: {
  cliProfile?: ProjectCLIProfile;
  cliProfileLoading: boolean;
  project?: Project;
  payload?: ConnectPayload;
  loading: boolean;
  onOpenProjectTab: (tab: ProjectTab) => void;
  onOpenConfigArea: (area: ConfigArea) => void;
}) {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/projects\/[^/]+\/connect\/([^/]+)/)?.[1];
  const activeSection: ProjectConnectSection = connectSections.some((section) => section.id === selectedSection) ? selectedSection as ProjectConnectSection : "overview";
  if (!project) {
    return (
      <section className="panel">
        <p className="text-sm text-muted">Create a project to view connection details.</p>
      </section>
    );
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Connect</p>
          <h2>{project.ref}</h2>
        </div>
        {payload?.studio_url ? <RuntimeLink className="button secondary h-8 min-h-8 justify-center" label="Studio" projectRef={project.ref} url={payload.studio_url} /> : null}
      </div>
      {loading || !payload ? (
        <p className="mt-4 text-sm text-muted">Loading connection payload...</p>
      ) : (
        <div className="mt-4 grid gap-3">
          <ConnectSectionBody
            activeSection={activeSection}
            cliProfile={cliProfile}
            cliProfileLoading={cliProfileLoading}
            payload={payload}
            onOpenConfigArea={onOpenConfigArea}
            onOpenProjectTab={onOpenProjectTab}
            onSelect={(section) => void navigate({ to: section === "overview" ? `/projects/${project.ref}/connect` : `/projects/${project.ref}/connect/${section}` })}
          />
        </div>
      )}
    </section>
  );
}

function ConnectSectionBody({
  activeSection,
  cliProfile,
  cliProfileLoading,
  onOpenConfigArea,
  onOpenProjectTab,
  onSelect,
  payload,
}: {
  activeSection: ProjectConnectSection;
  cliProfile?: ProjectCLIProfile;
  cliProfileLoading: boolean;
  payload: ConnectPayload;
  onOpenProjectTab: (tab: ProjectTab) => void;
  onOpenConfigArea: (area: ConfigArea) => void;
  onSelect: (section: ProjectConnectSection) => void;
}) {
  if (activeSection === "overview") {
    const endpointCount = Object.keys(endpointEntries(payload)).length;
    const customCount = payload.custom_api_urls?.length ?? 0;
    const cards: Array<{ section: ProjectConnectSection; label: string; value: string; detail: string }> = [
      { section: "endpoints", label: "Endpoints", value: `${endpointCount} URLs`, detail: customCount ? `${customCount} custom API URLs ready` : "API, REST, Auth, GraphQL, Realtime, Functions, Storage" },
      { section: "keys", label: "API keys", value: `${Object.keys(payload.api_keys).length} handles`, detail: "Publishable, secret, anon, service role" },
      { section: "jwt", label: "JWT", value: `${Object.keys(payload.jwt).length} items`, detail: `${payload.jwt_signing_keys?.length ?? 0} signing keys` },
      { section: "database", label: "Database", value: "Postgres", detail: "Direct URI, pooler, psql, parts" },
      { section: "storage", label: "Storage", value: "S3 compatible", detail: "Endpoint and credential handles" },
      { section: "links", label: "Links", value: "Studio + docs", detail: "Studio, REST docs, GraphQL explorer, logs" },
      { section: "cli", label: "CLI", value: "Compatibility", detail: "Supabase CLI and supadupa-cli profile" },
      { section: "snippets", label: "Snippets", value: `${Object.keys(payload.sdk_snippets).length} SDKs`, detail: "Connection and SDK initialization" },
      { section: "secrets", label: "Secrets", value: `${Object.keys(payload.secret_handles).length} handles`, detail: "Audited secret references" },
    ];
    return (
      <>
        <ConnectActions onOpenProjectTab={onOpenProjectTab} onOpenConfigArea={onOpenConfigArea} />
        <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
          {cards.map((card) => (
            <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" key={card.section} onClick={() => onSelect(card.section)} type="button">
              <p className="label">{card.label}</p>
              <p className="mt-2 truncate text-sm font-medium">{card.value}</p>
              <p className="mt-1 truncate text-xs text-muted">{card.detail}</p>
            </button>
          ))}
        </div>
      </>
    );
  }

  if (activeSection === "endpoints") {
    return <ConnectSection defaultOpen title="Endpoints" icon={Globe2} entries={endpointEntries(payload)} />;
  }
  if (activeSection === "keys") {
    return <ConnectSection defaultOpen title="API keys" icon={KeyRound} entries={payload.api_keys} />;
  }
  if (activeSection === "jwt") {
    return (
      <>
        <ConnectSection defaultOpen title="JWT" icon={Shield} entries={payload.jwt} />
        <JWTSigningKeysSection defaultOpen keys={payload.jwt_signing_keys ?? []} />
      </>
    );
  }
  if (activeSection === "database") {
    return (
      <>
        <ConnectSection defaultOpen title="Postgres URIs" icon={Database} entries={payload.postgres} />
        <ConnectSection defaultOpen title="Pooler" icon={Server} entries={payload.pooler} />
        {Object.entries(payload.postgres_parts).map(([mode, values]) => (
          <ConnectSection compact key={mode} title={`Postgres ${mode}`} icon={Database} entries={values} />
        ))}
      </>
    );
  }
  if (activeSection === "storage") {
    return <ConnectSection defaultOpen title="Storage" icon={Boxes} entries={payload.storage} />;
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
        <ConnectSection defaultOpen title="SDK snippets" icon={Copy} entries={payload.sdk_snippets} />
      </>
    );
  }
  return <ConnectSection defaultOpen title="Secret handles" icon={KeyRound} entries={payload.secret_handles} />;
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

function ConnectActions({
  onOpenProjectTab,
  onOpenConfigArea,
}: {
  onOpenProjectTab: (tab: ProjectTab) => void;
  onOpenConfigArea: (area: ConfigArea) => void;
}) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <div className="mb-2 flex items-center gap-2 text-faint">
        <SlidersHorizontal size={14} />
        <span className="label">Actions</span>
      </div>
      <div className="flex flex-wrap gap-2">
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenConfigArea("network")} type="button">
          <Globe2 size={14} />
          Domains / network
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("auth")} type="button">
          <Mail size={14} />
          Auth mail
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("functions")} type="button">
          <Command size={14} />
          Services / env
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("storage")} type="button">
          <Boxes size={14} />
          Storage / CDN
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("backups")} type="button">
          <RotateCcw size={14} />
          Backups
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("logs")} type="button">
          <Activity size={14} />
          Logs
        </button>
        <button className="button secondary h-8 min-h-8" onClick={() => onOpenProjectTab("activity")} type="button">
          <Activity size={14} />
          Activity
        </button>
      </div>
    </div>
  );
}

function JWTSigningKeysSection({ defaultOpen = false, keys }: { keys: ConnectPayload["jwt_signing_keys"]; defaultOpen?: boolean }) {
  const addToast = useUIStore((state) => state.addToast);
  async function copyPublicKey(publicKey: string, label: string) {
    await navigator.clipboard?.writeText(publicKey);
    addToast({ title: "Copied public key", detail: label });
  }

  return (
    <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
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
                <span className={`pill ${key.status === "current" ? "healthy" : key.status === "next" ? "provisioning" : ""}`}>{key.status}</span>
              </div>
              <p className="mt-1 truncate font-mono text-xs text-faint">{key.handle}</p>
              <p className="mt-1 truncate font-mono text-xs text-faint">{key.public_key.replace(/\n/g, " ")}</p>
            </div>
            <button className="icon-button" onClick={() => void copyPublicKey(key.public_key, key.kid || key.kind)} type="button">
              <Copy size={14} />
            </button>
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
        <span className="pill healthy">compat</span>
      </summary>
      <div className="mt-3 grid gap-2">
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
        <CopyRow compact label="env" value={env} />
        <CopyRow compact label="supabase/config.toml" value={profile.supabase_config_toml} />
        <CopyRow compact label="json" value={json} />
      </div>
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

function ConnectSection({ title, icon: Icon, entries, compact = false, defaultOpen = false }: { title: string; icon: LucideIcon; entries: Record<string, string>; compact?: boolean; defaultOpen?: boolean }) {
  return (
    <details className="rounded-md border border-border bg-bg p-3" open={defaultOpen}>
      <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
        <Icon size={14} />
        <span className="label">{title}</span>
        <span className="ml-auto text-xs text-faint">{Object.keys(entries).length} items</span>
      </summary>
      <div className="mt-3 grid gap-2">
        {Object.entries(entries).map(([key, value]) => (
          <CopyRow compact={compact} key={key} label={key} value={value} />
        ))}
      </div>
    </details>
  );
}

export function ProjectMetricsPanel({ metrics, loading }: { metrics?: ProjectMetrics; loading: boolean }) {
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Reports</p>
          <h2>Project metrics</h2>
        </div>
        {metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null}
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading project metrics...</p> : null}
        {metrics ? (
          <>
            <div className="metric-grid">
              <Metric label="Status" value={metrics.status} />
              <Metric label="Tier" value={metrics.resource_tier} />
              <Metric label="Reserved CPU" value={metrics.resources.cpu.toString()} />
              <Metric label="Reserved RAM" value={formatBytes(metrics.resources.ram_mb * 1024 * 1024)} />
              <Metric label="Reserved IOPS" value={metrics.resources.disk_iops.toString()} />
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
    </section>
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
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Routing</p>
          <h2>Route manifest</h2>
        </div>
      </div>
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
                <span className={`pill ${route.tls ? "healthy" : "provisioning"}`}>{route.tls ? "tls" : "plain"}</span>
                <span className={`pill ${route.ssl_enforced ? "healthy" : "provisioning"}`}>{route.ssl_enforced ? "ssl enforced" : "ssl optional"}</span>
                {route.cache_control ? <span className={`pill ${route.smart_cdn ? "healthy" : "provisioning"}`}>{route.smart_cdn ? "smart cdn" : "cdn"}</span> : null}
              </div>
              {route.ip_allowlist && route.ip_allowlist.length > 0 ? (
                <p className="mt-1 truncate font-mono text-xs text-muted">{route.ip_allowlist.join(", ")}</p>
              ) : null}
              {route.cache_control ? <p className="mt-1 truncate font-mono text-xs text-muted">{route.cache_control}</p> : null}
              <p className="mt-1 truncate font-mono text-xs text-faint">{route.upstream_url}</p>
              <div className="mt-2 flex justify-end gap-1">
                <a className="icon-button h-8 min-h-8 min-w-8" href={routeURL(route)} rel="noreferrer" target="_blank" title={`Open ${route.name}`}>
                  <ExternalLink size={14} />
                </a>
                <button className="icon-button h-8 min-h-8 min-w-8" onClick={() => void copyRouteURL(route)} title={`Copy ${route.name}`} type="button">
                  <Copy size={14} />
                </button>
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
                <span className="pill healthy">{route.protocol}</span>
                <span className={`pill ${route.tls ? "healthy" : "provisioning"}`}>{route.tls ? "tls" : "plain"}</span>
                <span className="pill">{route.entrypoint}</span>
              </div>
              <p className="mt-1 truncate font-mono text-xs text-faint">{route.upstream_address}</p>
              <div className="mt-2 flex justify-end gap-1">
                <button className="icon-button h-8 min-h-8 min-w-8" onClick={() => void copyTCPRoute(route)} title={`Copy ${route.name}`} type="button">
                  <Copy size={14} />
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
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
      {url ? (
        <a className="icon-button" href={url} rel="noreferrer" target="_blank" title={`Open ${label}`}>
          <ExternalLink size={14} />
        </a>
      ) : null}
      <button className="icon-button" onClick={() => void copy()} type="button">
        <Copy size={14} />
      </button>
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
