import { projectTabs, type ProjectTab } from "./project-config";

export const projectRouteByTab: Record<ProjectTab, string> = {
  overview: "/projects/$ref",
  connect: "/projects/$ref/connect",
  access: "/projects/$ref/access",
  database: "/projects/$ref/database",
  logs: "/projects/$ref/logs",
  backups: "/projects/$ref/backups",
  config: "/projects/$ref/config",
  activity: "/projects/$ref/activity",
};

export function projectPath(ref: string, ...segments: Array<string | undefined>) {
  const encoded = [ref, ...segments.filter((segment): segment is string => Boolean(segment))]
    .map((segment) => encodeURIComponent(segment));
  return `/projects/${encoded.join("/")}`;
}

export function projectRefFromPathname(pathname: string) {
  const parts = projectPathParts(pathname);
  if (parts[1] === "new") {
    return "";
  }
  return decodePathPart(parts[1]);
}

export function projectTabFromPathname(pathname: string): ProjectTab {
  const suffix = decodePathPart(projectPathParts(pathname)[2]);
  return projectTabs.find((tab) => tab.suffix === suffix)?.id ?? "overview";
}

export function projectSectionFromPathname(pathname: string, tabSuffix?: string) {
  return projectSubrouteFromPathname(pathname, tabSuffix).section;
}

export function projectSubrouteFromPathname(pathname: string, tabSuffix?: string) {
  const parts = projectPathParts(pathname);
  const suffix = decodePathPart(parts[2]);
  if (tabSuffix && suffix !== tabSuffix) {
    return { section: "overview", item: "" };
  }
  return { section: decodePathPart(parts[3], "overview"), item: decodePathPart(parts[4]) };
}

function projectPathParts(pathname: string) {
  const parts = pathname.split("/").filter(Boolean);
  return parts[0] === "projects" ? parts : [];
}

function decodePathPart(value: string | undefined, fallback = "") {
  if (!value) {
    return fallback;
  }
  try {
    return decodeURIComponent(value);
  } catch {
    return fallback;
  }
}
