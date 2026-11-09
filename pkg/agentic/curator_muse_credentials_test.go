package agentic_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Both coupled credential forms project identical homes and build a plan;
// native leaves the backend absent even when the parent supplies one.
func TestBuildPlanMuseV4CredentialForms(t *testing.T) {
	for _, selection := range []agentic.CuratorCredentialSelection{agentic.CuratorCredentialStoreFile, agentic.CuratorCredentialStoreNative} {
		t.Run(string(selection), func(t *testing.T) {
			context := validMuseCuratorContext()
			context.CredentialStore.Selection = selection
			if selection == agentic.CuratorCredentialStoreNative {
				delete(context.Env, agentic.CuratorMuseCredentialBackendVariable)
			}
			wire, err := json.Marshal(context)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := agentic.DecodeCuratorFragment(wire)
			if err != nil {
				t.Fatal(err)
			}
			context = *decoded
			req := museCuratorRequest(t, &context, museV3HomeParent)
			req.Env = append(req.Env, "TBH_CREDENTIAL_BACKEND=keychain")
			plan, err := agentic.BuildPlan(museCuratorRegistry(t), req, agentic.LaunchModeExec)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			wantHomes := agentic.CuratorMuseHomes{
				Parent: museV3HomeParent, XDGConfigHome: museV3HomeParent + "/config",
				XDGDataHome: museV3HomeParent + "/data", XDGStateHome: museV3HomeParent + "/state",
				XDGCacheHome: museV3HomeParent + "/cache",
			}
			homes, err := agentic.CuratorMuseHomesFromV4Env(context.Env)
			if err != nil || homes != wantHomes {
				t.Fatalf("env homes = %+v, %v; want %+v", homes, err, wantHomes)
			}
			homes, err = context.MuseHomes()
			if err != nil || homes != wantHomes {
				t.Fatalf("context homes = %+v, %v; want %+v", homes, err, wantHomes)
			}
			provenance, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok || provenance.MuseHomes == nil || *provenance.MuseHomes != wantHomes {
				t.Fatalf("plan has no matching homes provenance: %+v", provenance)
			}
			got := map[string]string{}
			for _, entry := range plan.Env {
				key, value, _ := strings.Cut(entry, "=")
				if strings.HasPrefix(key, "XDG_") || key == agentic.CuratorMuseCredentialBackendVariable {
					got[key] = value
				}
			}
			if !reflect.DeepEqual(got, context.Env) {
				t.Fatalf("plan homes/backend = %v, want %v", got, context.Env)
			}
		})
	}
}

// Invalid pairings and closed-shape violations return the exact malformed
// diagnostic and no plan or homes; validation does not rewrite the input.
func TestBuildPlanMuseV4CredentialRefusals(t *testing.T) {
	const storeDiagnostic = `v4 muse fragments require credential_store exactly {"selection":"file"} or {"selection":"native"}`
	cases := []struct {
		name      string
		selection agentic.CuratorCredentialSelection
		backend   *string
		extraEnv  bool
		overrides bool
		want      string
	}{
		{"native-file", "native", stringPointer("file"), false, false, "v4 muse native credential_store forbids TBH_CREDENTIAL_BACKEND"},
		{"native-keychain", "native", stringPointer("keychain"), false, false, "v4 muse native credential_store forbids TBH_CREDENTIAL_BACKEND"},
		{"native-empty", "native", stringPointer(""), false, false, "v4 muse native credential_store forbids TBH_CREDENTIAL_BACKEND"},
		{"native-other", "native", stringPointer("other"), false, false, "v4 muse native credential_store forbids TBH_CREDENTIAL_BACKEND"},
		{"file-absent", "file", nil, false, false, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"file-keychain", "file", stringPointer("keychain"), false, false, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"file-empty", "file", stringPointer(""), false, false, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"file-other", "file", stringPointer("other"), false, false, "v4 muse fragments require TBH_CREDENTIAL_BACKEND=file"},
		{"keychain", "keychain", nil, false, false, storeDiagnostic},
		{"unknown", "other", nil, false, false, storeDiagnostic},
		{"empty-selection", "", nil, false, false, storeDiagnostic},
		{"file-overrides", "file", stringPointer("file"), false, true, storeDiagnostic},
		{"native-overrides", "native", nil, false, true, storeDiagnostic},
		{"file-extra-env", "file", stringPointer("file"), true, false, "muse fragments require exactly the four XDG variables"},
		{"native-extra-env", "native", nil, true, false, "muse fragments require exactly the four XDG variables"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			context := validMuseCuratorContext()
			context.CredentialStore.Selection = tc.selection
			delete(context.Env, agentic.CuratorMuseCredentialBackendVariable)
			if tc.backend != nil {
				context.Env[agentic.CuratorMuseCredentialBackendVariable] = *tc.backend
			}
			if tc.extraEnv {
				context.Env["EXTRA"] = ""
			}
			if tc.overrides {
				context.CredentialStore.ConfigOverrides = map[string]string{}
			}
			before, err := json.Marshal(context)
			if err != nil {
				t.Fatal(err)
			}
			want := malformedV3Suffix(tc.want)
			if err := agentic.ValidateCuratorContext(&context, museV3HomeParent); !errors.Is(err, agentic.ErrCuratorContextMalformed) || err.Error() != want {
				t.Fatalf("ValidateCuratorContext = %v, want %s", err, want)
			}
			if homes, err := context.MuseHomes(); !errors.Is(err, agentic.ErrCuratorContextMalformed) || err.Error() != want || homes != (agentic.CuratorMuseHomes{}) {
				t.Fatalf("MuseHomes = %+v, %v, want zero homes and %s", homes, err, want)
			}
			plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
			if !errors.Is(err, agentic.ErrCuratorContextMalformed) || err.Error() != "agentic: building plan for muse: "+want || !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("BuildPlan = %+v, %v; want zero plan and %s", plan, err, want)
			}
			after, err := json.Marshal(context)
			if err != nil || string(before) != string(after) {
				t.Fatalf("input changed: %s -> %s (%v)", before, after, err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

// Unknown wire members cannot disappear during decoding into the typed store.
func TestMuseV4CredentialUnknownMember(t *testing.T) {
	for _, selection := range []string{"file", "native"} {
		t.Run(selection, func(t *testing.T) {
			_, err := agentic.DecodeCuratorFragment([]byte(`{"credential_store":{"selection":"` + selection + `","extra":true}}`))
			want := malformedV3Suffix(`fragment does not decode: agentic: malformed typed Curator context: credential_store carries an unknown member "extra"`)
			if !errors.Is(err, agentic.ErrCuratorContextMalformed) || err.Error() != want {
				t.Fatalf("DecodeCuratorFragment = %v, want %s", err, want)
			}
		})
	}
}

// The standalone env reader admits only the four homes with an absent backend
// or backend=file; credential selection is checked by the context validators.
func TestMuseV4CredentialEnvRefusals(t *testing.T) {
	for _, backend := range []string{"", "keychain", "other"} {
		t.Run("backend-"+backend, func(t *testing.T) {
			context := validMuseCuratorContext()
			context.Env[agentic.CuratorMuseCredentialBackendVariable] = backend
			homes, err := agentic.CuratorMuseHomesFromV4Env(context.Env)
			if !errors.Is(err, agentic.ErrCuratorContextMalformed) || err.Error() != malformedV3Suffix("v4 muse fragments require TBH_CREDENTIAL_BACKEND=file") || homes != (agentic.CuratorMuseHomes{}) {
				t.Fatalf("homes = %+v, err = %v", homes, err)
			}
		})
	}
}
