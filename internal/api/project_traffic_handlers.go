package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

// routeTraffic is one project route's traffic, with the route name (the part of
// the Traefik router after the project ref) instead of the raw router name.
type routeTraffic struct {
	Route string `json:"route"`
	control.RouterTraffic
}

type trafficTotals struct {
	RequestsTotal  float64 `json:"requests_total"`
	RequestsPerSec float64 `json:"requests_per_sec"`
	ErrorRate      float64 `json:"error_rate"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	P95Ms          float64 `json:"p95_ms"`
	BytesInPerSec  float64 `json:"bytes_in_per_sec"`
	BytesOutPerSec float64 `json:"bytes_out_per_sec"`
	Status2xx      float64 `json:"status_2xx"`
	Status3xx      float64 `json:"status_3xx"`
	Status4xx      float64 `json:"status_4xx"`
	Status5xx      float64 `json:"status_5xx"`
}

type projectTrafficResponse struct {
	Enabled       bool           `json:"enabled"`
	LastScrape    *time.Time     `json:"last_scrape,omitempty"`
	WindowSeconds float64        `json:"window_seconds"`
	Routes        []routeTraffic `json:"routes"`
	Totals        trafficTotals  `json:"totals"`
	CertExpiresAt *time.Time     `json:"cert_expires_at,omitempty"`
}

type projectTrafficSummary struct {
	Ref string `json:"ref"`
	trafficTotals
}

type fleetTrafficResponse struct {
	Enabled         bool                    `json:"enabled"`
	LastScrape      *time.Time              `json:"last_scrape,omitempty"`
	WindowSeconds   float64                 `json:"window_seconds"`
	Projects        []projectTrafficSummary `json:"projects"`
	EntrypointConns map[string]float64      `json:"entrypoint_connections"`
	Totals          trafficTotals           `json:"totals"`
	CertExpiresAt   *time.Time              `json:"cert_expires_at,omitempty"`
}

// splitRouterRef maps a Traefik router name ("<ref>-<route>" or "<ref>") to its
// owning project ref (longest match wins, since refs may contain hyphens) and
// the trailing route name.
func splitRouterRef(router string, refsByLen []string) (ref, route string, ok bool) {
	for _, r := range refsByLen {
		if router == r {
			return r, "root", true
		}
		if strings.HasPrefix(router, r+"-") {
			return r, router[len(r)+1:], true
		}
	}
	return "", "", false
}

func refsByLengthDesc(projects []control.Project) []string {
	refs := make([]string, 0, len(projects))
	for _, p := range projects {
		refs = append(refs, p.Ref)
	}
	sort.Slice(refs, func(i, j int) bool { return len(refs[i]) > len(refs[j]) })
	return refs
}

func accumulate(t *trafficTotals, rt control.RouterTraffic) {
	// requests_per_sec / error_rate / latency are window rates; aggregate by
	// weighting error-rate and latency with each route's request rate.
	prevRate := t.RequestsPerSec
	t.RequestsTotal += rt.RequestsTotal
	t.RequestsPerSec += rt.RequestsPerSec
	t.BytesInPerSec += rt.BytesInPerSec
	t.BytesOutPerSec += rt.BytesOutPerSec
	t.Status2xx += rt.Status2xx
	t.Status3xx += rt.Status3xx
	t.Status4xx += rt.Status4xx
	t.Status5xx += rt.Status5xx
	if t.RequestsPerSec > 0 {
		t.ErrorRate = (t.ErrorRate*prevRate + rt.ErrorRate*rt.RequestsPerSec) / t.RequestsPerSec
		t.AvgLatencyMs = (t.AvgLatencyMs*prevRate + rt.AvgLatencyMs*rt.RequestsPerSec) / t.RequestsPerSec
		// p95 isn't additive; surface the worst route's p95 as the project's.
		if rt.P95Ms > t.P95Ms {
			t.P95Ms = rt.P95Ms
		}
	}
}

func getProjectTrafficHandler(store control.Store, traffic *control.TrafficCollector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		resp := projectTrafficResponse{Enabled: traffic.Enabled(), Routes: []routeTraffic{}}
		if traffic.Enabled() {
			rep := traffic.Report()
			resp.LastScrape = rep.LastScrape
			resp.WindowSeconds = rep.WindowSeconds
			resp.CertExpiresAt = rep.CertExpiresAt
			refs := []string{project.Ref}
			for _, rt := range rep.Routers {
				if ref, route, ok := splitRouterRef(rt.Router, refs); ok && ref == project.Ref {
					resp.Routes = append(resp.Routes, routeTraffic{Route: route, RouterTraffic: rt})
					accumulate(&resp.Totals, rt)
				}
			}
			sort.Slice(resp.Routes, func(i, j int) bool { return resp.Routes[i].Route < resp.Routes[j].Route })
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func getFleetTrafficHandler(store control.Store, traffic *control.TrafficCollector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := projectsVisibleToRequest(r, store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resp := fleetTrafficResponse{Enabled: traffic.Enabled(), Projects: []projectTrafficSummary{}, EntrypointConns: map[string]float64{}}
		if traffic.Enabled() {
			rep := traffic.Report()
			resp.LastScrape = rep.LastScrape
			resp.WindowSeconds = rep.WindowSeconds
			resp.EntrypointConns = rep.EntrypointConns
			resp.CertExpiresAt = rep.CertExpiresAt
			refs := refsByLengthDesc(projects)
			perProject := map[string]*trafficTotals{}
			for _, rt := range rep.Routers {
				ref, _, ok := splitRouterRef(rt.Router, refs)
				if !ok {
					continue
				}
				if perProject[ref] == nil {
					perProject[ref] = &trafficTotals{}
				}
				accumulate(perProject[ref], rt)
				accumulate(&resp.Totals, rt)
			}
			for _, ref := range refs {
				if t := perProject[ref]; t != nil {
					resp.Projects = append(resp.Projects, projectTrafficSummary{Ref: ref, trafficTotals: *t})
				}
			}
			sort.Slice(resp.Projects, func(i, j int) bool { return resp.Projects[i].RequestsPerSec > resp.Projects[j].RequestsPerSec })
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
