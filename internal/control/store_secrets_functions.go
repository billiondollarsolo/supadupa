package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func (s *MemoryStore) ListProjectFunctions(ctx context.Context, ref string) ([]ProjectFunction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functions[ref], cloneProjectFunctions, func(left, right ProjectFunction) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) DeployProjectFunction(ctx context.Context, ref string, input ProjectFunctionInput) (ProjectFunction, error) {
	name, err := normalizeFunctionName(input.Name)
	if err != nil {
		return ProjectFunction{}, err
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		return ProjectFunction{}, fmt.Errorf("function source is required")
	}
	entrypoint := strings.TrimSpace(input.Entrypoint)
	if entrypoint == "" {
		entrypoint = "index.ts"
	}
	entrypoint, err = normalizeFunctionEntrypoint(entrypoint)
	if err != nil {
		return ProjectFunction{}, err
	}
	secrets, err := normalizeFunctionSecretValues(input.Secrets)
	if err != nil {
		return ProjectFunction{}, err
	}

	now := time.Now().UTC()
	sum := sha256.Sum256([]byte(source))
	next := ProjectFunction{
		ID:          newID(),
		ProjectRef:  ref,
		Name:        name,
		Version:     1,
		Entrypoint:  entrypoint,
		VerifyJWT:   input.VerifyJWT,
		Status:      "deployed",
		SourceHash:  hex.EncodeToString(sum[:]),
		SourceBytes: len([]byte(source)),
		Secrets:     maskFunctionSecrets(secrets),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunction{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	functions := s.functions[ref]
	for index, existing := range functions {
		if existing.Name == name {
			next.ID = existing.ID
			next.Version = existing.Version + 1
			next.CreatedAt = existing.CreatedAt
			functions[index] = next
			s.functions[ref] = functions
			return cloneProjectFunction(next), nil
		}
	}
	s.functions[ref] = append(functions, next)
	return cloneProjectFunction(next), nil
}

func (s *MemoryStore) DeleteProjectFunction(ctx context.Context, ref string, name string) error {
	normalized, err := normalizeFunctionName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	functions := s.functions[ref]
	for index, function := range functions {
		if function.Name == normalized {
			s.functions[ref] = append(functions[:index], functions[index+1:]...)
			s.functionRegions[ref] = removeFunctionRegions(s.functionRegions[ref], normalized)
			s.functionStorageMounts[ref] = removeFunctionStorageMounts(s.functionStorageMounts[ref], normalized, "")
			return nil
		}
	}
	return fmt.Errorf("%w: function %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) ListProjectFunctionRegions(ctx context.Context, ref string) ([]ProjectFunctionRegion, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functionRegions[ref], cloneProjectFunctionRegions, func(left, right ProjectFunctionRegion) bool {
		if left.FunctionName != right.FunctionName {
			return left.FunctionName < right.FunctionName
		}
		return left.Region < right.Region
	}), nil
}

func (s *MemoryStore) CreateProjectFunctionRegion(ctx context.Context, ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	region, err := normalizeProjectFunctionRegion(ref, input)
	if err != nil {
		return ProjectFunctionRegion{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunctionRegion{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if !functionExists(s.functions[ref], region.FunctionName) {
		return ProjectFunctionRegion{}, fmt.Errorf("%w: function %s for project %s", ErrNotFound, region.FunctionName, ref)
	}
	if region.HostID != "" {
		if _, ok := s.hosts[region.HostID]; !ok {
			return ProjectFunctionRegion{}, fmt.Errorf("%w: host %s", ErrNotFound, region.HostID)
		}
	}
	for index, existing := range s.functionRegions[ref] {
		if existing.FunctionName == region.FunctionName && existing.Region == region.Region {
			region.ID = existing.ID
			region.CreatedAt = existing.CreatedAt
			s.functionRegions[ref][index] = region
			return cloneProjectFunctionRegion(region), nil
		}
	}
	s.functionRegions[ref] = append(s.functionRegions[ref], region)
	return cloneProjectFunctionRegion(region), nil
}

func (s *MemoryStore) DeleteProjectFunctionRegion(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("function region id is required")
	}
	ref = strings.ToLower(strings.TrimSpace(ref))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	regions := s.functionRegions[ref]
	for index, region := range regions {
		if region.ID == id {
			s.functionRegions[ref] = append(regions[:index], regions[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: function region %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectFunctionStorageMounts(ctx context.Context, ref string) ([]ProjectFunctionStorageMount, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functionStorageMounts[ref], cloneProjectFunctionStorageMounts, func(left, right ProjectFunctionStorageMount) bool {
		if left.FunctionName != right.FunctionName {
			return left.FunctionName < right.FunctionName
		}
		return left.MountPath < right.MountPath
	}), nil
}

func (s *MemoryStore) CreateProjectFunctionStorageMount(ctx context.Context, ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	mount, err := normalizeProjectFunctionStorageMount(ref, input)
	if err != nil {
		return ProjectFunctionStorageMount{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if !functionExists(s.functions[ref], mount.FunctionName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: function %s for project %s", ErrNotFound, mount.FunctionName, ref)
	}
	if !storageBucketExists(s.storageBuckets[ref], mount.BucketName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, mount.BucketName, ref)
	}
	for _, existing := range s.functionStorageMounts[ref] {
		if existing.FunctionName == mount.FunctionName && existing.MountPath == mount.MountPath {
			return ProjectFunctionStorageMount{}, fmt.Errorf("%w: function storage mount %s for project %s", ErrConflict, mount.MountPath, ref)
		}
	}
	s.functionStorageMounts[ref] = append(s.functionStorageMounts[ref], mount)
	return cloneProjectFunctionStorageMount(mount), nil
}

func (s *MemoryStore) DeleteProjectFunctionStorageMount(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("function storage mount id is required")
	}
	ref = strings.ToLower(strings.TrimSpace(ref))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	mounts := s.functionStorageMounts[ref]
	for index, mount := range mounts {
		if mount.ID == id {
			s.functionStorageMounts[ref] = append(mounts[:index], mounts[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: function storage mount %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectReplicationPipelines(ctx context.Context, ref string) ([]ProjectReplicationPipeline, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.replicationPipelines[ref], cloneReplicationPipelines, func(left, right ProjectReplicationPipeline) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectReplicationPipeline(ctx context.Context, ref string, input ProjectReplicationPipelineInput) (ProjectReplicationPipeline, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	pipelineType, err := normalizeReplicationPipelineType(input.Type)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	destination, err := normalizeReplicationDestination(input.Destination)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	sourceSchema := strings.ToLower(strings.TrimSpace(input.SourceSchema))
	if sourceSchema == "" {
		sourceSchema = "public"
	}
	sourceTable := strings.ToLower(strings.TrimSpace(input.SourceTable))
	if sourceTable == "" {
		return ProjectReplicationPipeline{}, fmt.Errorf("source_table is required")
	}
	if !identifierPattern.MatchString(sourceSchema) || !identifierPattern.MatchString(sourceTable) {
		return ProjectReplicationPipeline{}, fmt.Errorf("source schema and table must be simple Postgres identifiers")
	}
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = sourceTable + "-" + pipelineType
	}
	if !refPattern.MatchString(name) {
		return ProjectReplicationPipeline{}, fmt.Errorf("pipeline name must be 3-64 lowercase letters, numbers, or dashes")
	}
	destinationURI := strings.TrimSpace(input.DestinationURI)
	if err := validateReplicationDestinationConfig(destination, destinationURI, config); err != nil {
		return ProjectReplicationPipeline{}, err
	}
	if err := validateReplicationSecretHandles(config); err != nil {
		return ProjectReplicationPipeline{}, err
	}
	credentialHandle := strings.TrimSpace(input.CredentialHandle)
	if credentialHandle != "" && !strings.HasPrefix(credentialHandle, "secret://") {
		return ProjectReplicationPipeline{}, fmt.Errorf("credential_handle must be a secret:// handle")
	}

	now := time.Now().UTC()
	pipeline := ProjectReplicationPipeline{
		ID:               newID(),
		ProjectRef:       ref,
		Name:             name,
		Type:             pipelineType,
		SourceSchema:     sourceSchema,
		SourceTable:      sourceTable,
		Destination:      destination,
		DestinationURI:   destinationURI,
		CredentialHandle: credentialHandle,
		Config:           config,
		Status:           "configured",
		Message:          "declarative replication pipeline recorded",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectReplicationPipeline{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.replicationPipelines[ref] {
		if existing.Name == name {
			return ProjectReplicationPipeline{}, fmt.Errorf("%w: replication pipeline %s for project %s already exists", ErrConflict, name, ref)
		}
	}
	s.replicationPipelines[ref] = append(s.replicationPipelines[ref], pipeline)
	return cloneReplicationPipeline(pipeline), nil
}

func (s *MemoryStore) DeleteProjectReplicationPipeline(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("replication pipeline id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	pipelines := s.replicationPipelines[ref]
	for index, pipeline := range pipelines {
		if pipeline.ID == id {
			s.replicationPipelines[ref] = append(pipelines[:index], pipelines[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: replication pipeline %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectEmbeddingJobs(ctx context.Context, ref string) ([]ProjectEmbeddingJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.embeddingJobs[ref], cloneEmbeddingJobs, func(left, right ProjectEmbeddingJob) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectEmbeddingJob(ctx context.Context, ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	job, err := normalizeProjectEmbeddingJob(ref, input)
	if err != nil {
		return ProjectEmbeddingJob{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectEmbeddingJob{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.embeddingJobs[ref] {
		if existing.Name == job.Name {
			return ProjectEmbeddingJob{}, fmt.Errorf("%w: embedding job %s for project %s already exists", ErrConflict, job.Name, ref)
		}
	}
	s.embeddingJobs[ref] = append(s.embeddingJobs[ref], job)
	return job, nil
}

func (s *MemoryStore) DeleteProjectEmbeddingJob(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("embedding job id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	jobs := s.embeddingJobs[ref]
	for index, job := range jobs {
		if job.ID == id {
			s.embeddingJobs[ref] = append(jobs[:index], jobs[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: embedding job %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) EnsureProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if len(s.secrets[ref]) == 0 {
		s.secrets[ref] = generateProjectSecrets(ref)
	} else {
		ensureProjectSigningKeys(ref, s.secrets[ref])
		ensureSupabaseAPIKeys(ref, s.secrets[ref], time.Now().UTC())
	}
	return secretsToSlice(s.secrets[ref]), nil
}

func (s *MemoryStore) ListProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return secretsToSlice(s.secrets[ref]), nil
}

func (s *MemoryStore) RevealProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error) {
	normalizedKind, err := normalizeProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		return ProjectSecret{}, fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	return secret, nil
}

func (s *MemoryStore) UpsertProjectSecret(ctx context.Context, ref string, kind string, input ProjectSecretInput) (ProjectSecret, error) {
	normalizedKind, err := normalizeCustomProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}
	value := strings.TrimSpace(input.Value)
	if value == "" {
		return ProjectSecret{}, fmt.Errorf("secret value is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if s.secrets[ref] == nil {
		s.secrets[ref] = map[string]ProjectSecret{}
	}
	now := time.Now().UTC()
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		secret = ProjectSecret{
			ID:         newID(),
			ProjectRef: ref,
			Kind:       normalizedKind,
			CreatedAt:  now,
		}
	} else {
		secret.RotatedAt = &now
	}
	secret.Value = value
	secret.Masked = maskSecret(value)
	s.secrets[ref][normalizedKind] = secret
	return secret, nil
}

func (s *MemoryStore) DeleteProjectSecret(ctx context.Context, ref string, kind string) error {
	normalizedKind, err := normalizeCustomProjectSecretKind(kind)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if _, ok := s.secrets[ref][normalizedKind]; !ok {
		return fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	delete(s.secrets[ref], normalizedKind)
	return nil
}

func (s *MemoryStore) RotateProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error) {
	normalizedKind, err := normalizeManagedProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}
	if strings.HasPrefix(normalizedKind, "jwt_signing_key_previous_") {
		return ProjectSecret{}, fmt.Errorf("archived JWT signing keys cannot be rotated")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		return ProjectSecret{}, fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	if normalizedKind == "jwt_signing_key_current" {
		return s.rotateCurrentJWTSigningKeyLocked(ref, secret), nil
	}
	now := time.Now().UTC()
	value := randomSecretValue(ref, normalizedKind)
	switch normalizedKind {
	case "anon_key":
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
		value = supabaseRoleJWT(ref, "anon", s.secrets[ref]["jwt_secret"].Value)
	case "service_role":
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
		value = supabaseRoleJWT(ref, "service_role", s.secrets[ref]["jwt_secret"].Value)
	}
	secret.Value = value
	secret.Masked = maskSecret(value)
	secret.RotatedAt = &now
	s.secrets[ref][normalizedKind] = secret
	if normalizedKind == "jwt_secret" {
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
	}
	return secret, nil
}

func (s *MemoryStore) rotateCurrentJWTSigningKeyLocked(ref string, current ProjectSecret) ProjectSecret {
	now := time.Now().UTC()
	archiveKind := fmt.Sprintf("jwt_signing_key_previous_%s", now.Format("20060102t150405z"))
	current.Kind = archiveKind
	current.Value = updateJWTSigningKeyMaterialStatus(current.Value, "previous")
	current.Masked = maskSecret(current.Value)
	current.RotatedAt = &now
	s.secrets[ref][archiveKind] = current

	next, ok := s.secrets[ref]["jwt_signing_key_next"]
	if !ok {
		next = newProjectSecret(ref, "jwt_signing_key_next", now)
	}
	next.Kind = "jwt_signing_key_current"
	next.Value = updateJWTSigningKeyMaterialStatus(next.Value, "current")
	next.Masked = maskSecret(next.Value)
	next.RotatedAt = &now
	s.secrets[ref]["jwt_signing_key_current"] = next
	s.secrets[ref]["jwt_signing_key_next"] = newProjectSecret(ref, "jwt_signing_key_next", now)
	return next
}
