package cloudadapter

import (
	"bytes"
	"testing"

	"cyber-deploy-hub/internal/config"
)

func TestAESGCMEncryptorEncryptsPrivateKey(t *testing.T) {
	encryptor, err := NewAESGCMEncryptor(config.CloudConfig{
		KeyEncryptionKey:   "0123456789abcdef0123456789abcdef",
		KeyEncryptionKeyID: "test-key",
	})
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}

	secret, err := encryptor.Encrypt([]byte("private-key"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if secret.KeyID != "test-key" {
		t.Fatalf("KeyID = %q", secret.KeyID)
	}
	if len(secret.Nonce) == 0 {
		t.Fatal("nonce is empty")
	}
	if bytes.Contains(secret.Ciphertext, []byte("private-key")) {
		t.Fatal("ciphertext contains plaintext")
	}
}

func TestAESGCMEncryptorRequiresKey(t *testing.T) {
	encryptor, err := NewAESGCMEncryptor(config.CloudConfig{})
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor: %v", err)
	}
	if err := encryptor.Ready(); err == nil {
		t.Fatal("expected missing key error")
	}
}
