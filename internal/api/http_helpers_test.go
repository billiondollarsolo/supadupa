package api

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestLogRollbackErrorDoesNotPanic(t *testing.T) {
	before := RollbackFailureTotal()
	// nil err must be a no-op
	logRollbackError(context.Background(), "noop", nil)
	if RollbackFailureTotal() != before {
		t.Fatalf("nil error must not increment metric")
	}

	// non-nil err must log without panicking and increment metric
	logRollbackError(context.Background(), "delete after apply failure", errors.New("rollback boom"))
	if RollbackFailureTotal() != before+1 {
		t.Fatalf("expected metric +1, before=%d after=%d", before, RollbackFailureTotal())
	}
}

func TestLogRollbackErrorLogsOnFailure(t *testing.T) {
	var records []slog.Record
	handler := &captureHandler{records: &records}
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logRollbackError(context.Background(), "delete project database queue after apply failure", errors.New("store unavailable"))
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}
	rec := records[0]
	if rec.Level != slog.LevelError {
		t.Fatalf("expected error level, got %v", rec.Level)
	}
	if rec.Message != "rollback failed" {
		t.Fatalf("expected message %q, got %q", "rollback failed", rec.Message)
	}

	attrs := map[string]any{}
	rec.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	if attrs["action"] != "delete project database queue after apply failure" {
		t.Fatalf("unexpected action attr: %#v", attrs["action"])
	}
	errAttr, ok := attrs["error"].(error)
	if !ok {
		t.Fatalf("expected error attr, got %#v", attrs["error"])
	}
	if errAttr.Error() != "store unavailable" {
		t.Fatalf("unexpected error attr: %v", errAttr)
	}

	// nil must not produce a record
	logRollbackError(context.Background(), "should not log", nil)
	if len(records) != 1 {
		t.Fatalf("expected still 1 log record after nil err, got %d", len(records))
	}
}

type captureHandler struct {
	records *[]slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, rec slog.Record) error {
	*h.records = append(*h.records, rec.Clone())
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(string) slog.Handler { return h }
