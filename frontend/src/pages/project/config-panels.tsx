import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Globe2, Plus, RadioTower, Save, SlidersHorizontal, Trash2, X } from "lucide-react";
import {
  addProjectDomain,
  createProjectNetworkConnection,
  deleteProjectDomain,
  deleteProjectNetworkConnection,
  destroyProject,
  resetProjectDomainCertificate,
  updateProjectConfig,
  updateProjectServices,
  uploadProjectDomainCertificate,
} from "../../api";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import {
  configAreaLabels,
  configSchemas,
  projectServiceLabels,
  type ConfigArea,
} from "../../lib/project-config";
import { formatDateTime, formatTime } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import type { Project, ProjectConfig, ProjectDomain, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectServices } from "../../types";

function ConfigDetailHeader({ detail, title, onBack }: { title: string; detail: string; onBack: () => void }) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <button className="segmented mb-3 h-8" onClick={onBack} type="button">
        <ArrowLeft size={14} />
        Back
      </button>
      <p className="label">{title}</p>
      <p className="mt-1 text-sm text-muted">{detail}</p>
    </div>
  );
}

function ConfigResourceCard({ detail, label, meta, status, onClick }: { label: string; meta: string; detail: string; status: string; onClick: () => void }) {
  return (
    <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <div className="mb-3 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{label}</p>
          <p className="truncate font-mono text-xs text-faint">{meta}</p>
        </div>
        <span className={`pill ${status === "active" || status === "ready" || status === "validated" ? "healthy" : ""}`}>{status}</span>
      </div>
      <p className="truncate text-xs text-muted">{detail}</p>
    </button>
  );
}

export function DomainsPanel({ project, domains, loading, enabled }: { project?: Project; domains: ProjectDomain[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/config\/domains\/([^/]+)/)?.[1];
  const selectedFqdn = selectedItem ? decodeURIComponent(selectedItem) : "";
  const selectedDomain = selectedFqdn && selectedFqdn !== "new" ? domains.find((domain) => domain.fqdn === selectedFqdn) : undefined;
  const basePath = project ? `/projects/${project.ref}/config/domains` : "";
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
      void navigate({ to: `/projects/${variables.ref}/config/domains` });
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
      cell: ({ row }) => <span className="pill">{row.original.cert_status}</span>,
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
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Domains</p>
          <h2>Custom ingress</h2>
        </div>
        <Globe2 size={15} className="text-faint" />
      </div>
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Custom domains disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the custom_domains feature flag for this org before adding ingress domains.</p>
            </div>
          ) : null}
          <button className="button secondary w-fit" disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button">
            <Plus size={14} />
            Add domain
          </button>
          {loading ? <p className="text-sm text-muted">Loading domains...</p> : null}
          {!loading ? <DataTable columns={domainColumns} data={domains} emptyText="No custom domains configured." minWidth={760} /> : null}
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
            <input className="input font-mono" disabled={!enabled} placeholder="api.example.com" value={fqdn} onChange={(event) => setFqdn(event.target.value)} />
            <button className="button secondary justify-center" disabled={!enabled || !project || addMutation.isPending || fqdn.trim().length === 0} type="submit">
              <Plus size={14} />
              Add
            </button>
          </div>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <ConfigDetailHeader detail={selectedDomain ? "Custom ingress domain and certificate state." : "Domain not found in the current project."} title={selectedFqdn} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedDomain ? (
            <div className="grid gap-2">
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Certificate</p><p className="text-sm font-medium">{selectedDomain.cert_status}</p></div>
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
                <textarea className="input min-h-28 font-mono text-xs" placeholder="-----BEGIN CERTIFICATE-----" value={certificatePEM} onChange={(event) => setCertificatePEM(event.target.value)} />
                <textarea className="input min-h-28 font-mono text-xs" placeholder="-----BEGIN PRIVATE KEY-----" value={privateKeyPEM} onChange={(event) => setPrivateKeyPEM(event.target.value)} />
                <div className="flex gap-2 max-sm:flex-col">
                  <button className="button secondary justify-center" disabled={!project || uploadCertMutation.isPending || !certificatePEM.trim() || !privateKeyPEM.trim()} type="submit">
                    <Save size={14} />
                    Upload certificate
                  </button>
                  <button className="button secondary justify-center" disabled={!project || resetCertMutation.isPending || selectedDomain.cert_mode !== "byo"} onClick={() => project && resetCertMutation.mutate({ ref: project.ref, domain: selectedDomain.fqdn })} type="button">
                    Reset to ACME
                  </button>
                </div>
                {uploadCertMutation.error ? <p className="text-sm text-danger">{String(uploadCertMutation.error)}</p> : null}
                {resetCertMutation.error ? <p className="text-sm text-danger">{String(resetCertMutation.error)}</p> : null}
              </form>
              <button className="button danger w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, domain: selectedDomain.fqdn })} type="button">
                <X size={14} />
                Remove domain
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-3 grid gap-2">
        {addMutation.error ? <p className="text-sm text-danger">{addMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function NetworkConnectionsPanel({ project, policy, connections, loading, enabled }: { project?: Project; policy?: ProjectNetworkPolicy; connections: ProjectNetworkConnection[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/config\/network\/([^/]+)/)?.[1];
  const selectedConnection = selectedItem && selectedItem !== "new" ? connections.find((connection) => connection.id === selectedItem) : undefined;
  const basePath = project ? `/projects/${project.ref}/config/network` : "";
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
      void navigate({ to: `/projects/${variables.ref}/config/network` });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectNetworkConnection(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || parseLines(form.cidrs).length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Security</p>
          <h2>Private networks</h2>
        </div>
        <RadioTower size={15} className="text-faint" />
      </div>
      <div className="mt-4 grid grid-cols-3 gap-2 max-md:grid-cols-1">
        <div className="metric-cell">
          <p className="label">Ingress allowlist</p>
          <h3>{allowlistEntries.length > 0 ? `${allowlistEntries.length} CIDR${allowlistEntries.length === 1 ? "" : "s"}` : "Open"}</h3>
          <p className="truncate text-xs text-faint">{allowlistEntries.join(", ") || "0.0.0.0/0 equivalent"}</p>
        </div>
        <div className="metric-cell">
          <p className="label">Route TLS</p>
          <h3>{sslEnforced ? "Enforced" : "Optional"}</h3>
          <p className="text-xs text-faint">{sslEnforced ? "Routes require secure ingress" : "Operator allows non-strict ingress"}</p>
        </div>
        <div className="metric-cell">
          <p className="label">Connections</p>
          <h3>{connections.length}</h3>
          <p className="text-xs text-faint">{connections.length === 1 ? "Private declaration" : "Private declarations"}</p>
        </div>
      </div>
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          {!enabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">Network restrictions disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the network_restrictions feature flag for this org before requesting private connectivity.</p>
            </div>
          ) : null}
          <button className="button secondary w-fit" disabled={!enabled || !project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button">
            <Plus size={14} />
            Request network
          </button>
          <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
            {loading ? <p className="text-sm text-muted">Loading private network connections...</p> : null}
            {!loading && connections.length === 0 ? <p className="text-sm text-muted">No private network connections requested.</p> : null}
            {connections.map((connection) => (
              <ConfigResourceCard
                detail={`${connection.cidrs.join(", ")}${connection.endpoint_id ? ` · ${connection.endpoint_id}` : ""}`}
                key={connection.id}
                label={connection.name}
                meta={`${connection.type} · ${connection.provider}${connection.region ? ` · ${connection.region}` : ""}`}
                status={connection.status}
                onClick={() => basePath && void navigate({ to: `${basePath}/${connection.id}` })}
              />
            ))}
          </div>
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
            <input className="input font-mono" disabled={!enabled} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            <select className="input" disabled={!enabled} value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value })}>
              <option value="privatelink">PrivateLink</option>
              <option value="vpc_peering">VPC peering</option>
              <option value="private_endpoint">Private endpoint</option>
              <option value="wireguard">WireGuard</option>
              <option value="operator_network">Operator network</option>
            </select>
            <select className="input" disabled={!enabled} value={form.provider} onChange={(event) => setForm({ ...form, provider: event.target.value })}>
              <option value="aws">AWS</option>
              <option value="gcp">GCP</option>
              <option value="azure">Azure</option>
              <option value="custom">Custom</option>
              <option value="operator">Operator</option>
            </select>
            <input className="input font-mono" disabled={!enabled} value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} />
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <input className="input font-mono" disabled={!enabled} value={form.endpoint_id} onChange={(event) => setForm({ ...form, endpoint_id: event.target.value })} />
            <textarea className="input min-h-[52px] font-mono" disabled={!enabled} value={form.cidrs} onChange={(event) => setForm({ ...form, cidrs: event.target.value })} />
          </div>
          <textarea className="input min-h-[64px] font-mono" disabled={!enabled} value={form.config} onChange={(event) => setForm({ ...form, config: event.target.value })} />
          <button className="button secondary justify-center" disabled={!enabled || !project || createMutation.isPending || parseLines(form.cidrs).length === 0} type="submit">
            <Plus size={14} />
            Request network
          </button>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <ConfigDetailHeader detail={selectedConnection ? `${selectedConnection.type} via ${selectedConnection.provider}` : "Network connection not found in the current project."} title={selectedConnection?.name ?? selectedItem} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedConnection ? (
            <div className="grid gap-2">
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Status</p><p className="text-sm font-medium">{selectedConnection.status}</p></div>
                <div className="metric-cell"><p className="label">Region</p><p className="text-sm font-medium">{selectedConnection.region || "operator"}</p></div>
                <div className="metric-cell"><p className="label">Endpoint</p><p className="truncate font-mono text-sm font-medium">{selectedConnection.endpoint_id || "pending"}</p></div>
              </div>
              <div className="metric-cell">
                <p className="label">CIDRs</p>
                <p className="mt-1 font-mono text-sm text-muted">{selectedConnection.cidrs.join(", ") || "none"}</p>
              </div>
              <button className="button danger w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, id: selectedConnection.id })} type="button">
                <X size={14} />
                Delete network
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-4 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
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
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Services</p>
          <h2>Enabled stack services</h2>
        </div>
        <span className="pill healthy">{enabledCount}/{projectServiceLabels.length}</span>
      </div>
      <div className="mt-4 grid grid-cols-2 gap-2 max-sm:grid-cols-1">
        {loading ? <p className="text-sm text-muted">Loading services...</p> : null}
        {projectServiceLabels.map((service) => {
          const checked = draft[service.key] ?? true;
          return (
            <label className="config-toggle" key={service.key}>
              <span>
                <span className="block text-sm font-medium">{service.label}</span>
                <span className="block font-mono text-xs text-faint">{service.key}</span>
              </span>
              <input checked={checked} onChange={(event) => setDraft({ ...draft, [service.key]: event.target.checked })} type="checkbox" />
            </label>
          );
        })}
      </div>
      <div className="mt-4 flex items-center justify-between gap-3">
        <p className="truncate text-xs text-muted">{services ? `Last changed ${formatTime(services.updated_at)}` : "Desired service state is loaded from the project spec."}</p>
        <button className="button secondary" disabled={!project || loading || mutation.isPending} onClick={() => project && mutation.mutate({ ref: project.ref, next: draft })} type="button">
          <Save size={14} />
          Save
        </button>
      </div>
      {mutation.error ? <p className="mt-3 text-sm text-danger">{mutation.error.message}</p> : null}
    </section>
  );
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

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Configuration</p>
          <h2>Project services</h2>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>
      <div className="mt-4 grid gap-3">
        <div className="grid grid-cols-3 gap-2 max-sm:grid-cols-2">
          {(Object.keys(configAreaLabels) as ConfigArea[]).map((option) => (
            <button className={`segmented ${area === option ? "active" : ""}`} key={option} onClick={() => onAreaChange(option)} type="button">
              {configAreaLabels[option]}
            </button>
          ))}
        </div>
        {loading ? <p className="text-sm text-muted">Loading configuration...</p> : null}
        {!project ? <p className="text-sm text-muted">Select a project to edit service configuration.</p> : null}
        {project && !loading ? (
          <div className="grid gap-2">
            {schema.map((field) => {
              const value = draft[field.key] ?? "";
              return (
                <label className="config-row" key={field.key}>
                  <span>
                    <span className="text-sm font-medium">{field.label}</span>
                    <span className="mt-1 block font-mono text-xs text-faint">{field.key}</span>
                  </span>
                  {field.kind === "boolean" ? (
                    <input checked={value === "true"} onChange={(event) => updateValue(field.key, event.target.checked ? "true" : "false")} type="checkbox" />
                  ) : field.kind === "textarea" ? (
                    <textarea
                      className="input min-h-[88px] font-mono"
                      onChange={(event) => updateValue(field.key, event.target.value)}
                      value={value}
                    />
                  ) : (
                    <input
                      className="input font-mono"
                      inputMode={field.kind === "number" ? "numeric" : "text"}
                      onChange={(event) => updateValue(field.key, event.target.value)}
                      value={value}
                    />
                  )}
                </label>
              );
            })}
            <button className="button secondary justify-center" disabled={!project || mutation.isPending} onClick={() => mutation.mutate({ ref: project.ref, nextArea: area, values: draft })} type="button">
              <Save size={14} />
              Save {configAreaLabels[area]}
            </button>
          </div>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </section>
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
      <section className="panel border-danger/40">
        <div className="section-head">
          <div>
            <p className="label text-danger">Danger zone</p>
            <h2>Destroy project</h2>
            <p className="mt-1 text-xs text-faint">Requires global admin or project admin access. This action is audited.</p>
          </div>
          <Trash2 size={15} className="text-danger" />
        </div>
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
            <input className="input font-mono" disabled={!project} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} />
          </label>
          <button className="button danger justify-center" disabled={!project || !canDestroy || destroyMutation.isPending} onClick={() => setModalOpen(true)} type="button">
            <Trash2 size={14} />
            Destroy project
          </button>
          {destroyMutation.error ? <p className="text-sm text-danger">{destroyMutation.error.message}</p> : null}
        </div>
      </section>
      <Modal
        description="This permanently removes the project from supadupa."
        onClose={() => !destroyMutation.isPending && setModalOpen(false)}
        open={modalOpen}
        title={`Destroy ${project?.name ?? "project"}?`}
        footer={(
          <>
            <button className="button secondary" disabled={destroyMutation.isPending} onClick={() => setModalOpen(false)} type="button">Cancel</button>
            <button
              className="button danger"
              disabled={!project || !canDestroy || destroyMutation.isPending}
              onClick={() => project && destroyMutation.mutate({ ref: project.ref, keepVolumes: retainVolumes })}
              type="button"
            >
              {destroyMutation.isPending ? "Destroying..." : "Destroy project"}
            </button>
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
