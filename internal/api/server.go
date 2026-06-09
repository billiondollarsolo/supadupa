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
}

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

	mux := http.NewServeMux()
	registerAPIRoutes(mux, routeRegistrationConfig{
		store:               store,
		auth:                auth,
		provisioner:         cfg.Provisioner,
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
