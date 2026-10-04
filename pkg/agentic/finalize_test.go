package agentic_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func sealEnvValue(t *testing.T, env []string, name string) (string, bool) {
	t.Helper()
	found := false
	var value string
	for _, entry := range env {
		entryName, entryValue, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if entryName == name {
			found = true
			value = entryValue
		}
	}
	return value, found
}

func TestFinalizePlanAppliesOverlaysInOrder(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{
		FragmentEnv: []string{"SEAL_FRAGMENT_A=fragment-a", "SEAL_SHARED=from-fragment"},
		PromptEnv:   []string{"SEAL_SHARED=from-prompt", "SEAL_PROMPT_B=prompt-b"},
		NativeTail:  []string{"--sealed-tail", "tail-value"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	wantArgv := append(append([]string(nil), base.Argv...), "--sealed-tail", "tail-value")
	if strings.Join(final.Argv, "\x00") != strings.Join(wantArgv, "\x00") {
		t.Fatalf("final argv = %q, want base plus the native tail", final.Argv)
	}
	for name, want := range map[string]string{"SEAL_FRAGMENT_A": "fragment-a", "SEAL_SHARED": "from-prompt", "SEAL_PROMPT_B": "prompt-b"} {
		got, found := sealEnvValue(t, final.Env, name)
		if !found || got != want {
			t.Fatalf("final env %s = %q, want %q", name, got, want)
		}
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	// Every overlaid name is an env-sensitive selector: changing one refuses.
	for _, name := range []string{"SEAL_FRAGMENT_A", "SEAL_SHARED", "SEAL_PROMPT_B"} {
		changed := final
		changed.Env = append([]string(nil), final.Env...)
		replaced := false
		for i, entry := range changed.Env {
			if entryName, _, ok := strings.Cut(entry, "="); ok && entryName == name {
				changed.Env[i] = name + "=tampered"
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("selector %s missing from the final env", name)
		}
		if err := changed.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
			t.Fatalf("changed selector %s admitted: %v", name, err)
		}
	}
	// A base entry outside overlays remains bound to the exact final env.
	if len(final.Env) == 0 {
		t.Fatal("final plan carries no environment to probe")
	}
	untouched := final
	untouched.Env = append([]string(nil), final.Env...)
	baseName, _, ok := strings.Cut(untouched.Env[0], "=")
	if !ok {
		t.Fatalf("base env entry %q has no value", untouched.Env[0])
	}
	if baseName == "SEAL_FRAGMENT_A" || baseName == "SEAL_SHARED" || baseName == "SEAL_PROMPT_B" {
		t.Fatalf("probe entry %q is an overlaid selector, want a base entry", baseName)
	}
	untouched.Env[0] = baseName + "=renewed-value"
	if err := untouched.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("changed base entry admitted: %v", err)
	}
}

func TestFinalizedPlanBindsExactFinalArgv(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{
		NativeTail: []string{"--sealed-tail"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	appended := final
	appended.Argv = append(append([]string(nil), final.Argv...), "--extra-element")
	if err := appended.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("appended argv element admitted: %v", err)
	}
	if len(final.Argv) == 0 {
		t.Fatal("final plan carries no argv to truncate")
	}
	truncated := final
	truncated.Argv = append([]string(nil), final.Argv[:len(final.Argv)-1]...)
	if err := truncated.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("truncated argv admitted: %v", err)
	}
	swapped := final
	swapped.Argv = append([]string(nil), final.Argv...)
	swapped.Argv[len(swapped.Argv)-1] = "--other-tail"
	if err := swapped.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("swapped argv element admitted: %v", err)
	}
	moved := final
	moved.Binary += "-changed"
	if err := moved.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("changed binary admitted: %v", err)
	}
}

func TestFinalizePlanValidatesFragmentBeforePrompt(t *testing.T) {
	base := sealTestClaudePlan(t)
	cases := []struct {
		name     string
		overlays agentic.FinalizeOverlays
		bindings []agentic.TypedBinding
		wantIn   string
	}{
		{
			name:     "fragment-before-prompt",
			overlays: agentic.FinalizeOverlays{FragmentEnv: []string{"1BAD=oops"}, PromptEnv: []string{"2BAD=oops"}},
			wantIn:   "fragment",
		},
		{
			name:     "prompt-before-tail",
			overlays: agentic.FinalizeOverlays{PromptEnv: []string{"2BAD=oops"}, NativeTail: []string{"bad\x00tail"}},
			wantIn:   "prompt",
		},
		{
			name:     "tail-before-bindings",
			overlays: agentic.FinalizeOverlays{NativeTail: []string{"bad\x00tail"}},
			bindings: []agentic.TypedBinding{{Kind: "future-kind"}},
			wantIn:   "native tail",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentic.FinalizePlan(base, tc.overlays, tc.bindings)
			if !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
				t.Fatalf("FinalizePlan err = %v, want ErrFinalizeOverlayMalformed", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("FinalizePlan err = %v, want the %s refusal first", err, tc.wantIn)
			}
		})
	}
}

func TestFinalizePlanRefusesMalformedOverlayEntries(t *testing.T) {
	base := sealTestClaudePlan(t)
	cases := []struct {
		name    string
		entries []string
	}{
		{name: "no-value", entries: []string{"SEAL_NOVALUE"}},
		{name: "bad-name", entries: []string{"1SEAL_BAD=oops"}},
		{name: "nul-value", entries: []string{"SEAL_NUL=bad\x00value"}},
		{name: "duplicate", entries: []string{"SEAL_DUP=one", "SEAL_DUP=two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{FragmentEnv: tc.entries}, nil)
			if !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
				t.Fatalf("FinalizePlan err = %v, want ErrFinalizeOverlayMalformed", err)
			}
		})
	}
}

func TestFinalizePlanNativeTailRules(t *testing.T) {
	base := sealTestClaudePlan(t)
	if _, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{NativeTail: []string{"bad\x00token"}}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeOverlayMalformed for a NUL tail token", err)
	}
	// An empty token is legal argv: exec carries it, so finalization keeps it.
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{NativeTail: []string{""}}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan refused an empty tail token: %v", err)
	}
	if len(final.Argv) != len(base.Argv)+1 || final.Argv[len(final.Argv)-1] != "" {
		t.Fatalf("final argv = %q, want the empty token appended", final.Argv)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
}

func TestFinalizePlanRefusesAnyBinding(t *testing.T) {
	base := sealTestClaudePlan(t)
	_, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, []agentic.TypedBinding{{Kind: "future-kind", Name: "n", Value: "v"}})
	if !errors.Is(err, agentic.ErrFinalizeBindingUnknown) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeBindingUnknown", err)
	}
}

func TestFinalizePlanRefusesPlanWithoutSealBasis(t *testing.T) {
	muse := sealTestMusePlan(t)
	if _, err := agentic.FinalizePlan(muse, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeNoSealBasis) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeNoSealBasis", err)
	}
}

func TestFinalizePlanRefusesSecondFinalization(t *testing.T) {
	base := sealTestClaudePlan(t)
	once, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if _, err := agentic.FinalizePlan(once, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeNoSealBasis) {
		t.Fatalf("second FinalizePlan err = %v, want ErrFinalizeNoSealBasis", err)
	}
}

func TestFinalizePlanEmptyOverlaysBindBaseProcess(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if strings.Join(final.Argv, "\x00") != strings.Join(base.Argv, "\x00") || strings.Join(final.Env, "\x00") != strings.Join(base.Env, "\x00") {
		t.Fatal("empty overlays changed the base process")
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	appended := final
	appended.Argv = append(append([]string(nil), final.Argv...), "--extra")
	if err := appended.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("appended argv element admitted: %v", err)
	}
}

func TestFinalizedUnsealedPlanExportsUnsealed(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Kind != agentic.SealKindUnsealedBound || seal.Data.Sealed != nil || seal.Data.Binding == nil {
		t.Fatalf("data = %+v, want local unsealed-bound projection", seal.Data)
	}
	if seal.HostedAdmissible() {
		t.Fatal("local finalized bindings are hosted-admissible")
	}
}
