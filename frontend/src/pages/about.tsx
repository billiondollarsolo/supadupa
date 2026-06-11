import { useQuery } from "@tanstack/react-query";
import { Github, ExternalLink } from "lucide-react";
import { getApiHealth } from "../api";
import { AppPanel } from "../components/app/app-panel";
import { InfoRow } from "../components/app/info-row";
import { BrandLogo } from "../components/brand-logo";
import { BrandWordmark } from "../components/brand-wordmark";
import { BUILDERS, REPO_URL } from "../components/built-by-footer";

export function AboutPage() {
  const health = useQuery({ queryKey: ["api-health"], queryFn: getApiHealth, refetchInterval: 30_000 });
  const version = health.data?.version ?? "—";
  const build = health.data?.build && health.data.build !== "unknown" ? health.data.build : "—";

  return (
    <div className="grid max-w-2xl gap-6">
      <AppPanel>
        <div className="flex items-center gap-4">
          <BrandLogo className="h-14 w-14" />
          <div className="min-w-0">
            <BrandWordmark className="text-[28px]!" />
            <p className="mt-1 text-sm text-muted">
              Self-hosted, multi-project Supabase control plane — run an isolated Supabase stack per project on your own infrastructure.
            </p>
          </div>
        </div>
      </AppPanel>

      <AppPanel eyebrow="Release" title="Version">
        <div className="mt-3 grid gap-1">
          <InfoRow title="Platform version" detail="Reported by the control-plane API" value={version} />
          <InfoRow title="Build" detail="Git commit this build was compiled from" value={build} />
          <InfoRow title="API status" detail="Live management API health" value={health.isError ? "offline" : health.data?.status ?? "checking"} />
        </div>
      </AppPanel>

      <AppPanel eyebrow="Project" title="Links & credits">
        <div className="mt-3 grid gap-1">
          <a className="usage-row hover:bg-surface-2" href={REPO_URL} rel="noreferrer noopener" target="_blank">
            <div className="min-w-0">
              <p className="flex items-center gap-2 truncate text-sm font-medium"><Github size={14} className="text-faint" />Source repository</p>
              <p className="truncate text-xs text-muted">{REPO_URL.replace("https://", "")}</p>
            </div>
            <ExternalLink size={14} className="text-faint" />
          </a>
          {BUILDERS.map((builder) => (
            <a className="usage-row hover:bg-surface-2" href={builder.url} key={builder.handle} rel="noreferrer noopener" target="_blank">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">@{builder.handle}</p>
                <p className="truncate text-xs text-muted">Built by · {builder.url.replace("https://", "")}</p>
              </div>
              <ExternalLink size={14} className="text-faint" />
            </a>
          ))}
        </div>
      </AppPanel>
    </div>
  );
}
