package api

import (
	"os"
	"testing"
)

// TestMain forces synchronous provisioning for the whole api test package.
// Production defaults to asynchronous provisioning (create returns 202 and a
// background goroutine + reconciler converge the project), but tests need the
// provisioning side effects to complete before assertions run.
func TestMain(m *testing.M) {
	defaultProvisionDispatcher = func(f func()) { f() }
	os.Exit(m.Run())
}
