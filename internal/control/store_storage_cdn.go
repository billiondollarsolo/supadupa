package control

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *MemoryStore) ListProjectStorageBuckets(ctx context.Context, ref string) ([]ProjectStorageBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.storageBuckets[ref], cloneStorageBuckets, func(left, right ProjectStorageBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectStorageBucket(ctx context.Context, ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectStorageBucket(ref, input)
	if err != nil {
		return ProjectStorageBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectStorageBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.storageBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectStorageBucket{}, fmt.Errorf("%w: storage bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.storageBuckets[ref] = append(s.storageBuckets[ref], bucket)
	return cloneStorageBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectStorageBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("storage bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.storageBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.storageBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			s.functionStorageMounts[ref] = removeFunctionStorageMounts(s.functionStorageMounts[ref], "", name)
			return nil
		}
	}
	return fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectVectorBuckets(ctx context.Context, ref string) ([]ProjectVectorBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.vectorBuckets[ref], cloneVectorBuckets, func(left, right ProjectVectorBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectVectorBucket(ctx context.Context, ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectVectorBucket(ref, input)
	if err != nil {
		return ProjectVectorBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.vectorBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectVectorBucket{}, fmt.Errorf("%w: vector bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.vectorBuckets[ref] = append(s.vectorBuckets[ref], bucket)
	return cloneVectorBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectVectorBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("vector bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.vectorBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.vectorBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: vector bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectAnalyticsBuckets(ctx context.Context, ref string) ([]ProjectAnalyticsBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.analyticsBuckets[ref], cloneAnalyticsBuckets, func(left, right ProjectAnalyticsBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectAnalyticsBucket(ctx context.Context, ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectAnalyticsBucket(ref, input)
	if err != nil {
		return ProjectAnalyticsBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAnalyticsBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.analyticsBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectAnalyticsBucket{}, fmt.Errorf("%w: analytics bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.analyticsBuckets[ref] = append(s.analyticsBuckets[ref], bucket)
	return cloneAnalyticsBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectAnalyticsBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("analytics bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.analyticsBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.analyticsBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: analytics bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) GetProjectCDNPolicy(ctx context.Context, ref string) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectCDNPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if policy, ok := s.cdnPolicies[ref]; ok {
		return cloneProjectCDNPolicy(policy), nil
	}
	return defaultProjectCDNPolicy(ref), nil
}

func (s *MemoryStore) UpdateProjectCDNPolicy(ctx context.Context, ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	policy, err := normalizeProjectCDNPolicy(ref, input)
	if err != nil {
		return ProjectCDNPolicy{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectCDNPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	s.cdnPolicies[ref] = cloneProjectCDNPolicy(policy)
	return cloneProjectCDNPolicy(policy), nil
}

func (s *MemoryStore) ListProjectCDNInvalidations(ctx context.Context, ref string) ([]CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.cdnInvalidations[ref], cloneCDNInvalidations, func(left, right CDNInvalidation) bool {
		return left.CreatedAt.After(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectCDNInvalidation(ctx context.Context, ref string, input CDNInvalidationInput) (CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	paths, err := normalizeCDNPaths(input.Paths, false)
	if err != nil {
		return CDNInvalidation{}, err
	}
	return s.createProjectCDNInvalidationLocked(ref, paths, "manual", "", "edge cache invalidation recorded")
}

func (s *MemoryStore) CreateProjectCDNObjectEvent(ctx context.Context, ref string, input CDNObjectEventInput) (CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	event, err := normalizeCDNObjectEvent(input)
	if err != nil {
		return CDNInvalidation{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return CDNInvalidation{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.cdnPolicies[ref]
	if !ok {
		policy = defaultProjectCDNPolicy(ref)
	}
	if !policy.Enabled || !policy.SmartRevalidation {
		return CDNInvalidation{}, fmt.Errorf("%w: smart cdn revalidation is disabled for project %s", ErrNotFound, ref)
	}
	if event.Bucket != "" && !storageBucketExists(s.storageBuckets[ref], event.Bucket) {
		return CDNInvalidation{}, fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, event.Bucket, ref)
	}
	path := storageObjectCDNPath(event.Bucket, event.ObjectPath)
	if !cdnPathIncluded(path, policy.IncludedPaths, policy.ExcludedPaths) {
		return CDNInvalidation{}, fmt.Errorf("object path %s is outside cdn policy scope", path)
	}
	return s.createProjectCDNInvalidationAlreadyLocked(ref, []string{path}, "storage_object_event", event.EventID, "smart cdn revalidation recorded for "+event.EventType)
}

func (s *MemoryStore) createProjectCDNInvalidationLocked(ref string, paths []string, source string, eventID string, message string) (CDNInvalidation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return CDNInvalidation{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return s.createProjectCDNInvalidationAlreadyLocked(ref, paths, source, eventID, message)
}

func (s *MemoryStore) createProjectCDNInvalidationAlreadyLocked(ref string, paths []string, source string, eventID string, message string) (CDNInvalidation, error) {
	now := time.Now().UTC()
	invalidation := CDNInvalidation{
		ID:          newID(),
		ProjectRef:  ref,
		Paths:       paths,
		Source:      source,
		EventID:     eventID,
		Status:      "completed",
		Message:     message,
		CreatedAt:   now,
		CompletedAt: &now,
	}
	s.cdnInvalidations[ref] = append(s.cdnInvalidations[ref], invalidation)
	return cloneCDNInvalidation(invalidation), nil
}

func (s *MemoryStore) ListProjectNetworkConnections(ctx context.Context, ref string) ([]ProjectNetworkConnection, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.networkConnections[ref], cloneNetworkConnections, func(left, right ProjectNetworkConnection) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectNetworkConnection(ctx context.Context, ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	connection, err := normalizeProjectNetworkConnection(ref, input)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.networkConnections[ref] {
		if existing.Name == connection.Name {
			return ProjectNetworkConnection{}, fmt.Errorf("%w: network connection %s for project %s already exists", ErrConflict, connection.Name, ref)
		}
	}
	s.networkConnections[ref] = append(s.networkConnections[ref], connection)
	return cloneNetworkConnection(connection), nil
}

func (s *MemoryStore) DeleteProjectNetworkConnection(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("network connection id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	connections := s.networkConnections[ref]
	for index, connection := range connections {
		if connection.ID == id {
			s.networkConnections[ref] = append(connections[:index], connections[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: network connection %s for project %s", ErrNotFound, id, ref)
}
