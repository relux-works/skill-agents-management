//go:build unix

package muse

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// shortPipeHolderVersionBody returns --version shell whose background
// holder provably outlives the leader: the holder blocks reading a FIFO
// the leader holds open, so its read can end only after the leader's
// exit closes that end. The leader's open is the readiness handshake —
// it completes only once the holder has opened the read end — and the
// leader's exit is the controlled release. The holder inherits the
// probe's stdout pipe and never touches it, so the probe copy genuinely
// blocks past the parent exit and drains through this EOF, in
// milliseconds and without any sleep, signal, or group kill.
func shortPipeHolderVersionBody(t *testing.T) string {
	t.Helper()
	lifetime := filepath.Join(t.TempDir(), "leader-lifetime")
	if err := syscall.Mkfifo(lifetime, 0o600); err != nil {
		t.Fatalf("creating the holder lifetime FIFO: %v", err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	return "(IFS= read -r holder_hold < " + quote(lifetime) + ") &\n" +
		"exec 9>" + quote(lifetime) + "\n" +
		"printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\nexit 0\n"
}
