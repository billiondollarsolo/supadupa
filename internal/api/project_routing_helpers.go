package api

import (
	"fmt"
	"net/http"

	"supadupa2026/internal/control"
)

func cfgProvisionerName(provisioner control.Provisioner) string {
	if provisioner == nil {
		return "unconfigured"
	}
	return provisioner.Name()
}

func serviceAuditMetadata(services map[string]control.ServiceSpec) map[string]string {
	metadata := map[string]string{}
	states := control.ProjectServiceStates(services)
	for _, service := range control.AllowedProjectServices() {
		metadata[service] = fmt.Sprintf("%t", states[service])
	}
	return metadata
}

func reconcileProjectRoutes(r *http.Request, store control.Store, ref string) (string, error) {
	project, err := store.GetProject(r.Context(), ref)
	if err != nil {
		return "", err
	}
	domains, err := store.ListProjectDomains(r.Context(), ref)
	if err != nil {
		return "", err
	}
	networkConfig, err := store.GetProjectConfig(r.Context(), ref, "network")
	if err != nil {
		return "", err
	}
	cdnPolicy, err := store.GetProjectCDNPolicy(r.Context(), ref)
	if err != nil {
		return "", err
	}
	replicas, err := store.ListProjectReplicas(r.Context(), ref)
	if err != nil {
		return "", err
	}
	platformDefaults, err := store.GetPlatformDefaults(r.Context())
	if err != nil {
		return "", err
	}
	routes, err := store.UpsertProjectRoutes(r.Context(), ref, control.RoutesForProjectDomainsWithNetworkAndCDN(project, domains, networkConfig, cdnPolicy))
	if err != nil {
		return "", err
	}
	networkConfig = control.ApplyDatabaseExternalAccessGate(networkConfig, platformDefaults)
	tcpRoutes := control.TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress(project, networkConfig, replicas, platformDefaults.DatabaseIngressAllowedCIDRs)
	return control.NewRoutingService("").RenderProjectWithTCPRoutes(project, routes, tcpRoutes)
}

func reconcileAllProjectRoutes(r *http.Request, store control.Store) ([]string, error) {
	projects, err := store.ListProjects(r.Context())
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(projects))
	for _, project := range projects {
		path, err := reconcileProjectRoutes(r, store, project.Ref)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}
