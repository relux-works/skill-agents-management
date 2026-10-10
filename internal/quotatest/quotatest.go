// Package quotatest provides loud fixture helpers for quota contract tests.
// It is never imported by production code and never runs an executable.
package quotatest

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func Context(t *testing.T, runtime string, hasHome bool) providerquota.ParseContext {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	c := providerquota.ParseContext{Runtime: runtime, ReadAt: now, RetrievedAt: now.Add(time.Second)}
	if hasHome {
		path := filepath.Join(home, "harness")
		c.Identity = &providerlimits.Identity{Provider: runtime, Home: path, HomeDisplay: "~/harness", Key: providerlimits.IdentityKey(runtime, path)}
	}
	return c
}
func Fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading quota fixture %s: %v", name, err)
	}
	return b
}
func BinaryEnv(t *testing.T, name string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := execfixture.WriteFile(path, []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
		t.Fatalf("writing inert executable fixture: %v", err)
	}
	return path, []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}
func RPC(t *testing.T, home string, payload []byte) []byte {
	t.Helper()
	init, err := json.Marshal(map[string]any{"id": 1, "result": map[string]any{"codexHome": home, "userAgent": "quota_fixture/0.153.4"}})
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		t.Fatalf("invalid RPC payload fixture: %v", err)
	}
	payload = compact.Bytes()
	out := append(init, '\n')
	out = append(out, []byte(`{"id":2,"result":`)...)
	out = append(out, payload...)
	return append(out, []byte("}\n")...)
}
func Reason(t *testing.T, r providerquota.QuotaRecord, err error, reason string) {
	t.Helper()
	var refusal *providerquota.Refusal
	if !errors.As(err, &refusal) || refusal.Reason != reason || r.State != providerquota.Unavailable || len(r.Windows) != 0 {
		t.Fatalf("got %v, %v; want unavailable/%s with no windows", r, err, reason)
	}
}
