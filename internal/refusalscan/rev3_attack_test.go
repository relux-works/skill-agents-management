package refusalscan

import "testing"

func TestRev3FixBoundary(t *testing.T) {
	for _, c := range []struct {
		name, source string
		red          bool
	}{
		{"comparison_call_storage", `func keep(e error,h *struct{Cause error})bool{h.Cause=e;return true};func Use(h *struct{Cause error})bool{if e:=validateInternal();e!=nil{return keep(e,h)==true};return false}`, true},
		{"comparison_direct_call_storage", `func keep(e error,h *struct{Cause error})bool{h.Cause=e;return true};func Use(h *struct{Cause error})bool{return keep(ErrUnknownCuratorDescriptor,h)==true}`, true},
		{"classification_pointer_alias", `func Use()error{if e:=validateInternal();e!=nil{var out struct{Denied bool};p:=&out;p.Denied=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out.Denied)};return nil}`, true},
		{"classification_slice_alias", `func Use()error{if e:=validateInternal();e!=nil{out:=make([]bool,1);alias:=out;alias[0]=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out[0])};return nil}`, true},
		{"wrapped_pointer_bool_control", `func Use()bool{if e:=validateInternal();e!=nil{var out struct{Denied bool};p:=&out;p.Denied=errors.Is(e,ErrUnknownCuratorDescriptor);return out.Denied};return false}`, true},
	} {
		t.Run(c.name, func(t *testing.T) { reviewShape(t, c.source, c.red) })
	}
}

func TestRev3FreeHunt(t *testing.T) {
	for _, c := range []struct{ name, source string }{
		{"parenthesized_wrapper", `func Use()error{if e:=validateInternal();e!=nil{return (fmt.Errorf)("%w",e)};return nil}`},
		{"parenthesized_classifier", `func Use()bool{if e:=validateInternal();e!=nil{return (errors.Is)(e,ErrUnknownCuratorDescriptor)};return false}`},
	} {
		t.Run(c.name, func(t *testing.T) { reviewShape(t, c.source, false) })
	}
}
