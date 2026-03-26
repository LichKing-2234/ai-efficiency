package pkg

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// Generate a random 32-byte key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	key := hex.EncodeToString(keyBytes)

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"json credentials", `{"token":"ghp_abc123","type":"pat"}`},
		{"unicode", "你好世界 🌍"},
		{"long text", string(make([]byte, 10000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := Encrypt(tt.plaintext, key)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			if ciphertext == tt.plaintext {
				t.Error("ciphertext should differ from plaintext")
			}

			decrypted, err := Decrypt(ciphertext, key)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	key := hex.EncodeToString(keyBytes)

	c1, _ := Encrypt("same plaintext", key)
	c2, _ := Encrypt("same plaintext", key)

	if c1 == c2 {
		t.Error("two encryptions of same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1Bytes := make([]byte, 32)
	key2Bytes := make([]byte, 32)
	rand.Read(key1Bytes)
	rand.Read(key2Bytes)
	key1 := hex.EncodeToString(key1Bytes)
	key2 := hex.EncodeToString(key2Bytes)

	ciphertext, err := Encrypt("secret data", key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(ciphertext, key2)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	_, err := Encrypt("test", "not-hex")
	if err == nil {
		t.Error("Encrypt with invalid hex key should fail")
	}

	_, err = Encrypt("test", hex.EncodeToString([]byte("short")))
	if err == nil {
		t.Error("Encrypt with short key should fail")
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	key := hex.EncodeToString(keyBytes)

	_, err := Decrypt("not-hex", key)
	if err == nil {
		t.Error("Decrypt with invalid hex ciphertext should fail")
	}

	_, err = Decrypt(hex.EncodeToString([]byte("short")), key)
	if err == nil {
		t.Error("Decrypt with too-short ciphertext should fail")
	}
}

func TestDecryptInvalidKey(t *testing.T) {
	_, err := Decrypt("aabbccdd", "not-hex-key!!!")
	if err == nil {
		t.Error("Decrypt with invalid hex key should fail")
	}

	_, err = Decrypt("aabbccdd", hex.EncodeToString([]byte("short")))
	if err == nil {
		t.Error("Decrypt with short key should fail")
	}
}

func TestEncryptDecryptLargePayload(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	key := hex.EncodeToString(keyBytes)

	// 64KB payload
	large := make([]byte, 65536)
	rand.Read(large)
	plaintext := hex.EncodeToString(large)

	ct, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt large: %v", err)
	}
	pt, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("Decrypt large: %v", err)
	}
	if pt != plaintext {
		t.Error("round-trip failed for large payload")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	key := hex.EncodeToString(keyBytes)

	ct, err := Encrypt("secret", key)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the last byte
	ctBytes, _ := hex.DecodeString(ct)
	ctBytes[len(ctBytes)-1] ^= 0xff
	tampered := hex.EncodeToString(ctBytes)

	_, err = Decrypt(tampered, key)
	if err == nil {
		t.Error("Decrypt with tampered ciphertext should fail")
	}
}

func TestEncryptExact32ByteKey(t *testing.T) {
	// Exactly 32 bytes = 64 hex chars
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	ct, err := Encrypt("test", key)
	if err != nil {
		t.Fatalf("Encrypt with exact 32-byte key: %v", err)
	}
	pt, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if pt != "test" {
		t.Errorf("got %q, want %q", pt, "test")
	}
}

func TestEncryptKeyTooLong(t *testing.T) {
	// 33 bytes = 66 hex chars
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef00"
	_, err := Encrypt("test", key)
	if err == nil {
		t.Error("Encrypt with 33-byte key should fail")
	}
}

func TestDecryptKeyTooLong(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef00"
	_, err := Decrypt("aabbccdd", key)
	if err == nil {
		t.Error("Decrypt with 33-byte key should fail")
	}
}

func TestDecryptEmptyCiphertext(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	key := hex.EncodeToString(keyBytes)

	// Empty hex string → empty ciphertext → too short
	_, err := Decrypt("", key)
	if err == nil {
		t.Error("Decrypt with empty ciphertext should fail")
	}
}
