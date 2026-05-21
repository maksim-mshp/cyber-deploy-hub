package cloudadapter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"cyber-deploy-hub/internal/config"
)

type PrivateKeyEncryptor interface {
	Ready() error
	Encrypt(plaintext []byte) (EncryptedSecret, error)
}

type AESGCMEncryptor struct {
	key   []byte
	keyID string
}

func NewAESGCMEncryptor(cfg config.CloudConfig) (*AESGCMEncryptor, error) {
	key, err := parseEncryptionKey(cfg.KeyEncryptionKey)
	if err != nil {
		return nil, err
	}
	return &AESGCMEncryptor{key: key, keyID: cfg.KeyEncryptionKeyID}, nil
}

func (e *AESGCMEncryptor) Ready() error {
	if len(e.key) == 0 {
		return errors.New("CLOUD_KEY_ENCRYPTION_KEY is required")
	}
	return nil
}

func (e *AESGCMEncryptor) Encrypt(plaintext []byte) (EncryptedSecret, error) {
	if err := e.Ready(); err != nil {
		return EncryptedSecret{}, err
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return EncryptedSecret{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedSecret{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedSecret{}, err
	}
	return EncryptedSecret{
		Ciphertext: gcm.Seal(nil, nonce, plaintext, nil),
		Nonce:      nonce,
		KeyID:      e.keyID,
	}, nil
}

func (e *AESGCMEncryptor) Decrypt(secret EncryptedSecret) ([]byte, error) {
	if err := e.Ready(); err != nil {
		return nil, err
	}
	if len(secret.Ciphertext) == 0 {
		return nil, errors.New("ciphertext is empty")
	}
	if len(secret.Nonce) == 0 {
		return nil, errors.New("nonce is empty")
	}
	if secret.KeyID != "" && e.keyID != "" && secret.KeyID != e.keyID {
		return nil, errors.New("private key encryption key id mismatch")
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, secret.Nonce, secret.Ciphertext, nil)
}

func parseEncryptionKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if key, err := base64.StdEncoding.DecodeString(value); err == nil && validAESKeySize(len(key)) {
		return key, nil
	}
	if key, err := hex.DecodeString(value); err == nil && validAESKeySize(len(key)) {
		return key, nil
	}
	raw := []byte(value)
	if validAESKeySize(len(raw)) {
		return raw, nil
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

func validAESKeySize(size int) bool {
	return size == 16 || size == 24 || size == 32
}
