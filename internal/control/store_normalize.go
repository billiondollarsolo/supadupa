package control

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

func normalizeProjectSecretKind(kind string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		return "", fmt.Errorf("secret kind is required")
	}
	if strings.Contains(normalized, "/") || !secretKindPattern.MatchString(normalized) {
		return "", fmt.Errorf("secret kind %q is invalid", normalized)
	}
	return normalized, nil
}

func normalizeManagedProjectSecretKind(kind string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		return "", fmt.Errorf("secret kind is required")
	}
	if _, ok := secretPrefixes[normalized]; !ok {
		if !strings.HasPrefix(normalized, "jwt_signing_key_previous_") {
			return "", fmt.Errorf("unsupported secret kind %q", normalized)
		}
	}
	return normalized, nil
}

func normalizeCustomProjectSecretKind(kind string) (string, error) {
	normalized, err := normalizeProjectSecretKind(kind)
	if err != nil {
		return "", err
	}
	if _, managed := secretPrefixes[normalized]; managed || strings.HasPrefix(normalized, "jwt_signing_key_") {
		return "", fmt.Errorf("secret kind %q is managed by the control plane", normalized)
	}
	return normalized, nil
}

func normalizeFunctionName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("function name is required")
	}
	if !refPattern.MatchString(normalized) {
		return "", fmt.Errorf("function name must be 3-64 lowercase letters, numbers, or dashes")
	}
	return normalized, nil
}

func normalizeProjectFunctionRegion(ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	functionName, err := normalizeFunctionName(input.FunctionName)
	if err != nil {
		return ProjectFunctionRegion{}, err
	}
	region := strings.ToLower(strings.TrimSpace(input.Region))
	if region == "" {
		region = "local"
	}
	if len(region) > 64 || strings.ContainsAny(region, " \r\n\t/\\") {
		return ProjectFunctionRegion{}, fmt.Errorf("region is invalid")
	}
	hostID := strings.TrimSpace(input.HostID)
	routingPolicy := strings.ToLower(strings.TrimSpace(input.RoutingPolicy))
	if routingPolicy == "" {
		routingPolicy = "nearest"
	}
	switch routingPolicy {
	case "nearest", "primary", "weighted":
	default:
		return ProjectFunctionRegion{}, fmt.Errorf("routing_policy must be nearest, primary, or weighted")
	}
	now := time.Now().UTC()
	projectRef := strings.ToLower(strings.TrimSpace(ref))
	return ProjectFunctionRegion{
		ID:            newID(),
		ProjectRef:    projectRef,
		FunctionName:  functionName,
		HostID:        hostID,
		Region:        region,
		RoutingPolicy: routingPolicy,
		InvocationURL: fmt.Sprintf("https://%s.%s.%s.functions.internal", functionName, region, projectRef),
		Status:        "configured",
		Message:       "regional invocation declaration recorded",
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func normalizeProjectFunctionStorageMount(ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	functionName, err := normalizeFunctionName(input.FunctionName)
	if err != nil {
		return ProjectFunctionStorageMount{}, err
	}
	bucketName := strings.ToLower(strings.TrimSpace(input.BucketName))
	if !refPattern.MatchString(bucketName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	mountPath := strings.TrimSpace(strings.ReplaceAll(input.MountPath, "\\", "/"))
	if mountPath == "" {
		mountPath = "/mnt/" + bucketName
	}
	cleaned := path.Clean(mountPath)
	if !strings.HasPrefix(cleaned, "/mnt/") || cleaned == "/mnt" || strings.Contains(cleaned, "/../") {
		return ProjectFunctionStorageMount{}, fmt.Errorf("mount_path must be an absolute path under /mnt")
	}
	prefix := strings.TrimSpace(strings.ReplaceAll(input.Prefix, "\\", "/"))
	if prefix != "" {
		prefix = path.Clean(prefix)
		if prefix == "." {
			prefix = ""
		}
		if strings.HasPrefix(prefix, "../") || strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "/../") {
			return ProjectFunctionStorageMount{}, fmt.Errorf("prefix must be relative to the bucket")
		}
	}
	envAlias := strings.ToUpper(strings.TrimSpace(input.EnvAlias))
	if envAlias == "" {
		envAlias = strings.ToUpper(strings.ReplaceAll(functionName+"_"+bucketName+"_MOUNT", "-", "_"))
	}
	if !envAliasPattern.MatchString(envAlias) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("env_alias must be an uppercase environment variable name")
	}
	now := time.Now().UTC()
	return ProjectFunctionStorageMount{
		ID:           newID(),
		ProjectRef:   strings.ToLower(strings.TrimSpace(ref)),
		FunctionName: functionName,
		BucketName:   bucketName,
		MountPath:    cleaned,
		ReadOnly:     input.ReadOnly,
		Prefix:       prefix,
		EnvAlias:     envAlias,
		Status:       "configured",
		Message:      "function storage mount declaration recorded",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func functionExists(functions []ProjectFunction, name string) bool {
	for _, function := range functions {
		if function.Name == name {
			return true
		}
	}
	return false
}

func storageBucketExists(buckets []ProjectStorageBucket, name string) bool {
	for _, bucket := range buckets {
		if bucket.Name == name {
			return true
		}
	}
	return false
}

func removeFunctionRegions(regions []ProjectFunctionRegion, functionName string) []ProjectFunctionRegion {
	out := regions[:0]
	for _, region := range regions {
		if functionName != "" && region.FunctionName == functionName {
			continue
		}
		out = append(out, region)
	}
	return out
}

func removeFunctionStorageMounts(mounts []ProjectFunctionStorageMount, functionName string, bucketName string) []ProjectFunctionStorageMount {
	out := mounts[:0]
	for _, mount := range mounts {
		if functionName != "" && mount.FunctionName == functionName {
			continue
		}
		if bucketName != "" && mount.BucketName == bucketName {
			continue
		}
		out = append(out, mount)
	}
	return out
}

func normalizeReplicaName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("replica name is required")
	}
	if !replicaNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("replica name must be 3-64 lowercase letters, numbers, or dashes, and cannot start or end with a dash")
	}
	return normalized, nil
}

func validateReplicaPublicDNSHost(ref string, replicaName string, domain string) error {
	label := fmt.Sprintf("db-replica-%s-%s", routeName(replicaName), strings.ToLower(strings.TrimSpace(ref)))
	if len(label) <= 63 {
		host := replicaDatabaseHost(ref, replicaName, strings.Trim(strings.ToLower(strings.TrimSpace(domain)), "."))
		if len(host) > 253 {
			return fmt.Errorf("project replica host %s exceeds the 253-character DNS name limit; shorten the replica name, project ref, or apps domain", host)
		}
		return nil
	}
	maxReplicaNameLength := 63 - len("db-replica-") - 1 - len(strings.TrimSpace(ref))
	if maxReplicaNameLength < 3 {
		return fmt.Errorf("project ref %q is too long for public read-replica DNS labels; maximum ref length for replicas is 48 characters", ref)
	}
	return fmt.Errorf("replica name must be at most %d characters for project ref %q so public host %s stays within the 63-character DNS label limit", maxReplicaNameLength, ref, label)
}

func normalizeLogDrainTarget(target string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if normalized == "" {
		return "", fmt.Errorf("log drain target is required")
	}
	if _, ok := allowedLogDrainTargets[normalized]; !ok {
		return "", fmt.Errorf("unsupported log drain target %q", normalized)
	}
	return normalized, nil
}

func normalizeReplicationPipelineType(pipelineType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(pipelineType))
	if normalized == "" {
		normalized = "logical"
	}
	if _, ok := allowedReplicationPipelineTypes[normalized]; !ok {
		return "", fmt.Errorf("unsupported replication pipeline type %q", normalized)
	}
	return normalized, nil
}

func normalizeReplicationDestination(destination string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(destination))
	if normalized == "" {
		return "", fmt.Errorf("replication destination is required")
	}
	if _, ok := allowedReplicationDestinations[normalized]; !ok {
		return "", fmt.Errorf("unsupported replication destination %q", normalized)
	}
	return normalized, nil
}

func validateReplicationDestinationConfig(destination string, destinationURI string, config map[string]string) error {
	switch destination {
	case "postgres", "webhook":
		if strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination %s requires destination_uri", destination)
		}
	case "s3", "iceberg":
		if strings.TrimSpace(config["bucket"]) == "" && strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination %s requires bucket or destination_uri", destination)
		}
	case "bigquery":
		if strings.TrimSpace(config["dataset"]) == "" {
			return fmt.Errorf("replication destination bigquery requires dataset")
		}
	case "snowflake":
		if strings.TrimSpace(config["warehouse"]) == "" || strings.TrimSpace(config["database"]) == "" {
			return fmt.Errorf("replication destination snowflake requires warehouse and database")
		}
	case "redshift":
		if strings.TrimSpace(config["cluster"]) == "" && strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination redshift requires cluster or destination_uri")
		}
	}
	return nil
}

func validateReplicationSecretHandles(config map[string]string) error {
	for key, value := range config {
		value = strings.TrimSpace(value)
		if value == "" || !isSensitiveProjectConfigKey(key) {
			continue
		}
		if !strings.HasPrefix(value, "secret://") {
			return fmt.Errorf("replication config %s must use a secret:// handle", key)
		}
	}
	return nil
}

func normalizeProjectEmbeddingJob(ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[provider]; !ok {
		return ProjectEmbeddingJob{}, fmt.Errorf("unsupported embedding provider %q", provider)
	}
	sourceSchema := strings.ToLower(strings.TrimSpace(input.SourceSchema))
	if sourceSchema == "" {
		sourceSchema = "public"
	}
	sourceTable := strings.ToLower(strings.TrimSpace(input.SourceTable))
	sourceColumn := strings.ToLower(strings.TrimSpace(input.SourceColumn))
	primaryKeyColumn := strings.ToLower(strings.TrimSpace(input.PrimaryKeyColumn))
	if primaryKeyColumn == "" {
		primaryKeyColumn = "id"
	}
	destinationTable := strings.ToLower(strings.TrimSpace(input.DestinationTable))
	if destinationTable == "" {
		destinationTable = sourceTable + "_embeddings"
	}
	destinationColumn := strings.ToLower(strings.TrimSpace(input.DestinationColumn))
	if destinationColumn == "" {
		destinationColumn = "embedding"
	}
	for label, identifier := range map[string]string{
		"source_schema":      sourceSchema,
		"source_table":       sourceTable,
		"source_column":      sourceColumn,
		"primary_key_column": primaryKeyColumn,
		"destination_table":  destinationTable,
		"destination_column": destinationColumn,
	} {
		if !identifierPattern.MatchString(identifier) {
			return ProjectEmbeddingJob{}, fmt.Errorf("%s must be a simple Postgres identifier", label)
		}
	}
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = sourceTable + "-" + sourceColumn + "-embeddings"
	}
	if !refPattern.MatchString(name) {
		return ProjectEmbeddingJob{}, fmt.Errorf("embedding job name must be 3-64 lowercase letters, numbers, or dashes")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		if provider == "openai" {
			model = "text-embedding-3-small"
		} else {
			model = "default"
		}
	}
	dimension := input.Dimension
	if dimension == 0 {
		dimension = 1536
	}
	if dimension < 1 || dimension > 65535 {
		return ProjectEmbeddingJob{}, fmt.Errorf("dimension must be between 1 and 65535")
	}
	batchSize := input.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}
	if batchSize < 1 || batchSize > 10000 {
		return ProjectEmbeddingJob{}, fmt.Errorf("batch_size must be between 1 and 10000")
	}
	schedule := strings.TrimSpace(input.Schedule)
	if schedule == "" {
		schedule = "manual"
	}
	if len(schedule) > 120 || strings.ContainsAny(schedule, "\r\n") {
		return ProjectEmbeddingJob{}, fmt.Errorf("schedule is invalid")
	}
	now := time.Now().UTC()
	return ProjectEmbeddingJob{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		SourceSchema:      sourceSchema,
		SourceTable:       sourceTable,
		SourceColumn:      sourceColumn,
		PrimaryKeyColumn:  primaryKeyColumn,
		DestinationTable:  destinationTable,
		DestinationColumn: destinationColumn,
		Provider:          provider,
		Model:             model,
		Dimension:         dimension,
		Schedule:          schedule,
		BatchSize:         batchSize,
		Status:            "configured",
		Message:           "automatic embedding pipeline recorded",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func normalizeProjectVectorBucket(ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectVectorBucket{}, fmt.Errorf("vector bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	dimension := input.Dimension
	if dimension == 0 {
		dimension = 1536
	}
	if dimension < 1 || dimension > 65535 {
		return ProjectVectorBucket{}, fmt.Errorf("dimension must be between 1 and 65535")
	}
	distance := strings.ToLower(strings.TrimSpace(input.Distance))
	if distance == "" {
		distance = "cosine"
	}
	if _, ok := allowedVectorBucketDistances[distance]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector distance %q", distance)
	}
	indexMethod := strings.ToLower(strings.TrimSpace(input.IndexMethod))
	if indexMethod == "" {
		indexMethod = "hnsw"
	}
	if _, ok := allowedVectorBucketIndexes[indexMethod]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector index method %q", indexMethod)
	}
	backend := strings.ToLower(strings.TrimSpace(input.StorageBackend))
	if backend == "" {
		backend = "postgres"
	}
	if _, ok := allowedVectorBucketBackends[backend]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector bucket backend %q", backend)
	}
	storageURI := strings.TrimSpace(input.StorageURI)
	if backend == "s3" && storageURI == "" {
		return ProjectVectorBucket{}, fmt.Errorf("s3 vector bucket requires storage_uri")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectVectorBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectVectorBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectVectorBucket{
		ID:             newID(),
		ProjectRef:     ref,
		Name:           name,
		Dimension:      dimension,
		Distance:       distance,
		IndexMethod:    indexMethod,
		StorageBackend: backend,
		StorageURI:     storageURI,
		Metadata:       metadata,
		Status:         "configured",
		Message:        "vector bucket declaration recorded",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeProjectAnalyticsBucket(ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectAnalyticsBucket{}, fmt.Errorf("analytics bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	storageURI := strings.TrimSpace(input.StorageURI)
	if storageURI == "" {
		return ProjectAnalyticsBucket{}, fmt.Errorf("storage_uri is required")
	}
	if err := validateAnalyticsStorageURI(storageURI); err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	catalogURI := strings.TrimSpace(input.CatalogURI)
	if catalogURI != "" {
		parsed, err := url.Parse(catalogURI)
		if err != nil || parsed.Scheme == "" {
			return ProjectAnalyticsBucket{}, fmt.Errorf("catalog_uri is invalid")
		}
	}
	warehouse := strings.TrimSpace(input.Warehouse)
	if warehouse == "" {
		warehouse = name
	}
	if strings.ContainsAny(warehouse, "\r\n\t") || len(warehouse) > 128 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("warehouse is invalid")
	}
	credentialHandle := strings.TrimSpace(input.CredentialHandle)
	if credentialHandle != "" && !strings.HasPrefix(credentialHandle, "secret://") {
		return ProjectAnalyticsBucket{}, fmt.Errorf("credential_handle must be a secret:// handle")
	}
	formatVersion := input.FormatVersion
	if formatVersion == 0 {
		formatVersion = 2
	}
	if formatVersion != 1 && formatVersion != 2 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("format_version must be 1 or 2")
	}
	partitioning := strings.TrimSpace(input.Partitioning)
	if strings.ContainsAny(partitioning, "\r\n") || len(partitioning) > 256 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("partitioning is invalid")
	}
	retentionDays := input.RetentionDays
	if retentionDays < 0 || retentionDays > 3650 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("retention_days must be between 0 and 3650")
	}
	compactionSchedule := strings.TrimSpace(input.CompactionSchedule)
	if compactionSchedule == "" {
		compactionSchedule = "manual"
	}
	if strings.ContainsAny(compactionSchedule, "\r\n\t") || len(compactionSchedule) > 128 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("compaction_schedule is invalid")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectAnalyticsBucket{
		ID:                 newID(),
		ProjectRef:         ref,
		Name:               name,
		StorageURI:         storageURI,
		CatalogURI:         catalogURI,
		Warehouse:          warehouse,
		CredentialHandle:   credentialHandle,
		FormatVersion:      formatVersion,
		Partitioning:       partitioning,
		RetentionDays:      retentionDays,
		CompactionSchedule: compactionSchedule,
		Metadata:           metadata,
		Status:             "configured",
		Message:            "Iceberg analytics bucket declaration recorded",
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func validateAnalyticsStorageURI(storageURI string) error {
	parsed, err := url.Parse(storageURI)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("storage_uri is invalid")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "s3", "gs", "az", "file":
		return nil
	default:
		return fmt.Errorf("storage_uri must use s3://, gs://, az://, or file://")
	}
}

func normalizeProjectAuthHook(ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	hookType := strings.ToLower(strings.TrimSpace(input.HookType))
	if hookType == "" {
		return ProjectAuthHook{}, fmt.Errorf("hook_type is required")
	}
	if _, ok := allowedAuthHookTypes[hookType]; !ok {
		return ProjectAuthHook{}, fmt.Errorf("unsupported auth hook_type %q", hookType)
	}
	targetURI := strings.TrimSpace(input.TargetURI)
	edgeFunction := strings.TrimSpace(input.EdgeFunction)
	if edgeFunction != "" {
		normalized, err := normalizeFunctionName(edgeFunction)
		if err != nil {
			return ProjectAuthHook{}, err
		}
		edgeFunction = normalized
	}
	if input.Enabled && targetURI == "" && edgeFunction == "" {
		return ProjectAuthHook{}, fmt.Errorf("enabled auth hooks require target_uri or edge_function")
	}
	if targetURI != "" {
		parsed, err := url.Parse(targetURI)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ProjectAuthHook{}, fmt.Errorf("target_uri is invalid")
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return ProjectAuthHook{}, fmt.Errorf("target_uri must use http or https")
		}
		if len(targetURI) > 512 || strings.ContainsAny(targetURI, "\r\n\t ") {
			return ProjectAuthHook{}, fmt.Errorf("target_uri is invalid")
		}
	}
	secretHandle := strings.TrimSpace(input.SecretHandle)
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return ProjectAuthHook{}, fmt.Errorf("secret_handle must use a secret:// handle")
	}
	headers, err := normalizeConfigValues(input.Headers)
	if err != nil {
		return ProjectAuthHook{}, err
	}
	for key, value := range headers {
		if value == "" {
			continue
		}
		if isSensitiveAuthHookHeaderKey(key) && !strings.HasPrefix(value, "secret://") {
			return ProjectAuthHook{}, fmt.Errorf("auth hook header %s must use a secret:// handle", key)
		}
		if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
			return ProjectAuthHook{}, fmt.Errorf("auth hook header %s is invalid", key)
		}
	}
	timeoutMS := input.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = 5000
	}
	if timeoutMS < 100 || timeoutMS > 30000 {
		return ProjectAuthHook{}, fmt.Errorf("timeout_ms must be between 100 and 30000")
	}
	retryAttempts := input.RetryAttempts
	if retryAttempts < 0 || retryAttempts > 5 {
		return ProjectAuthHook{}, fmt.Errorf("retry_attempts must be between 0 and 5")
	}
	now := time.Now().UTC()
	status := "disabled"
	message := "Auth hook declaration recorded"
	if input.Enabled {
		status = "configured"
		message = "Auth hook declaration ready for runtime sync"
	}
	return ProjectAuthHook{
		ID:            newID(),
		ProjectRef:    ref,
		HookType:      hookType,
		Enabled:       input.Enabled,
		TargetURI:     targetURI,
		EdgeFunction:  edgeFunction,
		SecretHandle:  secretHandle,
		Headers:       headers,
		TimeoutMS:     timeoutMS,
		RetryAttempts: retryAttempts,
		Status:        status,
		Message:       message,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func isSensitiveAuthHookHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-auth-token":
		return true
	default:
		return isSensitiveProjectConfigKey(key)
	}
}

func normalizeProjectAuthClient(ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ProjectAuthClient{}, fmt.Errorf("auth client name is required")
	}
	if len(name) > 96 || strings.ContainsAny(name, "\r\n\t") {
		return ProjectAuthClient{}, fmt.Errorf("auth client name is invalid")
	}
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		clientID = "oauth_" + newID()
	}
	if len(clientID) > 128 || strings.ContainsAny(clientID, "\r\n\t ") {
		return ProjectAuthClient{}, fmt.Errorf("client_id is invalid")
	}
	secretHandle := strings.TrimSpace(input.ClientSecretHandle)
	if input.Confidential && secretHandle == "" {
		return ProjectAuthClient{}, fmt.Errorf("confidential auth clients require client_secret_handle")
	}
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return ProjectAuthClient{}, fmt.Errorf("client_secret_handle must use a secret:// handle")
	}
	redirectURIs, err := normalizeOAuthRedirectURIs(input.RedirectURIs)
	if err != nil {
		return ProjectAuthClient{}, err
	}
	grantTypes, err := normalizeOAuthValues("grant_type", input.GrantTypes, allowedOAuthClientGrantTypes, []string{"authorization_code", "refresh_token"})
	if err != nil {
		return ProjectAuthClient{}, err
	}
	scopes, err := normalizeOAuthValues("scope", input.Scopes, allowedOAuthClientScopes, []string{"openid", "profile", "email"})
	if err != nil {
		return ProjectAuthClient{}, err
	}
	now := time.Now().UTC()
	return ProjectAuthClient{
		ID:                 newID(),
		ProjectRef:         ref,
		Name:               name,
		ClientID:           clientID,
		ClientSecretHandle: secretHandle,
		RedirectURIs:       redirectURIs,
		GrantTypes:         grantTypes,
		Scopes:             scopes,
		Confidential:       input.Confidential,
		Status:             "registered",
		Message:            "OAuth 2.1 client registration recorded",
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func normalizeOAuthRedirectURIs(values []string) ([]string, error) {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("redirect_uri %q is invalid", trimmed)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return nil, fmt.Errorf("redirect_uri %q must use http or https", trimmed)
		}
		if len(trimmed) > 512 || strings.ContainsAny(trimmed, "\r\n\t ") {
			return nil, fmt.Errorf("redirect_uri %q is invalid", trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one redirect_uri is required")
	}
	sort.Strings(out)
	return out, nil
}

func normalizeOAuthValues(label string, values []string, allowed map[string]struct{}, defaults []string) ([]string, error) {
	if len(values) == 0 {
		values = defaults
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.ToLower(strings.TrimSpace(part))
			if normalized == "" {
				continue
			}
			if _, ok := allowed[normalized]; !ok {
				return nil, fmt.Errorf("unsupported OAuth %s %q", label, normalized)
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeProjectDatabaseExtension(ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if !extensionNamePattern.MatchString(normalizedName) {
		return ProjectDatabaseExtension{}, fmt.Errorf("database extension name must be a valid extension identifier")
	}
	base, ok := defaultDatabaseExtensions[normalizedName]
	if !ok {
		return ProjectDatabaseExtension{}, fmt.Errorf("unsupported database extension %q", normalizedName)
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = base.Schema
	}
	if err := validateDatabaseIdentifier("extension schema", schema); err != nil {
		return ProjectDatabaseExtension{}, err
	}
	version := strings.TrimSpace(input.Version)
	if strings.ContainsAny(version, "\r\n\t ") || len(version) > 64 {
		return ProjectDatabaseExtension{}, fmt.Errorf("extension version is invalid")
	}
	enabled := base.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	status := "disabled"
	message := "database extension disabled"
	if enabled {
		status = "enabled"
		message = "database extension enabled"
	}
	now := time.Now().UTC()
	return ProjectDatabaseExtension{
		ID:         newID(),
		ProjectRef: ref,
		Name:       normalizedName,
		Schema:     schema,
		Version:    version,
		Enabled:    enabled,
		Status:     status,
		Message:    message,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func normalizeProjectDatabaseCronJob(ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron job name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schedule := strings.TrimSpace(input.Schedule)
	if err := validateCronSchedule(schedule); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron command is required")
	}
	if len(command) > 8192 || strings.ContainsRune(command, 0) {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron command is invalid")
	}
	database := strings.ToLower(strings.TrimSpace(input.Database))
	if database == "" {
		database = "postgres"
	}
	if err := validateDatabaseIdentifier("cron database", database); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if username == "" {
		username = "postgres"
	}
	if err := validateDatabaseIdentifier("cron username", username); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 60
	}
	if timeoutSeconds < 1 || timeoutSeconds > 86400 {
		return ProjectDatabaseCronJob{}, fmt.Errorf("timeout_seconds must be between 1 and 86400")
	}
	maxRuntimeSeconds := input.MaxRuntimeSeconds
	if maxRuntimeSeconds == 0 {
		maxRuntimeSeconds = timeoutSeconds
	}
	if maxRuntimeSeconds < 1 || maxRuntimeSeconds > 86400 {
		return ProjectDatabaseCronJob{}, fmt.Errorf("max_runtime_seconds must be between 1 and 86400")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	status := "paused"
	message := "pg_cron job declaration paused"
	if input.Active {
		status = "scheduled"
		message = "pg_cron job declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseCronJob{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		Schedule:          schedule,
		Command:           command,
		Database:          database,
		Username:          username,
		Active:            input.Active,
		TimeoutSeconds:    timeoutSeconds,
		MaxRuntimeSeconds: maxRuntimeSeconds,
		Metadata:          metadata,
		Status:            status,
		Message:           message,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func validateCronSchedule(schedule string) error {
	if schedule == "" {
		return fmt.Errorf("cron schedule is required")
	}
	if len(schedule) > 128 || strings.ContainsAny(schedule, "\r\n\t") {
		return fmt.Errorf("cron schedule is invalid")
	}
	switch strings.ToLower(schedule) {
	case "@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually", "@reboot":
		return nil
	}
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return fmt.Errorf("cron schedule must be a five-field expression or supported @schedule")
	}
	for _, part := range parts {
		if len(part) > 32 || strings.ContainsAny(part, ";'\"`\\") {
			return fmt.Errorf("cron schedule field %q is invalid", part)
		}
	}
	return nil
}

func normalizeProjectDatabaseQueue(ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseQueue{}, fmt.Errorf("database queue name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = "pgmq"
	}
	if err := validateDatabaseIdentifier("queue schema", schema); err != nil {
		return ProjectDatabaseQueue{}, err
	}
	retentionMinutes := input.RetentionMinutes
	if retentionMinutes == 0 {
		retentionMinutes = 1440
	}
	if retentionMinutes < 1 || retentionMinutes > 525600 {
		return ProjectDatabaseQueue{}, fmt.Errorf("retention_minutes must be between 1 and 525600")
	}
	visibilityTimeoutSeconds := input.VisibilityTimeoutSeconds
	if visibilityTimeoutSeconds == 0 {
		visibilityTimeoutSeconds = 30
	}
	if visibilityTimeoutSeconds < 1 || visibilityTimeoutSeconds > 86400 {
		return ProjectDatabaseQueue{}, fmt.Errorf("visibility_timeout_seconds must be between 1 and 86400")
	}
	maxRetries := input.MaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}
	if maxRetries < 0 || maxRetries > 1000 {
		return ProjectDatabaseQueue{}, fmt.Errorf("max_retries must be between 0 and 1000")
	}
	deadLetterQueue := strings.ToLower(strings.TrimSpace(input.DeadLetterQueue))
	if deadLetterQueue != "" {
		if !refPattern.MatchString(deadLetterQueue) {
			return ProjectDatabaseQueue{}, fmt.Errorf("dead_letter_queue must be 3-64 lowercase letters, numbers, or dashes")
		}
		if deadLetterQueue == name {
			return ProjectDatabaseQueue{}, fmt.Errorf("dead_letter_queue must be different from name")
		}
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseQueue{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseQueue{}, err
	}
	status := "paused"
	message := "pgmq queue declaration paused"
	if input.Active {
		status = "ready"
		message = "pgmq queue declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseQueue{
		ID:                       newID(),
		ProjectRef:               ref,
		Name:                     name,
		Schema:                   schema,
		RetentionMinutes:         retentionMinutes,
		VisibilityTimeoutSeconds: visibilityTimeoutSeconds,
		MaxRetries:               maxRetries,
		DeadLetterQueue:          deadLetterQueue,
		Active:                   input.Active,
		Metadata:                 metadata,
		Status:                   status,
		Message:                  message,
		CreatedAt:                now,
		UpdatedAt:                now,
	}, nil
}

func normalizeProjectDatabaseWebhook(ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseWebhook{}, fmt.Errorf("database webhook name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = "public"
	}
	if err := validateDatabaseIdentifier("webhook schema", schema); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	table := strings.ToLower(strings.TrimSpace(input.Table))
	if err := validateDatabaseIdentifier("webhook table", table); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	events, err := normalizeDatabaseWebhookEvents(input.Events)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook endpoint must be an https URL")
	}
	if len(endpoint) > 2048 || strings.ContainsAny(endpoint, "\r\n\t") {
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook endpoint is invalid")
	}
	method := strings.ToUpper(strings.TrimSpace(input.HTTPMethod))
	if method == "" {
		method = "POST"
	}
	switch method {
	case "POST", "PUT", "PATCH":
	default:
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook http_method must be POST, PUT, or PATCH")
	}
	headers, err := normalizeConfigValues(input.Headers)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	if err := validateReplicationSecretHandles(headers); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 10
	}
	if timeoutSeconds < 1 || timeoutSeconds > 300 {
		return ProjectDatabaseWebhook{}, fmt.Errorf("timeout_seconds must be between 1 and 300")
	}
	retryCount := input.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}
	if retryCount < 0 || retryCount > 25 {
		return ProjectDatabaseWebhook{}, fmt.Errorf("retry_count must be between 0 and 25")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	status := "paused"
	message := "database webhook declaration paused"
	if input.Active {
		status = "ready"
		message = "database webhook declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseWebhook{
		ID:             newID(),
		ProjectRef:     ref,
		Name:           name,
		Schema:         schema,
		Table:          table,
		Events:         events,
		Endpoint:       endpoint,
		HTTPMethod:     method,
		Headers:        headers,
		TimeoutSeconds: timeoutSeconds,
		RetryCount:     retryCount,
		Active:         input.Active,
		Metadata:       metadata,
		Status:         status,
		Message:        message,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeDatabaseWebhookEvents(input []string) ([]string, error) {
	if len(input) == 0 {
		input = []string{"insert", "update", "delete"}
	}
	allowed := map[string]struct{}{"insert": {}, "update": {}, "delete": {}}
	seen := map[string]struct{}{}
	out := []string{}
	for _, event := range input {
		normalized := strings.ToLower(strings.TrimSpace(event))
		if _, ok := allowed[normalized]; !ok {
			return nil, fmt.Errorf("webhook event %q is not supported", event)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("webhook events are required")
	}
	sort.Strings(out)
	return out, nil
}

func normalizeProjectDatabaseSchema(ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema name must be 3-64 lowercase letters, numbers, or dashes")
	}
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema version is required")
	}
	if len(version) > 128 || strings.ContainsAny(version, "\r\n\t ") {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema version is invalid")
	}
	schemaName := strings.ToLower(strings.TrimSpace(input.Schema))
	if schemaName == "" {
		schemaName = "public"
	}
	if err := validateDatabaseIdentifier("database schema", schemaName); err != nil {
		return ProjectDatabaseSchema{}, err
	}
	sql := strings.TrimSpace(input.SQL)
	if sql == "" {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema sql is required")
	}
	if len(sql) > 262144 || strings.ContainsRune(sql, 0) {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema sql is invalid")
	}
	applyOrder := input.ApplyOrder
	if applyOrder < 0 || applyOrder > 1000000 {
		return ProjectDatabaseSchema{}, fmt.Errorf("apply_order must be between 0 and 1000000")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseSchema{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseSchema{}, err
	}
	sum := sha256.Sum256([]byte(sql))
	status := "paused"
	message := "declarative schema migration paused"
	if input.Active {
		status = "pending"
		message = "declarative schema migration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseSchema{
		ID:         newID(),
		ProjectRef: ref,
		Name:       name,
		Version:    version,
		Schema:     schemaName,
		SQL:        sql,
		Checksum:   hex.EncodeToString(sum[:]),
		ApplyOrder: applyOrder,
		Active:     input.Active,
		Metadata:   metadata,
		Status:     status,
		Message:    message,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func mergedDatabaseExtensions(ref string, overrides []ProjectDatabaseExtension) []ProjectDatabaseExtension {
	now := time.Now().UTC()
	byName := map[string]ProjectDatabaseExtension{}
	for _, name := range defaultDatabaseExtensionOrder {
		base := defaultDatabaseExtensions[name]
		base.ID = "default:" + name
		base.ProjectRef = ref
		base.CreatedAt = now
		base.UpdatedAt = now
		byName[name] = base
	}
	for _, override := range overrides {
		override.ProjectRef = ref
		if strings.TrimSpace(override.Status) == "" {
			if override.Enabled {
				override.Status = "enabled"
			} else {
				override.Status = "disabled"
			}
		}
		byName[override.Name] = override
	}
	extensions := make([]ProjectDatabaseExtension, 0, len(byName))
	seen := map[string]struct{}{}
	for _, name := range defaultDatabaseExtensionOrder {
		if extension, ok := byName[name]; ok {
			extensions = append(extensions, extension)
			seen[name] = struct{}{}
		}
	}
	for name, extension := range byName {
		if _, ok := seen[name]; ok {
			continue
		}
		extensions = append(extensions, extension)
	}
	sort.SliceStable(extensions, func(i, j int) bool {
		leftDefault := indexOfDefaultDatabaseExtension(extensions[i].Name)
		rightDefault := indexOfDefaultDatabaseExtension(extensions[j].Name)
		if leftDefault >= 0 && rightDefault >= 0 {
			return leftDefault < rightDefault
		}
		if leftDefault >= 0 {
			return true
		}
		if rightDefault >= 0 {
			return false
		}
		return extensions[i].Name < extensions[j].Name
	})
	return extensions
}

func indexOfDefaultDatabaseExtension(name string) int {
	for index, candidate := range defaultDatabaseExtensionOrder {
		if candidate == name {
			return index
		}
	}
	return -1
}

func countEnabledDatabaseExtensions(ref string, overrides []ProjectDatabaseExtension) int {
	count := 0
	for _, extension := range mergedDatabaseExtensions(ref, overrides) {
		if extension.Enabled {
			count++
		}
	}
	return count
}

func normalizeProjectDatabaseRole(ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if err := validateDatabaseIdentifier("database role name", name); err != nil {
		return ProjectDatabaseRole{}, err
	}
	if _, reserved := reservedDatabaseRoles[name]; reserved || strings.HasPrefix(name, "pg_") {
		return ProjectDatabaseRole{}, fmt.Errorf("database role %q is reserved by the upstream stack", name)
	}
	inherit := true
	if input.Inherit != nil {
		inherit = *input.Inherit
	}
	connectionLimit := input.ConnectionLimit
	if connectionLimit < -1 {
		return ProjectDatabaseRole{}, fmt.Errorf("connection_limit must be -1 or greater")
	}
	passwordHandle := strings.TrimSpace(input.PasswordSecretHandle)
	if input.Login && passwordHandle == "" {
		return ProjectDatabaseRole{}, fmt.Errorf("login database roles require password_secret_handle")
	}
	if passwordHandle != "" && !strings.HasPrefix(passwordHandle, "secret://") {
		return ProjectDatabaseRole{}, fmt.Errorf("password_secret_handle must use a secret:// handle")
	}
	memberOf, err := normalizeDatabaseRoleMembers(input.MemberOf, name)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	schemaGrants, err := normalizeDatabaseSchemaGrants(input.SchemaGrants)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseRole{}, err
	}
	now := time.Now().UTC()
	return ProjectDatabaseRole{
		ID:                   newID(),
		ProjectRef:           ref,
		Name:                 name,
		Login:                input.Login,
		Inherit:              inherit,
		BypassRLS:            input.BypassRLS,
		ConnectionLimit:      connectionLimit,
		PasswordSecretHandle: passwordHandle,
		MemberOf:             memberOf,
		SchemaGrants:         schemaGrants,
		Metadata:             metadata,
		Status:               "configured",
		Message:              "database role declaration recorded",
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

func validateDatabaseIdentifier(label string, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s must be a valid PostgreSQL identifier", label)
	}
	return nil
}

func normalizeDatabaseRoleMembers(values []string, roleName string) ([]string, error) {
	members := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		member := strings.ToLower(strings.TrimSpace(value))
		if member == "" {
			continue
		}
		if err := validateDatabaseIdentifier("member role", member); err != nil {
			return nil, err
		}
		if member == roleName {
			return nil, fmt.Errorf("database role cannot be member of itself")
		}
		if _, ok := seen[member]; ok {
			continue
		}
		seen[member] = struct{}{}
		members = append(members, member)
	}
	sort.Strings(members)
	return members, nil
}

func normalizeDatabaseSchemaGrants(values map[string]string) (map[string]string, error) {
	grants := map[string]string{}
	for schema, privilegeList := range values {
		normalizedSchema := strings.ToLower(strings.TrimSpace(schema))
		if normalizedSchema == "" {
			continue
		}
		if err := validateDatabaseIdentifier("schema grant name", normalizedSchema); err != nil {
			return nil, err
		}
		privileges := []string{}
		seen := map[string]struct{}{}
		for _, privilege := range strings.Split(privilegeList, ",") {
			normalizedPrivilege := strings.ToLower(strings.TrimSpace(privilege))
			if normalizedPrivilege == "" {
				continue
			}
			if _, ok := allowedDatabaseRolePrivileges[normalizedPrivilege]; !ok {
				return nil, fmt.Errorf("unsupported database role privilege %q", normalizedPrivilege)
			}
			if _, ok := seen[normalizedPrivilege]; ok {
				continue
			}
			seen[normalizedPrivilege] = struct{}{}
			privileges = append(privileges, normalizedPrivilege)
		}
		sort.Strings(privileges)
		if len(privileges) > 0 {
			grants[normalizedSchema] = strings.Join(privileges, ",")
		}
	}
	return grants, nil
}

func normalizeProjectStorageBucket(ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectStorageBucket{}, fmt.Errorf("storage bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	fileSizeLimit := input.FileSizeLimit
	if fileSizeLimit < 0 {
		return ProjectStorageBucket{}, fmt.Errorf("file_size_limit cannot be negative")
	}
	if fileSizeLimit == 0 {
		fileSizeLimit = 50 * 1024 * 1024
	}
	mimeTypes := make([]string, 0, len(input.AllowedMimeTypes))
	seen := map[string]struct{}{}
	for _, value := range input.AllowedMimeTypes {
		mimeType := strings.ToLower(strings.TrimSpace(value))
		if mimeType == "" {
			continue
		}
		if len(mimeType) > 128 || strings.ContainsAny(mimeType, "\r\n\t ") || !strings.Contains(mimeType, "/") {
			return ProjectStorageBucket{}, fmt.Errorf("allowed_mime_types contains invalid value %q", value)
		}
		if _, ok := seen[mimeType]; ok {
			continue
		}
		seen[mimeType] = struct{}{}
		mimeTypes = append(mimeTypes, mimeType)
	}
	sort.Strings(mimeTypes)
	cacheControl := strings.TrimSpace(input.CacheControl)
	if cacheControl == "" {
		cacheControl = "3600"
	}
	if strings.ContainsAny(cacheControl, "\r\n") || len(cacheControl) > 128 {
		return ProjectStorageBucket{}, fmt.Errorf("cache_control is invalid")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectStorageBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectStorageBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectStorageBucket{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		Public:            input.Public,
		FileSizeLimit:     fileSizeLimit,
		AllowedMimeTypes:  mimeTypes,
		CacheControl:      cacheControl,
		AvifAutodetection: input.AvifAutodetection,
		Metadata:          metadata,
		Status:            "configured",
		Message:           "storage bucket declaration recorded",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func normalizeProjectNetworkConnection(ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(input.Provider)) + "-" + strings.ToLower(strings.TrimSpace(input.Type))
	}
	if !refPattern.MatchString(name) {
		return ProjectNetworkConnection{}, fmt.Errorf("network connection name must be 3-64 lowercase letters, numbers, or dashes")
	}
	connectionType := strings.ToLower(strings.TrimSpace(input.Type))
	if connectionType == "" {
		connectionType = "operator_network"
	}
	if _, ok := allowedNetworkConnectionTypes[connectionType]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("unsupported network connection type %q", connectionType)
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "operator"
	}
	if _, ok := allowedNetworkConnectionProviders[provider]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("unsupported network connection provider %q", provider)
	}
	cidrs, err := normalizeNetworkCIDRs(input.CIDRs)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}
	if err := validateReplicationSecretHandles(config); err != nil {
		return ProjectNetworkConnection{}, err
	}
	region := strings.ToLower(strings.TrimSpace(input.Region))
	if len(region) > 64 || strings.ContainsAny(region, " \r\n\t") {
		return ProjectNetworkConnection{}, fmt.Errorf("region is invalid")
	}
	endpointID := strings.TrimSpace(input.EndpointID)
	if len(endpointID) > 160 || strings.ContainsAny(endpointID, "\r\n\t") {
		return ProjectNetworkConnection{}, fmt.Errorf("endpoint_id is invalid")
	}
	now := time.Now().UTC()
	return ProjectNetworkConnection{
		ID:         newID(),
		ProjectRef: ref,
		Name:       name,
		Type:       connectionType,
		Provider:   provider,
		Region:     region,
		CIDRs:      cidrs,
		EndpointID: endpointID,
		Config:     config,
		Status:     "requested",
		Message:    "private network connection declaration recorded",
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func normalizeNetworkCIDRs(input []string) ([]string, error) {
	out := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.TrimSpace(part)
			if normalized == "" {
				continue
			}
			if prefix, err := netip.ParsePrefix(normalized); err == nil {
				normalized = prefix.String()
			} else if addr, err := netip.ParseAddr(normalized); err == nil {
				normalized = addr.String()
			} else {
				return nil, fmt.Errorf("invalid network cidr %q", part)
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one cidr is required")
	}
	return out, nil
}

func normalizeOptionalNetworkCIDRs(input []string) ([]string, error) {
	out, err := normalizeNetworkCIDRs(input)
	if err != nil {
		if strings.Contains(err.Error(), "at least one cidr is required") {
			return []string{}, nil
		}
		return nil, err
	}
	return out, nil
}

func isSensitiveProjectConfigKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "api_key", "token", "secret", "password", "access_key", "secret_key", "access_token", "bearer_token", "authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func validateLogDrainConfig(target string, config map[string]string) error {
	switch target {
	case "https", "loki", "sentry", "axiom":
		if strings.TrimSpace(config["url"]) == "" {
			return fmt.Errorf("log drain %s target requires url", target)
		}
	case "datadog":
		if strings.TrimSpace(config["api_key"]) == "" {
			return fmt.Errorf("log drain datadog target requires api_key")
		}
	case "s3":
		if strings.TrimSpace(config["bucket"]) == "" {
			return fmt.Errorf("log drain s3 target requires bucket")
		}
	}
	return nil
}

func defaultProjectCDNPolicy(ref string) ProjectCDNPolicy {
	return ProjectCDNPolicy{
		ProjectRef:                  ref,
		Enabled:                     false,
		BrowserTTLSeconds:           3600,
		EdgeTTLSeconds:              3600,
		StaleWhileRevalidateSeconds: 60,
		IncludedPaths:               []string{"/storage/v1/object/public/*"},
		ExcludedPaths:               []string{},
		SmartRevalidation:           false,
		CacheControl:                "public, max-age=3600, s-maxage=3600, stale-while-revalidate=60",
		UpdatedAt:                   time.Now().UTC(),
	}
}

func normalizeProjectCDNPolicy(ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	browserTTL := input.BrowserTTLSeconds
	if browserTTL == 0 {
		browserTTL = 3600
	}
	edgeTTL := input.EdgeTTLSeconds
	if edgeTTL == 0 {
		edgeTTL = browserTTL
	}
	staleTTL := input.StaleWhileRevalidateSeconds
	if staleTTL == 0 {
		staleTTL = 60
	}
	for name, value := range map[string]int{
		"browser_ttl_seconds":            browserTTL,
		"edge_ttl_seconds":               edgeTTL,
		"stale_while_revalidate_seconds": staleTTL,
	} {
		if value < 0 || value > 31536000 {
			return ProjectCDNPolicy{}, fmt.Errorf("%s must be between 0 and 31536000", name)
		}
	}
	included, err := normalizeCDNPaths(input.IncludedPaths, true)
	if err != nil {
		return ProjectCDNPolicy{}, fmt.Errorf("included_paths %w", err)
	}
	if len(included) == 0 {
		included = []string{"/storage/v1/object/public/*"}
	}
	excluded, err := normalizeCDNPaths(input.ExcludedPaths, true)
	if err != nil {
		return ProjectCDNPolicy{}, fmt.Errorf("excluded_paths %w", err)
	}
	cacheControl := strings.TrimSpace(input.CacheControl)
	if cacheControl == "" {
		cacheControl = fmt.Sprintf("public, max-age=%d, s-maxage=%d", browserTTL, edgeTTL)
		if staleTTL > 0 {
			cacheControl += fmt.Sprintf(", stale-while-revalidate=%d", staleTTL)
		}
	}
	if err := validateCacheControl(cacheControl); err != nil {
		return ProjectCDNPolicy{}, err
	}
	return ProjectCDNPolicy{
		ProjectRef:                  ref,
		Enabled:                     input.Enabled,
		BrowserTTLSeconds:           browserTTL,
		EdgeTTLSeconds:              edgeTTL,
		StaleWhileRevalidateSeconds: staleTTL,
		IncludedPaths:               included,
		ExcludedPaths:               excluded,
		SmartRevalidation:           input.SmartRevalidation,
		CacheControl:                cacheControl,
		UpdatedAt:                   time.Now().UTC(),
	}, nil
}

func normalizeCDNPaths(paths []string, allowEmpty bool) ([]string, error) {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		normalized := strings.TrimSpace(path)
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, "/") {
			return nil, fmt.Errorf("path %q must start with /", path)
		}
		if strings.ContainsAny(normalized, "\r\n\t") || strings.Contains(normalized, "..") {
			return nil, fmt.Errorf("path %q is invalid", path)
		}
		if len(normalized) > 256 {
			return nil, fmt.Errorf("path %q is too long", path)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 && !allowEmpty {
		return nil, fmt.Errorf("at least one path is required")
	}
	return out, nil
}

func normalizeCDNObjectEvent(input CDNObjectEventInput) (CDNObjectEventInput, error) {
	eventID := strings.TrimSpace(input.EventID)
	if len(eventID) > 160 || strings.ContainsAny(eventID, "\r\n\t") {
		return CDNObjectEventInput{}, fmt.Errorf("event_id is invalid")
	}
	eventType := strings.ToLower(strings.TrimSpace(input.EventType))
	if eventType == "" {
		eventType = "object_changed"
	}
	switch eventType {
	case "object_created", "object_updated", "object_deleted", "object_changed":
	default:
		return CDNObjectEventInput{}, fmt.Errorf("unsupported storage event_type %q", eventType)
	}
	bucket := strings.ToLower(strings.TrimSpace(input.Bucket))
	if bucket != "" && !refPattern.MatchString(bucket) {
		return CDNObjectEventInput{}, fmt.Errorf("bucket must be 3-64 lowercase letters, numbers, or dashes")
	}
	objectPath := strings.Trim(strings.TrimSpace(input.ObjectPath), "/")
	if objectPath == "" {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is required")
	}
	if strings.ContainsAny(objectPath, "\r\n\t") || strings.Contains(objectPath, "..") {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is invalid")
	}
	if len(objectPath) > 512 {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is too long")
	}
	return CDNObjectEventInput{
		EventID:    eventID,
		Bucket:     bucket,
		ObjectPath: objectPath,
		EventType:  eventType,
	}, nil
}

func storageObjectCDNPath(bucket string, objectPath string) string {
	objectPath = strings.Trim(strings.TrimSpace(objectPath), "/")
	if bucket == "" {
		return "/storage/v1/object/public/" + objectPath
	}
	return "/storage/v1/object/public/" + bucket + "/" + objectPath
}

func cdnPathIncluded(path string, included []string, excluded []string) bool {
	for _, pattern := range excluded {
		if cdnPathPatternMatches(pattern, path) {
			return false
		}
	}
	for _, pattern := range included {
		if cdnPathPatternMatches(pattern, path) {
			return true
		}
	}
	return false
}

func cdnPathPatternMatches(pattern string, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

func validateCacheControl(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("cache_control cannot contain line breaks")
	}
	if len(value) > 256 {
		return fmt.Errorf("cache_control is too long")
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "private") || strings.Contains(lower, "no-store") {
		return fmt.Errorf("cache_control must describe a public edge-cacheable response")
	}
	return nil
}
