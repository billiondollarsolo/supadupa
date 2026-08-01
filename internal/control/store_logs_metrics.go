package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) ListProjectLogDrains(ctx context.Context, ref string) ([]LogDrain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.logDrains[ref], cloneLogDrains, func(left, right LogDrain) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectLogDrain(ctx context.Context, ref string, input LogDrainInput) (LogDrain, error) {
	target, err := normalizeLogDrainTarget(input.Target)
	if err != nil {
		return LogDrain{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return LogDrain{}, err
	}
	if err := validateLogDrainConfig(target, config); err != nil {
		return LogDrain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return LogDrain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drain := LogDrain{
		ID:         newID(),
		ProjectRef: ref,
		Target:     target,
		Config:     config,
		CreatedAt:  time.Now().UTC(),
	}
	s.logDrains[ref] = append(s.logDrains[ref], drain)
	return cloneLogDrain(drain), nil
}

func (s *MemoryStore) UpdateProjectLogDrain(ctx context.Context, ref string, id string, input LogDrainInput) (LogDrain, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return LogDrain{}, fmt.Errorf("log drain id is required")
	}
	target, err := normalizeLogDrainTarget(input.Target)
	if err != nil {
		return LogDrain{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return LogDrain{}, err
	}
	if err := validateLogDrainConfig(target, config); err != nil {
		return LogDrain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return LogDrain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drains := s.logDrains[ref]
	for index, drain := range drains {
		if drain.ID == id {
			drain.Target = target
			drain.Config = config
			drains[index] = drain
			s.logDrains[ref] = drains
			return cloneLogDrain(drain), nil
		}
	}
	return LogDrain{}, fmt.Errorf("%w: log drain %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) DeleteProjectLogDrain(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("log drain id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drains := s.logDrains[ref]
	for index, drain := range drains {
		if drain.ID == id {
			s.logDrains[ref] = append(drains[:index], drains[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: log drain %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) RecordProjectLog(ctx context.Context, input ProjectLogInput) (ProjectLog, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return ProjectLog{}, fmt.Errorf("project ref is required")
	}
	level := input.Level
	if level == "" {
		level = "info"
	}
	logEntry := ProjectLog{
		ID:         newID(),
		ProjectRef: input.ProjectRef,
		Level:      level,
		Message:    input.Message,
		Metadata:   input.Metadata,
		CreatedAt:  time.Now().UTC(),
	}
	if logEntry.Metadata == nil {
		logEntry.Metadata = map[string]string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return ProjectLog{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	s.projectLogs = append(s.projectLogs, logEntry)
	return logEntry, nil
}

func (s *MemoryStore) ListProjectLogs(ctx context.Context, ref string, limit int) ([]ProjectLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	matched := make([]ProjectLog, 0)
	for _, logEntry := range s.projectLogs {
		if logEntry.ProjectRef == ref {
			matched = append(matched, logEntry)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *MemoryStore) RecordAuditEvent(ctx context.Context, input AuditEventInput) (AuditEvent, error) {
	if strings.TrimSpace(input.Action) == "" {
		return AuditEvent{}, fmt.Errorf("audit action is required")
	}
	if strings.TrimSpace(input.Target) == "" {
		return AuditEvent{}, fmt.Errorf("audit target is required")
	}
	event := AuditEvent{
		ActorID:   strings.TrimSpace(input.ActorID),
		Action:    strings.TrimSpace(input.Action),
		Target:    strings.TrimSpace(input.Target),
		Metadata:  cloneStringMap(input.Metadata),
		CreatedAt: time.Now().UTC(),
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = newID()
	event.ChainIndex = len(s.auditEvents) + 1
	if len(s.auditEvents) > 0 {
		event.PreviousHash = s.auditEvents[len(s.auditEvents)-1].Hash
	}
	event.Hash = hashAuditEvent(event)
	s.auditEvents = append(s.auditEvents, event)
	return event, nil
}

func (s *MemoryStore) ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	start := len(s.auditEvents) - limit
	if start < 0 {
		start = 0
	}
	events := append([]AuditEvent(nil), s.auditEvents[start:]...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	return events, nil
}

// ListAuditEventsPage applies server-side filtering (action / actor / time
// window) and pagination over the full audit history, returning the matching
// slice plus the total match count. The full chain lives in memory, so this is
// an in-memory scan — no per-query SQL.
func (s *MemoryStore) ListAuditEventsPage(ctx context.Context, query AuditEventQuery) (AuditEventPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	action := strings.TrimSpace(query.Action)
	actor := strings.TrimSpace(query.ActorID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]AuditEvent, 0, len(s.auditEvents))
	for _, event := range s.auditEvents {
		if action != "" && event.Action != action {
			continue
		}
		if actor != "" && event.ActorID != actor {
			continue
		}
		if !query.Since.IsZero() && event.CreatedAt.Before(query.Since) {
			continue
		}
		if !query.Until.IsZero() && event.CreatedAt.After(query.Until) {
			continue
		}
		filtered = append(filtered, event)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return AuditEventPage{
		Events: append([]AuditEvent(nil), filtered[offset:end]...),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *MemoryStore) ListProjectAuditEvents(ctx context.Context, ref string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ref = strings.TrimSpace(ref)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	target := "project:" + ref
	matched := make([]AuditEvent, 0)
	for _, event := range s.auditEvents {
		if event.Target == target || event.Metadata["project_ref"] == ref || event.Metadata["ref"] == ref {
			matched = append(matched, event)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *MemoryStore) VerifyAuditLog(ctx context.Context) (AuditIntegrity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	integrity := AuditIntegrity{
		Verified:  true,
		Events:    len(s.auditEvents),
		CheckedAt: time.Now().UTC(),
	}
	previousHash := ""
	for index, event := range s.auditEvents {
		expectedIndex := index + 1
		if event.ChainIndex != expectedIndex || event.PreviousHash != previousHash || event.Hash != hashAuditEvent(event) {
			integrity.Verified = false
			integrity.BrokenAt = expectedIndex
			return integrity, nil
		}
		previousHash = event.Hash
	}
	integrity.HeadHash = previousHash
	return integrity, nil
}

func (s *MemoryStore) RecordProjectTelemetry(ctx context.Context, ref string, input TelemetrySampleInput) (TelemetrySample, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "unknown"
	}
	if input.CPUPercent < 0 {
		return TelemetrySample{}, fmt.Errorf("cpu percent cannot be negative")
	}
	if input.MemoryBytes < 0 || input.MemoryLimitBytes < 0 || input.DiskUsedBytes < 0 || input.DiskLimitBytes < 0 || input.NetworkRxBytes < 0 || input.NetworkTxBytes < 0 {
		return TelemetrySample{}, fmt.Errorf("telemetry counters cannot be negative")
	}
	now := time.Now().UTC()
	sampledAt := input.SampledAt.UTC()
	if sampledAt.IsZero() {
		sampledAt = now
	}
	if sampledAt.After(now.Add(telemetryMaxFutureSkew)) {
		return TelemetrySample{}, fmt.Errorf("project telemetry sampled_at cannot be more than %s in the future", telemetryMaxFutureSkew)
	}
	sample := TelemetrySample{
		ProjectRef:       ref,
		Source:           source,
		CPUPercent:       input.CPUPercent,
		MemoryBytes:      input.MemoryBytes,
		MemoryLimitBytes: input.MemoryLimitBytes,
		DiskUsedBytes:    input.DiskUsedBytes,
		DiskLimitBytes:   input.DiskLimitBytes,
		NetworkRxBytes:   input.NetworkRxBytes,
		NetworkTxBytes:   input.NetworkTxBytes,
		SampledAt:        sampledAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return TelemetrySample{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	s.telemetry[ref] = sample
	if s.telemetryHistory == nil {
		s.telemetryHistory = map[string][]TelemetryHistorySample{}
	}
	reservation := resourceReservationForSpec(project.Spec)
	s.telemetryHistory[ref] = append(s.telemetryHistory[ref], telemetryHistorySampleFromTelemetry(sample, reservation))
	s.telemetryHistory[ref] = compactTelemetryHistory(s.telemetryHistory[ref], now)
	return sample, nil
}

func (s *MemoryStore) RecordNodeTelemetry(ctx context.Context, hostID string, input NodeTelemetrySampleInput) (NodeTelemetrySample, error) {
	hostID = strings.TrimSpace(hostID)
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "unknown"
	}
	if input.CPUPercent < 0 || input.CPUUsedCores < 0 {
		return NodeTelemetrySample{}, fmt.Errorf("node cpu usage cannot be negative")
	}
	if input.CPUCapacityCores < 0 || input.MemoryUsedBytes < 0 || input.MemoryTotalBytes < 0 || input.DiskUsedBytes < 0 || input.DiskTotalBytes < 0 || input.DiskAvailableBytes < 0 || input.NetworkRxBytes < 0 || input.NetworkTxBytes < 0 {
		return NodeTelemetrySample{}, fmt.Errorf("node telemetry counters cannot be negative")
	}
	sampledAt := input.SampledAt.UTC()
	if sampledAt.IsZero() {
		sampledAt = time.Now().UTC()
	}
	sample := NodeTelemetrySample{
		HostID:             hostID,
		Source:             source,
		CPUPercent:         input.CPUPercent,
		CPUUsedCores:       input.CPUUsedCores,
		CPUCapacityCores:   input.CPUCapacityCores,
		MemoryUsedBytes:    input.MemoryUsedBytes,
		MemoryTotalBytes:   input.MemoryTotalBytes,
		DiskUsedBytes:      input.DiskUsedBytes,
		DiskTotalBytes:     input.DiskTotalBytes,
		DiskAvailableBytes: input.DiskAvailableBytes,
		NetworkSampled:     input.NetworkSampled,
		NetworkRxBytes:     input.NetworkRxBytes,
		NetworkTxBytes:     input.NetworkTxBytes,
		SampledAt:          sampledAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hosts[hostID]; !ok {
		return NodeTelemetrySample{}, fmt.Errorf("%w: host %s", ErrNotFound, hostID)
	}
	if s.nodeTelemetry == nil {
		s.nodeTelemetry = map[string]NodeTelemetrySample{}
	}
	s.nodeTelemetry[hostID] = sample
	return sample, nil
}

func (s *MemoryStore) GetFleetMetrics(ctx context.Context) (FleetMetrics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metrics := FleetMetrics{
		Orgs:             len(s.orgs),
		Users:            len(s.users),
		Hosts:            len(s.hosts),
		Projects:         len(s.projects),
		ProjectsByStatus: map[string]int{},
		NodeObserved:     []NodeTelemetrySample{},
		ProjectLogEvents: len(s.projectLogs),
		AuditEvents:      len(s.auditEvents),
		AuditVerified:    true,
		SampledAt:        time.Now().UTC(),
	}
	for _, project := range s.projects {
		metrics.ProjectsByStatus[string(project.Status)]++
	}
	for _, replicas := range s.replicas {
		metrics.ReadReplicas += len(replicas)
	}
	for _, host := range s.hosts {
		metrics.HostCapacity = addHostCapacity(metrics.HostCapacity, host.Capacity)
		metrics.HostUsed = addHostCapacity(metrics.HostUsed, host.Used)
		if sample, ok := s.nodeTelemetry[host.ID]; ok {
			metrics.NodeObserved = append(metrics.NodeObserved, sample)
		}
	}
	sort.Slice(metrics.NodeObserved, func(i, j int) bool {
		return metrics.NodeObserved[i].SampledAt.After(metrics.NodeObserved[j].SampledAt)
	})
	metrics.Observed = telemetryRollup(s.projects, s.telemetry, time.Now().UTC())
	s.addRegisteredProjectChildFleetMetricsLocked(&metrics)
	for ref := range s.projects {
		metrics.DatabaseExtensions += countEnabledDatabaseExtensions(ref, s.databaseExtensions[ref])
	}
	for ref, policy := range s.cdnPolicies {
		if _, ok := s.projects[ref]; ok && policy.Enabled {
			metrics.CDNEnabledProjects++
		}
	}
	for _, backup := range s.backups {
		metrics.Backups++
		metrics.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		metrics.WALArchives++
		metrics.WALArchiveBytes += archive.SizeBytes
	}
	previousHash := ""
	for index, event := range s.auditEvents {
		expectedIndex := index + 1
		if event.ChainIndex != expectedIndex || event.PreviousHash != previousHash || event.Hash != hashAuditEvent(event) {
			metrics.AuditVerified = false
			break
		}
		previousHash = event.Hash
	}
	return metrics, nil
}

func (s *MemoryStore) GetProjectMetrics(ctx context.Context, ref string) (ProjectMetrics, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectMetrics{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	metrics := ProjectMetrics{
		ProjectRef:   project.Ref,
		OrgID:        project.OrgID,
		Status:       project.Status,
		ResourceTier: project.Spec.ResourceTier,
		Resources:    resourceReservationForSpec(project.Spec),
		SampledAt:    time.Now().UTC(),
	}
	if sample, ok := s.telemetry[ref]; ok {
		metrics.Observed = &sample
	}
	s.addRegisteredProjectChildMetricsLocked(ref, &metrics)
	metrics.DatabaseExtensions = countEnabledDatabaseExtensions(ref, s.databaseExtensions[ref])
	if policy, ok := s.cdnPolicies[ref]; ok {
		metrics.CDNEnabled = policy.Enabled
	}
	for _, replica := range s.replicas[ref] {
		metrics.ReadReplicas++
		metrics.Resources = addHostCapacity(metrics.Resources, replicaReservationForTier(replica.Tier))
	}
	for _, backup := range s.backups {
		if backup.ProjectRef != ref {
			continue
		}
		metrics.Backups++
		metrics.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		if archive.ProjectRef != ref {
			continue
		}
		metrics.WALArchives++
		metrics.WALArchiveBytes += archive.SizeBytes
	}
	for _, logEntry := range s.projectLogs {
		if logEntry.ProjectRef == ref {
			metrics.ProjectLogEvents++
		}
	}
	target := "project:" + ref
	for _, event := range s.auditEvents {
		if event.Target == target || event.Metadata["project_ref"] == ref || event.Metadata["ref"] == ref {
			metrics.ActivityEvents++
		}
	}
	metrics.DBAllocatedBytes = int64(resourceReservationForSpec(project.Spec).DiskGB) * 1024 * 1024 * 1024
	metrics.StorageBytes = metrics.BackupStorageBytes + metrics.WALArchiveBytes
	return metrics, nil
}

func (s *MemoryStore) GetProjectTelemetryHistory(ctx context.Context, ref string, query TelemetryHistoryQuery) (ProjectTelemetryHistory, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	now := time.Now().UTC()
	normalized := normalizeTelemetryHistoryQuery(query, now)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectTelemetryHistory{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}

	samples := append([]TelemetryHistorySample(nil), s.telemetryHistory[ref]...)
	if len(samples) == 0 {
		if latest, ok := s.telemetry[ref]; ok {
			sampledAt := latest.SampledAt.UTC()
			if !sampledAt.Before(now.Add(-telemetryHistoryRetention)) && !sampledAt.After(now.Add(telemetryMaxFutureSkew)) {
				reservation := resourceReservationForSpec(s.projects[ref].Spec)
				samples = append(samples, telemetryHistorySampleFromTelemetry(latest, reservation))
			}
		}
	}
	points := telemetryHistoryPoints(samples, normalized)
	latestSampledAt := time.Time{}
	for _, sample := range samples {
		if latestSampledAt.IsZero() || sample.SampledAt.After(latestSampledAt) {
			latestSampledAt = sample.SampledAt
		}
	}
	return ProjectTelemetryHistory{
		ProjectRef:          ref,
		From:                normalized.From,
		To:                  normalized.To,
		StepSeconds:         int(normalized.Step.Seconds()),
		RetentionSeconds:    int(telemetryHistoryRetention.Seconds()),
		RawRetentionSeconds: int(telemetryHistoryRawRetention.Seconds()),
		LatestSampledAt:     latestSampledAt,
		Points:              points,
	}, nil
}
