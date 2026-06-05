package control

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const encryptedPayloadPrefix = "supadupa:v1:aesgcm:"
const vaultFileEncryptedPayloadPrefix = "supadupa:v1:vault-file:aesgcm:"
const commandEncryptedPayloadPrefix = "supadupa:v1:kms-command:"

type persistentPayloadCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(payload []byte) ([]byte, error)
	Name() string
}

type persistentEncryptionRouter struct {
	active  persistentPayloadCipher
	local   persistentPayloadCipher
	vault   persistentPayloadCipher
	command persistentPayloadCipher
}

func DefaultPersistentEncryption() (persistentPayloadCipher, error) {
	return PersistentEncryptionFromEnv(os.Getenv)
}

func PersistentEncryptionFromEnv(getenv func(string) string) (persistentPayloadCipher, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	local := newLocalPersistentCipher(getenv("SUPADUPA_SECRET_KEY"))
	vaultPath := strings.TrimSpace(getenv("SUPADUPA_VAULT_KEY_FILE"))
	commandValue := strings.TrimSpace(getenv("SUPADUPA_KMS_COMMAND"))
	provider := strings.ToLower(strings.TrimSpace(getenv("SUPADUPA_KMS_PROVIDER")))

	router := &persistentEncryptionRouter{local: local, active: local}
	if vaultPath != "" {
		vault, err := newVaultFilePersistentCipher(vaultPath)
		if err != nil {
			return nil, err
		}
		router.vault = vault
		if provider == "" {
			router.active = vault
		}
	}
	if commandValue != "" {
		command := commandPersistentCipher{command: commandValue}
		router.command = command
		if provider == "" {
			router.active = command
		}
	}

	switch provider {
	case "", "local", "env", "secret-key":
		if provider != "" {
			router.active = local
		}
	case "vault", "vault-file", "file":
		if router.vault == nil {
			return nil, fmt.Errorf("SUPADUPA_VAULT_KEY_FILE is required for %s persistent encryption", provider)
		}
		router.active = router.vault
	case "kms", "command", "kms-command":
		if router.command == nil {
			return nil, fmt.Errorf("SUPADUPA_KMS_COMMAND is required for %s persistent encryption", provider)
		}
		router.active = router.command
	default:
		return nil, fmt.Errorf("unknown SUPADUPA_KMS_PROVIDER %q", provider)
	}
	return router, nil
}

func (r *persistentEncryptionRouter) Name() string {
	return r.active.Name()
}

func (r *persistentEncryptionRouter) Encrypt(plaintext []byte) ([]byte, error) {
	return r.active.Encrypt(plaintext)
}

func (r *persistentEncryptionRouter) Decrypt(payload []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(payload, []byte(encryptedPayloadPrefix)):
		return r.local.Decrypt(payload)
	case bytes.HasPrefix(payload, []byte(vaultFileEncryptedPayloadPrefix)):
		if r.vault == nil {
			return nil, fmt.Errorf("SUPADUPA_VAULT_KEY_FILE is required to decrypt vault-file persistent payload")
		}
		return r.vault.Decrypt(payload)
	case bytes.HasPrefix(payload, []byte(commandEncryptedPayloadPrefix)):
		if r.command == nil {
			return nil, fmt.Errorf("SUPADUPA_KMS_COMMAND is required to decrypt command persistent payload")
		}
		return r.command.Decrypt(payload)
	default:
		return payload, nil
	}
}

type aesGCMPersistentCipher struct {
	name   string
	prefix string
	key    []byte
}

func newLocalPersistentCipher(secret string) aesGCMPersistentCipher {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		secret = "supadupa-local-development-secret-key"
	}
	sum := sha256.Sum256([]byte(secret))
	return aesGCMPersistentCipher{name: "local", prefix: encryptedPayloadPrefix, key: sum[:]}
}

func newVaultFilePersistentCipher(path string) (aesGCMPersistentCipher, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return aesGCMPersistentCipher{}, fmt.Errorf("read vault key file: %w", err)
	}
	material := strings.TrimSpace(string(payload))
	if material == "" {
		return aesGCMPersistentCipher{}, fmt.Errorf("vault key file is empty")
	}
	sum := sha256.Sum256([]byte(material))
	return aesGCMPersistentCipher{name: "vault-file", prefix: vaultFileEncryptedPayloadPrefix, key: sum[:]}, nil
}

func (c aesGCMPersistentCipher) Name() string {
	return c.name
}

func (c aesGCMPersistentCipher) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	payload := make([]byte, 0, len(c.prefix)+len(nonce)+len(ciphertext))
	payload = append(payload, []byte(c.prefix)...)
	payload = append(payload, nonce...)
	payload = append(payload, ciphertext...)
	return payload, nil
}

func (c aesGCMPersistentCipher) Decrypt(payload []byte) ([]byte, error) {
	prefix := []byte(c.prefix)
	if !bytes.HasPrefix(payload, prefix) {
		return payload, nil
	}
	encrypted := payload[len(prefix):]
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(encrypted) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted payload is shorter than nonce")
	}
	nonce := encrypted[:gcm.NonceSize()]
	ciphertext := encrypted[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

type commandPersistentCipher struct {
	command string
}

func (c commandPersistentCipher) Name() string {
	return "kms-command"
}

func (c commandPersistentCipher) Encrypt(plaintext []byte) ([]byte, error) {
	ciphertext, err := c.run("encrypt", plaintext)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, len(commandEncryptedPayloadPrefix)+len(ciphertext))
	payload = append(payload, []byte(commandEncryptedPayloadPrefix)...)
	payload = append(payload, ciphertext...)
	return payload, nil
}

func (c commandPersistentCipher) Decrypt(payload []byte) ([]byte, error) {
	prefix := []byte(commandEncryptedPayloadPrefix)
	if !bytes.HasPrefix(payload, prefix) {
		return payload, nil
	}
	return c.run("decrypt", payload[len(prefix):])
}

func (c commandPersistentCipher) run(operation string, input []byte) ([]byte, error) {
	parts := strings.Fields(c.command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("persistent KMS command is empty")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), "SUPADUPA_KMS_OPERATION="+operation)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("persistent KMS %s command failed: %w: %s", operation, err, message)
		}
		return nil, fmt.Errorf("persistent KMS %s command failed: %w", operation, err)
	}
	return output, nil
}

func encryptPersistentPayload(plaintext []byte) ([]byte, error) {
	encryption, err := DefaultPersistentEncryption()
	if err != nil {
		return nil, err
	}
	return encryption.Encrypt(plaintext)
}

func decryptPersistentPayload(payload []byte) ([]byte, error) {
	encryption, err := DefaultPersistentEncryption()
	if err != nil {
		return nil, err
	}
	return encryption.Decrypt(payload)
}
