package operator

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Metrics holds operator reconcile counters exposed in Prometheus text format
// on the metrics endpoint. It is dependency-free (no client_golang) so the
// operator binary stays slim while still presenting a scrapeable /metrics.
type Metrics struct {
	reconcileTotal   atomic.Int64
	reconcileErrors  atomic.Int64
	leaderStatus     atomic.Int64 // 1 when this replica holds leadership, else 0
	lastReconcileOK  atomic.Int64 // 1 when last reconcile succeeded, else 0
	leaderElectionOn bool
}

// NewMetrics returns a Metrics collector. leaderElectionEnabled controls whether
// the leader gauge is emitted.
func NewMetrics(leaderElectionEnabled bool) *Metrics {
	return &Metrics{leaderElectionOn: leaderElectionEnabled}
}

// RecordReconcile records the outcome of a reconcile loop iteration.
func (m *Metrics) RecordReconcile(err error) {
	m.reconcileTotal.Add(1)
	if err != nil {
		m.reconcileErrors.Add(1)
		m.lastReconcileOK.Store(0)
		return
	}
	m.lastReconcileOK.Store(1)
}

// SetLeader records whether this replica currently holds leadership.
func (m *Metrics) SetLeader(isLeader bool) {
	if isLeader {
		m.leaderStatus.Store(1)
		return
	}
	m.leaderStatus.Store(0)
}

// Handler renders the metrics in Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP supadupa_operator_reconcile_total Total reconcile loop iterations.\n")
		fmt.Fprintf(w, "# TYPE supadupa_operator_reconcile_total counter\n")
		fmt.Fprintf(w, "supadupa_operator_reconcile_total %d\n", m.reconcileTotal.Load())
		fmt.Fprintf(w, "# HELP supadupa_operator_reconcile_errors_total Total reconcile loop iterations that returned an error.\n")
		fmt.Fprintf(w, "# TYPE supadupa_operator_reconcile_errors_total counter\n")
		fmt.Fprintf(w, "supadupa_operator_reconcile_errors_total %d\n", m.reconcileErrors.Load())
		fmt.Fprintf(w, "# HELP supadupa_operator_last_reconcile_success Whether the last reconcile loop succeeded (1) or failed (0).\n")
		fmt.Fprintf(w, "# TYPE supadupa_operator_last_reconcile_success gauge\n")
		fmt.Fprintf(w, "supadupa_operator_last_reconcile_success %d\n", m.lastReconcileOK.Load())
		if m.leaderElectionOn {
			fmt.Fprintf(w, "# HELP supadupa_operator_leader Whether this replica currently holds leadership (1) or not (0).\n")
			fmt.Fprintf(w, "# TYPE supadupa_operator_leader gauge\n")
			fmt.Fprintf(w, "supadupa_operator_leader %d\n", m.leaderStatus.Load())
		}
	})
}
