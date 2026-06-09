package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"supadupa2026/internal/control"
)

func platformSSOUser(ctx context.Context, store control.Store, config control.PlatformSSOConfig, assertion control.PlatformSSOAssertion) (control.User, error) {
	email := strings.ToLower(strings.TrimSpace(assertion.Email))
	users, err := store.ListUsers(ctx)
	if err != nil {
		return control.User{}, err
	}
	for _, user := range users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	if !config.AutoProvision {
		return control.User{}, fmt.Errorf("%w: platform user %s", control.ErrNotFound, email)
	}
	role := strings.ToLower(strings.TrimSpace(assertion.Role))
	if role == "" {
		role = config.DefaultRole
	}
	if role != "admin" && role != "developer" && role != "viewer" {
		return control.User{}, fmt.Errorf("saml assertion role must be admin, developer, or viewer")
	}
	password, err := randomSSOPassword()
	if err != nil {
		return control.User{}, err
	}
	return store.CreateUser(ctx, control.CreateUserRequest{Email: email, Password: password, Role: role})
}

func randomSSOPassword() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "sso-" + base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}
