import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Copy, ExternalLink, KeyRound, X } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { disableAccountMFA, enrollAccountMFA, verifyAccountMFA } from "../../api";
import { DataTable } from "../../components/data-table";
import { AppPanel } from "../../components/app/app-panel";
import { Button, buttonVariants } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { StatusPill } from "../../components/ui/status-pill";
import { Field } from "../../components/ui/field";
import type { MFAEnrollment, MFAStatus, OrgAccessReview } from "../../types";
import { formatTime } from "../../lib/format";

type AccessReviewProject = OrgAccessReview["projects"][number];

// Owners and admins have fleet-wide reach by role definition — a per-project
// grant can't scope that away, so their inherited access is expected and never a
// finding. The real risk is a *non-privileged* user (developer/viewer) who can
// reach a project purely through broad org/team membership, with no explicit
// project grant scoping them. An empty project (no effective users) is also not
// a finding — it simply has no access yet.
export function projectNeedsReview(project: AccessReviewProject) {
  // effective/sources can be null in the API response (Go marshals empty slices
  // as null), so guard before reading.
  return (project.effective ?? []).some((entry) => {
    const role = (entry.role ?? "").toLowerCase();
    if (role === "owner" || role === "admin") return false;
    const sources = entry.sources ?? [];
    // Purely inherited == every source is org/team membership, none is an
    // explicit project grant.
    return sources.length > 0 && sources.every((source) => source.startsWith("org:") || source.startsWith("team:"));
  });
}

export function SecurityPanel({ status, loading }: { status?: MFAStatus | null; loading: boolean }) {
  const queryClient = useQueryClient();
  const [enrollment, setEnrollment] = useState<MFAEnrollment | undefined>();
  const [verifyCode, setVerifyCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const enrollMutation = useMutation({
    mutationFn: enrollAccountMFA,
    onSuccess: (payload) => {
      setEnrollment(payload);
      setVerifyCode("");
      void queryClient.invalidateQueries({ queryKey: ["account-mfa"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const verifyMutation = useMutation({
    mutationFn: verifyAccountMFA,
    onSuccess: () => {
      setEnrollment(undefined);
      setVerifyCode("");
      void queryClient.invalidateQueries({ queryKey: ["account-mfa"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const disableMutation = useMutation({
    mutationFn: disableAccountMFA,
    onSuccess: () => {
      setDisableCode("");
      void queryClient.invalidateQueries({ queryKey: ["account-mfa"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const current = enrollment ?? status;

  function cancelEnrollment() {
    setEnrollment(undefined);
    setVerifyCode("");
    enrollMutation.reset();
    verifyMutation.reset();
  }

  return (
    <AppPanel
      eyebrow="Security"
      title="Your account MFA"
      actions={
        <StatusPill
          tone={loading ? "info" : current?.enabled ? "success" : current?.pending ? "info" : "neutral"}
          label={loading ? "loading" : current?.enabled ? "enabled" : current?.pending ? "pending" : "disabled"}
        />
      }
      description={
        <>
          Manages multi-factor authentication for your own control-plane sign-in
          {status?.email ? <> ({status.email})</> : null}. It does not change MFA for other operators or end users.
        </>
      }
    >
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading MFA status...</p> : null}
        {!current?.enabled && !enrollment ? (
          <Button className="justify-self-start" variant="secondary" disabled={enrollMutation.isPending} onClick={() => enrollMutation.mutate()} type="button">
            <KeyRound size={14} />
            Enroll authenticator
          </Button>
        ) : null}
        {enrollment ? (
          <div className="grid gap-3">
            <div className="flex items-start gap-4 rounded-md border border-border bg-bg p-3 max-sm:flex-col max-sm:items-stretch">
              {/* QR needs a light quiet zone to scan reliably, so it sits on a
                  white tile regardless of theme. */}
              <div className="shrink-0 self-center rounded-md bg-white p-3">
                <QRCodeSVG value={enrollment.otpauth_url} size={132} level="M" marginSize={0} />
              </div>
              <div className="min-w-0 text-xs text-muted">
                <p className="text-sm font-medium text-text">Scan with your authenticator app</p>
                <p className="mt-1">
                  Open your authenticator (1Password, Authy, Google Authenticator, etc.) and scan this code, then enter the 6-digit
                  code below to confirm. Can&apos;t scan? Add the account manually with the setup key or{" "}
                  <span className="font-mono">otpauth://</span> URI below.
                </p>
              </div>
            </div>
            <div className="copy-row">
              <div className="min-w-0">
                <p className="label">Setup key (secret)</p>
                <p className="truncate font-mono text-sm">{enrollment.secret}</p>
              </div>
              <Button aria-label="Copy secret" variant="ghost" size="icon" onClick={() => void navigator.clipboard.writeText(enrollment.secret)} type="button">
                <Copy size={14} />
              </Button>
            </div>
            <div className="copy-row">
              <div className="min-w-0">
                <p className="label">Authenticator URI (otpauth)</p>
                <p className="break-all font-mono text-xs text-muted">{enrollment.otpauth_url}</p>
              </div>
              <Button aria-label="Copy authenticator URI" variant="ghost" size="icon" onClick={() => void navigator.clipboard.writeText(enrollment.otpauth_url)} type="button">
                <Copy size={14} />
              </Button>
            </div>
            <form className="grid gap-2" onSubmit={(event) => {
              event.preventDefault();
              verifyMutation.mutate(verifyCode);
            }}>
              <Field label="Verification code" hint="6-digit code from your authenticator app">
                <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 max-sm:grid-cols-1">
                  <Input inputMode="numeric" maxLength={6} placeholder="123456" value={verifyCode} onChange={(event) => setVerifyCode(event.target.value)} />
                  <Button className="justify-self-start" disabled={verifyMutation.isPending || verifyCode.length < 6} type="submit">
                    Verify
                  </Button>
                  <Button className="justify-self-start" variant="secondary" onClick={cancelEnrollment} type="button">
                    <X size={14} />
                    Cancel
                  </Button>
                </div>
              </Field>
            </form>
          </div>
        ) : null}
        {current?.enabled ? (
          <form className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1" onSubmit={(event) => {
            event.preventDefault();
            disableMutation.mutate(disableCode);
          }}>
            <Input inputMode="numeric" maxLength={6} placeholder="Code to disable" value={disableCode} onChange={(event) => setDisableCode(event.target.value)} />
            <Button className="justify-self-start" variant="danger" disabled={disableMutation.isPending || disableCode.length < 6} type="submit">
              Disable
            </Button>
          </form>
        ) : null}
        {enrollMutation.error ? <p className="text-sm text-danger">{enrollMutation.error.message}</p> : null}
        {verifyMutation.error ? <p className="text-sm text-danger">{verifyMutation.error.message}</p> : null}
        {disableMutation.error ? <p className="text-sm text-danger">{disableMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function AccessReviewPanel({ review, loading }: { review?: OrgAccessReview | null; loading: boolean }) {
  const members = review?.members ?? [];
  const teams = review?.teams ?? [];
  const projects = review?.projects ?? [];
  const reviewCount = useMemo(() => projects.filter(projectNeedsReview).length, [projects]);
  const columns = useMemo<ColumnDef<AccessReviewProject>[]>(
    () => [
      {
        header: "Project",
        accessorKey: "project_name",
        size: 200,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate">{row.original.project_name}</p>
            <p className="cell-sub font-mono">{row.original.project_ref}</p>
          </>
        ),
      },
      {
        header: "Effective users",
        accessorKey: "effective",
        size: 340,
        cell: ({ row }) => (
          <>
            <p className="truncate text-sm">{formatAccessEntries((row.original.effective ?? []).map((entry) => `${entry.email}:${entry.role}`), "No effective users")}</p>
            <p className="cell-sub">
              {(row.original.effective ?? []).length} users &middot; via {summarizeSources((row.original.effective ?? []).flatMap((entry) => entry.sources ?? []))}
            </p>
          </>
        ),
      },
      {
        header: "Explicit grants",
        accessorKey: "grants",
        size: 260,
        cell: ({ row }) => (
          <>
            <p className="truncate text-sm">{formatAccessEntries((row.original.grants ?? []).map((grant) => `${grant.subject_name}:${grant.role}`), "Inherited org access only")}</p>
            <p className="cell-sub">{(row.original.grants ?? []).length} direct project grants</p>
          </>
        ),
      },
      {
        header: "State",
        id: "state",
        size: 120,
        cell: ({ row }) => {
          if (projectNeedsReview(row.original)) {
            return <StatusPill tone="warning" label="review" />;
          }
          if ((row.original.effective ?? []).length === 0) {
            return <StatusPill tone="neutral" label="no access" />;
          }
          return <StatusPill tone="success" label="scoped" />;
        },
      },
      {
        header: "",
        id: "actions",
        size: 110,
        cell: ({ row }) => (
          <Link
            className={buttonVariants({ variant: "secondary", size: "sm" })}
            params={{ ref: row.original.project_ref }}
            to="/projects/$ref/auth"
          >
            Manage
            <ExternalLink size={12} />
          </Link>
        ),
      },
    ],
    [],
  );

  return (
    <AppPanel
      eyebrow="Access review"
      title="Effective project access"
      description={"\"Review\" flags projects a non-admin user (developer/viewer) can reach purely through org/team membership, with no explicit project grant scoping them. Owners and admins always have fleet-wide access by role, so their reach is never flagged."}
      actions={
        <div className="flex flex-wrap justify-end gap-2">
          <StatusPill tone="neutral" label={`${members.length} members`} />
          <StatusPill tone="neutral" label={`${teams.length} teams`} />
          <StatusPill tone={reviewCount > 0 ? "warning" : "success"} label={reviewCount > 0 ? `${reviewCount} need review` : `${projects.length} scoped`} />
        </div>
      }
    >
      {review ? <p className="mt-1 text-xs text-faint">Generated {formatTime(review.generated_at)}</p> : null}
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading access review...</p> : null}
        <DataTable columns={columns} data={projects} emptyText={loading ? "Loading access review..." : "No projects to review."} minWidth={920} rowClassName={(project) => projectNeedsReview(project) ? "table-row-warning" : ""} />
      </div>
    </AppPanel>
  );
}

function formatAccessEntries(entries: string[], fallback: string) {
  return entries.length > 0 ? entries.join(", ") : fallback;
}

// Collapse the per-user `sources[]` (e.g. "org:owner", "team:platform",
// "grant") into a deduped, human summary of WHY users have access.
function summarizeSources(sources: string[]) {
  const unique = Array.from(new Set(sources.filter(Boolean)));
  return unique.length > 0 ? unique.join(", ") : "direct grants";
}
