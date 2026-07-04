package security

import (
	"strings"
	"testing"
)

func TestVaultRoundTrip(t *testing.T) {
	vault := NewVault("a development key that is long enough")
	secret := "provider-token-that-must-not-leak"
	ciphertext, err := vault.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, secret) {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := vault.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != secret {
		t.Fatalf("got %q, want %q", plaintext, secret)
	}
}

func TestAgentTokenFormatAndHash(t *testing.T) {
	token, err := NewAgentToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "air_") {
		t.Fatalf("unexpected token format: %q", token)
	}
	if HashToken(token) == token {
		t.Fatal("token hash must not equal plaintext")
	}
}
