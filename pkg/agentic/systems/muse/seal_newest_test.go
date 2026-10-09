package muse

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestLauncherValidBuildWithoutTrailingRevisionEndToEnd pins the optional
// revision everywhere: a launcher-valid build without a trailing
// subrevision parses as a bare build, as a pinned filename and — through
// a frozen copy — seals end to end. The selector leg passes even under
// the M10 mutant; the filename and seal legs fail, which is what proves
// the narrowing instead of a shared gate.
func TestLauncherValidBuildWithoutTrailingRevisionEndToEnd(t *testing.T) {
	t.Parallel()
	const build = "1.4.5-R5500"
	identity, err := ParseBuildID(build)
	if err != nil || identity.Release != "1.4.5" || identity.Build != build {
		t.Fatalf("ParseBuildID(%q) = (%#v, %v), want the dotless build", build, identity, err)
	}
	named, err := ParsePinnedBinaryName("muse-bin-" + build)
	if err != nil || named != identity {
		t.Fatalf("ParsePinnedBinaryName(muse-bin-%q) = (%#v, %v), want the dotless build", build, named, err)
	}
	dir := t.TempDir()
	frozen, digest := writeFrozenMuseFixture(t, dir, build,
		"Muse Code 1.4.5 (1.4.5-R5500)", museYoloHelpFixture)
	req := launchRequest(t, "")
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "1.4.5"
	req.FrozenToolBinary = frozen
	req.FrozenToolBuild = build
	req.FrozenToolSHA256 = digest
	plan := buildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec on the dotless sealed plan: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedReleaseKey] != build {
		t.Fatalf("sealed release = %q, want the attested dotless build",
			seal.Data.Sealed.Selectors[sealedReleaseKey])
	}
}

// TestSealBindsAttestedNewest pins attested-build admission: a novel
// well-formed build no table ever listed seals in both postures, binds
// exactly what the binary attests, and round-trips through export and
// import. There is no verified-build list left to consult.
func TestSealBindsAttestedNewest(t *testing.T) {
	t.Parallel()
	const build = "1.5.0-R9999.1"
	version := "printf '%s\\n' 'Muse Code 1.5.0 (1.5.0-R9999.1)'\n"
	for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			_, env := writeMuseVersionHelpStub(t, version, museYoloHelpFixture)
			plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
				System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
				ToolRelease: "1.5.0", PermissionMode: mode,
			})
			if err := plan.VerifyBeforeExec(); err != nil {
				t.Fatalf("VerifyBeforeExec on the novel sealed plan: %v", err)
			}
			seal, err := plan.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			if seal.Data.Sealed.Selectors[sealedReleaseKey] != build {
				t.Fatalf("sealed release = %q, want the attested novel build",
					seal.Data.Sealed.Selectors[sealedReleaseKey])
			}
			imported, err := New().ImportSealedData(*seal.Data.Sealed)
			if err != nil {
				t.Fatalf("ImportSealedData of the novel seal: %v", err)
			}
			if err := imported.VerifyBeforeExec(plan); err != nil {
				t.Fatalf("imported VerifyBeforeExec on the novel seal: %v", err)
			}
		})
	}
}

// TestUnlistedReleaseUsesSupportedGrammar pins yolo on an unlisted
// release end to end: with help evidence the unlisted build maps through
// the supported grammar — exactly one bypass flag, native arguments
// forwarded verbatim — and seals what the binary attests.
func TestUnlistedReleaseUsesSupportedGrammar(t *testing.T) {
	t.Parallel()
	const build = "9.9.9-R777"
	_, env := writeMuseVersionHelpStub(t,
		"printf '%s\\n' 'Muse Code 9.9.9 (9.9.9-R777)'\n", museYoloHelpFixture)
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "9.9.9", PermissionMode: agentic.PermissionModeYolo,
		NativeArgs: []string{"do", "the", "thing"},
	})
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
	suffix := plan.Argv[len(plan.Argv)-3:]
	if suffix[0] != "do" || suffix[1] != "the" || suffix[2] != "thing" {
		t.Fatalf("argv suffix = %q, want the verbatim native arguments", suffix)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec on the unlisted sealed plan: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedReleaseKey] != build {
		t.Fatalf("sealed release = %q, want the attested unlisted build",
			seal.Data.Sealed.Selectors[sealedReleaseKey])
	}
}

// TestUnlistedReleaseWithoutHelpEvidenceRefuses pins the evidence half of
// unlisted admission: a novel build whose help declares no bypass flag
// refuses yolo. The BuildPlan leg pins the outer argv gate; the direct
// seal legs pin the sealer's independent enforcement — creation refuses
// a declaration-less novel build, and the known-build control still
// refuses when the M41 mutant exempts the novel one.
func TestUnlistedReleaseWithoutHelpEvidenceRefuses(t *testing.T) {
	t.Parallel()
	novelBinary, novelEnv := writeMuseVersionHelpStub(t,
		"printf '%s\\n' 'Muse Code 9.9.9 (9.9.9-R1.1)'\n", museNoYoloHelpFixture)
	_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: novelEnv,
		ToolRelease: "9.9.9", PermissionMode: agentic.PermissionModeYolo,
	}, agentic.LaunchModeInteractive)
	if !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("BuildPlan yolo without help evidence err = %v, want ErrPermissionModeUnsupported", err)
	}
	directSeal := func(t *testing.T, binary string, env []string, release string) error {
		t.Helper()
		plan := agentic.Plan{
			System: New().ID(), Mode: agentic.LaunchModeInteractive,
			Binary: binary, Argv: []string{"muse", "--model", "echo", museYoloFlag},
			Env:         append(append([]string(nil), env...), "MUSE_NO_AUTO_UPDATE=1"),
			ToolRelease: release, PermissionMode: agentic.PermissionModeYolo,
		}
		_, err := New().SealExecPlan(plan)
		return err
	}
	if err := directSeal(t, novelBinary, novelEnv, "9.9.9"); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("direct seal of declaration-less novel build err = %v, want ErrPermissionModeUnsupported", err)
	}
	knownBinary, knownEnv := writeMuseVersionHelpStub(t,
		"printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n", museNoYoloHelpFixture)
	if err := directSeal(t, knownBinary, knownEnv, "1.4.1"); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("direct seal of declaration-less known build err = %v, want ErrPermissionModeUnsupported", err)
	}
}

// TestNativeForwardsVerbatimOnUnlistedRelease pins native on an unlisted
// build: no help evidence is consulted — the binary documents no bypass
// flag — and native arguments forward byte for byte, even a
// caller-supplied bypass spelling. The sealed posture is native because
// the request said native, not because argv was scanned.
func TestNativeForwardsVerbatimOnUnlistedRelease(t *testing.T) {
	t.Parallel()
	_, env := writeMuseVersionHelpStub(t,
		"printf '%s\\n' 'Muse Code 9.9.9 (9.9.9-R1.1)'\n", museNoYoloHelpFixture)
	native := []string{"do", "--yolo", "the thing"}
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "9.9.9", PermissionMode: agentic.PermissionModeNative,
		NativeArgs: native,
	})
	suffix := plan.Argv[len(plan.Argv)-len(native):]
	for i := range native {
		if suffix[i] != native[i] {
			t.Fatalf("argv suffix = %q, want the verbatim native arguments %q", suffix, native)
		}
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec on the unlisted native plan: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedModeKey] != string(agentic.PermissionModeNative) {
		t.Fatalf("sealed mode = %q, want native despite the caller-supplied bypass spelling",
			seal.Data.Sealed.Selectors[sealedModeKey])
	}
	if _, present := seal.Data.Sealed.Selectors[sealedHelpStdoutKey]; present {
		t.Fatal("native seal carries help selectors it must never bind")
	}
}

// TestSealRefusesToolReleaseMismatch pins the caller-release cross-check:
// a supplied ToolRelease must equal the attested release in both
// postures, while a matching or empty claim plans. The 1.5.0 witness is
// the M44 mutant's exemption; the 9.9.9 control still refuses under it.
func TestSealRefusesToolReleaseMismatch(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			for _, claimed := range []string{"1.5.0", "9.9.9"} {
				req := museInteractiveRequest(t)
				req.PermissionMode = mode
				req.ToolRelease = claimed
				_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
				if !errors.Is(err, ErrMuseToolReleaseMismatch) {
					t.Fatalf("BuildPlan claiming %q err = %v, want ErrMuseToolReleaseMismatch", claimed, err)
				}
			}
			for _, claimed := range []string{"1.4.1", ""} {
				req := museInteractiveRequest(t)
				req.PermissionMode = mode
				req.ToolRelease = claimed
				plan, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
				if err != nil {
					t.Fatalf("BuildPlan claiming %q: %v", claimed, err)
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatalf("VerifyBeforeExec claiming %q: %v", claimed, err)
				}
			}
		})
	}
}
