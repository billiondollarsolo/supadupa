package scheduler

import (
	"testing"
	"time"
)

func TestBackupSchedulerTickFromEnvDefaults(t *testing.T) {
	tick, err := BackupSchedulerTickFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tick != DefaultBackupSchedulerTick {
		t.Fatalf("tick = %s, want %s", tick, DefaultBackupSchedulerTick)
	}
}

func TestBackupSchedulerTickFromEnvParsesDuration(t *testing.T) {
	tick, err := BackupSchedulerTickFromEnv(func(key string) string {
		if key != BackupSchedulerTickEnv {
			t.Fatalf("key = %q, want %q", key, BackupSchedulerTickEnv)
		}
		return "30s"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tick != 30*time.Second {
		t.Fatalf("tick = %s, want 30s", tick)
	}
}

func TestBackupSchedulerTickFromEnvRejectsInvalidDuration(t *testing.T) {
	tick, err := BackupSchedulerTickFromEnv(func(string) string { return "soon" })
	if err == nil {
		t.Fatal("expected error")
	}
	if tick != DefaultBackupSchedulerTick {
		t.Fatalf("tick = %s, want %s", tick, DefaultBackupSchedulerTick)
	}
}

func TestBackupSchedulerTickFromEnvRejectsNonPositiveDuration(t *testing.T) {
	tick, err := BackupSchedulerTickFromEnv(func(string) string { return "0s" })
	if err == nil {
		t.Fatal("expected error")
	}
	if tick != DefaultBackupSchedulerTick {
		t.Fatalf("tick = %s, want %s", tick, DefaultBackupSchedulerTick)
	}
}
