package control

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	secret []byte
}

const (
	AuthSecretEnv        = "SUPADUPA_AUTH_SECRET"
	PlatformSecretKeyEnv = "SUPADUPA_SECRET_KEY"
	minUserPasswordBytes = 12
)

type TokenClaims struct {
	Subject      string `json:"sub"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	TokenVersion int64  `json:"ver,omitempty"`
	Expires      int64  `json:"exp"`
	Audience     string `json:"aud,omitempty"`
	ProjectRef   string `json:"project_ref,omitempty"`
}

func NewAuthService(secret string) *AuthService {
	if secret == "" {
		secret = "dev-only-change-me"
	}
	return &AuthService{secret: []byte(secret)}
}

func AuthSecretFromEnv(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	if secret := strings.TrimSpace(getenv(AuthSecretEnv)); secret != "" {
		return secret
	}
	return strings.TrimSpace(getenv(PlatformSecretKeyEnv))
}

func (s *AuthService) Issue(user User, ttl time.Duration) (string, error) {
	claims := TokenClaims{
		Subject:      user.ID,
		Email:        user.Email,
		Role:         user.Role,
		TokenVersion: nextTokenVersion(user.TokenVersion - 1),
		Expires:      time.Now().Add(ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.sign(body), nil
}

func (s *AuthService) Verify(token string) (TokenClaims, error) {
	body, signature, ok := strings.Cut(token, ".")
	if !ok || body == "" || signature == "" {
		return TokenClaims{}, fmt.Errorf("invalid token")
	}
	if !hmac.Equal([]byte(signature), []byte(s.sign(body))) {
		return TokenClaims{}, fmt.Errorf("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return TokenClaims{}, err
	}
	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, err
	}
	if time.Now().Unix() > claims.Expires {
		return TokenClaims{}, fmt.Errorf("token expired")
	}
	return claims, nil
}

func (s *AuthService) sign(body string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashPassword(password string) string {
	digest := hex.EncodeToString(hashBytes([]byte(password)))
	hashed, err := bcrypt.GenerateFromPassword([]byte(digest), bcrypt.DefaultCost)
	if err == nil {
		return "bcrypt-sha256$" + string(hashed)
	}
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "sha256$" + hex.EncodeToString(hashBytes([]byte(password)))
	}
	saltHex := hex.EncodeToString(salt[:])
	return "sha256$" + saltHex + "$" + hex.EncodeToString(hashBytes([]byte(saltHex+password)))
}

func validateUserPassword(password string, required bool) error {
	if password == "" {
		if required {
			return fmt.Errorf("password is required")
		}
		return nil
	}
	if strings.TrimSpace(password) != password {
		return fmt.Errorf("password must not start or end with whitespace")
	}
	if strings.ContainsAny(password, "\r\n\t") {
		return fmt.Errorf("password must not contain control whitespace")
	}
	if len(password) < minUserPasswordBytes {
		return fmt.Errorf("password must be at least %d characters", minUserPasswordBytes)
	}
	normalized := strings.ToLower(password)
	if commonUserPasswords[normalized] {
		return fmt.Errorf("password is too common")
	}
	return nil
}

var commonUserPasswords = map[string]bool{
	"adminadmin":         true,
	"adminpassword":      true,
	"changeme":           true,
	"change-me":          true,
	"dev-only-change-me": true,
	"password":           true,
	"password1":          true,
	"password12":         true,
	"password123":        true,
	"password1234":       true,
	"password12345":      true,
	"password123456":     true,
	"supadupa":           true,
	"supadupa-password":  true,
	"supadupa123":        true,
	"supadupa1234":       true,
	"temporary-password": true,
	"test-password":      true,
	"test-password-123":  true,
	"welcome123":         true,
	"welcome1234":        true,
	"your-password-here": true,
	"yourpasswordhere":   true,
}

func verifyPassword(password string, encoded string) bool {
	verified, _ := verifyPasswordWithRehash(password, encoded)
	return verified
}

func verifyPasswordWithRehash(password string, encoded string) (bool, bool) {
	if hash := strings.TrimPrefix(encoded, "bcrypt-sha256$"); hash != encoded {
		digest := hex.EncodeToString(hashBytes([]byte(password)))
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(digest)) == nil, false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) == 2 {
		ok := hmac.Equal([]byte(parts[1]), []byte(hex.EncodeToString(hashBytes([]byte(password)))))
		if ok {
			noteLegacyPasswordHashVerify()
		}
		return ok, true
	}
	if len(parts) != 3 {
		return false, false
	}
	expected := hex.EncodeToString(hashBytes([]byte(parts[1] + password)))
	ok := hmac.Equal([]byte(parts[2]), []byte(expected))
	if ok {
		noteLegacyPasswordHashVerify()
	}
	return ok, true
}

func hashBytes(input []byte) []byte {
	sum := sha256.Sum256(input)
	return sum[:]
}

func GenerateTOTPSecret() (string, error) {
	var secret [20]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret[:]), "="), nil
}

func TOTPCode(secret string, at time.Time) (string, error) {
	return TOTPCodeForCounter(secret, uint64(at.Unix()/30))
}

func TOTPCodeForCounter(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", value%1000000), nil
}

func VerifyTOTPCode(secret string, code string, at time.Time) bool {
	_, ok := VerifyTOTPCodeCounter(secret, code, at)
	return ok
}

func VerifyTOTPCodeCounter(secret string, code string, at time.Time) (uint64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	current := at.Unix() / 30
	for offset := -1; offset <= 1; offset++ {
		counter := current + int64(offset)
		if counter < 0 {
			continue
		}
		expected, err := TOTPCodeForCounter(secret, uint64(counter))
		if err == nil && hmac.Equal([]byte(expected), []byte(code)) {
			return uint64(counter), true
		}
	}
	return 0, false
}

func TOTPAuthURL(issuer string, account string, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	values := url.Values{}
	values.Set("secret", secret)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", "6")
	values.Set("period", "30")
	return "otpauth://totp/" + label + "?" + values.Encode()
}
