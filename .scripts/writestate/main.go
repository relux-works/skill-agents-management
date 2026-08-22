// Command writestate is this repository's half of the cross-binary round trip.
//
// It writes limit state with the PORTED plane, through the same production
// entry points the extraction source's harness uses on its side
// (.scripts/limitstate_xrt.go): a captured provider transcript is classified,
// the classification mints a quota observation, and Store.Observe persists it.
// The source's own Report is then run over the result, and its projection is
// committed as a fixture.
//
// It lives under .scripts rather than tools/ because it is capture tooling, not
// a shipped command: `go build ./...` skips dot-directories, so it never
// reaches the CLI binary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pl "github.com/relux-works/skill-agents-management/pkg/providerlimits"
)

// fixedNow is the same instant .scripts/limitstate_xrt.go anchors on. The two
// harnesses have to agree on it or the two sides of the round trip would differ
// by a clock rather than by a schema.
var fixedNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

const (
	claudeHome = "/tmp/providerlimits-xrt/claude-home"
	codexHome  = "/tmp/providerlimits-xrt/codex-home"
)

func main() {
	stateRoot := flag.String("state-root", "", "parent of task-board/provider-limits")
	fixtures := flag.String("fixtures", "pkg/providerlimits/testdata", "this package's testdata directory")
	flag.Parse()
	if *stateRoot == "" {
		fail(fmt.Errorf("--state-root is required"))
	}
	fail(write(*stateRoot, *fixtures))
}

func write(stateRoot, fixtures string) error {
	store, err := pl.NewStore(pl.Options{
		Layout: pl.LayoutAt(stateRoot),
		Now:    func() time.Time { return fixedNow },
	})
	if err != nil {
		return err
	}
	table := pl.MustGroupTable()

	claudeStdout, err := os.ReadFile(filepath.Join(fixtures, "claude-429-usage-limit.json"))
	if err != nil {
		return err
	}
	claudeIdentity, err := pl.IdentityFor(pl.ProviderClaude, claudeHome)
	if err != nil {
		return err
	}
	const claudeModel = "claude-opus-5"
	claudeClass := pl.ClassifyClaudePrompt(pl.ClaudePromptResult{ExitCode: 1, Stdout: claudeStdout, Now: fixedNow})
	claudeObs, ok := claudeClass.QuotaObservation(pl.Evidence{
		RunID: "RUN-260822-2jouz3", TaskID: "TASK-260822-2jouz3", Model: claudeModel,
	})
	if !ok {
		return fmt.Errorf("the captured claude 429 fixture did not classify as provider quota (class=%q reason=%q)", claudeClass.Class, claudeClass.Reason)
	}
	if err := store.Observe(claudeIdentity, table.Group(pl.ProviderClaude, claudeModel), nil, claudeObs); err != nil {
		return err
	}

	codexLog, err := os.ReadFile(filepath.Join(fixtures, "codex-usage-limit-RUN-260801-a714a6.log"))
	if err != nil {
		return err
	}
	codexIdentity, err := pl.IdentityFor(pl.ProviderCodex, codexHome)
	if err != nil {
		return err
	}
	const codexModel = "gpt-5.5"
	codexClass := pl.ClassifyCodexPrompt(pl.CodexPromptResult{ExitCode: 1, Log: codexLog, Now: fixedNow})
	codexObs, ok := codexClass.QuotaObservation(pl.Evidence{
		RunID: "RUN-260822-2jouz4", TaskID: "TASK-260822-2jouz3", Model: codexModel,
	})
	if !ok {
		return fmt.Errorf("the captured codex usage-limit fixture did not classify as provider quota (class=%q reason=%q)", codexClass.Class, codexClass.Reason)
	}
	if err := store.Observe(codexIdentity, table.Group(pl.ProviderCodex, codexModel), nil, codexObs); err != nil {
		return err
	}

	manifest := []map[string]string{
		{"provider": pl.ProviderClaude, "home_normalized": claudeIdentity.Home, "identity_key": claudeIdentity.Key},
		{"provider": pl.ProviderCodex, "home_normalized": codexIdentity.Home, "identity_key": codexIdentity.Key},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateRoot, "task-board", "provider-limits", "ours-manifest.json"), append(data, '\n'), 0o644)
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "writestate:", err)
		os.Exit(1)
	}
}
