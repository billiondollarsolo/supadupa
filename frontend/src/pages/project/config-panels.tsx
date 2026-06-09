import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, ExternalLink, Globe2, Plus, RadioTower, Save, Search, SlidersHorizontal, Trash2, X } from "lucide-react";
import {
  addProjectDomain,
  createProjectNetworkConnection,
  deleteProjectDomain,
  deleteProjectNetworkConnection,
  destroyProject,
  getProjectConfig,
  resetProjectDomainCertificate,
  updateProjectConfig,
  updateProjectServices,
  uploadProjectDomainCertificate,
} from "../../api";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { Segmented } from "../../components/ui/segmented";
import { StatusPill } from "../../components/ui/status-pill";
import { Textarea } from "../../components/ui/textarea";
import {
  configAreaGuidedTab,
  configAreaLabels,
  configFieldGroups,
  configSchemas,
  projectServiceLabels,
  type ConfigArea,
  type ConfigField,
} from "../../lib/project-config";
import { formatDateTime, formatTime } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import { projectPath } from "../../lib/routes";
import { statusTone, type Tone } from "../../lib/status";
import type { Project, ProjectConfig, ProjectDomain, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectServices } from "../../types";

function ConfigDetailHeader({ detail, title, onBack }: { title: string; detail: string; onBack: () => void }) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <Button className="mb-3" onClick={onBack} size="sm" type="button" variant="secondary">
        <ArrowLeft size={14} />
        Back
      </Button>
      <p className="label">{title}</p>
      <p className="mt-1 text-sm text-muted">{detail}</p>
    </div>
  );
}

export function DomainsPanel({ project, domains, loading, enabled }: { project?: Project; domains: ProjectDomain[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/config\/domains\/([^/]+)/)?.[1];
  const selectedFqdn = selectedItem ? decodeURIComponent(selectedItem) : "";
  const selectedDomain = selectedFqdn && selectedFqdn !== "new" ? domains.find((domain) => domain.fqdn === selectedFqdn) : undefined;
  const basePath = project ? projectPath(project.ref, "config", "domains") : "";
  const [fqdn, setFqdn] = useState("");
  const [certificatePEM, setCertificatePEM] = useState("");
  const [privateKeyPEM, setPrivateKeyPEM] = useState("");
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["project-domains", ref] });
    void queryClient.invalidateQueries({ queryKey: ["connect", ref] });
    void queryClient.invalidateQueries({ queryKey: ["cli-profile", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const addMutation = useMutation({
    mutationFn: ({ ref, domain }: { ref: string; domain: string }) => addProjectDomain(ref, domain),
    onSuccess: (_, variables) => {
      setFqdn("");
      invalidate(variables.ref);
      void navigate({ to: projectPath(variables.ref, "config", "domains") });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, domain }: { ref: string; domain: string }) => deleteProjectDomain(ref, domain),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const uploadCertMutation = useMutation({
    mutationFn: ({ ref, domain, cert, key }: { ref: string; domain: string; cert: string; key: string }) => uploadProjectDomainCertificate(ref, domain, cert, key),
    onSuccess: (_, variables) => {
      setCertificatePEM("");
      setPrivateKeyPEM("");
      invalidate(variables.ref);
    },
  });
  const resetCertMutation = useMutation({
    mutationFn: ({ ref, domain }: { ref: string; domain: string }) => resetProjectDomainCertificate(ref, domain),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const domainColumns = useMemo<ColumnDef<ProjectDomain>[]>(() => [
    {
      accessorKey: "fqdn",
      header: "Domain",
      cell: ({ row }) => (
        <button className="font-mono text-sm text-primary" onClick={() => basePath && void navigate({ to: `${basePath}/${encodeURIComponent(row.original.fqdn)}` })} type="button">
          {row.original.fqdn}
        </button>
      ),
    },
    {
      accessorKey: "cert_status",
      header: "Certificate",
      cell: ({ row }) => <StatusPill status={row.original.cert_status} />,
    },
    {
      accessorKey: "cert_mode",
      header: "Mode",
      cell: ({ row }) => row.original.cert_mode || "acme",
    },
    {
      accessorKey: "cert_not_after",
      header: "Expires",
      cell: ({ row }) => row.original.cert_not_after ? formatDateTime(row.original.cert_not_after) : "Automatic",
    },
    {
      accessorKey: "updated_at",
      header: "Updated",
      cell: ({ row }) => formatDateTime(row.original.updated_at),
    },
  ], [basePath, navigate]);

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || fqdn.trim().length === 0) {
      return;
    }
    addMutation.mutate({ ref: project.ref, domain: fqdn });
  }

  return (
    <AppPanel actions={<Globe2 size={15} className="text-faint" />} eyebrow="Domains" title="Custom ingress">
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Custom domains disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the custom_domains feature flag for this org before adding ingress domains.</p>
            </div>
          ) : null}
          <Button className="w-fit" disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
            <Plus size={14} />
            Add domain
          </Button>
          {loading ? (
            <p className="text-sm text-muted">Loading domains...</p>
          ) : domains.length === 0 ? (
            <EmptyState
              icon={Globe2}
              title="No custom domains"
              description="Attach a custom ingress domain to serve this project's API and Studio over your own hostname."
              action={
                <Button disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
                  <Plus size={14} />
                  Add domain
                </Button>
              }
            />
          ) : (
            <DataTable columns={domainColumns} data={domains} emptyText="No custom domains configured." minWidth={760} />
          )}
        </div>
      ) : null}
      {selectedItem === "new" ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <ConfigDetailHeader detail="Attach one custom ingress domain to this project." title="New domain" onBack={() => basePath && void navigate({ to: basePath })} />
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Custom domains disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the custom_domains feature flag before submitting this domain.</p>
            </div>
          ) : null}
          <div className="flex gap-2 max-sm:flex-col">
            <Input className="font-mono" disabled={!enabled} placeholder="api.example.com" value={fqdn} onChange={(event) => setFqdn(event.target.value)} />
            <Button disabled={!enabled || !project || addMutation.isPending || fqdn.trim().length === 0} type="submit" variant="secondary">
              <Plus size={14} />
              Add
            </Button>
          </div>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <ConfigDetailHeader detail={selectedDomain ? "Custom ingress domain and certificate state." : "Domain not found in the current project."} title={selectedFqdn} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedDomain ? (
            <div className="grid gap-2">
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Certificate</p><div className="mt-1"><StatusPill status={selectedDomain.cert_status} /></div></div>
                <div className="metric-cell"><p className="label">Mode</p><p className="text-sm font-medium">{selectedDomain.cert_mode || "acme"}</p></div>
                <div className="metric-cell"><p className="label">FQDN</p><p className="truncate font-mono text-sm font-medium">{selectedDomain.fqdn}</p></div>
                <div className="metric-cell"><p className="label">Expires</p><p className="text-sm font-medium">{selectedDomain.cert_not_after ? formatDateTime(selectedDomain.cert_not_after) : "Automatic"}</p></div>
              </div>
              {selectedDomain.cert_fingerprint ? (
                <div className="rounded-md border border-border bg-bg p-3">
                  <p className="label">Fingerprint</p>
                  <p className="mt-1 break-all font-mono text-xs text-muted">{selectedDomain.cert_fingerprint}</p>
                </div>
              ) : null}
              <form className="grid gap-2 rounded-md border border-border bg-bg p-3" onSubmit={(event) => {
                event.preventDefault();
                if (project && certificatePEM.trim() && privateKeyPEM.trim()) {
                  uploadCertMutation.mutate({ ref: project.ref, domain: selectedDomain.fqdn, cert: certificatePEM, key: privateKeyPEM });
                }
              }}>
                <p className="label">Bring your own certificate</p>
                <Textarea className="min-h-28 font-mono text-xs" placeholder="-----BEGIN CERTIFICATE-----" value={certificatePEM} onChange={(event) => setCertificatePEM(event.target.value)} />
                <Textarea className="min-h-28 font-mono text-xs" placeholder="-----BEGIN PRIVATE KEY-----" value={privateKeyPEM} onChange={(event) => setPrivateKeyPEM(event.target.value)} />
                <div className="flex gap-2 max-sm:flex-col">
                  <Button disabled={!project || uploadCertMutation.isPending || !certificatePEM.trim() || !privateKeyPEM.trim()} type="submit" variant="secondary">
                    <Save size={14} />
                    Upload certificate
                  </Button>
                  <Button disabled={!project || resetCertMutation.isPending || selectedDomain.cert_mode !== "byo"} onClick={() => project && resetCertMutation.mutate({ ref: project.ref, domain: selectedDomain.fqdn })} type="button" variant="secondary">
                    Reset to ACME
                  </Button>
                </div>
                {uploadCertMutation.error ? <p className="text-sm text-danger">{String(uploadCertMutation.error)}</p> : null}
                {resetCertMutation.error ? <p className="text-sm text-danger">{String(resetCertMutation.error)}</p> : null}
              </form>
              <Button className="w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, domain: selectedDomain.fqdn })} type="button" variant="danger">
                <X size={14} />
                Remove domain
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-3 grid gap-2">
        {addMutation.error ? <p className="text-sm text-danger">{addMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function NetworkConnectionsPanel({ project, policy, connections, loading, enabled }: { project?: Project; policy?: ProjectNetworkPolicy; connections: ProjectNetworkConnection[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/config\/network\/([^/]+)/)?.[1];
  const selectedConnection = selectedItem && selectedItem !== "new" ? connections.find((connection) => connection.id === selectedItem) : undefined;
  const basePath = project ? projectPath(project.ref, "config", "network") : "";
  const allowlistEntries = parseLines(policy?.allowlist ?? "");
  const sslEnforced = (policy?.ssl_enforced ?? "true") !== "false";
  const [form, setForm] = useState({
    name: "aws-prod",
    type: "privatelink",
    provider: "aws",
    region: "us-east-1",
    cidrs: "10.0.0.0/16",
    endpoint_id: "vpce-",
    config: "account_id=123456789012\ntoken=secret://projects/example/private-link-token",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["network-policy", ref] });
    void queryClient.invalidateQueries({ queryKey: ["network-connections", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectNetworkConnection(ref, {
      name: form.name,
      type: form.type,
      provider: form.provider,
      region: form.region,
      cidrs: parseLines(form.cidrs),
      endpoint_id: form.endpoint_id,
      config: parseKeyValueLines(form.config),
    }),
    onSuccess: (_, variables) => {
      invalidate(variables.ref);
      void navigate({ to: projectPath(variables.ref, "config", "network") });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectNetworkConnection(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  const connectionColumns = useMemo<ColumnDef<ProjectNetworkConnection>[]>(() => [
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <button className="font-medium text-primary" onClick={() => basePath && void navigate({ to: `${basePath}/${row.original.id}` })} type="button">
          {row.original.name}
        </button>
      ),
    },
    {
      accessorKey: "type",
      header: "Type",
      cell: ({ row }) => `${row.original.type} · ${row.original.provider}${row.original.region ? ` · ${row.original.region}` : ""}`,
    },
    {
      accessorKey: "cidrs",
      header: "CIDRs",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.cidrs.join(", ") || "none"}</span>,
    },
    {
      accessorKey: "endpoint_id",
      header: "Endpoint",
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.endpoint_id || "pending"}</span>,
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: ({ row }) => <StatusPill status={row.original.status} />,
    },
  ], [basePath, navigate]);

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || parseLines(form.cidrs).length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  const allowlistPath = project ? projectPath(project.ref, "config", "runtime") : "";

  return (
    <AppPanel actions={<RadioTower size={15} className="text-faint" />} eyebrow="Network" title="Network access & private connectivity">
      <div className="mt-4 grid grid-cols-3 gap-2 max-md:grid-cols-1">
        <MetricCard
          label="Ingress allowlist"
          value={allowlistEntries.length > 0 ? `${allowlistEntries.length} CIDR${allowlistEntries.length === 1 ? "" : "s"}` : "Open"}
          detail={allowlistEntries.join(", ") || "0.0.0.0/0 equivalent"}
        />
        <MetricCard label="Route TLS" value={sslEnforced ? "Enforced" : "Optional"} detail={sslEnforced ? "Routes require secure ingress" : "Non-strict ingress allowed"} />
        <MetricCard label="Connections" value={connections.length} detail={connections.length === 1 ? "Private declaration" : "Private declarations"} />
      </div>
      {allowlistPath ? (
        <button className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline" onClick={() => void navigate({ to: allowlistPath })} type="button">
          Edit ingress allowlist in Runtime Config
          <ExternalLink size={11} />
        </button>
      ) : null}
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Network restrictions disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the network_restrictions feature flag for this org before requesting private connectivity.</p>
            </div>
          ) : null}
          <Button className="w-fit" disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
            <Plus size={14} />
            Request network
          </Button>
          {loading ? (
            <p className="text-sm text-muted">Loading private network connections...</p>
          ) : connections.length === 0 ? (
            <EmptyState
              icon={RadioTower}
              title="No private network connections"
              description="Declare a PrivateLink, VPC peering, or operator network binding to reach this project privately."
              action={
                <Button disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
                  <Plus size={14} />
                  Request network
                </Button>
              }
            />
          ) : (
            <DataTable columns={connectionColumns} data={connections} emptyText="No private network connections requested." minWidth={760} />
          )}
        </div>
      ) : null}
      {selectedItem === "new" ? (
        <form className="mt-4 grid gap-2" onSubmit={submit}>
          <ConfigDetailHeader detail="Declare one private connectivity request or operator network binding." title="New private network" onBack={() => basePath && void navigate({ to: basePath })} />
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Network restrictions disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the network_restrictions feature flag before submitting this request.</p>
            </div>
          ) : null}
          <div className="grid grid-cols-[minmax(0,1fr)_150px_110px_130px] gap-2 max-sm:grid-cols-1">
            <Input className="font-mono" disabled={!enabled} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            <NativeSelect disabled={!enabled} value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value })}>
              <option value="privatelink">PrivateLink</option>
              <option value="vpc_peering">VPC peering</option>
              <option value="private_endpoint">Private endpoint</option>
              <option value="wireguard">WireGuard</option>
              <option value="operator_network">Operator network</option>
            </NativeSelect>
            <NativeSelect disabled={!enabled} value={form.provider} onChange={(event) => setForm({ ...form, provider: event.target.value })}>
              <option value="aws">AWS</option>
              <option value="gcp">GCP</option>
              <option value="azure">Azure</option>
              <option value="custom">Custom</option>
              <option value="operator">Operator</option>
            </NativeSelect>
            <Input className="font-mono" disabled={!enabled} value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} />
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <Input className="font-mono" disabled={!enabled} value={form.endpoint_id} onChange={(event) => setForm({ ...form, endpoint_id: event.target.value })} />
            <Textarea className="min-h-[52px] font-mono" disabled={!enabled} value={form.cidrs} onChange={(event) => setForm({ ...form, cidrs: event.target.value })} />
          </div>
          <Textarea className="min-h-[64px] font-mono" disabled={!enabled} value={form.config} onChange={(event) => setForm({ ...form, config: event.target.value })} />
          <Button disabled={!enabled || !project || createMutation.isPending || parseLines(form.cidrs).length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Request network
          </Button>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <ConfigDetailHeader detail={selectedConnection ? `${selectedConnection.type} via ${selectedConnection.provider}` : "Network connection not found in the current project."} title={selectedConnection?.name ?? selectedItem} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedConnection ? (
            <div className="grid gap-2">
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Status</p><div className="mt-1"><StatusPill status={selectedConnection.status} /></div></div>
                <div className="metric-cell"><p className="label">Region</p><p className="text-sm font-medium">{selectedConnection.region || "operator"}</p></div>
                <div className="metric-cell"><p className="label">Endpoint</p><p className="truncate font-mono text-sm font-medium">{selectedConnection.endpoint_id || "pending"}</p></div>
              </div>
              <div className="metric-cell">
                <p className="label">CIDRs</p>
                <p className="mt-1 font-mono text-sm text-muted">{selectedConnection.cidrs.join(", ") || "none"}</p>
              </div>
              <Button className="w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, id: selectedConnection.id })} type="button" variant="danger">
                <X size={14} />
                Delete network
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-4 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function ServicesPanel({ project, services, loading }: { project?: Project; services?: ProjectServices; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, boolean>>({});
  const servicesKey = `${project?.ref ?? ""}:${services?.updated_at ?? ""}`;
  useEffect(() => {
    if (!services) {
      setDraft({});
      return;
    }
    setDraft(services.services);
  }, [servicesKey, services]);
  const enabledCount = projectServiceLabels.filter((service) => draft[service.key] ?? true).length;
  const on = (key: string) => draft[key] ?? true;
  // Hard dependency: Imgproxy only transforms Storage objects, so turning
  // Storage off also turns Imgproxy off. Soft advisories mirror the create wizard.
  function setService(key: string, value: boolean) {
    const next = { ...draft, [key]: value };
    if (key === "storage" && !value) next.imgproxy = false;
    setDraft(next);
  }
  const serviceWarnings: string[] = [];
  if (!on("studio")) serviceWarnings.push("Studio is off — this project has no dashboard; manage it via the API, CLI, or SQL.");
  if (on("analytics") !== on("vector")) serviceWarnings.push("Analytics (Logflare) and Vector are the logging pipeline — enable both or neither, or project logs won't be collected.");
  if (!on("rest") && on("storage")) serviceWarnings.push("Storage works best with the REST API enabled; some Storage operations route through PostgREST.");
  const mutation = useMutation({
    mutationFn: ({ ref, next }: { ref: string; next: Record<string, boolean> }) => updateProjectServices(ref, next),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-services", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  return (
    <AppPanel actions={<StatusPill tone="success" label={`${enabledCount}/${projectServiceLabels.length}`} />} eyebrow="Services" title="Enabled stack services">
      <div className="mt-4 grid grid-cols-2 gap-2 max-sm:grid-cols-1">
        {loading ? <p className="text-sm text-muted">Loading services...</p> : null}
        {projectServiceLabels.map((service) => {
          const blocked = service.key === "imgproxy" && !on("storage");
          const checked = on(service.key) && !blocked;
          return (
            <label className={`config-toggle ${blocked ? "opacity-50" : ""}`} key={service.key}>
              <span>
                <span className="block text-sm font-medium">{service.label}</span>
                <span className="block font-mono text-xs text-faint">{service.key}{blocked ? " · requires storage" : ""}</span>
              </span>
              <input checked={checked} disabled={blocked} onChange={(event) => setService(service.key, event.target.checked)} type="checkbox" />
            </label>
          );
        })}
      </div>
      {serviceWarnings.length ? (
        <ul className="mt-3 grid gap-1">
          {serviceWarnings.map((warning) => (
            <li className="text-xs leading-5 text-warning" key={warning}>⚠ {warning}</li>
          ))}
        </ul>
      ) : null}
      <div className="mt-4 flex items-center justify-between gap-3">
        <p className="truncate text-xs text-muted">{services ? `Last changed ${formatTime(services.updated_at)}` : "Desired service state is loaded from the project spec."}</p>
        <Button disabled={!project || loading || mutation.isPending} onClick={() => project && mutation.mutate({ ref: project.ref, next: draft })} type="button" variant="secondary">
          <Save size={14} />
          Save
        </Button>
      </div>
      {mutation.error ? <p className="mt-3 text-sm text-danger">{mutation.error.message}</p> : null}
    </AppPanel>
  );
}

const DB_INGRESS_MODES = [
  { id: "private", label: "Private", help: "Not reachable through the edge router. Only this project's own services connect to its database." },
  { id: "allowlisted", label: "Allowlisted", help: "Reachable only from the IP ranges listed below." },
  { id: "public", label: "Public", help: "Reachable from any IP address. Use only with strong, rotated database credentials." },
] as const;
type DBIngressMode = (typeof DB_INGRESS_MODES)[number]["id"];

function normalizeMode(value: string | undefined): DBIngressMode {
  const v = (value ?? "").toLowerCase();
  return v === "private" || v === "allowlisted" || v === "public" ? v : "private";
}

// Per-project database exposure. Each project's mode + allowlist are stored in
// its own `network` config and reconciled in isolation — one project's setting
// never affects another's. Saving re-renders just this project's edge routes.
export function DatabaseExposurePanel({ project, hostPublished, masterEnabled }: { project?: Project; hostPublished?: boolean; masterEnabled?: boolean }) {
  const ref = project?.ref ?? "";
  const queryClient = useQueryClient();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const config = useQuery({
    queryKey: ["project-config", ref, "network"],
    queryFn: () => getProjectConfig(ref, "network"),
    enabled: ref.length > 0,
  });
  const [mode, setMode] = useState<DBIngressMode>("private");
  const [allowlist, setAllowlist] = useState("");
  const [httpAllowlist, setHttpAllowlist] = useState("");
  const storedMode = normalizeMode(config.data?.config?.db_ingress_mode);
  const configKey = `${ref}:${config.data?.updated_at ?? ""}`;
  useEffect(() => {
    const stored = config.data?.config ?? {};
    setMode(normalizeMode(stored.db_ingress_mode));
    setAllowlist(parseLines(stored.db_allowlist ?? "").join("\n"));
    setHttpAllowlist(parseLines(stored.http_allowlist ?? "").join("\n"));
  }, [configKey, config.data]);

  const cidrs = parseLines(allowlist);
  const httpCidrs = parseLines(httpAllowlist);
  const mutation = useMutation({
    mutationFn: () => updateProjectConfig(ref, "network", { db_ingress_mode: mode, db_allowlist: cidrs.join("\n"), http_allowlist: httpCidrs.join("\n") }),
    onSuccess: () => {
      setConfirmOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["project-config", ref, "network"] });
      void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", ref] });
      void queryClient.invalidateQueries({ queryKey: ["network-policy", ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  const allowlistedWithoutCIDRs = mode === "allowlisted" && cidrs.length === 0;
  const effectiveTone: Tone = mode === "private" ? "neutral" : mode === "public" ? "danger" : "success";
  const effectiveLabel = mode === "private" ? "Private" : mode === "public" ? "Public / open" : `Allowlisted · ${cidrs.length} ${cidrs.length === 1 ? "range" : "ranges"}`;

  return (
    <AppPanel
      actions={<StatusPill tone={effectiveTone} label={effectiveLabel} />}
      eyebrow="Network"
      title="Database exposure"
      description={`How this project's Postgres and pooler are reachable from outside the platform. Applies only to ${project?.ref ?? "this project"}.`}
    >
      <div className="mt-4 grid gap-3">
        <div className="grid grid-cols-3 gap-2 max-sm:grid-cols-1">
          {DB_INGRESS_MODES.map((option) => (
            <button
              className={mode === option.id ? "segmented active h-auto flex-col items-start gap-1 p-3 text-left" : "segmented h-auto flex-col items-start gap-1 p-3 text-left"}
              key={option.id}
              onClick={() => setMode(option.id)}
              type="button"
            >
              <span className="text-sm font-medium">{option.label}</span>
              <span className="text-xs text-muted">{option.help}</span>
            </button>
          ))}
        </div>
        {mode === "allowlisted" ? (
          <Field label="Trusted client CIDRs" hint="One CIDR or IP per line. Only these ranges may reach this project's database.">
            <Textarea
              className="min-h-28 font-mono text-xs"
              placeholder={"203.0.113.10/32\n198.51.100.0/24"}
              value={allowlist}
              onChange={(event) => setAllowlist(event.target.value)}
            />
          </Field>
        ) : null}
        {mode === "public" ? (
          <div className="rounded-md border border-danger/40 px-3 py-2 text-xs text-danger">
            Any host on the internet can attempt to connect. Ensure credentials are strong and rotated.
          </div>
        ) : null}
        <div className="border-t border-border pt-3">
          <Field
            label="HTTP / Studio access allowlist"
            hint="Independent from the database allowlist above. Empty = open to all. Restricts which IPs can reach this project's API, Studio, and Storage over HTTPS — it never affects the database ports."
          >
            <Textarea
              className="min-h-20 font-mono text-xs"
              placeholder={"Empty = open to all\n203.0.113.10/32"}
              value={httpAllowlist}
              onChange={(event) => setHttpAllowlist(event.target.value)}
            />
          </Field>
        </div>
        <div className="flex items-center justify-between gap-2 rounded-md border border-border bg-bg px-3 py-2">
          <div className="min-w-0">
            <p className="truncate text-xs font-medium">Host database port</p>
            <p className="truncate text-xs text-muted">
              {hostPublished === undefined
                ? "Checking whether the platform publishes the database port…"
                : hostPublished
                  ? "Published — exposure changes here take effect for external clients."
                  : "Loopback only — set SUPADUPA_POSTGRES_ADDR / SUPADUPA_POOLER_ADDR to a public bind to allow external access."}
            </p>
          </div>
          <StatusPill tone={hostPublished === undefined ? "info" : hostPublished ? "success" : "neutral"} label={hostPublished === undefined ? "checking" : hostPublished ? "published" : "loopback"} />
        </div>
        {hostPublished === false && mode !== "private" ? (
          <div className="rounded-md border border-warning/40 px-3 py-2 text-xs text-warning">
            This project is set to {mode}, but the platform isn't publishing the database port yet, so external clients can't reach it. Routing and the allowlist are saved and will apply the moment the platform port is published.
          </div>
        ) : null}
        {masterEnabled === false && mode !== "private" ? (
          <div className="rounded-md border border-warning/40 px-3 py-2 text-xs text-warning">
            Platform external database access is currently <span className="font-medium">off</span>, so this project stays private fleet-wide until an admin enables it in Settings → Database ingress.
          </div>
        ) : null}
        <div className="flex items-center justify-between gap-3">
          <p className="text-xs text-muted">{config.isLoading ? "Loading…" : "Saving re-renders this project's edge routes immediately."}</p>
          <Button disabled={mutation.isPending || allowlistedWithoutCIDRs || config.isLoading} onClick={() => setConfirmOpen(true)} type="button" variant={mode === "public" ? "danger" : "default"}>
            {mutation.isPending ? "Saving…" : "Save exposure"}
          </Button>
        </div>
        {allowlistedWithoutCIDRs ? <p className="text-xs text-warning">Add at least one CIDR, or switch to Private.</p> : null}
        {mutation.error ? <p className="text-sm text-danger">{(mutation.error as Error).message}</p> : null}
      </div>
      <Modal
        description={exposureConfirmCopy(mode, cidrs.length).description}
        footer={
          <>
            <Button disabled={mutation.isPending} onClick={() => setConfirmOpen(false)} type="button" variant="secondary">Cancel</Button>
            <Button disabled={mutation.isPending} onClick={() => mutation.mutate()} type="button" variant={mode === "public" ? "danger" : "default"}>
              {mutation.isPending ? "Working…" : exposureConfirmCopy(mode, cidrs.length).confirmLabel}
            </Button>
          </>
        }
        onClose={() => !mutation.isPending && setConfirmOpen(false)}
        open={confirmOpen}
        title={exposureConfirmCopy(mode, cidrs.length).title}
      >
        <div className="grid gap-2 text-sm text-muted">
          <p>{exposureConfirmCopy(mode, cidrs.length).body.replace("{ref}", project?.ref ?? "this project")}</p>
          {storedMode !== mode ? <p className="text-xs text-faint">Changing exposure from <span className="font-mono">{storedMode}</span> to <span className="font-mono">{mode}</span>.</p> : null}
          {mode !== "private" && hostPublished === false ? <p className="text-xs text-warning">The platform isn't publishing the database port yet, so this won't be reachable externally until that's set.</p> : null}
          {mode !== "private" && masterEnabled === false ? <p className="text-xs text-warning">Platform external database access is off, so this stays private until an admin enables it.</p> : null}
        </div>
      </Modal>
    </AppPanel>
  );
}

function exposureConfirmCopy(mode: DBIngressMode, cidrCount: number): { title: string; description: string; body: string; confirmLabel: string } {
  switch (mode) {
    case "public":
      return {
        title: "Expose database to the internet?",
        description: "This makes the project database reachable from any IP address.",
        body: "Any host on the internet will be able to attempt connections to {ref}'s Postgres and pooler. Only proceed with strong, rotated credentials.",
        confirmLabel: "Make public",
      };
    case "allowlisted":
      return {
        title: "Limit database access by IP?",
        description: "Only the listed IP ranges will be able to reach this database.",
        body: `Only the ${cidrCount} listed IP ${cidrCount === 1 ? "range" : "ranges"} will be able to reach {ref}'s database. All other hosts are refused.`,
        confirmLabel: "Apply allowlist",
      };
    default:
      return {
        title: "Make database private?",
        description: "This removes external access to the project database.",
        body: "External clients will lose access to {ref}'s database. The project's own services keep connecting internally.",
        confirmLabel: "Make private",
      };
  }
}

export function ConfigPanel({
  project,
  area,
  config,
  loading,
  onAreaChange,
}: {
  project?: Project;
  area: ConfigArea;
  config?: ProjectConfig;
  loading: boolean;
  onAreaChange: (area: ConfigArea) => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const schema = configSchemas[area];
  const configKey = `${project?.ref ?? ""}:${area}:${config?.updated_at ?? ""}`;
  useEffect(() => {
    if (config) {
      setDraft(config.config);
    } else {
      setDraft({});
    }
  }, [configKey, config]);
  const mutation = useMutation({
    mutationFn: ({ ref, nextArea, values }: { ref: string; nextArea: ConfigArea; values: Record<string, string> }) =>
      updateProjectConfig(ref, nextArea, values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, variables.nextArea] });
      if (variables.nextArea === "network") {
        void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", variables.ref] });
        void queryClient.invalidateQueries({ queryKey: ["network-policy", variables.ref] });
      }
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const updateValue = (key: string, value: string) => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const [query, setQuery] = useState("");
  useEffect(() => {
    setQuery("");
  }, [area]);
  const needle = query.trim().toLowerCase();
  const groups = useMemo(() => {
    const base = configFieldGroups(area);
    if (!needle) return base;
    return base
      .map((group) => ({
        ...group,
        fields: group.fields.filter(
          (field) => field.label.toLowerCase().includes(needle) || field.key.toLowerCase().includes(needle),
        ),
      }))
      .filter((group) => group.fields.length > 0);
  }, [area, needle]);
  const matchCount = useMemo(() => groups.reduce((total, group) => total + group.fields.length, 0), [groups]);
  // Collapse all-but-first group by default for the very long areas, but expand
  // everything while the user is actively searching so matches are never hidden.
  const expandAll = Boolean(needle) || schema.length <= 16;

  const guided = configAreaGuidedTab[area];

  const renderField = (field: ConfigField) => {
    const value = draft[field.key] ?? "";
    const control =
      field.kind === "boolean" ? (
        <input checked={value === "true"} onChange={(event) => updateValue(field.key, event.target.checked ? "true" : "false")} type="checkbox" />
      ) : field.kind === "select" ? (
        <NativeSelect value={value} onChange={(event) => updateValue(field.key, event.target.value)}>
          {(field.options ?? []).map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </NativeSelect>
      ) : field.kind === "textarea" ? (
        <Textarea className="min-h-[88px] font-mono" onChange={(event) => updateValue(field.key, event.target.value)} value={value} />
      ) : (
        <Input className="font-mono" inputMode={field.kind === "number" ? "numeric" : "text"} onChange={(event) => updateValue(field.key, event.target.value)} value={value} />
      );
    return (
      <label className="config-row" key={field.key}>
        <span>
          <span className="text-sm font-medium">{field.label}</span>
          <span className="mt-1 block font-mono text-xs text-faint">{field.key}</span>
        </span>
        {control}
      </label>
    );
  };

  return (
    <AppPanel
      actions={<SlidersHorizontal size={15} className="text-faint" />}
      eyebrow="Runtime config · advanced"
      title="Raw service configuration"
      description="Low-level key/value settings applied directly to the rendered stack. Edits here and in the guided tabs write to the same configuration."
    >
      {project ? (
        <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
          <span className="text-faint">Guided editors:</span>
          <Link className="text-primary hover:underline" params={{ ref: project.ref }} to="/projects/$ref/auth">Auth</Link>
          <Link className="text-primary hover:underline" params={{ ref: project.ref }} to="/projects/$ref/database">Database</Link>
          <Link className="text-primary hover:underline" params={{ ref: project.ref }} to="/projects/$ref/storage">Storage</Link>
          <Link className="text-primary hover:underline" params={{ ref: project.ref }} to="/projects/$ref/functions">Functions</Link>
          <Link className="text-primary hover:underline" params={{ ref: project.ref }} to="/projects/$ref/realtime">Realtime</Link>
        </div>
      ) : null}
      <div className="mt-4 grid gap-3">
        <Segmented
          options={(Object.keys(configAreaLabels) as ConfigArea[]).map((option) => ({ value: option, label: configAreaLabels[option] }))}
          value={area}
          onChange={onAreaChange}
        />
        {guided && project ? (
          <button className="inline-flex w-fit items-center gap-1.5 text-xs text-primary hover:underline" onClick={() => void navigate({ to: projectPath(project.ref, guided.tab) })} type="button">
            <ExternalLink size={13} />
            Open the guided {guided.label} editor for {configAreaLabels[area]}
          </button>
        ) : null}
        {loading ? <p className="text-sm text-muted">Loading configuration...</p> : null}
        {!project ? <p className="text-sm text-muted">Select a project to edit service configuration.</p> : null}
        {project && !loading ? (
          <div className="grid gap-3">
            <label className="relative block">
              <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
              <Input
                className="pl-9"
                onChange={(event) => setQuery(event.target.value)}
                placeholder={`Filter ${configAreaLabels[area]} settings by name or key…`}
                type="search"
                value={query}
              />
            </label>
            {needle ? <p className="text-xs text-faint">{matchCount} setting{matchCount === 1 ? "" : "s"} match “{query.trim()}”.</p> : null}
            {groups.length === 0 ? (
              <EmptyState icon={Search} title="No matching settings" description={`No ${configAreaLabels[area]} setting matches “${query.trim()}”.`} />
            ) : (
              groups.map((group, index) => {
                const single = groups.length === 1;
                if (single) {
                  return (
                    <SubSection key={group.id} title={group.label}>
                      <div className="grid gap-2">{group.fields.map(renderField)}</div>
                    </SubSection>
                  );
                }
                return (
                  <details className="rounded-md border border-border bg-bg" key={group.id} open={expandAll || index === 0}>
                    <summary className="cursor-pointer select-none px-3 py-2 text-sm font-medium">
                      {group.label}
                      <span className="ml-2 text-xs font-normal text-faint">{group.fields.length}</span>
                    </summary>
                    <div className="grid gap-2 px-3 pb-3">{group.fields.map(renderField)}</div>
                  </details>
                );
              })
            )}
            <Button disabled={!project || mutation.isPending} onClick={() => mutation.mutate({ ref: project.ref, nextArea: area, values: draft })} type="button" variant="secondary">
              <Save size={14} />
              Save {configAreaLabels[area]}
            </Button>
          </div>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DangerZonePanel({ onDestroyed, project }: { project?: Project; onDestroyed: () => void }) {
  const queryClient = useQueryClient();
  const [confirmation, setConfirmation] = useState("");
  const [retainVolumes, setRetainVolumes] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const canDestroy = Boolean(project && confirmation === project.ref);
  const destroyMutation = useMutation({
    mutationFn: ({ ref, keepVolumes }: { ref: string; keepVolumes: boolean }) => destroyProject(ref, { retainVolumes: keepVolumes }),
    onSuccess: (_, variables) => {
      const ref = variables.ref;
      setModalOpen(false);
      setConfirmation("");
      setRetainVolumes(false);
      onDestroyed();
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      void queryClient.removeQueries({ queryKey: ["project", ref] });
      void queryClient.removeQueries({ queryKey: ["connect", ref] });
      void queryClient.removeQueries({ queryKey: ["cli-profile", ref] });
      void queryClient.removeQueries({ queryKey: ["project-metrics", ref] });
      void queryClient.removeQueries({ queryKey: ["project-route-manifest", ref] });
      void queryClient.removeQueries({ queryKey: ["project-domains", ref] });
      void queryClient.removeQueries({ queryKey: ["project-config", ref] });
      void queryClient.removeQueries({ queryKey: ["project-services", ref] });
      void queryClient.removeQueries({ queryKey: ["project-functions", ref] });
      void queryClient.removeQueries({ queryKey: ["project-replica-routing", ref] });
      void queryClient.removeQueries({ queryKey: ["project-secrets", ref] });
      void queryClient.removeQueries({ queryKey: ["project-logs", ref] });
      void queryClient.removeQueries({ queryKey: ["backups", ref] });
      void queryClient.removeQueries({ queryKey: ["backup-policy", ref] });
    },
  });

  return (
    <>
      <AppPanel actions={<Trash2 size={15} className="text-danger" />} className="border-danger/40" eyebrow="Danger zone" eyebrowClassName="text-danger" title="Destroy project">
        <p className="mt-1 text-xs text-faint">Requires global admin or project admin access. This action is audited.</p>
        <div className="mt-4 grid gap-3 rounded-md border border-danger/30 bg-bg p-3">
          <p className="text-sm text-muted">
            Destroying a project removes its rendered stack, routes, certificates, control-plane metadata, and generated secrets.
          </p>
          <label className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm text-muted">
            <input className="h-3.5 w-3.5 accent-accent" checked={retainVolumes} onChange={(event) => setRetainVolumes(event.target.checked)} type="checkbox" />
            Retain data volumes for manual recovery
          </label>
          <label className="grid gap-2">
            <span className="text-sm text-muted">Type <span className="font-mono text-text">{project?.ref ?? "project-ref"}</span> to enable destroy.</span>
            <Input className="font-mono" disabled={!project} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} />
          </label>
          <Button disabled={!project || !canDestroy || destroyMutation.isPending} onClick={() => setModalOpen(true)} type="button" variant="danger">
            <Trash2 size={14} />
            Destroy project
          </Button>
          {destroyMutation.error ? <p className="text-sm text-danger">{destroyMutation.error.message}</p> : null}
        </div>
      </AppPanel>
      <Modal
        description="This permanently removes the project from supadupa."
        onClose={() => !destroyMutation.isPending && setModalOpen(false)}
        open={modalOpen}
        title={`Destroy ${project?.name ?? "project"}?`}
        footer={(
          <>
            <Button disabled={destroyMutation.isPending} onClick={() => setModalOpen(false)} type="button" variant="secondary">Cancel</Button>
            <Button
              disabled={!project || !canDestroy || destroyMutation.isPending}
              onClick={() => project && destroyMutation.mutate({ ref: project.ref, keepVolumes: retainVolumes })}
              type="button"
              variant="danger"
            >
              {destroyMutation.isPending ? "Destroying..." : "Destroy project"}
            </Button>
          </>
        )}
      >
        <div className="grid gap-2 text-sm text-muted">
          <p>The project ref confirmation has been entered. This will remove <span className="font-mono text-text">{project?.ref}</span>.</p>
          <p>{retainVolumes ? "Data volumes will be retained for manual recovery." : "Data volumes will be removed by the provisioner when supported."}</p>
        </div>
      </Modal>
    </>
  );
}
