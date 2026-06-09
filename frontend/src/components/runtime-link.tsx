import { ExternalLink } from "lucide-react";
import type { MouseEvent } from "react";
import { useState } from "react";
import { createProjectStudioSession } from "../api";

export function RuntimeLink({ className = "button secondary h-8 min-h-8 justify-self-start", label, projectRef, url }: { label: string; url: string; className?: string; projectRef?: string }) {
  const routingHint = localRoutingHint(url);
  const [error, setError] = useState("");
  async function openRuntime(event: MouseEvent<HTMLAnchorElement>) {
    if (!projectRef || !isStudioURL(url)) {
      return;
    }
    event.preventDefault();
    setError("");
    const opened = window.open("about:blank", "_blank");
    try {
      if (opened) {
        opened.opener = null;
      }
      const session = await createProjectStudioSession(projectRef);
      const studioURL = withStudioCode(url, session.code);
      if (opened) {
        opened.location.replace(studioURL);
        return;
      }
      window.open(studioURL, "_blank", "noopener,noreferrer");
    } catch (err) {
      if (opened) {
        opened.close();
      }
      setError(err instanceof Error ? err.message : "Could not open Studio");
    }
  }
  return (
    <div className="grid gap-1">
      <a className={className} href={url} onClick={(event) => void openRuntime(event)} rel="noreferrer" target="_blank">
        <ExternalLink size={14} />
        {label}
      </a>
      {routingHint ? <p className="truncate text-xs text-faint">{routingHint}</p> : null}
      {error ? <p className="truncate text-xs text-danger">{error}</p> : null}
    </div>
  );
}

function isStudioURL(url: string) {
  try {
    return new URL(url).hostname.startsWith("studio-");
  } catch {
    return false;
  }
}

function withStudioCode(url: string, code: string) {
  const next = new URL(url);
  next.searchParams.set("supadupa_studio_code", code);
  return next.toString();
}

function localRoutingHint(url: string) {
  if (!isLocalBrowser()) {
    return "";
  }
  const host = hostFor(url);
  if (!host || isLoopbackHost(host)) {
    return "";
  }
  if (host.endsWith(".localhost")) {
    return `Uses the local project router for ${host}.`;
  }
  if (host.endsWith(".test") || host.endsWith(".local")) {
    return `Requires local DNS/hosts and the project router for ${host}.`;
  }
  return "";
}

function isLocalBrowser() {
  return isLoopbackHost(window.location.hostname) || window.location.hostname.endsWith(".localhost");
}

function isLoopbackHost(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1" || hostname === "[::1]";
}

function hostFor(url: string) {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}
