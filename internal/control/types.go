package control

type ProjectSpec struct {
	Ref          string                 `json:"ref"`
	OrgID        string                 `json:"org_id"`
	Name         string                 `json:"name"`
	HostID       string                 `json:"host_id"`
	Domain       string                 `json:"domain"`
	StackVersion string                 `json:"stack_version"`
	Profile      StackProfile           `json:"profile"`
	ResourceTier ResourceTier           `json:"resource_tier"`
	Services     map[string]ServiceSpec `json:"services"`
	Environment  map[string]string      `json:"environment"`
}

type ProjectStatus struct {
	Ref       string            `json:"ref"`
	Phase     ProjectPhase      `json:"phase"`
	Message   string            `json:"message"`
	Endpoints map[string]string `json:"endpoints"`
	Services  []RuntimeService  `json:"services,omitempty"`
}

type RuntimeService struct {
	Name           string `json:"name"`
	ComposeService string `json:"compose_service"`
	Desired        bool   `json:"desired"`
	State          string `json:"state"`
	Health         string `json:"health,omitempty"`
	ExitCode       int    `json:"exit_code,omitempty"`
	Message        string `json:"message,omitempty"`
}

type ProjectPhase string

const (
	ProjectProvisioning ProjectPhase = "provisioning"
	ProjectHealthy      ProjectPhase = "healthy"
	ProjectDegraded     ProjectPhase = "degraded"
	ProjectPaused       ProjectPhase = "paused"
	ProjectError        ProjectPhase = "error"
	ProjectDestroying   ProjectPhase = "destroying"
)

type StackProfile string

const (
	StackProfileEssential StackProfile = "essential"
	StackProfileFull      StackProfile = "full"
	StackProfileOrioleDB  StackProfile = "orioledb"
)

type ResourceTier string

const (
	ResourceTierSmall  ResourceTier = "small"
	ResourceTierMedium ResourceTier = "medium"
	ResourceTierLarge  ResourceTier = "large"
)

type ServiceSpec struct {
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

type ReplicaOpts struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	HostID           string       `json:"host_id"`
	Region           string       `json:"region"`
	Tier             ResourceTier `json:"tier"`
	ReadWeight       int          `json:"read_weight"`
	FailoverPriority int          `json:"failover_priority"`
}
