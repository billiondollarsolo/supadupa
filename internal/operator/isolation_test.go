package operator

import (
	"testing"
)

func sampleIsolationProject() Project {
	return Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services: map[string]ServiceSpec{
				"db": {
					Enabled: true,
					Image:   "supabase/postgres:15",
					Ports:   []ServicePortSpec{{Name: "postgres", Port: 5432}},
				},
				"kong": {
					Enabled: true,
					Image:   "kong/kong:3",
					Ports:   []ServicePortSpec{{Name: "http", Port: 8000}},
					Ingress: &ServiceIngressSpec{Enabled: true, Host: "alpha.example.com"},
				},
			},
		},
	}
}

func findPolicy(t *testing.T, iso ProjectIsolation, name string) NetworkPolicySpec {
	t.Helper()
	for _, p := range iso.NetworkPolicies {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("network policy %q not found in %#v", name, iso.NetworkPolicies)
	return NetworkPolicySpec{}
}

func TestIsolationForProjectServiceAccount(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{})
	if iso.ServiceAccountName != "alpha-runtime" {
		t.Fatalf("expected service account alpha-runtime, got %q", iso.ServiceAccountName)
	}
	if iso.Labels["supadupa.dev/project-ref"] != "alpha" || iso.Labels["app.kubernetes.io/managed-by"] != "supadupa-operator" {
		t.Fatalf("unexpected isolation labels %#v", iso.Labels)
	}
}

func TestIsolationDefaultDenyPolicy(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{NetworkPolicyEnabled: true})
	deny := findPolicy(t, iso, "alpha-default-deny")
	if len(deny.PodSelector) != 0 {
		t.Fatalf("expected empty pod selector for default deny, got %#v", deny.PodSelector)
	}
	if len(deny.PolicyTypes) != 2 || deny.PolicyTypes[0] != "Ingress" || deny.PolicyTypes[1] != "Egress" {
		t.Fatalf("expected Ingress+Egress policy types, got %#v", deny.PolicyTypes)
	}
	if len(deny.Ingress) != 0 || len(deny.Egress) != 0 {
		t.Fatalf("expected no rules on default deny, got ingress=%#v egress=%#v", deny.Ingress, deny.Egress)
	}
}

func TestIsolationAllowIntraPolicy(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{NetworkPolicyEnabled: true})
	intra := findPolicy(t, iso, "alpha-allow-intra")
	if len(intra.Ingress) != 1 || len(intra.Ingress[0].Peers) != 1 ||
		intra.Ingress[0].Peers[0].PodSelector["supadupa.dev/project-ref"] != "alpha" {
		t.Fatalf("expected intra ingress from project pods, got %#v", intra.Ingress)
	}
	if len(intra.Egress) != 1 || len(intra.Egress[0].Peers) != 1 ||
		intra.Egress[0].Peers[0].PodSelector["supadupa.dev/project-ref"] != "alpha" {
		t.Fatalf("expected intra egress to project pods, got %#v", intra.Egress)
	}
}

func TestIsolationAllowIngressControllerPolicy(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{NetworkPolicyEnabled: true, IngressControllerNamespace: "ingress-nginx"})
	policy := findPolicy(t, iso, "alpha-allow-ingress-controller")
	if policy.PodSelector["supadupa.dev/project-ref"] != "alpha" {
		t.Fatalf("expected ingress-controller policy to target project pods, got %#v", policy.PodSelector)
	}
	if len(policy.Ingress) != 1 || len(policy.Ingress[0].Peers) != 1 ||
		policy.Ingress[0].Peers[0].NamespaceSelector["kubernetes.io/metadata.name"] != "ingress-nginx" {
		t.Fatalf("expected namespaceSelector for ingress-nginx, got %#v", policy.Ingress)
	}
	// The only ingress-exposed service is kong on container port 8000.
	if len(policy.Ingress[0].Ports) != 1 || policy.Ingress[0].Ports[0].Port != 8000 {
		t.Fatalf("expected ingress-exposed port 8000, got %#v", policy.Ingress[0].Ports)
	}
}

func TestIsolationAllowIngressControllerCustomSelector(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{
		NetworkPolicyEnabled:               true,
		IngressControllerNamespaceSelector: map[string]string{"team": "platform"},
	})
	policy := findPolicy(t, iso, "alpha-allow-ingress-controller")
	if policy.Ingress[0].Peers[0].NamespaceSelector["team"] != "platform" {
		t.Fatalf("expected custom namespace selector, got %#v", policy.Ingress[0].Peers[0].NamespaceSelector)
	}
}

func TestIsolationAllowEgressPolicy(t *testing.T) {
	project := sampleIsolationProject()
	project.Spec.RuntimeNetwork = &ProjectNetworkSpec{
		AllowedEgressCIDRs:  []string{"203.0.113.0/24"},
		ExternalEgressPorts: []int{443},
	}
	iso := isolationForProject(project, "alpha", IsolationConfig{NetworkPolicyEnabled: true})
	egress := findPolicy(t, iso, "alpha-allow-egress")
	if len(egress.PolicyTypes) != 1 || egress.PolicyTypes[0] != "Egress" {
		t.Fatalf("expected egress-only policy types, got %#v", egress.PolicyTypes)
	}
	// rule 0: DNS to kube-system on 53 udp+tcp.
	dns := egress.Egress[0]
	if dns.Peers[0].NamespaceSelector["kubernetes.io/metadata.name"] != "kube-system" {
		t.Fatalf("expected DNS egress to kube-system, got %#v", dns.Peers)
	}
	if len(dns.Ports) != 2 || portKey(dns.Ports[0]) != "UDP/53" || portKey(dns.Ports[1]) != "TCP/53" {
		t.Fatalf("expected DNS udp/tcp 53 ports, got %#v", dns.Ports)
	}
	// rule 1: DB egress to project pods on 5432.
	db := egress.Egress[1]
	if db.Peers[0].PodSelector["supadupa.dev/project-ref"] != "alpha" || len(db.Ports) != 1 || db.Ports[0].Port != 5432 {
		t.Fatalf("expected DB egress to project pods on 5432, got %#v", db)
	}
	// rule 2: external CIDR egress.
	external := egress.Egress[2]
	if external.Peers[0].CIDR != "203.0.113.0/24" || len(external.Ports) != 1 || external.Ports[0].Port != 443 {
		t.Fatalf("expected external egress to 203.0.113.0/24:443, got %#v", external)
	}
}

func TestIsolationEgressOmitsExternalRuleWhenUnset(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{NetworkPolicyEnabled: true})
	egress := findPolicy(t, iso, "alpha-allow-egress")
	if len(egress.Egress) != 2 {
		t.Fatalf("expected only DNS + DB egress rules when no external config, got %#v", egress.Egress)
	}
}

func TestIsolationQuotaAndLimitRange(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{
		DefaultQuota: &ProjectQuotaDefaults{Hard: map[string]string{"requests.cpu": "4", "pods": "50"}},
		DefaultLimits: &ProjectLimitDefaults{
			Default:        map[string]string{"cpu": "500m"},
			DefaultRequest: map[string]string{"cpu": "100m"},
		},
	})
	if iso.Quota == nil || iso.Quota.Name != "alpha-quota" || iso.Quota.Hard["requests.cpu"] != "4" || iso.Quota.Hard["pods"] != "50" {
		t.Fatalf("unexpected quota %#v", iso.Quota)
	}
	if iso.LimitRange == nil || iso.LimitRange.Name != "alpha-limits" ||
		iso.LimitRange.Default["cpu"] != "500m" || iso.LimitRange.DefaultRequest["cpu"] != "100m" {
		t.Fatalf("unexpected limit range %#v", iso.LimitRange)
	}
}

func TestIsolationQuotaAndLimitRangeOmittedWhenNil(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{})
	if iso.Quota != nil {
		t.Fatalf("expected nil quota, got %#v", iso.Quota)
	}
	if iso.LimitRange != nil {
		t.Fatalf("expected nil limit range, got %#v", iso.LimitRange)
	}
}

func TestNamespaceLabelsPodSecurity(t *testing.T) {
	labels := namespaceLabels(sampleIsolationProject(), PodSecurityLevels{
		Enforce: "restricted", Audit: "restricted", Warn: "restricted",
	})
	if labels["pod-security.kubernetes.io/enforce"] != "restricted" ||
		labels["pod-security.kubernetes.io/enforce-version"] != "latest" ||
		labels["pod-security.kubernetes.io/audit"] != "restricted" ||
		labels["pod-security.kubernetes.io/warn"] != "restricted" {
		t.Fatalf("unexpected PSA labels %#v", labels)
	}
	if labels["supadupa.dev/project-ref"] != "alpha" {
		t.Fatalf("expected project-ref label, got %#v", labels)
	}
}

func TestNamespaceLabelsDefaultsToBaselineEnforceRestrictedAuditWarn(t *testing.T) {
	// Empty enforce defaults to baseline; audit/warn default to restricted so
	// violations are surfaced even when enforcement is relaxed.
	labels := namespaceLabels(sampleIsolationProject(), PodSecurityLevels{})
	if labels["pod-security.kubernetes.io/enforce"] != "baseline" {
		t.Fatalf("expected baseline enforce default, got %#v", labels)
	}
	if labels["pod-security.kubernetes.io/audit"] != "restricted" || labels["pod-security.kubernetes.io/warn"] != "restricted" {
		t.Fatalf("expected restricted audit/warn defaults, got %#v", labels)
	}
}

func TestNamespaceLabelsHonoursAuditWarnAndVersions(t *testing.T) {
	labels := namespaceLabels(sampleIsolationProject(), PodSecurityLevels{
		Enforce: "baseline", Audit: "restricted", Warn: "baseline",
		EnforceVersion: "v1.30", AuditVersion: "v1.29", WarnVersion: "v1.28",
	})
	if labels["pod-security.kubernetes.io/audit"] != "restricted" || labels["pod-security.kubernetes.io/warn"] != "baseline" {
		t.Fatalf("audit/warn not honoured: %#v", labels)
	}
	if labels["pod-security.kubernetes.io/enforce-version"] != "v1.30" ||
		labels["pod-security.kubernetes.io/audit-version"] != "v1.29" ||
		labels["pod-security.kubernetes.io/warn-version"] != "v1.28" {
		t.Fatalf("version labels not honoured: %#v", labels)
	}
}

func TestIsolationNetworkPolicyToggleOff(t *testing.T) {
	iso := isolationForProject(sampleIsolationProject(), "alpha", IsolationConfig{NetworkPolicyEnabled: false})
	if len(iso.NetworkPolicies) != 0 {
		t.Fatalf("expected no NetworkPolicies when toggle off, got %#v", iso.NetworkPolicies)
	}
	if iso.ServiceAccountName != "alpha-runtime" {
		t.Fatalf("expected SA still rendered when policies off, got %#v", iso.ServiceAccountName)
	}
}

func TestIsolationExtraEgressCIDRsMergedWithMetadataExcept(t *testing.T) {
	project := sampleIsolationProject()
	project.Spec.RuntimeNetwork = &ProjectNetworkSpec{
		AllowedEgressCIDRs:  []string{"0.0.0.0/0"},
		ExternalEgressPorts: []int{443},
	}
	iso := isolationForProject(project, "alpha", IsolationConfig{
		NetworkPolicyEnabled: true,
		ExtraEgressCIDRs:     []string{"198.51.100.0/24", "127.0.0.0/8"},
	})
	egress := findPolicy(t, iso, "alpha-allow-egress")
	external := egress.Egress[2]
	if len(external.Peers) != 2 {
		t.Fatalf("expected merged external + extra CIDRs (loopback dropped), got %#v", external.Peers)
	}
	if external.Peers[0].CIDR != "0.0.0.0/0" || len(external.Peers[0].Except) == 0 || external.Peers[0].Except[0] != "169.254.0.0/16" {
		t.Fatalf("expected metadata/link-local except on broad CIDR, got %#v", external.Peers[0])
	}
	if external.Peers[1].CIDR != "198.51.100.0/24" || len(external.Peers[1].Except) != 0 {
		t.Fatalf("expected extra CIDR with no except, got %#v", external.Peers[1])
	}
}

func TestDatabasePortHonoursOverride(t *testing.T) {
	project := sampleIsolationProject()
	project.Spec.RuntimeNetwork = &ProjectNetworkSpec{DatabasePort: 6543}
	if got := databasePort(project); got != 6543 {
		t.Fatalf("expected database port override 6543, got %d", got)
	}
}
