//go:build !unix

package muse

import "testing"

// shortPipeHolderProof needs Unix FIFOs for the provable holder lifetime;
// the gate-backed tests already require Unix the same way.
type shortPipeHolderProof struct{}

// newShortPipeHolderProof skips where the proof cannot run. The skip fires
// in the calling test goroutine before any probe runs.
func newShortPipeHolderProof(t *testing.T) *shortPipeHolderProof {
	t.Helper()
	t.Skip("the short pipe holder needs Unix FIFOs")
	return nil
}

func (*shortPipeHolderProof) versionBody() string { return "" }

func (*shortPipeHolderProof) stop() error { return nil }
