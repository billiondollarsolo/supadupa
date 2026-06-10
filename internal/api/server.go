package api

import (
	"log/slog"
	"net/http"
	"os"

	"supadupa2026/internal/control"
)

type Config struct {
	Addr         string
	Logger       *slog.Logger
	Provisioner  control.Provisioner
	Store        control.Store
	Auth         *control.AuthService
	AuthRequired bool
	CORSOrigins  []string

	// provisionDispatcher runs long-running provisioning work off the request
	// path. It defaults to launching a goroutine (asynchronous provisioning);
	// tests inject a synchronous runner for deterministic behavior.
	provisionDispatcher provisionDispatcher
}

// provisionDispatcher decouples slow infrastructure provisioning from the HTTP
// request that triggers it. Project creation persists the project in the
// "provisioning" phase, returns 202 immediately, and runs the actual compose-up
// / edge-router wiring through this dispatcher; the background reconciler then
// converges the project to healthy or error.
type provisionDispatcher func(func())

func asyncProvisionDispatcher(f func()) { go f() }

// defaultProvisionDispatcher is the dispatcher used when a Config does not set
// one. Production keeps it asynchronous; the test package overrides it with a
// synchronous runner in TestMain so provisioning completes deterministically.
var defaultProvisionDispatcher provisionDispatcher = asyncProvisionDispatcher

func NewServer(cfg Config) *http.Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := cfg.Store
	if store == nil {
		store = control.NewMemoryStore()
	}
	authLimiter := newAuthAttemptLimiter()
	secretAccessLimiter := newFixedWindowLimiter(maxSecretAccessAttempts, secretAccessWindow)
	mfaAccessLimiter := newFixedWindowLimiter(maxMFAAccessAttempts, mfaAccessWindow)
	ssoCallbackLimiter := newFixedWindowLimiter(maxSSOCallbackFailures, ssoCallbackWindow)
	studioSessions := newStudioSessionStore()
	ssoAssertions := newSSOAssertionReplayCache()
	auth := cfg.Auth
	if auth == nil {
		auth = control.NewAuthService(control.AuthSecretFromEnv(os.Getenv))
	}
	dispatch := cfg.provisionDispatcher
	if dispatch == nil {
		dispatch = defaultProvisionDispatcher
	}

	mux := http.NewServeMux()
	registerAPIRoutes(mux, routeRegistrationConfig{
		store:               store,
		auth:                auth,
		provisioner:         cfg.Provisioner,
		provisionDispatcher: dispatch,
		authRequired:        cfg.AuthRequired,
		authLimiter:         authLimiter,
		secretAccessLimiter: secretAccessLimiter,
		mfaAccessLimiter:    mfaAccessLimiter,
		ssoCallbackLimiter:  ssoCallbackLimiter,
		studioSessions:      studioSessions,
		ssoAssertions:       ssoAssertions,
	})

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: requestLogger(logger, withRequestBodyLimit(withCORS(withAuth(cfg.AuthRequired, auth, mux), cfg.CORSOrigins))),
	}
}
