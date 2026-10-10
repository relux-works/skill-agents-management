package agentic_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

// sealRev5EnvSystem is a public-plugin fixture: the real Claude plugin with
// only binary resolution, argv and the child environment overridden, so
// generated process shapes still drive Registry -> BuildPlan -> FinalizePlan
// -> ExportSeal -> DecodeSeal / ImportSeal. The binary is never executed.
type sealRev5EnvSystem struct {
	*claude.System
	binary  string
	argv    []string
	hasArgv bool
	env     []string
}

func (s *sealRev5EnvSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return s.binary, nil
}

func (s *sealRev5EnvSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if s.hasArgv {
		return append([]string(nil), s.argv...), nil
	}
	return s.System.Argv(req, mode)
}

func (s *sealRev5EnvSystem) ChildEnv([]string, agentic.LaunchRequest) ([]string, error) {
	return append([]string(nil), s.env...), nil
}

func sealRev5Build(t *testing.T, sys *sealRev5EnvSystem) agentic.Plan {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, agentic.LaunchRequest{
		System: sys.ID(),
		Model:  agentic.Model{ID: "seal-test"},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// TestFinalizedEmptyEnvRoundTrips is the zero-environment control: an
// untouched finalized plan with an empty process environment round-trips
// through ExportSeal/DecodeSeal/ImportSeal, and the imported verifier still
// binds the process. Required empty collections export as {} / [], never
// null; the strict wire keeps refusing null.
func TestFinalizedEmptyEnvRoundTrips(t *testing.T) {
	sys := &sealRev5EnvSystem{System: claude.New(), binary: "/bin/true", env: []string{}}
	final, err := agentic.FinalizePlan(sealRev5Build(t, sys), agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Binding == nil || seal.Data.Binding.Selectors == nil {
		t.Fatal("empty selectors collapsed: export lost the required empty map")
	}
	if seal.Data.Binding.EnvNames == nil || seal.Data.Binding.Argv == nil {
		t.Fatal("empty collections collapsed: export lost a required empty slice")
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	for _, member := range []string{`"selectors":null`, `"env_names":null`, `"argv":null`} {
		if strings.Contains(string(wire), member) {
			t.Fatalf("empty selectors collapsed: wire carries %s", member)
		}
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatalf("DecodeSeal refused the untouched empty-env export: %v", err)
	}
	imported, err := agentic.ImportSeal(sys, decoded)
	if err != nil {
		t.Fatalf("ImportSeal refused the untouched empty-env export: %v", err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatalf("imported verifier refused the untouched empty-env process: %v", err)
	}
	changed := final
	changed.Env = append(append([]string(nil), final.Env...), "OUTSIDE=value")
	if err := imported.VerifyBeforeExec(changed); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("imported verifier admitted an env change: %v", err)
	}
}

// TestSealRoundTripProperty is the generated class test for the
// empty-env-roundtrip finding: across generated environment sizes (biased to
// zero), generated argv tails and generated overlay layers, an untouched
// supported finalized process always round-trips through
// ExportSeal/DecodeSeal/ImportSeal. The seed is fixed, so the 64 cases are
// deterministic. No required collection may serialize as null.
func TestSealRoundTripProperty(t *testing.T) {
	values := []string{"", "v", "value with spaces", "a=b=c", "hello-world_09", "unicode-here"}
	tails := []string{"", "--flag", "tail-value", "--key=value", "unicode-tail"}
	rng := rand.New(rand.NewSource(20261004))
	const cases = 64
	for i := 0; i < cases; i++ {
		t.Run(fmt.Sprintf("case-%02d", i), func(t *testing.T) {
			envSizes := []int{0, 0, 0, 1, 1, 2, 3, 5, 8}
			base := make([]string, 0, 8)
			for n := 0; n < envSizes[rng.Intn(len(envSizes))]; n++ {
				base = append(base, fmt.Sprintf("PROP_BASE_%d=%s", n, values[rng.Intn(len(values))]))
			}
			var fragment, prompt []string
			for n := 0; n < rng.Intn(3); n++ {
				fragment = append(fragment, fmt.Sprintf("PROP_FRAG_%d_%d=%s", i, n, values[rng.Intn(len(values))]))
			}
			for n := 0; n < rng.Intn(3); n++ {
				prompt = append(prompt, fmt.Sprintf("PROP_PROMPT_%d_%d=%s", i, n, values[rng.Intn(len(values))]))
			}
			if len(base) > 0 && rng.Intn(2) == 0 {
				name, _, _ := strings.Cut(base[0], "=")
				prompt = append(prompt, name+"=overridden")
			}
			var tail []string
			for n := 0; n < rng.Intn(4); n++ {
				tail = append(tail, tails[rng.Intn(len(tails))])
			}
			sys := &sealRev5EnvSystem{System: claude.New(), binary: "/bin/true", env: base}
			final, err := agentic.FinalizePlan(sealRev5Build(t, sys), agentic.FinalizeOverlays{
				FragmentEnv: fragment,
				PromptEnv:   prompt,
				NativeTail:  tail,
			}, nil)
			if err != nil {
				t.Fatalf("FinalizePlan: %v", err)
			}
			if err := final.VerifyBeforeExec(); err != nil {
				t.Fatalf("finalized plan refused itself: %v", err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			wire, err := json.Marshal(seal)
			if err != nil {
				t.Fatalf("marshal seal: %v", err)
			}
			for _, member := range []string{`"selectors":null`, `"env_names":null`, `"argv":null`, `"artifacts":null`} {
				if strings.Contains(string(wire), member) {
					t.Fatalf("required collection serialized as null: %s", member)
				}
			}
			decoded, err := agentic.DecodeSeal(wire)
			if err != nil {
				t.Fatalf("DecodeSeal refused the untouched export: %v", err)
			}
			imported, err := agentic.ImportSeal(sys, decoded)
			if err != nil {
				t.Fatalf("ImportSeal refused the untouched export: %v", err)
			}
			if err := imported.VerifyBeforeExec(final); err != nil {
				t.Fatalf("imported verifier refused the untouched process: %v", err)
			}
			changed := final
			changed.Argv = append(append([]string(nil), final.Argv...), "property-tail")
			if err := imported.VerifyBeforeExec(changed); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("imported verifier admitted an argv change: %v", err)
			}
		})
	}
	t.Logf("round-trip property: %d of %d generated cases accepted", cases, cases)
}

// TestBuildPlanRefusesUnrepresentableStrings drives invalid UTF-8 through the
// production BuildPlan entry point in each seal-bound field: the binary, one
// argv element, one env value and one env name. Each refuses typed as a plugin
// contract violation; no accepted plan can carry a string guard JSON would
// silently repair.
func TestBuildPlanRefusesUnrepresentableStrings(t *testing.T) {
	bad := string([]byte{0xff})
	for _, tc := range []struct {
		name   string
		system *sealRev5EnvSystem
	}{
		{name: "binary", system: &sealRev5EnvSystem{System: claude.New(), binary: "/bin/tr" + bad, env: []string{"PATH=/bin"}}},
		{name: "argv", system: &sealRev5EnvSystem{System: claude.New(), binary: "/bin/true", argv: []string{"/bin/true", bad}, hasArgv: true, env: []string{"PATH=/bin"}}},
		{name: "env-value", system: &sealRev5EnvSystem{System: claude.New(), binary: "/bin/true", env: []string{"PATH=/bin", "PROP_BAD=" + bad}}},
		{name: "env-name", system: &sealRev5EnvSystem{System: claude.New(), binary: "/bin/true", env: []string{"PATH=/bin", "PROP_" + bad + "=value"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := agentic.NewRegistry()
			if err := registry.Register(tc.system); err != nil {
				t.Fatalf("Register: %v", err)
			}
			_, err := agentic.BuildPlan(registry, agentic.LaunchRequest{
				System: tc.system.ID(),
				Model:  agentic.Model{ID: "seal-test"},
			}, agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrPluginContract) {
				t.Fatalf("BuildPlan with invalid UTF-8 %s admitted or untyped: %v", tc.name, err)
			}
		})
	}
}

// TestFinalizePlanRefusesUnrepresentableStrings drives invalid UTF-8 through
// the production FinalizePlan entry point in each input field: base binary,
// base argv, fragment and prompt values, an env name, and the native tail.
// Each refuses typed; the native-tail case also kills the narrowing mutant
// that admits exactly the 0xff witness token.
func TestFinalizePlanRefusesUnrepresentableStrings(t *testing.T) {
	bad := string([]byte{0xff})
	t.Run("base-binary", func(t *testing.T) {
		base := sealTestClaudePlan(t)
		base.Binary = "/bin/tr" + bad
		if _, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
			t.Fatalf("invalid UTF-8 base binary admitted or untyped: %v", err)
		}
	})
	t.Run("base-argv", func(t *testing.T) {
		base := sealTestClaudePlan(t)
		base.Argv = append(append([]string(nil), base.Argv...), bad)
		if _, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
			t.Fatalf("invalid UTF-8 base argv admitted or untyped: %v", err)
		}
	})
	for _, tc := range []struct {
		name     string
		overlays agentic.FinalizeOverlays
	}{
		{name: "fragment-value", overlays: agentic.FinalizeOverlays{FragmentEnv: []string{"PROP_FRAG=" + bad}}},
		{name: "prompt-value", overlays: agentic.FinalizeOverlays{PromptEnv: []string{"PROP_PROMPT=" + bad}}},
		{name: "env-name", overlays: agentic.FinalizeOverlays{FragmentEnv: []string{"PROP_" + bad + "=value"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := agentic.FinalizePlan(sealTestClaudePlan(t), tc.overlays, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
				t.Fatalf("invalid UTF-8 %s admitted or untyped: %v", tc.name, err)
			}
		})
	}
	t.Run("native-tail", func(t *testing.T) {
		if _, err := agentic.FinalizePlan(sealTestClaudePlan(t), agentic.FinalizeOverlays{NativeTail: []string{bad}}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
			t.Fatalf("invalid UTF-8 tail admitted: %v", err)
		}
	})
}

// TestSealValidUnicodeRoundTrips is the valid-Unicode control: non-ASCII text
// that IS valid UTF-8 in every seal-bound field round-trips exactly through
// ExportSeal/DecodeSeal/ImportSeal, and the imported verifier binds it.
func TestSealValidUnicodeRoundTrips(t *testing.T) {
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "claude-héllo-世界-🎉")
	if err := execfixture.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write stub executable: %v", err)
	}
	sys := &sealRev5EnvSystem{System: claude.New(), binary: binary, env: []string{"PROP_BASE=héllo-世界-🎉"}}
	final, err := agentic.FinalizePlan(sealRev5Build(t, sys), agentic.FinalizeOverlays{
		FragmentEnv: []string{"PROP_FRAG=fragment-🎉"},
		PromptEnv:   []string{"PROP_PROMPT=prompt-世界"},
		NativeTail:  []string{"--name", "völue-🎉", ""},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan refused valid Unicode: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatalf("DecodeSeal: %v", err)
	}
	if decoded.Data.Binding == nil || decoded.Data.Binding.Binary != binary {
		t.Fatalf("decoded binding binary = %+v, want %q", decoded.Data.Binding, binary)
	}
	wantTail := []string{"--name", "völue-🎉", ""}
	gotTail := decoded.Data.Binding.Argv[len(decoded.Data.Binding.Argv)-3:]
	if strings.Join(gotTail, "\x00") != strings.Join(wantTail, "\x00") {
		t.Fatalf("decoded binding argv tail = %q, want %q", gotTail, wantTail)
	}
	imported, err := agentic.ImportSeal(sys, decoded)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatalf("imported verifier refused the untouched Unicode process: %v", err)
	}
}
