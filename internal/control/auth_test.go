package control

import "testing"

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
