package control

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
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
	byCode    map[string]float64
	durSum    float64
	durCount  float64
	buckets   map[string]float64 // le label -> cumulative count (for percentiles)
	reqBytes  float64
	respBytes float64
}

type trafficSnapshot struct {
	at          time.Time
	routers     map[string]*routerStat
	epConns     map[string]float64
	epReqs      map[string]float64
	certNotAfter float64 // soonest cert expiry (unix seconds), 0 if none
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
		case "traefik_router_request_duration_seconds_bucket":
			if r := snap.router(labels["router"]); r != nil {
				r.buckets[labels["le"]] += value
			}
		case "traefik_router_requests_bytes_total":
			if r := snap.router(labels["router"]); r != nil {
				r.reqBytes += value
			}
		case "traefik_router_responses_bytes_total":
			if r := snap.router(labels["router"]); r != nil {
				r.respBytes += value
			}
		case "traefik_open_connections":
			snap.epConns[labels["entrypoint"]] += value
		case "traefik_entrypoint_requests_total":
			snap.epReqs[labels["entrypoint"]] += value
		case "traefik_tls_certs_not_after":
			if value > 0 && (snap.certNotAfter == 0 || value < snap.certNotAfter) {
				snap.certNotAfter = value
			}
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
		r = &routerStat{byCode: map[string]float64{}, buckets: map[string]float64{}}
		s.routers[name] = r
	}
	return r
}

// RouterTraffic is the derived, window-rate view of one Traefik router.
type RouterTraffic struct {
	Router          string  `json:"router"`
	RequestsTotal   float64 `json:"requests_total"`
	RequestsPerSec  float64 `json:"requests_per_sec"`
	ErrorRate       float64 `json:"error_rate"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	P50Ms           float64 `json:"p50_ms"`
	P95Ms           float64 `json:"p95_ms"`
	P99Ms           float64 `json:"p99_ms"`
	BytesInPerSec   float64 `json:"bytes_in_per_sec"`
	BytesOutPerSec  float64 `json:"bytes_out_per_sec"`
	Status2xx       float64 `json:"status_2xx"`
	Status3xx       float64 `json:"status_3xx"`
	Status4xx       float64 `json:"status_4xx"`
	Status5xx       float64 `json:"status_5xx"`
}

// TrafficReport is the collector's current computed state.
type TrafficReport struct {
	Enabled         bool               `json:"enabled"`
	LastScrape      *time.Time         `json:"last_scrape,omitempty"`
	WindowSeconds   float64            `json:"window_seconds"`
	Routers         []RouterTraffic    `json:"routers"`
	EntrypointConns map[string]float64 `json:"entrypoint_connections"`
	CertExpiresAt   *time.Time         `json:"cert_expires_at,omitempty"`
}

// percentileFromBuckets estimates a latency quantile (ms) from the per-window
// delta of cumulative Prometheus histogram buckets (le -> count).
func percentileFromBuckets(cur, prev map[string]float64, p float64) float64 {
	type b struct {
		le    float64
		count float64
	}
	bs := make([]b, 0, len(cur))
	for le, c := range cur {
		f := math.Inf(1)
		if le != "+Inf" {
			if v, err := strconv.ParseFloat(le, 64); err == nil {
				f = v
			}
		}
		bs = append(bs, b{le: f, count: c - prev[le]})
	}
	if len(bs) == 0 {
		return 0
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].le < bs[j].le })
	total := bs[len(bs)-1].count // cumulative +Inf bucket = all observations
	if total <= 0 {
		return 0
	}
	target := p * total
	for _, e := range bs {
		if e.count >= target {
			if math.IsInf(e.le, 1) {
				// Fell into the overflow bucket; use the largest finite boundary.
				for i := len(bs) - 1; i >= 0; i-- {
					if !math.IsInf(bs[i].le, 1) {
						return bs[i].le * 1000
					}
				}
				return 0
			}
			return e.le * 1000
		}
	}
	return 0
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
	if cur.certNotAfter > 0 {
		exp := time.Unix(int64(cur.certNotAfter), 0).UTC()
		rep.CertExpiresAt = &exp
	}
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
		rt.Status2xx, rt.Status3xx, rt.Status4xx, rt.Status5xx = classifyCodes(r.byCode)
		if prev != nil && dt > 0 {
			p := prev.routers[name]
			var pTotal, pErr, pSum, pCount, pReqBytes, pRespBytes float64
			var pBuckets map[string]float64
			if p != nil {
				pTotal, pErr = sumCodes(p.byCode)
				pSum, pCount = p.durSum, p.durCount
				pReqBytes, pRespBytes = p.reqBytes, p.respBytes
				pBuckets = p.buckets
			}
			cTotal, cErr := sumCodes(r.byCode)
			dReq := cTotal - pTotal
			if dReq > 0 {
				rt.RequestsPerSec = dReq / dt
				rt.ErrorRate = (cErr - pErr) / dReq
				if dCount := r.durCount - pCount; dCount > 0 {
					rt.AvgLatencyMs = (r.durSum - pSum) / dCount * 1000
				}
				rt.P50Ms = percentileFromBuckets(r.buckets, pBuckets, 0.50)
				rt.P95Ms = percentileFromBuckets(r.buckets, pBuckets, 0.95)
				rt.P99Ms = percentileFromBuckets(r.buckets, pBuckets, 0.99)
			}
			rt.BytesInPerSec = (r.reqBytes - pReqBytes) / dt
			rt.BytesOutPerSec = (r.respBytes - pRespBytes) / dt
		}
		rep.Routers = append(rep.Routers, rt)
	}
	return rep
}

func classifyCodes(byCode map[string]float64) (s2, s3, s4, s5 float64) {
	for code, v := range byCode {
		if code == "" {
			continue
		}
		switch code[0] {
		case '2':
			s2 += v
		case '3':
			s3 += v
		case '4':
			s4 += v
		case '5':
			s5 += v
		}
	}
	return
}
