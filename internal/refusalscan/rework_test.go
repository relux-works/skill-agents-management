package refusalscan

import (
	"errors"
	"fmt"
	"testing"
)

func TestReworkScopedConsumption(t *testing.T) {
	cases := []struct {
		name, source string
		red          bool
	}{
		{"classification_field", `func Use()error{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);var out struct{Denied bool};out.Denied=classified;return fmt.Errorf("classified: %v",out.Denied)};return nil}`, true},
		{"classification_index", `func Use()error{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);out:=make([]bool,1);out[0]=classified;return fmt.Errorf("classified: %v",out[0])};return nil}`, true},
		{"classification_direct_field", `func Use()error{if e:=validateInternal();e!=nil{var out struct{Denied bool};out.Denied=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out.Denied)};return nil}`, true},
		{"classification_direct_index", `func Use()error{if e:=validateInternal();e!=nil{out:=make(map[string]bool);out["denied"]=errors.Is(e,ErrUnknownCuratorDescriptor);return fmt.Errorf("classified: %v",out["denied"])};return nil}`, true},
		{"classification_field_bool", `func Use()bool{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);var out struct{Denied bool};out.Denied=classified;return out.Denied};return false}`, true},
		{"classification_index_bool", `func Use()bool{if e:=validateInternal();e!=nil{out:=make([]bool,1);out[0]=errors.Is(e,ErrUnknownCuratorDescriptor);return out[0]};return false}`, true},
		{"diagnostic_call", `type diagnostic struct{Cause error};func pack(e error)diagnostic{return diagnostic{e}};func Use()diagnostic{if e:=validateInternal();e!=nil{return pack(e)};return diagnostic{}}`, true},
		{"inline_diagnostic", `type diagnostic struct{Denied bool};func Use()diagnostic{if e:=validateInternal();e!=nil{if errors.Is(e,ErrUnknownCuratorDescriptor){return diagnostic{Denied:true}}};return diagnostic{}}`, false},
		{"custom_bool_call", `func classify(e error)bool{return e!=nil};func Use()bool{if e:=validateInternal();e!=nil{return classify(e)};return false}`, true},
		{"custom_string_call", `func describe(e error)string{return e.Error()};func Use()string{if e:=validateInternal();e!=nil{return describe(e)};return ""}`, true},
		{"second_error_drop", `func tuple()(error,error){return validateInternal(),validateInternal()};func Use()error{if e,_:=tuple();e!=nil{return e};return nil}`, true},
		{"first_error_drop", `func tuple()(error,error){return validateInternal(),validateInternal()};func Use()error{if _,e:=tuple();e!=nil{return e};return nil}`, true},
		{"second_error_escape", `func tuple()(error,error){return validateInternal(),validateInternal()};func Use(h *struct{Cause error})error{if e,other:=tuple();e!=nil{h.Cause=other;return e};return nil}`, true},
		{"two_errors_propagated", `func tuple()(error,error){return validateInternal(),validateInternal()};func Use()error{if e,other:=tuple();e!=nil{return errors.Join(e,other)};return nil}`, false},
		{"switch_case", `func Use()bool{switch e:=validateInternal();{case errors.Is(e,ErrUnknownCuratorDescriptor):return true};return false}`, false},
		{"switch_case_escape", `func Use()error{switch e:=validateInternal();{case errors.Is(e,ErrUnknownCuratorDescriptor):return fmt.Errorf("classified: %v",errors.Is(e,ErrUnknownCuratorDescriptor))};return nil}`, true},
		{"width_wrap", `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%*w",10,e)};return nil}`, false},
		{"precision_wrap", `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%.*w",2,e)};return nil}`, false},
		{"width_precision_wrap", `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%*.*w",10,2,e)};return nil}`, false},
		{"indexed_star_wrap", `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%[1]*.[2]*[3]w",10,2,e)};return nil}`, false},
		{"width_wrong_arg", `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%*v: %w",10,e,errors.New("other"))};return nil}`, true},
		{"sprintf_wrap", `func Use()string{if e:=validateInternal();e!=nil{return fmt.Sprintf("%w",e)};return ""}`, true},
		{"slog_output", `func Use(){if e:=validateInternal();e!=nil{slog.Error("refused", "error", e)}}`, false},
		{"slog_retention", `func Use()*slog.Logger{if e:=validateInternal();e!=nil{return slog.Default().With("error", e)};return nil}`, true},
		{"log_retention", `type writer struct{};func(writer)Write(p []byte)(int,error){return len(p),nil};func(writer)Error()string{return "writer"};var Other=writer{};func Use()*log.Logger{return log.New(Other,"",0)}`, true},
		{"sprintf_text", `func Use()string{if e:=validateInternal();e!=nil{return fmt.Sprintf("%v",e)};return ""}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { reviewShape(t, c.source, c.red) })
	}
}

func TestStarWrappingPreservesRuntimeIdentity(t *testing.T) {
	refusal := errors.New("refusal")
	cases := []struct {
		name, format string
		args         []any
	}{
		{"width", "%*w", []any{10, refusal}},
		{"precision", "%.*w", []any{2, refusal}},
		{"both", "%*.*w", []any{10, 2, refusal}},
		{"indexed", "%[1]*.[2]*[3]w", []any{10, 2, refusal}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if wrapped := fmt.Errorf(c.format, c.args...); !errors.Is(wrapped, refusal) {
				t.Fatalf("%s lost refusal identity: %v", c.format, wrapped)
			}
		})
	}
}
