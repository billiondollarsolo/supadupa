package control

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestAuthSecretFromEnvPrefersDedicatedAuthSecret(t *testing.T) {
	secret := AuthSecretFromEnv(func(key string) string {
		switch key {
		case AuthSecretEnv:
			return "auth-secret"
		case PlatformSecretKeyEnv:
			return "platform-secret"
		default:
			return ""
		}
	})
	if secret != "auth-secret" {
		t.Fatalf("secret = %q, want auth-secret", secret)
	}
}

func TestAuthSecretFromEnvFallsBackToPlatformSecretKey(t *testing.T) {
	secret := AuthSecretFromEnv(func(key string) string {
		switch key {
		case AuthSecretEnv:
			return ""
		case PlatformSecretKeyEnv:
			return "platform-secret"
		default:
			return ""
		}
	})
	if secret != "platform-secret" {
		t.Fatalf("secret = %q, want platform-secret", secret)
	}
}

func TestAuthSecretFromEnvTrimsAndAllowsDevelopmentDefault(t *testing.T) {
	if secret := AuthSecretFromEnv(func(string) string { return "  " }); secret != "" {
		t.Fatalf("secret = %q, want empty", secret)
	}
}

func TestHashPasswordUsesBcryptSHA256(t *testing.T) {
	hash := hashPassword("correct horse battery staple")
	if !strings.HasPrefix(hash, "bcrypt-sha256$") {
		t.Fatalf("expected bcrypt-sha256 hash, got %q", hash)
	}
	if !verifyPassword("correct horse battery staple", hash) {
		t.Fatal("expected bcrypt-sha256 hash to verify")
	}
	if verifyPassword("wrong", hash) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestAuthenticateUserRehashesLegacySHA256Password(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	legacyHash := legacySaltedPasswordHash("super-secure", "abcdef")
	store.mu.Lock()
	user.PasswordHash = legacyHash
	store.users[user.Email] = user
	store.mu.Unlock()

	authenticated, err := store.AuthenticateUser(ctx, "admin@example.com", "super-secure")
	if err != nil {
		t.Fatalf("authenticate legacy hash: %v", err)
	}
	if authenticated.PasswordHash == legacyHash {
		t.Fatal("expected returned user to contain rehashed password")
	}
	store.mu.RLock()
	rehash := store.users[user.Email].PasswordHash
	store.mu.RUnlock()
	if !strings.HasPrefix(rehash, "bcrypt-sha256$") {
		t.Fatalf("expected stored password to be rehashed, got %q", rehash)
	}
}

func TestUserPasswordPolicyRejectsWeakAndPlaceholderPasswords(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for _, password := range []string{
		"",
		"short",
		"password1234",
		"dev-only-change-me",
		" leading-space-password",
		"trailing-space-password ",
		"contains\ttab-password",
	} {
		_, err := store.CreateUser(ctx, CreateUserRequest{Email: "weak-" + strings.ReplaceAll(password, " ", "-") + "@example.com", Password: password, Role: "admin"})
		if err == nil {
			t.Fatalf("expected password %q to be rejected", password)
		}
	}
}

func TestUpdateUserPasswordPolicyRejectsWeakPasswordAndAllowsEmptyPassword(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	originalHash := user.PasswordHash
	if _, err := store.UpdateUser(ctx, user.ID, UpdateUserRequest{Email: "renamed@example.com", Password: "password1234", Role: "admin"}); err == nil || !strings.Contains(err.Error(), "too common") {
		t.Fatalf("expected weak update password rejection, got %v", err)
	}
	updated, err := store.UpdateUser(ctx, user.ID, UpdateUserRequest{Email: "renamed@example.com", Role: "admin"})
	if err != nil {
		t.Fatalf("update user without password: %v", err)
	}
	if updated.PasswordHash != originalHash {
		t.Fatal("expected empty password update to preserve existing password hash")
	}
	if _, err := store.AuthenticateUser(ctx, "renamed@example.com", "super-secure"); err != nil {
		t.Fatalf("expected preserved password to authenticate: %v", err)
	}
}

func TestVerifyTOTPCodeCounterReturnsAcceptedCounter(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate totp secret: %v", err)
	}
	at := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	code, err := TOTPCode(secret, at)
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	counter, ok := VerifyTOTPCodeCounter(secret, code, at)
	if !ok {
		t.Fatal("expected totp code to verify")
	}
	if counter != uint64(at.Unix()/30) {
		t.Fatalf("expected counter %d, got %d", at.Unix()/30, counter)
	}
	if _, ok := VerifyTOTPCodeCounter(secret, code, at.Add(90*time.Second)); ok {
		t.Fatal("expected code outside verification window to fail")
	}
}

func TestUserMFARejectsReplayedCounters(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	enrollment, err := store.BeginUserMFAEnrollment(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin mfa enrollment: %v", err)
	}
	now := time.Now().UTC()
	enrollCode, err := TOTPCode(enrollment.Secret, now.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("enroll code: %v", err)
	}
	if _, err := store.ConfirmUserMFA(ctx, user.ID, enrollCode); err != nil {
		t.Fatalf("confirm mfa: %v", err)
	}
	currentCode, err := TOTPCode(enrollment.Secret, now)
	if err != nil {
		t.Fatalf("current code: %v", err)
	}
	if _, err := store.VerifyUserMFA(ctx, user.ID, currentCode); err != nil {
		t.Fatalf("verify current mfa code: %v", err)
	}
	if _, err := store.VerifyUserMFA(ctx, user.ID, currentCode); err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("expected replayed mfa code rejection, got %v", err)
	}
	previousCode, err := TOTPCode(enrollment.Secret, now.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("previous code: %v", err)
	}
	if _, err := store.DisableUserMFA(ctx, user.ID, previousCode); err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("expected lower counter disable rejection, got %v", err)
	}
}

func TestPlatformSCIMTokenHashUsesKeyedVersionedFormat(t *testing.T) {
	t.Setenv(AuthSecretEnv, "scim-hmac-test-secret")
	hash := HashPlatformSCIMToken("scim-secret-token-value-123456")
	if !strings.HasPrefix(hash, "hmac-sha256$") {
		t.Fatalf("expected versioned HMAC SCIM hash, got %q", hash)
	}
	config := PlatformSSOConfig{SCIMEnabled: true, SCIMTokenHash: hash}
	if !VerifyPlatformSCIMToken(config, "scim-secret-token-value-123456") {
		t.Fatal("expected SCIM token to verify")
	}
	if VerifyPlatformSCIMToken(config, "wrong-scim-token-value-123456") {
		t.Fatal("expected wrong SCIM token to fail")
	}
	if PlatformSCIMTokenNeedsRehash(config, "scim-secret-token-value-123456") {
		t.Fatal("new SCIM hash should not need rehash")
	}
}

func TestPlatformSCIMTokenAcceptsLegacySHA256AndSignalsRehash(t *testing.T) {
	t.Setenv(AuthSecretEnv, "scim-hmac-test-secret")
	token := "legacy-scim-token-value-123456"
	config := PlatformSSOConfig{SCIMEnabled: true, SCIMTokenHash: legacyPlatformSCIMTokenHash(token)}
	if !VerifyPlatformSCIMToken(config, token) {
		t.Fatal("expected legacy SCIM token hash to verify")
	}
	if !PlatformSCIMTokenNeedsRehash(config, token) {
		t.Fatal("expected legacy SCIM token hash to need rehash")
	}
	if VerifyPlatformSCIMToken(config, "wrong-legacy-scim-token") {
		t.Fatal("expected wrong legacy SCIM token to fail")
	}
}

func TestPlatformSCIMTokenInputRequiresMinimumLength(t *testing.T) {
	_, err := normalizePlatformSSOInput(PlatformSSOConfigInput{SCIMEnabled: true, SCIMToken: "short"})
	if err == nil || !strings.Contains(err.Error(), "scim_token must be at least 24 characters") {
		t.Fatalf("expected short SCIM token rejection, got %v", err)
	}
}

func legacySaltedPasswordHash(password string, salt string) string {
	return "sha256$" + salt + "$" + strings.ToLower(hex.EncodeToString(hashBytes([]byte(salt+password))))
}
