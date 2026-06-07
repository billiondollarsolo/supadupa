package control

import (
	"context"
	"os"
	"strings"
)

type recoveryPosture struct {
	RecoveryGuardEnabled       bool
	DurableUpgradeGuardEnabled bool
	RecoveryReadyTargets       int
	DefaultRecoveryReadyTarget bool
}

func fleetRecoveryPosture(ctx context.Context, store Store) (recoveryPosture, error) {
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return recoveryPosture{}, err
	}
	posture := recoveryPosture{
		RecoveryGuardEnabled:       envBoolForPosture("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS"),
		DurableUpgradeGuardEnabled: envBoolForPosture("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP"),
	}
	for _, target := range targets {
		if target.RecoveryReady {
			posture.RecoveryReadyTargets++
			if target.Default {
				posture.DefaultRecoveryReadyTarget = true
			}
		}
	}
	return posture, nil
}

func envBoolForPosture(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
