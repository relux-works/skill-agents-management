//go:build !unix

package muse

import "testing"

// shortPipeHolderVersionBody needs Unix FIFOs for the provable holder
// lifetime; the gate-backed tests already require Unix the same way.
func shortPipeHolderVersionBody(t *testing.T) string {
	t.Helper()
	t.Skip("the short pipe holder needs Unix FIFOs")
	return ""
}
