// Command limitstate_xrt is the cross-binary round-trip harness.
//
// It is compiled against the EXTRACTION SOURCE's own providerlimits module —
// .scripts/capture-limit-state.sh copies it into a scratch module whose go.mod
// replaces that import path onto the source checkout — so everything it writes
// is written by the source's code through the source's production entry points,
// and everything it reads is read by them.
//
// It exists because "the port is byte-compatible" is not a claim a test in this
// repository can settle on its own: a round trip through one implementation
// agrees with itself no matter what the bytes are.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pl "github.com/relux-works/skill-project-management/pkg/providerlimits"
	rc "github.com/relux-works/skill-project-management/pkg/remoteconfig"
)

// fixedNow anchors every capture so the artifacts are reproducible: a
// re-capture that differs anywhere but in the source's own behaviour would
// otherwise show up as a diff on every run and teach the reader to ignore it.
var fixedNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

// The two homes are absolute and outside any checkout, so the identity keys the
// source computes for them are a function of the path string alone.
const (
	claudeHome = "/tmp/providerlimits-xrt/claude-home"
	codexHome  = "/tmp/providerlimits-xrt/codex-home"
)

type manifestEntry struct {
	Provider       string `json:"provider"`
	HomeGiven      string `json:"home_given"`
	HomeNormalized string `json:"home_normalized"`
	IdentityKey    string `json:"identity_key"`
	Group          string `json:"group"`
	Model          string `json:"model"`
	StateFile      string `json:"state_file"`
}

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: limitstate_xrt write|read [flags]"))
	}
	mode := os.Args[1]
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	stateRoot := fs.String("state-root", "", "parent of task-board/provider-limits")
	fixtures := fs.String("fixtures", "", "the source package's testdata directory")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fail(err)
	}
	if *stateRoot == "" && mode != "tables" {
		fail(fmt.Errorf("--state-root is required"))
	}
	switch mode {
	case "write":
		fail(write(*stateRoot, *fixtures))
	case "read":
		fail(read(*stateRoot))
	case "tables":
		fail(tables())
	default:
		fail(fmt.Errorf("unknown mode %q", mode))
	}
}

func newStore(stateRoot string) *pl.Store {
	store, err := pl.NewStore(pl.Options{
		Layout: pl.LayoutAt(stateRoot),
		Now:    func() time.Time { return fixedNow },
	})
	if err != nil {
		fail(err)
	}
	return store
}

// write drives the source's real detection path: a captured provider
// transcript is classified, the classification mints a quota observation, and
// Store.Observe persists the suppression. Nothing here hand-builds a record.
func write(stateRoot, fixtures string) error {
	if fixtures == "" {
		return fmt.Errorf("--fixtures is required in write mode")
	}
	store := newStore(stateRoot)
	table := pl.MustGroupTable()
	var manifest []manifestEntry

	// Claude: a captured 429 usage-limit envelope suppresses the plan group.
	claudeStdout, err := os.ReadFile(filepath.Join(fixtures, "claude-429-usage-limit.json"))
	if err != nil {
		return err
	}
	claudeIdentity, err := pl.IdentityFor(pl.ProviderClaude, claudeHome)
	if err != nil {
		return err
	}
	const claudeModel = "claude-opus-5"
	claudeGroup := table.Group(pl.ProviderClaude, claudeModel)
	claudeClass := pl.ClassifyClaudePrompt(pl.ClaudePromptResult{ExitCode: 1, Stdout: claudeStdout, Now: fixedNow})
	claudeObs, ok := claudeClass.QuotaObservation(pl.Evidence{
		RunID: "RUN-260822-2jouz3", TaskID: "TASK-260822-2jouz3", Model: claudeModel,
	})
	if !ok {
		return fmt.Errorf("the captured claude 429 fixture did not classify as provider quota (class=%q reason=%q)", claudeClass.Class, claudeClass.Reason)
	}
	if err := store.Observe(claudeIdentity, claudeGroup, nil, claudeObs); err != nil {
		return err
	}
	manifest = append(manifest, manifestEntry{
		Provider: pl.ProviderClaude, HomeGiven: claudeHome, HomeNormalized: claudeIdentity.Home,
		IdentityKey: claudeIdentity.Key, Group: claudeGroup, Model: claudeModel,
		StateFile: filepath.Base(store.Layout().StateFile(claudeIdentity.Key)),
	})

	// Codex: a captured usage-limit transcript suppresses the plan group, and a
	// probe claim is taken on it so the fixture carries a live lease too.
	codexLog, err := os.ReadFile(filepath.Join(fixtures, "codex-usage-limit-RUN-260801-a714a6.log"))
	if err != nil {
		return err
	}
	codexIdentity, err := pl.IdentityFor(pl.ProviderCodex, codexHome)
	if err != nil {
		return err
	}
	const codexModel = "gpt-5.5"
	codexGroup := table.Group(pl.ProviderCodex, codexModel)
	codexClass := pl.ClassifyCodexPrompt(pl.CodexPromptResult{ExitCode: 1, Log: codexLog, Now: fixedNow})
	codexObs, ok := codexClass.QuotaObservation(pl.Evidence{
		RunID: "RUN-260822-2jouz4", TaskID: "TASK-260822-2jouz3", Model: codexModel,
	})
	if !ok {
		return fmt.Errorf("the captured codex usage-limit fixture did not classify as provider quota (class=%q reason=%q)", codexClass.Class, codexClass.Reason)
	}
	if err := store.Observe(codexIdentity, codexGroup, nil, codexObs); err != nil {
		return err
	}
	manifest = append(manifest, manifestEntry{
		Provider: pl.ProviderCodex, HomeGiven: codexHome, HomeNormalized: codexIdentity.Home,
		IdentityKey: codexIdentity.Key, Group: codexGroup, Model: codexModel,
		StateFile: filepath.Base(store.Layout().StateFile(codexIdentity.Key)),
	})

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateRoot, "task-board", "provider-limits", "xrt-manifest.json"), append(data, '\n'), 0o644)
}

// read is the other direction: the SOURCE's Report over whatever state file
// this repository wrote. Its JSON goes to stdout and is committed as a fixture,
// so a port that started writing a shape the source cannot interpret shows up
// as a diff in the source's own projection rather than in ours.
func read(stateRoot string) error {
	store := newStore(stateRoot)
	var identities []pl.Identity
	for _, pair := range [][2]string{{pl.ProviderClaude, claudeHome}, {pl.ProviderCodex, codexHome}} {
		identity, err := pl.IdentityFor(pair[0], pair[1])
		if err != nil {
			return err
		}
		identities = append(identities, identity)
	}
	report := store.Report(identities)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

// tables captures the source's own tables — the ones this port has to match by
// VALUE rather than by shape — and prints them as JSON.
//
// Everything here is read out of the SOURCE's exported API, so the fixture is a
// captured fact rather than a constant somebody re-typed on this side. The
// backoff-alias policy in particular is probed BEHAVIOURALLY: the source's key
// resolution is unexported, so each candidate key is fed through the real
// SpawnLimitsConfig JSON decode and the answer is whether the row survived it.
func tables() error {
	type groupRow struct {
		Group      string   `json:"group"`
		Provider   string   `json:"provider"`
		Broker     string   `json:"broker"`
		Members    []string `json:"members"`
		Provenance string   `json:"provenance"`
	}
	out := map[string]any{}

	var ladder []string
	for _, step := range pl.DefaultLadder() {
		ladder = append(ladder, step.String())
	}
	out["providerlimits_default_ladder"] = ladder
	out["providerlimits_max_ladder_step"] = pl.MaxLadderStep.String()
	out["providerlimits_default_probe_lease_minutes"] = pl.DefaultProbeLeaseMinutes
	out["providerlimits_default_probe_claim_max_minutes"] = pl.DefaultProbeClaimMaxMinutes

	var remoteLadder []string
	for _, step := range rc.DefaultSpawnBackoffLadder() {
		remoteLadder = append(remoteLadder, step.String())
	}
	out["remoteconfig_default_backoff_ladder"] = remoteLadder
	out["remoteconfig_max_backoff_step"] = rc.MaxSpawnBackoffStep.String()

	var rows []groupRow
	for _, row := range pl.DefaultGroups() {
		rows = append(rows, groupRow{
			Group: row.Group, Provider: row.Provider, Broker: row.Broker,
			Members: row.Members, Provenance: string(row.Provenance),
		})
	}
	out["default_groups"] = rows

	classifiers := map[string]bool{}
	for _, broker := range []string{"anthropic", "openai", "alibaba", "google", "", "claude", "codex"} {
		classifiers[broker] = pl.HasClassifier(broker)
	}
	out["has_classifier_by_broker"] = classifiers

	runtimeClassifiers := map[string]bool{}
	for _, runtime := range []string{"claude", "codex", "qwen", "gemini", "agy", "muse", "qwen-codex", "not-a-runtime"} {
		runtimeClassifiers[runtime] = pl.HasClassifierForRuntime(runtime)
	}
	out["has_classifier_by_runtime"] = runtimeClassifiers

	// The alias policy, probed through the source's real decode.
	accepted := map[string]bool{}
	for _, key := range []string{
		"claude", "codex", "qwen", "gemini", "agy", "muse",
		"anthropic", "openai", "alibaba", "google", "definitely-not-a-broker",
	} {
		var limits rc.SpawnLimitsConfig
		payload := fmt.Sprintf(`{"backoff":{%q:["2m","5m"]}}`, key)
		if err := json.Unmarshal([]byte(payload), &limits); err != nil {
			return fmt.Errorf("probing backoff key %q: %w", key, err)
		}
		_, kept := limits.Backoff[key]
		accepted[key] = kept
	}
	out["backoff_key_accepted"] = accepted

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "limitstate_xrt:", err)
		os.Exit(1)
	}
	os.Exit(0)
}
