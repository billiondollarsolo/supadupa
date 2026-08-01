package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

type contextKey string

const (
	tokenClaimsKey contextKey = "token_claims"
	authBypassKey  contextKey = "auth_bypass"
)

const authCookieName = "supadupa_session"

const maxRequestBodyBytes = 10 * 1024 * 1024
const defaultJSONBodyBytes = 1 * 1024 * 1024

var errRequestBodyTooLarge = errors.New("request body too large")

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func withRequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var limit int64 = maxRequestBodyBytes
		if isMutatingMethod(r.Method) && isJSONRequest(r) {
			limit = defaultJSONBodyBytes
		}
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func isJSONRequest(r *http.Request) bool {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.HasPrefix(contentType, "application/json")
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func withCORS(next http.Handler, configuredOrigins []string) http.Handler {
	allowedOrigins := allowedCORSOrigins(configuredOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Supadupa-Browser")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		} else if origin != "" && (r.Method == http.MethodOptions || isMutatingMethod(r.Method)) && !isCrossOriginCallbackPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		if origin == "" && isMutatingMethod(r.Method) && !isCrossOriginCallbackPath(r.URL.Path) && usesCookieAuth(r) {
			writeError(w, http.StatusForbidden, "origin is required for cookie-authenticated mutations")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isCrossOriginCallbackPath(path string) bool {
	return path == "/v1/auth/sso/saml/callback"
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func usesCookieAuth(r *http.Request) bool {
	if bearerTokenFromRequest(r) != "" {
		return false
	}
	cookie, err := r.Cookie(authCookieName)
	return err == nil && strings.TrimSpace(cookie.Value) != ""
}

func allowedCORSOrigins(configured []string) map[string]bool {
	origins := configured
	if len(origins) == 0 {
		origins = strings.Split(os.Getenv("SUPADUPA_CORS_ORIGINS"), ",")
	}
	out := make(map[string]bool)
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			out[origin] = true
		}
	}
	if len(out) == 0 {
		addDerivedCORSOrigin(out, os.Getenv("SUPADUPA_ADMIN_URL"))
		addDerivedCORSOrigin(out, os.Getenv("SUPADUPA_ADMIN_HOST"))
		if len(out) == 0 {
			for _, origin := range []string{
				"http://localhost:3000",
				"http://127.0.0.1:3000",
				"http://localhost:3001",
				"http://127.0.0.1:3001",
				"http://localhost:5173",
				"http://127.0.0.1:5173",
				"http://localhost:5174",
				"http://127.0.0.1:5174",
			} {
				out[origin] = true
			}
		}
	}
	return out
}

func addDerivedCORSOrigin(out map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		out[strings.TrimRight(value, "/")] = true
		return
	}
	out["https://"+strings.Trim(value, "/")] = true
}

func withAuth(required bool, auth *control.AuthService, store control.Store, next http.Handler) http.Handler {
	if !required {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), authBypassKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token := tokenFromRequest(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := auth.Verify(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		if _, ok := requireCurrentUserClaims(w, r, store, claims); !ok {
			return
		}
		// Carry the authenticated actor + client IP so every audited action
		// downstream records who did it and from where.
		ctx := context.WithValue(r.Context(), tokenClaimsKey, claims)
		ctx = control.WithAuditContext(ctx, claims.Subject, clientIP(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isPublicPath(path string) bool {
	return path == "/healthz" ||
		path == "/v1/health" ||
		path == "/v1/auth/state" ||
		path == "/v1/auth/bootstrap" ||
		path == "/v1/auth/login" ||
		path == "/v1/auth/logout" ||
		path == "/v1/auth/studio/verify" ||
		path == "/v1/auth/sso/saml/start" ||
		path == "/v1/auth/sso/saml/callback" ||
		strings.HasPrefix(path, "/v1/scim/v2/")
}

func tokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if ok && strings.EqualFold(scheme, "Bearer") && token != "" {
		return token
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func bearerTokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return ""
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	cookie := authCookie(r, token, int(ttl.Seconds()))
	http.SetCookie(w, &cookie)
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	cookie := authCookie(r, "", -1)
	cookie.Expires = time.Unix(0, 0)
	http.SetCookie(w, &cookie)
}

func authCookie(r *http.Request, value string, maxAge int) http.Cookie {
	cookie := http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   maxAge,
	}
	if domain := authCookieDomain(r); domain != "" {
		cookie.Domain = domain
	}
	return cookie
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func authCookieDomain(r *http.Request) string {
	if configured := strings.TrimSpace(os.Getenv("SUPADUPA_COOKIE_DOMAIN")); configured != "" {
		if strings.EqualFold(configured, "host-only") || strings.EqualFold(configured, "none") {
			return ""
		}
		return strings.TrimPrefix(configured, ".")
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(r *http.Request, target any) error {
	return decodeLimitedJSON(r, target, defaultJSONBodyBytes)
}

func decodeLimitedJSON(r *http.Request, target any, limit int64) error {
	if limit <= 0 {
		limit = defaultJSONBodyBytes
	}
	if r.ContentLength > limit {
		return errRequestBodyTooLarge
	}
	reader := &io.LimitedReader{R: r.Body, N: limit + 1}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return normalizeJSONDecodeError(err, reader)
	}
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if reader.N <= 0 {
		return errRequestBodyTooLarge
	}
	if err != io.EOF {
		if err == nil {
			return errors.New("request body must contain only one JSON value")
		}
		return normalizeJSONDecodeError(err, reader)
	}
	return nil
}

func normalizeJSONDecodeError(err error, reader *io.LimitedReader) error {
	if reader.N <= 0 || strings.Contains(err.Error(), "http: request body too large") {
		return errRequestBodyTooLarge
	}
	return err
}

func writeDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// logRollbackError records a best-effort control-plane rollback failure so
// operators can detect orphaned project-child rows after apply-path cleanup.
// Primary create failures still own the user-facing error response.
func logRollbackError(ctx context.Context, action string, err error) {
	if err == nil {
		return
	}
	slog.Default().ErrorContext(ctx, "rollback failed", "action", action, "error", err)
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, control.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
