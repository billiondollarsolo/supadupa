package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"supadupa2026/internal/control"
)

type studioSessionCode struct {
	Claims     control.TokenClaims
	ProjectRef string
	ExpiresAt  time.Time
}

type studioSessionStore struct {
	mu    sync.Mutex
	codes map[string]studioSessionCode
}

type ssoAssertionReplayCache struct {
	mu         sync.Mutex
	signatures map[string]time.Time
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

type authResponse struct {
	Token       string       `json:"token,omitempty"`
	MFARequired bool         `json:"mfa_required,omitempty"`
	User        control.User `json:"user"`
}

type platformSSOCallbackResponse struct {
	Token string       `json:"token,omitempty"`
	User  control.User `json:"user"`
	SSO   string       `json:"sso"`
}

func newStudioSessionStore() *studioSessionStore {
	return &studioSessionStore{codes: map[string]studioSessionCode{}}
}

func newSSOAssertionReplayCache() *ssoAssertionReplayCache {
	return &ssoAssertionReplayCache{signatures: map[string]time.Time{}}
}

func (c *ssoAssertionReplayCache) Use(signature string, expiresAt time.Time, now time.Time) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}
	now = now.UTC()
	expiresAt = expiresAt.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	for existing, expiry := range c.signatures {
		if !now.Before(expiry) {
			delete(c.signatures, existing)
		}
	}
	if _, exists := c.signatures[signature]; exists {
		return false
	}
	c.signatures[signature] = expiresAt
	return true
}

func (c *ssoAssertionReplayCache) Forget(signature string) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return
	}
	c.mu.Lock()
	delete(c.signatures, signature)
	c.mu.Unlock()
}

func authStateHandler(store control.Store, auth *control.AuthService, authRequired bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"bootstrapped":  store.HasUsers(r.Context()),
			"auth_required": authRequired,
			"authenticated": false,
		}
		if auth != nil {
			if token := tokenFromRequest(r); token != "" {
				if claims, err := auth.Verify(token); err == nil {
					if user, err := store.GetUserByID(r.Context(), claims.Subject); err == nil {
						response["authenticated"] = true
						response["user"] = user
					}
				}
			}
		}
		if config, err := store.GetPlatformSSOConfig(r.Context()); err == nil {
			response["sso_enabled"] = config.Enabled
			response["sso_provider"] = config.Provider
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func bootstrapHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store.HasUsers(r.Context()) {
			writeError(w, http.StatusConflict, "admin user already exists")
			return
		}
		var payload authRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		user, err := store.CreateUser(r.Context(), control.CreateUserRequest{
			Email:    payload.Email,
			Password: payload.Password,
			Role:     "admin",
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		if at, err := store.RecordUserLogin(r.Context(), user.ID); err == nil {
			user.LastLoginAt = &at
		}
		control.Audit(r.Context(), store, "user.bootstrap", "user:"+user.ID, map[string]string{"email": user.Email})
		writeJSON(w, http.StatusCreated, authResponseForRequest(r, token, user))
	}
}

func loginHandler(store control.Store, auth *control.AuthService, limiter *authAttemptLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload authRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		now := time.Now().UTC()
		attemptKeys := authAttemptKeys(r, payload.Email)
		if allowed, retryAfter := limiter.AllowAll(attemptKeys, now); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many failed authentication attempts")
			return
		}
		user, err := store.AuthenticateUser(r.Context(), payload.Email, payload.Password)
		if err != nil {
			limiter.RecordFailures(attemptKeys, now)
			auditLoginFailure(r.Context(), store, r, payload.Email, "invalid_credentials")
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if user.MFAEnabled {
			verifiedUser, err := store.VerifyUserMFA(r.Context(), user.ID, payload.TOTPCode)
			if err != nil {
				limiter.RecordFailures(attemptKeys, now)
				auditLoginFailure(r.Context(), store, r, payload.Email, "mfa_required")
				writeJSON(w, http.StatusAccepted, authResponse{MFARequired: true, User: user})
				return
			}
			user = verifiedUser
		}
		limiter.RecordSuccesses(attemptKeys)
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		// Stamp the successful login (post-MFA). Best-effort: a recording failure
		// must not block an otherwise-valid login.
		if at, err := store.RecordUserLogin(r.Context(), user.ID); err == nil {
			user.LastLoginAt = &at
		}
		control.Audit(r.Context(), store, "user.login", "user:"+user.ID, map[string]string{"email": user.Email})
		writeJSON(w, http.StatusOK, authResponseForRequest(r, token, user))
	}
}

func authResponseForRequest(r *http.Request, token string, user control.User) authResponse {
	if browserModeRequest(r) {
		return authResponse{User: user}
	}
	return authResponse{Token: token, User: user}
}

func browserModeRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Supadupa-Browser")), "true")
}

func authAttemptKeys(r *http.Request, email string) []string {
	keys := []string{"ip:" + clientIP(r)}
	if normalizedEmail := strings.ToLower(strings.TrimSpace(email)); normalizedEmail != "" {
		keys = append(keys, "email:"+normalizedEmail)
	}
	return keys
}

func ssoCallbackFailureKeys(r *http.Request, assertion control.PlatformSSOAssertion) []string {
	keys := []string{"sso-ip:" + clientIP(r)}
	if email := strings.ToLower(strings.TrimSpace(assertion.Email)); email != "" {
		keys = append(keys, "sso-email:"+email)
	}
	if nameID := strings.TrimSpace(assertion.NameID); nameID != "" {
		keys = append(keys, "sso-nameid:"+strings.ToLower(nameID))
	}
	if issuer := strings.TrimSpace(assertion.Issuer); issuer != "" {
		keys = append(keys, "sso-issuer:"+strings.ToLower(issuer))
	}
	return keys
}

func auditLoginFailure(ctx context.Context, store control.Store, r *http.Request, email string, reason string) {
	control.Audit(ctx, store, "user.login_failed", "auth:login", map[string]string{
		"email":     strings.ToLower(strings.TrimSpace(email)),
		"client_ip": clientIP(r),
		"reason":    reason,
	})
}

func logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAuthCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
	}
}

func startPlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		initiation := control.PlatformSSOInitiationForConfig(config)
		if !initiation.Enabled {
			writeError(w, http.StatusNotFound, "platform sso is disabled")
			return
		}
		writeJSON(w, http.StatusOK, initiation)
	}
}

func platformSSOCallbackHandler(store control.Store, auth *control.AuthService, limiter *fixedWindowLimiter, replayCache *ssoAssertionReplayCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		if allowed, retryAfter := limiter.CheckAll(ssoCallbackFailureKeys(r, control.PlatformSSOAssertion{}), now); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many sso callback failures")
			return
		}
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var assertion control.PlatformSSOAssertion
		if err := decodeJSON(r, &assertion); err != nil {
			writeSSOCallbackFailure(w, r, store, limiter, assertion, "decode_failed", err, http.StatusBadRequest)
			return
		}
		if allowed, retryAfter := limiter.CheckAll(ssoCallbackFailureKeys(r, assertion), now); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many sso callback failures")
			return
		}
		if err := control.ValidatePlatformSSOAssertion(config, assertion, now); err != nil {
			writeSSOCallbackFailure(w, r, store, limiter, assertion, "validation_failed", err, http.StatusUnauthorized)
			return
		}
		if replayCache != nil && !replayCache.Use(assertion.Signature, assertion.NotOnOrAfter, now) {
			writeSSOCallbackFailure(w, r, store, limiter, assertion, "replayed", fmt.Errorf("saml assertion has already been used"), http.StatusUnauthorized)
			return
		}
		user, err := platformSSOUser(r.Context(), store, config, assertion)
		if err != nil {
			if replayCache != nil {
				replayCache.Forget(assertion.Signature)
			}
			writeStoreError(w, err)
			return
		}
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			if replayCache != nil {
				replayCache.Forget(assertion.Signature)
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		if at, err := store.RecordUserLogin(r.Context(), user.ID); err == nil {
			user.LastLoginAt = &at
		}
		control.Audit(r.Context(), store, "user.sso_login", "user:"+user.ID, map[string]string{
			"email":   user.Email,
			"issuer":  assertion.Issuer,
			"name_id": assertion.NameID,
		})
		limiter.ResetAll(ssoCallbackFailureKeys(r, assertion))
		writeJSON(w, http.StatusOK, platformSSOCallbackResponse{User: user, SSO: "saml"})
	}
}

func writeSSOCallbackFailure(w http.ResponseWriter, r *http.Request, store control.Store, limiter *fixedWindowLimiter, assertion control.PlatformSSOAssertion, reason string, err error, status int) {
	keys := ssoCallbackFailureKeys(r, assertion)
	if allowed, retryAfter := limiter.TakeAll(keys, time.Now().UTC()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many sso callback failures")
		return
	}
	control.Audit(r.Context(), store, "user.sso_callback_failed", "auth:sso", map[string]string{
		"reason":    reason,
		"detail":    sanitizeAuditReason(err),
		"email":     strings.ToLower(strings.TrimSpace(assertion.Email)),
		"issuer":    strings.TrimSpace(assertion.Issuer),
		"name_id":   strings.TrimSpace(assertion.NameID),
		"client_ip": clientIP(r),
	})
	if errors.Is(err, errRequestBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	writeError(w, status, err.Error())
}

func studioForwardAuthHandler(store control.Store, auth *control.AuthService, studioSessions *studioSessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectFromForwardedStudioRequest(r, store)
		if !ok {
			writeError(w, http.StatusNotFound, "studio project route not found")
			return
		}
		var claims control.TokenClaims
		authenticated := false
		// Prefer an already-established session cookie. The one-time studio code
		// is single-use, but Studio echoes it back in the URL across its own
		// internal redirects (/ -> /project/default), so a second forward-auth
		// would otherwise try to consume the same (now-spent) code and fail. Once
		// the first request has set the session cookie, later requests authenticate
		// with it and ignore the stale code in the query string.
		if token := tokenFromRequest(r); token != "" {
			if verified, err := auth.Verify(token); err == nil {
				claims = verified
				authenticated = true
			}
		}
		if !authenticated {
			studioCode := studioCodeFromForwardedRequest(r)
			if studioCode == "" {
				writeError(w, http.StatusUnauthorized, "missing supadupa session")
				return
			}
			consumed, err := studioSessions.Consume(studioCode, project.Ref, time.Now().UTC())
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid supadupa studio session")
				return
			}
			studioToken, err := auth.Issue(control.User{ID: consumed.Subject, Email: consumed.Email, Role: consumed.Role}, 15*time.Minute)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			cookie := authCookie(r, studioToken, int((15 * time.Minute).Seconds()))
			http.SetCookie(w, &cookie)
			claims = consumed
		}
		if !strings.EqualFold(claims.Role, "admin") {
			reqWithClaims := r.WithContext(context.WithValue(r.Context(), tokenClaimsKey, claims))
			if _, ok := requireProjectRole(w, reqWithClaims, store, project.Ref, roleViewer); !ok {
				return
			}
		}
		w.Header().Set("X-Supadupa-User", claims.Email)
		w.Header().Set("X-Supadupa-Project", project.Ref)
		w.WriteHeader(http.StatusNoContent)
	}
}

func studioCodeFromForwardedRequest(r *http.Request) string {
	for _, raw := range []string{r.URL.RawQuery, strings.TrimSpace(r.Header.Get("X-Forwarded-Uri"))} {
		if raw == "" {
			continue
		}
		query := raw
		if strings.Contains(raw, "?") {
			_, query, _ = strings.Cut(raw, "?")
		}
		values, err := url.ParseQuery(query)
		if err != nil {
			continue
		}
		if code := strings.TrimSpace(values.Get("supadupa_studio_code")); code != "" {
			return code
		}
		if code := strings.TrimSpace(values.Get("studio_code")); code != "" {
			return code
		}
	}
	return ""
}

func projectFromForwardedStudioRequest(r *http.Request, store control.Store) (control.Project, bool) {
	if ref := strings.TrimSpace(r.URL.Query().Get("project_ref")); ref != "" {
		project, err := store.GetProject(r.Context(), ref)
		if err != nil {
			return control.Project{}, false
		}
		if hasForwardedStudioRouteEvidence(r) && !forwardedStudioRequestMatchesProject(r, project) {
			return control.Project{}, false
		}
		return project, true
	}
	projects, err := store.ListProjects(r.Context())
	if err != nil {
		return control.Project{}, false
	}
	for _, project := range projects {
		if forwardedStudioRequestMatchesProject(r, project) {
			return project, true
		}
	}
	return control.Project{}, false
}

func hasForwardedStudioRouteEvidence(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Forwarded-Host")) != "" || strings.TrimSpace(r.Header.Get("X-Forwarded-Uri")) != ""
}

func forwardedStudioRequestMatchesProject(r *http.Request, project control.Project) bool {
	host := forwardedHost(r)
	uri := strings.TrimSpace(r.Header.Get("X-Forwarded-Uri"))
	if host != "" && strings.EqualFold(host, fmt.Sprintf("studio-%s.%s", project.Ref, project.Spec.Domain)) {
		return true
	}
	return strings.HasPrefix(uri, "/projects/"+project.Ref+"/studio")
}

func forwardedHost(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" && strings.TrimSpace(r.Header.Get("X-Forwarded-Uri")) != "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	return strings.ToLower(strings.Split(host, ":")[0])
}

func (s *studioSessionStore) Create(claims control.TokenClaims, projectRef string, ttl time.Duration) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("studio session store is unavailable")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", time.Time{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(random[:])
	expiresAt := time.Now().UTC().Add(ttl)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now().UTC())
	s.codes[code] = studioSessionCode{
		Claims:     claims,
		ProjectRef: strings.TrimSpace(projectRef),
		ExpiresAt:  expiresAt,
	}
	return code, expiresAt, nil
}

func (s *studioSessionStore) Consume(code string, projectRef string, now time.Time) (control.TokenClaims, error) {
	if s == nil {
		return control.TokenClaims{}, fmt.Errorf("studio session store is unavailable")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return control.TokenClaims{}, fmt.Errorf("studio session code is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	session, ok := s.codes[code]
	if !ok {
		return control.TokenClaims{}, fmt.Errorf("studio session code is invalid")
	}
	delete(s.codes, code)
	if !now.Before(session.ExpiresAt) {
		return control.TokenClaims{}, fmt.Errorf("studio session code is expired")
	}
	if !strings.EqualFold(strings.TrimSpace(session.ProjectRef), strings.TrimSpace(projectRef)) {
		return control.TokenClaims{}, fmt.Errorf("studio session code is not scoped for project")
	}
	return session.Claims, nil
}

func (s *studioSessionStore) cleanupLocked(now time.Time) {
	for code, session := range s.codes {
		if !now.Before(session.ExpiresAt) {
			delete(s.codes, code)
		}
	}
}
