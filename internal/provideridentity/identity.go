// Package provideridentity holds the credential-free identity value shared by
// the reactive limit and advisory quota planes. It performs no filesystem IO.
package provideridentity

import (
	"crypto/sha256"
	"encoding/hex"
)

// Identity identifies one normalized harness configuration home. Home is
// trusted caller evidence, never serialized; HomeDisplay is operator-only.
type Identity struct {
	Key         string `json:"identity"`
	Provider    string `json:"provider"`
	Home        string `json:"-"`
	HomeDisplay string `json:"home_display"`
}

// Key is the unchanged on-disk hash: provider, NUL, normalized path, SHA-256,
// first sixteen hex digits. It never derives from credential contents.
func Key(provider, normalizedHome string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + normalizedHome))
	return hex.EncodeToString(sum[:])[:16]
}
