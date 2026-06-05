import { ExternalLink } from "lucide-react";

export function RuntimeLink({ className = "button secondary h-8 min-h-8 justify-center", label, url }: { label: string; url: string; className?: string }) {
  const routingHint = localRoutingHint(url);
  return (
    <div className="grid gap-1">
      <a className={className} href={url} rel="noreferrer" target="_blank">
        <ExternalLink size={14} />
        {label}
      </a>
      {routingHint ? <p className="truncate text-xs text-faint">{routingHint}</p> : null}
    </div>
  );
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
