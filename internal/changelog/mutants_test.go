package changelog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChangelogNarrowingMutants runs real behavioral tests in child test
// processes against copies of the production script. No repository file is
// mutated. Each one-member widening must fail its named witness while the
// positive control stays green; syntax/launch failures do not count as kills.
//
// A witness may name a parent/subtest pair (the precise one-member witness
// for a widening that admits exactly one read); the parent body still runs
// and the named subtest must fail. The full witness still runs in the
// normal suite, so narrowing a child run changes no coverage.

// changelogChildOutcome is one bounded child test run: combined output, exit
// code, and launch/timeout error.
type changelogChildOutcome struct {
	out                           string
	stdout, stderr                string
	exit                          int
	err                           error
	attested, timedOut, truncated bool
}

// changelogKillVerdict is the mutant-harness kill decision. A kill requires a
// witness that failed by name with no timeout attestation at any nesting level,
// and a control that passed with a clean attestation.
type changelogKillVerdict struct {
	kill   bool
	reason string
}

// A truncated result needs an explicit clean out-of-band signal, as does an
// untruncated result: missing evidence is unknown, never absence of timeout.
func changelogAttestationAllowsKill(outcome changelogChildOutcome) bool {
	return outcome.attested
}

// classifyChangelogKill is the single kill-decision site shared by the mutant
// harness and the F1 regression. A plain exit-plus-FAIL check is not enough:
// a TIMEOUT inside a child behavioral test makes the child test binary exit 1
// with its named FAIL, which is indistinguishable from a genuine kill without
// the cross-process timeout signal. The classifier therefore refuses to
// credit a kill whenever either child tree attests a timeout,
// however deep the timeout nested.
func classifyChangelogKill(mutant, witnessName, controlName string, witness, control changelogChildOutcome) changelogKillVerdict {
	if witness.err != nil {
		if timeout, ok := asChangelogTimeout(witness.err); ok {
			return changelogKillVerdict{reason: fmt.Sprintf(
				"mutant %s: witness %s TIMEOUT after %s, not a kill (a hung witness never counts as a kill):\n%s",
				mutant, witnessName, timeout.elapsed, timeout)}
		}
		return changelogKillVerdict{reason: fmt.Sprintf("launch failure, not a kill: %v", witness.err)}
	}
	if witness.exit == 0 || !strings.Contains(witness.out, "--- FAIL: "+witnessName) {
		return changelogKillVerdict{reason: fmt.Sprintf(
			"SURVIVOR or unnamed failure: witness %s exit %d\n%s", witnessName, witness.exit, witness.out)}
	}
	if witness.timedOut {
		return changelogKillVerdict{reason: fmt.Sprintf(
			"mutant %s: witness %s tree attests a changelog TIMEOUT, not a kill (a timeout at any nesting level never counts as a kill):\n%s",
			mutant, witnessName, witness.out)}
	}
	if control.err != nil {
		if timeout, ok := asChangelogTimeout(control.err); ok {
			return changelogKillVerdict{reason: fmt.Sprintf(
				"mutant %s: control %s TIMEOUT after %s, not a kill (a hung control fails the subtest):\n%s",
				mutant, controlName, timeout.elapsed, timeout)}
		}
		return changelogKillVerdict{reason: fmt.Sprintf("launch failure, not a kill: %v", control.err)}
	}
	if control.exit != 0 {
		return changelogKillVerdict{reason: fmt.Sprintf(
			"positive control %s exit %d; not a narrowing kill\n%s", controlName, control.exit, control.out)}
	}
	if control.timedOut {
		return changelogKillVerdict{reason: fmt.Sprintf(
			"mutant %s: control %s tree attests a changelog TIMEOUT, not a kill (a timeout at any nesting level never counts as a kill):\n%s",
			mutant, controlName, control.out)}
	}
	if !changelogAttestationAllowsKill(witness) || !changelogAttestationAllowsKill(control) {
		return changelogKillVerdict{reason: "unknown timeout attestation, not a kill"}
	}
	return changelogKillVerdict{kill: true, reason: fmt.Sprintf(
		"mutant %s: witness %s exit %d (expected-red); control %s exit 0",
		mutant, witnessName, witness.exit, controlName)}
}

func TestChangelogNarrowingMutants(t *testing.T) {
	if os.Getenv("CHANGELOG_TEST_SCRIPT") != "" {
		t.Skip("child behavioral run")
	}
	original := readFile(t, scriptPath(t))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	type mutant struct{ name, from, to, witness, control string }
	check := "TestCheckAcceptsValidFragments"
	release := "TestReleaseWritesSectionRemovesFragments"
	mutants := []mutant{
		{"name-underscore", "*[!A-Za-z0-9-]*", "*[!A-Za-z0-9_-]*", "TestCheckRefusesBadName/bad_name.md", check},
		{"empty-file", "  [ -s \"$path\" ] ||", "  [ \"$base\" = empty.md ] && return 0\n  [ -s \"$path\" ] ||", "TestCheckRefusesEmptyFragment", check},
		{"utf8-one-file", "  if command -v iconv", "  if [ \"$base\" = bad-utf8.md ]; then :\n  elif command -v iconv", "TestCheckRefusesInvalidUTF8", check},
		{"newline-one-file", "  if [ \"$(tail -c 1 \"$path\" | wc -l)\" -eq 0 ]; then", "  if [ \"$base\" != no-newline.md ] && [ \"$(tail -c 1 \"$path\" | wc -l)\" -eq 0 ]; then", "TestCheckRefusesMissingTrailingNewline/no-newline.md", check},
		{"heading-one-line", "    /^#/ { fail", "    $0 == \"# Heading\" { next }\n    /^#/ { fail", "TestCheckRefusesHeading/head.md", check},
		{"frontmatter-one-file", "  LC_ALL=C awk -v path=", "  [ \"$base\" = matter.md ] && return 0\n  LC_ALL=C awk -v path=", "TestCheckRefusesFrontMatter", check},
		{"prose-one-line", "    /^[ \\t]*$/ { next }", "    $0 == \"Just a paragraph.\" { bullets++; next }\n    /^[ \\t]*$/ { next }", "TestCheckRefusesNonBulletContent/plain.md", check},
		{"version-latest", "validate_version() {\n", "validate_version() {\n  [ \"$1\" = latest ] && return 0\n", "TestReleaseRefusesInvalidVersion/version=\"latest\"", release},
		{"date-april31", "    4|6|9|11) max=30 ;;", "    4) max=31 ;;\n    6|9|11) max=30 ;;", "TestReleaseRefusesInvalidDate/date=\"2026-04-31\"", release},
		{"duplicate-large-only", "    END { print found ? \"yes\" : \"no\" }", "    END { print (found && NR < 10000) ? \"yes\" : \"no\" }", "TestReleaseRefusesDuplicateVersionInLargeChangelog", release},
		// The version and header tokens stay present, but the matched header is
		// conditionally ignored. This attacks behavior, never a static grep guard.
		{"duplicate-token-preserved", "    /^## / && $0 ~ pattern { found = 1 }", "    /^## / && $0 ~ pattern && $0 != \"## v8.8.8\" { found = 1 }", "TestReleaseRefusesDuplicateVersion/legacy_bare_header_collides", "TestReleaseDuplicateCheckIgnoresBulletsAndSubstrings"},
		{"duplicate-one-version", "    /^## / && $0 ~ pattern { found = 1 }", "    /^## / && $0 ~ pattern && version != \"v0[.]1[.]0\" { found = 1 }", "TestReleaseRefusesDuplicateVersionInLargeChangelog", release},
		{"dirty-one-untracked", "  if [ -n \"$status\" ]; then", "  if [ -n \"$status\" ] && [ \"$status\" != \"?? untracked.txt\" ]; then", "TestReleaseRefusesDirtyTree/untracked_file", release},
		{"empty-one-version", "  if [ ! -s \"$list_tmp\" ] && [ \"$allow_empty\" != \"yes\" ]; then", "  if [ ! -s \"$list_tmp\" ] && [ \"$allow_empty\" != \"yes\" ] && [ \"$version\" != v0.2.0 ]; then", "TestReleaseRefusesEmptyWithoutAllowEmpty/carry-over_only_still_refuses", release},
		{"ignored-gate-only", "        die \"fragment $entry is ignored by .gitignore; remove the ignore and commit it before release\"", "        : # admit ignored paths at the committed-fragment gate", "TestReleaseRefusesIgnoredFragment", release},
		{"missing-unreleased-one-version", "  grep -E -q \"^## Unreleased$\" \"$CHANGELOG\" || die", "  grep -E -q \"^## Unreleased$\" \"$CHANGELOG\" || [ \"$version\" = v1.0.0 ] || die", "TestReleaseRefusesMissingUnreleasedSection", release},
		{"unknown-history-sentinel", "      echo \"$PROG: git log has no add history for ${order_frags[$i]}\" >&2\n      return 1", "      order_lasts[$i]=\"999999999\"", "TestReleaseRefusesUnknownAddHistory", release},
		{"root-history-hidden", "git_read_file \"$history_tmp\" log --root", "git_read_file \"$history_tmp\" -c log.showRoot=false log", "TestReleaseOrdersRootAddUnderLocalShowRootFalse", release},
		{"encoding-local", "-c i18n.logOutputEncoding=UTF-8", "-c core.quotePath=false", "TestReleasePinsLocalLogOutputEncoding", release},
		{"readd-first-lifetime", "    if [ \"$name\" = \"${order_frags[$i]}\" ]; then order_lasts[$i]=\"$seq\"; fi", "    if [ \"$name\" = \"${order_frags[$i]}\" ] && [ -z \"${order_lasts[$i]}\" ]; then order_lasts[$i]=\"$seq\"; fi", "TestReleaseOrdersReaddedFragmentByReadd", release},
		{"rename-detection", "log --root --no-color --no-renames", "log --root --no-color --find-renames=50%", "TestReleaseOrdersRenamedFragmentByNewName/default", release},
		{"check-ignore-128", "          1) die \"fragment", "          128) : ;;\n          1) die \"fragment", "TestReleaseRefusesEachFailedGitRead/ignored_branch_check_ignore", release},
		// An LF split at the check call site: the path is truncated at the
		// first newline, so the LF-bearing entry validates its valid prefix
		// twice instead of refusing.
		{"lf-split-check", "    if ! check_fragment \"$entry\" \"$base\"; then", "    entry=\"${entry%%$NL*}\"\n    base=\"${entry##*/}\"\n    if ! check_fragment \"$entry\" \"$base\"; then", "TestCheckRefusesNewlineFragmentName", check},
		// A working-tree read in release: the symlink gates are skipped and
		// the blob fetch becomes a symlink-following copy, so uncommitted
		// ignored target bytes are published. The tracked/ignored gates
		// above the block stay intact, and the hidden-state gate still runs
		// for every path except the N2 link, so only this member is admitted.
		{"committed-content-worktree", "    if [ -L \"$entry\" ]; then\n      die \"fragment $entry is a symlink; symlink fragments are not allowed\"\n    fi\n    ls_rec=\"\"\n    IFS= read -r -d '' ls_rec <\"$ls_tmp\" || [ -n \"$ls_rec\" ]\n    case \"$ls_rec\" in\n      120000\\ *) die \"fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed\" ;;\n      100644\\ *|100755\\ *) : ;;\n      *) die \"fragment $entry is not a committed regular file; nothing released\" ;;\n    esac\n    git_read_file \"$tree_tmp\" ls-tree -z HEAD -- \"$entry\"\n    if [ -s \"$tree_tmp\" ]; then\n      tree_rec=\"\"\n      IFS= read -r -d '' tree_rec <\"$tree_tmp\" || [ -n \"$tree_rec\" ]\n      case \"$tree_rec\" in\n        120000\\ *) die \"fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed\" ;;\n      esac\n    fi\n    refuse_hidden_state \"$entry\" \"$flag_tmp\"\n    base=\"${entry##*/}\"\n    blob=\"$blobdir/$base\"\n    git_read_file \"$blob\" cat-file blob \"HEAD:$entry\"", "    base=\"${entry##*/}\"\n    blob=\"$blobdir/$base\"\n    if [ \"$entry\" != \"changelog.d/link.md\" ]; then\n      refuse_hidden_state \"$entry\" \"$flag_tmp\"\n    fi\n    cp \"$entry\" \"$blob\"", "TestReleaseRefusesSymlinkIgnoredTarget/panel1", release},
		{"symlink-one-name", "  if [ -L \"$path\" ]; then", "  if [ -L \"$path\" ] && [ \"$base\" != link.md ]; then", "TestCheckPanelGrammarProbes/symlink", check},
		// P1: the hidden-state gate (flag + byte) is skipped for one fragment
		// path, so its assume/skip edit releases. Other paths still refuse.
		{"hidden-state-one-path", "refuse_hidden_state() {\n  local path=\"$1\" flag_tmp=\"$2\" rec=\"\" tag work_hash head_hash", "refuse_hidden_state() {\n  local path=\"$1\" flag_tmp=\"$2\" rec=\"\" tag work_hash head_hash\n  if [ \"$path\" = \"changelog.d/a.md\" ]; then return 0; fi", "TestReleaseRefusesHiddenFragmentEdits/--assume-unchanged", release},
		// P1 flag branch: assume-unchanged (lowercase tags) admitted while
		// skip-worktree still refuses. Killed by the flag-without-edit case
		// where bytes match and only the flag gate can refuse.
		{"hidden-flag-assume", "      [a-z]|S) die \"$path carries hidden index state", "      S) die \"$path carries hidden index state", "TestReleaseRefusesHiddenFragmentEdits/flag-without-edit-still-refuses", release},
		// P2: both git-mode checks in --check are skipped for one fragment
		// name, so its mode-120000 entry validates. Other names still refuse.
		{"check-mode-one-name", "    if ! check_index_symlink \"$entry\" \"$ls_tmp\"; then\n      bad=1\n      continue\n    fi\n    if [ \"$HEAD_EXISTS\" = \"yes\" ]; then\n      if ! check_head_symlink \"$entry\" \"$tree_tmp\"; then\n        bad=1\n        continue\n      fi\n    fi", "    if [ \"$base\" != link.md ]; then\n      if ! check_index_symlink \"$entry\" \"$ls_tmp\"; then\n        bad=1\n        continue\n      fi\n      if [ \"$HEAD_EXISTS\" = \"yes\" ]; then\n        if ! check_head_symlink \"$entry\" \"$tree_tmp\"; then\n          bad=1\n          continue\n        fi\n      fi\n    fi", "TestCheckRefusesSymlinkModeUnderNoSymlinksConfig", check},
	}
	// Each failed-read widening admits exactly one git command's exit-128
	// read, so its witness is the FIRST read_N subtest for that command
	// (positions pinned by the inventory assertion in the witness itself):
	// one member admitted, one member witnessed.
	failedReadWitness := map[string]string{
		"rev-parse":   "TestReleaseRefusesEachFailedGitRead/read_1_rev-parse",
		"status":      "TestReleaseRefusesEachFailedGitRead/read_2_status",
		"hash-object": "TestReleaseRefusesEachFailedGitRead/read_4_hash-object",
		"ls-files":    "TestReleaseRefusesEachFailedGitRead/read_3_ls-files",
		"ls-tree":     "TestReleaseRefusesEachFailedGitRead/read_7_ls-tree",
		"log":         "TestReleaseRefusesEachFailedGitRead/read_18_log",
		"cat-file":    "TestReleaseRefusesEachFailedGitRead/read_11_cat-file",
	}
	for _, command := range []string{"rev-parse", "status", "hash-object"} {
		mutants = append(mutants, mutant{
			"failed-read-" + command,
			"    git_status=\"$?\"\n",
			"    git_status=\"$?\"\n    if [ \"$1\" = " + command + " ] && [ \"$git_status\" -eq 128 ]; then\n      printf -v \"$target\" '%s' \"$git_output\"\n      return 0\n    fi\n",
			failedReadWitness[command], release,
		})
	}
	fileAnchor := "    code=\"$?\"\n    die \"git $* failed (exit $code); nothing released\"\n"
	for _, command := range []string{"ls-files", "ls-tree", "log", "cat-file"} {
		mutants = append(mutants, mutant{
			"failed-read-" + command,
			fileAnchor,
			"    code=\"$?\"\n    if [ \"$1\" = " + command + " ] && [ \"$code\" -eq 128 ]; then\n      return 0\n    fi\n    die \"git $* failed (exit $code); nothing released\"\n",
			failedReadWitness[command], release,
		})
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			// Deliberately sequential: one mutant at a time, so the
			// suite's child fan-out stays flat regardless of the
			// package-wide admission cap.
			if n := strings.Count(original, m.from); n != 1 {
				t.Fatalf("mutation anchor count %d, want 1", n)
			}
			script := filepath.Join(t.TempDir(), "changelog-release.sh")
			if err := os.WriteFile(script, []byte(strings.Replace(original, m.from, m.to, 1)), 0o755); err != nil {
				t.Fatal(err)
			}
			if stdout, stderr, exit, err := runChangelogChild("", fixtureEnv(t), changelogChildTimeout(), "bash", "-n", script); err != nil || exit != 0 {
				t.Fatalf("invalid mutant, not a kill: %v exit %d\nstdout: %s\nstderr: %s", err, exit, stdout, stderr)
			}
			// Every child runs under the per-run deadline in its registered
			// process subtree. A timeout is TIMEOUT, not a kill: a hung
			// witness never counts as a kill, and a hung control fails
			// the subtest. A witness naming a parent/subtest pair runs
			// only that subtest (the parent body still runs). Witness
			// and control are independent children (separate fixtures,
			// one shared script) run SEQUENTIALLY: one child at a time
			// per mutant, so the suite never fans out past the
			// package-wide cap even before the gate is reached. The
			// shared classifier reads witness first, then control, and
			// refuses any outcome carrying the cross-process timeout
			// marker.
			run := func(env []string, name string) changelogChildOutcome {
				pattern := "^" + name + "$"
				if parent, sub, ok := strings.Cut(name, "/"); ok {
					pattern = "^" + parent + "$/^" + sub + "$"
				}
				return runChangelogOutcome("",
					env, changelogChildTimeout(),
					executable, "-test.count=1", "-test.v", "-test.run="+pattern)
			}
			childEnv := append(fixtureEnv(t), "CHANGELOG_TEST_SCRIPT="+script)
			witness := run(childEnv, m.witness)
			control := run(childEnv, m.control)
			verdict := classifyChangelogKill(m.name, m.witness, m.control, witness, control)
			if !verdict.kill {
				t.Fatalf("%s", verdict.reason)
			}
			t.Logf("%s", verdict.reason)
		})
	}
}

// TestChangelogClassifierRefusesNestedTimeout is the F1 regression: a TIMEOUT
// inside a child behavioral test (a 1ns child bound for the witness only, the
// outer bound and the control unchanged) makes the child test binary exit 1
// with its named FAIL, and the shared kill classifier must refuse to credit
// it as a kill. The witness runs against the ORIGINAL unmutated script, which
// can never be killed, so any credited kill is a false positive by
// construction. Production call sites: runChangelogChild in bounded_test.go
// launches both children; classifyChangelogKill decides.
func TestChangelogClassifierRefusesNestedTimeout(t *testing.T) {
	if os.Getenv("CHANGELOG_TEST_SCRIPT") != "" {
		t.Skip("child behavioral run")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const witness = "TestCheckRefusesEmptyFragment"
	const control = "TestCheckAcceptsValidFragments"
	run := func(env []string, name string) changelogChildOutcome {
		return runChangelogOutcome("",
			env, changelogChildTimeout(),
			executable, "-test.count=1", "-test.v", "-test.run=^"+name+"$")
	}
	witnessEnv := append(fixtureEnv(t), "CHANGELOG_TEST_CHILD_TIMEOUT=1ns")
	witnessOut := run(witnessEnv, witness)
	controlOut := run(fixtureEnv(t), control)
	if witnessOut.err != nil {
		t.Fatalf("F1 setup: outer witness run failed to complete: %v", witnessOut.err)
	}
	// Red premise: without the marker check this outcome WOULD credit — the
	// child really did exit 1 with the named FAIL.
	if witnessOut.exit == 0 || !strings.Contains(witnessOut.out, "--- FAIL: "+witness) {
		t.Fatalf("F1 setup: 1ns witness did not fail by name (exit %d); the regression proves nothing without the false-kill shape:\n%s",
			witnessOut.exit, witnessOut.out)
	}
	if !changelogOutputMarksTimeout(witnessOut.out) {
		t.Fatalf("F1 setup: 1ns witness output carries no TIMEOUT marker:\n%s", witnessOut.out)
	}
	if controlOut.err != nil || controlOut.exit != 0 {
		t.Fatalf("F1 setup: control %s exit %d err %v:\n%s", control, controlOut.exit, controlOut.err, controlOut.out)
	}
	verdict := classifyChangelogKill("F1-probe(no-mutation)", witness, control, witnessOut, controlOut)
	if verdict.kill {
		t.Fatalf("nested TIMEOUT credited as a kill (the unmutated script can never be killed):\n%s", verdict.reason)
	}
	if !strings.Contains(verdict.reason, "TIMEOUT") {
		t.Errorf("refusal reason names no TIMEOUT:\n%s", verdict.reason)
	}
	t.Logf("nested timeout refused by name: %.160s", verdict.reason)
}

// TestChangelogClassifierRefusesControlTimeout pins the control half of the
// cross-process timeout signal: a control that passed but carries a TIMEOUT
// marker must still refuse the kill. A passing-but-marked control cannot
// arise from a real child (runScript/runGit fail the test on a timeout), so
// the marked side is a structurally faithful synthetic outcome; the
// end-to-end marker propagation is proven by the F1 regression above. Pure
// decision test — no children.
func TestChangelogClassifierRefusesControlTimeout(t *testing.T) {
	witness := changelogChildOutcome{attested: true,
		out:  "=== RUN   TestCheckRefusesEmptyFragment\n--- FAIL: TestCheckRefusesEmptyFragment\n",
		exit: 1,
	}
	control := changelogChildOutcome{attested: true, timedOut: true,
		out: "=== RUN   TestCheckAcceptsValidFragments\n" +
			"running git [status --porcelain]: changelog test child TIMEOUT after 5s: git status\n" +
			"--- PASS: TestCheckAcceptsValidFragments\n",
		exit: 0,
	}
	verdict := classifyChangelogKill("probe", "TestCheckRefusesEmptyFragment", "TestCheckAcceptsValidFragments", witness, control)
	if verdict.kill {
		t.Fatalf("marked control credited as a kill:\n%s", verdict.reason)
	}
	if !strings.Contains(verdict.reason, "TIMEOUT") {
		t.Errorf("refusal reason names no TIMEOUT:\n%s", verdict.reason)
	}
}

// TestChangelogClassifierCreditsCleanKill pins the no-false-refusal half: a
// witness that failed by name with no marker and a control that passed with
// no marker credit a kill. Pure decision test — no children.
func TestChangelogClassifierCreditsCleanKill(t *testing.T) {
	witness := changelogChildOutcome{attested: true,
		out:  "=== RUN   TestCheckRefusesEmptyFragment\n--- FAIL: TestCheckRefusesEmptyFragment\n",
		exit: 1,
	}
	control := changelogChildOutcome{attested: true,
		out:  "=== RUN   TestCheckAcceptsValidFragments\n--- PASS: TestCheckAcceptsValidFragments\n",
		exit: 0,
	}
	verdict := classifyChangelogKill("probe", "TestCheckRefusesEmptyFragment", "TestCheckAcceptsValidFragments", witness, control)
	if !verdict.kill {
		t.Fatalf("clean witness/control refused:\n%s", verdict.reason)
	}
}

// TestChangelogClassifierMutantsKilled proves the marker checks are
// load-bearing. Each mutant splices classifyChangelogKill via a go overlay
// (the splice's only change) and the witness command runs the F1 regression
// plus both decision unit tests:
//   - marker-ignored drops BOTH marker checks: the F1 regression and the
//     control unit must fail (both timeout shapes credited), while the clean
//     positive still passes (the mutant weakens refusal, it does not break
//     clean kills).
//   - witness-marker-ignored drops only the witness check (narrowing: the
//     gate stays present on the control side and admits exactly the
//     witness-marked member): the F1 regression must fail while the control
//     unit and the clean positive still pass.
func TestChangelogClassifierMutantsKilled(t *testing.T) {
	const (
		nested   = "TestChangelogClassifierRefusesNestedTimeout"
		control  = "TestChangelogClassifierRefusesControlTimeout"
		positive = "TestChangelogClassifierCreditsCleanKill"
	)
	const (
		witnessCheck = "\tif witness.timedOut {\n"
		controlCheck = "\tif control.timedOut {\n"
	)
	mutants := []struct {
		id              string
		splices         [][2]string
		wantNestedFail  bool
		wantControlFail bool
	}{
		{"marker-ignored", [][2]string{
			{witnessCheck, "\tif false { // witness timeout marker ignored\n"},
			{controlCheck, "\tif false { // control timeout marker ignored\n"},
		}, true, true},
		{"witness-marker-ignored", [][2]string{
			{witnessCheck, "\tif false { // witness timeout marker ignored\n"},
		}, true, false},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			// Deliberately sequential: one overlay mutant run at a
			// time, like the narrowing subtests above.
			root := repoRoot(t)
			file := filepath.Join(root, "internal", "changelog", "mutants_test.go")
			combined, _ := runOverlayMutant(t, m.id, file, m.splices,
				"^("+nested+"|"+control+"|"+positive+")$")
			nestedFailed := strings.Contains(combined, "--- FAIL: "+nested)
			controlFailed := strings.Contains(combined, "--- FAIL: "+control)
			positivePassed := strings.Contains(combined, "--- PASS: "+positive)
			if nestedFailed != m.wantNestedFail || controlFailed != m.wantControlFail || !positivePassed {
				t.Fatalf("%s mutant: unexpected witness split (nested FAIL=%t want %t, control FAIL=%t want %t, positive PASS=%t want true):\n%s",
					m.id, nestedFailed, m.wantNestedFail, controlFailed, m.wantControlFail, positivePassed, combined)
			}
			t.Logf("%s mutant killed by the named witness split (expected-red)", m.id)
		})
	}
}
