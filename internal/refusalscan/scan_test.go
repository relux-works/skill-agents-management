package refusalscan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSourceFilesEnumeratesAllNonTestGoFilesFromModuleRootWithoutGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/agentic/plan.go", "package agentic\n\nfunc Plan() {}\n")
	writeFile(t, root, "pkg/agentic/systems/claude/context.go", "package claude\n")
	writeFile(t, root, "pkg/agentic/plan_test.go", "package agentic\n")
	writeFile(t, root, "pkg/other/ignored.go", "package other\n")
	writeFile(t, root, "docs/ignored.go", "package docs\n")

	files, err := SourceFiles(root)
	if err != nil {
		t.Fatalf("SourceFiles: %v", err)
	}
	want := []string{"pkg/agentic/plan.go", "pkg/agentic/systems/claude/context.go"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("SourceFiles = %#v, want %#v", files, want)
	}
}

func TestSourceFilesRejectsSymlinkedGoFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "outside/linked.go", "package agentic\nfunc Linked() error { return ErrCuratorContextMalformed }\n")
	if err := os.MkdirAll(filepath.Join(root, "pkg", "agentic"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "pkg", "agentic", "linked.go")
	if err := os.Symlink(filepath.Join(root, "outside", "linked.go"), link); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}

	_, err := SourceFiles(root)
	if err == nil || !strings.Contains(err.Error(), "symlinked Go source file") || !strings.Contains(err.Error(), "linked.go") {
		t.Fatalf("SourceFiles error = %v, want named symlinked Go source refusal with path", err)
	}
}

func TestSourceFilesRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "outside/nested/linked.go", "package nested\n")
	link := filepath.Join(root, "pkg", "agentic", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside", "nested"), link); err != nil {
		t.Fatalf("create package-tree symlink: %v", err)
	}

	_, err := SourceFiles(root)
	if err == nil || !strings.Contains(err.Error(), "symlinked directory") || !strings.Contains(err.Error(), "linked") {
		t.Fatalf("SourceFiles error = %v, want named symlinked-directory refusal with path", err)
	}
}

func TestDiscoverEnumeratesRefusalReturnsAcrossEverySourceFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/agentic/guards.go", `package agentic

import "fmt"

var ErrExampleRefusal = fmt.Errorf("refused")

type ExampleRefusal struct{}

func (*ExampleRefusal) Error() string { return "refused" }

func Direct() error {
	if true {
		return fmt.Errorf("wrapped: %w", ErrExampleRefusal)
	}
	return nil
}

func Typed() error {
	if true {
		return &ExampleRefusal{}
	}
	return nil
}

func Validate() error {
	return ErrExampleRefusal
}

func Forward() error {
	if err := Validate(); err != nil {
		return err
	}
	return nil
}

func TupleRefusal() (int, error) {
	return 1, ErrExampleRefusal
}

func TupleForward() (int, error) {
	return TupleRefusal()
}
`)
	writeFile(t, root, "pkg/agentic/nested/untouched.go", `package nested

import "errors"
var ErrExampleRefusal = errors.New("nested refusal")
func Existing() error { return ErrExampleRefusal }
`)

	// The nested package has its own error object with the same spelling. Discovery scans the whole tree from root without
	// consulting Git status or revision state.
	sites, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var got []string
	for _, site := range sites {
		got = append(got, site.Function+" | "+site.Guard+" | "+site.Return)
	}
	want := []string{
		"Direct | if true | return fmt.Errorf(\"wrapped: %w\", ErrExampleRefusal)",
		"Typed | if true | return &ExampleRefusal{}",
		"Validate | unconditional | return ErrExampleRefusal",
		"Forward | if err != nil | return err",
		"TupleRefusal | unconditional | return 1, ErrExampleRefusal",
		"TupleForward | unconditional | return TupleRefusal()",
		"Existing | unconditional | return ErrExampleRefusal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Discovered refusal sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestDiscoverRejectsTypedRefusalUsesOutsideAllowedShapes(t *testing.T) {
	fixtures := []struct {
		name string
		body string
	}{
		{
			name: "local assignment",
			body: `func Bad() error { err := ErrExampleRefusal; if err != nil { return err }; return nil }`,
		},
		{
			name: "constructor assignment",
			body: `func Bad() error { err := refusalFactory(); return err }`,
		},
		{
			name: "argument",
			body: `func Bad() error { consume(ErrExampleRefusal); return nil }`,
		},
		{
			name: "struct storage",
			body: `func Bad() error { stored := struct{ cause error }{cause: ErrExampleRefusal}; _ = stored; return nil }`,
		},
		{
			name: "map storage",
			body: `func Bad() error { stored := map[string]error{"cause": ErrExampleRefusal}; _ = stored; return nil }`,
		},
		{
			name: "var initializer",
			body: `func Bad() error { var stored error = ErrExampleRefusal; _ = stored; return nil }`,
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "pkg/agentic/guards.go", `package agentic
import "errors"
var ErrExampleRefusal = errors.New("refused")
func consume(error) {}
func refusalFactory() error { return ErrExampleRefusal }
`+fixture.body+"\n")
			_, err := Discover(root)
			if err == nil || !strings.Contains(err.Error(), "typed refusal use outside a return expression or refusal if-init") || !strings.Contains(err.Error(), "guards.go:") {
				t.Fatalf("Discover error = %v, want a named out-of-shape refusal with file:line", err)
			}
		})
	}
}

func TestUseShapeBoundaryRejectsConstructorForwardingAndReportsHelperForwardingOutOfScope(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/agentic/guards.go", `package agentic
import "errors"
var ErrCuratorExample = errors.New("refused")
func refusalFactory() error { return ErrCuratorExample }
func BadConstructorForwarding() error { err := refusalFactory(); return err }
func validateInternal() error {
	if true { return ErrCuratorExample }
	return nil
}
func OutOfScopeHelperForwarding() error { err := validateInternal(); return err }
`)
	if _, err := Discover(root); err == nil || !strings.Contains(err.Error(), "typed refusal use outside a return expression or refusal if-init") || !strings.Contains(err.Error(), "guards.go:") {
		t.Fatalf("constructor forwarding error = %v, want file:line refusal", err)
	}

	writeFile(t, root, "pkg/agentic/guards.go", `package agentic
import "errors"
var ErrCuratorExample = errors.New("refused")
func validateInternal() error {
	if true { return ErrCuratorExample }
	return nil
}
func OutOfScopeHelperForwarding() error { err := validateInternal(); return err }
`)
	if _, err := Discover(root); err == nil || !strings.Contains(err.Error(), "guards.go:8") {
		t.Fatalf("full helper set must refuse forwarding with file:line: %v", err)
	}
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", relative, err)
	}
}
