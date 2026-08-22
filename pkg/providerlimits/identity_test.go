package providerlimits

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func TestIdentityKeyIsTheDocumentedHash(t *testing.T) {
	sum := sha256.Sum256([]byte("codex" + "\x00" + "/tmp/codex-home"))
	want := hex.EncodeToString(sum[:])[:16]
	if got := IdentityKey("codex", "/tmp/codex-home"); got != want {
		t.Fatalf("IdentityKey = %q, want %q", got, want)
	}
	if len(want) != 16 {
		t.Fatalf("identity key length = %d, want 16", len(want))
	}
}

// One home path shared by two providers still yields two records.
func TestOneHomeSharedByTwoProvidersYieldsTwoIdentities(t *testing.T) {
	home := t.TempDir()
	codex, err := IdentityFor(ProviderCodex, home)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	claude, err := IdentityFor(ProviderClaude, home)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if codex.Key == claude.Key {
		t.Fatal("two providers sharing one home must not collide")
	}
}

// Two CODEX_HOME values are two identities with independent quota: two state
// files, two identity keys, and a suppression under one never subtracts the group
// under the other.
func TestTwoProviderHomesAreIsolated(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(Options{Layout: LayoutAt(root)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	homeA := filepath.Join(root, "codex-real")
	homeB := filepath.Join(root, "codex-scratch")

	t.Setenv(homeEnvVarFor(t, ProviderCodex), homeA)
	identityA, err := ResolveIdentity(ProviderCodex)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	t.Setenv(homeEnvVarFor(t, ProviderCodex), homeB)
	identityB, err := ResolveIdentity(ProviderCodex)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	if identityA.Key == identityB.Key {
		t.Fatal("two CODEX_HOME values must produce two identity keys")
	}

	class := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You've hit your usage limit.\n")})
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-000080"})
	if !ok {
		t.Fatal("expected a provider-quota observation")
	}
	if err := store.Observe(identityA, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if _, exists := store.GroupRecordFor(identityA, codexPlan); !exists {
		t.Fatal("the suppression must be recorded under the observing identity")
	}
	if _, exists := store.GroupRecordFor(identityB, codexPlan); exists {
		t.Fatal("a suppression on a scratch home must never hide the operator's real one")
	}
	// The claim gate follows the identity too.
	if _, granted, _ := store.ClaimProbe(identityA, codexPlan, Claimant{RunID: "RUN-260802-000081", OwnerKind: OwnerProcess, PID: 1}); granted {
		t.Fatal("the suppressed identity must refuse the claim")
	}
	if _, granted, _ := store.ClaimProbe(identityB, codexPlan, Claimant{RunID: "RUN-260802-000082", OwnerKind: OwnerProcess, PID: 1}); !granted {
		t.Fatal("the other identity's group is untouched and must be admitted")
	}

	// Two state files, one per identity, with the identity key in the path.
	for _, identity := range []Identity{identityA, identityB} {
		path := store.Layout().StateFile(identity.Key)
		if !strings.Contains(path, identity.Key) {
			t.Errorf("state path %q does not carry the identity key", path)
		}
	}
	if _, err := os.Stat(store.Layout().StateFile(identityA.Key)); err != nil {
		t.Fatalf("identity A has no state file: %v", err)
	}
	// Identity B has no file yet, because granting an available group writes
	// nothing. Suppress it too, and the two identities hold two separate files.
	if _, err := os.Stat(store.Layout().StateFile(identityB.Key)); !os.IsNotExist(err) {
		t.Fatalf("identity B has a state file before anything was observed under it: %v", err)
	}
	if err := store.Observe(identityB, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, err := os.Stat(store.Layout().StateFile(identityB.Key)); err != nil {
		t.Fatalf("identity B has no state file: %v", err)
	}
	stateA, err := os.ReadFile(store.Layout().StateFile(identityA.Key))
	if err != nil {
		t.Fatal(err)
	}
	stateB, err := os.ReadFile(store.Layout().StateFile(identityB.Key))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateA), identityB.Key) || strings.Contains(string(stateB), identityA.Key) {
		t.Fatal("a group record must never be shared between identities")
	}
}

func TestProviderHomeNormalisation(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "codex")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	trailing, err := NormalizeProviderHome(nested + string(filepath.Separator))
	if err != nil {
		t.Fatalf("NormalizeProviderHome: %v", err)
	}
	plain, err := NormalizeProviderHome(nested)
	if err != nil {
		t.Fatalf("NormalizeProviderHome: %v", err)
	}
	if trailing != plain {
		t.Fatalf("a trailing separator changed the normalised home: %q vs %q", trailing, plain)
	}
	unclean, err := NormalizeProviderHome(filepath.Join(root, "x", "..", "codex"))
	if err != nil {
		t.Fatalf("NormalizeProviderHome: %v", err)
	}
	if unclean != plain {
		t.Fatalf("Clean did not apply: %q vs %q", unclean, plain)
	}

	// Symlinks resolve, so two spellings of one home are one identity.
	link := filepath.Join(root, "codex-link")
	if err := os.Symlink(nested, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink, err := NormalizeProviderHome(link)
	if err != nil {
		t.Fatalf("NormalizeProviderHome: %v", err)
	}
	if viaLink != plain {
		t.Fatalf("EvalSymlinks did not apply: %q vs %q", viaLink, plain)
	}
}

// Case is deliberately NOT folded. On a case-insensitive filesystem two spellings
// of one home therefore produce two identities: a duplicated record, which is a
// lost optimisation and never a wrong suppression. Folding would be wrong on
// case-sensitive volumes.
func TestProviderHomeCaseIsNotFolded(t *testing.T) {
	if IdentityKey(ProviderCodex, "/tmp/Codex-Home") == IdentityKey(ProviderCodex, "/tmp/codex-home") {
		t.Fatal("case was folded into the identity key")
	}

	// And through the production resolver, over paths that do not exist so the
	// filesystem's own case sensitivity cannot decide the answer.
	root := t.TempDir()
	mixed, err := IdentityFor(ProviderCodex, filepath.Join(root, "Codex-Home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	lower, err := IdentityFor(ProviderCodex, filepath.Join(root, "codex-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if mixed.Key == lower.Key {
		t.Fatal("normalisation folded case; that would be wrong on a case-sensitive volume")
	}
	if strings.Contains(mixed.Home, "codex-home") {
		t.Fatalf("normalised home = %q, want the original spelling preserved", mixed.Home)
	}
}

// A home that does not exist normalises fine, which is one half of the proof that
// nothing inside a provider home is opened.
func TestNormalisationDoesNotRequireTheHomeToExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never", "created", "codex")
	identity, err := IdentityFor(ProviderCodex, missing)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if identity.Key == "" {
		t.Fatal("a missing home must still produce an identity")
	}
}

// Nothing inside a provider home is ever opened or reported.
//
// The proof has three parts: the identity is the hash of the PATH STRING alone
// (computed independently here), an unreadable home directory still resolves, and
// only home_display — a path with $HOME collapsed to ~ — reaches any surface.
func TestNothingInsideAProviderHomeIsOpenedOrReported(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-ant-SUPER-SECRET-TOKEN-do-not-leak"
	for _, name := range []string{"auth.json", "config.toml", "credentials"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(`{"token":"`+secret+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	identity, err := IdentityFor(ProviderCodex, home)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}

	// The hash input is the path string only: recompute it here from the resolved
	// path with no reference to anything inside the directory.
	normalized, err := NormalizeProviderHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := IdentityKey(ProviderCodex, normalized); identity.Key != want {
		t.Fatalf("identity key = %q, want the path-only hash %q; the key must not derive from anything inside the home", identity.Key, want)
	}

	store, err := NewStore(Options{Layout: LayoutAt(root)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	class := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You've hit your usage limit.\n")})
	obs, _ := class.QuotaObservation(Evidence{RunID: "RUN-260802-000090"})
	if err := store.Observe(identity, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// No persisted byte and no reported byte carries anything from inside the home.
	report := store.Report([]Identity{identity})
	if len(report.Identities) != 1 {
		t.Fatalf("report carries %d identities", len(report.Identities))
	}
	entry := report.Identities[0]
	if strings.Contains(entry.HomeDisplay, secret) {
		t.Fatal("home_display leaked a credential")
	}
	if entry.HomeDisplay != CollapseHome(normalized) {
		t.Fatalf("home_display = %q, want the collapsed path %q", entry.HomeDisplay, CollapseHome(normalized))
	}
	walkStateTree(t, store.Layout().Root, func(path string, data []byte) {
		if strings.Contains(string(data), secret) {
			t.Fatalf("%s contains a credential read from inside the provider home", path)
		}
	})

	// And the home is not merely unread by accident: make it unreadable and
	// unlistable, and identity resolution plus reporting still work.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions cannot demonstrate this here")
	}
	if err := os.Chmod(home, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
	again, err := IdentityFor(ProviderCodex, home)
	if err != nil {
		t.Fatalf("resolving an unreadable home failed, so something inside it is being opened: %v", err)
	}
	if again.Key != identity.Key {
		t.Fatalf("identity changed when the home became unreadable: %q vs %q", again.Key, identity.Key)
	}
	if got := store.Report([]Identity{again}); len(got.Identities) != 1 {
		t.Fatal("reporting an unreadable home failed")
	}
}

func walkStateTree(t *testing.T, root string, check func(path string, data []byte)) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		check(path, data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

func TestCollapseHomeRendersTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if got := CollapseHome(filepath.Join(home, ".codex")); got != "~/.codex" {
		t.Fatalf("CollapseHome = %q, want ~/.codex", got)
	}
	if got := CollapseHome(home); got != "~" {
		t.Fatalf("CollapseHome(home) = %q, want ~", got)
	}
	if got := CollapseHome("/opt/codex"); got != "/opt/codex" {
		t.Fatalf("CollapseHome = %q, want the path unchanged", got)
	}
}

func TestDefaultProviderHomeUsesTheEnvironmentThenTheConvention(t *testing.T) {
	codexEnv := homeEnvVarFor(t, ProviderCodex)
	claudeEnv := homeEnvVarFor(t, ProviderClaude)

	t.Setenv(codexEnv, "")
	if got, err := DefaultProviderHome(ProviderCodex); err != nil || got != "~/.codex" {
		t.Fatalf("DefaultProviderHome = (%q, %v), want ~/.codex", got, err)
	}
	t.Setenv(codexEnv, "/tmp/other-codex")
	if got, _ := DefaultProviderHome(ProviderCodex); got != "/tmp/other-codex" {
		t.Fatalf("DefaultProviderHome = %q, want the environment value", got)
	}
	t.Setenv(claudeEnv, "")
	if got, _ := DefaultProviderHome(ProviderClaude); got != "~/.claude" {
		t.Fatalf("DefaultProviderHome = %q, want ~/.claude", got)
	}
	if _, err := DefaultProviderHome("qwen"); err == nil {
		t.Fatal("a provider with no defined home must be an error, not a guess")
	}

	// The two variables must be DIFFERENT, or the assertions above would pass
	// for a resolution that read one variable for both harnesses.
	if codexEnv == claudeEnv {
		t.Fatalf("both harnesses report %q as their home variable; this test cannot tell them apart", codexEnv)
	}
	t.Setenv(codexEnv, "/tmp/only-codex")
	t.Setenv(claudeEnv, "")
	if got, _ := DefaultProviderHome(ProviderClaude); got != "~/.claude" {
		t.Fatalf("setting %s changed the claude home to %q; the resolution is reading the wrong plugin", codexEnv, got)
	}
}

// TestTheProviderHomeComesFromTheHarnessPluginNotFromATableHere is invariant 5
// on the home rule, checked against the plugin that owns it.
//
// The extraction source kept a per-runtime home table inside this package. This
// port resolves through the agentic system plugin instead, and this test is
// what makes that a checkable claim rather than a comment: the values
// DefaultProviderHome produces must be the ones the plugin declares, for every
// declared runtime, including the ones that declare nothing.
func TestTheProviderHomeComesFromTheHarnessPluginNotFromATableHere(t *testing.T) {
	checked, refused := 0, 0
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		system, ok := agentic.Default.Lookup(declaration.System)
		if !ok {
			continue
		}
		capabilities := system.Capabilities()
		runtime := declaration.ID.String()
		checked++

		if capabilities.HomeEnvVar == "" && capabilities.DefaultHome == "" {
			refused++
			if _, err := DefaultProviderHome(runtime); err == nil {
				t.Errorf("runtime %q resolved a home, but its harness %q declares neither a home variable nor a default; the answer was invented somewhere", runtime, declaration.System)
			}
			continue
		}
		if capabilities.HomeEnvVar != "" {
			t.Setenv(capabilities.HomeEnvVar, "")
		}
		got, err := DefaultProviderHome(runtime)
		if err != nil {
			t.Errorf("runtime %q: %v", runtime, err)
			continue
		}
		if got != capabilities.DefaultHome {
			t.Errorf("runtime %q resolved home %q; its harness %q declares %q", runtime, got, declaration.System, capabilities.DefaultHome)
		}
		if capabilities.HomeEnvVar != "" {
			t.Setenv(capabilities.HomeEnvVar, "/tmp/from-the-environment-"+runtime)
			if got, _ := DefaultProviderHome(runtime); got != "/tmp/from-the-environment-"+runtime {
				t.Errorf("runtime %q did not read %s: got %q", runtime, capabilities.HomeEnvVar, got)
			}
			t.Setenv(capabilities.HomeEnvVar, "")
		}
	}
	if checked == 0 {
		t.Fatal("no declared runtime had a registered plugin; this test proved nothing")
	}
	if refused == 0 {
		t.Fatal("every declared runtime declared a home rule, so this test never exercised the refusal — the arm where a guess would be invisible")
	}
}

// TestAnOperatorDeclaredRuntimeInheritsItsHarnessHome is the case the source
// needed a hand-written table row and a paragraph for: alibaba models driven by
// the codex harness. Here it falls out of the declaration.
func TestAnOperatorDeclaredRuntimeInheritsItsHarnessHome(t *testing.T) {
	const runtime = "qwen-codex"
	if _, ok := vendorplugin.Default.RuntimeDeclarationOf(runtime); ok {
		t.Fatalf("fixture assumption broken: %q must NOT be in the frozen table; the source keeps it as its worked example of an operator declaration", runtime)
	}
	if _, err := DefaultProviderHome(runtime); err == nil {
		t.Fatalf("%q resolved a home while undeclared; an undeclared runtime names no harness", runtime)
	}

	vendors := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(vendors); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := vendors.DeclareRuntime(vendorplugin.RuntimeDeclaration{
		ID: runtime, System: "codex", Vendor: "alibaba",
		Broker: vendorplugin.BrokerProvenance{
			Checked: []string{"pkg/providerlimits/identity_test.go"},
			Found:   "declared by this test the way an operator declares it in project configuration",
		},
	}); err != nil {
		t.Fatalf("declaring %q: %v", runtime, err)
	}

	codexEnv := homeEnvVarFor(t, ProviderCodex)
	t.Setenv(codexEnv, "")
	got, err := DefaultProviderHomeIn(vendors, agentic.Default, runtime)
	if err != nil {
		t.Fatalf("DefaultProviderHomeIn(%q): %v", runtime, err)
	}
	if want, _ := DefaultProviderHomeIn(vendors, agentic.Default, ProviderCodex); got != want {
		t.Errorf("%q resolved home %q; it launches through the codex harness, which resolves %q", runtime, got, want)
	}

	// And it is still its own IDENTITY: the same home under two providers is
	// two rows on disk, which is what keeps its suppressions separate from
	// plain codex's.
	qwenIdentity, err := IdentityFor(runtime, "/tmp/shared-codex-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	codexIdentity, err := IdentityFor(ProviderCodex, "/tmp/shared-codex-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if qwenIdentity.Key == codexIdentity.Key {
		t.Fatal("qwen-codex and codex sharing one home collided into one identity; their quota is not shared and their state must not be either")
	}
}

// --- layout -----------------------------------------------------------------

func TestDefaultLayoutPrefersXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	layout, err := DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout: %v", err)
	}
	if want := filepath.Join("/tmp/xdg-state", "task-board", "provider-limits"); layout.Root != want {
		t.Fatalf("root = %q, want %q", layout.Root, want)
	}
}

func TestLayoutPaths(t *testing.T) {
	layout := LayoutAt("/state")
	key := "9f1c2ab73d40e5b8"
	cases := map[string]string{
		layout.StateFile(key):         "/state/task-board/provider-limits/9f1c2ab73d40e5b8.state.json",
		layout.LockFile(key):          "/state/task-board/provider-limits/9f1c2ab73d40e5b8.state.json.lock",
		layout.IndexFile():            "/state/task-board/provider-limits/identities.json",
		layout.SpreadCursorFile():     "/state/task-board/provider-limits/spread-cursor.json",
		layout.SimulateFile():         "/state/task-board/provider-limits/simulate.json",
		layout.SpreadCursorLockFile(): "/state/task-board/provider-limits/spread-cursor.json.lock",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
}

// --- the identity index -----------------------------------------------------

func TestIdentityIndexIsUpsertedAndListed(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	identities := f.store.ListIndexedIdentities()
	if len(identities) != 1 || identities[0].Key != f.identity.Key {
		t.Fatalf("indexed identities = %+v, want the observing identity", identities)
	}
	if identities[0].Provider != ProviderCodex {
		t.Errorf("provider = %q", identities[0].Provider)
	}
	if identities[0].HomeDisplay == "" {
		t.Error("home_display must be indexed so an operator can tell the identities apart")
	}
}

// A corrupt or missing index is rebuilt by scanning the per-identity files, which
// are authoritative. The index can never be the reason a spawn is blocked.
func TestCorruptIndexIsRebuiltFromTheAuthoritativeFiles(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	for _, payload := range []string{"{ not json", `{"version":99,"identities":{}}`} {
		if err := os.WriteFile(f.store.Layout().IndexFile(), []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		identities := f.store.ListIndexedIdentities()
		if len(identities) != 1 || identities[0].Key != f.identity.Key {
			t.Fatalf("index payload %q: rebuilt identities = %+v, want the identity recovered from its state file", payload, identities)
		}
	}

	if err := os.Remove(f.store.Layout().IndexFile()); err != nil {
		t.Fatal(err)
	}
	if got := f.store.ListIndexedIdentities(); len(got) != 1 {
		t.Fatalf("a missing index must be rebuilt: %+v", got)
	}
	// A rebuilt index never blocks a write.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000a0"); err != nil {
		t.Fatalf("Observe after index loss: %v", err)
	}
}

func TestEmptyIdentityIsCollectedAfterThirtyDays(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	statePath := f.store.Layout().StateFile(f.identity.Key)

	// A success leaves an available record, which is still a record.
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: "RUN-260802-0000a1"}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}
	// Age the record past the stale-record TTL so the file empties out on the next
	// write, then age the identity past the empty-identity TTL.
	f.clock.Advance(StaleRecordTTL + time.Hour)
	if err := f.store.ClearGroup(f.identity, "codex-spark", "test"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); exists {
		t.Fatal("a record with no observation for 24h must be dropped")
	}
	if err := f.store.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities: %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal("an identity that still holds a record must not be collected")
	}

	// Empty the file, then age it past the empty-identity TTL.
	if err := os.WriteFile(statePath, []byte(`{"version":3,"identity":"`+f.identity.Key+`","provider":"codex","groups":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(EmptyIdentityTTL + time.Hour)
	if err := f.store.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("an empty identity older than %s must be deleted: %v", EmptyIdentityTTL, err)
	}
	if got := f.store.ListIndexedIdentities(); len(got) != 0 {
		t.Fatalf("the index entry must go with the file: %+v", got)
	}
}

// A stale record that gates nothing is deleted outright. Nothing is lost: a
// missing record already reads as available, which is what an untouched
// available record meant anyway.
func TestStaleRecordGarbageCollection(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	if got := f.record(t, codexPlan).BackoffStep; got != 0 {
		t.Fatalf("backoff step = %d", got)
	}
	// Escalate, then clear the group with a success, so the stale record on the
	// sweep's desk is an available one carrying an old escalation history.
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-0000a2")
	if err := f.observeQuota(t, codexPlan, &lease, "RUN-260802-0000a2"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if got := f.record(t, codexPlan).BackoffStep; got != 1 {
		t.Fatalf("backoff step = %d, want 1", got)
	}
	if err := f.store.ObserveSuccess(f.identity, codexPlan, Evidence{RunID: "RUN-260802-0000a2"}); err != nil {
		t.Fatalf("ObserveSuccess: %v", err)
	}

	f.clock.Advance(StaleRecordTTL + time.Minute)
	// Any write triggers the sweep.
	if err := f.store.ClearGroup(f.identity, "codex-spark", "test"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); exists {
		t.Fatal("an available record with no observation for 24h must be dropped, so a stale escalation cannot follow the machine forever")
	}
	// The group is available again and the ladder starts from zero.
	f.suppress(t, codexPlan)
	if got := f.record(t, codexPlan).BackoffStep; got != 0 {
		t.Fatalf("backoff step after GC = %d, want 0", got)
	}
}

// The 24h sweep runs on ENTRY to a mutation, before the closure can attach a
// fresh lease that would make the record look live.
//
// This is the reviewer's cycle-2 F5 shape, driven entirely through the public
// API: escalate a group to the top of the ladder, let a day pass with nothing
// observed, then send max_parallel spawns at it at once. On the reviewed
// implementation ClaimProbe converted the record to probing first and the
// post-closure sweep then skipped it as live, so the first probe after 24h
// re-used the old max step — the escalation followed the machine forever, which
// is exactly what the TTL exists to prevent.
//
// Ageing out is a RESET, not a delete: the record stays, claim-gated at step 0.
// Deleting it would read as available and hand the group to all twenty callers
// at once — the D16 mistake, made by the garbage collector instead of by an
// operator.
func TestStaleGatingRecordIsAgedOutBeforeTheClaimDecision(t *testing.T) {
	f := newStoreFixture(t)
	ladder := f.store.Ladder()
	maxStep := len(ladder) - 1
	f.escalateToMaxStep(t, codexPlan)

	// A day and an hour with nothing observed on the group.
	f.clock.Advance(StaleRecordTTL + time.Hour)

	const parallel = 20
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, parallel)
	if unleased != 0 {
		t.Fatalf("%d of %d callers were admitted without a lease; ageing out must not delete the claim gate", unleased, parallel)
	}
	if leased != 1 {
		t.Fatalf("%d of %d callers were granted the probe, want exactly 1", leased, parallel)
	}
	winner := leases[0]

	record, exists := f.store.GroupRecordFor(f.identity, codexPlan)
	if !exists {
		t.Fatal("the aged-out record must still exist; a missing record reads as available")
	}
	if record.BackoffStep != 0 {
		t.Fatalf("backoff step = %d, want 0: a %s silence must forget the escalation, not carry step %d into the probe", record.BackoffStep, StaleRecordTTL, maxStep)
	}
	if record.AgedOutAt == nil {
		t.Error("an aged-out record must record when the sweep reset it")
	}
	if record.Evidence != nil || record.ProviderResetHint != nil {
		t.Errorf("ageing out must clear the old evidence and hint: %+v / %+v", record.Evidence, record.ProviderResetHint)
	}
	if record.State != StateProbing || record.ProbeLease == nil {
		t.Fatalf("the winner's lease must be persisted: state=%q lease=%+v", record.State, record.ProbeLease)
	}
	if !f.warnings.Contains("aged out from step") {
		t.Errorf("an operator must be told the backoff was aged out; warnings = %v", f.warnings.All())
	}

	// The winner's probe fails. The ladder restarts from the bottom, because the
	// step it would otherwise have re-used was 24h of nothing.
	if err := f.observeQuota(t, codexPlan, &winner, winner.RunID); err != nil {
		t.Fatalf("Observe from the lease owner: %v", err)
	}
	after := f.record(t, codexPlan)
	if after.BackoffStep != 1 {
		t.Fatalf("backoff step after the first post-TTL probe = %d, want 1 (the reviewed implementation reported %d)", after.BackoffStep, maxStep)
	}
	if got := after.NextProbeAt.Sub(f.clock.Now()); got != ladder[1] {
		t.Fatalf("next probe delay = %s, want %s", got, ladder[1])
	}
}

// The sweep never touches a record that holds a lease — resolving a lease
// belongs to resolveStaleLease alone. That leaves one way for a stale escalation
// to reach a claim: the lease is broken inside ClaimProbe, after the entry sweep
// has already run. The claim path therefore ages the record out again, after the
// lease is resolved and before the step is read.
func TestStaleEscalationDoesNotSurviveOnABrokenLease(t *testing.T) {
	f := newStoreFixture(t)
	ladder := f.store.Ladder()
	f.escalateToMaxStep(t, codexPlan)

	// A probe is claimed at the end of the window, and its owner then vanishes
	// without reporting anything at all.
	f.clock.Set(f.nextProbeAt(t, codexPlan))
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000b1"); !granted {
		t.Fatal("the elapsed step must be claimable")
	}
	f.clock.Advance(StaleRecordTTL + time.Hour)

	const parallel = 20
	leased, unleased, leases := f.claimConcurrently(t, codexPlan, parallel)
	if unleased != 0 {
		t.Fatalf("%d of %d callers were admitted without a lease", unleased, parallel)
	}
	if leased != 1 {
		t.Fatalf("%d of %d callers were granted the probe, want exactly 1", leased, parallel)
	}
	if got := f.record(t, codexPlan).BackoffStep; got != 0 {
		t.Fatalf("backoff step = %d, want 0: breaking a dead owner's lease must not smuggle the old step past the sweep", got)
	}
	winner := leases[0]
	if err := f.observeQuota(t, codexPlan, &winner, winner.RunID); err != nil {
		t.Fatalf("Observe from the lease owner: %v", err)
	}
	after := f.record(t, codexPlan)
	if after.BackoffStep != 1 {
		t.Fatalf("backoff step = %d, want 1", after.BackoffStep)
	}
	if got := after.NextProbeAt.Sub(f.clock.Now()); got != ladder[1] {
		t.Fatalf("next probe delay = %s, want %s", got, ladder[1])
	}
}

// The claim is not the only way into a stale record. A spawn that launched into
// a group needing no claim, or one that outlived the state it was selected
// against, reports quota evidence with no lease at all — and that write must see
// an aged-out record too, or the ladder resumes at the step a day of silence was
// supposed to forget.
//
// This is the half of the entry sweep that the claim path's own age-out does not
// cover, and it fails when the sweep is moved back after the mutation closure.
func TestALeaselessObservationCannotResumeAStaleEscalation(t *testing.T) {
	f := newStoreFixture(t)
	ladder := f.store.Ladder()
	maxStep := len(ladder) - 1
	f.escalateToMaxStep(t, codexPlan)
	f.clock.Advance(StaleRecordTTL + time.Hour)

	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000e1"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	record := f.record(t, codexPlan)
	if record.BackoffStep != 1 {
		t.Fatalf("backoff step = %d, want 1: after %s of silence the ladder restarts rather than resuming at step %d", record.BackoffStep, StaleRecordTTL, maxStep)
	}
	if got := record.NextProbeAt.Sub(f.clock.Now()); got != ladder[1] {
		t.Fatalf("next probe delay = %s, want %s (step %d would be %s)", got, ladder[1], maxStep, ladder[maxStep])
	}
	if record.State != StateSuppressed {
		t.Fatalf("state = %q, want suppressed", record.State)
	}
}

// Ageing out is idempotent: a record already sitting at a claim-gated step 0 has
// nothing to forget, so a later sweep must not rewrite it. Without this the
// sweep would write on every mutation for the rest of the file's life.
func TestAgeingOutANeutralGateIsANoOp(t *testing.T) {
	f := newStoreFixture(t)
	f.escalateToMaxStep(t, codexPlan)
	f.clock.Advance(StaleRecordTTL + time.Hour)
	// Any write runs the sweep.
	if err := f.store.ClearGroup(f.identity, "codex-spark", "test"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	first := f.record(t, codexPlan)
	if first.BackoffStep != 0 || first.State != StateProbeEligible || first.AgedOutAt == nil {
		t.Fatalf("the stale gate was not aged out: %+v", first)
	}

	f.clock.Advance(StaleRecordTTL + time.Hour)
	if err := f.store.ClearGroup(f.identity, "codex-spark", "test"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}
	second := f.record(t, codexPlan)
	if !second.AgedOutAt.Equal(*first.AgedOutAt) {
		t.Fatalf("a neutral gate was aged out again: %s then %s", first.AgedOutAt, second.AgedOutAt)
	}
	if !second.NextProbeAt.Equal(*first.NextProbeAt) {
		t.Fatalf("a neutral gate's probe window was rewritten: %s then %s", first.NextProbeAt, second.NextProbeAt)
	}
	// And it is still a gate, not availability.
	leased, unleased, _ := f.claimConcurrently(t, codexPlan, 8)
	if unleased != 0 || leased != 1 {
		t.Fatalf("leased=%d unleased=%d, want exactly one leased grant", leased, unleased)
	}
}

// A record whose probe lease is live is never collected, however old its last
// observation is. Collecting it would delete the lease that was just granted, and a
// missing record reads as available — so the claim the sweep just erased would be
// handed to every concurrent spawn.
func TestStaleRecordWithALiveLeaseIsNotCollected(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	// A whole day passes with nothing looking at the group, so its last observation
	// is now older than the stale-record TTL. Reads never collect, so the record is
	// still there when the next spawn arrives.
	f.clock.Advance(StaleRecordTTL + time.Hour)
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); !exists {
		t.Fatal("the fixture needs the record to survive until the claim; reads must not collect")
	}

	// The claim itself is a write, so the sweep runs in the same critical section
	// that just wrote the lease.
	lease, granted := f.claim(t, codexPlan, "RUN-260802-0000a3")
	if !granted || !lease.Held() {
		t.Fatal("the elapsed step must be claimable by exactly one caller")
	}
	record, exists := f.store.GroupRecordFor(f.identity, codexPlan)
	if !exists {
		t.Fatal("the sweep collected a record whose lease had just been granted")
	}
	if record.ProbeLease == nil || record.ProbeLease.RunID != lease.RunID {
		t.Fatalf("the granted lease was not persisted: %+v", record.ProbeLease)
	}
	// And the gate the lease represents still holds.
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000a4"); granted {
		t.Fatal("a second caller was admitted; the claim gate was collected away")
	}
}

// Identity garbage collection overlaps a live writer holding that identity's lock.
//
// The unlocked directory scan can only NOMINATE a candidate: between the scan and
// the delete, a concurrent writer can be halfway through populating exactly this
// file. This test freezes a writer inside its lock with the file on disk still
// empty and aged — the precise window in which the identity looks collectible —
// runs collection, and asserts that collection skipped it, did NOT unlink the live
// lock, and that the writer's state persisted.
//
// It fails on the reviewed implementation, which read eligibility without the lock,
// deleted the state file, and then unconditionally removed the sibling lock.
func TestIdentityCollectionNeverRacesALiveWriter(t *testing.T) {
	root := t.TempDir()
	layout := LayoutAt(root)
	clock := newTestClock(time.Date(2026, 8, 1, 1, 53, 40, 0, time.UTC))
	warnings := &warnSink{}

	identity, err := IdentityFor(ProviderCodex, filepath.Join(root, "codex-home"))
	if err != nil {
		t.Fatal(err)
	}
	statePath := layout.StateFile(identity.Key)
	lockPath := layout.LockFile(identity.Key)
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	// An empty identity file, aged past the empty-identity TTL: genuinely
	// collectible the instant nobody is working on it.
	empty := `{"version":3,"identity":"` + identity.Key + `","provider":"codex","groups":{}}`
	if err := os.WriteFile(statePath, []byte(empty), 0o600); err != nil {
		t.Fatal(err)
	}
	aged := clock.Now().Add(-2 * EmptyIdentityTTL)
	if err := os.Chtimes(statePath, aged, aged); err != nil {
		t.Fatal(err)
	}

	newStore := func(lockTimeout time.Duration) *Store {
		store, err := NewStore(Options{Layout: layout, Now: clock.Now, Warn: warnings.Warn, LockTimeout: lockTimeout})
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return store
	}

	writer := newStore(5 * time.Second)
	var (
		once     sync.Once
		inside   = make(chan struct{})
		release  = make(chan struct{})
		finished = make(chan error, 1)
	)
	// Freeze the writer between "lock held, decision made" and "bytes on disk".
	writer.writeState = func(path string, payload any) error {
		once.Do(func() {
			close(inside)
			<-release
		})
		return writeJSONAtomic(path, payload)
	}
	go func() { finished <- writer.ClearGroup(identity, codexPlan, "writer") }()

	select {
	case <-inside:
	case <-time.After(10 * time.Second):
		t.Fatal("the writer never reached its write")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the writer must hold the identity lock: %v", err)
	}

	collector := newStore(100 * time.Millisecond)
	if err := collector.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities must stay fail-open: %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("collection deleted an identity file while a writer held its lock: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("collection unlinked a live foreign lock: %v", err)
	}
	if !warnings.Contains("not collected") {
		t.Errorf("skipping a busy identity must warn; warnings = %v", warnings.All())
	}

	close(release)
	if err := <-finished; err != nil {
		t.Fatalf("the writer's own call failed: %v", err)
	}

	// The write the lock was protecting survived, and is visible to the collector.
	record, ok := collector.GroupRecordFor(identity, codexPlan)
	if !ok {
		t.Fatal("the writer's state was lost: collection ran inside its critical section")
	}
	if record.State != StateProbeEligible {
		t.Fatalf("state = %q, want probe_eligible", record.State)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("the writer's lock outlived its holder: %v", err)
	}

	// Narrowing: the guard is "do not race a writer", not "never collect". With the
	// record gone and nobody holding the lock, the same identity is collected, and
	// collection leaves no orphan lock behind.
	if err := os.WriteFile(statePath, []byte(empty), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(statePath, aged, aged); err != nil {
		t.Fatal(err)
	}
	// The writer's own write refreshed the index entry, so age the identity again
	// through the clock rather than through the file.
	clock.Advance(EmptyIdentityTTL + time.Hour)
	if err := collector.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("an aged empty identity with no live writer must be collected: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("collection left an orphan lock behind: %v", err)
	}
	if got := collector.ListIndexedIdentities(); len(got) != 0 {
		t.Fatalf("the index entry must go with the file: %+v", got)
	}
}

// The unlocked directory scan can go stale between nominating a candidate and
// taking its lock: a writer can populate exactly that file in the gap. Eligibility
// must therefore be DECIDED under the lock, not merely re-read there.
//
// This test occupies that window precisely — the identity is empty and aged when
// it is nominated, and holds a fresh record by the time the lock is granted — and
// asserts the freshly written state survives.
//
// It fails on any implementation that trusts the unlocked scan, including one that
// re-reads under the lock and ignores the answer.
func TestIdentityCollectionRechecksEligibilityUnderTheLock(t *testing.T) {
	root := t.TempDir()
	layout := LayoutAt(root)
	clock := newTestClock(time.Date(2026, 8, 1, 1, 53, 40, 0, time.UTC))
	warnings := &warnSink{}

	identity, err := IdentityFor(ProviderCodex, filepath.Join(root, "codex-home"))
	if err != nil {
		t.Fatal(err)
	}
	statePath := layout.StateFile(identity.Key)
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	seedEmpty := func() {
		empty := `{"version":3,"identity":"` + identity.Key + `","provider":"codex","groups":{}}`
		if err := os.WriteFile(statePath, []byte(empty), 0o600); err != nil {
			t.Fatal(err)
		}
		aged := clock.Now().Add(-2 * EmptyIdentityTTL)
		if err := os.Chtimes(statePath, aged, aged); err != nil {
			t.Fatal(err)
		}
	}
	newStore := func() *Store {
		store, err := NewStore(Options{Layout: layout, Now: clock.Now, Warn: warnings.Warn, LockTimeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return store
	}

	seedEmpty()
	writer := newStore()
	collector := newStore()
	raced := false
	// Between nomination and the lock, a writer claims the identity and populates
	// it. The lock is free at that instant, so the writer completes; collection
	// then acquires the lock and must find the record it left.
	collector.beforeCollectLock = func(string) {
		if raced {
			return
		}
		raced = true
		if err := writer.ClearGroup(identity, codexPlan, "writer"); err != nil {
			t.Errorf("the racing writer failed: %v", err)
		}
	}

	if err := collector.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities must stay fail-open: %v", err)
	}
	if !raced {
		t.Fatal("the identity was never nominated; the test would prove nothing")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("collection deleted a file that stopped being empty before the lock was granted: %v", err)
	}
	record, ok := collector.GroupRecordFor(identity, codexPlan)
	if !ok {
		t.Fatal("the racing writer's record was collected away")
	}
	if record.State != StateProbeEligible {
		t.Fatalf("state = %q, want probe_eligible", record.State)
	}

	// Narrowing: with nothing racing it, the same nomination collects.
	collector.beforeCollectLock = nil
	seedEmpty()
	clock.Advance(EmptyIdentityTTL + time.Hour)
	if err := collector.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("an aged empty identity with nothing racing it must be collected: %v", err)
	}
}
