package operator

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type fakeLeaseClient struct {
	lease          *Lease
	createConflict bool
	getErr         error
	createCount    int
	updateCount    int
}

func (c *fakeLeaseClient) GetLease(_ context.Context, _ string, _ string) (*Lease, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.lease == nil {
		return nil, nil
	}
	copy := *c.lease
	return &copy, nil
}

func (c *fakeLeaseClient) CreateLease(_ context.Context, namespace string, lease Lease) (int, error) {
	c.createCount++
	if c.createConflict {
		return http.StatusConflict, context.Canceled // any non-nil error; status drives caller
	}
	lease.Metadata.Namespace = namespace
	c.lease = &lease
	return http.StatusCreated, nil
}

func (c *fakeLeaseClient) UpdateLease(_ context.Context, namespace string, lease Lease) error {
	c.updateCount++
	lease.Metadata.Namespace = namespace
	c.lease = &lease
	return nil
}

func newElector(client LeaseClient, now time.Time) *LeaderElector {
	return &LeaderElector{
		Client:        client,
		Namespace:     "supadupa",
		Name:          "supadupa-operator-leader",
		Identity:      "pod-a",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}
}

func TestLeaderElectorAcquiresWhenLeaseMissing(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeLeaseClient{}
	got, err := newElector(client, now).TryAcquireOrRenew(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !got || client.createCount != 1 || client.lease.Spec.HolderIdentity != "pod-a" {
		t.Fatalf("expected acquisition, got leader=%v create=%d lease=%#v", got, client.createCount, client.lease)
	}
}

func TestLeaderElectorRenewsOwnLease(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeLeaseClient{lease: &Lease{
		Metadata: ObjectMeta{Name: "supadupa-operator-leader"},
		Spec:     LeaseSpec{HolderIdentity: "pod-a", LeaseDurationSeconds: 30, RenewTime: now.Add(-5 * time.Second).Format(time.RFC3339)},
	}}
	got, err := newElector(client, now).TryAcquireOrRenew(context.Background())
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !got || client.updateCount != 1 {
		t.Fatalf("expected renewal, got leader=%v update=%d", got, client.updateCount)
	}
}

func TestLeaderElectorYieldsToValidHolder(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeLeaseClient{lease: &Lease{
		Metadata: ObjectMeta{Name: "supadupa-operator-leader"},
		Spec:     LeaseSpec{HolderIdentity: "pod-b", LeaseDurationSeconds: 30, RenewTime: now.Add(-5 * time.Second).Format(time.RFC3339)},
	}}
	got, err := newElector(client, now).TryAcquireOrRenew(context.Background())
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if got || client.updateCount != 0 {
		t.Fatalf("expected to yield to valid holder, got leader=%v update=%d", got, client.updateCount)
	}
}

func TestLeaderElectorTakesOverExpiredLease(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeLeaseClient{lease: &Lease{
		Metadata: ObjectMeta{Name: "supadupa-operator-leader"},
		Spec:     LeaseSpec{HolderIdentity: "pod-b", LeaseDurationSeconds: 30, RenewTime: now.Add(-2 * time.Minute).Format(time.RFC3339), LeaseTransitions: 1},
	}}
	got, err := newElector(client, now).TryAcquireOrRenew(context.Background())
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if !got || client.updateCount != 1 || client.lease.Spec.HolderIdentity != "pod-a" || client.lease.Spec.LeaseTransitions != 2 {
		t.Fatalf("expected takeover with incremented transitions, got leader=%v lease=%#v", got, client.lease)
	}
}

func TestLeaderElectorLosesCreateRace(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeLeaseClient{createConflict: true}
	got, err := newElector(client, now).TryAcquireOrRenew(context.Background())
	if err != nil {
		t.Fatalf("expected conflict to be swallowed, got %v", err)
	}
	if got {
		t.Fatalf("expected to lose create race")
	}
}
