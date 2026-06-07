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
)

type AuthService struct {
	secret []byte
}

const (
	AuthSecretEnv        = "SUPADUPA_AUTH_SECRET"
	PlatformSecretKeyEnv = "SUPADUPA_SECRET_KEY"
)

type TokenClaims struct {
	Subject    string `json:"sub"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Expires    int64  `json:"exp"`
	Audience   string `json:"aud,omitempty"`
	ProjectRef string `json:"project_ref,omitempty"`
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
		Subject: user.ID,
		Email:   user.Email,
		Role:    user.Role,
		Expires: time.Now().Add(ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.sign(body), nil
}

func (s *AuthService) IssueStudio(claims TokenClaims, projectRef string, ttl time.Duration) (string, error) {
	studioClaims := TokenClaims{
		Subject:    claims.Subject,
		Email:      claims.Email,
		Role:       claims.Role,
		Expires:    time.Now().Add(ttl).Unix(),
		Audience:   "studio",
		ProjectRef: strings.TrimSpace(projectRef),
	}
	payload, err := json.Marshal(studioClaims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + s.sign(body), nil
}

func (s *AuthService) VerifyStudio(token string, projectRef string) (TokenClaims, error) {
	claims, err := s.Verify(token)
	if err != nil {
		return TokenClaims{}, err
	}
	if claims.Audience != "studio" {
		return TokenClaims{}, fmt.Errorf("token is not scoped for studio")
	}
	if !strings.EqualFold(strings.TrimSpace(claims.ProjectRef), strings.TrimSpace(projectRef)) {
		return TokenClaims{}, fmt.Errorf("token is not scoped for project")
	}
	return claims, nil
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
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "sha256$" + hex.EncodeToString(hashBytes([]byte(password)))
	}
	saltHex := hex.EncodeToString(salt[:])
	return "sha256$" + saltHex + "$" + hex.EncodeToString(hashBytes([]byte(saltHex+password)))
}

func verifyPassword(password string, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) == 2 {
		return hmac.Equal([]byte(parts[1]), []byte(hex.EncodeToString(hashBytes([]byte(password)))))
	}
	if len(parts) != 3 {
		return false
	}
	expected := hex.EncodeToString(hashBytes([]byte(parts[1] + password)))
	return hmac.Equal([]byte(parts[2]), []byte(expected))
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
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(at.Unix() / 30)
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
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	for offset := -1; offset <= 1; offset++ {
		expected, err := TOTPCode(secret, at.Add(time.Duration(offset)*30*time.Second))
		if err == nil && hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
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
