package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Explicit path selectors are transported separately, including spaces, while
// omission preserves the legacy argv and every serialized Status byte. Paths
// do not change the decoded status identity or observation contents.
func TestCLIStatusReaderOptionalPaths(t *testing.T) {
	t.Setenv("CURATOR_ENGINES_CONFIG", "/ambient/engines.toml")
	t.Setenv("CURATOR_ENGINES_CACHE_ROOT", "/ambient/cache")
	output := fixtureBytes(t, nil)
	now := time.Unix(1800000000, 0).UTC()
	query := validStatusQuery()
	baseline, err := decodeStatus(output, query.Runtime, query.Model, query.CuratorEnginesProfile, now)
	if err != nil {
		t.Fatal(err)
	}
	baseline.Observed = []Observation{{Source: "curator-engines status --json", Detail: "broker_state=serving broker_source=attested"}}
	wantBytes, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		config, cache string
		extra         []string
	}{
		{},
		{config: "/catalog with spaces/engines.toml", extra: []string{"--config", "/catalog with spaces/engines.toml"}},
		{cache: "/state with spaces", extra: []string{"--cache-root", "/state with spaces"}},
		{config: "/selected/engines.toml", cache: "/selected/cache", extra: []string{"--config", "/selected/engines.toml", "--cache-root", "/selected/cache"}},
	} {
		t.Run(test.config+"|"+test.cache, func(t *testing.T) {
			runner := &scriptedRunner{output: output}
			query := validStatusQuery()
			query.CuratorEnginesConfig, query.CuratorEnginesCacheRoot = test.config, test.cache
			reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithClock(func() time.Time { return now }))
			status, err := reader.Status(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := append([]string{"curator-engines", "status", "--engine", "qwen-local-engine", "--json"}, test.extra...)
			if !reflect.DeepEqual(runner.lastArgs, wantArgs) || runner.lastDir != query.CuratorEnginesProject {
				t.Fatalf("argv = %q, dir = %q; want %q, %q", runner.lastArgs, runner.lastDir, wantArgs, query.CuratorEnginesProject)
			}
			gotBytes, err := json.Marshal(status)
			if err != nil || string(gotBytes) != string(wantBytes) {
				t.Fatalf("status changed: got %s, want %s, err = %v", gotBytes, wantBytes, err)
			}
		})
	}
}

// Optional explicit paths must be absolute, matching the engine CLI contract;
// invalid selectors refuse before starting a subprocess and return no status.
func TestCLIStatusReaderRefusesRelativeOptionalPaths(t *testing.T) {
	for _, test := range []struct{ config, cache string }{
		{config: "engines.toml"}, {cache: "cache"}, {config: " "}, {cache: " "},
	} {
		t.Run(test.config+"|"+test.cache, func(t *testing.T) {
			runner := &scriptedRunner{output: fixtureBytes(t, nil)}
			query := validStatusQuery()
			query.CuratorEnginesConfig, query.CuratorEnginesCacheRoot = test.config, test.cache
			status, err := NewCLIStatusReader(WithCommandRunner(runner.run)).Status(context.Background(), query)
			if !errors.Is(err, ErrStatusQueryInvalid) || runner.calls != 0 || !reflect.DeepEqual(status, Status{}) {
				t.Fatalf("invalid path: err = %v, calls = %d, status = %#v", err, runner.calls, status)
			}
		})
	}
}
