package codex

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/internal/planningio"
)

type plannerRead struct{ operation, path string }

// This is a recording decorator, not a substitute filesystem. Even failed or
// ignored-result calls are recorded before delegating to the production reader.
// No embedded Reader: adding an operation to the interface requires this fake
// to implement it explicitly, rather than silently bypass recording.
type recordingPlannerReader struct {
	base        planningio.Reader
	baseCatalog func(string, int64) ([]byte, error)
	calls       []plannerRead
}

func (r *recordingPlannerReader) record(op, path string) {
	r.calls = append(r.calls, plannerRead{op, path})
}
func (r *recordingPlannerReader) ReadFile(path string) ([]byte, error) {
	r.record("ReadFile", path)
	return r.base.ReadFile(path)
}
func (r *recordingPlannerReader) ReadRegular(path string, limit int64) ([]byte, error) {
	r.record("ReadRegular", path)
	return r.baseCatalog(path, limit)
}
func (r *recordingPlannerReader) Open(path string) (*os.File, error) {
	r.record("Open", path)
	return r.base.Open(path)
}
func (r *recordingPlannerReader) OpenFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	r.record("OpenFile", path)
	return r.base.OpenFile(path, flags, mode)
}
func (r *recordingPlannerReader) Stat(path string) (os.FileInfo, error) {
	r.record("Stat", path)
	return r.base.Stat(path)
}
func (r *recordingPlannerReader) Lstat(path string) (os.FileInfo, error) {
	r.record("Lstat", path)
	return r.base.Lstat(path)
}
func (r *recordingPlannerReader) Abs(path string) (string, error) {
	r.record("Abs", path)
	return r.base.Abs(path)
}
func (r *recordingPlannerReader) EvalSymlinks(path string) (string, error) {
	r.record("EvalSymlinks", path)
	return r.base.EvalSymlinks(path)
}
func (r *recordingPlannerReader) LookPath(env []string, name string) (string, error) {
	r.record("LookPath", name)
	return r.base.LookPath(env, name)
}
func (r *recordingPlannerReader) IsRegularFile(path string) bool {
	r.record("IsRegularFile", path)
	return r.base.IsRegularFile(path)
}

func recordPlannerReads(t *testing.T) *recordingPlannerReader {
	t.Helper()
	// These tests and all their subtests are sequential. Go holds parallel
	// tests until this test and its cleanup have returned.
	previous := planningio.Default
	previousCatalog := readCatalogFile
	recorder := &recordingPlannerReader{base: previous, baseCatalog: previousCatalog}
	planningio.Default = recorder
	readCatalogFile = recorder.ReadRegular
	t.Cleanup(func() { planningio.Default = previous; readCatalogFile = previousCatalog })
	return recorder
}

// TestCarriedSettingsRefuseBeforeReads closes settings-read-order-witness
// (repeat-of settings-refusal-after-binary-resolution). It calls public
// BuildPlan with real, readable fixtures, records attempted filesystem and
// resolver operations, and requires ZERO on every refusal. Missing paths and
// error precedence are not used as read proxies. Snapshot construction and
// fixture creation happen before recording starts.
func TestCarriedSettingsRefuseBeforeReads(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
	base := catalogRequest(t, home, work, "low")
	snapshot := mustSnapshot(t, base)
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	recorder := recordPlannerReads(t)
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			for _, binding := range []string{"id", "snapshot"} {
				t.Run(binding, func(t *testing.T) {
					for _, row := range carriedSettingsRefusalRows() {
						t.Run(row.name, func(t *testing.T) {
							req := base
							req.LocalProvider = &agentic.LocalProviderBinding{ID: base.LocalProvider.ID, Settings: row.settings}
							if binding == "snapshot" {
								req.LocalProvider.Snapshot = snapshot
							}
							recorder.calls = nil
							_, err := agentic.BuildPlan(registry, req, mode)
							// Assert independently of the returned error: a discarded
							// read followed by the right refusal is still a failure.
							if len(recorder.calls) != 0 {
								t.Errorf("settings refusal performed reads: %+v", recorder.calls)
							}
							requireLocalSettingsRefusal(t, err, row.kind, row.want, row.settings[0].Key, row.settings[0].Value)
						})
					}
				})
			}
		})
	}
}

// The positive control proves this exact public path reaches the recorder:
// binary lookup, config/catalog reads, materialisation and seal verification
// are all observed with admitted settings. A disconnected fake fails here.
func TestCarriedSettingsReadRecorderObservesAdmittedPlan(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
	req := settingsRequest(t, home, work, []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "true"}})
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	recorder := recordPlannerReads(t)
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			recorder.calls = nil
			plan, err := agentic.BuildPlan(registry, req, mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []plannerRead{
				{"LookPath", executableName},
				{"EvalSymlinks", plan.Binary},
				{"ReadFile", filepath.Join(home, "config.toml")},
				{"ReadRegular", filepath.Join(home, "catalog.local.json")},
				{"OpenFile", effectiveCatalogPath(t, plan)},
				{"Lstat", effectiveCatalogPath(t, plan)},
				{"ReadRegular", effectiveCatalogPath(t, plan)},
			} {
				if !slices.Contains(recorder.calls, want) {
					t.Errorf("missing read %+v in %+v", want, recorder.calls)
				}
			}
			recorder.calls = nil
			if err := plan.VerifyBeforeExec(); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(recorder.calls, plannerRead{"ReadRegular", effectiveCatalogPath(t, plan)}) {
				t.Fatalf("seal verification bypassed read recorder: %+v", recorder.calls)
			}
		})
	}
}
