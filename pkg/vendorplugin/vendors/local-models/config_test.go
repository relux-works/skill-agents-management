package localmodels

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

const validTOML = `
inference_engines = ["mlx"]

[runtimes.local-qwen]
system = "pi"
engine = "mlx"

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description           = "Local Qwen3.8-27B, MLX 8-bit"
publisher             = "alibaba"
family                = "qwen"
lifecycle             = "current"
context_window_tokens = 131072
effort_support        = "none"
engine                = "mlx"

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit".pointer]
agents_infra_project  = "/Users/op/skill-agents-management"
agents_infra_profile  = "local-qwen"
`

// legacyTOML is the exact pre-inference-engine configuration shape accepted
// by v0.4.0. The absent fields mean "no engine requirement"; they are not a
// failed or partial read and must remain source-compatible.
const legacyTOML = `
[runtimes.local-qwen]
system = "pi"

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit"]
description           = "Local Qwen3.8-27B, MLX 8-bit"
publisher             = "alibaba"
family                = "qwen"
lifecycle             = "current"
context_window_tokens = 131072
effort_support        = "none"

[runtimes.local-qwen.models."qwen-3.8-27b-mlx-8bit".pointer]
agents_infra_project  = "/Users/op/skill-agents-management"
agents_infra_profile  = "local-qwen"
`

func TestShippedLocalQwenFixtureMatchesTheConfiguredAxes(t *testing.T) {
	body, err := os.ReadFile("testdata/local-qwen.toml")
	if err != nil {
		t.Fatalf("ReadFile local-qwen fixture: %v", err)
	}
	result := newConfigLoader(func() ([]byte, bool, error) { return body, false, nil }).load()
	if result.Err != nil || result.Absent {
		t.Fatalf("local-qwen fixture result = %+v", result)
	}
	runtime, ok := runtimeEntry(result.Config, "local-qwen")
	if !ok || runtime.System != "pi" || runtime.Engine.ID != "mlx" {
		t.Fatalf("runtime axes = %#v", runtime)
	}
	model, ok := runtime.Models["qwen-3.8-27b-mlx-8bit"]
	if !ok || model.Publisher != "alibaba" || model.Family != "qwen" || model.Engine != runtime.Engine || model.Pointer.AgentsInfraProfile != "local-qwen" {
		t.Fatalf("model axes = %#v", model)
	}
}

// fakeFileReader is the "fake filesystem for local-models.toml's single-tier,
// memoize-forever load" the adversarial plan's §1 fakes section describes: a
// manually-controlled file-presence/content source with a call counter.
type fakeFileReader struct {
	mu      sync.Mutex
	calls   int
	content []byte
	absent  bool
	err     error
	// blockUntil, when non-nil, is closed by the test to release a read that
	// is deliberately held open, for the single-flight proof.
	blockUntil chan struct{}
}

func (f *fakeFileReader) read() ([]byte, bool, error) {
	f.mu.Lock()
	f.calls++
	block := f.blockUntil
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content, f.absent, f.err
}

func (f *fakeFileReader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestLoadConfigAbsentIsNotAnError is case 1: absent local-models.toml is
// not an error, and loadConfig's own three-way ConfigResult is correct in
// isolation.
func TestLoadConfigAbsentIsNotAnError(t *testing.T) {
	reader := &fakeFileReader{absent: true}
	loader := newConfigLoader(reader.read)
	result := loader.load()
	if !result.Absent {
		t.Fatalf("result = %+v, want Absent: true", result)
	}
	if result.Err != nil {
		t.Fatalf("absent config carries an error: %v", result.Err)
	}
}

// TestLoadConfigMalformedIsADistinctResultFromAbsent is case 2: a present,
// unparsable file (or a pointer missing a required field) is a DISTINCT
// typed result from absent, never conflated with it.
func TestLoadConfigMalformedIsADistinctResultFromAbsent(t *testing.T) {
	t.Run("invalid TOML", func(t *testing.T) {
		reader := &fakeFileReader{content: []byte("not = [valid toml")}
		result := newConfigLoader(reader.read).load()
		if result.Absent {
			t.Fatal("invalid TOML was reported as Absent")
		}
		if !errors.Is(result.Err, ErrConfigMalformed) {
			t.Fatalf("err = %v, want ErrConfigMalformed", result.Err)
		}
	})
	t.Run("pointer missing agents_infra_project", func(t *testing.T) {
		reader := &fakeFileReader{content: []byte(`
[runtimes.local-qwen]
system = "pi"
[runtimes.local-qwen.models."m"]
description = "d"
lifecycle = "current"
effort_support = "none"
[runtimes.local-qwen.models."m".pointer]
agents_infra_profile = "local-qwen"
`)}
		result := newConfigLoader(reader.read).load()
		if result.Absent {
			t.Fatal("a missing pointer field was reported as Absent")
		}
		if !errors.Is(result.Err, ErrConfigMalformed) {
			t.Fatalf("err = %v, want ErrConfigMalformed", result.Err)
		}
	})
	t.Run("pointer missing agents_infra_profile", func(t *testing.T) {
		reader := &fakeFileReader{content: []byte(`
[runtimes.local-qwen]
system = "pi"
[runtimes.local-qwen.models."m"]
description = "d"
lifecycle = "current"
effort_support = "none"
[runtimes.local-qwen.models."m".pointer]
agents_infra_project = "/x"
`)}
		result := newConfigLoader(reader.read).load()
		if !errors.Is(result.Err, ErrConfigMalformed) {
			t.Fatalf("err = %v, want ErrConfigMalformed", result.Err)
		}
	})
}

// TestLoadConfigValidResolvesExactly is case 3/4(c): a valid config parses
// into the exact pointer and metadata the fixture names.
func TestLoadConfigValidResolvesExactly(t *testing.T) {
	reader := &fakeFileReader{content: []byte(validTOML)}
	result := newConfigLoader(reader.read).load()
	if result.Absent || result.Err != nil {
		t.Fatalf("valid config was reported as absent/malformed: %+v", result)
	}
	entry, ok := runtimeEntry(result.Config, "local-qwen")
	if !ok {
		t.Fatal("local-qwen runtime not found in parsed config")
	}
	if entry.System != agentic.SystemID("pi") {
		t.Fatalf("system = %q, want pi", entry.System)
	}
	wantEngine := plugin.Ref{ID: "mlx", Kind: inferenceengine.Kind}
	if entry.Engine != wantEngine {
		t.Fatalf("runtime engine = %#v, want %#v", entry.Engine, wantEngine)
	}
	model, ok := entry.Models[vendorplugin.ModelID("qwen-3.8-27b-mlx-8bit")]
	if !ok {
		t.Fatal("model qwen-3.8-27b-mlx-8bit not found")
	}
	if model.Pointer.AgentsInfraProject != "/Users/op/skill-agents-management" || model.Pointer.AgentsInfraProfile != "local-qwen" {
		t.Fatalf("pointer = %+v, want the fixture's project/profile", model.Pointer)
	}
	if model.Publisher != "alibaba" || model.Family != "qwen" {
		t.Fatalf("publisher/family = %q/%q, want alibaba/qwen", model.Publisher, model.Family)
	}
	if model.Engine != wantEngine {
		t.Fatalf("model engine = %#v, want %#v", model.Engine, wantEngine)
	}
}

func TestLoadConfigRefusesUnknownAndPreservesMismatchedEngineReferences(t *testing.T) {
	t.Run("present blank runtime engine", func(t *testing.T) {
		body := strings.Replace(validTOML, "engine = \"mlx\"", "engine = \"\"", 1)
		result := newConfigLoader(func() ([]byte, bool, error) { return []byte(body), false, nil }).load()
		if !errors.Is(result.Err, ErrConfigMalformed) || !strings.Contains(result.Err.Error(), "present but blank") {
			t.Fatalf("blank runtime engine err = %v, want typed present-but-blank refusal", result.Err)
		}
	})

	t.Run("unknown runtime engine", func(t *testing.T) {
		body := strings.Replace(validTOML, "engine = \"mlx\"", "engine = \"unknown\"", 1)
		result := newConfigLoader(func() ([]byte, bool, error) { return []byte(body), false, nil }).load()
		if !errors.Is(result.Err, ErrConfigMalformed) || !strings.Contains(result.Err.Error(), "not declared") {
			t.Fatalf("unknown runtime engine err = %v, want typed not-declared refusal", result.Err)
		}
	})

	t.Run("unknown engine on a zero-model runtime", func(t *testing.T) {
		body := "inference_engines = [\"mlx\"]\n[runtimes.local-qwen]\nsystem = \"pi\"\nengine = \"unknown\"\n"
		result := newConfigLoader(func() ([]byte, bool, error) { return []byte(body), false, nil }).load()
		if !errors.Is(result.Err, ErrConfigMalformed) || !strings.Contains(result.Err.Error(), "not declared") {
			t.Fatalf("zero-model unknown engine err = %v, want typed not-declared refusal", result.Err)
		}
	})

	t.Run("runtime and model engine mismatch", func(t *testing.T) {
		body := strings.Replace(validTOML, "inference_engines = [\"mlx\"]", "inference_engines = [\"mlx\", \"other\"]", 1)
		body = strings.Replace(body, "engine                = \"mlx\"", "engine                = \"other\"", 1)
		result := newConfigLoader(func() ([]byte, bool, error) { return []byte(body), false, nil }).load()
		if result.Err != nil {
			t.Fatalf("parse should preserve both configured refs for BuildLaunch mismatch refusal: %v", result.Err)
		}
		entry, _ := runtimeEntry(result.Config, "local-qwen")
		if entry.Engine == entry.Models["qwen-3.8-27b-mlx-8bit"].Engine {
			t.Fatal("test fixture did not produce distinct runtime/model engine refs")
		}
	})
}

// TestLoadConfigZeroModelsIsAValidNonEmptyResult is case 4(d): a file that
// PARSES but declares zero models under a runtime is a real config error,
// distinct from absence and from a parse failure — it must be a VALID,
// EMPTY ConfigResult, not folded into either permissive branch. What makes
// it loud is Registry.Register's own ErrNoModels, driven in vendor_test.go;
// this test only pins the loader's own honest classification.
func TestLoadConfigZeroModelsIsAValidNonEmptyResult(t *testing.T) {
	reader := &fakeFileReader{content: []byte(`
[runtimes.local-qwen]
system = "pi"
`)}
	result := newConfigLoader(reader.read).load()
	if result.Absent {
		t.Fatal("a runtime with zero models was reported as Absent")
	}
	if result.Err != nil {
		t.Fatalf("a runtime with zero models is a valid parse, not a malformed one: %v", result.Err)
	}
	entry, ok := runtimeEntry(result.Config, "local-qwen")
	if !ok || len(entry.Models) != 0 {
		t.Fatal("test setup: expected zero models")
	}
}

// TestLoadConfigSingleFlightsConcurrentCallers is case 4(e): a concurrent
// Peek during the first, not-yet-completed load is single-flighted, and
// every caller observes the identical memoized result.
func TestLoadConfigSingleFlightsConcurrentCallers(t *testing.T) {
	release := make(chan struct{})
	reader := &fakeFileReader{content: []byte(validTOML), blockUntil: release}
	loader := newConfigLoader(reader.read)

	const goroutines = 8
	results := make([]ConfigResult, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i] = loader.load()
		}(i)
	}
	close(release)
	wg.Wait()

	if reader.callCount() != 1 {
		t.Fatalf("read() was called %d times concurrently, want exactly 1 (single-flighted)", reader.callCount())
	}
	for i, result := range results {
		if result.Err != nil || result.Absent {
			t.Fatalf("goroutine %d got %+v", i, result)
		}
	}
}

// TestLoadConfigMemoizesForever is case 4(f): the memoized result never
// changes after the first successful read, even if the underlying source
// mutates afterward.
func TestLoadConfigMemoizesForever(t *testing.T) {
	reader := &fakeFileReader{content: []byte(validTOML)}
	loader := newConfigLoader(reader.read)

	first := loader.load()
	if first.Err != nil || first.Absent {
		t.Fatalf("first load: %+v", first)
	}

	reader.mu.Lock()
	reader.content = []byte("not = [valid toml")
	reader.mu.Unlock()

	second := loader.load()
	if second.Err != nil || second.Absent {
		t.Fatalf("second load changed after the underlying source mutated: %+v", second)
	}
	if len(second.Config.Runtimes) != len(first.Config.Runtimes) {
		t.Fatal("second load's Config differs from the first; the loader is not memoized forever")
	}
	if reader.callCount() != 1 {
		t.Fatalf("read() was called %d times; a memoized-forever loader reads exactly once", reader.callCount())
	}
}
