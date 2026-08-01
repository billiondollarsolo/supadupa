package control

import (
	"context"
	"fmt"
	"strings"
)

func (s *MemoryStore) ListProjectDatabaseExtensions(ctx context.Context, ref string) ([]ProjectDatabaseExtension, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	extensions := mergedDatabaseExtensions(ref, s.databaseExtensions[ref])
	return cloneDatabaseExtensions(extensions), nil
}

func (s *MemoryStore) UpdateProjectDatabaseExtension(ctx context.Context, ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	extension, err := normalizeProjectDatabaseExtension(ref, name, input)
	if err != nil {
		return ProjectDatabaseExtension{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseExtension{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	extensions := s.databaseExtensions[ref]
	for index, existing := range extensions {
		if existing.Name == extension.Name {
			extension.ID = existing.ID
			extension.CreatedAt = existing.CreatedAt
			extensions[index] = extension
			s.databaseExtensions[ref] = extensions
			return cloneDatabaseExtension(extension), nil
		}
	}
	s.databaseExtensions[ref] = append(s.databaseExtensions[ref], extension)
	return cloneDatabaseExtension(extension), nil
}

func (s *MemoryStore) ListProjectDatabaseCronJobs(ctx context.Context, ref string) ([]ProjectDatabaseCronJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseCronJobs[ref], cloneDatabaseCronJobs, func(left, right ProjectDatabaseCronJob) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseCronJob(ctx context.Context, ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	job, err := normalizeProjectDatabaseCronJob(ref, input)
	if err != nil {
		return ProjectDatabaseCronJob{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseCronJob{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseCronJobs[ref] {
		if existing.Name == job.Name {
			return ProjectDatabaseCronJob{}, fmt.Errorf("%w: database cron job %s for project %s already exists", ErrConflict, job.Name, ref)
		}
	}
	s.databaseCronJobs[ref] = append(s.databaseCronJobs[ref], job)
	return cloneDatabaseCronJob(job), nil
}

func (s *MemoryStore) DeleteProjectDatabaseCronJob(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database cron job name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	jobs := s.databaseCronJobs[ref]
	for index, job := range jobs {
		if job.Name == name {
			s.databaseCronJobs[ref] = append(jobs[:index], jobs[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database cron job %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseQueues(ctx context.Context, ref string) ([]ProjectDatabaseQueue, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseQueues[ref], cloneDatabaseQueues, func(left, right ProjectDatabaseQueue) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseQueue(ctx context.Context, ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	queue, err := normalizeProjectDatabaseQueue(ref, input)
	if err != nil {
		return ProjectDatabaseQueue{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseQueue{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseQueues[ref] {
		if existing.Name == queue.Name {
			return ProjectDatabaseQueue{}, fmt.Errorf("%w: database queue %s for project %s already exists", ErrConflict, queue.Name, ref)
		}
	}
	s.databaseQueues[ref] = append(s.databaseQueues[ref], queue)
	return cloneDatabaseQueue(queue), nil
}

func (s *MemoryStore) DeleteProjectDatabaseQueue(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database queue name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	queues := s.databaseQueues[ref]
	for index, queue := range queues {
		if queue.Name == name {
			s.databaseQueues[ref] = append(queues[:index], queues[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database queue %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseWebhooks(ctx context.Context, ref string) ([]ProjectDatabaseWebhook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseWebhooks[ref], cloneDatabaseWebhooks, func(left, right ProjectDatabaseWebhook) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseWebhook(ctx context.Context, ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	webhook, err := normalizeProjectDatabaseWebhook(ref, input)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseWebhook{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseWebhooks[ref] {
		if existing.Name == webhook.Name {
			return ProjectDatabaseWebhook{}, fmt.Errorf("%w: database webhook %s for project %s already exists", ErrConflict, webhook.Name, ref)
		}
	}
	s.databaseWebhooks[ref] = append(s.databaseWebhooks[ref], webhook)
	return cloneDatabaseWebhook(webhook), nil
}

func (s *MemoryStore) DeleteProjectDatabaseWebhook(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database webhook name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	webhooks := s.databaseWebhooks[ref]
	for index, webhook := range webhooks {
		if webhook.Name == name {
			s.databaseWebhooks[ref] = append(webhooks[:index], webhooks[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database webhook %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseSchemas(ctx context.Context, ref string) ([]ProjectDatabaseSchema, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseSchemas[ref], cloneDatabaseSchemas, func(left, right ProjectDatabaseSchema) bool {
		if left.ApplyOrder == right.ApplyOrder {
			if left.Name == right.Name {
				return left.Version < right.Version
			}
			return left.Name < right.Name
		}
		return left.ApplyOrder < right.ApplyOrder
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseSchema(ctx context.Context, ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	schema, err := normalizeProjectDatabaseSchema(ref, input)
	if err != nil {
		return ProjectDatabaseSchema{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseSchema{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseSchemas[ref] {
		if existing.Name == schema.Name && existing.Version == schema.Version {
			return ProjectDatabaseSchema{}, fmt.Errorf("%w: database schema %s@%s for project %s already exists", ErrConflict, schema.Name, schema.Version, ref)
		}
	}
	s.databaseSchemas[ref] = append(s.databaseSchemas[ref], schema)
	return cloneDatabaseSchema(schema), nil
}

func (s *MemoryStore) DeleteProjectDatabaseSchema(ctx context.Context, ref string, name string, version string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	version = strings.TrimSpace(version)
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database schema name must be 3-64 lowercase letters, numbers, or dashes")
	}
	if version == "" {
		return fmt.Errorf("database schema version is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	schemas := s.databaseSchemas[ref]
	for index, schema := range schemas {
		if schema.Name == name && schema.Version == version {
			s.databaseSchemas[ref] = append(schemas[:index], schemas[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database schema %s@%s for project %s", ErrNotFound, name, version, ref)
}

func (s *MemoryStore) ListProjectDatabaseRoles(ctx context.Context, ref string) ([]ProjectDatabaseRole, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseRoles[ref], cloneDatabaseRoles, func(left, right ProjectDatabaseRole) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseRole(ctx context.Context, ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	role, err := normalizeProjectDatabaseRole(ref, input)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseRole{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseRoles[ref] {
		if existing.Name == role.Name {
			return ProjectDatabaseRole{}, fmt.Errorf("%w: database role %s for project %s already exists", ErrConflict, role.Name, ref)
		}
	}
	s.databaseRoles[ref] = append(s.databaseRoles[ref], role)
	return cloneDatabaseRole(role), nil
}

func (s *MemoryStore) DeleteProjectDatabaseRole(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validateDatabaseIdentifier("database role name", name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	roles := s.databaseRoles[ref]
	for index, role := range roles {
		if role.Name == name {
			s.databaseRoles[ref] = append(roles[:index], roles[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database role %s for project %s", ErrNotFound, name, ref)
}
