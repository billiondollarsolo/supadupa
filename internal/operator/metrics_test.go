package operator

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerRendersPrometheusText(t *testing.T) {
	m := NewMetrics(true)
	m.RecordReconcile(nil)
	m.RecordReconcile(context.Canceled)
	m.SetLeader(true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("unexpected content type %q", ct)
	}
	for _, want := range []string{
		"supadupa_operator_reconcile_total 2",
		"supadupa_operator_reconcile_errors_total 1",
		"supadupa_operator_last_reconcile_success 0",
		"supadupa_operator_leader 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in metrics output:\n%s", want, body)
		}
	}
}

func TestMetricsLeaderGaugeOmittedWhenLeaderElectionDisabled(t *testing.T) {
	m := NewMetrics(false)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(rec.Body.String(), "supadupa_operator_leader ") {
		t.Fatalf("leader gauge should be omitted when leader election disabled:\n%s", rec.Body.String())
	}
}
