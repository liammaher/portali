package api

import (
	"os"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/crypto"
)

// Hundreds of test vaults: full-cost key derivation would take minutes.
func TestMain(m *testing.M) {
	crypto.UseFastKDFForTests()
	os.Exit(m.Run())
}
