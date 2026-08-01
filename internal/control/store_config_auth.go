package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) GetProjectConfig(ctx context.Context, ref string, area string) (ProjectConfig, error) {
	normalizedArea, err := normalizeConfigArea(area)
	if err != nil {
		return ProjectConfig{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectConfig{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if config, ok := s.configs[ref][normalizedArea]; ok {
		return mergeProjectConfigWithDefaults(ref, normalizedArea, config), nil
	}
	return defaultProjectConfig(ref, normalizedArea), nil
}

func (s *MemoryStore) ListProjectConfigs(ctx context.Context, ref string) ([]ProjectConfig, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	configs := make([]ProjectConfig, 0, len(s.configs[ref]))
	for _, config := range s.configs[ref] {
		configs = append(configs, mergeProjectConfigWithDefaults(ref, config.Area, config))
	}
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Area < configs[j].Area
	})
	if configs == nil {
		configs = []ProjectConfig{}
	}
	return configs, nil
}

func (s *MemoryStore) UpdateProjectConfig(ctx context.Context, ref string, area string, input ProjectConfigInput) (ProjectConfig, error) {
	normalizedArea, err := normalizeConfigArea(area)
	if err != nil {
		return ProjectConfig{}, err
	}
	configMap, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectConfig{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if s.configs[ref] == nil {
		s.configs[ref] = map[string]ProjectConfig{}
	}
	base := defaultProjectConfig(ref, normalizedArea).Config
	if existing, ok := s.configs[ref][normalizedArea]; ok {
		for key, value := range existing.Config {
			base[key] = value
		}
	}
	for key, value := range configMap {
		base[key] = value
	}
	if normalizedArea == "general" {
		if err := validateGeneralConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "network" {
		if err := validateNetworkConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "auth" {
		if err := validateAuthConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "auth_providers" {
		if err := validateAuthProvidersConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "ai" {
		if err := validateAIConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "pooler" {
		if err := validatePoolerConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "functions" {
		if err := validateFunctionsConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "smtp" {
		if err := validateSMTPConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	config := ProjectConfig{
		ProjectRef: ref,
		Area:       normalizedArea,
		Config:     base,
		UpdatedAt:  time.Now().UTC(),
	}
	s.configs[ref][normalizedArea] = cloneProjectConfig(config)
	return config, nil
}

func (s *MemoryStore) ListProjectAuthClients(ctx context.Context, ref string) ([]ProjectAuthClient, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.authClients[ref], cloneAuthClients, func(left, right ProjectAuthClient) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectAuthClient(ctx context.Context, ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	client, err := normalizeProjectAuthClient(ref, input)
	if err != nil {
		return ProjectAuthClient{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAuthClient{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.authClients[ref] {
		if existing.ClientID == client.ClientID {
			return ProjectAuthClient{}, fmt.Errorf("%w: auth client %s for project %s already exists", ErrConflict, client.ClientID, ref)
		}
	}
	s.authClients[ref] = append(s.authClients[ref], client)
	return cloneAuthClient(client), nil
}

func (s *MemoryStore) DeleteProjectAuthClient(ctx context.Context, ref string, clientID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("client_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	clients := s.authClients[ref]
	for index, client := range clients {
		if client.ClientID == clientID {
			s.authClients[ref] = append(clients[:index], clients[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: auth client %s for project %s", ErrNotFound, clientID, ref)
}

func (s *MemoryStore) ListProjectAuthHooks(ctx context.Context, ref string) ([]ProjectAuthHook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.authHooks[ref], cloneAuthHooks, func(left, right ProjectAuthHook) bool {
		return left.HookType < right.HookType
	}), nil
}

func (s *MemoryStore) CreateProjectAuthHook(ctx context.Context, ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	hook, err := normalizeProjectAuthHook(ref, input)
	if err != nil {
		return ProjectAuthHook{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAuthHook{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for index, existing := range s.authHooks[ref] {
		if existing.HookType == hook.HookType {
			hook.ID = existing.ID
			hook.CreatedAt = existing.CreatedAt
			s.authHooks[ref][index] = hook
			return cloneAuthHook(hook), nil
		}
	}
	s.authHooks[ref] = append(s.authHooks[ref], hook)
	return cloneAuthHook(hook), nil
}

func (s *MemoryStore) DeleteProjectAuthHook(ctx context.Context, ref string, hookType string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	hookType = strings.ToLower(strings.TrimSpace(hookType))
	if hookType == "" {
		return fmt.Errorf("hook_type is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	hooks := s.authHooks[ref]
	for index, hook := range hooks {
		if hook.HookType == hookType {
			s.authHooks[ref] = append(hooks[:index], hooks[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: auth hook %s for project %s", ErrNotFound, hookType, ref)
}
