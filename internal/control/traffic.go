package control

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TrafficCollector scrapes Traefik's Prometheus endpoint and keeps the two most
// recent snapshots in memory, so per-router request rates, error rates, and
// latency can be derived over the scrape window without any external store.
type TrafficCollector struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	prev   *trafficSnapshot
	cur    *trafficSnapshot
}

type routerStat struct {
	byCode   map[string]float64
	durSum   float64
	durCount float64
}

type trafficSnapshot struct {
	at      time.Time
	routers map[string]*routerStat
	epConns map[string]float64
	epReqs  map[string]float64
}

func NewTrafficCollector(url string) *TrafficCollector {
	return &TrafficCollector{url: strings.TrimSpace(url), client: &http.Client{Timeout: 10 * time.Second}}
}

func (c *TrafficCollector) Enabled() bool { return c != nil && c.url != "" }

// Scrape fetches and parses the metrics endpoint, rotating the snapshot pair.
func (c *TrafficCollector) Scrape(ctx context.Context, now time.Time) error {
	if !c.Enabled() {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("traefik metrics returned %d", resp.StatusCode)
	}
	snap := &trafficSnapshot{at: now, routers: map[string]*routerStat{}, epConns: map[string]float64{}, epReqs: map[string]float64{}}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		name, labels, value, ok := parsePromLine(line)
		if !ok {
			continue
		}
		switch name {
		case "traefik_router_requests_total":
			r := snap.router(labels["router"])
			if r != nil {
				r.byCode[labels["code"]] += value
			}
		case "traefik_router_request_duration_seconds_sum":
			if r := snap.router(labels["router"]); r != nil {
				r.durSum += value
			}
		case "traefik_router_request_duration_seconds_count":
			if r := snap.router(labels["router"]); r != nil {
				r.durCount += value
			}
		case "traefik_open_connections":
			snap.epConns[labels["entrypoint"]] += value
		case "traefik_entrypoint_requests_total":
			snap.epReqs[labels["entrypoint"]] += value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.prev = c.cur
	c.cur = snap
	c.mu.Unlock()
	return nil
}

// parsePromLine parses a single Prometheus exposition line:
// `name{k="v",k2="v2"} 123.4` (or `name 123.4`). Sufficient for Traefik's
// label set (no commas/spaces inside label values).
func parsePromLine(line string) (name string, labels map[string]string, value float64, ok bool) {
	sp := strings.LastIndexByte(line, ' ')
	if sp < 0 {
		return "", nil, 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(line[sp+1:]), 64)
	if err != nil {
		return "", nil, 0, false
	}
	head := strings.TrimSpace(line[:sp])
	labels = map[string]string{}
	if brace := strings.IndexByte(head, '{'); brace >= 0 {
		name = head[:brace]
		end := strings.LastIndexByte(head, '}')
		if end <= brace {
			return "", nil, 0, false
		}
		for _, pair := range strings.Split(head[brace+1:end], ",") {
			eq := strings.IndexByte(pair, '=')
			if eq < 0 {
				continue
			}
			labels[strings.TrimSpace(pair[:eq])] = strings.Trim(strings.TrimSpace(pair[eq+1:]), `"`)
		}
	} else {
		name = head
	}
	return name, labels, v, true
}

func (s *trafficSnapshot) router(label string) *routerStat {
	name := strings.TrimSuffix(label, "@file")
	if name == "" {
		return nil
	}
	r := s.routers[name]
	if r == nil {
		r = &routerStat{byCode: map[string]float64{}}
		s.routers[name] = r
	}
	return r
}

// RouterTraffic is the derived, window-rate view of one Traefik router.
type RouterTraffic struct {
	Router         string  `json:"router"`
	RequestsTotal  float64 `json:"requests_total"`
	RequestsPerSec float64 `json:"requests_per_sec"`
	ErrorRate      float64 `json:"error_rate"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
}

// TrafficReport is the collector's current computed state.
type TrafficReport struct {
	Enabled            bool                  `json:"enabled"`
	LastScrape         *time.Time            `json:"last_scrape,omitempty"`
	WindowSeconds      float64               `json:"window_seconds"`
	Routers            []RouterTraffic       `json:"routers"`
	EntrypointConns    map[string]float64    `json:"entrypoint_connections"`
}

func sumCodes(byCode map[string]float64) (total, errors float64) {
	for code, v := range byCode {
		total += v
		if len(code) > 0 && (code[0] == '4' || code[0] == '5') {
			errors += v
		}
	}
	return total, errors
}

// Report computes per-router rates from the prev→cur window. Totals are
// cumulative (since Traefik start); rates/latency are over the window.
func (c *TrafficCollector) Report() TrafficReport {
	rep := TrafficReport{Enabled: c.Enabled(), EntrypointConns: map[string]float64{}, Routers: []RouterTraffic{}}
	if !c.Enabled() {
		return rep
	}
	c.mu.Lock()
	cur, prev := c.cur, c.prev
	c.mu.Unlock()
	if cur == nil {
		return rep
	}
	at := cur.at
	rep.LastScrape = &at
	for ep, v := range cur.epConns {
		rep.EntrypointConns[ep] = v
	}
	var dt float64
	if prev != nil {
		dt = cur.at.Sub(prev.at).Seconds()
	}
	rep.WindowSeconds = dt
	for name, r := range cur.routers {
		total, _ := sumCodes(r.byCode)
		rt := RouterTraffic{Router: name, RequestsTotal: total}
		if prev != nil && dt > 0 {
			p := prev.routers[name]
			var pTotal, pErr, pSum, pCount float64
			if p != nil {
				pTotal, pErr = sumCodes(p.byCode)
				pSum, pCount = p.durSum, p.durCount
			}
			cTotal, cErr := sumCodes(r.byCode)
			dReq := cTotal - pTotal
			if dReq > 0 {
				rt.RequestsPerSec = dReq / dt
				rt.ErrorRate = (cErr - pErr) / dReq
				if dCount := r.durCount - pCount; dCount > 0 {
					rt.AvgLatencyMs = (r.durSum - pSum) / dCount * 1000
				}
			}
		}
		rep.Routers = append(rep.Routers, rt)
	}
	return rep
}
