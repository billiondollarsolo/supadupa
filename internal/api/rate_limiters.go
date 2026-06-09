package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxAuthFailures = 5
const maxAuthAttemptKeys = 10000
const authAttemptWindow = 10 * time.Minute
const authAttemptLockout = 15 * time.Minute
const maxSecretAccessAttempts = 10
const secretAccessWindow = 10 * time.Minute
const maxMFAAccessAttempts = 5
const mfaAccessWindow = 10 * time.Minute
const maxSSOCallbackFailures = 5
const ssoCallbackWindow = 10 * time.Minute

type authAttempt struct {
	Failures    int
	FirstSeenAt time.Time
	LockedUntil time.Time
}

type authAttemptLimiter struct {
	mu       sync.Mutex
	attempts map[string]authAttempt
}

type fixedWindowLimit struct {
	Count       int
	WindowStart time.Time
}

type fixedWindowLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string]fixedWindowLimit
}

func newAuthAttemptLimiter() *authAttemptLimiter {
	return &authAttemptLimiter{attempts: map[string]authAttempt{}}
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, window: window, attempts: map[string]fixedWindowLimit{}}
}

func (l *authAttemptLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	if l == nil || key == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	attempt := l.attempts[key]
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false, attempt.LockedUntil.Sub(now)
	}
	return true, 0
}

func (l *authAttemptLimiter) AllowAll(keys []string, now time.Time) (bool, time.Duration) {
	var longestRetry time.Duration
	for _, key := range keys {
		allowed, retryAfter := l.Allow(key, now)
		if !allowed && retryAfter > longestRetry {
			longestRetry = retryAfter
		}
	}
	return longestRetry == 0, longestRetry
}

func (l *authAttemptLimiter) RecordFailure(key string, now time.Time) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	attempt := l.attempts[key]
	if attempt.FirstSeenAt.IsZero() || now.Sub(attempt.FirstSeenAt) > authAttemptWindow {
		attempt = authAttempt{FirstSeenAt: now}
	}
	attempt.Failures++
	if attempt.Failures >= maxAuthFailures {
		attempt.LockedUntil = now.Add(authAttemptLockout)
	}
	l.attempts[key] = attempt
	l.enforceLimitLocked(now)
}

func (l *authAttemptLimiter) RecordFailures(keys []string, now time.Time) {
	for _, key := range keys {
		l.RecordFailure(key, now)
	}
}

func (l *authAttemptLimiter) RecordSuccess(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *authAttemptLimiter) RecordSuccesses(keys []string) {
	for _, key := range keys {
		l.RecordSuccess(key)
	}
}

func (l *authAttemptLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		if !attempt.LockedUntil.IsZero() {
			if now.After(attempt.LockedUntil) {
				delete(l.attempts, key)
			}
			continue
		}
		if !attempt.FirstSeenAt.IsZero() && now.Sub(attempt.FirstSeenAt) > authAttemptWindow {
			delete(l.attempts, key)
		}
	}
}

func (l *authAttemptLimiter) enforceLimitLocked(now time.Time) {
	if len(l.attempts) <= maxAuthAttemptKeys {
		return
	}
	l.pruneLocked(now)
	for len(l.attempts) > maxAuthAttemptKeys {
		var oldestKey string
		var oldest time.Time
		for key, attempt := range l.attempts {
			candidate := attempt.FirstSeenAt
			if candidate.IsZero() {
				candidate = attempt.LockedUntil
			}
			if oldestKey == "" || candidate.Before(oldest) {
				oldestKey = key
				oldest = candidate
			}
		}
		if oldestKey == "" {
			return
		}
		delete(l.attempts, oldestKey)
	}
}

func (l *fixedWindowLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	if l == nil || key == "" || l.limit <= 0 || l.window <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, attempt := range l.attempts {
		if now.Sub(attempt.WindowStart) >= l.window {
			delete(l.attempts, existingKey)
		}
	}
	attempt := l.attempts[key]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= l.window {
		l.attempts[key] = fixedWindowLimit{Count: 1, WindowStart: now}
		return true, 0
	}
	if attempt.Count >= l.limit {
		return false, l.window - now.Sub(attempt.WindowStart)
	}
	attempt.Count++
	l.attempts[key] = attempt
	return true, 0
}

func (l *fixedWindowLimiter) TakeAll(keys []string, now time.Time) (bool, time.Duration) {
	if l == nil || l.limit <= 0 || l.window <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, attempt := range l.attempts {
		if now.Sub(attempt.WindowStart) >= l.window {
			delete(l.attempts, existingKey)
		}
	}
	var retryAfter time.Duration
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		attempt := l.attempts[key]
		if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= l.window {
			continue
		}
		if attempt.Count >= l.limit {
			wait := l.window - now.Sub(attempt.WindowStart)
			if wait > retryAfter {
				retryAfter = wait
			}
		}
	}
	if retryAfter > 0 {
		return false, retryAfter
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		attempt := l.attempts[key]
		if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= l.window {
			l.attempts[key] = fixedWindowLimit{Count: 1, WindowStart: now}
			continue
		}
		attempt.Count++
		l.attempts[key] = attempt
	}
	return true, 0
}

func (l *fixedWindowLimiter) CheckAll(keys []string, now time.Time) (bool, time.Duration) {
	if l == nil || l.limit <= 0 || l.window <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, attempt := range l.attempts {
		if now.Sub(attempt.WindowStart) >= l.window {
			delete(l.attempts, existingKey)
		}
	}
	var retryAfter time.Duration
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		attempt := l.attempts[key]
		if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= l.window {
			continue
		}
		if attempt.Count >= l.limit {
			wait := l.window - now.Sub(attempt.WindowStart)
			if wait > retryAfter {
				retryAfter = wait
			}
		}
	}
	return retryAfter == 0, retryAfter
}

func (l *fixedWindowLimiter) Reset(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *fixedWindowLimiter) ResetAll(keys []string) {
	for _, key := range keys {
		l.Reset(key)
	}
}

func sensitiveActionKey(r *http.Request, parts ...string) string {
	subject := "anonymous"
	if claims, ok := claimsFromRequest(r); ok && claims.Subject != "" {
		subject = claims.Subject
	}
	normalized := []string{subject, clientIP(r)}
	for _, part := range parts {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(part)))
	}
	return strings.Join(normalized, "|")
}

func secretAccessKeys(r *http.Request, action string, ref string, kind string) []string {
	subject := "anonymous"
	if claims, ok := claimsFromRequest(r); ok && claims.Subject != "" {
		subject = claims.Subject
	}
	action = strings.ToLower(strings.TrimSpace(action))
	ref = strings.ToLower(strings.TrimSpace(ref))
	kind = strings.ToLower(strings.TrimSpace(kind))
	return []string{
		strings.Join([]string{"secret-access-account", subject, action, ref, kind}, "|"),
		strings.Join([]string{"secret-access-ip", clientIP(r), action, ref, kind}, "|"),
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func sanitizeAuditReason(err error) string {
	if err == nil {
		return "unknown"
	}
	reason := strings.TrimSpace(err.Error())
	reason = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t':
			return ' '
		default:
			return r
		}
	}, reason)
	if reason == "" {
		return "unknown"
	}
	if len(reason) > 160 {
		return reason[:160]
	}
	return reason
}
