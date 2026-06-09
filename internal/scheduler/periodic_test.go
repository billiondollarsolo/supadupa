package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestPeriodicRunnerSkipsOverlappingPasses(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewPeriodicRunner("test", logger)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool)

	go func() {
		ran := runner.RunOnce(ctx, func(context.Context) {
			close(started)
			<-release
		})
		done <- ran
	}()

	<-started
	overlapRan := runner.RunOnce(ctx, func(context.Context) {
		t.Fatal("overlapping pass should not run")
	})
	if overlapRan {
		t.Fatal("expected overlapping pass to be skipped")
	}

	close(release)
	if !<-done {
		t.Fatal("expected first pass to run")
	}

	rerun := false
	if !runner.RunOnce(ctx, func(context.Context) {
		rerun = true
	}) {
		t.Fatal("expected pass after release to run")
	}
	if !rerun {
		t.Fatal("expected pass body after release to run")
	}
}
