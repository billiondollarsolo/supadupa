package operator

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

// ProjectIsolation describes the per-project namespace-scoped isolation objects
// (ServiceAccount, NetworkPolicies, ResourceQuota, LimitRange) the operator
// applies into a project's runtime namespace when isolation is enabled.
type ProjectIsolation struct {
	Labels             map[string]string
	ServiceAccountName string
	NetworkPolicies    []NetworkPolicySpec
	Quota              *ResourceQuotaSpec
	LimitRange         *LimitRangeSpec
}

// NetworkPolicySpec is a CNI-agnostic description of a networking.k8s.io/v1
// NetworkPolicy rendered for a project.
type NetworkPolicySpec struct {
	Name        string
	PodSelector map[string]string
	PolicyTypes []string
	Ingress     []NetworkPolicyRule
	Egress      []NetworkPolicyRule
}

// NetworkPolicyRule is a single ingress/egress rule (peers + ports).
type NetworkPolicyRule struct {
	Peers []NetworkPolicyPeer
	Ports []NetworkPolicyPort
}

// NetworkPolicyPeer mirrors the subset of NetworkPolicyPeer the operator needs.
type NetworkPolicyPeer struct {
	PodSelector       map[string]string
	NamespaceSelector map[string]string
	CIDR              string
	// Except lists CIDRs carved out of CIDR (ipBlock.except). Used to exclude
	// in-cluster / link-local ranges from a broad external egress allowance.
	Except []string
}

// NetworkPolicyPort mirrors a single NetworkPolicyPort (protocol + port).
type NetworkPolicyPort struct {
	Protocol string
	Port     int
}

// ResourceQuotaSpec captures the hard limits for a project ResourceQuota.
type ResourceQuotaSpec struct {
	Name string
	Hard map[string]string
}

// LimitRangeSpec captures container default/defaultRequest limits.
type LimitRangeSpec struct {
	Name           string
	Default        map[string]string
	DefaultRequest map[string]string
}

// ProjectQuotaDefaults are the platform-wide ResourceQuota hard limits.
type ProjectQuotaDefaults struct {
	Hard map[string]string
}

// ProjectLimitDefaults are the platform-wide LimitRange container defaults.
type ProjectLimitDefaults struct {
	Default        map[string]string
	DefaultRequest map[string]string
}

// IsolationConfig carries operator-level configuration into the pure builder.
type IsolationConfig struct {
	// NetworkPolicyEnabled gates whether the per-project NetworkPolicy set is
	// rendered at all. When false no NetworkPolicies are produced.
	NetworkPolicyEnabled               bool
	IngressControllerNamespaceSelector map[string]string
	IngressControllerNamespace         string
	DNSNamespace                       string
	// ExtraEgressCIDRs are platform-wide external egress destinations applied to
	// every project, merged with per-project allowedEgressCidrs.
	ExtraEgressCIDRs []string
	DefaultQuota     *ProjectQuotaDefaults
	DefaultLimits    *ProjectLimitDefaults
}

// PodSecurityLevels captures the PSA enforce/audit/warn levels and versions
// stamped on a runtime namespace.
type PodSecurityLevels struct {
	Enforce        string
	Audit          string
	Warn           string
	EnforceVersion string
	AuditVersion   string
	WarnVersion    string
}

// metadataEgressExcept lists in-cluster / link-local ranges that must never be
// reachable via an external egress CIDR. 169.254.169.254/32 is the cloud
// metadata endpoint (SSRF target); 169.254.0.0/16 is link-local generally.
var metadataEgressExcept = []string{"169.254.0.0/16"}

const (
	labelProjectRef = "supadupa.dev/project-ref"
	labelManagedBy  = "app.kubernetes.io/managed-by"
)

// isolationForProject renders the full ProjectIsolation object set for a project.
// name is the kubernetesName(ref) base used to prefix isolation resource names.
// It is a pure function (no I/O) for straightforward unit testing.
func isolationForProject(project Project, name string, cfg IsolationConfig) ProjectIsolation {
	ref := strings.TrimSpace(project.Spec.Ref)
	if ref == "" {
		ref = strings.TrimSpace(project.Metadata.Name)
	}

	labels := map[string]string{
		labelManagedBy:  "supadupa-operator",
		labelProjectRef: ref,
	}

	projectSelector := map[string]string{labelProjectRef: ref}

	iso := ProjectIsolation{
		Labels:             labels,
		ServiceAccountName: name + "-runtime",
	}

	if cfg.NetworkPolicyEnabled {
		iso.NetworkPolicies = networkPoliciesForProject(project, name, projectSelector, cfg)
	}

	if cfg.DefaultQuota != nil && len(cfg.DefaultQuota.Hard) > 0 {
		iso.Quota = &ResourceQuotaSpec{
			Name: name + "-quota",
			Hard: copyStringMap(cfg.DefaultQuota.Hard),
		}
	}
	if cfg.DefaultLimits != nil && (len(cfg.DefaultLimits.Default) > 0 || len(cfg.DefaultLimits.DefaultRequest) > 0) {
		iso.LimitRange = &LimitRangeSpec{
			Name:           name + "-limits",
			Default:        copyStringMap(cfg.DefaultLimits.Default),
			DefaultRequest: copyStringMap(cfg.DefaultLimits.DefaultRequest),
		}
	}

	return iso
}

func networkPoliciesForProject(project Project, name string, projectSelector map[string]string, cfg IsolationConfig) []NetworkPolicySpec {
	policies := []NetworkPolicySpec{
		// default-deny: no rules, both directions.
		{
			Name:        name + "-default-deny",
			PodSelector: map[string]string{},
			PolicyTypes: []string{"Ingress", "Egress"},
		},
		// allow-intra: pods in the project may talk to each other both directions.
		{
			Name:        name + "-allow-intra",
			PodSelector: map[string]string{},
			PolicyTypes: []string{"Ingress", "Egress"},
			Ingress: []NetworkPolicyRule{{
				Peers: []NetworkPolicyPeer{{PodSelector: copyStringMap(projectSelector)}},
			}},
			Egress: []NetworkPolicyRule{{
				Peers: []NetworkPolicyPeer{{PodSelector: copyStringMap(projectSelector)}},
			}},
		},
	}

	// allow-ingress-controller: ingress from the ingress-controller namespace to
	// the pods that expose an Ingress (kong/studio/storage etc.).
	ingressSelector := ingressControllerNamespaceSelector(cfg)
	if ingressPorts := ingressExposedPorts(project); len(ingressPorts) > 0 {
		policies = append(policies, NetworkPolicySpec{
			Name:        name + "-allow-ingress-controller",
			PodSelector: copyStringMap(projectSelector),
			PolicyTypes: []string{"Ingress"},
			Ingress: []NetworkPolicyRule{{
				Peers: []NetworkPolicyPeer{{NamespaceSelector: copyStringMap(ingressSelector)}},
				Ports: ingressPorts,
			}},
		})
	}

	// allow-egress: DNS, own DB pod/port, and any configured external egress.
	egressRules := []NetworkPolicyRule{
		// DNS to kube-system (or configured namespace).
		{
			Peers: []NetworkPolicyPeer{{NamespaceSelector: dnsNamespaceSelector(cfg)}},
			Ports: []NetworkPolicyPort{
				{Protocol: "UDP", Port: 53},
				{Protocol: "TCP", Port: 53},
			},
		},
		// intra-namespace DB egress (own project pods on the DB port).
		{
			Peers: []NetworkPolicyPeer{{PodSelector: copyStringMap(projectSelector)}},
			Ports: []NetworkPolicyPort{{Protocol: "TCP", Port: databasePort(project)}},
		},
	}
	if external := externalEgressRule(project, cfg.ExtraEgressCIDRs); external != nil {
		egressRules = append(egressRules, *external)
	}
	policies = append(policies, NetworkPolicySpec{
		Name:        name + "-allow-egress",
		PodSelector: copyStringMap(projectSelector),
		PolicyTypes: []string{"Egress"},
		Egress:      egressRules,
	})

	return policies
}

func ingressControllerNamespaceSelector(cfg IsolationConfig) map[string]string {
	if len(cfg.IngressControllerNamespaceSelector) > 0 {
		return copyStringMap(cfg.IngressControllerNamespaceSelector)
	}
	ns := strings.TrimSpace(cfg.IngressControllerNamespace)
	if ns == "" {
		ns = "ingress-nginx"
	}
	return map[string]string{"kubernetes.io/metadata.name": ns}
}

func dnsNamespaceSelector(cfg IsolationConfig) map[string]string {
	ns := strings.TrimSpace(cfg.DNSNamespace)
	if ns == "" {
		ns = "kube-system"
	}
	return map[string]string{"kubernetes.io/metadata.name": ns}
}

// ingressExposedPorts collects the container ports for project services that
// declare an enabled Ingress, so the ingress controller may reach them.
func ingressExposedPorts(project Project) []NetworkPolicyPort {
	resources, err := resourcesForProject(project)
	if err != nil {
		return nil
	}
	seen := map[int]struct{}{}
	ports := make([]NetworkPolicyPort, 0)
	for _, workload := range resources.Workloads {
		if workload.Ingress == nil || len(workload.Ports) == 0 {
			continue
		}
		for _, port := range workload.Ports {
			target := port.TargetPort
			if target <= 0 {
				target = port.Port
			}
			if target <= 0 || target > 65535 {
				continue
			}
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			protocol := strings.ToUpper(strings.TrimSpace(port.Protocol))
			if protocol == "" {
				protocol = "TCP"
			}
			ports = append(ports, NetworkPolicyPort{Protocol: protocol, Port: target})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	return ports
}

// databasePort resolves the project's database service port. It honours an
// explicit runtimeNetwork override and otherwise derives it from the rendered
// "db" service, falling back to the canonical Postgres port.
func databasePort(project Project) int {
	if project.Spec.RuntimeNetwork != nil && project.Spec.RuntimeNetwork.DatabasePort > 0 {
		return project.Spec.RuntimeNetwork.DatabasePort
	}
	dbService := "db"
	if project.Spec.RuntimeNetwork != nil {
		if svc := strings.TrimSpace(project.Spec.RuntimeNetwork.DatabaseService); svc != "" {
			dbService = svc
		}
	}
	dbName := kubernetesName(dbService)
	resources, err := resourcesForProject(project)
	if err == nil {
		for _, workload := range resources.Workloads {
			if workload.ServiceName != dbName {
				continue
			}
			for _, port := range workload.Ports {
				target := port.TargetPort
				if target <= 0 {
					target = port.Port
				}
				if target > 0 && target <= 65535 {
					return target
				}
			}
		}
	}
	return 5432
}

// externalEgressRule renders an egress rule for the configured external CIDRs
// (per-project allowedEgressCidrs merged with platform-wide extraEgressCIDRs)
// and ports, or nil when none are configured. Every CIDR carries a mandatory
// ipBlock.except carving out link-local / cloud-metadata ranges so a careless
// broad CIDR (e.g. 0.0.0.0/0) cannot re-open SSRF to 169.254.169.254 or reach
// the rest of the cluster via link-local. Loopback CIDRs are dropped outright.
func externalEgressRule(project Project, extraCIDRs []string) *NetworkPolicyRule {
	cidrs := make([]string, 0)
	if project.Spec.RuntimeNetwork != nil {
		cidrs = append(cidrs, project.Spec.RuntimeNetwork.AllowedEgressCIDRs...)
	}
	cidrs = append(cidrs, extraCIDRs...)

	seen := map[string]struct{}{}
	peers := make([]NetworkPolicyPeer, 0, len(cidrs))
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" || isLoopbackCIDR(cidr) {
			continue
		}
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		peer := NetworkPolicyPeer{CIDR: cidr}
		if except := metadataExceptFor(cidr); len(except) > 0 {
			peer.Except = except
		}
		peers = append(peers, peer)
	}
	if len(peers) == 0 {
		return nil
	}

	var ports []NetworkPolicyPort
	if project.Spec.RuntimeNetwork != nil {
		ports = make([]NetworkPolicyPort, 0, len(project.Spec.RuntimeNetwork.ExternalEgressPorts))
		for _, port := range project.Spec.RuntimeNetwork.ExternalEgressPorts {
			if port <= 0 || port > 65535 {
				continue
			}
			ports = append(ports, NetworkPolicyPort{Protocol: "TCP", Port: port})
		}
	}
	return &NetworkPolicyRule{Peers: peers, Ports: ports}
}

// metadataExceptFor returns the link-local / metadata except CIDRs that fall
// inside the supplied CIDR. ipBlock.except entries must be subsets of the block,
// so only ranges contained in cidr are returned.
func metadataExceptFor(cidr string) []string {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(metadataEgressExcept))
	for _, exc := range metadataEgressExcept {
		excIP, _, err := net.ParseCIDR(exc)
		if err != nil {
			continue
		}
		if network.Contains(excIP) {
			out = append(out, exc)
		}
	}
	return out
}

func isLoopbackCIDR(cidr string) bool {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ip.IsLoopback()
}

// namespaceLabels returns the management + PodSecurity labels applied to a
// project's runtime namespace. Enforce defaults to "baseline" (the Supabase db
// pod runs as root); audit and warn default to "restricted" so violations are
// surfaced via the API server even where enforcement is relaxed. Versions
// default to "latest".
func namespaceLabels(project Project, levels PodSecurityLevels) map[string]string {
	ref := strings.TrimSpace(project.Spec.Ref)
	if ref == "" {
		ref = strings.TrimSpace(project.Metadata.Name)
	}
	enforce := strings.TrimSpace(levels.Enforce)
	if enforce == "" {
		enforce = "baseline"
	}
	audit := strings.TrimSpace(levels.Audit)
	if audit == "" {
		audit = "restricted"
	}
	warn := strings.TrimSpace(levels.Warn)
	if warn == "" {
		warn = "restricted"
	}
	return map[string]string{
		labelManagedBy:                               "supadupa-operator",
		labelProjectRef:                              ref,
		"pod-security.kubernetes.io/enforce":         enforce,
		"pod-security.kubernetes.io/enforce-version": defaultVersion(levels.EnforceVersion),
		"pod-security.kubernetes.io/audit":           audit,
		"pod-security.kubernetes.io/audit-version":   defaultVersion(levels.AuditVersion),
		"pod-security.kubernetes.io/warn":            warn,
		"pod-security.kubernetes.io/warn-version":    defaultVersion(levels.WarnVersion),
	}
}

func defaultVersion(v string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return "latest"
}

// portKey is a small helper used in tests/builders for stable identification.
func portKey(port NetworkPolicyPort) string {
	return port.Protocol + "/" + strconv.Itoa(port.Port)
}
