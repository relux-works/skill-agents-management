package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func buildCodexPlanForMode(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("register Codex: %v", err)
	}
	return agentic.BuildPlan(registry, req, mode)
}

func mustSnapshot(t *testing.T, req agentic.LaunchRequest) *ProviderSnapshot {
	t.Helper()
	snapshot, err := ReadProviderSnapshot(req)
	if err != nil {
		t.Fatalf("ReadProviderSnapshot: %v", err)
	}
	if snapshot == nil || !snapshot.valid() {
		t.Fatalf("ReadProviderSnapshot returned an invalid snapshot: %#v", snapshot)
	}
	return snapshot
}

func withSnapshot(req agentic.LaunchRequest, snapshot *ProviderSnapshot) agentic.LaunchRequest {
	req.LocalProvider = &agentic.LocalProviderBinding{ID: req.LocalProvider.ID, Snapshot: snapshot}
	return req
}

// TestSnapshotArgvMatchesTheIDPathByteForByte pins the snapshot contract's
// first half: on unchanged config, the snapshot argv is the ID-only argv,
// through the production BuildPlan entry point for both Exec and DryRun.
func TestSnapshotArgvMatchesTheIDPathByteForByte(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			content := writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := providerRequest(t, home, workDir, "local-story")

			snapshot := mustSnapshot(t, req)
			sum := sha256.Sum256([]byte(content))
			if want := hex.EncodeToString(sum[:]); snapshot.Digest() != want {
				t.Fatalf("snapshot digest = %q, want the config bytes' SHA-256 %q", snapshot.Digest(), want)
			}
			if snapshot.ProviderID() != "local-story" {
				t.Fatalf("snapshot provider id = %q, want %q", snapshot.ProviderID(), "local-story")
			}

			idPlan, err := buildCodexPlanForMode(t, req, mode)
			if err != nil {
				t.Fatalf("BuildPlan(ID path): %v", err)
			}
			snapshotPlan, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
			if err != nil {
				t.Fatalf("BuildPlan(snapshot path): %v", err)
			}
			if !reflect.DeepEqual(snapshotPlan.Argv, idPlan.Argv) {
				t.Fatalf("snapshot argv %#v differs from the ID-only argv %#v", snapshotPlan.Argv, idPlan.Argv)
			}
		})
	}
}

// TestSnapshotPinsThePlannedProviderAcrossAConfigMutation is the A/A/B/A
// regression: the consumer snapshots A, plans A, the config mutates to B,
// and the snapshot launch still carries A (argv and digest) while the ID-only
// path observes B, proving the mutation took effect.
func TestSnapshotPinsThePlannedProviderAcrossAConfigMutation(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := providerRequest(t, home, workDir, "local-story")

			snapshot := mustSnapshot(t, req)
			digestA := snapshot.Digest()
			planned, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
			if err != nil {
				t.Fatalf("BuildPlan(snapshot, before mutation): %v", err)
			}
			if !containsPair(planned.Argv, "-c", `model_providers.local-story.base_url="http://127.0.0.1:38171/v1"`) {
				t.Fatalf("planned argv does not carry A: %q", planned.Argv)
			}

			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38172/v1", "responses", false)

			launched, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
			if err != nil {
				t.Fatalf("BuildPlan(snapshot, after mutation): %v", err)
			}
			if !reflect.DeepEqual(launched.Argv, planned.Argv) {
				t.Fatalf("snapshot argv changed across the mutation:\n before %#v\n after  %#v", planned.Argv, launched.Argv)
			}
			if snapshot.Digest() != digestA {
				t.Fatalf("snapshot digest changed across the mutation: %q vs %q", snapshot.Digest(), digestA)
			}

			idPlan, err := buildCodexPlanForMode(t, req, mode)
			if err != nil {
				t.Fatalf("BuildPlan(ID path, after mutation): %v", err)
			}
			if !containsPair(idPlan.Argv, "-c", `model_providers.local-story.base_url="http://127.0.0.1:38172/v1"`) {
				t.Fatalf("ID-only argv does not carry B after the mutation (the mutation did not take effect): %q", idPlan.Argv)
			}
		})
	}
}

// TestSnapshotPathPerformsZeroConfigReads deletes the config after the
// snapshot: the snapshot plan still succeeds on the pinned entry, while the
// ID-only control refuses absent, proving the snapshot branch read nothing.
func TestSnapshotPathPerformsZeroConfigReads(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := providerRequest(t, home, workDir, "local-story")
			snapshot := mustSnapshot(t, req)

			if err := os.Remove(filepath.Join(home, "config.toml")); err != nil {
				t.Fatalf("remove config after snapshot: %v", err)
			}

			plan, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
			if err != nil {
				t.Fatalf("BuildPlan(snapshot, config deleted) = %v; the snapshot path must not read config.toml", err)
			}
			if !containsPair(plan.Argv, "-c", `model_providers.local-story.base_url="http://127.0.0.1:38171/v1"`) {
				t.Fatalf("snapshot argv after deletion does not carry the pinned entry: %q", plan.Argv)
			}

			if _, err := buildCodexPlanForMode(t, req, mode); !errors.Is(err, agentic.ErrLocalProviderAbsent) {
				t.Fatalf("BuildPlan(ID path, config deleted) = %v, want %v (the control must prove the deletion took effect)", err, agentic.ErrLocalProviderAbsent)
			}
		})
	}
}

// TestReadProviderSnapshotRefusalParity holds the constructor to the ID-only
// plan path's typed refusals: every config failure that refuses a plan must
// refuse the snapshot with the same kind and the same errors.Is identity.
func TestReadProviderSnapshotRefusalParity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		providerID string
		kind       agentic.LocalProviderRefusalKind
		want       error
		configure  func(t *testing.T, req *agentic.LaunchRequest)
	}{
		{name: "absent_private_config", kind: agentic.LocalProviderAbsent, want: agentic.ErrLocalProviderAbsent},
		{
			name: "private_config_read_failure_is_not_absence",
			kind: agentic.LocalProviderReadFailed,
			want: agentic.ErrLocalProviderReadFailed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(req.Home, "config.toml"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed_private_config",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(req.Home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "private_provider_table_missing",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"local-story\"\n")
			},
		},
		{
			name: "provider_not_bound_in_private_home",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-other", "http://127.0.0.1:38171/v1", "responses", false)
			},
		},
		{
			name: "malformed_provider_id_whitespace",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req.LocalProvider = &agentic.LocalProviderBinding{ID: " local-story"}
			},
		},
		{
			name:       "malformed_dotted_provider_id",
			providerID: "local-a.b",
			kind:       agentic.LocalProviderMalformed,
			want:       agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"local-a.b\"\n")
			},
		},
		{
			name: "builtin_provider_id_is_not_a_local_binding",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "openai", "http://127.0.0.1:38171/v1", "responses", false)
				req.LocalProvider = &agentic.LocalProviderBinding{ID: "openai"}
			},
		},
		{
			name: "malformed_base_url_whitespace",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1 ", "responses", false)
			},
		},
		{
			name: "wire_api_is_required",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nrequires_openai_auth = false\n")
			},
		},
		{
			name: "unsupported_chat_completions_transport",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "chat_completions", false)
			},
		},
		{
			name: "account_auth_required_by_local_binding",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", true)
			},
		},
		{
			name: "credential_env_key_is_refused",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"openai\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nenv_key = \"OPENAI_API_KEY\"\n")
			},
		},
		{
			name: "non_loopback_endpoint",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://192.0.2.17/v1", "responses", false)
			},
		},
		{
			name: "https_endpoint_is_unsupported",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "https://127.0.0.1:38171/v1", "responses", false)
			},
		},
		{
			name: "conflicting_codex_home_binding",
			kind: agentic.LocalProviderConflicting,
			want: agentic.ErrLocalProviderConflicting,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req.Env = append(req.Env, "CODEX_HOME="+t.TempDir())
			},
		},
		{
			name: "provider_home_cannot_be_resolved",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				req.Home = "~"
				req.Env = filterEnvKeys(req.Env, "HOME")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			providerID := test.providerID
			if providerID == "" {
				providerID = "local-story"
			}
			req := providerRequest(t, home, workDir, providerID)
			if test.configure != nil {
				test.configure(t, &req)
			}
			snapshot, constructorErr := ReadProviderSnapshot(req)
			if snapshot != nil {
				t.Fatalf("ReadProviderSnapshot returned a snapshot on a refused config: %#v", snapshot)
			}
			if !errors.Is(constructorErr, test.want) {
				t.Fatalf("ReadProviderSnapshot error = %v, want %v", constructorErr, test.want)
			}
			var constructorRefusal *agentic.LocalProviderRefusal
			if !errors.As(constructorErr, &constructorRefusal) || constructorRefusal.Kind != test.kind {
				t.Fatalf("ReadProviderSnapshot error = %v, want typed refusal kind %q", constructorErr, test.kind)
			}
			_, planErr := buildCodexPlan(t, req)
			if !errors.Is(planErr, test.want) {
				t.Fatalf("BuildPlan error = %v, want %v (constructor/plan parity)", planErr, test.want)
			}
			var planRefusal *agentic.LocalProviderRefusal
			if !errors.As(planErr, &planRefusal) || planRefusal.Kind != test.kind {
				t.Fatalf("BuildPlan error = %v, want typed refusal kind %q (constructor/plan parity)", planErr, test.kind)
			}
		})
	}
}

// TestReadProviderSnapshotRefusesAnEmptyBinding holds the constructor's empty-ID
// refusal to the plan path's: both refuse unbound before any file read.
func TestReadProviderSnapshotRefusesAnEmptyBinding(t *testing.T) {
	t.Parallel()
	req := providerRequest(t, t.TempDir(), t.TempDir(), "")
	if snapshot, err := ReadProviderSnapshot(req); snapshot != nil || !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("ReadProviderSnapshot(empty ID) = %#v, %v; want nil, %v", snapshot, err, agentic.ErrLocalProviderUnbound)
	}
	nilReq := providerRequest(t, t.TempDir(), t.TempDir(), "local-story")
	nilReq.LocalProvider = nil
	if snapshot, err := ReadProviderSnapshot(nilReq); snapshot != nil || !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("ReadProviderSnapshot(nil binding) = %#v, %v; want nil, %v", snapshot, err, agentic.ErrLocalProviderUnbound)
	}
}

// TestSnapshotBindingRefusesForgedSnapshots proves no unvalidated literal
// construction reaches a launch: a foreign value, a zero snapshot, a typed
// nil, and an ID-mismatched snapshot are all refused through BuildPlan.
func TestSnapshotBindingRefusesForgedSnapshots(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	writeProviderConfig(t, home, "local-other", "http://127.0.0.1:38172/v1", "responses", false)
	base := providerRequest(t, home, workDir, "local-story")
	other := providerRequest(t, home, workDir, "local-other")
	otherSnapshot := mustSnapshot(t, other)

	var typedNil *ProviderSnapshot
	tests := []struct {
		name     string
		snapshot any
		kind     agentic.LocalProviderRefusalKind
		want     error
	}{
		{name: "foreign_string_value", snapshot: "local-story", kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "foreign_int_value", snapshot: 42, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "zero_snapshot_literal", snapshot: &ProviderSnapshot{}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "typed_nil_snapshot", snapshot: typedNil, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "snapshot_bound_to_another_id", snapshot: otherSnapshot, kind: agentic.LocalProviderConflicting, want: agentic.ErrLocalProviderConflicting},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := base
			req.LocalProvider = &agentic.LocalProviderBinding{ID: "local-story", Snapshot: test.snapshot}
			plan, err := buildCodexPlan(t, req)
			if !errors.Is(err, test.want) {
				t.Fatalf("BuildPlan error = %v, want %v", err, test.want)
			}
			var refusal *agentic.LocalProviderRefusal
			if !errors.As(err, &refusal) || refusal.Kind != test.kind {
				t.Fatalf("BuildPlan error = %v, want typed refusal kind %q", err, test.kind)
			}
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("refused launch returned a plan: %#v", plan)
			}
		})
	}
}

// TestManagedSessionRefusesLocalProviderWithoutReadingConfig holds the managed
// argv's refusal for both paths: the mode is unsupported with a provider
// binding, snapshot or not, and the refusal fires without a config read —
// deleting the config still refuses unsupported, never absent.
func TestManagedSessionRefusesLocalProviderWithoutReadingConfig(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := providerRequest(t, home, workDir, "local-story")
	snapshot := mustSnapshot(t, req)

	for _, binding := range []struct {
		name string
		req  agentic.LaunchRequest
	}{
		{name: "id_path", req: req},
		{name: "snapshot_path", req: withSnapshot(req, snapshot)},
	} {
		t.Run(binding.name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildCodexPlanForMode(t, binding.req, agentic.LaunchModeManagedSession); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
				t.Fatalf("BuildPlan(managed-session) = %v, want %v", err, agentic.ErrLocalProviderUnsupported)
			}
		})
	}

	if err := os.Remove(filepath.Join(home, "config.toml")); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	for _, binding := range []struct {
		name string
		req  agentic.LaunchRequest
	}{
		{name: "id_path_without_config", req: req},
		{name: "snapshot_path_without_config", req: withSnapshot(req, snapshot)},
	} {
		t.Run(binding.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildCodexPlanForMode(t, binding.req, agentic.LaunchModeManagedSession)
			if !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
				t.Fatalf("BuildPlan(managed-session, config deleted) = %v, want %v (a read would refuse absent instead)", err, agentic.ErrLocalProviderUnsupported)
			}
		})
	}
}

// TestProviderArgvSpellingIsFrozen pins the absolute bytes of the shared -c
// spelling that the ID-only literal moved into verbatim. The snapshot/ID
// agreement test pins the two paths to each other but not to the original
// bytes; this test freezes all five pairs, including the quoting, through
// both the shared spelling and the production BuildPlan entry point.
func TestProviderArgvSpellingIsFrozen(t *testing.T) {
	t.Parallel()
	want := []string{
		"-c", `model_provider="local-story"`,
		"-c", `model_providers.local-story.name="task local provider"`,
		"-c", `model_providers.local-story.base_url="http://127.0.0.1:38171/v1"`,
		"-c", `model_providers.local-story.wire_api="responses"`,
		"-c", `model_providers.local-story.requires_openai_auth=false`,
	}
	if got := providerArgv("local-story", privateProvider{Name: "task local provider", BaseURL: "http://127.0.0.1:38171/v1"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("providerArgv = %#v, want %#v", got, want)
	}
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, providerRequest(t, home, workDir, "local-story"))
	if err != nil {
		t.Fatalf("BuildPlan(ID path): %v", err)
	}
	for i := 0; i < len(want); i += 2 {
		if !containsPair(plan.Argv, want[i], want[i+1]) {
			t.Fatalf("ID-only argv %q misses pair %q %q", plan.Argv, want[i], want[i+1])
		}
	}
}

// TestReadProviderSnapshotReadsFreshBytes proves the constructor performs its
// read on every call: re-snapshotting after a rotation yields a new digest,
// and a carried snapshot on the request is ignored rather than returned.
func TestReadProviderSnapshotReadsFreshBytes(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := providerRequest(t, home, workDir, "local-story")
	first := mustSnapshot(t, req)

	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38172/v1", "responses", false)
	second := mustSnapshot(t, req)
	if first.Digest() == second.Digest() {
		t.Fatalf("re-snapshot after rotation kept digest %q; the constructor must read fresh bytes", first.Digest())
	}
	stale := withSnapshot(req, first)
	third := mustSnapshot(t, stale)
	if third.Digest() != second.Digest() {
		t.Fatalf("constructor with a carried snapshot returned digest %q, want the fresh %q", third.Digest(), second.Digest())
	}

	forged := req
	forged.LocalProvider = &agentic.LocalProviderBinding{ID: "local-story", Snapshot: "not a snapshot"}
	if _, err := ReadProviderSnapshot(forged); err != nil {
		t.Fatalf("ReadProviderSnapshot with a forged carried snapshot = %v; the constructor must ignore the carried value and read fresh", err)
	}
}

// TestTomlDecodeShapePinsTheParseGateOperands pins the two library premises
// that make M-snapshot.go-L107-O1 and M-snapshot.go-L107-O2 equivalent
// mutants: go-toml parses the whole document before decoding into the map,
// so every decode error leaves the target nil (the err != nil disjunct is
// subsumed by config == nil), and every successful decode into a map
// allocates (even for empty input), so err == nil with a nil map is
// unreachable (the config == nil disjunct guards no reachable state). A
// library behavior change fails here instead of silently reviving either
// mutant. The census in tools/codex-provider-mutants carries the same
// justification next to the counts.
func TestTomlDecodeShapePinsTheParseGateOperands(t *testing.T) {
	t.Parallel()
	valid := "model_provider = \"openai\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n"
	t.Run("decode_error_leaves_a_nil_map", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{
			"[broken\n",
			valid + "[broken\n",
			valid + "[model_providers.local-story]\nname = \"second\"\n",
		} {
			var config map[string]any
			if err := toml.Unmarshal([]byte(body), &config); err == nil {
				t.Fatalf("Unmarshal(%q) succeeded; the L107-O1 equivalence premise needs an error here", body)
			} else if config != nil {
				t.Fatalf("Unmarshal(%q) errored yet left %#v; L107-O1 (err != nil dropped) would no longer be subsumed by config == nil", body, config)
			}
		}
	})
	t.Run("successful_decode_always_allocates", func(t *testing.T) {
		t.Parallel()
		bodies := [][]byte{nil, {}, []byte("  \n\t\n"), []byte("# just a comment\n"), []byte(valid)}
		for _, body := range bodies {
			var config map[string]any
			if err := toml.Unmarshal(body, &config); err != nil {
				t.Fatalf("Unmarshal(%q) = %v; the L107-O2 equivalence premise needs success here", body, err)
			} else if config == nil {
				t.Fatalf("Unmarshal(%q) succeeded with a nil map; L107-O2 (config == nil dropped) would no longer be equivalent", body)
			}
		}
	})
}
