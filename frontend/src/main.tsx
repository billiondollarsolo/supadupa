import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { App } from "./app";
import {
  AboutPage,
  AuditLogPage,
  CreateProjectPage,
  FleetDashboardPage,
  HostsPage,
  LoginPage,
  OrganizationsPage,
  ProjectAccessPage,
  ProjectActivityPage,
  ProjectBackupsPage,
  ProjectConfigPage,
  ProjectConnectPage,
  ProjectDatabasePage,
  ProjectLogsPage,
  ProjectOverviewPage,
  ProjectsListPage,
  SecurityRoutePage,
  SettingsRoutePage,
} from "./pages";
import "./styles.css";

document.title = "SUPADUPA";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: 1,
    },
  },
});

const rootRoute = createRootRoute({
  component: App,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: FleetDashboardPage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "login",
  component: LoginPage,
});

const organizationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "organizations",
  component: OrganizationsPage,
});

const organizationsSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "organizations/$section",
  component: OrganizationsPage,
});

const projectsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects",
  component: ProjectsListPage,
});

const createProjectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/new",
  component: CreateProjectPage,
});

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref",
  component: ProjectOverviewPage,
});

const projectConnectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/connect",
  component: ProjectConnectPage,
});

const projectConnectSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/connect/$section",
  component: ProjectConnectPage,
});

const projectAccessRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/access",
  component: ProjectAccessPage,
});

const projectDatabaseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/database",
  component: ProjectDatabasePage,
});

const projectDatabaseSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/database/$section",
  component: ProjectDatabasePage,
});

const projectDatabaseSectionItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/database/$section/$item",
  component: ProjectDatabasePage,
});

const projectLogsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/logs",
  component: ProjectLogsPage,
});

const projectLogsSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/logs/$section",
  component: ProjectLogsPage,
});

const projectLogsSectionItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/logs/$section/$item",
  component: ProjectLogsPage,
});

const projectConfigRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/config",
  component: ProjectConfigPage,
});

const projectConfigSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/config/$section",
  component: ProjectConfigPage,
});

const projectConfigSectionItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/config/$section/$item",
  component: ProjectConfigPage,
});

const projectBackupsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/backups",
  component: ProjectBackupsPage,
});

const projectActivityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "projects/$ref/activity",
  component: ProjectActivityPage,
});

const securityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "security",
  component: SecurityRoutePage,
});

const securitySectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "security/$section",
  component: SecurityRoutePage,
});

const hostsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "hosts",
  component: HostsPage,
});

const hostsItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "hosts/$item",
  component: HostsPage,
});

const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit",
  component: AuditLogPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "settings",
  component: SettingsRoutePage,
});

const settingsSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "settings/$section",
  component: SettingsRoutePage,
});

const settingsSectionItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "settings/$section/$item",
  component: SettingsRoutePage,
});

const aboutRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "about",
  component: AboutPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  organizationsRoute,
  organizationsSectionRoute,
  projectsRoute,
  createProjectRoute,
  projectRoute,
  projectConnectRoute,
  projectConnectSectionRoute,
  projectAccessRoute,
  projectDatabaseRoute,
  projectDatabaseSectionRoute,
  projectDatabaseSectionItemRoute,
  projectLogsRoute,
  projectLogsSectionRoute,
  projectLogsSectionItemRoute,
  projectConfigRoute,
  projectConfigSectionRoute,
  projectConfigSectionItemRoute,
  projectBackupsRoute,
  projectActivityRoute,
  securityRoute,
  securitySectionRoute,
  hostsRoute,
  hostsItemRoute,
  auditRoute,
  settingsRoute,
  settingsSectionRoute,
  settingsSectionItemRoute,
  aboutRoute,
]);

const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
