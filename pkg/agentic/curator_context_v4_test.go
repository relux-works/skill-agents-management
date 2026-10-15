package agentic_test

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// curatorV4CorpusDir holds the launch-env-fragment-v4 schema cases copied
// byte for byte from curator-spec main 5509499,
// conformance/environments-muse-v4/schema-cases (CIP-0017, CIP-0015).
const curatorV4CorpusDir = "testdata/curator-launch-env-fragment-v4"

// curatorV4InterimUnsupported names the schema-valid cases this launcher
// refuses with the typed unsupported-policy refusal until the Codex -c
// credential-store consumer lands; they must never be accepted silently.
var curatorV4InterimUnsupported = map[string]bool{
	"valid-codex-file.json":     true,
	"valid-codex-keychain.json": true,
}

type curatorV4Case struct {
	Schema   string `json:"schema"`
	Instance string `json:"instance"`
	Valid    bool   `json:"valid"`
}

// curatorV4RequestHome derives the LaunchRequest.Home a consumer would pass
// for a decoded fragment: the shared XDG parent for muse, the single
// managed-home value otherwise.
func curatorV4RequestHome(context *agentic.CuratorContext) string {
	if config, ok := context.Env["XDG_CONFIG_HOME"]; ok && context.Environment == "muse" {
		return path.Dir(config)
	}
	for _, value := range context.Env {
		return value
	}
	return ""
}

// curatorV4Verdict decodes wire with the module's strict reader and runs the
// typed validator, the gate BuildPlan applies before any plugin surface.
func curatorV4Verdict(wire []byte) (*agentic.CuratorContext, error) {
	context, err := agentic.DecodeCuratorFragment(wire)
	if err != nil {
		return nil, err
	}
	return context, agentic.ValidateCuratorContext(context, curatorV4RequestHome(context))
}

// TestMuseV4ClosedShape drives every vendored v4 schema case through the
// strict decoder and the validator. Schema-valid cases are accepted, except
// the documented interim Codex store refusals; schema-invalid cases are
// refused as malformed or as an unsupported revision. Cases that only show an
// OLD schema rejecting a v4 token are the old reader's rule: this v4 reader
// must accept the same bytes as v4. A valid muse fragment without the fact
// also builds a real plan.
func TestMuseV4ClosedShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(curatorV4CorpusDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []curatorV4Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 67 {
		t.Fatalf("vendored v4 corpus has %d cases, want the 67 of curator-spec 5509499", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Instance, func(t *testing.T) {
			wire, err := os.ReadFile(filepath.Join(curatorV4CorpusDir, c.Instance))
			if err != nil {
				t.Fatal(err)
			}
			context, err := curatorV4Verdict(wire)
			oldSchemaOnV4Token := c.Schema != "launch-env-fragment-v4.schema.json" && strings.Contains(string(wire), `"launch-env-fragment-v4"`)
			switch {
			case curatorV4InterimUnsupported[c.Instance]:
				if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
					t.Fatalf("err = %v, want the typed unsupported-policy refusal", err)
				}
			case c.Valid || oldSchemaOnV4Token:
				if err != nil {
					t.Fatalf("schema-valid v4 fragment refused: %v", err)
				}
			default:
				if !errors.Is(err, agentic.ErrCuratorContextMalformed) && !errors.Is(err, agentic.ErrCuratorFragmentRevisionUnsupported) {
					t.Fatalf("schema-invalid fragment err = %v, want a malformed or unsupported-revision refusal", err)
				}
			}
			if c.Instance == "valid-muse-no-root.json" {
				if _, err := buildMuseCuratorPlan(t, *context, curatorV4RequestHome(context)); err != nil {
					t.Fatalf("BuildPlan for a valid v4 muse fragment: %v", err)
				}
			}
		})
	}
}

// TestBuildPlanCuratorContextRefusesInvalidFragmentV4 covers the v4 reader
// refusals the schema corpus cannot express: wire defects (repeated keys at
// depth, trailing data, mistyped members), typed values only a Go caller can
// build, and the managed muse revision rules, each through BuildPlan.
func TestBuildPlanCuratorContextRefusesInvalidFragmentV4(t *testing.T) {
	validWire, err := os.ReadFile(filepath.Join(curatorV4CorpusDir, "valid-muse-root.json"))
	if err != nil {
		t.Fatal(err)
	}
	wireCases := []struct {
		name, wire, want string
	}{
		{"repeated-top-member", `{"fragment":"launch-env-fragment-v4","fragment":"launch-env-fragment-v4"}`, `repeated member "fragment"`},
		{"repeated-env-member", strings.Replace(string(validWire), `"TBH_CREDENTIAL_BACKEND": "file"`, `"TBH_CREDENTIAL_BACKEND": "file", "TBH_CREDENTIAL_BACKEND": "file"`, 1), `repeated member "TBH_CREDENTIAL_BACKEND"`},
		{"repeated-root-member", strings.Replace(string(validWire), `"exclusive": true`, `"exclusive": true, "exclusive": true`, 1), `repeated member "exclusive"`},
		{"trailing-data", string(validWire) + "{}", "trailing data"},
		{"not-an-object", `["launch-env-fragment-v4"]`, "fragment must be one JSON object"},
		{"root-number", strings.Replace(string(validWire), `"exclusive": true`, `"exclusive": 1`, 1), "root_context must be exactly"},
		{"store-not-object", strings.Replace(string(validWire), `{
    "selection": "file"
  }`, `"file"`, 1), "credential_store must be a JSON object"},
		{"store-selection-null", strings.Replace(string(validWire), `"selection": "file"`, `"selection": null`, 1), "credential_store selection must not be null"},
		{"store-selection-number", strings.Replace(string(validWire), `"selection": "file"`, `"selection": 1`, 1), "credential_store selection must be a string"},
		{"store-overrides-not-object", strings.Replace(string(validWire), `"selection": "file"`, `"selection": "file", "config_overrides": "x"`, 1), "config_overrides must be an object of strings"},
		{"store-override-number", strings.Replace(string(validWire), `"selection": "file"`, `"selection": "file", "config_overrides": {"cli_auth_credentials_store": 1}`, 1), "config_overrides cli_auth_credentials_store must be a string"},
		{"unknown-member", strings.Replace(string(validWire), `"fragment":`, `"extra": 1, "fragment":`, 1), "fragment does not decode"},
	}
	for _, tc := range wireCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentic.DecodeCuratorFragment([]byte(tc.wire))
			if !errors.Is(err, agentic.ErrCuratorContextMalformed) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode err = %v, want malformed containing %q", err, tc.want)
			}
		})
	}

	typedCases := []struct {
		name    string
		mutate  func(*agentic.CuratorContext)
		wantErr error
		want    string
	}{
		{"v3-managed-muse", func(c *agentic.CuratorContext) {
			c.Revision, c.CredentialStore, c.RootContext = agentic.CuratorLaunchFragmentV3, nil, nil
		}, agentic.ErrCuratorFragmentRevisionUnsupported, "upgrade Curator to a release that emits launch-env-fragment-v4"},
		{"v2-managed-muse", func(c *agentic.CuratorContext) {
			c.Revision, c.CredentialStore, c.RootContext = agentic.CuratorLaunchFragmentV2, nil, nil
		}, agentic.ErrCuratorContextMalformed, "muse fragments require launch-env-fragment-v4"},
		{"v4-permissions-absent", func(c *agentic.CuratorContext) { c.Permissions = nil }, agentic.ErrCuratorContextMalformed, "v4 fragments require the permissions member"},
		{"v3-credential-store", func(c *agentic.CuratorContext) { c.Revision, c.RootContext = agentic.CuratorLaunchFragmentV3, nil }, agentic.ErrCuratorContextMalformed, "credential_store requires launch-env-fragment-v4"},
		{"v3-root-context", func(c *agentic.CuratorContext) { c.Revision, c.CredentialStore = agentic.CuratorLaunchFragmentV3, nil }, agentic.ErrCuratorContextMalformed, "root_context requires launch-env-fragment-v4"},
		{"root-not-exclusive", func(c *agentic.CuratorContext) { c.RootContext = &agentic.CuratorRootContext{} }, agentic.ErrCuratorContextMalformed, `root_context must be exactly {"exclusive":true}`},
		{"store-absent", func(c *agentic.CuratorContext) { c.CredentialStore = nil }, agentic.ErrCuratorContextMalformed, `v4 muse fragments require credential_store exactly {"selection":"file"}`},
		{"store-overrides", func(c *agentic.CuratorContext) {
			c.CredentialStore = &agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreFile, ConfigOverrides: map[string]string{}}
		}, agentic.ErrCuratorContextMalformed, `v4 muse fragments require credential_store exactly {"selection":"file"}`},
		{"backend-absent", func(c *agentic.CuratorContext) { delete(c.Env, agentic.CuratorMuseCredentialBackendVariable) }, agentic.ErrCuratorContextMalformed, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"backend-keychain", func(c *agentic.CuratorContext) { c.Env[agentic.CuratorMuseCredentialBackendVariable] = "keychain" }, agentic.ErrCuratorContextMalformed, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"backend-plus-HOME", func(c *agentic.CuratorContext) { c.Env["HOME"] = "/managed/home" }, agentic.ErrCuratorContextMalformed, "muse fragments must not carry HOME"},
	}
	for _, tc := range typedCases {
		t.Run(tc.name, func(t *testing.T) {
			context := validMuseCuratorContext()
			context.RootContext = &agentic.CuratorRootContext{Exclusive: true}
			tc.mutate(&context)
			_, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
			if !errors.Is(err, tc.wantErr) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildPlan err = %v, want %v containing %q", err, tc.wantErr, tc.want)
			}
		})
	}

	// Non-muse v4 policy: absent or native is accepted, an unknown selection
	// is malformed, and an agreeing Codex file/keychain policy refuses typed
	// as unsupported instead of launching without its override.
	for _, plugin := range curatorPlugins {
		t.Run("policy-"+plugin.name, func(t *testing.T) {
			build := func(store *agentic.CuratorCredentialStore) error {
				context := validCuratorContext(plugin)
				context.Revision = agentic.CuratorLaunchFragmentV4
				context.Permissions = validAcceptanceCuratorPermissions()
				context.CredentialStore = store
				_, err := buildCuratorPlan(t, plugin, context)
				return err
			}
			for _, store := range []*agentic.CuratorCredentialStore{nil, {Selection: agentic.CuratorCredentialStoreNative}} {
				if err := build(store); err != nil {
					t.Fatalf("v4 %s with store %+v refused: %v", plugin.environment, store, err)
				}
			}
			if err := build(&agentic.CuratorCredentialStore{Selection: "plaintext"}); !errors.Is(err, agentic.ErrCuratorContextMalformed) ||
				!strings.Contains(err.Error(), "credential_store selection must be native, file or keychain") {
				t.Fatalf("unknown selection err = %v", err)
			}
			if err := build(&agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreNative, ConfigOverrides: map[string]string{"cli_auth_credentials_store": "file"}}); !errors.Is(err, agentic.ErrCuratorContextMalformed) ||
				!strings.Contains(err.Error(), "a native credential_store carries no config_overrides") {
				t.Fatalf("native with overrides err = %v", err)
			}
			file := &agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreFile, ConfigOverrides: map[string]string{"cli_auth_credentials_store": "file"}}
			keychain := &agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreKeychain, ConfigOverrides: map[string]string{"cli_auth_credentials_store": "keyring"}}
			if plugin.environment != "codex_cli" {
				if err := build(file); !errors.Is(err, agentic.ErrCuratorContextMalformed) || !strings.Contains(err.Error(), "only codex_cli admits a non-native credential_store") {
					t.Fatalf("non-codex file store err = %v", err)
				}
				return
			}
			for _, store := range []*agentic.CuratorCredentialStore{file, keychain} {
				if err := build(store); !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
					t.Fatalf("codex %s store err = %v, want the typed unsupported refusal", store.Selection, err)
				}
			}
			disagreeing := &agentic.CuratorCredentialStore{Selection: agentic.CuratorCredentialStoreFile, ConfigOverrides: map[string]string{"cli_auth_credentials_store": "keyring"}}
			if err := build(disagreeing); !errors.Is(err, agentic.ErrCuratorContextMalformed) || !strings.Contains(err.Error(), "config_overrides must be exactly") {
				t.Fatalf("codex disagreeing store err = %v", err)
			}
			rooted := validCuratorContext(plugin)
			rooted.Revision = agentic.CuratorLaunchFragmentV4
			rooted.Permissions = validAcceptanceCuratorPermissions()
			rooted.RootContext = &agentic.CuratorRootContext{Exclusive: true}
			if _, err := buildCuratorPlan(t, plugin, rooted); !errors.Is(err, agentic.ErrCuratorContextMalformed) || !strings.Contains(err.Error(), "root_context is admitted only on the muse environment") {
				t.Fatalf("codex root_context err = %v", err)
			}
		})
	}
}

// TestCuratorV4ProvenanceCarriesPolicy: the fact and the store ride the
// plan's fragment identity, so a stored identity that lacks the fact is
// stale against one that carries it.
func TestCuratorV4ProvenanceCarriesPolicy(t *testing.T) {
	context := validMuseCuratorContext()
	plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := plan.CuratorContextProvenanceSnapshot()
	if !ok || stored.Fragment.CredentialStore == nil || stored.Fragment.CredentialStore.Selection != agentic.CuratorCredentialStoreFile || stored.Fragment.RootContext != nil {
		t.Fatalf("provenance policy = %+v", stored.Fragment)
	}
	if stored.Fragment.Env[agentic.CuratorMuseCredentialBackendVariable] != agentic.CuratorMuseCredentialBackendFile {
		t.Fatalf("provenance env lacks the backend constant: %v", stored.Fragment.Env)
	}
	current := stored
	current.Fragment.RootContext = &agentic.CuratorRootContext{Exclusive: true}
	if err := agentic.ValidateCuratorContextIdentity(stored, current); !errors.Is(err, agentic.ErrCuratorContextStale) {
		t.Fatalf("gaining the fact err = %v, want stale", err)
	}
	legacy := stored
	legacy.Fragment.Revision = agentic.CuratorLaunchFragmentV3
	if err := agentic.ValidateCuratorContextIdentity(legacy, stored); !errors.Is(err, agentic.ErrCuratorContextStale) {
		t.Fatalf("stored v3 muse identity err = %v, want stale", err)
	}
}
