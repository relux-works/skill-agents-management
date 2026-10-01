package refusalscan

import (
	"fmt"
	"testing"
)

func TestComparisonOperandsRespectClosedConsumers(t *testing.T) {
	for _, c := range []struct {
		name, source string
		red          bool
	}{
		{"bound_call", `func keep(e error,h *struct{Cause error})bool{h.Cause=e;return true};func Use(h *struct{Cause error})bool{if e:=validateInternal();e!=nil{return keep(e,h)==true};return false}`, true},
		{"direct_call", `func keep(e error,h *struct{Cause error})bool{h.Cause=e;return true};func Use(h *struct{Cause error})bool{return keep(ErrUnknownCuratorDescriptor,h)==true}`, true},
		{"right_operand", `func keep(e error)bool{return e!=nil};func Use()bool{return false!=keep(ErrUnknownCuratorDescriptor)}`, true},
		{"nested_operand", `func keep(e error)bool{return e!=nil};func Use()bool{return ((keep(ErrUnknownCuratorDescriptor)==true)!=false)==true}`, true},
		{"sentinel", `func Use(e error)bool{return e==ErrUnknownCuratorDescriptor}`, false},
		{"bound_nil", `func Use()bool{if e:=validateInternal();e!=nil{return e!=nil};return false}`, false},
		{"classifier", `func Use()bool{if e:=validateInternal();e!=nil{return errors.Is(e,ErrUnknownCuratorDescriptor)==true};return false}`, false},
		{"nested_classifier", `func Use()bool{if e:=validateInternal();e!=nil{return (false!=errors.Is(e,ErrUnknownCuratorDescriptor))==true};return false}`, false},
	} {
		t.Run(c.name, func(t *testing.T) { reviewShape(t, c.source, c.red) })
	}
}

func TestParenthesizedKnownConsumers(t *testing.T) {
	for _, c := range []struct {
		name, source string
		red          bool
	}{
		{"wrapper", `func Use()error{if e:=validateInternal();e!=nil{return (((fmt.Errorf)))("%w",e)};return nil}`, false},
		{"classifier", `func Use()bool{if e:=validateInternal();e!=nil{return (((errors.Is)))(e,ErrUnknownCuratorDescriptor)};return false}`, false},
		{"string", `func Use()string{if e:=validateInternal();e!=nil{return ((fmt.Sprint))(e)};return ""}`, false},
		{"join", `func Use()error{if e:=validateInternal();e!=nil{return ((errors.Join))(e)};return nil}`, false},
		{"custom", `func keep(e error)bool{return e!=nil};func Use()bool{if e:=validateInternal();e!=nil{return ((keep))(e)};return false}`, true},
		{"wrong_wrap", `func Use()error{if e:=validateInternal();e!=nil{return ((fmt.Errorf))("%v: %w",e,errors.New("other"))};return nil}`, true},
	} {
		t.Run(c.name, func(t *testing.T) { reviewShape(t, c.source, c.red) })
	}
}

// Generate the storage class across target families, provenance paths and
// outcomes. The invariant rejects the WRITE, independently of what aliases
// already exist or how the caller later uses the aggregate. Each source runs
// through Discover, the same entry point called by the live coverage guard.
func TestDerivedStorageClass(t *testing.T) {
	targets := []struct{ name, setup, target, read string }{
		{"field", "var out struct{Denied bool};", "out.Denied", "out.Denied"},
		{"pointer_alias", "var out struct{Denied bool};alias:=&out;", "alias.Denied", "out.Denied"},
		{"slice_alias", "out:=make([]bool,1);alias:=out;", "alias[0]", "out[0]"},
		{"map_alias", "out:=make(map[string]bool);alias:=out;", `alias["denied"]`, `out["denied"]`},
		{"dereference", "var out bool;alias:=&out;", "*alias", "out"},
		{"parenthesized_target", "var out struct{Denied bool};alias:=&out;", "(alias.Denied)", "out.Denied"},
		{"pointer_index", "var out [1]bool;alias:=&out;", "alias[0]", "out[0]"},
	}
	paths := []struct{ name, setup, value string }{
		{"direct", "", "errors.Is(e,ErrUnknownCuratorDescriptor)"},
		{"local", "classified:=errors.Is(e,ErrUnknownCuratorDescriptor);", "classified"},
		{"value_alias", "classified:=errors.Is(e,ErrUnknownCuratorDescriptor);var copied=classified;", "copied"},
	}
	for _, target := range targets {
		for _, path := range paths {
			for _, outcome := range []string{"bool", "error", "unused"} {
				t.Run(target.name+"/"+path.name+"/"+outcome, func(t *testing.T) {
					result, tail, fallback := "bool", "return "+target.read, "false"
					if outcome == "error" {
						result, tail, fallback = "error", `return fmt.Errorf("classified: %v",`+target.read+`)`, "nil"
					} else if outcome == "unused" {
						tail = "return false"
					}
					source := fmt.Sprintf("func Use()%s{if e:=validateInternal();e!=nil{%s%s%s=%s;%s};return %s}", result, target.setup, path.setup, target.target, path.value, tail, fallback)
					reviewShape(t, source, true)
				})
			}
		}
	}
	t.Run("local_bool_control", func(t *testing.T) {
		reviewShape(t, `func Use()bool{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);alias:=classified;return alias};return false}`, false)
	})
	t.Run("inline_diagnostic_control", func(t *testing.T) {
		reviewShape(t, `type diagnostic struct{Denied bool};func Use()diagnostic{if e:=validateInternal();e!=nil{if errors.Is(e,ErrUnknownCuratorDescriptor){return diagnostic{Denied:true}}};return diagnostic{}}`, false)
	})
}
