import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { listStackReleases } from "../api";
import { useDashboardContext } from "../lib/dashboard-context";
import { CreateProjectPanel, ProjectCards, ProjectTable } from "./projects/panels";

export function ProjectsListPage() {
  const { activeRef, hosts, orgs, projectList, projects, routeToProject } = useDashboardContext();
  const navigate = useNavigate();
  const orgNamesById = new Map((orgs.data ?? []).map((org) => [org.id, org.name]));
  const hostsById = new Map((hosts.data ?? []).map((host) => [host.id, host]));
  return (
    <div className="grid gap-6">
      <ProjectCards
        projects={projectList}
        orgNamesById={orgNamesById}
        hostsById={hostsById}
        selectedRef={activeRef}
        onSelect={(ref) => routeToProject(ref)}
        onAccess={(ref) => routeToProject(ref, "auth")}
        onCreate={() => void navigate({ to: "/projects/new" })}
        loading={projects.isLoading || hosts.isLoading || orgs.isLoading}
      />
      <ProjectTable
        projects={projectList}
        orgNamesById={orgNamesById}
        hostsById={hostsById}
        selectedRef={activeRef}
        onSelect={(ref) => routeToProject(ref)}
        loading={projects.isLoading || hosts.isLoading}
      />
    </div>
  );
}

export function CreateProjectPage() {
  const { activeOrgId, hosts, onProjectCreated, orgs, platformDefaults, setSelectedOrgId } = useDashboardContext();
  const stackReleases = useQuery({ queryKey: ["stack-releases"], queryFn: listStackReleases });
  return (
    <CreateProjectPanel
      orgId={activeOrgId}
      orgs={orgs.data ?? []}
      hosts={hosts.data ?? []}
      defaults={platformDefaults.data}
      stackReleases={stackReleases.data ?? []}
      onSelectOrg={setSelectedOrgId}
      onCreated={onProjectCreated}
    />
  );
}
