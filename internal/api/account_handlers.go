package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"supadupa2026/internal/control"
)

func changeAccountPasswordHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		var payload struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		user, err := store.GetUserByID(r.Context(), userID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		// Verify the current password before allowing a change.
		if _, err := store.AuthenticateUser(r.Context(), user.Email, payload.CurrentPassword); err != nil {
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		if _, err := store.UpdateUser(r.Context(), userID, control.UpdateUserRequest{Email: user.Email, Role: user.Role, Password: payload.NewPassword}); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "account.password_change", "user:"+userID, map[string]string{"email": user.Email})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		status, err := store.GetUserMFAStatus(r.Context(), userID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func enrollAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		enrollment, err := store.BeginUserMFAEnrollment(r.Context(), userID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.mfa_enroll", "user:"+userID, map[string]string{"email": enrollment.Email})
		writeJSON(w, http.StatusCreated, enrollment)
	}
}

func verifyAccountMFAHandler(store control.Store, limiter *fixedWindowLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		var payload mfaCodeRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		limitKey := sensitiveActionKey(r, "mfa-verify", userID)
		if allowed, retryAfter := limiter.Allow(limitKey, time.Now().UTC()); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many mfa attempts")
			return
		}
		status, err := store.ConfirmUserMFA(r.Context(), userID, payload.Code)
		if err != nil {
			auditMFACodeFailure(r.Context(), store, "user.mfa_verify_failed", userID, err)
			writeStoreError(w, err)
			return
		}
		limiter.Reset(limitKey)
		control.Audit(r.Context(), store, "user.mfa_verify", "user:"+userID, map[string]string{"email": status.Email})
		writeJSON(w, http.StatusOK, status)
	}
}

func disableAccountMFAHandler(store control.Store, limiter *fixedWindowLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		var payload mfaCodeRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		limitKey := sensitiveActionKey(r, "mfa-disable", userID)
		if allowed, retryAfter := limiter.Allow(limitKey, time.Now().UTC()); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many mfa attempts")
			return
		}
		status, err := store.DisableUserMFA(r.Context(), userID, payload.Code)
		if err != nil {
			auditMFACodeFailure(r.Context(), store, "user.mfa_disable_failed", userID, err)
			writeStoreError(w, err)
			return
		}
		limiter.Reset(limitKey)
		control.Audit(r.Context(), store, "user.mfa_disable", "user:"+userID, map[string]string{"email": status.Email})
		writeJSON(w, http.StatusOK, status)
	}
}

func auditMFACodeFailure(ctx context.Context, store control.Store, action string, userID string, err error) {
	control.Audit(ctx, store, action, "user:"+userID, map[string]string{
		"reason": sanitizeAuditReason(err),
	})
}
