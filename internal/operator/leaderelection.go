package operator

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// LeaseClient is the minimal Lease API surface the LeaderElector needs. It is
// satisfied by *KubernetesClient and faked in tests.
type LeaseClient interface {
	GetLease(ctx context.Context, namespace string, name string) (*Lease, error)
	CreateLease(ctx context.Context, namespace string, lease Lease) (int, error)
	UpdateLease(ctx context.Context, namespace string, lease Lease) error
}

// LeaderElector implements lease-based leader election against a
// coordination.k8s.io/v1 Lease. Only the current holder reconciles; followers
// poll until they can acquire (the holder's lease has expired). This is a
// dependency-free implementation suitable for the operator's single-active
// controller model.
type LeaderElector struct {
	Client        LeaseClient
	Namespace     string
	Name          string
	Identity      string
	LeaseDuration time.Duration
	RetryPeriod   time.Duration
	Now           func() time.Time
}

func (e *LeaderElector) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *LeaderElector) leaseDuration() time.Duration {
	if e.LeaseDuration > 0 {
		return e.LeaseDuration
	}
	return 15 * time.Second
}

func (e *LeaderElector) retryPeriod() time.Duration {
	if e.RetryPeriod > 0 {
		return e.RetryPeriod
	}
	return 5 * time.Second
}

// TryAcquireOrRenew attempts to become or remain the leader. It returns true
// when this identity holds the lease after the call.
func (e *LeaderElector) TryAcquireOrRenew(ctx context.Context) (bool, error) {
	now := e.now()
	durationSeconds := int(e.leaseDuration().Seconds())
	if durationSeconds < 1 {
		durationSeconds = 1
	}

	existing, err := e.Client.GetLease(ctx, e.Namespace, e.Name)
	if err != nil {
		return false, err
	}
	if existing == nil {
		status, err := e.Client.CreateLease(ctx, e.Namespace, Lease{
			Metadata: ObjectMeta{Name: e.Name, Namespace: e.Namespace},
			Spec: LeaseSpec{
				HolderIdentity:       e.Identity,
				LeaseDurationSeconds: durationSeconds,
				AcquireTime:          now.UTC().Format(time.RFC3339),
				RenewTime:            now.UTC().Format(time.RFC3339),
				LeaseTransitions:     0,
			},
		})
		if err != nil {
			// A concurrent creator won the race; we are a follower this round.
			if status == http.StatusConflict {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	holder := strings.TrimSpace(existing.Spec.HolderIdentity)
	if holder == e.Identity {
		// Renew our own lease.
		existing.Spec.HolderIdentity = e.Identity
		existing.Spec.LeaseDurationSeconds = durationSeconds
		existing.Spec.RenewTime = now.UTC().Format(time.RFC3339)
		if err := e.Client.UpdateLease(ctx, e.Namespace, *existing); err != nil {
			return false, err
		}
		return true, nil
	}

	// Held by someone else: only take over if their lease has expired.
	if !e.leaseExpired(existing, now) {
		return false, nil
	}
	transitions := existing.Spec.LeaseTransitions + 1
	existing.Spec.HolderIdentity = e.Identity
	existing.Spec.LeaseDurationSeconds = durationSeconds
	existing.Spec.AcquireTime = now.UTC().Format(time.RFC3339)
	existing.Spec.RenewTime = now.UTC().Format(time.RFC3339)
	existing.Spec.LeaseTransitions = transitions
	if err := e.Client.UpdateLease(ctx, e.Namespace, *existing); err != nil {
		return false, err
	}
	return true, nil
}

func (e *LeaderElector) leaseExpired(lease *Lease, now time.Time) bool {
	renew := strings.TrimSpace(lease.Spec.RenewTime)
	if renew == "" {
		return true
	}
	renewedAt, err := time.Parse(time.RFC3339, renew)
	if err != nil {
		return true
	}
	duration := time.Duration(lease.Spec.LeaseDurationSeconds) * time.Second
	if duration <= 0 {
		duration = e.leaseDuration()
	}
	return now.After(renewedAt.Add(duration))
}
