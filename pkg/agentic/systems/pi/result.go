package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const TurnResultContract = "agents-infra.pi-turn-result"
const TurnResultSchemaVersion = 1
const TurnResultMaxStdoutBytes = 1 << 20

type TurnResultClass string

const (
	TurnResultOK               TurnResultClass = "ok"
	TurnResultProcessARefused  TurnResultClass = "process-a-refused"
	TurnResultChildFailed      TurnResultClass = "child-failed"
	TurnResultToolFailed       TurnResultClass = "tool-failed"
	TurnResultCancelled        TurnResultClass = "cancelled"
	TurnResultDeadlineExceeded TurnResultClass = "deadline-exceeded"
	TurnResultInvalid          TurnResultClass = "result-invalid"
	TurnResultCleanupFailed    TurnResultClass = "cleanup-failed"
)

type TurnResultCode string

const (
	TurnCodeCleanupFailed        TurnResultCode = "pi_turn_cleanup_failed"
	TurnCodeRequestInvalid       TurnResultCode = "pi_turn_request_invalid"
	TurnCodeProfileMissing       TurnResultCode = "pi_turn_profile_missing"
	TurnCodeProfileUnknown       TurnResultCode = "pi_turn_profile_unknown"
	TurnCodeProfileMismatch      TurnResultCode = "pi_turn_profile_mismatch"
	TurnCodeEnvironmentMalformed TurnResultCode = "pi_turn_environment_malformed"
	TurnCodeEnvironmentDenied    TurnResultCode = "pi_turn_environment_denied"
	TurnCodeConfigurationInvalid TurnResultCode = "pi_turn_configuration_invalid"
	TurnCodeAuthorizationDenied  TurnResultCode = "pi_turn_authorization_denied"
	TurnCodeIdentityInvalid      TurnResultCode = "pi_turn_identity_invalid"
	TurnCodeRuntimeRefused       TurnResultCode = "pi_turn_runtime_refused"
	// TurnCodeLifecycleIntegrityUnknown is Process A refusing a turn because
	// it could not establish the integrity of its own lifecycle evidence
	// (lease, restart or quarantine ledger, cache state). The wire document
	// carries only this code: no paths, identity, raw child errors or cache
	// contents. It is a refusal, not a child or tool failure, and pairs with
	// exit 1 exactly like every other Process-A refusal.
	TurnCodeLifecycleIntegrityUnknown TurnResultCode = "pi_turn_lifecycle_integrity_unknown"
	TurnCodeChildFailed               TurnResultCode = "pi_turn_child_failed"
	TurnCodeToolFailed                TurnResultCode = "pi_turn_tool_failed"
	TurnCodeCancelled                 TurnResultCode = "pi_turn_cancelled"
	TurnCodeDeadlineExceeded          TurnResultCode = "pi_turn_deadline_exceeded"
	TurnCodeResultInvalid             TurnResultCode = "pi_turn_result_invalid"
)

type TurnIntervention string

const (
	TurnInterventionNone     TurnIntervention = "none"
	TurnInterventionCancel   TurnIntervention = "cancel"
	TurnInterventionDeadline TurnIntervention = "deadline"
)

type ProcessACleanupOutcome string

const (
	ProcessACleanupNotRequired ProcessACleanupOutcome = "not-required"
	ProcessACleanupSucceeded   ProcessACleanupOutcome = "succeeded"
	ProcessACleanupFailed      ProcessACleanupOutcome = "failed"
)

type ProcessAExit struct {
	Code     int
	Signaled bool
}

type TurnResultInput struct {
	Stdout          []byte
	StdoutTruncated bool
	Exit            ProcessAExit
	Intervention    TurnIntervention
	Cleanup         ProcessACleanupOutcome
}

type TurnResult struct {
	Class     TurnResultClass
	Code      TurnResultCode
	FinalText string
}

type TurnResultError struct {
	Class TurnResultClass
	Code  TurnResultCode
}

func (e *TurnResultError) Error() string {
	return fmt.Sprintf("pi turn result: class=%s code=%s", e.Class, e.Code)
}

func (e *TurnResultError) Unwrap() error {
	switch e.Class {
	case TurnResultProcessARefused:
		return ErrTurnProcessARefused
	case TurnResultChildFailed:
		return ErrTurnChildFailed
	case TurnResultToolFailed:
		return ErrTurnToolFailed
	case TurnResultCancelled:
		return context.Canceled
	case TurnResultDeadlineExceeded:
		return context.DeadlineExceeded
	case TurnResultCleanupFailed:
		return ErrTurnCleanupFailed
	default:
		return ErrTurnResultInvalid
	}
}

var (
	ErrTurnResultInvalid   = errors.New("pi: turn result is invalid")
	ErrTurnProcessARefused = errors.New("pi: Process A refused the turn")
	ErrTurnChildFailed     = errors.New("pi: child process failed")
	ErrTurnToolFailed      = errors.New("pi: tool failed")
	ErrTurnCleanupFailed   = errors.New("pi: Process A cleanup failed")
)

// ValidateTurnResult is the sole schema-1 parser and classifier. It copies
// stdout before validation and never includes document contents in errors.
func ValidateTurnResult(input TurnResultInput) (TurnResult, error) {
	stdout := append([]byte(nil), input.Stdout...)
	switch input.Intervention {
	case TurnInterventionNone:
		if input.Cleanup != ProcessACleanupNotRequired || input.Exit.Signaled {
			return invalidTurnResult()
		}
	case TurnInterventionCancel, TurnInterventionDeadline:
		if input.Cleanup != ProcessACleanupSucceeded && input.Cleanup != ProcessACleanupFailed {
			return invalidTurnResult()
		}
		if input.Cleanup == ProcessACleanupFailed {
			return errorTurnResult(TurnResultCleanupFailed, TurnCodeCleanupFailed)
		}
		if input.Intervention == TurnInterventionCancel {
			return errorTurnResult(TurnResultCancelled, TurnCodeCancelled)
		}
		return errorTurnResult(TurnResultDeadlineExceeded, TurnCodeDeadlineExceeded)
	default:
		return invalidTurnResult()
	}
	if input.StdoutTruncated || len(stdout) > TurnResultMaxStdoutBytes {
		return invalidTurnResult()
	}

	if input.Exit.Code < 0 || input.Exit.Code > 3 {
		return invalidTurnResult()
	}
	document, err := decodeTurnResultDocument(stdout)
	if err != nil || document.contract != TurnResultContract || document.schemaVersion != TurnResultSchemaVersion {
		return invalidTurnResult()
	}

	if document.status == "ok" {
		if input.Exit.Code != 0 || !document.finalTextPresent || document.errorPresent {
			return invalidTurnResult()
		}
		return TurnResult{Class: TurnResultOK, FinalText: document.finalText}, nil
	}
	if document.status != "error" || document.finalTextPresent || !document.errorPresent {
		return invalidTurnResult()
	}
	class, expectedExit, ok := turnCodeClass(document.code)
	if !ok || input.Exit.Code != expectedExit {
		return invalidTurnResult()
	}
	return errorTurnResult(class, document.code)
}

func invalidTurnResult() (TurnResult, error) {
	return errorTurnResult(TurnResultInvalid, TurnCodeResultInvalid)
}

func errorTurnResult(class TurnResultClass, code TurnResultCode) (TurnResult, error) {
	result := TurnResult{Class: class, Code: code}
	return result, &TurnResultError{Class: class, Code: code}
}

func turnCodeClass(code TurnResultCode) (TurnResultClass, int, bool) {
	switch code {
	case TurnCodeCleanupFailed:
		return TurnResultCleanupFailed, 1, true
	case TurnCodeRequestInvalid, TurnCodeProfileMissing, TurnCodeProfileUnknown, TurnCodeProfileMismatch,
		TurnCodeEnvironmentMalformed, TurnCodeEnvironmentDenied, TurnCodeConfigurationInvalid,
		TurnCodeAuthorizationDenied, TurnCodeIdentityInvalid, TurnCodeRuntimeRefused,
		TurnCodeLifecycleIntegrityUnknown:
		return TurnResultProcessARefused, 1, true
	case TurnCodeChildFailed:
		return TurnResultChildFailed, 1, true
	case TurnCodeToolFailed:
		return TurnResultToolFailed, 1, true
	case TurnCodeCancelled:
		return TurnResultCancelled, 2, true
	case TurnCodeDeadlineExceeded:
		return TurnResultDeadlineExceeded, 2, true
	case TurnCodeResultInvalid:
		return TurnResultInvalid, 3, true
	default:
		return "", 0, false
	}
}

type turnResultDocument struct {
	contract         string
	schemaVersion    int
	status           string
	finalText        string
	finalTextPresent bool
	errorPresent     bool
	code             TurnResultCode
}

func decodeTurnResultDocument(data []byte) (turnResultDocument, error) {
	if !utf8.Valid(data) {
		return turnResultDocument{}, errors.New("invalid UTF-8")
	}
	// encoding/json deliberately skips leading JSON whitespace. Schema 1 is
	// narrower: stdout starts with the one result object, while only trailing
	// ASCII whitespace is permitted after it.
	if len(data) == 0 || data[0] != '{' {
		return turnResultDocument{}, errors.New("result does not start with an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return turnResultDocument{}, errors.New("result is not an object")
	}
	var result turnResultDocument
	seen := map[string]bool{}
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return turnResultDocument{}, err
		}
		name, ok := nameToken.(string)
		if !ok || seen[name] {
			return turnResultDocument{}, errors.New("duplicate or invalid member")
		}
		seen[name] = true
		switch name {
		case "contract":
			err = decoder.Decode(&result.contract)
		case "schema_version":
			err = decoder.Decode(&result.schemaVersion)
		case "status":
			err = decoder.Decode(&result.status)
		case "final_text":
			result.finalTextPresent = true
			err = decoder.Decode(&result.finalText)
		case "error":
			result.errorPresent = true
			result.code, err = decodeTurnError(decoder)
		default:
			return turnResultDocument{}, errors.New("unknown member")
		}
		if err != nil {
			return turnResultDocument{}, err
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return turnResultDocument{}, errors.New("unterminated result object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return turnResultDocument{}, errors.New("trailing JSON value")
	}
	if !seen["contract"] || !seen["schema_version"] || !seen["status"] {
		return turnResultDocument{}, errors.New("missing result member")
	}
	return result, nil
}

func decodeTurnError(decoder *json.Decoder) (TurnResultCode, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return "", errors.New("error is not an object")
	}
	var code TurnResultCode
	seenCode := false
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return "", err
		}
		name, ok := nameToken.(string)
		if !ok || name != "code" || seenCode {
			return "", errors.New("unknown or duplicate error member")
		}
		seenCode = true
		if err := decoder.Decode(&code); err != nil {
			return "", err
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') || !seenCode {
		return "", errors.New("invalid error object")
	}
	return code, nil
}
