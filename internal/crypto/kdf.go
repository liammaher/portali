package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"golang.org/x/crypto/argon2"
)

const (
	argonThreads = 1  // sequential: deterministic performance across machines
	keyLen       = 32 // 256-bit
	saltLen      = 32
	secretKeyLen = 16 // 128-bit
)

// The Argon2id cost. Only UseFastKDFForTests changes it.
var (
	argonTime   uint32 = 3
	argonMemory uint32 = 64 * 1024 // 64 MB
)

// UseFastKDFForTests makes key derivation cheap for the rest of this test
// binary. Packages whose tests create hundreds of vaults call it from
// TestMain: at full cost each derivation takes about a second under -race.
// Vaults made this way only unlock with the same setting. It panics outside
// a test binary, so a release build can never derive weak keys.
func UseFastKDFForTests() {
	if !testing.Testing() {
		panic("crypto: UseFastKDFForTests outside a test binary")
	}
	argonTime, argonMemory = 1, 1024 // 1 MB
}

// DeriveVaultKey derives a 256-bit key from password + secret key + salt
// using Argon2id.
func DeriveVaultKey(password, secretKey, salt []byte) []byte {
	combined := make([]byte, len(password)+len(secretKey))
	copy(combined, password)
	copy(combined[len(password):], secretKey)
	key := argon2.IDKey(combined, salt, argonTime, argonMemory, argonThreads, keyLen)
	clear(combined)
	return key
}

// GenerateSalt returns 32 bytes of cryptographically secure random data.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// GenerateSecretKey returns 16 bytes (128-bit) of cryptographically secure random data.
func GenerateSecretKey() ([]byte, error) {
	key := make([]byte, secretKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// HashSecretKey returns SHA-256 of the secret key for storage verification.
func HashSecretKey(secretKey []byte) []byte {
	h := sha256.Sum256(secretKey)
	return h[:]
}
