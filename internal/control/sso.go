package control

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func defaultPlatformSSOConfig() PlatformSSOConfig {
	now := time.Now().UTC()
	return PlatformSSOConfig{
		Provider:    "saml",
		DefaultRole: "developer",
		UpdatedAt:   now,
	}
}

func normalizePlatformSSOInput(input PlatformSSOConfigInput) (PlatformSSOConfig, error) {
	config := PlatformSSOConfig{
		Enabled:       input.Enabled,
		Provider:      "saml",
		IDPEntityID:   strings.TrimSpace(input.IDPEntityID),
		SSOURL:        strings.TrimSpace(input.SSOURL),
		Certificate:   strings.TrimSpace(input.Certificate),
		ACSURL:        strings.TrimSpace(input.ACSURL),
		MetadataURL:   strings.TrimSpace(input.MetadataURL),
		EmailDomain:   strings.TrimPrefix(strings.ToLower(strings.TrimSpace(input.EmailDomain)), "@"),
		AutoProvision: input.AutoProvision,
		DefaultRole:   strings.ToLower(strings.TrimSpace(input.DefaultRole)),
		UpdatedAt:     time.Now().UTC(),
	}
	if config.DefaultRole == "" {
		config.DefaultRole = "developer"
	}
	if !validPlatformRole(config.DefaultRole) {
		return PlatformSSOConfig{}, fmt.Errorf("default_role must be admin, developer, or viewer")
	}
	if config.EmailDomain != "" && strings.Contains(config.EmailDomain, "@") {
		return PlatformSSOConfig{}, fmt.Errorf("email_domain must be a domain, not an email address")
	}
	if config.Enabled {
		if config.IDPEntityID == "" {
			return PlatformSSOConfig{}, fmt.Errorf("idp_entity_id is required when platform sso is enabled")
		}
		if config.SSOURL == "" {
			return PlatformSSOConfig{}, fmt.Errorf("sso_url is required when platform sso is enabled")
		}
		if _, err := url.ParseRequestURI(config.SSOURL); err != nil {
			return PlatformSSOConfig{}, fmt.Errorf("sso_url must be a valid URL")
		}
		if config.ACSURL == "" {
			return PlatformSSOConfig{}, fmt.Errorf("acs_url is required when platform sso is enabled")
		}
		if _, err := url.ParseRequestURI(config.ACSURL); err != nil {
			return PlatformSSOConfig{}, fmt.Errorf("acs_url must be a valid URL")
		}
		if config.Certificate == "" {
			return PlatformSSOConfig{}, fmt.Errorf("certificate_pem is required when platform sso is enabled")
		}
		if _, err := parsePlatformSSOCertificate(config.Certificate); err != nil {
			return PlatformSSOConfig{}, err
		}
	}
	if config.MetadataURL != "" {
		if _, err := url.ParseRequestURI(config.MetadataURL); err != nil {
			return PlatformSSOConfig{}, fmt.Errorf("metadata_url must be a valid URL")
		}
	}
	return config, nil
}

func normalizedPlatformSSOConfig(config PlatformSSOConfig) PlatformSSOConfig {
	if config.Provider == "" {
		config.Provider = "saml"
	}
	if config.DefaultRole == "" {
		config.DefaultRole = "developer"
	}
	if config.UpdatedAt.IsZero() {
		config.UpdatedAt = time.Now().UTC()
	}
	config.EmailDomain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(config.EmailDomain)), "@")
	return config
}

func PlatformSSOAssertionSignaturePayload(assertion PlatformSSOAssertion) []byte {
	email := strings.ToLower(strings.TrimSpace(assertion.Email))
	payload := strings.Join([]string{
		strings.TrimSpace(assertion.Issuer),
		strings.TrimSpace(assertion.Audience),
		email,
		strings.TrimSpace(assertion.NameID),
		assertion.NotOnOrAfter.UTC().Format(time.RFC3339),
	}, "\n")
	return []byte(payload)
}

func ValidatePlatformSSOAssertion(config PlatformSSOConfig, assertion PlatformSSOAssertion, now time.Time) error {
	config = normalizedPlatformSSOConfig(config)
	if !config.Enabled {
		return fmt.Errorf("platform sso is disabled")
	}
	if strings.TrimSpace(assertion.Issuer) != config.IDPEntityID {
		return fmt.Errorf("saml issuer does not match configured idp")
	}
	if strings.TrimSpace(assertion.Audience) != config.ACSURL {
		return fmt.Errorf("saml audience does not match configured acs url")
	}
	email := strings.ToLower(strings.TrimSpace(assertion.Email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("saml assertion email is required")
	}
	if config.EmailDomain != "" && !strings.HasSuffix(email, "@"+config.EmailDomain) {
		return fmt.Errorf("saml assertion email is outside the allowed domain")
	}
	if assertion.NameID == "" {
		return fmt.Errorf("saml assertion name_id is required")
	}
	if assertion.NotOnOrAfter.IsZero() || !now.UTC().Before(assertion.NotOnOrAfter.UTC()) {
		return fmt.Errorf("saml assertion is expired")
	}
	signature, err := decodeAssertionSignature(assertion.Signature)
	if err != nil {
		return err
	}
	cert, err := parsePlatformSSOCertificate(config.Certificate)
	if err != nil {
		return err
	}
	if err := cert.CheckSignature(x509.SHA256WithRSA, PlatformSSOAssertionSignaturePayload(assertion), signature); err != nil {
		return fmt.Errorf("saml assertion signature is invalid")
	}
	return nil
}

func PlatformSSOInitiationForConfig(config PlatformSSOConfig) PlatformSSOInitiation {
	config = normalizedPlatformSSOConfig(config)
	return PlatformSSOInitiation{
		Enabled:     config.Enabled,
		Provider:    config.Provider,
		IDPEntityID: config.IDPEntityID,
		LoginURL:    config.SSOURL,
		ACSURL:      config.ACSURL,
		MetadataURL: config.MetadataURL,
		RequestedAt: time.Now().UTC(),
	}
}

func parsePlatformSSOCertificate(certificatePEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(certificatePEM)))
	if block == nil {
		return nil, fmt.Errorf("certificate_pem must contain a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("certificate_pem is invalid: %w", err)
	}
	return cert, nil
}

func decodeAssertionSignature(signature string) ([]byte, error) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil, fmt.Errorf("saml assertion signature is required")
	}
	if decoded, err := base64.StdEncoding.DecodeString(signature); err == nil {
		return decoded, nil
	}
	decoded, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("saml assertion signature must be base64")
	}
	return decoded, nil
}

func validPlatformRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "developer", "viewer":
		return true
	default:
		return false
	}
}
