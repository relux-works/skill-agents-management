package codex

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Each alteration starts from a real BuildPlan -> FinalizePlan -> ExportSeal
// round trip. Refusal must happen before integrity checks can mask lossy decoding.
func TestSealWireNullAndKindCrossingRefusals(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	base, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{PromptEnv: []string{"NULL_TEST=value"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := agentic.ImportSeal(New(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
	if decoded.HostedAdmissible() {
		t.Fatal("sealed control is hosted-admissible")
	}
	type level struct {
		name     string
		path     []string
		members  []string
		crossing string
	}
	levels := []level{
		{"envelope", nil, []string{"schema", "schema_version", "data"}, "binding"},
		{"data", []string{"data"}, []string{"kind", "sealed"}, "binding"},
		{"sealed", []string{"data", "sealed"}, []string{"system", "binary", "argv", "artifacts", "selectors", "digest", "binding"}, "kind"},
		{"binding", []string{"data", "sealed", "binding"}, []string{"binary", "argv", "env_names", "selectors", "key_id", "digest"}, "sealed"},
		{"artifact", []string{"data", "sealed", "artifacts", "0"}, []string{"name", "path", "digest"}, "binding"},
	}
	count := 0
	for _, scope := range levels {
		for _, member := range scope.members {
			for _, variant := range []string{"null", "null-shadow", "null-after", "wrong-type", "missing"} {
				if variant == "missing" && scope.name == "sealed" && (member == "selectors" || member == "binding") {
					continue
				}
				count++
				t.Run(scope.name+"/"+member+"/"+variant, func(t *testing.T) {
					var object map[string]any
					if err := json.Unmarshal(wire, &object); err != nil {
						t.Fatal(err)
					}
					target := sealNullObjectAt(object, scope.path)
					var changed []byte
					if variant == "null" || variant == "wrong-type" || variant == "missing" {
						target[member] = nil
						if variant == "wrong-type" {
							target[member] = true
						}
						if variant == "missing" {
							delete(target, member)
						}
						changed, err = json.Marshal(object)
					} else {
						// Preserve duplicates in the raw token stream, at this exact level.
						raw, marshalErr := json.Marshal(target)
						if marshalErr != nil {
							t.Fatal(marshalErr)
						}
						key, _ := json.Marshal(member)
						marker := string(key) + ":"
						replacement := marker + "null," + marker
						if variant == "null-after" {
							value, _ := json.Marshal(target[member])
							marker += string(value)
							replacement = marker + "," + string(key) + ":null"
						}
						altered := strings.Replace(string(raw), marker, replacement, 1)
						original := string(raw)
						canonical, marshalErr := json.Marshal(object)
						if marshalErr != nil {
							t.Fatal(marshalErr)
						}
						changed = []byte(strings.Replace(string(canonical), original, altered, 1))
						if string(changed) == string(canonical) {
							t.Fatal("probe did not alter the selected object")
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					assertSealNullWireRefuses(t, changed)
				})
			}
		}
		count++
		t.Run(scope.name+"/kind-crossing", func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(wire, &object); err != nil {
				t.Fatal(err)
			}
			sealNullObjectAt(object, scope.path)[scope.crossing] = map[string]any{}
			changed, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			assertSealNullWireRefuses(t, changed)
		})
	}
	for _, path := range [][]string{{"data", "sealed", "argv"}, {"data", "sealed", "artifacts"}, {"data", "sealed", "binding", "argv"}, {"data", "sealed", "binding", "env_names"}, {"data", "sealed", "binding", "selectors"}} {
		count++
		t.Run(strings.Join(path, "/")+"/null-element", func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(wire, &object); err != nil {
				t.Fatal(err)
			}
			target := sealNullObjectAt(object, path[:len(path)-1])
			field := path[len(path)-1]
			if array, ok := target[field].([]any); ok {
				array[0] = nil
			} else {
				target[field].(map[string]any)["0"] = nil
			}
			changed, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			assertSealNullWireRefuses(t, changed)
		})
	}
	t.Logf("null/shadow/kind-crossing attacks: %d of %d across 5 object levels", count, count)
}

func sealNullObjectAt(object map[string]any, path []string) map[string]any {
	var node any = object
	for _, member := range path {
		if member == "0" {
			node = node.([]any)[0]
		} else {
			node = node.(map[string]any)[member]
		}
	}
	return node.(map[string]any)
}

func assertSealNullWireRefuses(t *testing.T, wire []byte) {
	t.Helper()
	seal, decodeErr := agentic.DecodeSeal(wire)
	_, importErr := agentic.ImportSeal(New(), seal)
	if !errors.Is(decodeErr, agentic.ErrSealMalformed) || importErr == nil || seal.HostedAdmissible() {
		t.Fatalf("null or kind-crossing wire admitted: decode=%v import=%v hosted=%v", decodeErr, importErr, seal.HostedAdmissible())
	}
}
