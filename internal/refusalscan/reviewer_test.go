package refusalscan

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func reviewShape(t *testing.T, source string, red bool) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "pkg/agentic/reviewer.go", identityFixture+source)
	sites, err := Discover(root)
	if red {
		if err == nil || !strings.Contains(err.Error(), "typed refusal use outside") || !strings.Contains(err.Error(), "reviewer.go:") {
			t.Fatalf("forbidden shape admitted: err=%v sites=%v", err, sites)
		}
	} else if err != nil {
		t.Fatal(err)
	}
}
func TestReviewerS1(t *testing.T) {
	reviewShape(t, `type denial int;func(denial) Error()string{return "denial"};const Other=denial(1);func Use()error{return Other}`, false)
}
func TestReviewerS2(t *testing.T) {
	reviewShape(t, `func first()error{return validateInternal()};func second()error{return first()};func Use()error{value:=second();return value}`, true)
}
func TestReviewerS3(t *testing.T) {
	reviewShape(t, `func Use()(result error){if e:=validateInternal();e!=nil{result=e;return};return}`, true)
}
func TestReviewerS4(t *testing.T) {
	reviewShape(t, `func init(){if e:=validateInternal();e!=nil{panic(fmt.Sprint(e))}}`, false)
}
func TestReviewerS5(t *testing.T) {
	reviewShape(t, `func Use()error{if e:=validateInternal();e!=nil{panic(e)};return nil}`, true)
}
func TestReviewerS6(t *testing.T) {
	reviewShape(t, `func Classify()bool{switch e:=validateInternal();{case errors.Is(e,ErrUnknownCuratorDescriptor):return true};return false}`, false)
}
func TestReviewerS7(t *testing.T) {
	for name, source := range map[string]string{
		"field": `func Use()error{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);var out struct{Denied bool};out.Denied=classified;return fmt.Errorf("classified: %v",out.Denied)};return nil}`,
		"index": `func Use()error{if e:=validateInternal();e!=nil{classified:=errors.Is(e,ErrUnknownCuratorDescriptor);out:=make([]bool,1);out[0]=classified;return fmt.Errorf("classified: %v",out[0])};return nil}`,
	} {
		t.Run(name, func(t *testing.T) { reviewShape(t, source, true) })
	}
}
func TestReviewerS8(t *testing.T) {
	reviewShape(t, `func Args()error{return validateInternal()};func Use(){Args:=func()int{return 1};value:=Args();_ =value}`, false)
}
func TestReviewerS10(t *testing.T) {
	reviewShape(t, `func Lookup()(any,bool){switch e:=validateInternal();{case e!=nil:return nil,false};return 1,true}`, false)
}
func TestReviewerS11(t *testing.T) {
	reviewShape(t, `type diagnostic struct{Message string};func Inspect()diagnostic{if e:=validateInternal();e!=nil{return diagnostic{fmt.Sprint(e)}};return diagnostic{}}`, false)
}
func TestReviewerS12(t *testing.T) {
	reviewShape(t, `type issue struct{};func(issue)Error()string{return "issue"};func(issue)Unwrap()[]error{return []error{ErrUnknownCuratorDescriptor}}`, false)
}
func TestReviewerStorage(t *testing.T) {
	for name, source := range map[string]string{
		"map":             `func Use(m map[string]error){if e:=validateInternal();e!=nil{m["error"]=e}}`,
		"consume_call":    `var retained error;func keep(e error)bool{retained=e;return true};func Use()bool{if e:=validateInternal();e!=nil{return keep(e)};return false}`,
		"diagnostic_call": `type diagnostic struct{Cause error};func pack(e error)diagnostic{return diagnostic{e}};func Use()diagnostic{if e:=validateInternal();e!=nil{return pack(e)};return diagnostic{}}`,
	} {
		t.Run(name, func(t *testing.T) { reviewShape(t, source, true) })
	}
}
func TestReviewerFreeHunt(t *testing.T) {
	t.Run("width_wrap", func(t *testing.T) {
		reviewShape(t, `func Use()error{if e:=validateInternal();e!=nil{return fmt.Errorf("%*w",10,e)};return nil}`, false)
	})
	t.Run("second_error_drop", func(t *testing.T) {
		reviewShape(t, `func tuple()(error,error){return validateInternal(),validateInternal()};func Use()error{if e,_:=tuple();e!=nil{return e};return nil}`, true)
	})
}

func TestReviewerWrapRuntime(t *testing.T) {
	e := errors.New("refusal")
	wrapped := fmt.Errorf("%*w", 10, e)
	if !errors.Is(wrapped, e) {
		t.Fatal("not wrapped")
	}
	t.Logf("width-star wrapping preserves error identity: %v", wrapped)
}
