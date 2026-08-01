package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type auditContextKey struct{}

type auditContextValue struct {
	actorID  string
	clientIP string
}

// WithAuditContext attaches the authenticated actor and client IP to a context
// so every Audit() call made downstream records WHO did the action and from
// WHERE, without each call site having to pass them. Set by the API auth
// middleware (per request) and by the login handler (which runs pre-auth).
func WithAuditContext(ctx context.Context, actorID, clientIP string) context.Context {
	return context.WithValue(ctx, auditContextKey{}, auditContextValue{actorID: strings.TrimSpace(actorID), clientIP: strings.TrimSpace(clientIP)})
}

func auditContextFrom(ctx context.Context) auditContextValue {
	v, _ := ctx.Value(auditContextKey{}).(auditContextValue)
	return v
}

func Audit(ctx context.Context, store Store, action string, target string, metadata map[string]string) {
	if store == nil {
		return
	}
	actor := auditContextFrom(ctx)
	// The hash chain already covers ActorID and (sorted) metadata, so the actor
	// and client_ip we add here are tamper-protected like every other field.
	if actor.clientIP != "" {
		merged := make(map[string]string, len(metadata)+1)
		for k, v := range metadata {
			merged[k] = v
		}
		if _, exists := merged["client_ip"]; !exists {
			merged["client_ip"] = actor.clientIP
		}
		metadata = merged
	}
	_, _ = store.RecordAuditEvent(ctx, AuditEventInput{
		ActorID:  actor.actorID,
		Action:   action,
		Target:   target,
		Metadata: metadata,
	})
}

func LogProject(ctx context.Context, store Store, ref string, level string, message string, metadata map[string]string) {
	if store == nil || ref == "" {
		return
	}
	_, _ = store.RecordProjectLog(ctx, ProjectLogInput{
		ProjectRef: ref,
		Level:      level,
		Message:    message,
		Metadata:   metadata,
	})
}

func SecretRevealFor(secret ProjectSecret) ProjectSecretReveal {
	return ProjectSecretReveal{
		Kind:      secret.Kind,
		Value:     secret.Value,
		CreatedAt: secret.CreatedAt,
		RotatedAt: secret.RotatedAt,
	}
}

func validateCreateProject(req CreateProjectRequest) error {
	if req.OrgID == "" {
		return fmt.Errorf("org id is required")
	}
	if !projectRefPattern.MatchString(req.Ref) {
		return fmt.Errorf("ref must be 3-55 lowercase letters, numbers, or hyphens, and cannot start or end with a hyphen")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	domain, err := normalizeDomain(req.Domain)
	if err != nil {
		return fmt.Errorf("domain %w", err)
	}
	if err := validateGeneratedProjectFQDNs(req.Ref, domain); err != nil {
		return err
	}
	if strings.TrimSpace(req.StackVersion) == "" {
		return fmt.Errorf("stack version is required")
	}
	if err := validateSupportedStackVersion(req.StackVersion); err != nil {
		return err
	}
	if err := validateStackProfile(req.Profile); err != nil {
		return err
	}
	if err := validateResourceTier(req.ResourceTier); err != nil {
		return err
	}
	if err := validateResourceSizing(req.CPU, req.RAMMB, req.DiskGB); err != nil {
		return err
	}
	if _, err := normalizeProjectServices(req.Services); err != nil {
		return err
	}
	if err := validateEnforcedResourceMinimum(req.toSpec()); err != nil {
		return err
	}
	return nil
}

// validateResourceSizing bounds optional exact-size overrides. A zero value
// means "use the recommended size" and is always valid; non-zero values must
// fall within sane platform limits.
func validateResourceSizing(cpu, ramMB, diskGB int) error {
	if cpu < 0 || ramMB < 0 || diskGB < 0 {
		return fmt.Errorf("resource sizing cannot be negative")
	}
	if cpu > maxProjectCPU {
		return fmt.Errorf("cpu cannot exceed %d cores", maxProjectCPU)
	}
	if ramMB > 0 && ramMB < minProjectRAMMB {
		return fmt.Errorf("ram cannot be below %d MB", minProjectRAMMB)
	}
	if ramMB > maxProjectRAMMB {
		return fmt.Errorf("ram cannot exceed %d MB", maxProjectRAMMB)
	}
	if diskGB > maxProjectDiskGB {
		return fmt.Errorf("disk cannot exceed %d GB", maxProjectDiskGB)
	}
	return nil
}

func validateEnforcedResourceMinimum(spec ProjectSpec) error {
	if !spec.EnforceLimits {
		return nil
	}
	reservation := resourceReservationForSpec(spec)
	minimum := minimumReservationForSpec(spec)
	if reservation.CPU < minimum.CPU || reservation.RAMMB < minimum.RAMMB || reservation.DiskGB < minimum.DiskGB {
		return fmt.Errorf("enforced limits cannot be below the selected stack minimum (%d CPU, %d MB RAM, %d GB disk)", minimum.CPU, minimum.RAMMB, minimum.DiskGB)
	}
	return nil
}

func validateGeneratedProjectFQDNs(ref string, domain string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	for label, host := range generatedProjectHosts(ref, domain) {
		if len(host) > 253 {
			return fmt.Errorf("%s host %s exceeds the 253-character DNS name limit; shorten the project ref or apps domain", label, host)
		}
	}
	return nil
}

func generatedProjectHosts(ref string, domain string) map[string]string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	return map[string]string{
		"project API":      projectHost(ref, domain),
		"project Studio":   studioHost(ref, domain),
		"project Storage":  storageHost(ref, domain),
		"project database": databaseHost(ref, domain),
		"project pooler":   poolerHost(ref, domain),
	}
}

func AllowedProjectServices() []string {
	return append([]string(nil), allowedProjectServices...)
}

func DefaultProjectServiceStates() map[string]bool {
	out := map[string]bool{}
	for _, name := range allowedProjectServices {
		out[name] = true
	}
	return out
}

func ProjectServiceStates(input map[string]ServiceSpec) map[string]bool {
	out := DefaultProjectServiceStates()
	for key, spec := range input {
		normalized, ok := normalizeProjectServiceName(key)
		if !ok {
			continue
		}
		out[normalized] = spec.Enabled
	}
	return out
}

func normalizeProjectServices(input map[string]bool) (map[string]ServiceSpec, error) {
	states := DefaultProjectServiceStates()
	for key, enabled := range input {
		normalized, ok := normalizeProjectServiceName(key)
		if !ok {
			return nil, fmt.Errorf("unsupported project service %q", key)
		}
		states[normalized] = enabled
	}
	out := map[string]ServiceSpec{}
	for _, name := range allowedProjectServices {
		out[name] = ServiceSpec{Enabled: states[name]}
	}
	return out, nil
}

func normalizeProjectServiceName(input string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	switch normalized {
	case "edge-runtime", "edge_runtime":
		normalized = "functions"
	case "supavisor":
		normalized = "pooler"
	case "logflare":
		normalized = "analytics"
	}
	for _, allowed := range allowedProjectServices {
		if normalized == allowed {
			return normalized, true
		}
	}
	return "", false
}

func normalizeConfigArea(area string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(area))
	if normalized == "" {
		return "", fmt.Errorf("config area is required")
	}
	if _, ok := allowedConfigAreas[normalized]; !ok {
		return "", fmt.Errorf("unsupported config area %q", normalized)
	}
	return normalized, nil
}

func normalizeConfigValues(values map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range values {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			return nil, fmt.Errorf("config key is required")
		}
		if strings.ContainsAny(normalizedKey, " /\\") {
			return nil, fmt.Errorf("config key %q is invalid", key)
		}
		out[normalizedKey] = strings.TrimSpace(value)
	}
	return out, nil
}

func validateGeneralConfig(config map[string]string) error {
	switch strings.ToLower(strings.TrimSpace(config["environment"])) {
	case "", "development", "production":
		return nil
	default:
		return fmt.Errorf("environment must be development or production")
	}
}

func validateNetworkConfig(config map[string]string) error {
	for _, key := range []string{"http_allowlist", "db_allowlist"} {
		for _, entry := range splitAllowlist(config[key]) {
			if _, err := netip.ParsePrefix(entry); err == nil {
				continue
			}
			if _, err := netip.ParseAddr(entry); err == nil {
				continue
			}
			return fmt.Errorf("invalid %s entry %q", key, entry)
		}
	}
	switch strings.ToLower(strings.TrimSpace(config["db_ingress_mode"])) {
	case "", "private", "allowlisted", "public":
	default:
		return fmt.Errorf("db_ingress_mode must be private, allowlisted, or public")
	}
	if strings.EqualFold(strings.TrimSpace(config["db_ingress_mode"]), "allowlisted") && len(splitAllowlist(config["db_allowlist"])) == 0 {
		return fmt.Errorf("allowlisted database ingress requires at least one db_allowlist entry")
	}
	return nil
}

func validateAuthConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "mfa_phone_otp_length", 4, 10); err != nil {
		return err
	}
	if maxFrequency := strings.TrimSpace(config["mfa_phone_max_frequency"]); maxFrequency != "" {
		if _, err := time.ParseDuration(maxFrequency); err != nil {
			return fmt.Errorf("mfa_phone_max_frequency must be a duration")
		}
	}
	provider := strings.ToLower(strings.TrimSpace(config["captcha_provider"]))
	if provider != "" {
		switch provider {
		case "hcaptcha", "turnstile":
		default:
			return fmt.Errorf("unsupported captcha_provider %q", provider)
		}
	}
	secretHandle := strings.TrimSpace(config["captcha_secret_handle"])
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return fmt.Errorf("captcha_secret_handle must be a secret:// handle")
	}
	return nil
}

func validateAuthProvidersConfig(config map[string]string) error {
	for key, value := range config {
		if strings.HasSuffix(key, "_secret_handle") || strings.HasSuffix(key, "_token_handle") || strings.HasSuffix(key, "_key_handle") || key == "sms_test_otp_handle" {
			if trimmed := strings.TrimSpace(value); trimmed != "" && !strings.HasPrefix(trimmed, "secret://") {
				return fmt.Errorf("%s must be a secret:// handle", key)
			}
		}
	}
	smsProvider := strings.ToLower(strings.TrimSpace(config["sms_provider"]))
	if smsProvider != "" {
		switch smsProvider {
		case "twilio", "twilio_verify", "messagebird", "textlocal", "vonage":
		default:
			return fmt.Errorf("unsupported sms_provider %q", smsProvider)
		}
	}
	if err := validateIntegerConfig(config, "sms_otp_exp", 1, 86400); err != nil {
		return err
	}
	if err := validateIntegerConfig(config, "sms_otp_length", 4, 10); err != nil {
		return err
	}
	if maxFrequency := strings.TrimSpace(config["sms_max_frequency"]); maxFrequency != "" {
		if _, err := time.ParseDuration(maxFrequency); err != nil {
			return fmt.Errorf("sms_max_frequency must be a duration")
		}
	}
	if validUntil := strings.TrimSpace(config["sms_test_otp_valid_until"]); validUntil != "" {
		if _, err := time.Parse(time.RFC3339, validUntil); err != nil {
			return fmt.Errorf("sms_test_otp_valid_until must be an RFC3339 timestamp")
		}
	}
	oidcIssuer := strings.TrimSpace(config["oauth_oidc_issuer_url"])
	if oidcIssuer != "" {
		parsed, err := url.Parse(oidcIssuer)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("oauth_oidc_issuer_url must be an https URL")
		}
	}
	return nil
}

func validateAIConfig(config map[string]string) error {
	for _, key := range []string{"openai_api_key_handle", "huggingface_api_key_handle", "studio_assistant_key_handle"} {
		value := strings.TrimSpace(config[key])
		if value != "" && !strings.HasPrefix(value, "secret://") {
			return fmt.Errorf("%s must be a secret:// handle", key)
		}
	}
	provider := strings.ToLower(strings.TrimSpace(config["default_embedding_provider"]))
	if provider == "" {
		provider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[provider]; !ok {
		return fmt.Errorf("unsupported default embedding provider %q", provider)
	}
	assistantProvider := strings.ToLower(strings.TrimSpace(config["studio_assistant_provider"]))
	if assistantProvider == "" {
		assistantProvider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[assistantProvider]; !ok {
		return fmt.Errorf("unsupported studio assistant provider %q", assistantProvider)
	}
	dimension := strings.TrimSpace(config["default_embedding_dimension"])
	if dimension != "" {
		value, err := strconv.Atoi(dimension)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("default_embedding_dimension must be between 1 and 65535")
		}
	}
	return nil
}

func validatePoolerConfig(config map[string]string) error {
	mode := strings.ToLower(strings.TrimSpace(config["pool_mode"]))
	if mode == "" {
		mode = "transaction"
	}
	switch mode {
	case "transaction", "session", "both":
	default:
		return fmt.Errorf("unsupported pool_mode %q", mode)
	}
	tier := strings.ToLower(strings.TrimSpace(config["dedicated_pooler_tier"]))
	if tier == "" {
		tier = "small"
	}
	switch tier {
	case "small", "medium", "large":
	default:
		return fmt.Errorf("unsupported dedicated_pooler_tier %q", tier)
	}
	if err := validateIntegerConfig(config, "default_pool_size", 1, 10000); err != nil {
		return err
	}
	if err := validateIntegerConfig(config, "max_client_connections", 1, 100000); err != nil {
		return err
	}
	if err := validateFixedIntegerConfig(config, "transaction_port", 6543); err != nil {
		return err
	}
	if err := validateFixedIntegerConfig(config, "session_port", 5432); err != nil {
		return err
	}
	return nil
}

func validateFunctionsConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "worker_timeout_ms", 100, 300000); err != nil {
		return err
	}
	policy := strings.ToLower(strings.TrimSpace(config["deployment_policy"]))
	if policy == "" {
		return nil
	}
	switch policy {
	case "manual", "ci", "locked":
		return nil
	default:
		return fmt.Errorf("unsupported deployment_policy %q", policy)
	}
}

func validateSMTPConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "port", 1, 65535); err != nil {
		return err
	}
	passwordHandle := strings.TrimSpace(config["password_handle"])
	if passwordHandle != "" && !strings.HasPrefix(passwordHandle, "secret://") {
		return fmt.Errorf("password_handle must be a secret:// handle")
	}
	mode := strings.ToLower(strings.TrimSpace(config["tls_mode"]))
	if mode == "" {
		return nil
	}
	switch mode {
	case "starttls", "implicit", "none":
		return nil
	default:
		return fmt.Errorf("unsupported smtp tls_mode %q", mode)
	}
}

func validateIntegerConfig(config map[string]string, key string, min int, max int) error {
	raw := strings.TrimSpace(config[key])
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return nil
}

func validateFixedIntegerConfig(config map[string]string, key string, expected int) error {
	raw := strings.TrimSpace(config[key])
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value != expected {
		return fmt.Errorf("%s is fixed at %d for hosted-compatible public routing", key, expected)
	}
	return nil
}

func normalizeMembershipRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	if normalized == "" {
		normalized = "developer"
	}
	if _, ok := allowedMembershipRoles[normalized]; !ok {
		return "", fmt.Errorf("unsupported membership role %q", normalized)
	}
	return normalized, nil
}

func normalizeTeamSlug(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func (s *MemoryStore) resolveAccessSubjectIDLocked(orgID string, subjectType string, subjectID string) (string, error) {
	switch subjectType {
	case "user":
		email := strings.ToLower(strings.TrimSpace(subjectID))
		user, ok := s.users[email]
		if ok {
			return user.ID, nil
		}
		for _, user := range s.users {
			if user.ID == subjectID {
				return user.ID, nil
			}
		}
		return "", fmt.Errorf("%w: user %s", ErrNotFound, subjectID)
	case "team":
		if team, ok := s.teams[orgID][normalizeTeamSlug(subjectID)]; ok {
			return team.ID, nil
		}
		for _, team := range s.teams[orgID] {
			if team.ID == subjectID {
				return team.ID, nil
			}
		}
		return "", fmt.Errorf("%w: team %s for org %s", ErrNotFound, subjectID, orgID)
	default:
		return "", fmt.Errorf("subject type must be user or team")
	}
}

func higherRole(left string, right string) string {
	if membershipRoleRank(right) > membershipRoleRank(left) {
		return right
	}
	return left
}

func mergeEffectiveRole(roles map[string]EffectiveProjectRole, userID string, email string, role string, source string) {
	current := roles[email]
	if current.Email == "" {
		current = EffectiveProjectRole{UserID: userID, Email: email, Role: role}
	}
	current.Sources = append(current.Sources, source)
	current.Role = higherRole(current.Role, role)
	roles[email] = current
}

func membershipRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return 4
	case "admin":
		return 3
	case "developer":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

func resourceReservationForTier(tier ResourceTier) HostCapacity {
	reservation, ok := resourceTierReservations[tier]
	if !ok {
		return resourceTierReservations[ResourceTierSmall]
	}
	return reservation
}

func defaultReplicaTierForProject(spec ProjectSpec) ResourceTier {
	if isReplicaResourceTier(spec.ResourceTier) {
		return spec.ResourceTier
	}
	return ResourceTierSmall
}

func validateReplicaResourceTier(tier ResourceTier) error {
	if isReplicaResourceTier(tier) {
		return nil
	}
	return fmt.Errorf("unsupported replica resource tier %q; replicas support small, medium, or large", tier)
}

func isReplicaResourceTier(tier ResourceTier) bool {
	_, ok := resourceTierReservations[tier]
	return ok
}

func minimumReservationForSpec(spec ProjectSpec) HostCapacity {
	states := ProjectServiceStates(spec.Services)
	if len(spec.Services) == 0 {
		states = DefaultProjectServiceStates()
	}
	ramMB := 2048
	cpuUnits := 100
	diskGB := 20
	if states["auth"] {
		ramMB += 256
		cpuUnits += 20
	}
	if states["rest"] {
		ramMB += 256
		cpuUnits += 20
	}
	if states["realtime"] {
		ramMB += 512
		cpuUnits += 30
	}
	if states["storage"] {
		ramMB += 512
		cpuUnits += 30
		diskGB += 20
	}
	if states["imgproxy"] {
		ramMB += 256
		cpuUnits += 20
	}
	if states["functions"] {
		ramMB += 512
		cpuUnits += 20
	}
	if states["pooler"] {
		ramMB += 256
		cpuUnits += 10
	}
	if states["studio"] {
		ramMB += 512
		cpuUnits += 10
	}
	if states["analytics"] {
		ramMB += 1024
		cpuUnits += 30
		diskGB += 10
	}
	if states["vector"] {
		ramMB += 256
		cpuUnits += 10
	}
	if states["graphql"] {
		ramMB += 128
		cpuUnits += 10
	}
	if spec.Profile == StackProfileOrioleDB {
		ramMB += 1024
		cpuUnits += 50
		diskGB += 20
	}
	cpu := (cpuUnits + 99) / 100
	if cpu < 1 {
		cpu = 1
	}
	if ramMB%512 != 0 {
		ramMB = ((ramMB / 512) + 1) * 512
	}
	if diskGB < 20 {
		diskGB = 20
	}
	return HostCapacity{CPU: cpu, RAMMB: ramMB, DiskGB: diskGB, Project: 1}
}

func recommendedReservationForSpec(spec ProjectSpec) HostCapacity {
	minimum := minimumReservationForSpec(spec)
	return HostCapacity{
		CPU:     clampInt(addPercentRoundUp(minimum.CPU, recommendedCPUHeadroomPercent), 1, maxProjectCPU),
		RAMMB:   clampInt(roundUpInt(addPercentRoundUp(minimum.RAMMB, recommendedRAMHeadroomPercent), recommendedRAMRoundingStepMB), minProjectRAMMB, maxProjectRAMMB),
		DiskGB:  clampInt(roundUpInt(addPercentRoundUp(minimum.DiskGB, recommendedDiskHeadroomPercent), recommendedDiskRoundingStepGB), 1, maxProjectDiskGB),
		Project: minimum.Project,
	}
}

func addPercentRoundUp(value int, percent int) int {
	if value <= 0 {
		return 0
	}
	return (value*(100+percent) + 99) / 100
}

func roundUpInt(value int, step int) int {
	if value <= 0 || step <= 1 {
		return value
	}
	return ((value + step - 1) / step) * step
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// resourceReservationForSpec resolves the capacity actually reserved for a
// project. New projects carry exact custom sizing. Historical tier values are
// still understood as internal aliases.
func resourceReservationForSpec(spec ProjectSpec) HostCapacity {
	reservation := resourceReservationForTier(spec.ResourceTier)
	if spec.ResourceTier == ResourceTierCustom {
		reservation = recommendedReservationForSpec(spec)
	}
	if spec.CPU > 0 {
		reservation.CPU = spec.CPU
	}
	if spec.RAMMB > 0 {
		reservation.RAMMB = spec.RAMMB
	}
	if spec.DiskGB > 0 {
		reservation.DiskGB = spec.DiskGB
	}
	return reservation
}

// EffectiveResourceSizing resolves the CPU cores, RAM (MB), and disk (GB) for a
// spec. It is the single source of truth shared by capacity accounting and the
// provisioner's optional runtime-limit enforcement.
func EffectiveResourceSizing(spec ProjectSpec) (cpu int, ramMB int, diskGB int) {
	reservation := resourceReservationForSpec(spec)
	return reservation.CPU, reservation.RAMMB, reservation.DiskGB
}

type projectServiceResourceWeight struct {
	name      string
	cpuWeight int
	ramWeight int
}

// ProjectServiceResourceAllocations splits an enforced project CPU/RAM budget
// across every runtime service that has its own container. The allocation is
// per-container, because Docker Compose and Kubernetes limits are container
// scoped rather than aggregate project-scoped. GraphQL is intentionally absent:
// it is a Postgres extension in this stack, not a separate workload.
func ProjectServiceResourceAllocations(spec ProjectSpec) map[string]ProjectServiceResourceAllocation {
	if !spec.EnforceLimits {
		return map[string]ProjectServiceResourceAllocation{}
	}
	cpu, ramMB, _ := EffectiveResourceSizing(spec)
	if cpu <= 0 || ramMB <= 0 {
		return map[string]ProjectServiceResourceAllocation{}
	}
	services := projectServiceResourceWeights(spec)
	cpuParts := distributeWeightedInt(cpu*1000, services, func(service projectServiceResourceWeight) int {
		return service.cpuWeight
	})
	ramParts := distributeWeightedInt(ramMB, services, func(service projectServiceResourceWeight) int {
		return service.ramWeight
	})
	out := make(map[string]ProjectServiceResourceAllocation, len(services))
	for _, service := range services {
		out[service.name] = ProjectServiceResourceAllocation{
			CPUMilli: cpuParts[service.name],
			RAMMB:    ramParts[service.name],
		}
	}
	return out
}

func projectServiceResourceWeights(spec ProjectSpec) []projectServiceResourceWeight {
	states := ProjectServiceStates(spec.Services)
	services := []projectServiceResourceWeight{
		{name: "db", cpuWeight: 100, ramWeight: 2048},
		{name: "kong", cpuWeight: 40, ramWeight: 768},
		{name: "meta", cpuWeight: 15, ramWeight: 256},
	}
	if states["auth"] {
		services = append(services, projectServiceResourceWeight{name: "auth", cpuWeight: 20, ramWeight: 256})
	}
	if states["rest"] {
		services = append(services, projectServiceResourceWeight{name: "rest", cpuWeight: 20, ramWeight: 256})
	}
	if states["realtime"] {
		services = append(services, projectServiceResourceWeight{name: "realtime", cpuWeight: 30, ramWeight: 512})
	}
	if states["storage"] {
		services = append(services, projectServiceResourceWeight{name: "storage", cpuWeight: 30, ramWeight: 512})
	}
	if states["imgproxy"] {
		services = append(services, projectServiceResourceWeight{name: "imgproxy", cpuWeight: 20, ramWeight: 256})
	}
	if states["functions"] {
		services = append(services, projectServiceResourceWeight{name: "functions", cpuWeight: 20, ramWeight: 512})
	}
	if states["pooler"] {
		services = append(services, projectServiceResourceWeight{name: "pooler", cpuWeight: 10, ramWeight: 256})
	}
	if states["studio"] {
		services = append(services, projectServiceResourceWeight{name: "studio", cpuWeight: 10, ramWeight: 512})
	}
	if states["analytics"] {
		services = append(services, projectServiceResourceWeight{name: "analytics", cpuWeight: 30, ramWeight: 1024})
	}
	if states["vector"] {
		services = append(services, projectServiceResourceWeight{name: "vector", cpuWeight: 10, ramWeight: 256})
	}
	return services
}

type weightedIntRemainder struct {
	name      string
	remainder int
	index     int
}

func distributeWeightedInt(total int, services []projectServiceResourceWeight, weight func(projectServiceResourceWeight) int) map[string]int {
	out := make(map[string]int, len(services))
	if total <= 0 || len(services) == 0 {
		return out
	}
	totalWeight := 0
	for _, service := range services {
		if value := weight(service); value > 0 {
			totalWeight += value
		}
	}
	if totalWeight <= 0 {
		share := total / len(services)
		remainder := total % len(services)
		for index, service := range services {
			out[service.name] = share
			if index < remainder {
				out[service.name]++
			}
		}
		return out
	}
	allocated := 0
	remainders := make([]weightedIntRemainder, 0, len(services))
	for index, service := range services {
		value := weight(service)
		if value <= 0 {
			continue
		}
		raw := total * value
		base := raw / totalWeight
		out[service.name] = base
		allocated += base
		remainders = append(remainders, weightedIntRemainder{name: service.name, remainder: raw % totalWeight, index: index})
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].remainder != remainders[j].remainder {
			return remainders[i].remainder > remainders[j].remainder
		}
		return remainders[i].index < remainders[j].index
	})
	for index := 0; index < total-allocated && index < len(remainders); index++ {
		out[remainders[index].name]++
	}
	return out
}

func replicaReservationForTier(tier ResourceTier) HostCapacity {
	reservation := resourceReservationForTier(tier)
	reservation.Project = 0
	return reservation
}

func (s *MemoryStore) projectReplicaRoutingLocked(project Project, replicas []ProjectReplica) ProjectReplicaRouting {
	targets := make([]ProjectReplicaRouteTarget, 0, len(replicas))
	var candidate *ProjectReplicaRouteTarget
	primaryReplicaID := ""
	for index, replica := range replicas {
		replica = normalizedReplicaForRouting(replica, index)
		target := replicaRouteTarget(replica)
		targets = append(targets, target)
		if replica.Role == "primary" {
			primaryReplicaID = replica.ID
		}
		if replica.Status == "healthy" && replica.Role != "primary" {
			next := target
			if candidate == nil || compareReplicaRouteTarget(next, *candidate) < 0 {
				candidate = &next
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return compareReplicaRouteTarget(targets[i], targets[j]) < 0
	})
	healthyReadTargets := make([]ProjectReplicaRouteTarget, 0, len(targets))
	for _, target := range targets {
		if target.Status == "healthy" && target.Role != "primary" && target.Weight > 0 {
			healthyReadTargets = append(healthyReadTargets, target)
		}
	}
	return ProjectReplicaRouting{
		ProjectRef:         project.Ref,
		PrimaryURI:         projectPrimaryURI(project),
		ReadStrategy:       "weighted-healthy",
		AutoFailover:       true,
		PrimaryReplicaID:   primaryReplicaID,
		FailoverCandidate:  candidate,
		HealthyReadTargets: healthyReadTargets,
		AllTargets:         targets,
	}
}

func normalizedReplicaForRouting(replica ProjectReplica, index int) ProjectReplica {
	if replica.Role == "" {
		replica.Role = "read"
	}
	if replica.ReadWeight <= 0 {
		replica.ReadWeight = 100
	}
	if replica.FailoverPriority <= 0 {
		replica.FailoverPriority = index + 1
	}
	return replica
}

func replicaRouteTarget(replica ProjectReplica) ProjectReplicaRouteTarget {
	return ProjectReplicaRouteTarget{
		ReplicaID:             replica.ID,
		Name:                  replica.Name,
		URI:                   replica.ReadURI,
		Region:                replica.Region,
		Weight:                replica.ReadWeight,
		FailoverPriority:      replica.FailoverPriority,
		ReplicationLagBytes:   replica.ReplicationLagBytes,
		ReplicationLagSeconds: replica.ReplicationLagSeconds,
		Role:                  replica.Role,
		Status:                replica.Status,
	}
}

func compareReplicaFailoverCandidate(left ProjectReplica, right ProjectReplica) int {
	return compareReplicaRouteTarget(replicaRouteTarget(normalizedReplicaForRouting(left, 0)), replicaRouteTarget(normalizedReplicaForRouting(right, 0)))
}

func compareReplicaRouteTarget(left ProjectReplicaRouteTarget, right ProjectReplicaRouteTarget) int {
	if left.FailoverPriority != right.FailoverPriority {
		return left.FailoverPriority - right.FailoverPriority
	}
	if left.ReplicationLagSeconds != right.ReplicationLagSeconds {
		return left.ReplicationLagSeconds - right.ReplicationLagSeconds
	}
	if left.ReplicationLagBytes != right.ReplicationLagBytes {
		if left.ReplicationLagBytes < right.ReplicationLagBytes {
			return -1
		}
		return 1
	}
	return strings.Compare(left.Name, right.Name)
}

func projectPrimaryURI(project Project) string {
	return fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@db.%s.internal:5432/postgres", project.Ref)
}

func defaultReplicaMessage(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func hostHasCapacity(host Host, reservation HostCapacity) bool {
	next := addHostCapacity(host.Used, reservation)
	return capacityWithinLimit(next.CPU, host.Capacity.CPU) &&
		capacityWithinLimit(next.RAMMB, host.Capacity.RAMMB) &&
		capacityWithinLimit(next.DiskGB, host.Capacity.DiskGB) &&
		capacityWithinLimit(next.Project, host.Capacity.Project)
}

func capacityWithinLimit(next int, limit int) bool {
	return limit <= 0 || next <= limit
}

func quotaWithinLimit(next int, limit int) bool {
	return limit <= 0 || next <= limit
}

func (s *MemoryStore) validateOrgQuotaLocked(orgID string, reservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(quota.Used, reservation)
	if !quotaWithinLimit(next.Project, quota.MaxProjects) {
		return fmt.Errorf("%w: org %s project quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) validateOrgReplicaQuotaLocked(orgID string, reservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(quota.Used, reservation)
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) validateProjectScaleQuotaLocked(orgID string, oldReservation HostCapacity, newReservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(subtractHostCapacity(quota.Used, oldReservation), newReservation)
	if !quotaWithinLimit(next.Project, quota.MaxProjects) {
		return fmt.Errorf("%w: org %s project quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) orgQuotaLocked(orgID string) OrgQuota {
	quota := s.orgQuotas[orgID]
	if quota.OrgID == "" {
		quota.OrgID = orgID
		quota.UpdatedAt = time.Now().UTC()
	}
	quota.Used = s.orgUsageLocked(orgID)
	return quota
}

func (s *MemoryStore) orgUsageLocked(orgID string) HostCapacity {
	usage := HostCapacity{}
	for _, project := range s.projects {
		if project.OrgID == orgID {
			usage = addHostCapacity(usage, resourceReservationForSpec(project.Spec))
		}
	}
	for ref, replicas := range s.replicas {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		for _, replica := range replicas {
			usage = addHostCapacity(usage, replicaReservationForTier(replica.Tier))
		}
	}
	return usage
}

func (s *MemoryStore) userByIDLocked(id string) (User, bool) {
	for _, user := range s.users {
		if user.ID == id {
			return user, true
		}
	}
	return User{}, false
}

func mfaStatusForUser(user User) MFAStatus {
	var confirmedAt *time.Time
	if !user.MFAConfirmedAt.IsZero() {
		confirmedAt = &user.MFAConfirmedAt
	}
	updatedAt := user.MFAUpdatedAt
	if updatedAt.IsZero() {
		updatedAt = user.CreatedAt
	}
	return MFAStatus{
		UserID:      user.ID,
		Email:       user.Email,
		Enabled:     user.MFAEnabled,
		Pending:     user.MFAPendingSecret != "",
		ConfirmedAt: confirmedAt,
		UpdatedAt:   updatedAt,
	}
}

func hashAuditEvent(event AuditEvent) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%d\n", event.ChainIndex))
	builder.WriteString(event.PreviousHash)
	builder.WriteByte('\n')
	builder.WriteString(event.ID)
	builder.WriteByte('\n')
	builder.WriteString(event.ActorID)
	builder.WriteByte('\n')
	builder.WriteString(event.Action)
	builder.WriteByte('\n')
	builder.WriteString(event.Target)
	builder.WriteByte('\n')
	builder.WriteString(event.CreatedAt.UTC().Format(time.RFC3339Nano))
	builder.WriteByte('\n')
	keys := make([]string, 0, len(event.Metadata))
	for key := range event.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(event.Metadata[key])
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func addHostCapacity(left HostCapacity, right HostCapacity) HostCapacity {
	return HostCapacity{
		CPU:     left.CPU + right.CPU,
		RAMMB:   left.RAMMB + right.RAMMB,
		DiskGB:  left.DiskGB + right.DiskGB,
		Project: left.Project + right.Project,
	}
}

func subtractHostCapacity(left HostCapacity, right HostCapacity) HostCapacity {
	return HostCapacity{
		CPU:     maxInt(0, left.CPU-right.CPU),
		RAMMB:   maxInt(0, left.RAMMB-right.RAMMB),
		DiskGB:  maxInt(0, left.DiskGB-right.DiskGB),
		Project: maxInt(0, left.Project-right.Project),
	}
}

func telemetryHistorySampleFromTelemetry(sample TelemetrySample, reservation HostCapacity) TelemetryHistorySample {
	return TelemetryHistorySample{
		ProjectRef:               sample.ProjectRef,
		Source:                   sample.Source,
		Samples:                  1,
		CPUPercent:               sample.CPUPercent,
		CPUReservationPercent:    cpuReservationPercent(sample.CPUPercent, reservation.CPU),
		MemoryBytes:              sample.MemoryBytes,
		MemoryLimitBytes:         sample.MemoryLimitBytes,
		MemoryReservationPercent: byteReservationPercent(sample.MemoryBytes, int64(reservation.RAMMB)*1024*1024),
		DiskUsedBytes:            sample.DiskUsedBytes,
		DiskLimitBytes:           sample.DiskLimitBytes,
		DiskReservationPercent:   byteReservationPercent(sample.DiskUsedBytes, int64(reservation.DiskGB)*1024*1024*1024),
		NetworkRxBytes:           sample.NetworkRxBytes,
		NetworkTxBytes:           sample.NetworkTxBytes,
		ReservedCPU:              reservation.CPU,
		ReservedRAMMB:            reservation.RAMMB,
		ReservedDiskGB:           reservation.DiskGB,
		SampledAt:                sample.SampledAt.UTC(),
	}
}

func compactTelemetryHistory(samples []TelemetryHistorySample, now time.Time) []TelemetryHistorySample {
	if len(samples) == 0 {
		return nil
	}
	now = now.UTC()
	keepAfter := now.Add(-telemetryHistoryRetention)
	rawAfter := now.Add(-telemetryHistoryRawRetention)
	raw := make([]TelemetryHistorySample, 0, len(samples))
	rollups := map[time.Time]*telemetryHistoryAccumulator{}
	for _, sample := range samples {
		if sample.Samples <= 0 {
			sample.Samples = 1
		}
		sample.SampledAt = sample.SampledAt.UTC()
		if sample.SampledAt.Before(keepAfter) {
			continue
		}
		if !sample.SampledAt.Before(rawAfter) {
			raw = append(raw, sample)
			continue
		}
		bucket := sample.SampledAt.Truncate(telemetryHistoryRollupStep)
		acc := rollups[bucket]
		if acc == nil {
			acc = &telemetryHistoryAccumulator{sampledAt: bucket}
			rollups[bucket] = acc
		}
		acc.add(sample)
	}

	out := make([]TelemetryHistorySample, 0, len(raw)+len(rollups))
	for _, acc := range rollups {
		out = append(out, acc.point())
	}
	out = append(out, raw...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].SampledAt.Before(out[j].SampledAt)
	})
	return out
}

func normalizeTelemetryHistoryQuery(query TelemetryHistoryQuery, now time.Time) TelemetryHistoryQuery {
	now = now.UTC()
	to := query.To.UTC()
	if to.IsZero() || to.After(now) {
		to = now
	}
	from := query.From.UTC()
	if from.IsZero() {
		from = to.Add(-1 * time.Hour)
	}
	minFrom := to.Add(-telemetryHistoryRetention)
	if from.Before(minFrom) {
		from = minFrom
	}
	if !from.Before(to) {
		from = to.Add(-1 * time.Hour)
	}
	if from.Before(minFrom) {
		from = minFrom
	}
	step := query.Step
	if step <= 0 {
		step = telemetryHistoryDefaultStep(to.Sub(from))
	}
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	if step > 24*time.Hour {
		step = 24 * time.Hour
	}
	limit := query.Limit
	if limit <= 0 || limit > telemetryHistoryMaxPoints {
		limit = telemetryHistoryMaxPoints
	}
	return TelemetryHistoryQuery{From: from, To: to, Step: step, Limit: limit}
}

func telemetryHistoryDefaultStep(window time.Duration) time.Duration {
	switch {
	case window <= time.Hour:
		return 15 * time.Second
	case window <= 6*time.Hour:
		return time.Minute
	case window <= 24*time.Hour:
		return 5 * time.Minute
	case window <= 7*24*time.Hour:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

func telemetryHistoryPoints(samples []TelemetryHistorySample, query TelemetryHistoryQuery) []ProjectTelemetryHistoryPoint {
	if len(samples) == 0 {
		return []ProjectTelemetryHistoryPoint{}
	}
	buckets := map[time.Time]*telemetryHistoryAccumulator{}
	for _, sample := range samples {
		if sample.SampledAt.Before(query.From) || sample.SampledAt.After(query.To) {
			continue
		}
		bucket := sample.SampledAt.Truncate(query.Step)
		if bucket.Before(query.From) {
			bucket = query.From
		}
		acc := buckets[bucket]
		if acc == nil {
			acc = &telemetryHistoryAccumulator{sampledAt: bucket}
			buckets[bucket] = acc
		}
		acc.add(sample)
	}
	points := make([]ProjectTelemetryHistoryPoint, 0, len(buckets))
	for _, acc := range buckets {
		point := acc.historyPoint()
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].SampledAt.Before(points[j].SampledAt)
	})
	if query.Limit > 0 && len(points) > query.Limit {
		points = points[len(points)-query.Limit:]
	}
	return points
}

type telemetryHistoryAccumulator struct {
	sampledAt                time.Time
	projectRef               string
	source                   string
	samples                  int
	cpuPercent               float64
	cpuReservationPercent    float64
	memoryBytes              float64
	memoryLimitBytes         float64
	memoryReservationPercent float64
	diskUsedBytes            float64
	diskLimitBytes           float64
	diskReservationPercent   float64
	networkRxBytes           float64
	networkTxBytes           float64
	reservedCPU              float64
	reservedRAMMB            float64
	reservedDiskGB           float64
}

func (a *telemetryHistoryAccumulator) add(sample TelemetryHistorySample) {
	weight := sample.Samples
	if weight <= 0 {
		weight = 1
	}
	if a.projectRef == "" {
		a.projectRef = sample.ProjectRef
	}
	if a.source == "" {
		a.source = sample.Source
	} else if sample.Source != "" && a.source != sample.Source {
		a.source = "mixed"
	}
	if a.sampledAt.IsZero() {
		a.sampledAt = sample.SampledAt
	}
	w := float64(weight)
	a.samples += weight
	a.cpuPercent += sample.CPUPercent * w
	a.cpuReservationPercent += sample.CPUReservationPercent * w
	a.memoryBytes += float64(sample.MemoryBytes) * w
	a.memoryLimitBytes += float64(sample.MemoryLimitBytes) * w
	a.memoryReservationPercent += sample.MemoryReservationPercent * w
	a.diskUsedBytes += float64(sample.DiskUsedBytes) * w
	a.diskLimitBytes += float64(sample.DiskLimitBytes) * w
	a.diskReservationPercent += sample.DiskReservationPercent * w
	a.networkRxBytes += float64(sample.NetworkRxBytes) * w
	a.networkTxBytes += float64(sample.NetworkTxBytes) * w
	a.reservedCPU += float64(sample.ReservedCPU) * w
	a.reservedRAMMB += float64(sample.ReservedRAMMB) * w
	a.reservedDiskGB += float64(sample.ReservedDiskGB) * w
}

func (a telemetryHistoryAccumulator) point() TelemetryHistorySample {
	samples := maxInt(1, a.samples)
	return TelemetryHistorySample{
		ProjectRef:               a.projectRef,
		Source:                   a.source,
		Samples:                  samples,
		CPUPercent:               a.avg(a.cpuPercent),
		CPUReservationPercent:    a.avg(a.cpuReservationPercent),
		MemoryBytes:              int64(a.avg(a.memoryBytes)),
		MemoryLimitBytes:         int64(a.avg(a.memoryLimitBytes)),
		MemoryReservationPercent: a.avg(a.memoryReservationPercent),
		DiskUsedBytes:            int64(a.avg(a.diskUsedBytes)),
		DiskLimitBytes:           int64(a.avg(a.diskLimitBytes)),
		DiskReservationPercent:   a.avg(a.diskReservationPercent),
		NetworkRxBytes:           int64(a.avg(a.networkRxBytes)),
		NetworkTxBytes:           int64(a.avg(a.networkTxBytes)),
		ReservedCPU:              int(a.avg(a.reservedCPU)),
		ReservedRAMMB:            int(a.avg(a.reservedRAMMB)),
		ReservedDiskGB:           int(a.avg(a.reservedDiskGB)),
		SampledAt:                a.sampledAt,
	}
}

func (a telemetryHistoryAccumulator) historyPoint() ProjectTelemetryHistoryPoint {
	sample := a.point()
	return ProjectTelemetryHistoryPoint{
		SampledAt:                sample.SampledAt,
		Source:                   sample.Source,
		Samples:                  sample.Samples,
		CPUPercent:               sample.CPUPercent,
		CPUReservationPercent:    sample.CPUReservationPercent,
		MemoryBytes:              sample.MemoryBytes,
		MemoryLimitBytes:         sample.MemoryLimitBytes,
		MemoryReservationPercent: sample.MemoryReservationPercent,
		DiskUsedBytes:            sample.DiskUsedBytes,
		DiskLimitBytes:           sample.DiskLimitBytes,
		DiskReservationPercent:   sample.DiskReservationPercent,
		NetworkRxBytes:           sample.NetworkRxBytes,
		NetworkTxBytes:           sample.NetworkTxBytes,
		ReservedCPU:              sample.ReservedCPU,
		ReservedRAMMB:            sample.ReservedRAMMB,
		ReservedDiskGB:           sample.ReservedDiskGB,
	}
}

func (a telemetryHistoryAccumulator) avg(value float64) float64 {
	if a.samples <= 0 {
		return 0
	}
	return value / float64(a.samples)
}

func cpuReservationPercent(cpuPercent float64, reservedCPU int) float64 {
	if reservedCPU <= 0 {
		return 0
	}
	usedCores := cpuPercent / 100
	return (usedCores / float64(reservedCPU)) * 100
}

func byteReservationPercent(used int64, reserved int64) float64 {
	if reserved <= 0 {
		return 0
	}
	return (float64(used) / float64(reserved)) * 100
}

func telemetryRollup(projects map[string]Project, samples map[string]TelemetrySample, now time.Time) TelemetryRollup {
	const staleAfter = 5 * time.Minute
	rollup := TelemetryRollup{StaleAfterSeconds: int(staleAfter.Seconds())}
	for ref := range projects {
		sample, ok := samples[ref]
		if !ok {
			continue
		}
		if rollup.LatestSampledAt.IsZero() || sample.SampledAt.After(rollup.LatestSampledAt) {
			rollup.LatestSampledAt = sample.SampledAt
		}
		if rollup.OldestSampledAt.IsZero() || sample.SampledAt.Before(rollup.OldestSampledAt) {
			rollup.OldestSampledAt = sample.SampledAt
		}
		if now.Sub(sample.SampledAt) > staleAfter {
			rollup.StaleProjects++
			continue
		}
		rollup.ProjectsSampled++
		rollup.CPUPercent += sample.CPUPercent
		rollup.MemoryBytes += sample.MemoryBytes
		rollup.MemoryLimitBytes += sample.MemoryLimitBytes
		rollup.DiskUsedBytes += sample.DiskUsedBytes
		rollup.DiskLimitBytes += sample.DiskLimitBytes
		rollup.NetworkRxBytes += sample.NetworkRxBytes
		rollup.NetworkTxBytes += sample.NetworkTxBytes
	}
	return rollup
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
