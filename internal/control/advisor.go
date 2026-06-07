package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type AdvisorFinding struct {
	ID             string    `json:"id"`
	ProjectRef     string    `json:"project_ref"`
	Severity       string    `json:"severity"`
	Category       string    `json:"category"`
	Title          string    `json:"title"`
	Message        string    `json:"message"`
	Recommendation string    `json:"recommendation"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func FleetAdvisorFindings(ctx context.Context, store Store) ([]AdvisorFinding, error) {
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	findings := []AdvisorFinding{}
	posture, err := fleetRecoveryPosture(ctx, store)
	if err != nil {
		return nil, err
	}
	if !posture.RecoveryGuardEnabled {
		findings = append(findings, advisorFinding(now, "platform", "high", "recoverability", "Recovery-ready target guard is disabled", "Physical backups and WAL archives can be written to local-only or untested targets.", "Set SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true and restart the control plane before production project traffic."))
	}
	if !posture.DurableUpgradeGuardEnabled {
		findings = append(findings, advisorFinding(now, "platform", "medium", "recoverability", "Durable upgrade backup guard is disabled", "Project upgrades can proceed with local-only pre-upgrade backups.", "Set SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true so upgrades require a tested durable off-host backup artifact."))
	}
	if posture.RecoveryReadyTargets == 0 {
		findings = append(findings, advisorFinding(now, "platform", "high", "recoverability", "No recovery-ready backup target", "No S3-compatible target is tested, durable off-host, and recovery-ready.", "Add an off-host S3/R2/remote-MinIO target, run the server-side target test, and make it the platform default."))
	} else if !posture.DefaultRecoveryReadyTarget {
		findings = append(findings, advisorFinding(now, "platform", "medium", "recoverability", "No default recovery-ready backup target", "At least one target is recovery-ready, but none is the platform default for control-plane and unbound project backup uploads.", "Mark a tested durable off-host target as default or bind every project backup policy explicitly."))
	}
	for _, project := range projects {
		ref := project.Ref
		if project.Status != ProjectHealthy && project.Status != ProjectPaused {
			findings = append(findings, advisorFinding(now, ref, "critical", "health", "Project is not healthy", fmt.Sprintf("Project status is %s: %s", project.Status, strings.TrimSpace(project.Message)), "Inspect project logs and reconcile the project until it returns to healthy."))
		}
		if project.Status == ProjectPaused {
			findings = append(findings, advisorFinding(now, ref, "info", "operations", "Project is paused", "Paused projects are intentionally unavailable until resumed.", "Resume the project before expecting API, database, or function traffic to succeed."))
		}
		backupPolicy, err := store.GetBackupPolicy(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !backupPolicy.Enabled {
			findings = append(findings, advisorFinding(now, ref, "high", "recoverability", "Backups are disabled", "Daily logical backups are not scheduled for this project.", "Enable a backup policy and verify restore artifacts regularly."))
		}
		pitrPolicy, err := store.GetPITRPolicy(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !pitrPolicy.Enabled {
			findings = append(findings, advisorFinding(now, ref, "medium", "recoverability", "PITR is disabled", "WAL archiving is not enabled, so restore points are limited to discrete backups.", "Configure PITR with an archive bucket and an appropriate retention window."))
		}
		networkConfig, err := store.GetProjectConfig(ctx, ref, "network")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(networkConfig.Config["ip_allowlist"]) == "" {
			findings = append(findings, advisorFinding(now, ref, "medium", "security", "Ingress is open to all IPs", "No project-level IP allowlist is configured.", "Set a network IP allowlist for production projects or document why public ingress is required."))
		}
		databaseConfig, err := store.GetProjectConfig(ctx, ref, "database")
		if err != nil {
			return nil, err
		}
		if strings.ToLower(strings.TrimSpace(databaseConfig.Config["ssl_enforced"])) != "true" {
			findings = append(findings, advisorFinding(now, ref, "high", "security", "Database SSL is not enforced", "Database SSL enforcement is disabled in project config.", "Enable database SSL enforcement before allowing production database traffic."))
		}
		if strings.ToLower(strings.TrimSpace(databaseConfig.Config["performance_advisor_mode"])) != "fleet" {
			findings = append(findings, advisorFinding(now, ref, "low", "performance", "Fleet advisor mode is not enabled", "Security and performance advisor results are only available through per-project Studio for this project.", "Set database performance_advisor_mode=fleet to include the project in fleet-level advisor review."))
		}
		drains, err := store.ListProjectLogDrains(ctx, ref)
		if err != nil {
			return nil, err
		}
		if len(drains) == 0 {
			findings = append(findings, advisorFinding(now, ref, "low", "observability", "No log drains configured", "Project logs are not exported to an external destination.", "Configure a log drain for production observability and incident retention."))
		}
		buckets, err := store.ListProjectStorageBuckets(ctx, ref)
		if err != nil {
			return nil, err
		}
		for _, bucket := range buckets {
			if bucket.Public {
				findings = append(findings, advisorFinding(now, ref, "medium", "security", "Public storage bucket", fmt.Sprintf("Storage bucket %q is public.", bucket.Name), "Confirm public access is intentional and keep cache-control and object policies scoped."))
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		left := advisorSeverityRank(findings[i].Severity)
		right := advisorSeverityRank(findings[j].Severity)
		if left != right {
			return left < right
		}
		if findings[i].ProjectRef != findings[j].ProjectRef {
			return findings[i].ProjectRef < findings[j].ProjectRef
		}
		return findings[i].Title < findings[j].Title
	})
	return findings, nil
}

func advisorFinding(createdAt time.Time, ref string, severity string, category string, title string, message string, recommendation string) AdvisorFinding {
	if strings.TrimSpace(message) == "" {
		message = title
	}
	id := strings.ToLower(strings.ReplaceAll(ref+"-"+category+"-"+title, " ", "-"))
	id = strings.NewReplacer(":", "-", ".", "-", "/", "-").Replace(id)
	return AdvisorFinding{
		ID:             id,
		ProjectRef:     ref,
		Severity:       severity,
		Category:       category,
		Title:          title,
		Message:        message,
		Recommendation: recommendation,
		Status:         "open",
		CreatedAt:      createdAt,
	}
}

func advisorSeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	case "info":
		return 4
	default:
		return 5
	}
}
