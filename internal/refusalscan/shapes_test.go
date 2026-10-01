package refusalscan

import (
	"strings"
	"testing"
)

const identityFixture = `package agentic
import "errors"
import "fmt"
import "log"
import "log/slog"
var _ = slog.Info
var ErrUnknownCuratorDescriptor = errors.New("refused")
var _ = fmt.Sprintf
var _ = log.Print
func validateInternal() error { if true { return ErrUnknownCuratorDescriptor }; return nil }
type classifier struct{}
func (*classifier) Error() string {return "refused"}
`

func TestAcceptedConsumptionShapes(t *testing.T) {
	cases := []struct {
		name, source string
		red          bool
	}{
		{"typed_sentinel", `type denial string;func (denial) Error() string {return "denied"};var unprefixed = denial("denied");func Use() error {return unprefixed}`, false},
		{"constant_sentinel", `type denial string;func (denial) Error() string {return "denied"};const unprefixed = denial("denied");func Use() error {return unprefixed}`, false},
		{"global_alias", `var stored = ErrUnknownCuratorDescriptor`, true},
		{"direct", `func Use() error { return validateInternal() }`, false},
		{"returning_if_init", `func Use() error { if err := validateInternal(); err != nil { return err }; return nil }`, false},
		{"panic_text", `func init() {if err:=validateInternal();err!=nil {panic(fmt.Sprintf("registration: %v",err))}}`, false},
		{"terminal_panic", `func init() { if err := validateInternal(); err != nil { panic(err) } }`, false},
		{"panic_not_last", `func init() { if err := validateInternal(); err != nil { panic(err); println("later") } }`, true},
		{"panic_error_result", `func Use() error { if err := validateInternal(); err != nil { panic(err) }; return nil }`, true},
		{"panic_escape", `var stored error; func init() { if err := validateInternal(); err != nil { stored = err; panic(err) } }`, true},
		{"Is_method", `func (*classifier) Is(target error) bool { return target == ErrUnknownCuratorDescriptor }`, false},
		{"errors_Is", `func Classify(err error) bool { return errors.Is(err, ErrUnknownCuratorDescriptor) }`, false},
		{"classification_storage", `func Use() error { stored := struct{ cause error }{ErrUnknownCuratorDescriptor}; _ = errors.Is(nil, stored.cause); return stored.cause }`, true},
		{"local_forward", `func Use() error { err := validateInternal(); return err }`, true},
		{"bare_named_return", `func Use() (err error) { err = validateInternal(); return }`, true},
		{"if_init_escape", `var stored error; func Use() error { if err := validateInternal(); err != nil { stored = err; return err }; return nil }`, true},
		{"shadowed_sentinel", `func Use() error { ErrUnknownCuratorDescriptor := errors.New("ordinary local"); return ErrUnknownCuratorDescriptor }`, false},
		{"args_names", `func args() error { return validateInternal() }; func Args() error {return validateInternal()}; func Use() { args := 1; Args := 2; _ = args; _ = Args }`, false},
		{"not_found", `func Lookup() (any, bool) { if err := validateInternal(); err != nil { return nil, false }; return 1, true }`, false},
		{"not_found_local", `func Lookup() (any, bool) { err := validateInternal(); if err != nil { return nil, false }; return 1, true }`, true},
		{"diagnostic", `type diagnostic struct{ Reason string }; func Inspect() diagnostic { if err := validateInternal(); err != nil { return diagnostic{Reason: err.Error()} }; return diagnostic{} }`, false},
		{"diagnostic_error_field", `type diagnostic struct{ Cause error }; func Inspect() diagnostic { if err := validateInternal(); err != nil { return diagnostic{Cause: err} }; return diagnostic{} }`, true},
		{"protocol_Error", `type message struct{}; func (*message) Error() string { return ErrUnknownCuratorDescriptor.Error() }`, false},
		{"non_protocol_Error", `type message struct{}; func (*message) Error(n int) string { return ErrUnknownCuratorDescriptor.Error() }`, true},
		{"protocol_Unwrap", `type message struct{}; func (*message) Error() string { return "message" }; func (*message) Unwrap() error { return ErrUnknownCuratorDescriptor }`, false},
		{"protocol_As", `func (*classifier) As(target any) bool { return errors.Is(ErrUnknownCuratorDescriptor, nil) }`, false},
		{"non_protocol_Is", `type message struct{}; func (*message) Is(target error) bool { local := ErrUnknownCuratorDescriptor; return target == local }`, true},
		{"drop_blank", `func Use() { _ = validateInternal() }`, true},
		{"drop_blank_tuple", `func tuple() (int,error) { return 1, validateInternal() }; func Use() { if n, _ := tuple(); n != 0 { println(n) } }`, true},
		{"explicit_drop", `func Use() { if err := validateInternal(); err != nil {} }`, false},
		{"field_storage", `type holder struct{Cause error}; func Use(h *holder) { if err := validateInternal(); err != nil { h.Cause = err } }`, true},
		{"slice_storage", `func Use() []error { if err := validateInternal(); err != nil { return []error{err} }; return nil }`, true},
		{"channel_storage", `func Use(ch chan error) { if err := validateInternal(); err != nil {ch <- err} }`, true},
		{"hidden_storage", `func Use() any { if err := validateInternal(); err != nil {return any(err)}; return nil }`, true},
		{"classification_to_error", `func Use() error { if err := validateInternal(); err != nil {return fmt.Errorf("classified: %v", errors.Is(err, ErrUnknownCuratorDescriptor))};return nil }`, true},
		{"classification_local", `func Use() bool {if err:=validateInternal();err!=nil {classified:=errors.Is(err,ErrUnknownCuratorDescriptor);return classified};return false}`, false},
		{"classification_local_to_error", `func Use() error {if err:=validateInternal();err!=nil {classified:=errors.Is(err,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",classified)};return nil}`, true},
		{"classification_alias_to_error", `func Use() error {if err:=validateInternal();err!=nil {classified:=errors.Is(err,ErrUnknownCuratorDescriptor);alias:=classified;return fmt.Errorf("classified: %v",alias)};return nil}`, true},
		{"classification_aggregate_to_error", `func Use() error {if err:=validateInternal();err!=nil {classified:=errors.Is(err,ErrUnknownCuratorDescriptor);value:=struct{Denied bool}{classified};return fmt.Errorf("classified: %v",value)};return nil}`, true},
		{"classification_var_to_error", `func Use() error {if err:=validateInternal();err!=nil {classified:=errors.Is(err,ErrUnknownCuratorDescriptor);var alias=classified;return fmt.Errorf("classified: %v",alias)};return nil}`, true},
		{"indexed_wrap", `func Use() error {if err:=validateInternal();err!=nil {return fmt.Errorf("%[2]w %[1]s", "wrapped", err)};return nil}`, false},
		{"wrong_wrapped_argument", `func Use() error {if err:=validateInternal();err!=nil {return fmt.Errorf("%v: %w", err, errors.New("other"))};return nil}`, true},
		{"indexed_wrong_wrap", `func Use() error {if err:=validateInternal();err!=nil {return fmt.Errorf("%[2]v: %[1]w", errors.New("other"), err)};return nil}`, true},
		{"escaped_wrap", `func Use() error {if err:=validateInternal();err!=nil {return fmt.Errorf("%%w %v",err)};return nil}`, true},
		{"wrapped", `func Use() error { if err := validateInternal(); err != nil {return fmt.Errorf("wrapped: %w", err)};return nil }`, false},
		{"switch_init", `func Use() bool {switch err := validateInternal(); {case err != nil: return false};return true}`, false},
		{"switch_error", `func Use() error {switch err := validateInternal(); {case err != nil: return err};return nil}`, true},
		{"switch_escape", `func Use() error { var stored error;switch err := validateInternal(); {case err != nil: stored = err};return stored}`, true},
		{"log", `func Use() {if err := validateInternal(); err != nil {log.Print(err)}}`, false},
		{"string", `func Use() string {return fmt.Sprint(validateInternal())}`, false},
		{"closure_escape", `func Use() func() error {if err := validateInternal(); err != nil {return func() error {return err}};return nil}`, true},
		{"non_error_tuple_slot", `func tuple() (int,error) {return 1,validateInternal()};func Use() int {if n,err:=tuple();err!=nil {return n};return 0}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "pkg/agentic/guards.go", identityFixture+c.source)
			sites, err := Discover(root)
			if !c.red && (c.name == "typed_sentinel" || c.name == "constant_sentinel") {
				found := false
				for _, site := range sites {
					if site.Function == "Use" {
						found = true
					}
				}
				if !found {
					t.Fatal("unprefixed concrete sentinel escaped inventory")
				}
			}
			if c.red {
				if err == nil || !strings.Contains(err.Error(), "typed refusal use outside") || !strings.Contains(err.Error(), "guards.go:") {
					t.Fatalf("Discover error = %v; want shape refusal at file:line", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiscoverRejectsZZNewForward(t *testing.T) {
	for _, body := range []string{
		`func ZZValidateAgain() error { err := validateInternal(); return err }`,
		`func ZZValidateAgain() (err error) { err = validateInternal(); return }`,
	} {
		root := t.TempDir()
		writeFile(t, root, "pkg/agentic/guards.go", identityFixture)
		writeFile(t, root, "pkg/agentic/zz_new_forward.go", "package agentic\n"+body)
		_, err := Discover(root)
		if err == nil || !strings.Contains(err.Error(), "zz_new_forward.go:2") {
			t.Fatalf("new-forward guard error = %v; want file:line", err)
		}
	}
}

func TestDiscoverRecognizesUnprefixedSentinelNewFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/agentic/guards.go", identityFixture)
	writeFile(t, root, "pkg/agentic/zz_unprefixed.go", `package agentic
func ZZUnknownKind() error { return ErrUnknownCuratorDescriptor }
`)
	sites, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.File == "zz_unprefixed.go" && s.Line == 2 {
			return
		}
	}
	t.Fatal("unprefixed new-file refusal escaped inventory")
}

// The broadened contract admits protocol formatting but still rejects ordinary
// locals whose scope outlives a consuming statement.
func TestStrictSubsetRejectsExistingConsumerClasses(t *testing.T) {
	for _, body := range []string{
		`func Lookup() (int, bool) { err := validateInternal(); if err != nil { return 0, false }; return 1, true }`,
		`func Inspect() string { err := validateInternal(); if err != nil { return "unparseable" }; return "inspected" }`,
	} {
		root := t.TempDir()
		writeFile(t, root, "pkg/agentic/guards.go", identityFixture+body)
		if _, err := Discover(root); err == nil {
			t.Fatal("ordinary local consumer escaped")
		}
	}
}
