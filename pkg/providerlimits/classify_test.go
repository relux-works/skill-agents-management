package providerlimits

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// --- Codex, prompt mode -----------------------------------------------------

// The two real 2026-08-01 tails are the whole reason this feature exists.
func TestCodexRealLogTailsClassifyAsProviderQuota(t *testing.T) {
	for _, name := range []string{
		"codex-usage-limit-RUN-260801-a714a6.log",
		"codex-usage-limit-RUN-260801-c95402.log",
	} {
		t.Run(name, func(t *testing.T) {
			got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: readFixture(t, name)})
			if got.Class != ClassProviderQuota {
				t.Fatalf("class = %q (%s), want provider_quota", got.Class, got.Reason)
			}
			if got.Provider != ProviderCodex {
				t.Errorf("provider = %q", got.Provider)
			}
			if !strings.Contains(strings.ToLower(got.Excerpt), "you've hit your usage limit") {
				t.Errorf("excerpt = %q, want the observed marker line", got.Excerpt)
			}
			if got.ResetHint == nil {
				t.Fatal("the observed line carries a reset hint and it must be captured for diagnosis")
			}
			if !strings.Contains(got.ResetHint.Raw, "Aug 5th, 2026 10:30 AM") {
				t.Errorf("reset hint raw = %q", got.ResetHint.Raw)
			}
		})
	}
}

func TestCodexTemplateVariantsAllClassify(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(string(readFixture(t, "codex-templates.txt"))), "\n")
	if len(lines) < 8 {
		t.Fatalf("expected the full template family, got %d lines", len(lines))
	}
	for _, line := range lines {
		got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("some earlier output\n" + line + "\n")})
		if got.Class != ClassProviderQuota {
			t.Errorf("template %q classified as %q (%s), want provider_quota", line, got.Class, got.Reason)
		}
	}
}

// The marker is anchored at line start and requires the ERROR: prefix. This
// repository's own logs and design documents are full of the phrase, and a
// narrated occurrence must not be able to suppress a provider.
func TestCodexMarkerMustBeAtLineStartWithErrorPrefix(t *testing.T) {
	cases := []struct {
		name string
		log  string
	}{
		{"indented", "    ERROR: You've hit your usage limit.\n"},
		{"quoted with a prefix", "> ERROR: You've hit your usage limit.\n"},
		{"no ERROR prefix", "You've hit your usage limit.\n"},
		{"mid line", "the child said ERROR: You've hit your usage limit.\n"},
		{"warning instead of error", "WARN: You've hit your usage limit.\n"},
		{"different marker", "ERROR: You've hit your rate limit.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte(tc.log)})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q, want not-a-limit; the gate wrongly admitted %q", got.Class, tc.log)
			}
		})
	}
}

func TestCodexQuotedDesignDocumentIsNotALimit(t *testing.T) {
	got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: readFixture(t, "codex-marker-quoted.log")})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q, want not-a-limit: a transcript that quotes the marker is not a provider error", got.Class)
	}
}

// Exit code 0 is never classified, whatever the transcript says.
func TestExitZeroIsNeverClassified(t *testing.T) {
	t.Run("codex", func(t *testing.T) {
		got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 0, Log: readFixture(t, "codex-usage-limit-RUN-260801-a714a6.log")})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q on an exit-0 run carrying the real marker", got.Class)
		}
	})
	t.Run("claude", func(t *testing.T) {
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 0, Stdout: readFixture(t, "claude-429-usage-limit.json")})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q on an exit-0 run carrying the real 429 envelope", got.Class)
		}
	})
	t.Run("claude success whose result quotes the markers", func(t *testing.T) {
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 0, Stdout: readFixture(t, "claude-success-with-limit-prose.json")})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q on a successful run that merely discusses the markers", got.Class)
		}
	})
}

func TestCodexScansOnlyTheLastScanWindow(t *testing.T) {
	marker := "ERROR: You've hit your usage limit.\n"
	filler := strings.Repeat("x\n", (ClassifyScanBytes/2)+4096)
	got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte(marker + filler)})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q; a marker older than the %d-byte scan window must be out of reach", got.Class, ClassifyScanBytes)
	}
	got = ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte(filler + marker)})
	if got.Class != ClassProviderQuota {
		t.Fatalf("class = %q; a marker inside the scan window must be found", got.Class)
	}
}

// The same defect class as the Claude framing cut, in the sibling classifier: a
// fixed-byte cut can land exactly on a marker that was NOT at a line start, and
// the remainder of that line then looks like a line of its own. The cut would
// have manufactured the anchor AC 3 exists to require.
func TestCodexScanWindowCutCannotManufactureALineStart(t *testing.T) {
	markerLine := "ERROR: You've hit your usage limit.\n"
	// Size the trailing filler so the cut lands exactly on the marker's first
	// byte, whatever comes before it.
	pad := ClassifyScanBytes - len(markerLine)
	rest := strings.Repeat("x\n", pad/2)
	if pad%2 == 1 {
		rest = "x" + rest
	}
	build := func(before string) []byte {
		log := []byte(before + markerLine + rest)
		if len(log)-ClassifyScanBytes != len(before) {
			t.Fatalf("cut at %d, want it exactly on the marker at %d", len(log)-ClassifyScanBytes, len(before))
		}
		return log
	}

	log := build("earlier line\n2026-08-01 the log narrates: ")
	got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the marker is mid-line and only the cut put it at the window's start", got.Class, got.Reason)
	}

	// Narrowing, not deleting: when the cut falls on a line boundary the
	// window's first line is whole, and a genuine line-start marker there must
	// still classify. A fix that always drops the first line would fail here.
	log = build("earlier line\n2026-08-01 the log narrates\n")
	if got = ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log}); got.Class != ClassProviderQuota {
		t.Fatalf("class = %q (%s), want provider_quota: a genuine line-start marker at the window's start must still classify", got.Class, got.Reason)
	}
}

// --- Claude, prompt mode ----------------------------------------------------

func TestClaudeCapturedEnvelopes(t *testing.T) {
	cases := []struct {
		fixture string
		want    OutcomeClass
		why     string
	}{
		{"claude-429-per-minute.json", ClassProviderQuota, "429 covers both a per-minute rate limit and a plan exhaustion, and the envelope does not distinguish them; the backoff ladder is designed so it never has to be distinguished"},
		{"claude-429-usage-limit.json", ClassProviderQuota, "429 usage limit"},
		{"claude-400-credit-balance.json", ClassProviderQuota, "400 plus credit-balance text"},
		{"claude-529-null-status.json", ClassNotLimit, "overload is transient capacity, not quota"},
		{"claude-401-auth.json", ClassNotLimit, "auth stays in the capability-failure branch"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, tc.fixture)})
			if got.Class != tc.want {
				t.Fatalf("class = %q (%s), want %q — %s", got.Class, got.Reason, tc.want, tc.why)
			}
		})
	}
}

// The five captured envelopes all read subtype "success". A classifier keying on
// subtype would be wrong on every one of them.
func TestClaudeSubtypeCarriesNoInformation(t *testing.T) {
	for _, name := range []string{
		"claude-429-per-minute.json",
		"claude-429-usage-limit.json",
		"claude-400-credit-balance.json",
		"claude-529-null-status.json",
		"claude-401-auth.json",
	} {
		var envelope map[string]any
		if err := json.Unmarshal(readFixture(t, name), &envelope); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if envelope["subtype"] != "success" {
			t.Fatalf("%s: subtype = %v; the capture this rule rests on reads \"success\"", name, envelope["subtype"])
		}
	}
	// Same subtype, opposite verdicts: subtype cannot be the discriminator.
	limit := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, "claude-429-usage-limit.json")})
	notLimit := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, "claude-401-auth.json")})
	if limit.Class == notLimit.Class {
		t.Fatal("two envelopes with subtype \"success\" produced the same class; api_error_status is the discriminator")
	}
}

func TestClaudeIsErrorFalseIsNeverALimit(t *testing.T) {
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, "claude-is-error-false.json")})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q; the envelope is authoritative when it parses and it reports is_error false", got.Class)
	}
}

func TestClaudeStatusPrecedence(t *testing.T) {
	envelope := func(status, result string) []byte {
		return []byte(`{"type":"result","is_error":true,"subtype":"success","terminal_reason":"api_error","api_error_status":` + status + `,"result":"` + result + `"}`)
	}
	cases := []struct {
		name   string
		stdout []byte
		want   OutcomeClass
	}{
		{"429", envelope("429", "API Error: Request rejected (429)"), ClassProviderQuota},
		{"400 credit balance", envelope("400", "Credit balance is too low"), ClassProviderQuota},
		{"400 other", envelope("400", "invalid request: bad model"), ClassNotLimit},
		{"401", envelope("401", "Invalid API key · Fix external API key"), ClassNotLimit},
		{"403", envelope("403", "Forbidden"), ClassNotLimit},
		{"500", envelope("500", "Internal server error"), ClassNotLimit},
		{"503", envelope("503", "Service unavailable"), ClassNotLimit},
		{"529", envelope("529", "Overloaded"), ClassNotLimit},
		// The status is authoritative when present: a non-limit status wins even
		// when the text carries a limit phrase.
		{"401 with limit prose", envelope("401", "Invalid API key. You've reached your usage limit."), ClassNotLimit},
		{"529 with limit prose", envelope("529", "Overloaded. You've reached your usage limit."), ClassNotLimit},
		// Absent or null status is the only path to text.
		{"null status with limit prose", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":null,"result":"You've reached your usage limit."}`), ClassProviderQuota},
		{"absent status with limit prose", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","result":"Credit balance is too low"}`), ClassProviderQuota},
		{"null status without limit prose", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":null,"result":"API Error: Repeated 529 Overloaded errors."}`), ClassNotLimit},
		// terminal_reason gates before the status is read.
		{"non api_error terminal reason", []byte(`{"type":"result","is_error":true,"terminal_reason":"user_abort","api_error_status":429,"result":"cancelled"}`), ClassNotLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: tc.stdout})
			if got.Class != tc.want {
				t.Fatalf("class = %q (%s), want %q", got.Class, got.Reason, tc.want)
			}
		})
	}
}

// A present, non-null api_error_status that does not decode to an integer is
// NOT the absent case, and must not open the text fallback.
//
// The distinction the reviewed implementation lost: "can I read this value?" and
// "does this value mean anything?" are different questions. `present=false`
// answered the first one and was consumed as an answer to the second, so a
// malformed authoritative discriminator handed the decision to limit prose —
// precisely the prose oracle §4.2 exists to keep out of the suppression path.
//
// Every case here carries a limit phrase in `result`, so a test that passes only
// because the prose was innocuous cannot exist.
func TestClaudePresentButUnreadableStatusNeverReachesTheTextFallback(t *testing.T) {
	const prose = "API Error: Request rejected. You've reached your usage limit."
	envelope := func(status string) []byte {
		return []byte(`{"type":"result","is_error":true,"subtype":"success","terminal_reason":"api_error","api_error_status":` + status + `,"result":"` + prose + `"}`)
	}
	cases := []struct {
		name   string
		stdout []byte
	}{
		{"string", envelope(`"not-an-integer"`)},
		{"numeric string", envelope(`"429"`)},
		{"float", envelope(`429.5`)},
		{"boolean", envelope(`true`)},
		{"object", envelope(`{"code":429}`)},
		{"array", envelope(`[429]`)},
		{"empty string", envelope(`""`)},
		{"captured fixture", readFixture(t, "claude-malformed-status.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: tc.stdout})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q (%s), want not-a-limit: a present but unreadable status is evidence that the envelope is wrong, not evidence that the field was omitted", got.Class, got.Reason)
			}
			if !strings.Contains(got.Reason, "present and not an integer") {
				t.Fatalf("reason = %q, want it to name the unreadable status", got.Reason)
			}
		})
	}
}

// The narrowing half: the three forms that DO reach a verdict still reach it, so
// the gate above cannot be satisfied by refusing everything.
func TestClaudeStatusFormsAreDistinguished(t *testing.T) {
	const prose = "You've reached your usage limit."
	cases := []struct {
		name       string
		stdout     []byte
		want       OutcomeClass
		wantReason string
	}{
		{"integer decides", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"` + prose + `"}`), ClassProviderQuota, "api_error_status 429"},
		{"null falls through to text", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":null,"result":"` + prose + `"}`), ClassProviderQuota, "text fallback on null api_error_status"},
		{"absent falls through to text", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","result":"` + prose + `"}`), ClassProviderQuota, "text fallback on absent api_error_status"},
		{"unreadable stops", []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":"429","result":"` + prose + `"}`), ClassNotLimit, `api_error_status is present and not an integer ("429")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: tc.stdout})
			if got.Class != tc.want {
				t.Fatalf("class = %q (%s), want %q", got.Class, got.Reason, tc.want)
			}
			if !strings.Contains(got.Reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q: the four status forms must be distinguishable in the record", got.Reason, tc.wantReason)
			}
		})
	}
}

// The whole production chain, not just the classifier: an unreadable status with
// limit prose must not be mintable into an Observation, and therefore cannot
// suppress anything.
func TestUnreadableStatusCannotSuppressAGroup(t *testing.T) {
	f := newStoreFixture(t)
	claudeIdentity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	class := ClassifyClaudePrompt(ClaudePromptResult{
		ExitCode: 1,
		Now:      f.clock.Now(),
		Stdout:   readFixture(t, "claude-malformed-status.json"),
	})
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-0000d1", Model: "claude-opus-5"})
	if ok {
		t.Fatalf("an unreadable status must not mint a quota observation: %+v", obs)
	}
	if err := f.store.Observe(claudeIdentity, "claude-max", &Lease{}, obs); !errors.Is(err, ErrNotProviderQuota) {
		t.Fatalf("Observe = %v, want ErrNotProviderQuota", err)
	}
	if _, exists := f.store.GroupRecordFor(claudeIdentity, "claude-max"); exists {
		t.Fatal("a group was suppressed by a malformed envelope")
	}
}

func TestClaudeReadsTheLastCompleteObject(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":401,"result":"Invalid API key"}
{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassProviderQuota {
		t.Fatalf("class = %q, want the LAST envelope's verdict", got.Class)
	}
}

// --- F6: what counts as "the JSON did not parse at all" ---------------------

// A run killed part-way through the next write leaves a complete authoritative
// envelope followed by the opening of another object. The complete envelope must
// still decide; the partial one carries nothing.
//
// This attacks the gate rather than reading it: the trailing fragment contains
// limit prose and the complete envelope says is_error false, so an
// implementation that loses the complete object suppresses a group on prose the
// provider never asserted as an error.
func TestClaudeTrailingPartialObjectDoesNotDiscardTheLastCompleteResult(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":false,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}
{"type":"result","result":"You've reached your usage limit.`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the last COMPLETE envelope reports is_error false", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "is_error false") {
		t.Fatalf("reason = %q, want the complete envelope's verdict, not a fallback verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// A parsed object of another type is not "the JSON did not parse at all".
// §4.1 step 3 sends it to not-a-limit; relabelling it as unparsed lets prose
// anywhere in the transcript decide whenever a run ends on a non-result line.
func TestClaudeParsedNonResultObjectIsNotTreatedAsUnparsed(t *testing.T) {
	stdout := []byte(`{"type":"assistant","message":{"role":"assistant","content":"You've reached your usage limit."}}`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: a parsed non-result object must not reach the transcript fallback", got.Class, got.Reason)
	}
	if strings.Contains(got.Reason, "unparsed envelope") {
		t.Fatalf("reason = %q, want a parsed-object verdict rather than the fallback verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// A complete result envelope whose fields are unreadable is not absent either —
// the same reasoning as a present-but-unreadable api_error_status.
func TestClaudeUnreadableResultEnvelopeIsNotTreatedAsUnparsed(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":"yes","result":"You've reached your usage limit."}`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit", got.Class, got.Reason)
	}
	if strings.Contains(got.Reason, "unparsed envelope") {
		t.Fatalf("reason = %q, want an unreadable-envelope verdict rather than the fallback verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// The bound is proven by NARROWING it, not by deleting it: retaining the last
// complete object must keep deciding, and the genuinely-unparsed cases must keep
// reaching the fallback. A fix that merely refused to fall back would pass the
// three tests above and fail this one.
func TestClaudeCompleteObjectRetentionIsNarrowedNotDeleted(t *testing.T) {
	t.Run("a complete limit envelope still decides behind a partial object", func(t *testing.T) {
		stdout := []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}
{"type":"result","result":"truncated`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want provider_quota decided by the retained envelope", got.Class, got.Reason)
		}
	})
	// CHANGED BY F11. A window that ends inside a half-written envelope is not a
	// window with no JSON in it: the write was cut off, and the fields it DID
	// reach are the authoritative ones — `api_error_status` precedes `result` in
	// every captured envelope — so the prose of a truncated envelope routinely
	// sits behind a status that contradicts it. See
	// TestClaudeTruncatedEnvelopeDoesNotLetItsOwnProseDecide for the shape this
	// buys, and the two subtests below for the fallback it does not cost.
	t.Run("a tail that ends inside a half-written envelope is not evidence", func(t *testing.T) {
		stdout := []byte(`{"type":"result","result":"You've reached your usage limit.`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the envelope was not read, which is not the same as no envelope", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "ends inside an incomplete top-level JSON value") {
			t.Fatalf("reason = %q, want the truncated-value verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	t.Run("a tail with no JSON in it at all still falls back", func(t *testing.T) {
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte("claude exited before emitting json\nYou've reached your usage limit.\n")})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "unparsed envelope") {
			t.Fatalf("class = %q (%s), want the fallback to survive for a genuinely unparsed tail", got.Class, got.Reason)
		}
	})
	t.Run("a brace-balanced non-JSON span is not a parsed object", func(t *testing.T) {
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte("panic: {not json at all}\nYou've reached your usage limit.\n")})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "unparsed envelope") {
			t.Fatalf("class = %q (%s), want the fallback for a span that is not JSON", got.Class, got.Reason)
		}
	})
	// The scan window starts mid-line, so the first object is headless. It must
	// not swallow the complete envelope that follows it.
	t.Run("a leading partial object does not hide the complete one behind it", func(t *testing.T) {
		stdout := []byte(`"content":"noise"}
{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":401,"result":"Invalid API key"}`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "401") {
			t.Fatalf("class = %q (%s), want the complete 401 envelope to decide", got.Class, got.Reason)
		}
	})
}

// assertClaudeStdoutSuppressesNothing drives stdout through the whole production
// chain — ClassifyClaudePrompt, QuotaObservation, Store.Observe — and fails if
// anything was persisted. The classifier returning not-a-limit is not on its own
// proof that no state can be written.
func assertClaudeStdoutSuppressesNothing(t *testing.T, stdout []byte) {
	t.Helper()
	assertClaudeStdoutSuppressesNothingIn(t, ClaudePromptResult{ExitCode: 1, Stdout: stdout})
}

func assertClaudeStdoutSuppressesNothingIn(t *testing.T, r ClaudePromptResult) {
	t.Helper()
	f := newStoreFixture(t)
	claudeIdentity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	r.Now = f.clock.Now()
	class := ClassifyClaudePrompt(r)
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-0000f6", Model: "claude-opus-5"})
	if ok {
		t.Fatalf("a non-limit classification must not mint a quota observation: %+v", obs)
	}
	if err := f.store.Observe(claudeIdentity, "claude-max", &Lease{}, obs); !errors.Is(err, ErrNotProviderQuota) {
		t.Fatalf("Observe = %v, want ErrNotProviderQuota", err)
	}
	if _, exists := f.store.GroupRecordFor(claudeIdentity, "claude-max"); exists {
		t.Fatal("a group was suppressed without a provider-quota observation")
	}
}

// --- F7: an arbitrary byte cut must not invent or destroy structure ---------

// oversizedClaudeTranscript builds a transcript whose scan-window cut lands at
// byte `cutAt` of the first line.
//
// The first line is `prefix + filler + suffix`, with the filler made of whole
// repetitions of `unit` so the cut can be aimed at a chosen position inside a
// repeating structure (a plain string, an escape sequence, a run of nested
// key/value pairs). `tail` lines follow, and the whole first line is discarded
// by the window.
//
// The arithmetic is forced, not searched: the cut is at
// len(stdout)-ClassifyScanBytes, so fixing the tail fixes the first line's
// length at ClassifyScanBytes + cutAt - 1 - len(rest). Every test below depends
// on the cut landing exactly where it says, so the helper asserts it.
func oversizedClaudeTranscript(t *testing.T, prefix, unit, suffix string, cutAt int, tail ...string) []byte {
	t.Helper()
	rest := ""
	if len(tail) > 0 {
		rest = strings.Join(tail, "\n") + "\n"
	}
	targetFirst := ClassifyScanBytes + cutAt - 1 - len(rest)
	need := targetFirst - len(prefix) - len(suffix)
	if need < len(unit) {
		t.Fatalf("first line has no room for filler: need=%d", need)
	}
	// Any bytes left over by the unit size go in front of the repeats, so the
	// repeating region itself stays byte-aligned with the cut.
	filler := strings.Repeat("a", need%len(unit)) + strings.Repeat(unit, need/len(unit))
	first := prefix + filler + suffix
	if len(first) != targetFirst {
		t.Fatalf("first line is %d bytes, want %d", len(first), targetFirst)
	}
	stdout := []byte(first + "\n" + rest)
	cut := len(stdout) - ClassifyScanBytes
	if cut != cutAt {
		t.Fatalf("cut landed at byte %d of the first line, want %d", cut, cutAt)
	}
	if cut <= len(prefix) || cut >= len(first) {
		t.Fatalf("cut at %d is not inside the filler (prefix %d, first line %d)", cut, len(prefix), len(first))
	}
	if stdout[cut-1] == '\n' {
		t.Fatalf("cut at %d landed on a line boundary; this helper builds mid-line cuts", cut)
	}
	return stdout
}

const claudeAssistantPrefix = `{"type":"assistant","message":{"role":"assistant","content":"`

// The reviewer's cycle-4 reproduction. The 256 KiB cut lands inside a JSON
// string, so a scanner started at the cut reads that string's CLOSING quote as
// an opening one, inverts its state, and never sees the braces of the complete
// authoritative envelope that follows. The envelope says is_error false — not a
// limit, stop — while the prose inside it says the opposite, so an
// implementation that loses the framing suppresses a group on text §4.1 never
// gives a vote.
func TestClaudeMidStringWindowCutDoesNotHideALaterCompleteResult(t *testing.T) {
	stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", `"}}`, 1024,
		`{"type":"result","is_error":false,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}`,
	)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the complete envelope after the cut reports is_error false", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "is_error false") {
		t.Fatalf("reason = %q, want the recovered envelope's verdict rather than a fallback verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// The mirror, and the reason the fix cannot be "refuse to decide after a cut":
// the same mid-string cut must still let a real 429 envelope suppress, and the
// reason must name api_error_status rather than prose.
func TestClaudeMidStringWindowCutStillReadsACompleteLimitEnvelope(t *testing.T) {
	stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", `"}}`, 1024,
		`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`,
	)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want provider_quota decided by the recovered envelope", got.Class, got.Reason)
	}
}

// A cut inside a two-byte escape sequence lands on the backslash for one parity
// of the filler and on the escaped character for the other. Both are scanned,
// because a fix that only handles the parity it was written against is not a
// fix.
func TestClaudeWindowCutInsideAnEscapeSequenceIsRecovered(t *testing.T) {
	// Both offsets of the two-byte unit, so the cut lands on the backslash for
	// one and on the escaped byte for the other. A fix that only handles the
	// parity it was written against is not a fix.
	for _, cutAt := range []int{1024, 1025} {
		t.Run("cut at "+strconv.Itoa(cutAt), func(t *testing.T) {
			stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, `\n`, `"}}`, cutAt,
				`{"type":"result","is_error":false,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}`,
			)
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "is_error false") {
				t.Fatalf("class = %q (%s), want the complete envelope after the cut to decide", got.Class, got.Reason)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
}

// A cut at brace depth greater than zero: the scanner resumes believing it is
// at depth 0 and would read the enclosing object's closing braces as top-level
// ones. The sweep covers one whole period of the repeating unit, so the cut
// lands inside a key string, inside a value string, on a quote, on a colon and
// on a comma across the subtests.
func TestClaudeWindowCutInsideANestedObjectIsRecovered(t *testing.T) {
	const unit = `"k":"v",`
	for offset := 0; offset < len(unit); offset++ {
		cutAt := 1024 + offset
		t.Run("cut at "+strconv.Itoa(cutAt), func(t *testing.T) {
			stdout := oversizedClaudeTranscript(t,
				`{"type":"assistant","message":{"role":"assistant","usage":{`, unit, `"end":"x"}}}`, cutAt,
				`{"type":"result","is_error":false,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}`,
			)
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "is_error false") {
				t.Fatalf("class = %q (%s), want the complete envelope after the cut to decide", got.Class, got.Reason)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
}

// The one framing case that cannot be decided: a truncated window with no line
// boundary in it at all. There is no position in it whose JSON token state is
// known, so it yields neither an envelope nor a vote — the prose in it must not
// suppress, because an authoritative envelope may be sitting in the bytes the
// window dropped.
func TestClaudeUnframableTruncatedWindowIsNotEvidence(t *testing.T) {
	stdout := []byte(`{"type":"assistant","content":"` +
		strings.Repeat("a", ClassifyScanBytes) +
		` You've reached your usage limit.`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: an unframable window is not evidence", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "no line boundary") {
		t.Fatalf("reason = %q, want the unframable-window verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// Recovering framing from raw newlines holds for conforming JSON, where a raw
// control character cannot appear inside a string. A provider that emitted one
// anyway could place text that LOOKS like a top-level envelope inside a string.
// It is refused in the headless case because a real top-level envelope occupies
// whole lines and an interior span does not: its closing brace is followed by
// the rest of the enclosing string on the same line.
func TestClaudeInteriorSpanAfterAnUnescapedNewlineIsNotAnEnvelope(t *testing.T) {
	forged := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"forged"}`
	stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", "\n"+forged+`"}}`, 1024)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: a span inside a string is not a top-level envelope", got.Class, got.Reason)
	}
	if strings.Contains(got.Reason, "429") {
		t.Fatalf("reason = %q, a forged interior span decided the classification", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// --- F8: a nested object is not a top-level envelope ------------------------

// nestedEnvelopeReproduction is the reviewer's cycle-5 shape: ONE RFC 8259-valid
// outer assistant object whose first line carries a string large enough to force
// the 256 KiB cut INSIDE it, followed by a NESTED object occupying whole lines of
// its own and shaped exactly like a 429 result envelope. There is no top-level
// result envelope anywhere in the transcript.
//
// A newline proves the scanner is outside every JSON string. It proves nothing
// about brace depth, and whole-line occupancy does not either: a pretty-printed
// nested object occupies whole lines too.
func nestedEnvelopeReproduction(t *testing.T) []byte {
	t.Helper()
	stdout := oversizedClaudeTranscript(t,
		`{"type":"assistant","message":{"role":"assistant","content":"`, "a", `"},"nested":`, 1024,
		`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429) · You've reached your usage limit."}`,
		`}`,
	)
	// The input is a conforming transcript, not a malformed one: the defect must
	// be attacked on JSON the provider could actually emit.
	if !json.Valid(stdout) {
		t.Fatal("the reproduction must be one valid JSON document, otherwise it proves nothing")
	}
	return stdout
}

// Two gates answer "is this object top-level", and each is attacked on its own.
// Neither may be left to the other: the measured gate does not exist for a
// declared tail, and the inferred gate is not applied where the head was read.
func TestClaudeNestedWholeLineObjectIsNotATopLevelEnvelope(t *testing.T) {
	// This process did the cutting, so the head is in the same buffer and where
	// the window begins is measured rather than argued.
	t.Run("the transcript's own head places the window inside the outer object", func(t *testing.T) {
		stdout := nestedEnvelopeReproduction(t)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: a nested object is not a top-level result envelope", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "provably top-level") || !strings.Contains(got.Reason, "head puts its first byte inside an enclosing container") {
			t.Fatalf("reason = %q, want the measured verdict from the transcript's head", got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// The production shape: the runtime kept a ring buffer, so the head this
	// module would have measured is gone. Nothing in the tail's own bytes can
	// place absolute depth, so the tail is refused before they are weighed.
	t.Run("a declared tail has no head, so nothing in it is top-level", func(t *testing.T) {
		full := nestedEnvelopeReproduction(t)
		tail := full[bytes.IndexByte(full, '\n')-64:]
		if bytes.IndexByte(tail, '\n') < 0 {
			t.Fatal("the tail must retain a line boundary, otherwise the unframable gate decides instead")
		}
		r := ClaudePromptResult{ExitCode: 1, Stdout: tail, StdoutTruncated: true}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit for the same shape without a head", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "provably top-level") || !strings.Contains(got.Reason, "declared tail with no anchor") {
			t.Fatalf("reason = %q, want the unanchored-tail verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})
	// The same tail with a TRUTHFUL anchor is still refused, and by measurement
	// rather than by the blanket rule above: the outer object it names is never
	// closed inside the window, so depth never reaches zero.
	t.Run("an anchored tail whose enclosing object never closes is still nested", func(t *testing.T) {
		full := nestedEnvelopeReproduction(t)
		cut := bytes.IndexByte(full, '\n') + 1
		r := ClaudePromptResult{
			ExitCode:        1,
			Stdout:          full[cut : len(full)-1], // drop the outer object's closing brace
			StdoutTruncated: true,
			StdoutAnchor:    &TailAnchor{Containers: openObjects(1)},
		}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the nested envelope is not top-level", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "head puts its first byte inside an enclosing container") {
			t.Fatalf("reason = %q, want the measured nested verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})
}

// A declared tail that ends inside an unclosed object cannot place top level
// either: the same relative depth is consistent with "the window began one level
// deep" and with "the last write was cut mid-object", and those two disagree
// about whether the objects in between were top-level. Without an anchor there
// is nothing to break the tie.
//
// The window carries limit prose as well, so an implementation that treats
// "cannot prove" as "did not parse" suppresses a group on it.
func TestClaudeDeclaredTailEndingInsideAnOpenObjectIsNotEvidence(t *testing.T) {
	r := ClaudePromptResult{ExitCode: 1, StdoutTruncated: true, Stdout: []byte(
		`ontent":"noise"}}` + "\n" +
			`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":401,"result":"Invalid API key"}` + "\n" +
			`{"type":"result","is_error":true,"result":"You've reached your usage limit.`)}
	got := ClassifyClaudePrompt(r)
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "provably top-level") || !strings.Contains(got.Reason, "declared tail with no anchor") {
		t.Fatalf("reason = %q, want the unproven-top-level verdict for an unanchored tail", got.Reason)
	}
	assertClaudeStdoutSuppressesNothingIn(t, r)
}

// --- F9: a declared tail has no top level of its own ------------------------

// killedOuterObjectReproduction is the reviewer's cycle-6 shape, and it is the
// one that killed the balance argument outright.
//
// A conforming Claude assistant object opens, carries a string large enough to
// push the retained tail past its own start, and encloses a NESTED whole-line
// object shaped exactly like a 429 result envelope. The child is then KILLED
// before the outer closing brace is written.
//
// What the retained tail therefore contains is a window that begins at absolute
// depth one, holds a balanced nested object, and ends at relative depth zero
// having closed nothing it did not open. Every byte of it is consistent with a
// genuine top-level envelope, and there is no top-level result envelope in the
// transcript at all. Appending the single missing brace makes the whole thing
// valid JSON, which is what proves the observed bytes are a real provider-JSON
// prefix rather than invented noise.
// The anchor it returns is the one a runtime that framed the stream while
// writing it would have measured, computed the same way rather than counted by
// hand — an untruthful anchor would prove nothing about the gate.
func killedOuterObjectReproduction(t *testing.T) (tail []byte, anchor *TailAnchor) {
	t.Helper()
	envelope := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,` +
		`"result":"API Error: Request rejected (429) · You've reached your usage limit."}`
	killed := []byte(`{"type":"assistant","message":{"role":"assistant","content":"` +
		strings.Repeat("a", 4096) + `"},` + "\n" + `"nested":` + "\n" + envelope + "\n")
	completed := append(append([]byte(nil), killed...), '}')
	if !json.Valid(completed) {
		t.Fatal("the killed transcript must be a prefix of valid provider JSON, otherwise it proves nothing")
	}
	cut := len(killed) - 1024
	tail = killed[cut:]
	if bytes.IndexByte(tail, '\n') < 0 {
		t.Fatal("the tail must retain a line boundary, otherwise the unframable gate decides instead")
	}
	at := claudeScanHeadFrom(killed[:cut], tokenState{})
	if at.containers.empty() || !at.inString {
		t.Fatalf("the tail must begin nested and inside a string (containers=%q inString=%v)", at.containers, at.inString)
	}
	return tail, anchorFrom(at)
}

// anchorFrom is the TailAnchor a caller that measured the dropped bytes would
// supply for the state at the first retained byte. Tests convert through it
// rather than writing the container stack by hand, so an anchor in a test is
// always the measurement and never the test's opinion of it.
func anchorFrom(at tokenState) *TailAnchor {
	containers := make([]ContainerKind, 0, len(at.containers))
	for i := 0; i < len(at.containers); i++ {
		containers = append(containers, ContainerKind(at.containers[i]))
	}
	return &TailAnchor{Containers: containers, InString: at.inString, Escaped: at.escaped}
}

// openObjects is the container stack of n nested OBJECTS, for the anchors a test
// states directly.
func openObjects(n int) []ContainerKind {
	stack := make([]ContainerKind, 0, n)
	for i := 0; i < n; i++ {
		stack = append(stack, ContainerObject)
	}
	return stack
}

// The reviewer's reproduction, driven the whole way through the production path:
// ClassifyClaudePrompt -> QuotaObservation -> Store.Observe. No group record may
// be written.
//
// This is the case no case analysis over the window's bytes can reach. The tail
// closes nothing it did not open and ends at relative depth zero, so the two
// inferred rules that used to guard this scan both pass it — the shape is not a
// missing branch, it is the false premise those branches rested on.
func TestClaudeDeclaredTailInsideAKilledOuterObjectIsNotEvidence(t *testing.T) {
	tail, anchor := killedOuterObjectReproduction(t)
	r := ClaudePromptResult{ExitCode: 1, Stdout: tail, StdoutTruncated: true}
	got := ClassifyClaudePrompt(r)
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the 429 envelope is nested inside an outer object opened before the tail", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "provably top-level") {
		t.Fatalf("reason = %q, want the unproven-top-level verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothingIn(t, r)

	// The same shape with a truthful anchor is refused by MEASUREMENT: the outer
	// object never closes inside the window, so absolute depth never reaches
	// zero and the nested envelope is not a top-level one.
	anchored := ClaudePromptResult{ExitCode: 1, Stdout: tail, StdoutTruncated: true, StdoutAnchor: anchor}
	if got = ClassifyClaudePrompt(anchored); got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit for the anchored tail too", got.Class, got.Reason)
	}
	assertClaudeStdoutSuppressesNothingIn(t, anchored)
}

// The refusal above is proven by NARROWING it. An implementation that refused
// every tail, or that refused every window beginning inside an enclosing object,
// would pass every F9 test and fail this one.
func TestClaudeAnchoredTailTopLevelProofIsNarrowedNotDeleted(t *testing.T) {
	envelope := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,` +
		`"result":"API Error: Request rejected (429) · You've reached your usage limit."}`
	// The mirror of the reproduction: the outer object opened before the tail is
	// CLOSED inside it, so absolute depth returns to zero and the envelope that
	// follows really is top-level. It must still suppress, and the suppression
	// must be driven into the store — a classifier verdict alone is not proof
	// that the production path still works.
	t.Run("a top-level envelope after the enclosing object closes still suppresses", func(t *testing.T) {
		tail := []byte(`aaaa"},"nested":{"k":"v"}}` + "\n" + envelope + "\n")
		f := newStoreFixture(t)
		identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
		if err != nil {
			t.Fatalf("IdentityFor: %v", err)
		}
		got := ClassifyClaudePrompt(ClaudePromptResult{
			ExitCode:        1,
			Stdout:          tail,
			StdoutTruncated: true,
			StdoutAnchor:    &TailAnchor{Containers: openObjects(2), InString: true},
			Now:             f.clock.Now(),
		})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want provider_quota from the genuinely top-level envelope", got.Class, got.Reason)
		}
		obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0000f9", Model: "claude-opus-5"})
		if !ok {
			t.Fatal("a provider_quota classification must mint an observation")
		}
		if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		record, exists := f.store.GroupRecordFor(identity, "claude-max")
		if !exists || record.State != StateSuppressed {
			t.Fatalf("record = %+v (exists=%v), want a suppressed group", record, exists)
		}
	})
	// The same mirror on the reproduction's OWN shape: the outer object that made
	// the nested 429 unusable is closed, and a genuine top-level 429 follows it in
	// the same retained tail. The refusal must be about where the envelope sits,
	// not about the transcript looking complicated.
	t.Run("the reproduction's shape with the outer object closed still suppresses", func(t *testing.T) {
		nested := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"nested, not top-level"}`
		full := []byte(`{"type":"assistant","message":{"role":"assistant","content":"` +
			strings.Repeat("a", 4096) + `"},` + "\n" + `"nested":` + "\n" + nested + "\n}" + "\n" + envelope + "\n")
		if !json.Valid(full[:len(full)-len(envelope)-2]) {
			t.Fatal("the outer object must be complete for this mirror to mean anything")
		}
		cut := len(full) - 1024
		at := claudeScanHeadFrom(full[:cut], tokenState{})
		if at.containers.empty() || !at.inString {
			t.Fatalf("the tail must still begin nested and inside a string (containers=%q inString=%v)", at.containers, at.inString)
		}
		f := newStoreFixture(t)
		identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
		if err != nil {
			t.Fatalf("IdentityFor: %v", err)
		}
		got := ClassifyClaudePrompt(ClaudePromptResult{
			ExitCode:        1,
			Stdout:          full[cut:],
			StdoutTruncated: true,
			StdoutAnchor:    anchorFrom(at),
			Now:             f.clock.Now(),
		})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the top-level envelope after the outer close to decide", got.Class, got.Reason)
		}
		obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0001f9", Model: "claude-opus-5"})
		if !ok {
			t.Fatal("a provider_quota classification must mint an observation")
		}
		if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if record, exists := f.store.GroupRecordFor(identity, "claude-max"); !exists || record.State != StateSuppressed {
			t.Fatalf("record = %+v (exists=%v), want a suppressed group", record, exists)
		}
	})
	// An anchor at depth zero is the ordinary ring-buffer case, and it keeps the
	// F6 retention: a complete envelope still decides behind a write the kill cut
	// in half.
	t.Run("a tail anchored at top level keeps the retention", func(t *testing.T) {
		tail := []byte(envelope + "\n" + `{"type":"result","is_error":true,"result":"killed mid-write`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: tail, StdoutTruncated: true, StdoutAnchor: &TailAnchor{}})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the retained envelope to decide in an anchored tail", got.Class, got.Reason)
		}
	})
	// A broken anchor is not a weaker anchor. A container that is neither an
	// object nor an array cannot have been measured, so the anchor is discarded
	// whole and the tail is refused as unanchored rather than scanned from a stack
	// nobody can defend.
	t.Run("a container kind that is not a container discards the whole anchor", func(t *testing.T) {
		broken := &TailAnchor{Containers: []ContainerKind{ContainerObject, ContainerKind('(')}}
		r := ClaudePromptResult{ExitCode: 1, Stdout: []byte(envelope + "\n"), StdoutTruncated: true, StdoutAnchor: broken}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "declared tail with no anchor") {
			t.Fatalf("class = %q (%s), want the unanchored verdict for a broken anchor", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})
	// An anchor on an INTACT buffer decides nothing: byte 0 of a whole transcript
	// is absolute depth zero by construction, and a caller's claim to the
	// contrary must not be able to hide an envelope.
	t.Run("an anchor cannot override an intact buffer", func(t *testing.T) {
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte(envelope + "\n"), StdoutAnchor: &TailAnchor{Containers: openObjects(3), InString: true}})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the envelope in an intact buffer to decide regardless of the anchor", got.Class, got.Reason)
		}
	})
}

// A span is complete only when it CLOSES at absolute depth zero, not merely when
// some brace closes while a top-level start is on record.
//
// The outer object of any nested pair always closes after its children, so a
// scan that completed a span at any depth still ends on the right answer for a
// finished object — the mistake only shows when the KILLED write is the one
// carrying the nested child. Then the last "complete" span runs from the killed
// object's start to its child's close, which is not valid JSON, and the
// authoritative envelope in front of it is discarded in favour of the prose
// fallback (F6 again, by a different route).
func TestClaudeNestedCloseInsideAKilledWriteDoesNotEndATopLevelSpan(t *testing.T) {
	stdout := []byte(`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant"},"content":"You've reached your usage limit.`)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the retained top-level envelope to decide", got.Class, got.Reason)
	}
	if strings.Contains(got.Reason, "unparsed envelope") {
		t.Fatalf("reason = %q, a nested close inside the killed write ended a top-level span", got.Reason)
	}
}

// A raw newline INSIDE a JSON string is forbidden by RFC 8259, so finding one
// says something about the EMITTER, not just about this position: its escaping
// cannot be trusted, and an "envelope" after the string closes may be string
// content that a non-conforming write let out.
//
// The gate has to be attacked where refusing and scanning-from-the-string
// DISAGREE. That needs the enclosing string to close inside the window, with a
// well-formed 429 envelope after it — otherwise a scan from inString=true finds
// no object anyway and the two behaviours are indistinguishable.
func TestClaudeRawNewlineInsideAStringRefusesEvenAWellFormedEnvelopeAfterIt(t *testing.T) {
	envelope := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`
	stdout := oversizedClaudeTranscript(t,
		`{"type":"assistant","content":"done"}`+"\n"+`crash dump: "`, "a", "", 1024,
		`still inside the string"`, envelope)
	// The window begins inside that string and its closing quote is IN the
	// window, so a scanner that ignored the non-conformance would reach the
	// envelope and suppress on it.
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the emitter put a raw newline inside a string", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "head puts its first byte inside a JSON string") {
		t.Fatalf("reason = %q, want the non-conformance verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)

	// Narrowing: the same shape with the string closed in the HEAD is a
	// conforming transcript, and the identical envelope must decide.
	stdout = oversizedClaudeTranscript(t,
		`{"type":"assistant","content":"done"}`+"\n"+`crash dump: "`, "a", `"`, 1024,
		`still outside the string`, envelope)
	if got = ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout}); got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the envelope to decide once the head closes the string", got.Class, got.Reason)
	}
}

// The measured window scan must count ABSOLUTE depth rather than refusing every
// window that began nested. This is the same narrowing as above on the path
// where this module cut the head itself.
func TestClaudeMeasuredWindowResumesAtTopLevelOnceTheEnclosingObjectCloses(t *testing.T) {
	envelope := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`
	// The cut lands inside the outer object's content string; the outer object
	// then closes on the window's first line, so the envelope after it is
	// top-level and must decide.
	stdout := oversizedClaudeTranscript(t,
		`{"type":"assistant","message":{"role":"assistant","content":"`, "a", `"}}`, 1024, envelope)
	if !json.Valid([]byte(strings.SplitN(string(stdout), "\n", 2)[0])) {
		t.Fatal("the first line must be one complete outer object for this to mean anything")
	}
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the envelope after the enclosing object closes to decide", got.Class, got.Reason)
	}
}

// Whole-line occupancy is the inferred defence against a non-conforming emitter
// putting a raw control character inside a string: text INSIDE that string then
// looks like an object, but its closing brace is followed by the rest of the
// string on the same line. Where the head was read this cannot happen — the
// string state at the window's first byte is measured — so the rule has to be
// attacked on the declared tail, which is the only place it still carries
// weight.
func TestClaudeDeclaredTailInteriorSpanIsNotAnEnvelope(t *testing.T) {
	forged := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"forged"}`
	r := ClaudePromptResult{ExitCode: 1, StdoutTruncated: true, Stdout: []byte(
		`ontent":"still inside the string` + "\n" + forged + `"}}` + "\n")}
	got := ClassifyClaudePrompt(r)
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: a span inside a string is not a top-level envelope", got.Class, got.Reason)
	}
	if strings.Contains(got.Reason, "429") {
		t.Fatalf("reason = %q, a forged interior span decided the classification", got.Reason)
	}
	assertClaudeStdoutSuppressesNothingIn(t, r)
}

// The measured gate has two arms and they answer different questions, so the
// depth arm must not be left to cover for the string arm.
//
// Here the head is at brace depth ZERO — the assistant object closed cleanly —
// and an unterminated quote follows it, so the window begins inside a string
// with nothing nested around it. A raw newline cannot occur inside a conforming
// JSON string, so this transcript is not conforming: either a string write was
// killed part-way, or the quote is noise in plain text. Those two readings
// disagree about whether what follows is structure at all, and nothing in the
// buffer settles it — so it is refused, and the forged envelope after the
// newline decides nothing.
func TestClaudeHeadPlacingTheWindowInsideAStringIsNotEvidence(t *testing.T) {
	forged := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}`
	stdout := oversizedClaudeTranscript(t,
		`{"type":"assistant","content":"done"}`+"\n"+`crash dump: "`, "a", "", 1024,
		forged,
	)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the window begins inside a string", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "head puts its first byte inside a JSON string") {
		t.Fatalf("reason = %q, want the measured string-state verdict; the depth arm must not be covering for it", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)

	// Narrowing: the same shape with the quote closed in the head puts the window
	// at depth zero and outside every string, and the envelope must decide.
	stdout = oversizedClaudeTranscript(t,
		`{"type":"assistant","content":"done"}`+"\n"+`crash dump: "`, "a", `"`, 1024,
		forged,
	)
	if got = ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout}); got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the envelope to decide once the head closes the string", got.Class, got.Reason)
	}
}

// The measured gate reads the head only to place the window. A closing brace at
// depth zero in that head is crash noise, not an enclosing object, and counting
// it would put the window at a negative depth and refuse a genuine envelope.
func TestClaudeHeadNoiseDoesNotRefuseAGenuineEnvelope(t *testing.T) {
	stdout := oversizedClaudeTranscript(t, `panic: unexpected }} in output: "`, "a", `"`, 1024,
		`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`,
	)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the envelope to decide behind crash noise in the head", got.Class, got.Reason)
	}
}

// The top-level proof is proven by NARROWING it. An implementation that simply
// refused every headless window, or that dropped the F6 retention, would pass
// both tests above and fail this one.
func TestClaudeTopLevelProofIsNarrowedNotDeleted(t *testing.T) {
	// The mirror of the reproduction: the same mid-string cut, the same
	// whole-line envelope after it, but the outer object is CLOSED on the first
	// line, so the envelope really is top-level. It must still suppress, and the
	// suppression must be driven all the way into the store — a classifier
	// verdict alone is not proof that the production path still works.
	t.Run("a genuine top-level 429 after the same cut still suppresses", func(t *testing.T) {
		stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", `"}}`, 1024,
			`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429) · You've reached your usage limit."}`,
		)
		if !json.Valid([]byte(strings.SplitN(string(stdout), "\n", 2)[0])) {
			t.Fatal("the first line must be a complete top-level object for this mirror to mean anything")
		}
		f := newStoreFixture(t)
		identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
		if err != nil {
			t.Fatalf("IdentityFor: %v", err)
		}
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout, Now: f.clock.Now()})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want provider_quota decided by the recovered top-level envelope", got.Class, got.Reason)
		}
		obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0000f8", Model: "claude-opus-5"})
		if !ok {
			t.Fatal("a provider_quota classification must mint an observation")
		}
		if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		record, exists := f.store.GroupRecordFor(identity, "claude-max")
		if !exists || record.State != StateSuppressed {
			t.Fatalf("record = %+v (exists=%v), want a suppressed group", record, exists)
		}
	})
	// An intact buffer is at absolute depth zero by construction, so the proof
	// does not apply to it: a stray closing brace is crash noise, not evidence of
	// nesting, and an object left open by a killed write must not discard the
	// complete envelope in front of it (F6).
	t.Run("an intact buffer is never subject to the depth proof", func(t *testing.T) {
		stdout := []byte(`"content":"noise"}
{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}
{"type":"result","result":"truncated`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the complete envelope in an intact buffer to decide", got.Class, got.Reason)
		}
	})
	// The inferred rules are a stand-in for a head that was never seen. Applying
	// them where the head WAS read costs accuracy for nothing: the balance
	// argument cannot tell a killed mid-object write from an enclosing object, so
	// it would discard the F6 retention a measured depth makes safe.
	t.Run("a measured window keeps the F6 retention the balance argument cannot", func(t *testing.T) {
		stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", `"}}`, 1024,
			`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`,
			`{"type":"result","is_error":true,"result":"killed mid-write`,
		)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the complete envelope to decide when the window's depth is measured", got.Class, got.Reason)
		}
	})
	// A headless window that closes nothing it did not open and ends closed is
	// proven, so the ordinary headless path keeps working — including the
	// fallback for a window that genuinely holds no JSON.
	t.Run("a proven headless window with no object still falls back", func(t *testing.T) {
		stdout := []byte(strings.Repeat("noise\n", ClassifyScanBytes/6) +
			"crashed before emitting json\nYou've reached your usage limit.\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "unparsed envelope") {
			t.Fatalf("class = %q (%s), want the fallback to survive a proven headless window", got.Class, got.Reason)
		}
	})
}

// The production caller keeps a ring buffer, so what reaches the classifier is
// already a tail — cut by someone else, below this module's own window, and
// therefore invisible to it. A tail is not detectable from its own bytes, so it
// is declared. What a declared tail cannot do is establish its own top level;
// only an anchor from the caller that framed the stream can (F9).
func TestDeclaredTailIsTreatedAsHeadless(t *testing.T) {
	t.Run("claude: a declared tail beginning mid-string has no top level of its own", func(t *testing.T) {
		result := `{"type":"result","is_error":false,"terminal_reason":"api_error","api_error_status":429,"result":"You've reached your usage limit."}`
		// Well under ClassifyScanBytes, so only the declaration makes it headless.
		stdout := []byte(`ontent":"` + strings.Repeat("a", 4096) + `"}}` + "\n" + result + "\n")
		r := ClaudePromptResult{ExitCode: 1, Stdout: stdout, StdoutTruncated: true}
		if got := ClassifyClaudePrompt(r); got.Class != ClassNotLimit || !strings.Contains(got.Reason, "declared tail with no anchor") {
			t.Fatalf("class = %q (%s), want the unanchored-tail verdict", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
		// The caller that owns the ring buffer knows where it began. Told that,
		// the same bytes yield the same envelope this module used to guess at —
		// and the cost of the anchor is only the string it starts inside.
		r.StdoutAnchor = &TailAnchor{Containers: openObjects(2), InString: true}
		if got := ClassifyClaudePrompt(r); got.Class != ClassNotLimit || !strings.Contains(got.Reason, "is_error false") {
			t.Fatalf("class = %q (%s), want the complete envelope in an ANCHORED declared tail to decide", got.Class, got.Reason)
		}
	})
	t.Run("claude: a declared tail with no line boundary is not evidence", func(t *testing.T) {
		stdout := []byte(`ontent":"` + strings.Repeat("a", 512) + ` You've reached your usage limit.`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout, StdoutTruncated: true})
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "no line boundary") {
			t.Fatalf("class = %q (%s), want the unframable verdict for a declared tail", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, ClaudePromptResult{ExitCode: 1, Stdout: stdout, StdoutTruncated: true})
	})
	t.Run("codex: a declared tail cannot manufacture a line start", func(t *testing.T) {
		log := []byte("the log narrates: ERROR: You've hit your usage limit.\nlater output\n")
		if got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log}); got.Class != ClassNotLimit {
			t.Fatalf("class = %q, the mid-line marker must not classify even without the declaration", got.Class)
		}
		// The same bytes with the mid-line prefix already gone: without the
		// declaration this is a genuine line start, with it it is a fragment.
		log = []byte("ERROR: You've hit your usage limit.\nlater output\n")
		if got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log}); got.Class != ClassProviderQuota {
			t.Fatalf("class = %q, an undeclared buffer starts at a real line start", got.Class)
		}
		if got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log, LogTruncated: true}); got.Class != ClassNotLimit {
			t.Fatalf("class = %q, a declared tail's first line is a fragment and cannot anchor the marker", got.Class)
		}
	})
	t.Run("codex: a declared tail still finds a marker on a whole line", func(t *testing.T) {
		log := []byte("fragment of an earlier line\nERROR: You've hit your usage limit.\n")
		if got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: log, LogTruncated: true}); got.Class != ClassProviderQuota {
			t.Fatalf("class = %q, declaring a tail must cost the fragment and nothing else", got.Class)
		}
	})
}

// The window bound is proven by NARROWING it. A fix that simply refused to
// classify anything truncated, or dropped the transcript fallback, would pass
// every test above and fail this one.
func TestClaudeWindowFramingIsNarrowedNotDeleted(t *testing.T) {
	t.Run("a framable truncated window with no complete object still falls back", func(t *testing.T) {
		stdout := []byte(strings.Repeat("noise\n", ClassifyScanBytes/6) +
			"crashed before emitting json\nYou've reached your usage limit.\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "unparsed envelope") {
			t.Fatalf("class = %q (%s), want the fallback to survive truncation", got.Class, got.Reason)
		}
	})
	t.Run("an intact buffer is never treated as headless", func(t *testing.T) {
		// Whole-line occupancy is a recovery rule for a headless window only.
		// Applying it to an intact buffer would silently drop envelopes a real
		// emitter can produce.
		stdout := []byte(`  {"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"x"}  trailing`)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the envelope in an intact buffer to decide", got.Class, got.Reason)
		}
	})
	t.Run("a cut on a line boundary gives up no bytes", func(t *testing.T) {
		// Recovery costs the partial first line. When the cut falls exactly on a
		// line boundary there is no partial line, so an envelope sitting at the
		// window's first byte must still decide: a fix that unconditionally
		// skips to the next newline would throw it away.
		envelope := `{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}` + "\n"
		pad := ClassifyScanBytes - len(envelope)
		filler := strings.Repeat("x\n", pad/2)
		if pad%2 == 1 {
			filler = "x" + filler
		}
		stdout := []byte("earlier assistant output\n" + envelope + filler)
		if cut := len(stdout) - ClassifyScanBytes; stdout[cut-1] != '\n' {
			t.Fatalf("cut at %d is not on a line boundary", cut)
		}
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the envelope at the window's first byte to decide", got.Class, got.Reason)
		}
	})
	t.Run("the last complete envelope after the cut still wins over an earlier one", func(t *testing.T) {
		stdout := oversizedClaudeTranscript(t, claudeAssistantPrefix, "a", `"}}`, 1024,
			`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"API Error: Request rejected (429)"}`,
			`{"type":"result","is_error":true,"terminal_reason":"api_error","api_error_status":401,"result":"Invalid API key"}`,
		)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "401") {
			t.Fatalf("class = %q (%s), want the LAST recovered envelope to decide", got.Class, got.Reason)
		}
	})
}

func TestClaudeTextFallbackWhenTheEnvelopeDoesNotParse(t *testing.T) {
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte("crashed before emitting json\nYou've reached your usage limit.\n")})
	if got.Class != ClassProviderQuota {
		t.Fatalf("class = %q (%s), want provider_quota from the text fallback", got.Class, got.Reason)
	}
	got = ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: []byte("crashed before emitting json\nsegmentation fault\n")})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q, want not-a-limit", got.Class)
	}
}

// --- the two disjoint classes ----------------------------------------------

func TestManagedUsageLimitedIsProviderQuota(t *testing.T) {
	var fixture struct {
		ThreadGoal struct {
			Status string `json:"status"`
		} `json:"threadGoal"`
	}
	if err := json.Unmarshal(readFixture(t, "codex-managed-usage-limit.json"), &fixture); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	signal, ok := ManagedSignalForCodexGoalStatus(fixture.ThreadGoal.Status)
	if !ok || signal != ManagedSignalUsageLimited {
		t.Fatalf("status %q mapped to (%q, %v)", fixture.ThreadGoal.Status, signal, ok)
	}
	got := ClassifyManaged(ProviderCodex, signal)
	if got.Class != ClassProviderQuota {
		t.Fatalf("class = %q, want provider_quota", got.Class)
	}
	if _, ok := got.QuotaObservation(Evidence{RunID: "RUN-260801-c95402"}); !ok {
		t.Fatal("a provider_quota classification must yield an Observation")
	}
}

// A managed budgetLimit reports exhaustion of the tokenBudget this runtime sent
// on a board goal. It proves nothing about the shared subscription, and the two
// classes are separate return values rather than a boolean plus a flag precisely
// so that it has no path to a group record.
func TestManagedBudgetLimitedIsRunBudgetAndCannotReachObserve(t *testing.T) {
	var fixture struct {
		ThreadGoal struct {
			Status      string `json:"status"`
			TokenBudget int    `json:"tokenBudget"`
		} `json:"threadGoal"`
	}
	if err := json.Unmarshal(readFixture(t, "codex-managed-budget-limit.json"), &fixture); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if fixture.ThreadGoal.TokenBudget == 0 {
		t.Fatal("the fixture must carry the runtime-imposed tokenBudget that makes this a run budget")
	}
	signal, ok := ManagedSignalForCodexGoalStatus(fixture.ThreadGoal.Status)
	if !ok || signal != ManagedSignalBudgetLimited {
		t.Fatalf("status %q mapped to (%q, %v)", fixture.ThreadGoal.Status, signal, ok)
	}
	got := ClassifyManaged(ProviderCodex, signal)
	if got.Class != ClassRunBudget {
		t.Fatalf("class = %q, want run_budget", got.Class)
	}
	if got.IsProviderQuota() {
		t.Fatal("run_budget must not read as provider_quota")
	}
	obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260802-000001"})
	if ok {
		t.Fatal("a run_budget classification must not yield an Observation")
	}
	if obs.IsProviderQuota() {
		t.Fatal("the zero Observation must not carry the provider-quota gate")
	}
}

func TestManagedUnknownSignalCarriesNothing(t *testing.T) {
	if _, ok := ManagedSignalForCodexGoalStatus("completed"); ok {
		t.Fatal("a completed goal must not map to a limit signal")
	}
	if got := ClassifyManaged(ProviderCodex, ManagedSignal("something_else")).Class; got != ClassNotLimit {
		t.Fatalf("class = %q, want not-a-limit", got)
	}
}

// --- reset hints ------------------------------------------------------------

func TestResetHintCaptureIsDiagnostic(t *testing.T) {
	now := time.Date(2026, 8, 1, 1, 53, 40, 0, time.UTC)
	got := ClassifyCodexPrompt(CodexPromptResult{
		ExitCode: 1,
		Now:      now,
		Log:      readFixture(t, "codex-usage-limit-RUN-260801-a714a6.log"),
	})
	if got.ResetHint == nil || got.ResetHint.Parsed == nil {
		t.Fatalf("the observed Aug-5 hint must parse for diagnosis; hint = %+v", got.ResetHint)
	}
	if got.ResetHint.Used {
		t.Error("the classifier must never mark a hint used; only the store's min() can")
	}
	if got.ResetHint.Note != ResetHintNote {
		t.Errorf("note = %q, want the diagnostic-only note", got.ResetHint.Note)
	}
	if !got.ResetHint.Parsed.After(now.Add(48 * time.Hour)) {
		t.Errorf("parsed hint %s is not the observed Aug-5 instant", got.ResetHint.Parsed)
	}
}

func TestUnparseableOrAbsentResetHintDoesNotAffectClassification(t *testing.T) {
	cases := []string{
		"ERROR: You've hit your usage limit.\n",
		"ERROR: You've hit your usage limit. or try again at the next blue moon.\n",
		"ERROR: You've hit your usage limit. or try again at 99th Blargember, 20xx 88:99 XM.\n",
	}
	for _, log := range cases {
		got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte(log)})
		if got.Class != ClassProviderQuota {
			t.Fatalf("class = %q for %q; detection must never depend on parsing a human date", got.Class, log)
		}
		if got.ResetHint != nil && got.ResetHint.Parsed != nil {
			t.Errorf("hint %q parsed to %s; it is not a date", got.ResetHint.Raw, got.ResetHint.Parsed)
		}
	}
}

func TestRelativeResetHintParses(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	got := ClassifyCodexPrompt(CodexPromptResult{
		ExitCode: 1,
		Now:      now,
		Log:      []byte("ERROR: You've hit your usage limit. or try again in 90 seconds.\n"),
	})
	if got.ResetHint == nil || got.ResetHint.Parsed == nil {
		t.Fatalf("hint = %+v", got.ResetHint)
	}
	if want := now.Add(90 * time.Second); !got.ResetHint.Parsed.Equal(want) {
		t.Errorf("parsed = %s, want %s", got.ResetHint.Parsed, want)
	}
}

func TestApostropheNormalisation(t *testing.T) {
	got := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You’ve hit your usage limit.\n")})
	if got.Class != ClassProviderQuota {
		t.Fatalf("class = %q; a typographic apostrophe must fold onto the ASCII one", got.Class)
	}
}
