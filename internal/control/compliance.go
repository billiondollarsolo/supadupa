package control

import (
	"context"
	"fmt"
	"strings"
	"supadupa2026/internal/env"
	"time"
)

type ComplianceReport struct {
	GeneratedAt   time.Time           `json:"generated_at"`
	Frameworks    []string            `json:"frameworks"`
	Summary       ComplianceSummary   `json:"summary"`
	Controls      []ComplianceControl `json:"controls"`
	DPAPosture    string              `json:"dpa_posture"`
	Certification string              `json:"certification"`
}

type ComplianceSummary struct {
	Passed       int `json:"passed"`
	ActionNeeded int `json:"action_needed"`
	ManualReview int `json:"manual_review"`
	Total        int `json:"total"`
}

type ComplianceControl struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Category       string   `json:"category"`
	Frameworks     []string `json:"frameworks"`
	Status         string   `json:"status"`
	Evidence       []string `json:"evidence"`
	Recommendation string   `json:"recommendation"`
}

func FleetComplianceReport(ctx context.Context, store Store) (ComplianceReport, error) {
	report := ComplianceReport{
		GeneratedAt:   time.Now().UTC(),
		Frameworks:    []string{"SOC 2", "HIPAA"},
		DPAPosture:    "operator-owned: use these controls as evidence for the deploying organization's DPA and BAA posture",
		Certification: "not certified by supadupa; certification remains the operator's responsibility",
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		return ComplianceReport{}, err
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return ComplianceReport{}, err
	}
	integrity, err := store.VerifyAuditLog(ctx)
	if err != nil {
		return ComplianceReport{}, err
	}
	posture, err := fleetRecoveryPosture(ctx, store)
	if err != nil {
		return ComplianceReport{}, err
	}

	admins := 0
	adminsWithMFA := 0
	for _, user := range users {
		if strings.EqualFold(user.Role, "admin") {
			admins++
			if user.MFAEnabled {
				adminsWithMFA++
			}
		}
	}

	report.Controls = append(report.Controls,
		complianceControl("COM-001", "Immutable audit chain", "audit", []string{"SOC 2 CC7.2", "HIPAA 164.312(b)"}, statusForBool(integrity.Verified), []string{
			fmt.Sprintf("%d audit events sealed", integrity.Events),
			fmt.Sprintf("head hash %s", env.FirstNonEmpty(integrity.HeadHash, "genesis")),
		}, "Keep audit retention policies aligned with the operator's evidence retention schedule."),
		complianceControl("COM-002", "Platform MFA for administrators", "access", []string{"SOC 2 CC6.1", "HIPAA 164.312(d)"}, statusForBool(admins > 0 && admins == adminsWithMFA), []string{
			fmt.Sprintf("%d/%d admin users have MFA enabled", adminsWithMFA, admins),
		}, "Require TOTP MFA for every platform administrator before production use."),
	)

	type fleetCounters struct {
		backupsEnabled   int
		pitrEnabled      int
		sslEnforced      int
		networkScoped    int
		logDrains        int
		rotatedSecrets   int
		projectsExamined int
	}
	counters := fleetCounters{projectsExamined: len(projects)}
	for _, project := range projects {
		ref := project.Ref
		backupPolicy, err := store.GetBackupPolicy(ctx, ref)
		if err != nil {
			return ComplianceReport{}, err
		}
		if backupPolicy.Enabled {
			counters.backupsEnabled++
		}
		pitrPolicy, err := store.GetPITRPolicy(ctx, ref)
		if err != nil {
			return ComplianceReport{}, err
		}
		if pitrPolicy.Enabled {
			counters.pitrEnabled++
		}
		databaseConfig, err := store.GetProjectConfig(ctx, ref, "database")
		if err != nil {
			return ComplianceReport{}, err
		}
		if strings.EqualFold(strings.TrimSpace(databaseConfig.Config["ssl_enforced"]), "true") {
			counters.sslEnforced++
		}
		networkConfig, err := store.GetProjectConfig(ctx, ref, "network")
		if err != nil {
			return ComplianceReport{}, err
		}
		if strings.TrimSpace(networkConfig.Config["db_allowlist"]) != "" || strings.TrimSpace(networkConfig.Config["http_allowlist"]) != "" {
			counters.networkScoped++
		}
		drains, err := store.ListProjectLogDrains(ctx, ref)
		if err != nil {
			return ComplianceReport{}, err
		}
		if len(drains) > 0 {
			counters.logDrains++
		}
		secrets, err := store.ListProjectSecrets(ctx, ref)
		if err != nil {
			return ComplianceReport{}, err
		}
		for _, secret := range secrets {
			if secret.RotatedAt != nil {
				counters.rotatedSecrets++
				break
			}
		}
	}
	totalProjects := len(projects)
	report.Controls = append(report.Controls,
		complianceControl("COM-003", "Backup policy coverage", "recoverability", []string{"SOC 2 CC7.3", "HIPAA 164.308(a)(7)"}, statusForFleet(totalProjects, counters.backupsEnabled), []string{
			fmt.Sprintf("%d/%d projects have backups enabled", counters.backupsEnabled, totalProjects),
		}, "Enable scheduled logical backups for every production project and periodically verify restores."),
		complianceControl("COM-004", "PITR/WAL archive coverage", "recoverability", []string{"SOC 2 CC7.3", "HIPAA 164.308(a)(7)"}, statusForFleet(totalProjects, counters.pitrEnabled), []string{
			fmt.Sprintf("%d/%d projects have PITR enabled", counters.pitrEnabled, totalProjects),
		}, "Enable WAL archiving for projects with recovery-point objectives below the backup interval."),
		complianceControl("COM-005", "Database SSL enforcement", "encryption", []string{"SOC 2 CC6.7", "HIPAA 164.312(e)(1)"}, statusForFleet(totalProjects, counters.sslEnforced), []string{
			fmt.Sprintf("%d/%d projects enforce database SSL", counters.sslEnforced, totalProjects),
		}, "Keep DB SSL enforcement enabled for every project that allows database traffic."),
		complianceControl("COM-006", "Ingress network restrictions", "network", []string{"SOC 2 CC6.6", "HIPAA 164.312(a)(1)"}, statusForFleet(totalProjects, counters.networkScoped), []string{
			fmt.Sprintf("%d/%d projects have an IP allowlist", counters.networkScoped, totalProjects),
		}, "Configure project network allowlists or document approved public-ingress exceptions."),
		complianceControl("COM-007", "External log retention", "observability", []string{"SOC 2 CC7.2", "HIPAA 164.312(b)"}, statusForFleet(totalProjects, counters.logDrains), []string{
			fmt.Sprintf("%d/%d projects export logs to a drain", counters.logDrains, totalProjects),
		}, "Configure log drains for production projects and align downstream retention with policy."),
		complianceControl("COM-008", "Secret rotation evidence", "secrets", []string{"SOC 2 CC6.1", "HIPAA 164.312(a)(2)(iv)"}, statusForFleet(totalProjects, counters.rotatedSecrets), []string{
			fmt.Sprintf("%d/%d projects have rotated at least one secret", counters.rotatedSecrets, totalProjects),
		}, "Rotate service-role, JWT, database, and storage keys on the operator's documented cadence."),
		complianceControl("COM-009", "Hosted-grade recovery guards", "recoverability", []string{"SOC 2 CC7.3", "HIPAA 164.308(a)(7)"}, statusForBool(posture.RecoveryGuardEnabled && posture.DurableUpgradeGuardEnabled), []string{
			fmt.Sprintf("recovery-ready target guard enabled: %t", posture.RecoveryGuardEnabled),
			fmt.Sprintf("durable upgrade backup guard enabled: %t", posture.DurableUpgradeGuardEnabled),
		}, "Enable SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS and SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP for production control planes."),
		complianceControl("COM-010", "Off-host recovery target readiness", "recoverability", []string{"SOC 2 CC7.3", "HIPAA 164.308(a)(7)"}, statusForBool(posture.RecoveryReadyTargets > 0 && posture.DefaultRecoveryReadyTarget), []string{
			fmt.Sprintf("%d recovery-ready backup targets", posture.RecoveryReadyTargets),
			fmt.Sprintf("default recovery-ready target: %t", posture.DefaultRecoveryReadyTarget),
		}, "Configure and test a durable off-host S3/R2/remote-MinIO backup target, then mark it default or bind every policy explicitly."),
		complianceControl("COM-011", "DPA/BAA and certification posture", "process", []string{"SOC 2", "HIPAA"}, "manual_review", []string{
			report.DPAPosture,
			report.Certification,
		}, "Attach operator-owned DPA/BAA, risk assessment, incident response, and certification evidence outside the control plane."),
	)
	for _, control := range report.Controls {
		switch control.Status {
		case "pass":
			report.Summary.Passed++
		case "manual_review":
			report.Summary.ManualReview++
		default:
			report.Summary.ActionNeeded++
		}
		report.Summary.Total++
	}
	return report, nil
}

func complianceControl(id string, title string, category string, frameworks []string, status string, evidence []string, recommendation string) ComplianceControl {
	return ComplianceControl{
		ID:             id,
		Title:          title,
		Category:       category,
		Frameworks:     frameworks,
		Status:         status,
		Evidence:       evidence,
		Recommendation: recommendation,
	}
}

func statusForBool(ok bool) string {
	if ok {
		return "pass"
	}
	return "action_needed"
}

func statusForFleet(total int, ok int) string {
	if total == 0 {
		return "manual_review"
	}
	return statusForBool(total == ok)
}
