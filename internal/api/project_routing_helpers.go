package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"supadupa2026/internal/control"
)

// detachedProvisionContext returns a context for a provisioner operation invoked
// from an HTTP handler. It is detached from the request's cancellation — so a
// dropped client connection (which is reset within ~200ms once provisioning
// reconfigures the shared edge-router) cannot SIGKILL in-flight `docker`
// commands and leave a project half-mutated — while keeping a hard time ceiling.
// Request-scoped values (e.g. the actor identity used for audit) are preserved.
// The background reconciler converges project status afterward regardless.
func detachedProvisionContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Minute)
}

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
	return reconcileProjectRoutesCtx(r.Context(), store, ref)
}

// reconcileProjectRoutesCtx is the context-driven core of reconcileProjectRoutes,
// usable off the request path (e.g. background provisioning).
func reconcileProjectRoutesCtx(ctx context.Context, store control.Store, ref string) (string, error) {
	project, err := store.GetProject(ctx, ref)
	if err != nil {
		return "", err
	}
	domains, err := store.ListProjectDomains(ctx, ref)
	if err != nil {
		return "", err
	}
	networkConfig, err := store.GetProjectConfig(ctx, ref, "network")
	if err != nil {
		return "", err
	}
	cdnPolicy, err := store.GetProjectCDNPolicy(ctx, ref)
	if err != nil {
		return "", err
	}
	replicas, err := store.ListProjectReplicas(ctx, ref)
	if err != nil {
		return "", err
	}
	platformDefaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		return "", err
	}
	routes, err := store.UpsertProjectRoutes(ctx, ref, control.RoutesForProjectDomainsWithNetworkAndCDN(project, domains, networkConfig, cdnPolicy))
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
