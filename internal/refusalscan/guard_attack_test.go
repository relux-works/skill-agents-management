package refusalscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These attacks drive the real package coverage guard in a non-Git copy. A
// scanner error and an unmapped refusal are both red outcomes at that entry.
func TestProductionGuardRejectsNewSourceAttacks(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, file, source, diagnostic string }{
		{"zz_new_forward", "zz_new_forward.go", `package agentic
func ZZValidateExtraChannel(d CuratorChannelDescriptor) error { err := validateCuratorDescriptor(d, false); return err }
`, "zz_new_forward.go:2"},
		{"unprefixed_sentinel", "zz_unprefixed_sentinel.go", `package agentic
func ZZUnknownCuratorDescriptor() error { return ErrUnknownCuratorDescriptor }
`, "zz_unprefixed_sentinel.go :: ZZUnknownCuratorDescriptor"},
		{"classification_field", "zz_classification_field.go", `package agentic
import("errors";"fmt")
func ZZClassifiedField(d CuratorChannelDescriptor) error {if e:=validateCuratorDescriptor(d,false);e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);var out struct{Denied bool};out.Denied=classified;return fmt.Errorf("classified: %v",out.Denied)};return nil}
`, "zz_classification_field.go:3"},
		{"classification_index", "zz_classification_index.go", `package agentic
import("errors";"fmt")
func ZZClassifiedIndex(d CuratorChannelDescriptor) error {if e:=validateCuratorDescriptor(d,false);e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);out:=make([]bool,1);out[0]=classified;return fmt.Errorf("classified: %v",out[0])};return nil}
`, "zz_classification_index.go:3"},
		{"diagnostic_call", "zz_diagnostic_call.go", `package agentic
type zzDiagnostic struct{Cause error}
func zzPack(e error)zzDiagnostic{return zzDiagnostic{Cause:e}}
func ZZDiagnostic(d CuratorChannelDescriptor)zzDiagnostic{if e:=validateCuratorDescriptor(d,false);e!=nil{return zzPack(e)};return zzDiagnostic{}}
`, "zz_diagnostic_call.go:4"},
		{"comparison_call_storage", "zz_comparison_call.go", `package agentic
func zzKeep(e error,h *struct{Cause error})bool{h.Cause=e;return true}
func ZZComparison(d CuratorChannelDescriptor,h *struct{Cause error})bool{if e:=validateCuratorDescriptor(d,false);e!=nil{return zzKeep(e,h)==true};return false}
`, "zz_comparison_call.go:3"},
		{"classification_pointer_alias", "zz_pointer_alias.go", `package agentic
import("errors";"fmt")
func ZZPointer(d CuratorChannelDescriptor)error{if e:=validateCuratorDescriptor(d,false);e!=nil{var out struct{Denied bool};alias:=&out;alias.Denied=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out.Denied)};return nil}
`, "zz_pointer_alias.go:3"},
		{"classification_slice_alias", "zz_slice_alias.go", `package agentic
import("errors";"fmt")
func ZZSlice(d CuratorChannelDescriptor)error{if e:=validateCuratorDescriptor(d,false);e!=nil{out:=make([]bool,1);alias:=out;alias[0]=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out[0])};return nil}
`, "zz_slice_alias.go:3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			copyModule(t, source, root)
			if err := os.WriteFile(filepath.Join(root, "pkg", "agentic", c.file), []byte(c.source), 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("go", "test", "-mod=mod", "./pkg/agentic", "-count=1", "-v", "-run", "^TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage$")
			command.Dir = root
			command.Env = append(os.Environ(), "GOWORK=off")
			output, err := command.CombinedOutput()
			code := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != 1 || !strings.Contains(string(output), "--- FAIL: TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage") || !strings.Contains(string(output), c.diagnostic) || strings.Contains(string(output), "[build failed]") {
				t.Fatalf("attack exit=%d, expected named coverage guard red for %s\n%s", code, c.file, output)
			}
			t.Logf("EXPECTED RED | attack=%s | exit=%d | test=TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage | diagnostic=%s", c.name, code, c.diagnostic)
		})
	}
}
