package main

import (
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestDiscoverMutationsCoversEveryTypedRefusalSource(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not identify the generator test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("change to module root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	rows := discoverMutations(token.NewFileSet(), []string{
		"pkg/agentic/systems/codex/provider.go",
		"pkg/agentic/systems/codex/snapshot.go",
		"pkg/agentic/localprovider.go",
	})
	var direct, validator, blankBinding int
	for _, row := range rows {
		if strings.HasPrefix(row.Source, "validateLoopbackBaseURL") {
			validator++
		}
		if row.ID == "M-provider.go-L28-O1" {
			blankBinding++
		}
		if row.Source == "typed LocalProviderRefusal return" {
			direct++
		}
	}
	// 45/33: the snapshot branch in localProviderArgs adds four direct
	// operands. L54 (snapshot ignored) is killed by
	// TestSnapshotPinsThePlannedProviderAcrossAConfigMutation,
	// TestSnapshotPathPerformsZeroConfigReads and
	// TestSnapshotBindingRefusesForgedSnapshots; L56-O2 (validity check
	// dropped) and L59 (ID check dropped) are killed by
	// TestSnapshotBindingRefusesForgedSnapshots. L56-O1 (!ok) is subsumed by
	// !snapshot.valid() — a failed assertion yields nil, which is always
	// invalid — so no test can distinguish its removal.
	//
	// 53/41: the snapshot constructor adds eight direct operands. Six are
	// killed by named behavioral tests, each verified by applying the
	// narrowing mutant (operand neutralized with false &&) and observing
	// the named test fail while the rest of its siblings pass:
	// L78-O1 (nil-binding disjunct dropped) is killed by
	// TestReadProviderSnapshotRefusesAnEmptyBinding, which panics on the
	// nil dereference the mutant admits; L78-O2 (empty-ID disjunct dropped)
	// is killed by the same test, which observes unsupported instead of
	// unbound; L82-O1 (whitespace check dropped) is killed by
	// TestReadProviderSnapshotRefusalParity/malformed_provider_id_whitespace,
	// which observes unsupported instead of malformed; L85-O1 (local- prefix
	// check dropped) is killed by
	// TestReadProviderSnapshotRefusalParity/builtin_provider_id_is_not_a_local_binding,
	// which observes a returned snapshot instead of the unsupported refusal;
	// L88-O1 (pattern check dropped) is killed by
	// TestReadProviderSnapshotRefusalParity/malformed_dotted_provider_id,
	// which observes unbound instead of malformed; L99-O1 (read-error check
	// dropped) is killed by
	// TestReadProviderSnapshotRefusalParity/absent_private_config and
	// .../private_config_read_failure_is_not_absence, which observe unbound
	// instead of absent/read_failed.
	//
	// L107-O1 and L107-O2 are equivalent mutants, like L56-O1 above.
	// go-toml parses the whole document before decoding into the map, so a
	// decode error always leaves the target nil and L107-O1 (err != nil
	// dropped) is subsumed by config == nil; conversely a successful decode
	// into a map always allocates (even for empty input), so err == nil
	// with a nil map is unreachable and L107-O2 (config == nil dropped)
	// changes nothing. TestTomlDecodeShapePinsTheParseGateOperands pins
	// both premises, so a library behavior change fails the suite instead
	// of silently reviving the mutants.
	if len(rows) != 53 || direct != 41 || validator != 12 || blankBinding != 1 {
		t.Fatalf("generated refusal operands = total %d, direct %d, validateLoopbackBaseURL %d, provider.go:28 %d; want 53, 41, 12, 1", len(rows), direct, validator, blankBinding)
	}
	// The counts above cannot see a count-preserving drift (one refusal
	// site removed while another is added), so the census also pins the
	// exact operand identity set.
	var got []string
	for _, row := range rows {
		got = append(got, row.ID)
	}
	sort.Strings(got)
	want := []string{
		"M-provider.go-L105-O1",
		"M-provider.go-L105-O2",
		"M-provider.go-L119-O1",
		"M-provider.go-L123-O1",
		"M-provider.go-L127-O1",
		"M-provider.go-L131-O1",
		"M-provider.go-L131-O2",
		"M-provider.go-L131-O3",
		"M-provider.go-L131-O4",
		"M-provider.go-L136-O1",
		"M-provider.go-L136-O2",
		"M-provider.go-L139-O1",
		"M-provider.go-L140-O1",
		"M-provider.go-L146-O1",
		"M-provider.go-L149-O1",
		"M-provider.go-L153-O1",
		"M-provider.go-L157-O1",
		"M-provider.go-L160-O1",
		"M-provider.go-L164-O1",
		"M-provider.go-L175-O1",
		"M-provider.go-L175-O2",
		"M-provider.go-L175-O3",
		"M-provider.go-L175-O4",
		"M-provider.go-L175-O5",
		"M-provider.go-L178-O1",
		"M-provider.go-L181-O1",
		"M-provider.go-L183-O1",
		"M-provider.go-L183-O2",
		"M-provider.go-L183-O3",
		"M-provider.go-L188-O1",
		"M-provider.go-L189-O1",
		"M-provider.go-L28-O1",
		"M-provider.go-L31-O1",
		"M-provider.go-L34-O1",
		"M-provider.go-L34-O2",
		"M-provider.go-L37-O1",
		"M-provider.go-L40-O1",
		"M-provider.go-L54-O1",
		"M-provider.go-L56-O1",
		"M-provider.go-L56-O2",
		"M-provider.go-L59-O1",
		"M-provider.go-L66-O1",
		"M-provider.go-L74-O1",
		"M-provider.go-L74-O2",
		"M-provider.go-L97-O1",
		"M-snapshot.go-L107-O1",
		"M-snapshot.go-L107-O2",
		"M-snapshot.go-L78-O1",
		"M-snapshot.go-L78-O2",
		"M-snapshot.go-L82-O1",
		"M-snapshot.go-L85-O1",
		"M-snapshot.go-L88-O1",
		"M-snapshot.go-L99-O1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated refusal operand set drifted:\n got  %q\n want %q", got, want)
	}
}
