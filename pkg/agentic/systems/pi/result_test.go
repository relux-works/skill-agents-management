package pi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func okTurnDocument(text string) []byte {
	return []byte(fmt.Sprintf(`{"contract":"%s","schema_version":1,"status":"ok","final_text":%q}`, TurnResultContract, text))
}

func errorTurnDocument(code TurnResultCode) []byte {
	return []byte(fmt.Sprintf(`{"contract":"%s","schema_version":1,"status":"error","error":{"code":%q}}`, TurnResultContract, code))
}

func uninterruptedInput(stdout []byte, exit int) TurnResultInput {
	return TurnResultInput{Stdout: stdout, Exit: ProcessAExit{Code: exit}, Intervention: TurnInterventionNone, Cleanup: ProcessACleanupNotRequired}
}

func TestValidateTurnResultAcceptsOnlyClosedExitDocumentTable(t *testing.T) {
	result, err := ValidateTurnResult(uninterruptedInput(okTurnDocument("answer"), 0))
	if err != nil || result != (TurnResult{Class: TurnResultOK, FinalText: "answer"}) {
		t.Fatalf("ok result=%+v err=%v", result, err)
	}

	tests := []struct {
		code     TurnResultCode
		class    TurnResultClass
		sentinel error
		exit     int
	}{
		{TurnCodeCleanupFailed, TurnResultCleanupFailed, ErrTurnCleanupFailed, 1},
		{TurnCodeRequestInvalid, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeProfileMissing, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeProfileUnknown, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeProfileMismatch, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeEnvironmentMalformed, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeEnvironmentDenied, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeConfigurationInvalid, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeAuthorizationDenied, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeIdentityInvalid, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeRuntimeRefused, TurnResultProcessARefused, ErrTurnProcessARefused, 1},
		{TurnCodeChildFailed, TurnResultChildFailed, ErrTurnChildFailed, 1},
		{TurnCodeToolFailed, TurnResultToolFailed, ErrTurnToolFailed, 1},
		{TurnCodeCancelled, TurnResultCancelled, context.Canceled, 2},
		{TurnCodeDeadlineExceeded, TurnResultDeadlineExceeded, context.DeadlineExceeded, 2},
		{TurnCodeResultInvalid, TurnResultInvalid, ErrTurnResultInvalid, 3},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			result, err := ValidateTurnResult(uninterruptedInput(errorTurnDocument(test.code), test.exit))
			if !errors.Is(err, test.sentinel) || result.Class != test.class || result.Code != test.code {
				t.Fatalf("result=%+v err=%v, want class=%s code=%s sentinel=%v", result, err, test.class, test.code, test.sentinel)
			}
			var typed *TurnResultError
			if !errors.As(err, &typed) || typed.Class != test.class || typed.Code != test.code {
				t.Fatalf("typed error=%+v from %v", typed, err)
			}
		})
	}
}

func TestValidateTurnResultInterventionAndCleanupPrecedence(t *testing.T) {
	for _, test := range []struct {
		name         string
		intervention TurnIntervention
		cleanup      ProcessACleanupOutcome
		class        TurnResultClass
		sentinel     error
	}{
		{"cancel cleanup succeeds", TurnInterventionCancel, ProcessACleanupSucceeded, TurnResultCancelled, context.Canceled},
		{"deadline cleanup succeeds", TurnInterventionDeadline, ProcessACleanupSucceeded, TurnResultDeadlineExceeded, context.DeadlineExceeded},
		{"cancel cleanup fails", TurnInterventionCancel, ProcessACleanupFailed, TurnResultCleanupFailed, ErrTurnCleanupFailed},
		{"deadline cleanup fails", TurnInterventionDeadline, ProcessACleanupFailed, TurnResultCleanupFailed, ErrTurnCleanupFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ValidateTurnResult(TurnResultInput{
				Stdout: []byte("not-json and deliberately disagreeing"), StdoutTruncated: true, Exit: ProcessAExit{Code: 137, Signaled: true},
				Intervention: test.intervention, Cleanup: test.cleanup,
			})
			if !errors.Is(err, test.sentinel) || result.Class != test.class {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestValidateTurnResultRefusesProtocolAndInputAttacks(t *testing.T) {
	validOK := string(okTurnDocument("answer"))
	validError := string(errorTurnDocument(TurnCodeChildFailed))
	tests := []struct {
		name  string
		input TurnResultInput
	}{
		{"absent", uninterruptedInput(nil, 0)},
		{"duplicate contract", uninterruptedInput([]byte(strings.Replace(validOK, `"contract":`, `"contract":"forged","contract":`, 1)), 0)},
		{"duplicate nested code", uninterruptedInput([]byte(strings.Replace(validError, `"code":`, `"code":"pi_turn_child_failed","code":`, 1)), 1)},
		{"unknown field", uninterruptedInput([]byte(strings.Replace(validOK, `"status":`, `"extra":1,"status":`, 1)), 0)},
		{"leading space", uninterruptedInput([]byte(" "+validOK), 0)},
		{"leading tab", uninterruptedInput([]byte("\t"+validOK), 0)},
		{"leading carriage return", uninterruptedInput([]byte("\r"+validOK), 0)},
		{"leading line feed", uninterruptedInput([]byte("\n"+validOK), 0)},
		{"second value", uninterruptedInput([]byte(validOK+` {}`), 0)},
		{"non-whitespace trailing", uninterruptedInput([]byte(validOK+` x`), 0)},
		{"invalid utf8", uninterruptedInput([]byte{0xff}, 0)},
		{"unknown contract", uninterruptedInput([]byte(strings.Replace(validOK, TurnResultContract, "forged", 1)), 0)},
		{"unknown schema", uninterruptedInput([]byte(strings.Replace(validOK, `"schema_version":1`, `"schema_version":2`, 1)), 0)},
		{"unknown status", uninterruptedInput([]byte(strings.Replace(validOK, `"status":"ok"`, `"status":"maybe"`, 1)), 0)},
		{"ok with error", uninterruptedInput([]byte(strings.Replace(validOK, `}`, `,"error":{"code":"pi_turn_child_failed"}}`, 1)), 0)},
		{"error with final text", uninterruptedInput([]byte(strings.Replace(validError, `,"error"`, `,"final_text":"forged","error"`, 1)), 1)},
		{"unknown code", uninterruptedInput(errorTurnDocument("pi_turn_future"), 1)},
		{"zero with error", uninterruptedInput(errorTurnDocument(TurnCodeChildFailed), 0)},
		{"one with ok", uninterruptedInput(okTurnDocument("answer"), 1)},
		{"wrong error exit", uninterruptedInput(errorTurnDocument(TurnCodeCancelled), 1)},
		{"signal without intervention", TurnResultInput{Stdout: okTurnDocument("answer"), Exit: ProcessAExit{Code: 0, Signaled: true}, Intervention: TurnInterventionNone, Cleanup: ProcessACleanupNotRequired}},
		{"unknown exit", uninterruptedInput(okTurnDocument("answer"), 4)},
		{"truncated", TurnResultInput{Stdout: okTurnDocument("answer"), StdoutTruncated: true, Exit: ProcessAExit{Code: 0}, Intervention: TurnInterventionNone, Cleanup: ProcessACleanupNotRequired}},
		{"cleanup without intervention", TurnResultInput{Stdout: okTurnDocument("answer"), Exit: ProcessAExit{Code: 0}, Intervention: TurnInterventionNone, Cleanup: ProcessACleanupSucceeded}},
		{"intervention without cleanup", TurnResultInput{Intervention: TurnInterventionCancel, Cleanup: ProcessACleanupNotRequired}},
		{"unknown intervention", TurnResultInput{Intervention: "future", Cleanup: ProcessACleanupSucceeded}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ValidateTurnResult(test.input)
			if !errors.Is(err, ErrTurnResultInvalid) || result.Class != TurnResultInvalid || result.Code != TurnCodeResultInvalid {
				t.Fatalf("result=%+v err=%v, want closed invalid class", result, err)
			}
		})
	}
}

func TestValidateTurnResultEnforcesOneMiBBoundAndPermitsTrailingASCIIWhitespace(t *testing.T) {
	document := okTurnDocument("answer")
	trailing := []byte(" \t\r\n")
	if _, err := ValidateTurnResult(uninterruptedInput(append(append([]byte(nil), document...), trailing...), 0)); err != nil {
		t.Fatalf("trailing ASCII whitespace refused: %v", err)
	}
	exact := append(append([]byte(nil), document...), []byte(strings.Repeat(" ", TurnResultMaxStdoutBytes-len(document)))...)
	if _, err := ValidateTurnResult(uninterruptedInput(exact, 0)); err != nil {
		t.Fatalf("exact bound refused: %v", err)
	}
	over := append(exact, ' ')
	if _, err := ValidateTurnResult(uninterruptedInput(over, 0)); !errors.Is(err, ErrTurnResultInvalid) {
		t.Fatalf("over bound = %v, want ErrTurnResultInvalid", err)
	}
}

func TestTurnResultErrorDoesNotExposeDocumentContents(t *testing.T) {
	secret := "profile=/secret/path token=hunter2"
	_, err := ValidateTurnResult(uninterruptedInput(okTurnDocument(secret), 1))
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("sanitized error = %q", err)
	}
}
