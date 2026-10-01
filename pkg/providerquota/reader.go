package providerquota

import (
	"bytes"
	"encoding/json"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"time"
)

type CwdPolicy string

const ScratchCwd CwdPolicy = "scratch"

type Request struct {
	Context ParseContext
	Env     []string
	// Basis is required when the plugin has no Reader; it names public evidence.
	Basis string
	// ForbiddenRoots are additional credential/configuration roots supplied by
	// the consumer. They are compared as strings and never inspected on disk.
	ForbiddenRoots []string
}

// Reader is optional; agentic.System remains closed. Implementations declare
// values and parse bytes. Neither method executes a process or reads a home.
type Reader interface {
	QuotaPlan(Request) (QuotaPlan, error)
	ParseQuota([]byte, ParseContext) (QuotaRecord, error)
}

// QuotaPlan is a value for a consumer-owned executor. Argv excludes Binary.
// Use an empty scratch cwd. If HoldStdinOpen is true, close stdin as soon as
// Complete returns true; otherwise close after Stdin and ignore Complete.
// Handshake protocols may set AfterInitialize: write only Stdin initially,
// wait for the id-1 result, then send AfterInitialize. An id-1 error aborts.
// Complete accepts only complete frames, never a partial JSON line.
// Parse is pure and receives explicit clocks and identity evidence.
type QuotaPlan struct {
	Binary          string
	Argv            []string
	Env             []string
	Stdin           []byte
	AfterInitialize []byte
	HoldStdinOpen   bool
	Complete        func([]byte) bool
	Timeout         time.Duration
	CwdPolicy       CwdPolicy
	SideEffectFlags []string
	// Ready is false when protocol details still need an operator decision.
	// An executor must refuse such a plan, with RefusalReason, without starting it.
	Ready         bool
	RefusalReason string
	Parse         func([]byte, ParseContext) (QuotaRecord, error)
}
type DispatchResult struct {
	Plan   *QuotaPlan
	Record *QuotaRecord
}

// Dispatch is a type assertion, with no system-identifier branch.
func Dispatch(system agentic.System, req Request) (DispatchResult, error) {
	if reader, ok := system.(Reader); ok {
		plan, err := reader.QuotaPlan(req)
		if err != nil {
			return DispatchResult{}, err
		}
		plan.Parse = reader.ParseQuota
		return DispatchResult{Plan: &plan}, nil
	}
	if req.Basis == "" {
		return DispatchResult{}, Refuse("basis_required")
	}
	record, err := Base(req.Context, "")
	if err != nil {
		return DispatchResult{}, err
	}
	record.State = NotSupported
	record.Basis = req.Basis
	record, err = Finish(record, req.Context)
	return DispatchResult{Record: &record}, err
}

// RPCResult extracts a single id-tagged JSONL response, ignoring notifications.
// No raw error message is propagated. Frames are bounded by the executor.
func RPCResult(stdout []byte, id int) (json.RawMessage, error) {
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			if msg.Error.Code == 401 || msg.Error.Code == -401 {
				return nil, Refuse("unauthenticated")
			}
			return nil, Refuse("rpc_error")
		}
		if len(msg.Result) == 0 || bytes.Equal(msg.Result, []byte("null")) {
			return nil, Refuse("payload_missing")
		}
		return msg.Result, nil
	}
	return nil, Refuse("response_missing")
}
func RPCComplete(id int) func([]byte) bool {
	return func(b []byte) bool {
		for _, line := range bytes.SplitAfter(b, []byte{'\n'}) {
			if len(line) == 0 || line[len(line)-1] != '\n' {
				continue
			}
			var msg struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(line, &msg) == nil && msg.ID != nil && *msg.ID == id && (len(msg.Result) > 0 || len(msg.Error) > 0) {
				return true
			}
		}
		return false
	}
}

// Coverage counts failures in the denominator, never as unsupported.
type Coverage struct {
	Readable    int      `json:"readable"`
	Of          int      `json:"of"`
	Unsupported []string `json:"unsupported"`
}

func Summarize(records []QuotaRecord) Coverage {
	out := Coverage{Of: len(records), Unsupported: []string{}}
	for _, r := range records {
		switch r.State {
		case Exact, PercentOnly, LastObserved:
			out.Readable++
		case NotSupported:
			out.Unsupported = append(out.Unsupported, r.Runtime)
		}
	}
	return out
}
