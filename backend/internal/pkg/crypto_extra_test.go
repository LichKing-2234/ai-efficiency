package pkg

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Encrypt – cover error paths
// ---------------------------------------------------------------------------

func TestEncryptEmptyKey(t *testing.T) {
	// Empty hex key → 0 bytes → "key must be 32 bytes, got 0"
	_, err := Encrypt("test", "")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !strings.Contains(err.Error(), "key must be 32 bytes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEncryptKeyExactly16Bytes(t *testing.T) {
	// 16 bytes = 32 hex chars — should fail (need 32 bytes)
	key := hex.EncodeToString(make([]byte, 16))
	_, err := Encrypt("test", key)
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if !strings.Contains(err.Error(), "key must be 32 bytes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptKeyExactly16Bytes(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 16))
	_, err := Decrypt("aabbccdd", key)
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if !strings.Contains(err.Error(), "key must be 32 bytes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptCiphertextExactlyNonceSize(t *testing.T) {
	// GCM nonce is 12 bytes. Ciphertext of exactly 12 bytes → no actual data → decrypt fails.
	key := hex.EncodeToString(make([]byte, 32))
	// 12 bytes = 24 hex chars
	ciphertext := hex.EncodeToString(make([]byte, 12))
	_, err := Decrypt(ciphertext, key)
	if err == nil {
		t.Fatal("expected error for ciphertext with only nonce and no data")
	}
}

func TestDecryptCiphertextShorterThanNonce(t *testing.T) {
	// Less than 12 bytes → "ciphertext too short"
	key := hex.EncodeToString(make([]byte, 32))
	ciphertext := hex.EncodeToString(make([]byte, 5))
	_, err := Decrypt(ciphertext, key)
	if err == nil {
		t.Fatal("expected error for ciphertext shorter than nonce")
	}
	if !strings.Contains(err.Error(), "ciphertext too short") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEncryptDecryptSpecialCharacters(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	specials := []string{
		"",
		"\x00\x01\x02",
		"line1\nline2\ttab",
		strings.Repeat("a", 1000),
	}
	for _, s := range specials {
		ct, err := Encrypt(s, key)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", s, err)
		}
		pt, err := Decrypt(ct, key)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if pt != s {
			t.Errorf("round-trip failed for %q", s)
		}
	}
}

func TestDecryptOddLengthHexCiphertext(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	// Odd-length hex string → hex.DecodeString fails
	_, err := Decrypt("abc", key)
	if err == nil {
		t.Fatal("expected error for odd-length hex ciphertext")
	}
	if !strings.Contains(err.Error(), "decode ciphertext") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptOddLengthHexKey(t *testing.T) {
	// Odd-length hex key → hex.DecodeString fails
	_, err := Decrypt("aabbccdd", "abc")
	if err == nil {
		t.Fatal("expected error for odd-length hex key")
	}
	if !strings.Contains(err.Error(), "decode key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEncryptOddLengthHexKey(t *testing.T) {
	_, err := Encrypt("test", "abc")
	if err == nil {
		t.Fatal("expected error for odd-length hex key")
	}
	if !strings.Contains(err.Error(), "decode key") {
		t.Errorf("unexpected error: %v", err)
	}
}

// failReader is an io.Reader that always returns an error.
type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("injected rand failure")
}

func TestEncryptNonceGenerationError(t *testing.T) {
	// Temporarily replace crypto/rand.Reader to force io.ReadFull to fail
	origReader := rand.Reader
	rand.Reader = failReader{}
	defer func() { rand.Reader = origReader }()

	key := hex.EncodeToString(make([]byte, 32))
	_, err := Encrypt("test", key)
	if err == nil {
		t.Fatal("expected error when rand.Reader fails")
	}
	if !strings.Contains(err.Error(), "generate nonce") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Ensure rand.Reader replacement doesn't break other tests by verifying
// a normal encrypt/decrypt still works after the deferred restore.
func TestEncryptDecryptAfterReaderRestore(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	ct, err := Encrypt("verify restore", key)
	if err != nil {
		t.Fatalf("Encrypt after restore: %v", err)
	}
	pt, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("Decrypt after restore: %v", err)
	}
	if pt != "verify restore" {
		t.Errorf("got %q, want %q", pt, "verify restore")
	}
}
