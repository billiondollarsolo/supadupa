package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"supadupa2026/internal/env"
	"sync/atomic"
	"syscall"
	"time"

	"supadupa2026/internal/operator"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	namespace := env.OrDefault("SUPADUPA_OPERATOR_NAMESPACE", "supadupa")
	interval := durationEnvOrDefault("SUPADUPA_OPERATOR_INTERVAL", 15*time.Second)

	client, err := operator.NewInClusterKubernetesClient()
	if err != nil {
		logger.Error("create kubernetes client", "error", err)
		os.Exit(1)
	}
	client.ControlNamespace = namespace

	reconciler := operator.Reconciler{
		Client:                     client,
		IsolationEnabled:           boolEnvOrDefault("SUPADUPA_OPERATOR_ISOLATION", true),
		RuntimeNamespacePrefix:     env.OrDefault("SUPADUPA_OPERATOR_RUNTIME_NAMESPACE_PREFIX", "supadupa-proj-"),
		PodSecurityEnforce:         env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_ENFORCE", "baseline"),
		PodSecurityAudit:           env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_AUDIT", "restricted"),
		PodSecurityWarn:            env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_WARN", "restricted"),
		PodSecurityEnforceVersion:  env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_ENFORCE_VERSION", "latest"),
		PodSecurityAuditVersion:    env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_AUDIT_VERSION", "latest"),
		PodSecurityWarnVersion:     env.OrDefault("SUPADUPA_OPERATOR_POD_SECURITY_WARN_VERSION", "latest"),
		NetworkPolicyEnabled:       boolEnvOrDefault("SUPADUPA_OPERATOR_NETWORK_POLICY_ENABLED", true),
		IngressControllerNamespace: env.OrDefault("SUPADUPA_OPERATOR_INGRESS_NAMESPACE", "ingress-nginx"),
		DNSNamespace:               env.OrDefault("SUPADUPA_OPERATOR_DNS_NAMESPACE", "kube-system"),
		ExtraEgressCIDRs:           listEnv("SUPADUPA_OPERATOR_EXTRA_EGRESS_CIDRS"),
		DefaultQuota:               quotaFromEnv(),
		DefaultLimits:              limitsFromEnv(),
	}
	// An explicit namespaceSelector (matchLabels) overrides the name-based
	// selector; otherwise derive it from the ingress namespace name.
	if selector := mapEnv("SUPADUPA_OPERATOR_INGRESS_NAMESPACE_SELECTOR"); len(selector) > 0 {
		reconciler.IngressControllerNamespaceSelector = selector
	} else {
		reconciler.IngressControllerNamespaceSelector = map[string]string{
			"kubernetes.io/metadata.name": reconciler.IngressControllerNamespace,
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	leaderElectionEnabled := boolEnvOrDefault("SUPADUPA_OPERATOR_LEADER_ELECTION", false)
	metrics := operator.NewMetrics(leaderElectionEnabled)

	var ready atomic.Bool
	healthServer := startHealthServer(logger, env.OrDefault("SUPADUPA_OPERATOR_HEALTH_ADDR", ":8081"), &ready)
	metricsServer := startMetricsServer(logger, strings.TrimSpace(os.Getenv("SUPADUPA_OPERATOR_METRICS_ADDR")), metrics)

	shutdown := func() {
		logger.Info("operator stopped")
		for _, server := range []*http.Server{healthServer, metricsServer} {
			if server == nil {
				continue
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdownCtx)
			cancel()
		}
	}

	var elector *operator.LeaderElector
	if leaderElectionEnabled {
		elector = &operator.LeaderElector{
			Client:        client,
			Namespace:     namespace,
			Name:          env.OrDefault("SUPADUPA_OPERATOR_LEADER_ELECTION_LEASE", "supadupa-operator-leader"),
			Identity:      leaderIdentity(),
			LeaseDuration: interval * 3,
			RetryPeriod:   interval,
		}
		logger.Info("leader election enabled", "lease", elector.Name, "identity", elector.Identity)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if leaderElectionEnabled {
			isLeader, err := elector.TryAcquireOrRenew(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Error("leader election", "error", err)
			}
			metrics.SetLeader(isLeader)
			if !isLeader {
				// Followers do not reconcile and report not-ready so traffic /
				// readiness reflects that this replica is passive.
				ready.Store(false)
				select {
				case <-ctx.Done():
					shutdown()
					return
				case <-ticker.C:
					continue
				}
			}
		}
		err := reconciler.ReconcileNamespace(ctx, namespace)
		if ctx.Err() == nil {
			metrics.RecordReconcile(err)
			if err != nil {
				logger.Error("reconcile namespace", "namespace", namespace, "error", err)
				ready.Store(false)
			} else {
				ready.Store(true)
			}
		}
		select {
		case <-ctx.Done():
			shutdown()
			return
		case <-ticker.C:
		}
	}
}

func startMetricsServer(logger *slog.Logger, addr string, metrics *operator.Metrics) *http.Server {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server", "addr", addr, "error", err)
		}
	}()
	return server
}

// leaderIdentity uses the pod name (downward API via SUPADUPA_OPERATOR_POD_NAME
// or HOSTNAME) so each replica has a distinct lease holder identity.
func leaderIdentity() string {
	if id := strings.TrimSpace(os.Getenv("SUPADUPA_OPERATOR_POD_NAME")); id != "" {
		return id
	}
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return "supadupa-operator"
}

// listEnv parses a comma-separated env var into a trimmed, non-empty slice.
func listEnv(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// mapEnv parses a comma-separated key=value env var into a map.
func mapEnv(name string) map[string]string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func startHealthServer(logger *slog.Logger, addr string, ready *atomic.Bool) *http.Server {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server", "addr", addr, "error", err)
		}
	}()
	return server
}

func quotaFromEnv() *operator.ProjectQuotaDefaults {
	hard := map[string]string{}
	addEnv(hard, "requests.cpu", "SUPADUPA_OPERATOR_QUOTA_REQUESTS_CPU")
	addEnv(hard, "requests.memory", "SUPADUPA_OPERATOR_QUOTA_REQUESTS_MEMORY")
	addEnv(hard, "limits.cpu", "SUPADUPA_OPERATOR_QUOTA_LIMITS_CPU")
	addEnv(hard, "limits.memory", "SUPADUPA_OPERATOR_QUOTA_LIMITS_MEMORY")
	addEnv(hard, "pods", "SUPADUPA_OPERATOR_QUOTA_PODS")
	addEnv(hard, "persistentvolumeclaims", "SUPADUPA_OPERATOR_QUOTA_PVCS")
	if len(hard) == 0 {
		return nil
	}
	return &operator.ProjectQuotaDefaults{Hard: hard}
}

func limitsFromEnv() *operator.ProjectLimitDefaults {
	def := map[string]string{}
	addEnv(def, "cpu", "SUPADUPA_OPERATOR_LIMIT_DEFAULT_CPU")
	addEnv(def, "memory", "SUPADUPA_OPERATOR_LIMIT_DEFAULT_MEMORY")
	req := map[string]string{}
	addEnv(req, "cpu", "SUPADUPA_OPERATOR_LIMIT_REQUEST_CPU")
	addEnv(req, "memory", "SUPADUPA_OPERATOR_LIMIT_REQUEST_MEMORY")
	if len(def) == 0 && len(req) == 0 {
		return nil
	}
	return &operator.ProjectLimitDefaults{Default: def, DefaultRequest: req}
}

func addEnv(target map[string]string, key string, env string) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		target[key] = value
	}
}

func boolEnvOrDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnvOrDefault(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
