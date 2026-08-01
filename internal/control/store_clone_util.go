package control

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func defaultProjectConfig(ref string, area string) ProjectConfig {
	return ProjectConfig{
		ProjectRef: ref,
		Area:       area,
		Config:     cloneStringMap(allowedConfigAreas[area]),
		UpdatedAt:  time.Now().UTC(),
	}
}

func mergeProjectConfigWithDefaults(ref string, area string, config ProjectConfig) ProjectConfig {
	merged := defaultProjectConfig(ref, area)
	merged.UpdatedAt = config.UpdatedAt
	for key, value := range config.Config {
		merged.Config[key] = value
	}
	return merged
}

func cloneProjectConfig(config ProjectConfig) ProjectConfig {
	config.Config = cloneStringMap(config.Config)
	return config
}

func cloneAuthClients(clients []ProjectAuthClient) []ProjectAuthClient {
	out := append([]ProjectAuthClient(nil), clients...)
	for index := range out {
		out[index] = cloneAuthClient(out[index])
	}
	return out
}

func cloneAuthClient(client ProjectAuthClient) ProjectAuthClient {
	client.RedirectURIs = append([]string(nil), client.RedirectURIs...)
	client.GrantTypes = append([]string(nil), client.GrantTypes...)
	client.Scopes = append([]string(nil), client.Scopes...)
	return client
}

func cloneAuthHooks(hooks []ProjectAuthHook) []ProjectAuthHook {
	out := append([]ProjectAuthHook(nil), hooks...)
	for index := range out {
		out[index] = cloneAuthHook(out[index])
	}
	return out
}

func cloneAuthHook(hook ProjectAuthHook) ProjectAuthHook {
	hook.Headers = cloneStringMap(hook.Headers)
	hook.RuntimeHeaders = cloneStringMap(hook.RuntimeHeaders)
	return hook
}

func cloneProjectRoutes(routes []ProjectRoute) []ProjectRoute {
	out := append([]ProjectRoute(nil), routes...)
	for index := range out {
		out[index].IPAllowlist = append([]string(nil), out[index].IPAllowlist...)
	}
	return out
}

func cloneProjectCDNPolicy(policy ProjectCDNPolicy) ProjectCDNPolicy {
	policy.IncludedPaths = append([]string{}, policy.IncludedPaths...)
	policy.ExcludedPaths = append([]string{}, policy.ExcludedPaths...)
	return policy
}

func cloneCDNInvalidations(invalidations []CDNInvalidation) []CDNInvalidation {
	out := append([]CDNInvalidation(nil), invalidations...)
	for index := range out {
		out[index] = cloneCDNInvalidation(out[index])
	}
	return out
}

func cloneCDNInvalidation(invalidation CDNInvalidation) CDNInvalidation {
	invalidation.Paths = append([]string(nil), invalidation.Paths...)
	if invalidation.Source == "" {
		invalidation.Source = "manual"
	}
	if invalidation.CompletedAt != nil {
		completed := *invalidation.CompletedAt
		invalidation.CompletedAt = &completed
	}
	return invalidation
}

func cloneNetworkConnections(connections []ProjectNetworkConnection) []ProjectNetworkConnection {
	out := append([]ProjectNetworkConnection(nil), connections...)
	for index := range out {
		out[index] = cloneNetworkConnection(out[index])
	}
	return out
}

func cloneNetworkConnection(connection ProjectNetworkConnection) ProjectNetworkConnection {
	connection.CIDRs = append([]string(nil), connection.CIDRs...)
	connection.Config = cloneStringMap(connection.Config)
	return connection
}

func cloneUsageSnapshots(snapshots []UsageSnapshot) []UsageSnapshot {
	out := append([]UsageSnapshot(nil), snapshots...)
	for index := range out {
		out[index] = cloneUsageSnapshot(out[index])
	}
	return out
}

func cloneUsageSnapshot(snapshot UsageSnapshot) UsageSnapshot {
	snapshot.Metrics = cloneOrgUsage(snapshot.Metrics)
	return snapshot
}

func cloneBillingInvoices(invoices []BillingInvoice) []BillingInvoice {
	out := append([]BillingInvoice(nil), invoices...)
	for index := range out {
		out[index] = cloneBillingInvoice(out[index])
	}
	return out
}

func cloneBillingInvoice(invoice BillingInvoice) BillingInvoice {
	invoice.LineItems = append([]BillingLineItem(nil), invoice.LineItems...)
	invoice.Metrics = cloneOrgUsage(invoice.Metrics)
	return invoice
}

func cloneOrgUsage(usage OrgUsage) OrgUsage {
	usage.ProjectsByStatus = cloneIntMap(usage.ProjectsByStatus)
	return usage
}

func cloneIntMap(input map[string]int) map[string]int {
	out := map[string]int{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneProjectFunctions(functions []ProjectFunction) []ProjectFunction {
	out := append([]ProjectFunction(nil), functions...)
	for index := range out {
		out[index] = cloneProjectFunction(out[index])
	}
	return out
}

func cloneProjectFunction(function ProjectFunction) ProjectFunction {
	function.Secrets = cloneStringMap(function.Secrets)
	return function
}

func cloneProjectFunctionRegions(regions []ProjectFunctionRegion) []ProjectFunctionRegion {
	return append([]ProjectFunctionRegion(nil), regions...)
}

func cloneProjectFunctionRegion(region ProjectFunctionRegion) ProjectFunctionRegion {
	return region
}

func cloneProjectFunctionStorageMounts(mounts []ProjectFunctionStorageMount) []ProjectFunctionStorageMount {
	return append([]ProjectFunctionStorageMount(nil), mounts...)
}

func cloneProjectFunctionStorageMount(mount ProjectFunctionStorageMount) ProjectFunctionStorageMount {
	return mount
}

func cloneReplicationPipelines(pipelines []ProjectReplicationPipeline) []ProjectReplicationPipeline {
	out := append([]ProjectReplicationPipeline(nil), pipelines...)
	for index := range out {
		out[index] = cloneReplicationPipeline(out[index])
	}
	return out
}

func cloneReplicationPipeline(pipeline ProjectReplicationPipeline) ProjectReplicationPipeline {
	pipeline.Config = cloneStringMap(pipeline.Config)
	return pipeline
}

func cloneEmbeddingJobs(jobs []ProjectEmbeddingJob) []ProjectEmbeddingJob {
	return append([]ProjectEmbeddingJob(nil), jobs...)
}

func cloneDatabaseExtensions(extensions []ProjectDatabaseExtension) []ProjectDatabaseExtension {
	return append([]ProjectDatabaseExtension(nil), extensions...)
}

func cloneDatabaseExtension(extension ProjectDatabaseExtension) ProjectDatabaseExtension {
	return extension
}

func cloneDatabaseCronJobs(jobs []ProjectDatabaseCronJob) []ProjectDatabaseCronJob {
	out := append([]ProjectDatabaseCronJob(nil), jobs...)
	for index := range out {
		out[index] = cloneDatabaseCronJob(out[index])
	}
	return out
}

func cloneDatabaseCronJob(job ProjectDatabaseCronJob) ProjectDatabaseCronJob {
	job.Metadata = cloneStringMap(job.Metadata)
	return job
}

func cloneDatabaseQueues(queues []ProjectDatabaseQueue) []ProjectDatabaseQueue {
	out := append([]ProjectDatabaseQueue(nil), queues...)
	for index := range out {
		out[index] = cloneDatabaseQueue(out[index])
	}
	return out
}

func cloneDatabaseQueue(queue ProjectDatabaseQueue) ProjectDatabaseQueue {
	queue.Metadata = cloneStringMap(queue.Metadata)
	return queue
}

func cloneDatabaseWebhooks(webhooks []ProjectDatabaseWebhook) []ProjectDatabaseWebhook {
	out := append([]ProjectDatabaseWebhook(nil), webhooks...)
	for index := range out {
		out[index] = cloneDatabaseWebhook(out[index])
	}
	return out
}

func cloneDatabaseWebhook(webhook ProjectDatabaseWebhook) ProjectDatabaseWebhook {
	webhook.Events = append([]string(nil), webhook.Events...)
	webhook.Headers = cloneStringMap(webhook.Headers)
	webhook.Metadata = cloneStringMap(webhook.Metadata)
	return webhook
}

func cloneDatabaseSchemas(schemas []ProjectDatabaseSchema) []ProjectDatabaseSchema {
	out := append([]ProjectDatabaseSchema(nil), schemas...)
	for index := range out {
		out[index] = cloneDatabaseSchema(out[index])
	}
	return out
}

func cloneDatabaseSchema(schema ProjectDatabaseSchema) ProjectDatabaseSchema {
	schema.Metadata = cloneStringMap(schema.Metadata)
	return schema
}

func cloneDatabaseRoles(roles []ProjectDatabaseRole) []ProjectDatabaseRole {
	out := append([]ProjectDatabaseRole(nil), roles...)
	for index := range out {
		out[index] = cloneDatabaseRole(out[index])
	}
	return out
}

func cloneDatabaseRole(role ProjectDatabaseRole) ProjectDatabaseRole {
	role.MemberOf = append([]string(nil), role.MemberOf...)
	role.SchemaGrants = cloneStringMap(role.SchemaGrants)
	role.Metadata = cloneStringMap(role.Metadata)
	return role
}

func cloneStorageBuckets(buckets []ProjectStorageBucket) []ProjectStorageBucket {
	out := append([]ProjectStorageBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneStorageBucket(out[index])
	}
	return out
}

func cloneStorageBucket(bucket ProjectStorageBucket) ProjectStorageBucket {
	bucket.AllowedMimeTypes = append([]string(nil), bucket.AllowedMimeTypes...)
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneVectorBuckets(buckets []ProjectVectorBucket) []ProjectVectorBucket {
	out := append([]ProjectVectorBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneVectorBucket(out[index])
	}
	return out
}

func cloneVectorBucket(bucket ProjectVectorBucket) ProjectVectorBucket {
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneAnalyticsBuckets(buckets []ProjectAnalyticsBucket) []ProjectAnalyticsBucket {
	out := append([]ProjectAnalyticsBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneAnalyticsBucket(out[index])
	}
	return out
}

func cloneAnalyticsBucket(bucket ProjectAnalyticsBucket) ProjectAnalyticsBucket {
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneLogDrains(drains []LogDrain) []LogDrain {
	out := append([]LogDrain(nil), drains...)
	for index := range out {
		out[index] = cloneLogDrain(out[index])
	}
	return out
}

func cloneAndSortProjectChildList[T any](input []T, clone func([]T) []T, less func(T, T) bool) []T {
	out := clone(input)
	sort.Slice(out, func(i, j int) bool {
		return less(out[i], out[j])
	})
	if out == nil {
		return []T{}
	}
	return out
}

func maskFunctionSecrets(secrets map[string]string) map[string]string {
	masked := map[string]string{}
	for key, value := range secrets {
		if strings.TrimSpace(value) == "" {
			continue
		}
		masked[key] = maskSecret(value)
	}
	return masked
}

func cloneStringMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func serviceEnabledMap(input map[string]ServiceSpec) map[string]bool {
	return ProjectServiceStates(input)
}

func cloneLogDrain(drain LogDrain) LogDrain {
	drain.Config = cloneStringMap(drain.Config)
	return drain
}

func normalizeDomain(input string) (string, error) {
	fqdn := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
	if fqdn == "" {
		return "", fmt.Errorf("fqdn is required")
	}
	if strings.Contains(fqdn, "://") || strings.ContainsAny(fqdn, "/ \\") {
		return "", fmt.Errorf("fqdn must be a hostname")
	}
	if len(fqdn) > 253 || !domainPattern.MatchString(fqdn) {
		return "", fmt.Errorf("fqdn must be a valid hostname")
	}
	return fqdn, nil
}

func (req CreateProjectRequest) toSpec() ProjectSpec {
	services, err := normalizeProjectServices(req.Services)
	if err != nil {
		services, _ = normalizeProjectServices(nil)
	}

	return ProjectSpec{
		Ref:           req.Ref,
		OrgID:         req.OrgID,
		Name:          req.Name,
		HostID:        req.HostID,
		Domain:        strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Domain)), "."),
		StackVersion:  strings.TrimSpace(req.StackVersion),
		Profile:       req.Profile,
		ResourceTier:  req.ResourceTier,
		CPU:           req.CPU,
		RAMMB:         req.RAMMB,
		DiskGB:        req.DiskGB,
		EnforceLimits: req.EnforceLimits,
		Services:      services,
		Environment:   req.Environment,
	}
}

func defaultPlatformDefaults() PlatformDefaults {
	now := time.Now().UTC()
	return PlatformDefaults{
		Domain:                      "supadupa.test",
		StackVersion:                "latest",
		Profile:                     StackProfileFull,
		ResourceTier:                ResourceTierCustom,
		BackupSchedule:              "daily",
		FeatureFlags:                cloneBoolMap(defaultPlatformFeatureFlags),
		DatabaseIngressAllowedCIDRs: []string{},
		SMTP:                        defaultPlatformSMTP(),
		UpdatedAt:                   now,
	}
}

func normalizedPlatformDefaults(defaults PlatformDefaults) PlatformDefaults {
	input := PlatformDefaultsInput{
		Domain:                      defaults.Domain,
		StackVersion:                defaults.StackVersion,
		Profile:                     defaults.Profile,
		ResourceTier:                defaults.ResourceTier,
		BackupSchedule:              defaults.BackupSchedule,
		FeatureFlags:                defaults.FeatureFlags,
		DatabaseIngressAllowedCIDRs: defaults.DatabaseIngressAllowedCIDRs,
		SMTP:                        defaults.SMTP,
	}
	normalized, err := normalizePlatformDefaults(input)
	if err != nil {
		return defaultPlatformDefaults()
	}
	if !defaults.UpdatedAt.IsZero() {
		normalized.UpdatedAt = defaults.UpdatedAt
	}
	return normalized
}

func normalizePlatformDefaults(input PlatformDefaultsInput) (PlatformDefaults, error) {
	domain := strings.TrimSpace(input.Domain)
	if domain == "" {
		domain = "supadupa.test"
	}
	normalizedDomain, err := normalizeDomain(domain)
	if err != nil {
		return PlatformDefaults{}, fmt.Errorf("domain %w", err)
	}
	if err := validateGeneratedProjectFQDNs(strings.Repeat("a", 55), normalizedDomain); err != nil {
		return PlatformDefaults{}, fmt.Errorf("domain %w", err)
	}
	stackVersion := strings.TrimSpace(input.StackVersion)
	if stackVersion == "" {
		stackVersion = "latest"
	}
	if err := validateSupportedStackVersion(stackVersion); err != nil {
		return PlatformDefaults{}, err
	}
	profile := input.Profile
	if profile == "" {
		profile = StackProfileFull
	}
	if err := validateStackProfile(profile); err != nil {
		return PlatformDefaults{}, err
	}
	tier := input.ResourceTier
	if tier == "" {
		tier = ResourceTierCustom
	}
	if err := validateResourceTier(tier); err != nil {
		return PlatformDefaults{}, err
	}
	if tier != ResourceTierCustom {
		return PlatformDefaults{}, fmt.Errorf("platform project resource defaults use exact sizing; resource_tier must be %q", ResourceTierCustom)
	}
	schedule := strings.TrimSpace(input.BackupSchedule)
	if schedule == "" {
		schedule = "daily"
	}
	if err := validateBackupSchedule(schedule); err != nil {
		return PlatformDefaults{}, err
	}
	smtp, err := normalizePlatformSMTP(input.SMTP)
	if err != nil {
		return PlatformDefaults{}, err
	}
	featureFlags, err := normalizePlatformFeatureFlags(input.FeatureFlags)
	if err != nil {
		return PlatformDefaults{}, err
	}
	databaseIngressAllowedCIDRs, err := normalizeOptionalNetworkCIDRs(input.DatabaseIngressAllowedCIDRs)
	if err != nil {
		return PlatformDefaults{}, fmt.Errorf("database ingress allowlist: %w", err)
	}
	return PlatformDefaults{
		Domain:                      normalizedDomain,
		StackVersion:                stackVersion,
		Profile:                     profile,
		ResourceTier:                tier,
		BackupSchedule:              schedule,
		FeatureFlags:                featureFlags,
		DatabaseIngressAllowedCIDRs: databaseIngressAllowedCIDRs,
		SMTP:                        smtp,
		UpdatedAt:                   time.Now().UTC(),
	}, nil
}

func validateSupportedStackVersion(version string) error {
	normalized := NormalizeStackReleaseVersion(version)
	if _, ok := ResolveStackReleaseManifestFromEnv(nil, normalized); ok {
		return nil
	}
	return fmt.Errorf("unsupported stack version %q; supported stable versions: %s", version, strings.Join(SupportedStackReleaseVersionsFromEnv(nil), ", "))
}

func normalizePlatformFeatureFlags(input map[string]bool) (map[string]bool, error) {
	out := cloneBoolMap(defaultPlatformFeatureFlags)
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return nil, fmt.Errorf("feature flag key is required")
		}
		if _, ok := defaultPlatformFeatureFlags[normalized]; !ok {
			return nil, fmt.Errorf("unsupported feature flag %q", normalized)
		}
		out[normalized] = value
	}
	return out, nil
}

func normalizeOrgFeatureOverrides(input map[string]bool) (map[string]bool, error) {
	out := map[string]bool{}
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return nil, fmt.Errorf("feature flag key is required")
		}
		if _, ok := defaultPlatformFeatureFlags[normalized]; !ok {
			return nil, fmt.Errorf("unsupported feature flag %q", normalized)
		}
		out[normalized] = value
	}
	return out, nil
}

func orgFeatureFlags(orgID string, overrides map[string]bool, defaults PlatformDefaults) OrgFeatureFlags {
	defaultFlags := cloneBoolMap(defaults.FeatureFlags)
	effective := cloneBoolMap(defaultFlags)
	normalizedOverrides, err := normalizeOrgFeatureOverrides(overrides)
	if err != nil {
		normalizedOverrides = map[string]bool{}
	}
	for key, value := range normalizedOverrides {
		effective[key] = value
	}
	return OrgFeatureFlags{
		OrgID:     orgID,
		Defaults:  defaultFlags,
		Overrides: normalizedOverrides,
		Effective: effective,
	}
}

func orgWithFeatureFlags(org Org, defaults PlatformDefaults) Org {
	flags := orgFeatureFlags(org.ID, org.FeatureFlagOverrides, defaults)
	org.FeatureFlagOverrides = flags.Overrides
	org.FeatureFlags = flags.Effective
	return org
}

func defaultPlatformSMTP() PlatformSMTP {
	return PlatformSMTP{
		Port:    587,
		TLSMode: "starttls",
	}
}

func normalizePlatformSMTP(input PlatformSMTP) (PlatformSMTP, error) {
	smtp := PlatformSMTP{
		Enabled:        input.Enabled,
		Host:           strings.TrimSpace(input.Host),
		Port:           input.Port,
		SenderName:     strings.TrimSpace(input.SenderName),
		SenderEmail:    strings.TrimSpace(input.SenderEmail),
		Username:       strings.TrimSpace(input.Username),
		PasswordHandle: strings.TrimSpace(input.PasswordHandle),
		TLSMode:        strings.ToLower(strings.TrimSpace(input.TLSMode)),
	}
	if smtp.Port == 0 {
		smtp.Port = 587
	}
	if smtp.TLSMode == "" {
		smtp.TLSMode = "starttls"
	}
	config := map[string]string{
		"port":            strconv.Itoa(smtp.Port),
		"password_handle": smtp.PasswordHandle,
		"tls_mode":        smtp.TLSMode,
	}
	if err := validateSMTPConfig(config); err != nil {
		return PlatformSMTP{}, err
	}
	if smtp.Enabled && smtp.Host == "" {
		return PlatformSMTP{}, fmt.Errorf("smtp host is required when platform smtp is enabled")
	}
	return smtp, nil
}

func validateStackProfile(profile StackProfile) error {
	switch profile {
	case StackProfileFull, StackProfileEssential, StackProfileOrioleDB:
		return nil
	default:
		return fmt.Errorf("unsupported stack profile %q", profile)
	}
}

func validateResourceTier(tier ResourceTier) error {
	switch tier {
	case ResourceTierSmall, ResourceTierMedium, ResourceTierLarge, ResourceTierCustom:
		return nil
	default:
		return fmt.Errorf("unsupported resource tier %q", tier)
	}
}

func validateBackupSchedule(schedule string) error {
	if schedule != "daily" && schedule != "hourly" {
		return fmt.Errorf("unsupported backup schedule %q", schedule)
	}
	return nil
}

func newID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

func generateProjectSecrets(ref string) map[string]ProjectSecret {
	now := time.Now().UTC()
	secrets := map[string]ProjectSecret{}
	for kind := range secretPrefixes {
		if kind == "anon_key" || kind == "service_role" {
			continue
		}
		secrets[kind] = newProjectSecret(ref, kind, now)
	}
	ensureSupabaseAPIKeys(ref, secrets, now)
	return secrets
}

func ensureProjectSigningKeys(ref string, secrets map[string]ProjectSecret) {
	now := time.Now().UTC()
	for _, kind := range []string{"jwt_signing_key_current", "jwt_signing_key_next"} {
		if _, ok := secrets[kind]; !ok {
			secrets[kind] = newProjectSecret(ref, kind, now)
		}
	}
}

func ensureSupabaseAPIKeys(ref string, secrets map[string]ProjectSecret, now time.Time) {
	jwtSecret := strings.TrimSpace(secrets["jwt_secret"].Value)
	if jwtSecret == "" {
		secrets["jwt_secret"] = newProjectSecret(ref, "jwt_secret", now)
		jwtSecret = secrets["jwt_secret"].Value
	}
	for _, role := range []string{"anon", "service_role"} {
		kind := "anon_key"
		if role == "service_role" {
			kind = "service_role"
		}
		token := supabaseRoleJWT(ref, role, jwtSecret)
		secret, ok := secrets[kind]
		if !ok {
			secret = ProjectSecret{
				ID:         newID(),
				ProjectRef: ref,
				Kind:       kind,
				CreatedAt:  now,
			}
		}
		if !looksLikeJWT(secret.Value) || !verifySupabaseRoleJWT(secret.Value, role, jwtSecret) {
			secret.Value = token
			secret.Masked = maskSecret(token)
			secrets[kind] = secret
		}
	}
}

func newProjectSecret(ref string, kind string, now time.Time) ProjectSecret {
	value := randomSecretValue(ref, kind)
	return ProjectSecret{
		ID:         newID(),
		ProjectRef: ref,
		Kind:       kind,
		Value:      value,
		Masked:     maskSecret(value),
		CreatedAt:  now,
	}
}

func randomSecretValue(ref string, kind string) string {
	if strings.HasPrefix(kind, "jwt_signing_key_") {
		status := "previous"
		switch kind {
		case "jwt_signing_key_current":
			status = "current"
		case "jwt_signing_key_next":
			status = "next"
		}
		return randomJWTSigningKeyValue(ref, status)
	}
	if kind == "db_password" {
		return mustRandomHex(secretByteLengths[kind])
	}
	return randomToken(secretPrefixes[kind], secretByteLengths[kind])
}

func supabaseRoleJWT(ref string, role string, jwtSecret string) string {
	now := time.Now().UTC().Unix()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":  "supabase",
		"ref":  ref,
		"role": role,
		"aud":  "authenticated",
		"iat":  now,
		"jti":  newID(),
		"exp":  int64(4102444800), // 2100-01-01T00:00:00Z, matching long-lived self-hosted API keys.
	}
	headerPayload, _ := json.Marshal(header)
	claimsPayload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerPayload) + "." + base64.RawURLEncoding.EncodeToString(claimsPayload)
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func looksLikeJWT(value string) bool {
	return len(strings.Split(strings.TrimSpace(value), ".")) == 3
}

func verifySupabaseRoleJWT(token string, role string, jwtSecret string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(actual, expected) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	return claims["role"] == role && claims["aud"] == "authenticated"
}

func randomJWTSigningKeyValue(ref string, status string) string {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return randomToken("jwk", 32)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return randomToken("jwk", 32)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return randomToken("jwk", 32)
	}
	material := JWTSigningKeyMaterial{
		KID:        ref + "-" + status + "-" + mustRandomHex(4),
		Alg:        "EdDSA",
		PublicKey:  strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))),
		PrivateKey: strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))),
		Status:     status,
	}
	payload, err := json.Marshal(material)
	if err != nil {
		return randomToken("jwk", 32)
	}
	return string(payload)
}

func updateJWTSigningKeyMaterialStatus(value string, status string) string {
	material := JWTSigningKeyMaterial{}
	if err := json.Unmarshal([]byte(value), &material); err != nil {
		return value
	}
	material.Status = status
	payload, err := json.Marshal(material)
	if err != nil {
		return value
	}
	return string(payload)
}

func defaultBackupPolicy(ref string) BackupPolicy {
	return defaultBackupPolicyForSchedule(ref, "daily")
}

func defaultBackupPolicyForSchedule(ref string, schedule string) BackupPolicy {
	if err := validateBackupSchedule(schedule); err != nil {
		schedule = "daily"
	}
	now := time.Now().UTC()
	next := nextBackupRun(now, schedule)
	return BackupPolicy{
		ProjectRef: ref,
		Enabled:    true,
		Schedule:   schedule,
		Kind:       "logical",
		NextRunAt:  &next,
		UpdatedAt:  now,
	}
}

func normalizeBackupStorageTargetInput(id string, existing BackupStorageTarget, input BackupStorageTargetInput, creating bool) (BackupStorageTarget, error) {
	targetType := strings.ToLower(strings.TrimSpace(input.Type))
	if targetType == "" {
		targetType = "s3"
	}
	if targetType != "s3" {
		return BackupStorageTarget{}, fmt.Errorf("unsupported backup storage target type %q", targetType)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target name is required")
	}
	if len(name) > 120 || strings.ContainsAny(name, "\r\n\t") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target name is invalid")
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint must be an absolute URL")
		}
		if parsed.User != nil {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint must not include credentials")
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint scheme must be http or https")
		}
		endpoint = strings.TrimRight(endpoint, "/")
		if err := validateBackupStorageTargetEndpointForSignedEgress(endpoint); err != nil {
			return BackupStorageTarget{}, err
		}
	}
	region := strings.TrimSpace(input.Region)
	if region == "" {
		region = "auto"
	}
	if len(region) > 80 || strings.ContainsAny(region, "\r\n\t /") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target region is invalid")
	}
	bucket := strings.TrimSpace(input.Bucket)
	if bucket == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target bucket is required")
	}
	if len(bucket) > 255 || strings.ContainsAny(bucket, "\r\n\t/\\") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target bucket is invalid")
	}
	prefix := strings.Trim(strings.TrimSpace(input.Prefix), "/")
	if strings.ContainsAny(prefix, "\r\n\t\\") || strings.Contains(prefix, "..") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target prefix is invalid")
	}
	accessKeyID := strings.TrimSpace(input.AccessKeyID)
	secretAccessKey := strings.TrimSpace(input.SecretAccessKey)
	if accessKeyID == "" && !creating {
		accessKeyID = existing.AccessKeyID
	}
	if secretAccessKey == "" && !creating {
		secretAccessKey = existing.SecretAccessKey
	}
	if accessKeyID == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target access key id is required")
	}
	if secretAccessKey == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target secret access key is required")
	}
	if strings.ContainsAny(accessKeyID, "\r\n\t") || strings.ContainsAny(secretAccessKey, "\r\n") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target credentials are invalid")
	}
	now := time.Now().UTC()
	createdAt := existing.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	target := BackupStorageTarget{
		ID:               id,
		Name:             name,
		Type:             targetType,
		Endpoint:         endpoint,
		Region:           region,
		Bucket:           bucket,
		Prefix:           prefix,
		AccessKeyID:      accessKeyID,
		SecretAccessKey:  secretAccessKey,
		SecretConfigured: secretAccessKey != "",
		ForcePathStyle:   input.ForcePathStyle,
		Default:          input.Default,
		LastTestedAt:     existing.LastTestedAt,
		LastTestStatus:   existing.LastTestStatus,
		LastTestError:    existing.LastTestError,
		CreatedAt:        createdAt,
		UpdatedAt:        now,
	}
	if !creating && backupStorageTargetConnectionChanged(existing, target) {
		target.LastTestedAt = nil
		target.LastTestStatus = ""
		target.LastTestError = ""
	}
	return target, nil
}

func redactBackupStorageTarget(target BackupStorageTarget) BackupStorageTarget {
	target.SecretConfigured = strings.TrimSpace(target.SecretAccessKey) != ""
	target.SecretAccessKey = ""
	target.DurableOffHost, target.RecoveryReady, target.ReadinessStatus, target.ReadinessMessage = backupStorageTargetReadiness(target)
	target.Warnings = backupStorageTargetNetworkWarnings(target)
	return target
}

func backupStorageTargetConnectionChanged(a BackupStorageTarget, b BackupStorageTarget) bool {
	return a.Endpoint != b.Endpoint ||
		a.Region != b.Region ||
		a.Bucket != b.Bucket ||
		a.Prefix != b.Prefix ||
		a.AccessKeyID != b.AccessKeyID ||
		a.SecretAccessKey != b.SecretAccessKey ||
		a.ForcePathStyle != b.ForcePathStyle
}

func defaultPITRPolicy(ref string) PITRPolicy {
	return PITRPolicy{
		ProjectRef:    ref,
		Enabled:       false,
		ArchiveBucket: "",
		RetentionDays: 7,
		UpdatedAt:     time.Now().UTC(),
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneProjectDomains(domains []ProjectDomain) []ProjectDomain {
	out := append([]ProjectDomain(nil), domains...)
	for index := range out {
		out[index].CertNotAfter = cloneTimePtr(out[index].CertNotAfter)
	}
	return out
}

func nextBackupRun(from time.Time, schedule string) time.Time {
	from = from.UTC()
	if schedule == "hourly" {
		return from.Add(time.Hour).Truncate(time.Hour)
	}
	next := time.Date(from.Year(), from.Month(), from.Day(), 2, 0, 0, 0, time.UTC)
	if !next.After(from) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func secretsToSlice(secrets map[string]ProjectSecret) []ProjectSecret {
	out := make([]ProjectSecret, 0, len(secrets))
	for _, secret := range secrets {
		out = append(out, secret)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Kind < out[j].Kind
	})
	return out
}

func randomToken(prefix string, bytes int) string {
	return prefix + "_" + mustRandomHex(bytes)
}

// cryptoRead is crypto/rand.Read, overridable in tests to force failure paths.
var cryptoRead = rand.Read

// randomHex returns cryptographically random hex or an error. It never falls back
// to weak time-based placeholders on CSPRNG failure (fail-closed).
func randomHex(bytes int) (string, error) {
	if bytes <= 0 {
		return "", fmt.Errorf("randomHex: bytes must be positive")
	}
	data := make([]byte, bytes)
	if _, err := cryptoRead(data); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func mustRandomHex(bytes int) string {
	value, err := randomHex(bytes)
	if err != nil {
		panic(err)
	}
	return value
}

func maskSecret(value string) string {
	if len(value) <= 10 {
		return "********"
	}
	return value[:6] + strings.Repeat("*", 12) + value[len(value)-4:]
}
