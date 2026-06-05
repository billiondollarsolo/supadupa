package control

import (
	"context"
	"time"
)

type Provisioner interface {
	Name() string
	Create(ctx context.Context, spec ProjectSpec) error
	SyncSecrets(ctx context.Context, ref string, spec ProjectSpec) error
	Destroy(ctx context.Context, ref string) error
	Status(ctx context.Context, ref string) (ProjectStatus, error)
	Upgrade(ctx context.Context, ref string, version string) error
	Pause(ctx context.Context, ref string) error
	Resume(ctx context.Context, ref string) error
	Scale(ctx context.Context, ref string, tier ResourceTier) error
	AddReplica(ctx context.Context, ref string, opts ReplicaOpts) error
}

type DestroyOptions struct {
	RetainVolumes bool `json:"retain_volumes"`
}

type OptionedDestroyer interface {
	DestroyWithOptions(ctx context.Context, ref string, opts DestroyOptions) error
}

type ConfigSyncer interface {
	SyncConfig(ctx context.Context, ref string, config ProjectConfig) error
}

type ServiceSyncer interface {
	SyncServices(ctx context.Context, ref string, spec ProjectSpec) error
}

type AuthHookSyncer interface {
	SyncAuthHooks(ctx context.Context, ref string, hooks []ProjectAuthHook) error
}

type TelemetryCollector interface {
	CollectProjectTelemetry(ctx context.Context, ref string) (TelemetrySampleInput, error)
}

type BranchCloneOptions struct {
	SourceRef string
	BranchRef string
	BranchID  string
	Name      string
	ExpiresAt *time.Time
}

type BranchCloneResult struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

type BranchCloner interface {
	CloneBranch(ctx context.Context, opts BranchCloneOptions) (BranchCloneResult, error)
}
