package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) CreateHost(ctx context.Context, req CreateHostRequest) (Host, error) {
	name := strings.TrimSpace(req.Name)
	address := strings.TrimSpace(req.Address)
	if name == "" {
		return Host{}, fmt.Errorf("host name is required")
	}
	if address == "" {
		return Host{}, fmt.Errorf("host address is required")
	}
	if req.Capacity.CPU < 0 || req.Capacity.RAMMB < 0 || req.Capacity.DiskGB < 0 || req.Capacity.Project < 0 {
		return Host{}, fmt.Errorf("host capacity cannot be negative")
	}
	host := Host{
		ID:        newID(),
		Name:      name,
		Address:   address,
		Capacity:  req.Capacity,
		Used:      HostCapacity{},
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.ID] = host
	return host, nil
}

func (s *MemoryStore) ListHosts(ctx context.Context) ([]Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := make([]Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})
	return hosts, nil
}

func (s *MemoryStore) GetHost(ctx context.Context, id string) (Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[id]
	if !ok {
		return Host{}, fmt.Errorf("%w: host %s", ErrNotFound, id)
	}
	return host, nil
}

func (s *MemoryStore) DeleteHost(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	host, ok := s.hosts[id]
	if !ok {
		return fmt.Errorf("%w: host %s", ErrNotFound, id)
	}
	if host.Used.CPU > 0 || host.Used.RAMMB > 0 || host.Used.DiskGB > 0 || host.Used.Project > 0 {
		return fmt.Errorf("%w: host %s still has reserved capacity", ErrConflict, id)
	}
	delete(s.hosts, id)
	return nil
}
