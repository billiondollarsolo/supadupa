import { useState } from "react";
import { ChevronRight, MoreHorizontal } from "lucide-react";
import { Link } from "@tanstack/react-router";
import {
  organizationSections,
  platformSettingsSections,
  projectSubnav,
  projectTabs,
  securitySections,
} from "../lib/project-config";
import { projectPath, projectRefFromPathname, projectSubrouteFromPathname, projectTabFromPathname } from "../lib/routes";
import type { Project } from "../types";

export type Crumb = { label: string; href?: string };

function labelFor(sections: ReadonlyArray<{ id: string; label: string }>, id: string, fallback: string) {
  return sections.find((section) => section.id === id)?.label ?? fallback;
}

function decode(value: string) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

// The last crumb is the current page, so it renders as plain text (no link).
function finalize(crumbs: Crumb[]): Crumb[] {
  return crumbs.map((crumb, index) => (index === crumbs.length - 1 ? { label: crumb.label } : crumb));
}

// Derive a breadcrumb trail from the pathname. Returns [] for top-level routes
// (Dashboard, Projects list, Hosts root, Audit, About) so the header can fall
// back to its single-title block. Only nested routes get a trail.
export function buildBreadcrumbs(pathname: string, opts: { activeProject?: Project; orgsEnabled: boolean }): Crumb[] {
  const parts = pathname.split("/").filter(Boolean);
  const head = parts[0];

  if (head === "projects") {
    const ref = projectRefFromPathname(pathname);
    if (!ref) {
      return [];
    }
    const tab = projectTabFromPathname(pathname);
    const tabMeta = projectTabs.find((entry) => entry.id === tab);
    const projectName = opts.activeProject?.ref === ref ? opts.activeProject?.name ?? ref : ref;
    const crumbs: Crumb[] = [
      { label: "Projects", href: "/projects" },
      { label: projectName, href: projectPath(ref) },
    ];
    if (tab !== "overview") {
      crumbs.push({ label: tabMeta?.label ?? tab, href: projectPath(ref, tabMeta?.suffix) });
    }
    const { section, item } = projectSubrouteFromPathname(pathname, tabMeta?.suffix);
    if (section && section !== "overview") {
      const subnav = projectSubnav[tab];
      const sectionLabel = subnav ? labelFor(subnav, section, section) : section;
      crumbs.push({ label: sectionLabel, href: projectPath(ref, tabMeta?.suffix, section) });
    }
    if (item) {
      crumbs.push({ label: decode(item) });
    }
    return finalize(crumbs);
  }

  if (head === "settings" && parts[1]) {
    const crumbs: Crumb[] = [
      { label: "Settings", href: "/settings" },
      { label: labelFor(platformSettingsSections, parts[1], parts[1]), href: `/settings/${parts[1]}` },
    ];
    if (parts[2]) {
      crumbs.push({ label: decode(parts[2]) });
    }
    return finalize(crumbs);
  }

  if (head === "organizations" && parts[1]) {
    const crumbs: Crumb[] = [
      { label: opts.orgsEnabled ? "Organizations" : "Access", href: "/organizations" },
      { label: labelFor(organizationSections, parts[1], parts[1]), href: `/organizations/${parts[1]}` },
    ];
    if (parts[2]) {
      crumbs.push({ label: decode(parts[2]) });
    }
    return finalize(crumbs);
  }

  if (head === "security" && parts[1]) {
    const crumbs: Crumb[] = [
      { label: "Security", href: "/security" },
      { label: labelFor(securitySections, parts[1], parts[1]), href: `/security/${parts[1]}` },
    ];
    if (parts[2]) {
      crumbs.push({ label: decode(parts[2]) });
    }
    return finalize(crumbs);
  }

  if (head === "hosts" && parts[1]) {
    return finalize([{ label: "Hosts", href: "/hosts" }, { label: decode(parts[1]) }]);
  }

  return [];
}

// Above this many crumbs, the middle collapses to an expandable ellipsis so the
// trail keeps first / … / parent / current and never overruns the header.
const MAX_VISIBLE = 4;

type CrumbNode = { kind: "crumb"; crumb: Crumb; index: number } | { kind: "ellipsis" };

export function Breadcrumbs({ crumbs }: { crumbs: Crumb[] }) {
  const [expanded, setExpanded] = useState(false);
  const collapsed = crumbs.length > MAX_VISIBLE && !expanded;

  const nodes: CrumbNode[] = collapsed
    ? [
        { kind: "crumb", crumb: crumbs[0], index: 0 },
        { kind: "ellipsis" },
        { kind: "crumb", crumb: crumbs[crumbs.length - 2], index: crumbs.length - 2 },
        { kind: "crumb", crumb: crumbs[crumbs.length - 1], index: crumbs.length - 1 },
      ]
    : crumbs.map((crumb, index) => ({ kind: "crumb", crumb, index }));

  function renderCrumb(crumb: Crumb, index: number) {
    const last = index === crumbs.length - 1;
    if (crumb.href && !last) {
      return (
        <Link className="truncate text-muted hover:text-text" to={crumb.href}>
          {crumb.label}
        </Link>
      );
    }
    return (
      <span aria-current={last ? "page" : undefined} className={`truncate ${last ? "font-medium text-text" : "text-muted"}`}>
        {crumb.label}
      </span>
    );
  }

  return (
    <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-sm">
      {nodes.map((node, position) => (
        <span className="flex min-w-0 items-center gap-1" key={node.kind === "ellipsis" ? "ellipsis" : `${node.crumb.label}-${node.index}`}>
          {position > 0 ? <ChevronRight className="shrink-0 text-faint" size={14} /> : null}
          {node.kind === "ellipsis" ? (
            <button
              aria-label="Show hidden breadcrumbs"
              className="shrink-0 rounded px-1 text-muted hover:text-text"
              onClick={() => setExpanded(true)}
              title="Show full path"
              type="button"
            >
              <MoreHorizontal size={16} />
            </button>
          ) : (
            renderCrumb(node.crumb, node.index)
          )}
        </span>
      ))}
    </nav>
  );
}
