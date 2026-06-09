package control

import (
	"context"

	"supadupa2026/internal/env"
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
		RecoveryGuardEnabled:       env.Bool("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS"),
		DurableUpgradeGuardEnabled: env.Bool("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP"),
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
