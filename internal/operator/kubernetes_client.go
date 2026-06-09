package operator

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultServiceAccountCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type KubernetesClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
	// ControlNamespace, when set, is protected from DeleteNamespace as a
	// defense-in-depth guard against deleting the operator's own namespace.
	ControlNamespace string
}

func NewInClusterKubernetesClient() (*KubernetesClient, error) {
	host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
	port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
	if host == "" || port == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are required")
	}
	token, err := os.ReadFile(defaultServiceAccountTokenPath)
	if err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	transport := &http.Transport{}
	if caPEM, err := os.ReadFile(defaultServiceAccountCAPath); err == nil {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(caPEM) {
			transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
	}
	return &KubernetesClient{
		BaseURL: "https://" + host + ":" + port,
		Token:   strings.TrimSpace(string(token)),
		Client:  &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}, nil
}

type projectList struct {
	Items []Project `json:"items"`
}

type projectConfigList struct {
	Items []ProjectConfig `json:"items"`
}

type projectAuthHooksList struct {
	Items []ProjectAuthHooks `json:"items"`
}

type projectBranchCloneList struct {
	Items []ProjectBranchClone `json:"items"`
}

type projectReplicaList struct {
	Items []ProjectReplica `json:"items"`
}

type retainedProjectResourcesList struct {
	Items []RetainedProjectResources `json:"items"`
}

type kubernetesObject struct {
	Metadata ObjectMeta `json:"metadata,omitempty"`
}

type kubernetesObjectList struct {
	Items []kubernetesObject `json:"items"`
}

type observedDeploymentList struct {
	Items []observedDeployment `json:"items"`
}

type observedDeployment struct {
	Metadata ObjectMeta `json:"metadata,omitempty"`
	Spec     struct {
		Replicas int32 `json:"replicas,omitempty"`
	} `json:"spec,omitempty"`
	Status struct {
		AvailableReplicas int32 `json:"availableReplicas,omitempty"`
		ReadyReplicas     int32 `json:"readyReplicas,omitempty"`
		UpdatedReplicas   int32 `json:"updatedReplicas,omitempty"`
	} `json:"status,omitempty"`
}

type observedPersistentVolumeClaimList struct {
	Items []observedPersistentVolumeClaim `json:"items"`
}

type observedPersistentVolumeClaim struct {
	Metadata ObjectMeta `json:"metadata,omitempty"`
	Status   struct {
		Phase string `json:"phase,omitempty"`
	} `json:"status,omitempty"`
}

func (c *KubernetesClient) ListProjects(ctx context.Context, namespace string) ([]Project, error) {
	var out projectList
	if err := c.do(ctx, http.MethodGet, projectListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) ListProjectConfigs(ctx context.Context, namespace string) ([]ProjectConfig, error) {
	var out projectConfigList
	if err := c.do(ctx, http.MethodGet, projectConfigListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) ListProjectAuthHooks(ctx context.Context, namespace string) ([]ProjectAuthHooks, error) {
	var out projectAuthHooksList
	if err := c.do(ctx, http.MethodGet, projectAuthHooksListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) ListProjectBranchClones(ctx context.Context, namespace string) ([]ProjectBranchClone, error) {
	var out projectBranchCloneList
	if err := c.do(ctx, http.MethodGet, projectBranchCloneListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) ListProjectReplicas(ctx context.Context, namespace string) ([]ProjectReplica, error) {
	var out projectReplicaList
	if err := c.do(ctx, http.MethodGet, projectReplicaListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) ListRetainedProjectResources(ctx context.Context, namespace string) ([]RetainedProjectResources, error) {
	var out retainedProjectResourcesList
	if err := c.do(ctx, http.MethodGet, retainedProjectResourcesListPath(namespace), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *KubernetesClient) EnsureNamespace(ctx context.Context, name string, labels map[string]string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("namespace name is required")
	}
	return c.apply(ctx, namespacePath(name), namespaceObject(name, labels))
}

func (c *KubernetesClient) DeleteNamespace(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if control := strings.TrimSpace(c.ControlNamespace); control != "" && name == control {
		return fmt.Errorf("refusing to delete control namespace %q", name)
	}
	return c.deleteIfPresent(ctx, namespacePath(name))
}

func (c *KubernetesClient) ApplyProjectIsolation(ctx context.Context, namespace string, _ Project, iso ProjectIsolation) error {
	if name := strings.TrimSpace(iso.ServiceAccountName); name != "" {
		if err := c.apply(ctx, serviceAccountPath(namespace, name), serviceAccountObject(name, iso.Labels)); err != nil {
			return err
		}
	}
	for _, policy := range iso.NetworkPolicies {
		if strings.TrimSpace(policy.Name) == "" {
			continue
		}
		if err := c.apply(ctx, networkPolicyPath(namespace, policy.Name), networkPolicyObject(policy, iso.Labels)); err != nil {
			return err
		}
	}
	if iso.Quota != nil && strings.TrimSpace(iso.Quota.Name) != "" {
		if err := c.apply(ctx, resourceQuotaPath(namespace, iso.Quota.Name), resourceQuotaObject(*iso.Quota, iso.Labels)); err != nil {
			return err
		}
	}
	if iso.LimitRange != nil && strings.TrimSpace(iso.LimitRange.Name) != "" {
		if err := c.apply(ctx, limitRangePath(namespace, iso.LimitRange.Name), limitRangeObject(*iso.LimitRange, iso.Labels)); err != nil {
			return err
		}
	}
	// Prune stale NetworkPolicies left over from previous renders.
	selector := projectLabelSelector(iso.Labels)
	if selector != "" {
		desired := map[string]struct{}{}
		for _, policy := range iso.NetworkPolicies {
			if name := strings.TrimSpace(policy.Name); name != "" {
				desired[name] = struct{}{}
			}
		}
		if err := c.pruneObjects(ctx, networkPoliciesCollectionPath(namespace, selector), desired, networkPolicyPath(namespace, ""), false); err != nil {
			return err
		}
	}
	return nil
}

func (c *KubernetesClient) ApplyProjectResources(ctx context.Context, namespace string, project Project, resources ProjectResources) error {
	if err := c.apply(ctx, configMapPath(namespace, resources.ConfigMapName), configMapObject(resources)); err != nil {
		return err
	}
	if err := c.apply(ctx, secretPath(namespace, resources.SecretName), secretObject(resources)); err != nil {
		return err
	}
	for _, workload := range resources.Workloads {
		for _, volume := range workload.Volumes {
			if err := c.apply(ctx, persistentVolumeClaimPath(namespace, workloadPVCName(workload, volume)), persistentVolumeClaimObject(workload, volume)); err != nil {
				return err
			}
		}
		if err := c.apply(ctx, deploymentPath(namespace, workload.DeploymentName), deploymentObject(project, resources, workload)); err != nil {
			return err
		}
		if len(workload.Ports) > 0 {
			if err := c.apply(ctx, servicePath(namespace, workload.KubeServiceName), serviceObject(workload)); err != nil {
				return err
			}
		}
		if workload.Ingress != nil && len(workload.Ports) > 0 {
			if err := c.apply(ctx, ingressPath(namespace, workload.IngressName), ingressObject(workload)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *KubernetesClient) PruneProjectResources(ctx context.Context, namespace string, _ Project, resources ProjectResources) error {
	selector := projectLabelSelector(resources.Labels)
	if selector == "" {
		return nil
	}
	desiredDeployments := map[string]struct{}{}
	desiredServices := map[string]struct{}{}
	desiredPVCs := map[string]struct{}{}
	desiredIngresses := map[string]struct{}{}
	for _, workload := range resources.Workloads {
		desiredDeployments[workload.DeploymentName] = struct{}{}
		if len(workload.Ports) > 0 {
			desiredServices[workload.KubeServiceName] = struct{}{}
		}
		if workload.Ingress != nil && len(workload.Ports) > 0 {
			desiredIngresses[workload.IngressName] = struct{}{}
		}
		for _, volume := range workload.Volumes {
			desiredPVCs[workloadPVCName(workload, volume)] = struct{}{}
		}
	}
	if err := c.pruneObjects(ctx, deploymentsCollectionPath(namespace, selector), desiredDeployments, deploymentPath(namespace, ""), false); err != nil {
		return err
	}
	if err := c.pruneObjects(ctx, servicesCollectionPath(namespace, selector), desiredServices, servicePath(namespace, ""), false); err != nil {
		return err
	}
	if err := c.pruneObjects(ctx, ingressesCollectionPath(namespace, selector), desiredIngresses, ingressPath(namespace, ""), false); err != nil {
		return err
	}
	return c.pruneObjects(ctx, persistentVolumeClaimsCollectionPath(namespace, selector), desiredPVCs, persistentVolumeClaimPath(namespace, ""), true)
}

func (c *KubernetesClient) ObserveProjectResources(ctx context.Context, namespace string, resources ProjectResources) (ProjectResourceObservation, error) {
	if len(resources.Workloads) == 0 {
		return ProjectResourceObservation{Ready: true, Message: "no workload resources to observe"}, nil
	}
	selector := projectLabelSelector(resources.Labels)
	if selector == "" {
		return ProjectResourceObservation{Checked: true, Ready: false, Message: "project label selector is incomplete"}, nil
	}

	var deploymentList observedDeploymentList
	if err := c.do(ctx, http.MethodGet, deploymentsCollectionPath(namespace, selector), nil, &deploymentList); err != nil {
		return ProjectResourceObservation{}, err
	}
	deployments := map[string]observedDeployment{}
	for _, deployment := range deploymentList.Items {
		if name := strings.TrimSpace(deployment.Metadata.Name); name != "" {
			deployments[name] = deployment
		}
	}

	issues := []string{}
	for _, workload := range resources.Workloads {
		deployment, ok := deployments[workload.DeploymentName]
		if !ok {
			issues = append(issues, fmt.Sprintf("deployment/%s is missing", workload.DeploymentName))
			continue
		}
		desiredReplicas := workload.Replicas
		if deployment.Spec.Replicas > desiredReplicas {
			desiredReplicas = deployment.Spec.Replicas
		}
		if deployment.Status.AvailableReplicas < desiredReplicas {
			issues = append(issues, fmt.Sprintf("deployment/%s available %d/%d", workload.DeploymentName, deployment.Status.AvailableReplicas, desiredReplicas))
		}
	}

	desiredPVCs := []string{}
	for _, workload := range resources.Workloads {
		for _, volume := range workload.Volumes {
			desiredPVCs = append(desiredPVCs, workloadPVCName(workload, volume))
		}
	}
	if len(desiredPVCs) > 0 {
		var pvcList observedPersistentVolumeClaimList
		if err := c.do(ctx, http.MethodGet, persistentVolumeClaimsCollectionPath(namespace, selector), nil, &pvcList); err != nil {
			return ProjectResourceObservation{}, err
		}
		pvcs := map[string]observedPersistentVolumeClaim{}
		for _, pvc := range pvcList.Items {
			if name := strings.TrimSpace(pvc.Metadata.Name); name != "" {
				pvcs[name] = pvc
			}
		}
		for _, name := range desiredPVCs {
			pvc, ok := pvcs[name]
			if !ok {
				issues = append(issues, fmt.Sprintf("persistentvolumeclaim/%s is missing", name))
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(pvc.Status.Phase), "Bound") {
				phase := strings.TrimSpace(pvc.Status.Phase)
				if phase == "" {
					phase = "unknown"
				}
				issues = append(issues, fmt.Sprintf("persistentvolumeclaim/%s phase %s", name, phase))
			}
		}
	}

	if len(issues) > 0 {
		return ProjectResourceObservation{Checked: true, Ready: false, Message: strings.Join(issues, "; ")}, nil
	}
	return ProjectResourceObservation{Checked: true, Ready: true, Message: "all workload deployments are available and persistent volume claims are bound"}, nil
}

func (c *KubernetesClient) DeleteProjectResources(ctx context.Context, namespace string, _ Project, resources ProjectResources) error {
	retainedPVCs := map[string]struct{}{}
	for index := len(resources.Workloads) - 1; index >= 0; index-- {
		workload := resources.Workloads[index]
		if workload.Ingress != nil {
			if err := c.deleteIfPresent(ctx, ingressPath(namespace, workload.IngressName)); err != nil {
				return err
			}
		}
		if err := c.deleteIfPresent(ctx, servicePath(namespace, workload.KubeServiceName)); err != nil {
			return err
		}
		if err := c.deleteIfPresent(ctx, deploymentPath(namespace, workload.DeploymentName)); err != nil {
			return err
		}
		for _, volume := range workload.Volumes {
			if volume.Retain {
				retainedPVCs[workloadPVCName(workload, volume)] = struct{}{}
				continue
			}
			if err := c.deleteIfPresent(ctx, persistentVolumeClaimPath(namespace, workloadPVCName(workload, volume))); err != nil {
				return err
			}
		}
	}
	if err := c.deleteManagedWorkloadResources(ctx, namespace, resources, retainedPVCs); err != nil {
		return err
	}
	if err := c.deleteIfPresent(ctx, configMapPath(namespace, resources.ConfigMapName)); err != nil {
		return err
	}
	return c.deleteIfPresent(ctx, secretPath(namespace, resources.SecretName))
}

func (c *KubernetesClient) deleteManagedWorkloadResources(ctx context.Context, namespace string, resources ProjectResources, retainedPVCs map[string]struct{}) error {
	selector := projectLabelSelector(resources.Labels)
	if selector == "" {
		return nil
	}
	desired := map[string]struct{}{}
	if err := c.pruneObjects(ctx, deploymentsCollectionPath(namespace, selector), desired, deploymentPath(namespace, ""), false); err != nil {
		return err
	}
	if err := c.pruneObjects(ctx, servicesCollectionPath(namespace, selector), desired, servicePath(namespace, ""), false); err != nil {
		return err
	}
	if err := c.pruneObjects(ctx, ingressesCollectionPath(namespace, selector), desired, ingressPath(namespace, ""), false); err != nil {
		return err
	}
	return c.pruneObjects(ctx, persistentVolumeClaimsCollectionPath(namespace, selector), retainedPVCs, persistentVolumeClaimPath(namespace, ""), true)
}

func (c *KubernetesClient) pruneObjects(ctx context.Context, listPath string, desired map[string]struct{}, itemPathPrefix string, preserveRetainedPVCs bool) error {
	var list kubernetesObjectList
	if err := c.do(ctx, http.MethodGet, listPath, nil, &list); err != nil {
		return err
	}
	for _, item := range list.Items {
		name := strings.TrimSpace(item.Metadata.Name)
		if name == "" {
			continue
		}
		if _, ok := desired[name]; ok {
			continue
		}
		if preserveRetainedPVCs && strings.EqualFold(item.Metadata.Labels["supadupa.dev/retain"], "true") {
			continue
		}
		if err := c.deleteIfPresent(ctx, itemPathPrefix+escapeKubernetesPathSegment(name)); err != nil {
			return err
		}
	}
	return nil
}

func (c *KubernetesClient) SetProjectFinalizers(ctx context.Context, namespace string, name string, finalizers []string) error {
	// A nil slice must clear finalizers (merge-patch treats null as delete), so
	// normalise to an empty slice when removing the last finalizer.
	if finalizers == nil {
		finalizers = []string{}
	}
	payload := map[string]any{"metadata": map[string]any{"finalizers": finalizers}}
	return c.do(ctx, http.MethodPatch, projectPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchProjectStatus(ctx context.Context, namespace string, name string, status ProjectStatus) error {
	payload := map[string]ProjectStatus{"status": status}
	return c.do(ctx, http.MethodPatch, projectStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchProjectConfigStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error {
	payload := map[string]ResourceStatus{"status": status}
	return c.do(ctx, http.MethodPatch, projectConfigStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchProjectAuthHooksStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error {
	payload := map[string]ResourceStatus{"status": status}
	return c.do(ctx, http.MethodPatch, projectAuthHooksStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchProjectBranchCloneStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error {
	payload := map[string]ResourceStatus{"status": status}
	return c.do(ctx, http.MethodPatch, projectBranchCloneStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchProjectReplicaStatus(ctx context.Context, namespace string, name string, status ReplicaStatus) error {
	payload := map[string]ReplicaStatus{"status": status}
	return c.do(ctx, http.MethodPatch, projectReplicaStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) PatchRetainedProjectResourcesStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error {
	payload := map[string]ResourceStatus{"status": status}
	return c.do(ctx, http.MethodPatch, retainedProjectResourcesStatusPath(namespace, name), payload, nil)
}

func (c *KubernetesClient) apply(ctx context.Context, path string, body any) error {
	return c.doWithContentType(ctx, http.MethodPatch, path+"?fieldManager=supadupa-operator&force=true", "application/apply-patch+yaml", body, nil, nil)
}

func (c *KubernetesClient) deleteIfPresent(ctx context.Context, path string) error {
	var status int
	if err := c.doWithContentType(ctx, http.MethodDelete, path, "", nil, nil, &status); err != nil {
		if status == http.StatusNotFound {
			return nil
		}
		return err
	}
	return nil
}

func (c *KubernetesClient) do(ctx context.Context, method string, path string, body any, out any) error {
	return c.doWithContentType(ctx, method, path, "application/merge-patch+json", body, out, nil)
}

func (c *KubernetesClient) doWithContentType(ctx context.Context, method string, path string, contentType string, body any, out any, statusOut *int) error {
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if statusOut != nil {
		*statusOut = response.StatusCode
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("kubernetes API %s %s returned HTTP %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode kubernetes API response: %w", err)
	}
	return nil
}

// Lease mirrors the subset of coordination.k8s.io/v1 Lease the operator needs
// for leader election.
type Lease struct {
	Metadata ObjectMeta `json:"metadata,omitempty"`
	Spec     LeaseSpec  `json:"spec,omitempty"`
}

type LeaseSpec struct {
	HolderIdentity       string `json:"holderIdentity,omitempty"`
	LeaseDurationSeconds int    `json:"leaseDurationSeconds,omitempty"`
	AcquireTime          string `json:"acquireTime,omitempty"`
	RenewTime            string `json:"renewTime,omitempty"`
	LeaseTransitions     int    `json:"leaseTransitions,omitempty"`
}

// GetLease fetches a Lease, returning (nil, nil) when it does not exist.
func (c *KubernetesClient) GetLease(ctx context.Context, namespace string, name string) (*Lease, error) {
	var lease Lease
	var status int
	if err := c.doWithContentType(ctx, http.MethodGet, leasePath(namespace, name), "", nil, &lease, &status); err != nil {
		if status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &lease, nil
}

// CreateLease creates a Lease, returning an error (with conflict detectable by
// the caller via status) if it already exists.
func (c *KubernetesClient) CreateLease(ctx context.Context, namespace string, lease Lease) (int, error) {
	lease.Metadata.Namespace = namespace
	body := map[string]any{
		"apiVersion": "coordination.k8s.io/v1",
		"kind":       "Lease",
		"metadata":   map[string]any{"name": lease.Metadata.Name, "namespace": namespace},
		"spec":       leaseSpecObject(lease.Spec),
	}
	var status int
	err := c.doWithContentType(ctx, http.MethodPost, leaseCollectionPath(namespace), "application/json", body, nil, &status)
	return status, err
}

// UpdateLease replaces a Lease's spec via merge patch.
func (c *KubernetesClient) UpdateLease(ctx context.Context, namespace string, lease Lease) error {
	body := map[string]any{"spec": leaseSpecObject(lease.Spec)}
	return c.do(ctx, http.MethodPatch, leasePath(namespace, lease.Metadata.Name), body, nil)
}

func leaseSpecObject(spec LeaseSpec) map[string]any {
	out := map[string]any{
		"holderIdentity":       spec.HolderIdentity,
		"leaseDurationSeconds": spec.LeaseDurationSeconds,
		"renewTime":            spec.RenewTime,
		"leaseTransitions":     spec.LeaseTransitions,
	}
	if spec.AcquireTime != "" {
		out["acquireTime"] = spec.AcquireTime
	}
	return out
}

func leasePath(namespace string, name string) string {
	return "/apis/coordination.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/leases/" + escapeKubernetesPathSegment(name)
}

func leaseCollectionPath(namespace string) string {
	return "/apis/coordination.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/leases"
}

func namespaceObject(name string, labels map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   name,
			"labels": labels,
		},
	}
}

func serviceAccountObject(name string, labels map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":   name,
			"labels": labels,
		},
		"automountServiceAccountToken": false,
	}
}

func networkPolicyObject(policy NetworkPolicySpec, labels map[string]string) map[string]any {
	spec := map[string]any{
		"podSelector": map[string]any{"matchLabels": policy.PodSelector},
	}
	if len(policy.PolicyTypes) > 0 {
		spec["policyTypes"] = policy.PolicyTypes
	}
	if rules := networkPolicyRules(policy.Ingress, "from"); rules != nil {
		spec["ingress"] = rules
	}
	if rules := networkPolicyRules(policy.Egress, "to"); rules != nil {
		spec["egress"] = rules
	}
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":   policy.Name,
			"labels": labels,
		},
		"spec": spec,
	}
}

// networkPolicyRules renders a list of ingress/egress rules. peerKey is "from"
// for ingress rules and "to" for egress rules per the NetworkPolicy schema.
func networkPolicyRules(rules []NetworkPolicyRule, peerKey string) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		entry := map[string]any{}
		if peers := networkPolicyPeers(rule.Peers); len(peers) > 0 {
			entry[peerKey] = peers
		}
		if ports := networkPolicyPorts(rule.Ports); len(ports) > 0 {
			entry["ports"] = ports
		}
		out = append(out, entry)
	}
	return out
}

func networkPolicyPeers(peers []NetworkPolicyPeer) []map[string]any {
	if len(peers) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		entry := map[string]any{}
		if len(peer.PodSelector) > 0 {
			entry["podSelector"] = map[string]any{"matchLabels": peer.PodSelector}
		}
		if len(peer.NamespaceSelector) > 0 {
			entry["namespaceSelector"] = map[string]any{"matchLabels": peer.NamespaceSelector}
		}
		if cidr := strings.TrimSpace(peer.CIDR); cidr != "" {
			ipBlock := map[string]any{"cidr": cidr}
			if len(peer.Except) > 0 {
				ipBlock["except"] = peer.Except
			}
			entry["ipBlock"] = ipBlock
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

func networkPolicyPorts(ports []NetworkPolicyPort) []map[string]any {
	if len(ports) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		protocol := strings.ToUpper(strings.TrimSpace(port.Protocol))
		if protocol == "" {
			protocol = "TCP"
		}
		out = append(out, map[string]any{"protocol": protocol, "port": port.Port})
	}
	return out
}

func resourceQuotaObject(quota ResourceQuotaSpec, labels map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ResourceQuota",
		"metadata": map[string]any{
			"name":   quota.Name,
			"labels": labels,
		},
		"spec": map[string]any{
			"hard": quota.Hard,
		},
	}
}

func limitRangeObject(limit LimitRangeSpec, labels map[string]string) map[string]any {
	item := map[string]any{"type": "Container"}
	if len(limit.Default) > 0 {
		item["default"] = limit.Default
	}
	if len(limit.DefaultRequest) > 0 {
		item["defaultRequest"] = limit.DefaultRequest
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "LimitRange",
		"metadata": map[string]any{
			"name":   limit.Name,
			"labels": labels,
		},
		"spec": map[string]any{
			"limits": []map[string]any{item},
		},
	}
}

func configMapObject(resources ProjectResources) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":   resources.ConfigMapName,
			"labels": resources.Labels,
		},
		"data": resources.ConfigData,
	}
}

func secretObject(resources ProjectResources) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":   resources.SecretName,
			"labels": resources.Labels,
		},
		"type":       "Opaque",
		"stringData": resources.SecretData,
	}
}

func deploymentObject(project Project, resources ProjectResources, workload ServiceResources) map[string]any {
	container := map[string]any{
		"name":            workload.ServiceName,
		"image":           serviceImage(workload.Spec),
		"imagePullPolicy": "IfNotPresent",
		"envFrom": []map[string]any{
			{"secretRef": map[string]any{"name": resources.SecretName}},
		},
		"securityContext": containerSecurityContext(project.Spec.RuntimeSecurityDefaults, workload.Spec.AllowPrivilegeEscalation, workload.Spec.DropCapabilities),
	}
	if workload.Spec.ReadOnlyRootFilesystem {
		container["securityContext"].(map[string]any)["readOnlyRootFilesystem"] = true
	}
	if command := cleanStringList(workload.Spec.Command); len(command) > 0 {
		container["command"] = command
	}
	if args := cleanStringList(workload.Spec.Args); len(args) > 0 {
		container["args"] = args
	}
	if probe := probeObject(workload.Spec.ReadinessProbe); probe != nil {
		container["readinessProbe"] = probe
	}
	if probe := probeObject(workload.Spec.LivenessProbe); probe != nil {
		container["livenessProbe"] = probe
	}
	if len(workload.Ports) > 0 {
		ports := make([]map[string]any, 0, len(workload.Ports))
		for _, port := range workload.Ports {
			ports = append(ports, map[string]any{
				"name":          port.Name,
				"containerPort": port.TargetPort,
				"protocol":      port.Protocol,
			})
		}
		container["ports"] = ports
	}
	if len(workload.Spec.Env) > 0 {
		env := make([]map[string]any, 0, len(workload.Spec.Env))
		keys := make([]string, 0, len(workload.Spec.Env))
		for key := range workload.Spec.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := workload.Spec.Env[key]
			env = append(env, map[string]any{"name": key, "value": value})
		}
		container["env"] = env
	}
	if len(workload.Volumes) > 0 || len(workload.ConfigFiles) > 0 || len(workload.WritablePaths) > 0 {
		mounts := make([]map[string]any, 0, len(workload.Volumes)+len(workload.ConfigFiles)+len(workload.WritablePaths))
		for _, volume := range workload.Volumes {
			mounts = append(mounts, map[string]any{"name": volume.Name, "mountPath": volume.MountPath})
		}
		for _, file := range workload.ConfigFiles {
			mounts = append(mounts, map[string]any{
				"name":      configFileVolumeName(file),
				"mountPath": file.MountPath,
				"subPath":   file.DataKey,
				"readOnly":  true,
			})
		}
		for _, writable := range workload.WritablePaths {
			mounts = append(mounts, map[string]any{"name": writableVolumeName(writable), "mountPath": writable.MountPath})
		}
		container["volumeMounts"] = mounts
	}
	podSpec := map[string]any{
		"automountServiceAccountToken": false,
		"securityContext":              podSecurityContext(project.Spec.RuntimeSecurityDefaults, workload.Spec.RunAsNonRoot),
		"containers":                   []map[string]any{container},
	}
	if sa := strings.TrimSpace(resources.ServiceAccountName); sa != "" {
		podSpec["serviceAccountName"] = sa
	}
	if initContainers := dependencyInitContainers(project, workload); len(initContainers) > 0 {
		podSpec["initContainers"] = initContainers
	}
	if len(workload.Volumes) > 0 || len(workload.ConfigFiles) > 0 || len(workload.WritablePaths) > 0 {
		volumes := make([]map[string]any, 0, len(workload.Volumes)+len(workload.ConfigFiles)+len(workload.WritablePaths))
		for _, volume := range workload.Volumes {
			volumes = append(volumes, map[string]any{
				"name": volume.Name,
				"persistentVolumeClaim": map[string]any{
					"claimName": workloadPVCName(workload, volume),
				},
			})
		}
		for _, file := range workload.ConfigFiles {
			volumes = append(volumes, map[string]any{
				"name": configFileVolumeName(file),
				"configMap": map[string]any{
					"name": resources.ConfigMapName,
					"items": []map[string]any{
						{"key": file.DataKey, "path": file.DataKey},
					},
				},
			})
		}
		for _, writable := range workload.WritablePaths {
			volumes = append(volumes, map[string]any{
				"name":     writableVolumeName(writable),
				"emptyDir": map[string]any{},
			})
		}
		podSpec["volumes"] = volumes
	}
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":   workload.DeploymentName,
			"labels": workload.Labels,
		},
		"spec": map[string]any{
			"replicas": workload.Replicas,
			"selector": map[string]any{"matchLabels": workloadSelectorLabels(workload)},
			"template": map[string]any{
				"metadata": map[string]any{"labels": workload.Labels},
				"spec":     podSpec,
			},
		},
	}
}

func serviceObject(workload ServiceResources) map[string]any {
	ports := make([]map[string]any, 0, len(workload.Ports))
	for _, port := range workload.Ports {
		ports = append(ports, map[string]any{
			"name":       port.Name,
			"port":       port.Port,
			"targetPort": port.TargetPort,
			"protocol":   port.Protocol,
		})
	}
	serviceType := strings.TrimSpace(workload.Spec.ServiceType)
	if serviceType == "" {
		serviceType = strings.TrimSpace(workload.Spec.Config["serviceType"])
	}
	if serviceType == "" {
		serviceType = "ClusterIP"
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":   workload.KubeServiceName,
			"labels": workload.Labels,
		},
		"spec": map[string]any{
			"type":     serviceType,
			"selector": workloadSelectorLabels(workload),
			"ports":    ports,
		},
	}
}

func persistentVolumeClaimObject(workload ServiceResources, volume ServiceVolumeSpec) map[string]any {
	labels := copyStringMap(workload.Labels)
	if volume.Retain {
		labels["supadupa.dev/retain"] = "true"
	}
	spec := map[string]any{
		"accessModes": []string{"ReadWriteOnce"},
		"resources": map[string]any{
			"requests": map[string]string{"storage": volume.Size},
		},
	}
	if volume.StorageClassName != "" {
		spec["storageClassName"] = volume.StorageClassName
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":   workloadPVCName(workload, volume),
			"labels": labels,
		},
		"spec": spec,
	}
}

func ingressObject(workload ServiceResources) map[string]any {
	path := workload.Ingress.Path
	if path == "" {
		path = "/"
	}
	spec := map[string]any{
		"rules": []map[string]any{
			{
				"host": workload.Ingress.Host,
				"http": map[string]any{
					"paths": []map[string]any{
						{
							"path":     path,
							"pathType": "Prefix",
							"backend": map[string]any{
								"service": map[string]any{
									"name": workload.KubeServiceName,
									"port": map[string]any{"number": workload.Ports[0].Port},
								},
							},
						},
					},
				},
			},
		},
	}
	if workload.Ingress.ClassName != "" {
		spec["ingressClassName"] = workload.Ingress.ClassName
	}
	if workload.Ingress.TLSSecretName != "" {
		spec["tls"] = []map[string]any{{
			"hosts":      []string{workload.Ingress.Host},
			"secretName": workload.Ingress.TLSSecretName,
		}}
	}
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "Ingress",
		"metadata": map[string]any{
			"name":        workload.IngressName,
			"labels":      workload.Labels,
			"annotations": workload.Ingress.Annotations,
		},
		"spec": spec,
	}
}

func workloadSelectorLabels(workload ServiceResources) map[string]string {
	return map[string]string{
		"supadupa.dev/project-ref":     workload.Labels["supadupa.dev/project-ref"],
		"supadupa.dev/project-service": workload.ServiceName,
	}
}

func podSecurityContext(defaults RuntimeSecurityDefaults, runAsNonRootOverride *bool) map[string]any {
	securityContext := map[string]any{
		"runAsNonRoot": true,
	}
	if runAsNonRootOverride != nil {
		securityContext["runAsNonRoot"] = *runAsNonRootOverride
	}
	if defaults.SeccompProfile != "" {
		securityContext["seccompProfile"] = map[string]any{"type": defaults.SeccompProfile}
	}
	return securityContext
}

func containerSecurityContext(defaults RuntimeSecurityDefaults, allowPrivilegeEscalationOverride *bool, dropCapabilitiesOverride []string) map[string]any {
	allowPrivilegeEscalation := false
	if defaults.AllowPrivilegeEscalation != nil {
		allowPrivilegeEscalation = *defaults.AllowPrivilegeEscalation
	}
	if allowPrivilegeEscalationOverride != nil {
		allowPrivilegeEscalation = *allowPrivilegeEscalationOverride
	}
	dropCapabilities := defaults.DropCapabilities
	if len(dropCapabilitiesOverride) > 0 {
		dropCapabilities = dropCapabilitiesOverride
	}
	if len(dropCapabilities) == 0 {
		dropCapabilities = []string{"ALL"}
	}
	return map[string]any{
		"allowPrivilegeEscalation": allowPrivilegeEscalation,
		"capabilities":             map[string]any{"drop": dropCapabilities},
	}
}

func probeObject(spec *ServiceProbeSpec) map[string]any {
	if spec == nil || spec.Port <= 0 || spec.Port > 65535 {
		return nil
	}
	probeType := strings.ToLower(strings.TrimSpace(spec.Type))
	if probeType == "" {
		probeType = "tcp"
	}
	probe := map[string]any{}
	switch probeType {
	case "http", "httpget", "http-get":
		path := strings.TrimSpace(spec.Path)
		if path == "" {
			path = "/"
		}
		probe["httpGet"] = map[string]any{"path": path, "port": spec.Port}
	case "tcp", "tcpsocket", "tcp-socket":
		probe["tcpSocket"] = map[string]any{"port": spec.Port}
	default:
		return nil
	}
	if spec.InitialDelaySeconds > 0 {
		probe["initialDelaySeconds"] = spec.InitialDelaySeconds
	}
	if spec.PeriodSeconds > 0 {
		probe["periodSeconds"] = spec.PeriodSeconds
	}
	if spec.TimeoutSeconds > 0 {
		probe["timeoutSeconds"] = spec.TimeoutSeconds
	}
	if spec.FailureThreshold > 0 {
		probe["failureThreshold"] = spec.FailureThreshold
	}
	return probe
}

func cleanStringList(input []string) []string {
	out := make([]string, 0, len(input))
	for _, value := range input {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func dependencyInitContainers(project Project, workload ServiceResources) []map[string]any {
	if len(workload.Dependencies) == 0 {
		return nil
	}
	projectRef := strings.TrimSpace(project.Spec.Ref)
	if projectRef == "" {
		projectRef = strings.TrimSpace(project.Metadata.Name)
	}
	out := make([]map[string]any, 0, len(workload.Dependencies))
	for _, dependency := range workload.Dependencies {
		service := kubernetesName(dependency.Service)
		if service == "" || dependency.Port <= 0 || dependency.Port > 65535 {
			continue
		}
		kubeServiceName := kubernetesJoinedName(projectRef, service)
		out = append(out, map[string]any{
			"name":            kubernetesJoinedName("wait", service),
			"image":           "busybox:1.37.0",
			"imagePullPolicy": "IfNotPresent",
			"command": []string{
				"sh",
				"-c",
				fmt.Sprintf("until nc -z %s %d; do sleep 2; done", kubeServiceName, dependency.Port),
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"readOnlyRootFilesystem":   true,
				"runAsNonRoot":             true,
				"runAsUser":                int64(65534),
				"runAsGroup":               int64(65534),
				"capabilities":             map[string]any{"drop": []string{"ALL"}},
			},
		})
	}
	return out
}

func writableVolumeName(writable ServiceWritableSpec) string {
	name := kubernetesName(writable.Name)
	if name == "" || name == "project" {
		name = "writable"
	}
	return kubernetesJoinedName("writable", name)
}

func configFileVolumeName(file ServiceConfigFileResources) string {
	name := kubernetesName(file.Name)
	if name == "" || name == "project" {
		name = "config-file"
	}
	return kubernetesJoinedName("config", name)
}

func workloadPVCName(workload ServiceResources, volume ServiceVolumeSpec) string {
	return kubernetesJoinedName(workload.DeploymentName, volume.Name)
}

func projectListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/projects"
}

func projectPath(namespace string, name string) string {
	return projectListPath(namespace) + "/" + escapeKubernetesPathSegment(name)
}

func projectStatusPath(namespace string, name string) string {
	return projectListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func projectConfigListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/projectconfigs"
}

func projectConfigStatusPath(namespace string, name string) string {
	return projectConfigListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func projectAuthHooksListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/projectauthhooks"
}

func projectAuthHooksStatusPath(namespace string, name string) string {
	return projectAuthHooksListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func projectBranchCloneListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/projectbranchclones"
}

func projectBranchCloneStatusPath(namespace string, name string) string {
	return projectBranchCloneListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func projectReplicaListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/projectreplicas"
}

func projectReplicaStatusPath(namespace string, name string) string {
	return projectReplicaListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func retainedProjectResourcesListPath(namespace string) string {
	return "/apis/platform.supadupa.dev/v1alpha1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/retainedprojectresources"
}

func retainedProjectResourcesStatusPath(namespace string, name string) string {
	return retainedProjectResourcesListPath(namespace) + "/" + escapeKubernetesPathSegment(name) + "/status"
}

func projectLabelSelector(labels map[string]string) string {
	managedBy := strings.TrimSpace(labels["app.kubernetes.io/managed-by"])
	projectRef := strings.TrimSpace(labels["supadupa.dev/project-ref"])
	if managedBy == "" || projectRef == "" {
		return ""
	}
	return url.QueryEscape("app.kubernetes.io/managed-by=" + managedBy + ",supadupa.dev/project-ref=" + projectRef)
}

func namespacePath(name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(name)
}

func serviceAccountPath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/serviceaccounts/" + escapeKubernetesPathSegment(name)
}

func resourceQuotaPath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/resourcequotas/" + escapeKubernetesPathSegment(name)
}

func limitRangePath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/limitranges/" + escapeKubernetesPathSegment(name)
}

func networkPolicyPath(namespace string, name string) string {
	return "/apis/networking.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/networkpolicies/" + escapeKubernetesPathSegment(name)
}

func networkPoliciesCollectionPath(namespace string, selector string) string {
	return "/apis/networking.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/networkpolicies?labelSelector=" + selector
}

func configMapPath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/configmaps/" + escapeKubernetesPathSegment(name)
}

func secretPath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/secrets/" + escapeKubernetesPathSegment(name)
}

func deploymentPath(namespace string, name string) string {
	return "/apis/apps/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/deployments/" + escapeKubernetesPathSegment(name)
}

func deploymentsCollectionPath(namespace string, selector string) string {
	return "/apis/apps/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/deployments?labelSelector=" + selector
}

func servicePath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/services/" + escapeKubernetesPathSegment(name)
}

func servicesCollectionPath(namespace string, selector string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/services?labelSelector=" + selector
}

func persistentVolumeClaimPath(namespace string, name string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/persistentvolumeclaims/" + escapeKubernetesPathSegment(name)
}

func persistentVolumeClaimsCollectionPath(namespace string, selector string) string {
	return "/api/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/persistentvolumeclaims?labelSelector=" + selector
}

func ingressPath(namespace string, name string) string {
	return "/apis/networking.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/ingresses/" + escapeKubernetesPathSegment(name)
}

func ingressesCollectionPath(namespace string, selector string) string {
	return "/apis/networking.k8s.io/v1/namespaces/" + escapeKubernetesPathSegment(namespace) + "/ingresses?labelSelector=" + selector
}

func escapeKubernetesPathSegment(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "/", "%2F")
}
