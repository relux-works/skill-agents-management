package changelog_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestChangelogNarrowingMutants runs real behavioral tests in child test
// processes against copies of the production script. No repository file is
// mutated. Each one-member widening must fail its named witness while the
// positive control stays green; syntax/launch failures do not count as kills.
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
		{"name-underscore", "*[!A-Za-z0-9-]*", "*[!A-Za-z0-9_-]*", "TestCheckRefusesBadName", check},
		{"empty-file", "  [ -s \"$path\" ] ||", "  [ \"$base\" = empty.md ] && return 0\n  [ -s \"$path\" ] ||", "TestCheckRefusesEmptyFragment", check},
		{"utf8-one-file", "  if command -v iconv", "  if [ \"$base\" = bad-utf8.md ]; then :\n  elif command -v iconv", "TestCheckRefusesInvalidUTF8", check},
		{"newline-one-file", "  if [ \"$(tail -c 1 \"$path\" | wc -l)\" -eq 0 ]; then", "  if [ \"$base\" != no-newline.md ] && [ \"$(tail -c 1 \"$path\" | wc -l)\" -eq 0 ]; then", "TestCheckRefusesMissingTrailingNewline", check},
		{"heading-one-line", "    /^#/ { fail", "    $0 == \"# Heading\" { next }\n    /^#/ { fail", "TestCheckRefusesHeading", check},
		{"frontmatter-one-file", "  LC_ALL=C awk -v path=", "  [ \"$base\" = matter.md ] && return 0\n  LC_ALL=C awk -v path=", "TestCheckRefusesFrontMatter", check},
		{"prose-one-line", "    /^[ \\t]*$/ { next }", "    $0 == \"Just a paragraph.\" { bullets++; next }\n    /^[ \\t]*$/ { next }", "TestCheckRefusesNonBulletContent", check},
		{"version-latest", "validate_version() {\n", "validate_version() {\n  [ \"$1\" = latest ] && return 0\n", "TestReleaseRefusesInvalidVersion", release},
		{"date-april31", "    4|6|9|11) max=30 ;;", "    4) max=31 ;;\n    6|9|11) max=30 ;;", "TestReleaseRefusesInvalidDate", release},
		{"duplicate-large-only", "    END { print found ? \"yes\" : \"no\" }", "    END { print (found && NR < 10000) ? \"yes\" : \"no\" }", "TestReleaseRefusesDuplicateVersionInLargeChangelog", release},
		// The version and header tokens stay present, but the matched header is
		// conditionally ignored. This attacks behavior, never a static grep guard.
		{"duplicate-token-preserved", "    /^## / && $0 ~ pattern { found = 1 }", "    /^## / && $0 ~ pattern && $0 != \"## v8.8.8\" { found = 1 }", "TestReleaseRefusesDuplicateVersion", "TestReleaseDuplicateCheckIgnoresBulletsAndSubstrings"},
		{"duplicate-one-version", "    /^## / && $0 ~ pattern { found = 1 }", "    /^## / && $0 ~ pattern && version != \"v0[.]1[.]0\" { found = 1 }", "TestReleaseRefusesDuplicateVersionInLargeChangelog", release},
		{"dirty-one-untracked", "  if [ -n \"$status\" ]; then", "  if [ -n \"$status\" ] && [ \"$status\" != \"?? untracked.txt\" ]; then", "TestReleaseRefusesDirtyTree", release},
		{"empty-one-version", "  if [ ! -s \"$list_tmp\" ] && [ \"$allow_empty\" != \"yes\" ]; then", "  if [ ! -s \"$list_tmp\" ] && [ \"$allow_empty\" != \"yes\" ] && [ \"$version\" != v0.2.0 ]; then", "TestReleaseRefusesEmptyWithoutAllowEmpty", release},
		{"ignored-gate-only", "        die \"fragment $entry is ignored by .gitignore; remove the ignore and commit it before release\"", "        : # admit ignored paths at the committed-fragment gate", "TestReleaseRefusesIgnoredFragment", release},
		{"missing-unreleased-one-version", "  grep -E -q \"^## Unreleased$\" \"$CHANGELOG\" || die", "  grep -E -q \"^## Unreleased$\" \"$CHANGELOG\" || [ \"$version\" = v1.0.0 ] || die", "TestReleaseRefusesMissingUnreleasedSection", release},
		{"unknown-history-sentinel", "      echo \"$PROG: git log has no add history for ${order_frags[$i]}\" >&2\n      return 1", "      order_lasts[$i]=\"999999999\"", "TestReleaseRefusesUnknownAddHistory", release},
		{"root-history-hidden", "git_read_file \"$history_tmp\" log --root", "git_read_file \"$history_tmp\" -c log.showRoot=false log", "TestReleaseOrdersRootAddUnderLocalShowRootFalse", release},
		{"encoding-local", "-c i18n.logOutputEncoding=UTF-8", "-c core.quotePath=false", "TestReleasePinsLocalLogOutputEncoding", release},
		{"readd-first-lifetime", "    if [ \"$name\" = \"${order_frags[$i]}\" ]; then order_lasts[$i]=\"$seq\"; fi", "    if [ \"$name\" = \"${order_frags[$i]}\" ] && [ -z \"${order_lasts[$i]}\" ]; then order_lasts[$i]=\"$seq\"; fi", "TestReleaseOrdersReaddedFragmentByReadd", release},
		{"rename-detection", "log --root --no-color --no-renames", "log --root --no-color --find-renames=50%", "TestReleaseOrdersRenamedFragmentByNewName", release},
		{"check-ignore-128", "          1) die \"fragment", "          128) : ;;\n          1) die \"fragment", "TestReleaseRefusesEachFailedGitRead", release},
		// An LF split at the check call site: the path is truncated at the
		// first newline, so the LF-bearing entry validates its valid prefix
		// twice instead of refusing.
		{"lf-split-check", "    if ! check_fragment \"$entry\" \"$base\"; then", "    entry=\"${entry%%$NL*}\"\n    base=\"${entry##*/}\"\n    if ! check_fragment \"$entry\" \"$base\"; then", "TestCheckRefusesNewlineFragmentName", check},
		// A working-tree read in release: the symlink gates are skipped and
		// the blob fetch becomes a symlink-following copy, so uncommitted
		// ignored target bytes are published. The tracked/ignored gates
		// above the block stay intact, and the hidden-state gate still runs
		// for every path except the N2 link, so only this member is admitted.
		{"committed-content-worktree", "    if [ -L \"$entry\" ]; then\n      die \"fragment $entry is a symlink; symlink fragments are not allowed\"\n    fi\n    ls_rec=\"\"\n    IFS= read -r -d '' ls_rec <\"$ls_tmp\" || [ -n \"$ls_rec\" ]\n    case \"$ls_rec\" in\n      120000\\ *) die \"fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed\" ;;\n      100644\\ *|100755\\ *) : ;;\n      *) die \"fragment $entry is not a committed regular file; nothing released\" ;;\n    esac\n    git_read_file \"$tree_tmp\" ls-tree -z HEAD -- \"$entry\"\n    if [ -s \"$tree_tmp\" ]; then\n      tree_rec=\"\"\n      IFS= read -r -d '' tree_rec <\"$tree_tmp\" || [ -n \"$tree_rec\" ]\n      case \"$tree_rec\" in\n        120000\\ *) die \"fragment $entry is a symlink in git (mode 120000); symlink fragments are not allowed\" ;;\n      esac\n    fi\n    refuse_hidden_state \"$entry\" \"$flag_tmp\"\n    base=\"${entry##*/}\"\n    blob=\"$blobdir/$base\"\n    git_read_file \"$blob\" cat-file blob \"HEAD:$entry\"", "    base=\"${entry##*/}\"\n    blob=\"$blobdir/$base\"\n    if [ \"$entry\" != \"changelog.d/link.md\" ]; then\n      refuse_hidden_state \"$entry\" \"$flag_tmp\"\n    fi\n    cp \"$entry\" \"$blob\"", "TestReleaseRefusesSymlinkIgnoredTarget", release},
		{"symlink-one-name", "  if [ -L \"$path\" ]; then", "  if [ -L \"$path\" ] && [ \"$base\" != link.md ]; then", "TestCheckPanelGrammarProbes", check},
		// P1: the hidden-state gate (flag + byte) is skipped for one fragment
		// path, so its assume/skip edit releases. Other paths still refuse.
		{"hidden-state-one-path", "refuse_hidden_state() {\n  local path=\"$1\" flag_tmp=\"$2\" rec=\"\" tag work_hash head_hash", "refuse_hidden_state() {\n  local path=\"$1\" flag_tmp=\"$2\" rec=\"\" tag work_hash head_hash\n  if [ \"$path\" = \"changelog.d/a.md\" ]; then return 0; fi", "TestReleaseRefusesHiddenFragmentEdits", release},
		// P1 flag branch: assume-unchanged (lowercase tags) admitted while
		// skip-worktree still refuses. Killed by the flag-without-edit case
		// where bytes match and only the flag gate can refuse.
		{"hidden-flag-assume", "      [a-z]|S) die \"$path carries hidden index state", "      S) die \"$path carries hidden index state", "TestReleaseRefusesHiddenFragmentEdits", release},
		// P2: both git-mode checks in --check are skipped for one fragment
		// name, so its mode-120000 entry validates. Other names still refuse.
		{"check-mode-one-name", "    if ! check_index_symlink \"$entry\" \"$ls_tmp\"; then\n      bad=1\n      continue\n    fi\n    if [ \"$HEAD_EXISTS\" = \"yes\" ]; then\n      if ! check_head_symlink \"$entry\" \"$tree_tmp\"; then\n        bad=1\n        continue\n      fi\n    fi", "    if [ \"$base\" != link.md ]; then\n      if ! check_index_symlink \"$entry\" \"$ls_tmp\"; then\n        bad=1\n        continue\n      fi\n      if [ \"$HEAD_EXISTS\" = \"yes\" ]; then\n        if ! check_head_symlink \"$entry\" \"$tree_tmp\"; then\n          bad=1\n          continue\n        fi\n      fi\n    fi", "TestCheckRefusesSymlinkModeUnderNoSymlinksConfig", check},
	}
	for _, command := range []string{"rev-parse", "status", "hash-object"} {
		mutants = append(mutants, mutant{
			"failed-read-" + command,
			"    git_status=\"$?\"\n",
			"    git_status=\"$?\"\n    if [ \"$1\" = " + command + " ] && [ \"$git_status\" -eq 128 ]; then\n      printf -v \"$target\" '%s' \"$git_output\"\n      return 0\n    fi\n",
			"TestReleaseRefusesEachFailedGitRead", release,
		})
	}
	fileAnchor := "    code=\"$?\"\n    die \"git $* failed (exit $code); nothing released\"\n"
	for _, command := range []string{"ls-files", "ls-tree", "log", "cat-file"} {
		mutants = append(mutants, mutant{
			"failed-read-" + command,
			fileAnchor,
			"    code=\"$?\"\n    if [ \"$1\" = " + command + " ] && [ \"$code\" -eq 128 ]; then\n      return 0\n    fi\n    die \"git $* failed (exit $code); nothing released\"\n",
			"TestReleaseRefusesEachFailedGitRead", release,
		})
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			if n := strings.Count(original, m.from); n != 1 {
				t.Fatalf("mutation anchor count %d, want 1", n)
			}
			script := filepath.Join(t.TempDir(), "changelog-release.sh")
			if err := os.WriteFile(script, []byte(strings.Replace(original, m.from, m.to, 1)), 0o755); err != nil {
				t.Fatal(err)
			}
			syntax := exec.Command("bash", "-n", script)
			if out, err := syntax.CombinedOutput(); err != nil {
				t.Fatalf("invalid mutant, not a kill: %v: %s", err, out)
			}
			run := func(name string) (string, int) {
				c := exec.Command(executable, "-test.count=1", "-test.v", "-test.run=^"+name+"$")
				c.Env = append(fixtureEnv(), "CHANGELOG_TEST_SCRIPT="+script)
				out, err := c.CombinedOutput()
				if err == nil {
					return string(out), 0
				}
				if e, ok := err.(*exec.ExitError); ok {
					return string(out), e.ExitCode()
				}
				t.Fatalf("launch failure, not a kill: %v", err)
				return "", -1
			}
			out, exit := run(m.witness)
			if exit == 0 || !strings.Contains(out, "--- FAIL: "+m.witness) {
				t.Fatalf("SURVIVOR or unnamed failure: witness %s exit %d\n%s", m.witness, exit, out)
			}
			t.Logf("mutant %s: witness %s exit %d (expected-red)", m.name, m.witness, exit)
			out, exit = run(m.control)
			if exit != 0 {
				t.Fatalf("positive control %s exit %d; not a narrowing kill\n%s", m.control, exit, out)
			}
			t.Logf("control %s exit 0", m.control)
		})
	}
}
