import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Copy, KeyRound } from "lucide-react";
import { disableAccountMFA, enrollAccountMFA, verifyAccountMFA } from "../../api";
import { DataTable } from "../../components/data-table";
import type { MFAEnrollment, MFAStatus, OrgAccessReview } from "../../types";
import { formatTime } from "../../lib/format";

type AccessReviewProject = OrgAccessReview["projects"][number];

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

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Security</p>
          <h2>Platform MFA</h2>
        </div>
        <span className={`pill ${current?.enabled ? "healthy" : current?.pending ? "provisioning" : ""}`}>
          {loading ? "loading" : current?.enabled ? "enabled" : current?.pending ? "pending" : "disabled"}
        </span>
      </div>
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading MFA status...</p> : null}
        {!current?.enabled ? (
          <button className="button secondary justify-center" disabled={enrollMutation.isPending} onClick={() => enrollMutation.mutate()} type="button">
            <KeyRound size={14} />
            Enroll authenticator
          </button>
        ) : null}
        {enrollment ? (
          <div className="grid gap-2">
            <div className="copy-row">
              <div className="min-w-0">
                <p className="label">Secret</p>
                <p className="truncate font-mono text-sm">{enrollment.secret}</p>
              </div>
              <button className="icon-button" onClick={() => void navigator.clipboard.writeText(enrollment.secret)} type="button">
                <Copy size={14} />
              </button>
            </div>
            <div className="copy-row">
              <div className="min-w-0">
                <p className="label">Authenticator URI</p>
                <p className="truncate font-mono text-xs text-muted">{enrollment.otpauth_url}</p>
              </div>
              <button className="icon-button" onClick={() => void navigator.clipboard.writeText(enrollment.otpauth_url)} type="button">
                <Copy size={14} />
              </button>
            </div>
            <form className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1" onSubmit={(event) => {
              event.preventDefault();
              verifyMutation.mutate(verifyCode);
            }}>
              <input className="input" inputMode="numeric" maxLength={6} placeholder="123456" value={verifyCode} onChange={(event) => setVerifyCode(event.target.value)} />
              <button className="button justify-center" disabled={verifyMutation.isPending || verifyCode.length < 6} type="submit">
                Verify
              </button>
            </form>
          </div>
        ) : null}
        {current?.enabled ? (
          <form className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1" onSubmit={(event) => {
            event.preventDefault();
            disableMutation.mutate(disableCode);
          }}>
            <input className="input" inputMode="numeric" maxLength={6} placeholder="Code to disable" value={disableCode} onChange={(event) => setDisableCode(event.target.value)} />
            <button className="button danger justify-center" disabled={disableMutation.isPending || disableCode.length < 6} type="submit">
              Disable
            </button>
          </form>
        ) : null}
        {enrollMutation.error ? <p className="text-sm text-danger">{enrollMutation.error.message}</p> : null}
        {verifyMutation.error ? <p className="text-sm text-danger">{verifyMutation.error.message}</p> : null}
        {disableMutation.error ? <p className="text-sm text-danger">{disableMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function AccessReviewPanel({ review, loading }: { review?: OrgAccessReview | null; loading: boolean }) {
  const members = review?.members ?? [];
  const teams = review?.teams ?? [];
  const projects = review?.projects ?? [];
  const columns = useMemo<ColumnDef<AccessReviewProject>[]>(
    () => [
      {
        header: "Project",
        accessorKey: "project_name",
        size: 230,
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
        size: 360,
        cell: ({ row }) => (
          <>
            <p className="truncate text-sm">{formatAccessEntries(row.original.effective.map((entry) => `${entry.email}:${entry.role}`), "No effective users")}</p>
            <p className="cell-sub">{row.original.effective.length} users resolved from org, team, and project grants</p>
          </>
        ),
      },
      {
        header: "Explicit grants",
        accessorKey: "grants",
        size: 300,
        cell: ({ row }) => (
          <>
            <p className="truncate text-sm">{formatAccessEntries(row.original.grants.map((grant) => `${grant.subject_name}:${grant.role}`), "Inherited org access only")}</p>
            <p className="cell-sub">{row.original.grants.length} direct project grants</p>
          </>
        ),
      },
      {
        header: "State",
        id: "state",
        size: 130,
        cell: ({ row }) => <span className={`pill ${row.original.effective.length > 0 ? "healthy" : "warning"}`}>{row.original.effective.length > 0 ? "covered" : "review"}</span>,
      },
    ],
    [],
  );

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Access review</p>
          <h2>Effective project access</h2>
          {review ? <p className="mt-1 text-xs text-faint">{formatTime(review.generated_at)}</p> : null}
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <span className="pill">{members.length} members</span>
          <span className="pill">{teams.length} teams</span>
          <span className="pill">{projects.length} projects</span>
        </div>
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading access review...</p> : null}
        <DataTable columns={columns} data={projects} emptyText={loading ? "Loading access review..." : "No projects to review."} minWidth={920} rowClassName={(project) => project.effective.length === 0 ? "table-row-warning" : ""} />
      </div>
    </section>
  );
}

function formatAccessEntries(entries: string[], fallback: string) {
  return entries.length > 0 ? entries.join(", ") : fallback;
}
