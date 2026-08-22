package providerlimits

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- F11: the transcript is a MIXED stream, not a JSON document --------------
//
// The window was scanned as if every byte of it were JSON, with each `"`
// toggling string state. Prose is not JSON. One unmatched quote in a crash line
// put that scan inside a phantom string and it never came out, so the complete
// authoritative `401` envelope on the NEXT LINE was invisible and the limit prose
// INSIDE that same envelope drove a suppression the envelope contradicts.
//
// The fix is structural rather than another lexer bit: values are parsed by
// encoding/json at positions of known absolute depth, prose is stepped over
// rather than lexed, and the transcript fallback is licensed only by a scan that
// read the whole window. "We found no envelope" and "we failed to read one" are
// now different answers, which is the distinction every finding in this family
// has turned on.

// f11Envelope401 is the reviewer's authoritative envelope: a complete result
// envelope whose status says auth failure and whose prose says usage limit. Any
// implementation that loses the envelope and keeps the prose suppresses on it.
const f11Envelope401 = `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":401,"result":"Invalid API key. You've reached your usage limit."}`

// f11Envelope429 is the same shape with a status that IS a limit. It exists so
// every refusal below can be narrowed: a gate that stopped classifying
// altogether would pass the negatives and fail the mirrors.
const f11Envelope429 = `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":429,"result":"API Error: Request rejected (429)"}`

// assertClaudeStdoutSuppresses is the positive counterpart of
// assertClaudeStdoutSuppressesNothing: it drives the whole production chain and
// fails unless a group record was actually written. A classifier verdict on its
// own is not proof that suppression still reaches the store.
func assertClaudeStdoutSuppresses(t *testing.T, stdout []byte, wantReason string) {
	t.Helper()
	f := newStoreFixture(t)
	identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout, Now: f.clock.Now()})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, wantReason) {
		t.Fatalf("class = %q (%s), want provider_quota with reason containing %q", got.Class, got.Reason, wantReason)
	}
	obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0000f11", Model: "claude-opus-5"})
	if !ok {
		t.Fatal("a provider_quota classification must mint an observation")
	}
	if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if record, exists := f.store.GroupRecordFor(identity, "claude-max"); !exists || record.State != StateSuppressed {
		t.Fatalf("record = %+v (exists=%v), want a suppressed group", record, exists)
	}
}

// The reviewer's cycle-8 reproduction, byte for byte, driven the whole way
// through the production path. The envelope is not merely "not lost" — it
// DECIDES, which is the difference between containing the prose and refusing the
// window.
func TestClaudeProseWithAnUnmatchedQuoteCannotHideTheEnvelopeAfterIt(t *testing.T) {
	stdout := []byte("crash prose with an unmatched quote: \"\n" + f11Envelope401)
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the complete 401 envelope after the prose is authoritative", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "api_error_status 401") {
		t.Fatalf("reason = %q, want the envelope's own verdict rather than a fallback or a refusal", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// Prose corrupts at most its own line. Each of these lines poisoned a
// brace-and-quote scan in a different way — an unmatched quote, a trailing
// backslash, an unbalanced brace, a bracket, an escaped quote with nothing to
// escape — and each is followed by the same authoritative envelope.
//
// Both directions are asserted on the same prose, so a fix that recovered
// framing by refusing to classify anything after prose fails the second half.
func TestClaudeProseCannotPoisonBeyondItsOwnLine(t *testing.T) {
	prose := []struct {
		name string
		line string
	}{
		{"unmatched quote", `crash prose with an unmatched quote: "`},
		{"trailing backslash", `windows path C:\logs\claude\`},
		{"unbalanced open brace", `panic: unexpected { in output`},
		{"unbalanced close brace", `panic: unexpected } in output`},
		{"brace-balanced non-json", `panic: {not json at all}`},
		{"bracketed level tag", `[error] the child died`},
		{"two unmatched quotes and a brace", `he said "hi and left { behind`},
		{"escaped quote in prose", `sed -e 's/\"/x/' failed`},
	}
	for _, p := range prose {
		t.Run(p.name+": the envelope after it still decides", func(t *testing.T) {
			stdout := []byte(p.line + "\n" + f11Envelope401 + "\n")
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "api_error_status 401") {
				t.Fatalf("class = %q (%s), want the 401 envelope to decide behind prose", got.Class, got.Reason)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
		t.Run(p.name+": a genuine 429 behind it still suppresses", func(t *testing.T) {
			assertClaudeStdoutSuppresses(t, []byte(p.line+"\n"+f11Envelope429+"\n"), "api_error_status 429")
		})
	}
}

// Prose BETWEEN two envelopes must not change which one decides. The first
// envelope is a limit and the last is not, so an implementation that stops
// reading at the prose — or that keeps the earlier span and calls it the last —
// suppresses on evidence the transcript itself supersedes.
func TestClaudeProseBetweenTwoEnvelopesDoesNotChangeWhichOneDecides(t *testing.T) {
	t.Run("the later 401 supersedes the earlier 429", func(t *testing.T) {
		stdout := []byte(f11Envelope429 + "\n" + `crash prose with an unmatched quote: "` + "\n" + f11Envelope401 + "\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "api_error_status 401") {
			t.Fatalf("class = %q (%s), want the LAST envelope to decide", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	t.Run("the later 429 supersedes the earlier 401", func(t *testing.T) {
		stdout := []byte(f11Envelope401 + "\n" + `crash prose with an unmatched quote: "` + "\n" + f11Envelope429 + "\n")
		assertClaudeStdoutSuppresses(t, stdout, "api_error_status 429")
	})
}

// The fallback §4.1 step 2 requires is NOT what the gate removed. A window with
// no JSON in it at all — including prose that would have poisoned the old scan —
// still reaches the text fallback and still suppresses.
func TestClaudeTailThatIsEntirelyProseStillFallsBack(t *testing.T) {
	t.Run("plain prose", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t,
			[]byte("claude exited before emitting json\nYou've reached your usage limit.\n"),
			"text fallback on unparsed envelope")
	})
	t.Run("prose carrying the quotes and braces that used to poison the scan", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t, []byte(
			"crash prose with an unmatched quote: \"\n"+
				"panic: {not json at all}\n"+
				`windows path C:\logs\claude\`+"\n"+
				"You've reached your usage limit.\n"), "text fallback on unparsed envelope")
	})
	t.Run("the same prose without a limit phrase decides nothing", func(t *testing.T) {
		stdout := []byte("crash prose with an unmatched quote: \"\nsegmentation fault\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
}

// A window that ends inside a half-written envelope is a FAILURE TO READ one,
// and this is the shape that proves the distinction is not academic: the killed
// write already carried `api_error_status`, because it precedes `result` in every
// captured envelope (§1.4). Treating the truncation as "no JSON at all" hands the
// decision to prose sitting three fields behind a status that contradicts it.
func TestClaudeTruncatedEnvelopeDoesNotLetItsOwnProseDecide(t *testing.T) {
	truncated := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":401,"result":"Invalid API key. You've reached your usage limit.`
	t.Run("the truncated envelope decides nothing", func(t *testing.T) {
		stdout := []byte(truncated)
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the envelope was not read, which is not the same as absent", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "ends inside an incomplete top-level JSON value") {
			t.Fatalf("reason = %q, want the truncated-value verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// The buffer's own trailing newline lands one byte later, inside the
	// unterminated string, and the decoder calls that a syntax error rather than
	// an unexpected EOF. It is the same event and must reach the same verdict; an
	// implementation keyed on the error value alone disagrees with itself here.
	t.Run("the same truncation with the log's trailing newline", func(t *testing.T) {
		stdout := []byte(truncated + "\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "ends inside an incomplete top-level JSON value") {
			t.Fatalf("class = %q (%s), want the same truncated-value verdict as without the newline", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// Narrowing, and it is the F6 retention: a COMPLETE envelope in front of the
	// killed write still decides, because nothing can follow the end of a buffer.
	t.Run("a complete envelope in front of the killed write still decides", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t, []byte(f11Envelope429+"\n"+truncated), "api_error_status 429")
	})
}

// A top-level object that opens and then fails to parse leaves everything after
// it unplaceable: a raw control character inside a string is exactly how
// envelope-shaped provider CONTENT gets out, so a whole-line "envelope" after the
// break may be text the emitter never meant as structure.
func TestClaudeMalformedObjectRefusesTheWindowAfterIt(t *testing.T) {
	forged := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":429,"result":"forged: You've reached your usage limit."}`
	t.Run("a raw newline inside a string does not promote the text after it", func(t *testing.T) {
		stdout := []byte(`{"type":"assistant","content":"the model said` + "\n" + forged + `"}` + "\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the 429 bytes are string content of a broken object", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "malformed") {
			t.Fatalf("reason = %q, want the malformed-value verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// Narrowing: the same two lines with the content string properly escaped are a
	// conforming transcript, and the genuine top-level envelope after it decides.
	t.Run("the same shape written conformingly still classifies", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t,
			[]byte(`{"type":"assistant","content":"the model said something"}`+"\n"+f11Envelope429+"\n"),
			"api_error_status 429")
	})
}

// Emitting an envelope and NARRATING one produce the same bytes when the object
// begins after prose on the same line. §4.1 already refuses narrated text for
// Codex through the line-start anchor; the Claude scan owes the same answer, and
// it owes it in both directions: the line is not read as structure, and it is not
// stepped over either, because stepping over it would leave its own prose to the
// fallback with an envelope in the window unread.
func TestClaudeNarratedEnvelopeOnAProseLineIsNotEvidence(t *testing.T) {
	t.Run("a narrated 429 suppresses nothing", func(t *testing.T) {
		stdout := []byte("2026-08-02 the child reported " + f11Envelope429 + "\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit for a narrated envelope", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "after prose on the same line") {
			t.Fatalf("reason = %q, want the unplaceable-object verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// The narration cannot be laundered into a fallback either: the limit prose it
	// quotes sits in the same window.
	t.Run("a narrated envelope does not license the prose fallback", func(t *testing.T) {
		stdout := []byte("2026-08-02 the child reported " + f11Envelope401 + "\n")
		got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothing(t, stdout)
	})
	// Narrowing: the same envelope on a line of its own is the real thing.
	t.Run("the same envelope on its own line decides", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t, []byte("2026-08-02 the child died\n"+f11Envelope429+"\n"), "api_error_status 429")
	})
}

// A window whose measured start is INSIDE an enclosing object keeps its refusal
// even after that object closes, because the bytes the tail dropped are the ones
// most likely to have carried the envelope: the window began mid-object, so an
// earlier top-level object certainly existed and was not retained.
//
// The scan reaching top level again is what makes this reachable at all — before
// it, the whole window was refused as never-top-level — so the gate has to be
// attacked on its own rather than left to that one.
func TestClaudeNestedWindowThatReturnsToTopLevelDoesNotLicenseProse(t *testing.T) {
	t.Run("prose after the enclosing object closes is not evidence", func(t *testing.T) {
		r := ClaudePromptResult{
			ExitCode:        1,
			Stdout:          []byte(`content"} You've reached your usage limit.` + "\n"),
			StdoutTruncated: true,
			StdoutAnchor:    &TailAnchor{Containers: openObjects(1), InString: true},
		}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the window began inside an object whose envelope was dropped", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "provably top-level") {
			t.Fatalf("reason = %q, want the unproven-top-level verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})
	// Narrowing: an ENVELOPE after the enclosing object closes is still read, so
	// the refusal is about prose rather than about the window being nested.
	t.Run("an envelope after the enclosing object closes still decides", func(t *testing.T) {
		f := newStoreFixture(t)
		identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
		if err != nil {
			t.Fatalf("IdentityFor: %v", err)
		}
		got := ClassifyClaudePrompt(ClaudePromptResult{
			ExitCode:        1,
			Stdout:          []byte(`content"}` + "\n" + f11Envelope429 + "\n"),
			StdoutTruncated: true,
			StdoutAnchor:    &TailAnchor{Containers: openObjects(1), InString: true},
			Now:             f.clock.Now(),
		})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the envelope after the enclosing object to decide", got.Class, got.Reason)
		}
		obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0002f11", Model: "claude-opus-5"})
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
}

// The fallback's licence is a property of the SCAN, not of the search for an
// envelope. Every row here carries the same limit prose, so a row that suppresses
// does so because the scan claimed to have read the whole window — and the last
// row proves the licence still exists.
func TestClaudeFallbackRequiresAScanThatReadTheWholeWindow(t *testing.T) {
	const prose = "You've reached your usage limit."
	cases := []struct {
		name       string
		stdout     []byte
		wantClass  OutcomeClass
		wantReason string
	}{
		{
			"a half-written object stops the scan",
			[]byte(`{"type":"result","result":"` + prose),
			ClassNotLimit,
			"ends inside an incomplete top-level JSON value",
		},
		{
			"a malformed object stops the scan",
			[]byte(`{"type":"result","result":"broken` + "\n" + `{"still":"going"}` + "\n" + prose + "\n"),
			ClassNotLimit,
			"malformed",
		},
		{
			"an object hidden behind prose stops the scan",
			[]byte(`saw {"type":"assistant","content":"x"} on stderr` + "\n" + prose + "\n"),
			ClassNotLimit,
			"after prose on the same line",
		},
		{
			"a window with no JSON in it is read end to end",
			[]byte("claude exited before emitting json\n" + prose + "\n"),
			ClassProviderQuota,
			"text fallback on unparsed envelope",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: tc.stdout})
			if got.Class != tc.wantClass || !strings.Contains(got.Reason, tc.wantReason) {
				t.Fatalf("class = %q (%s), want %q with reason containing %q", got.Class, got.Reason, tc.wantClass, tc.wantReason)
			}
			if tc.wantClass == ClassNotLimit {
				assertClaudeStdoutSuppressesNothing(t, tc.stdout)
			}
		})
	}
}

// Stepping over prose is allowed only where the prose cannot have OPENED
// anything, and "cannot" is checked rather than assumed. Each row here is a prose
// line that leaves a container open, followed by a whole-line 429 envelope that
// is therefore one level deep — the F8 shape reached through a prose line instead
// of through a cut. None of them may suppress.
func TestClaudeProseThatOpensAContainerStopsTheScan(t *testing.T) {
	nested := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":429,"result":"nested, not top-level"}`
	for _, tc := range []struct {
		name   string
		stdout string
	}{
		{"an array opened on a prose line", `[1,` + "\n" + nested + "\n]\n"},
		{"a bare array opener", `[` + "\n" + nested + "\n]\n"},
		// A `{` at the end of a prose line whose first KEY is on the next line is a
		// pretty-printed opener, and everything after it is that object's content.
		// The lookahead has to cross the line boundary to see it, and NOTHING ELSE
		// in this shape does: the following line opens no object of its own, so a
		// check that sliced the region at the newline would step over the opener,
		// find no JSON at all, and hand the limit prose inside the object to the
		// fallback.
		{"an object opener whose first key is on the next line", `stderr said {` + "\n" +
			`"api_error_status":429,"result":"You've reached your usage limit."}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := []byte(tc.stdout)
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q (%s), want not-a-limit: the envelope sits inside a container the prose opened", got.Class, got.Reason)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
	// Narrowing, and the reason the rule is syntactic rather than "a brace stops
	// the scan": a `{` followed by another `{` cannot open a JSON object under any
	// reading, so no emitter wrote one there and the envelope on the next line
	// really is top-level.
	t.Run("a prose brace that cannot open an object costs nothing", func(t *testing.T) {
		assertClaudeStdoutSuppresses(t, []byte("stderr said {\n"+f11Envelope429+"\n"), "api_error_status 429")
	})
	// Narrowing: a BALANCED bracket pair in prose — every log level tag ever
	// written — is inert and must not cost the envelope behind it.
	for _, tag := range []string{"[error]", "[trace]", "[2026-08-02T10:00:00Z]", "he said ] and left"} {
		t.Run("a balanced or closing-only prose bracket keeps classifying: "+tag, func(t *testing.T) {
			assertClaudeStdoutSuppresses(t, []byte(tag+" the child died\n"+f11Envelope429+"\n"), "api_error_status 429")
		})
	}
}

// --- F12: a closing bracket may not cancel an opener that comes AFTER it -----
//
// Bracket safety was a comparison of TOTALS, and totals discard order. A `]` at
// depth zero closes nothing — there is nothing open for it to close — so it is
// prose noise, exactly like the `[error]` tag the balanced case exists to
// protect. Counting it against a `[` that appears LATER in the same region made
// the pair read as balanced and let the scan resume inside the array that `[`
// actually opened, one level below top level. Every whole-line envelope after it
// is then nested, which is the F8 shape reached by a third route.
//
// The rule is now a left-to-right depth walk with the same zero clamp the
// top-level walk uses: a close at depth zero stays inert, an opener that is still
// open at the region's end stops the scan.

// The reviewer's cycle-9 reproduction, byte for byte, driven the whole way
// through the public production path — ClassifyClaudePrompt, QuotaObservation,
// Store.Observe. The 429 in it is real and is a limit; what disqualifies it is
// that it sits INSIDE the array the prose line opened, so it is not a top-level
// Claude result envelope at all.
func TestClaudeCloseBeforeOpenProseCannotAdmitANestedEnvelope(t *testing.T) {
	nested := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":429,"result":"nested, not top-level"}`
	stdout := []byte("crash prose closes noise ] then opens an array [\n" + nested + "\n]\n")
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the 429 is inside the array the prose opened", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "leaves a container open") {
		t.Fatalf("reason = %q, want the unclosed-container verdict rather than a fallback or an object verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// The refusal is about ORDER, so it has to be attacked from both sides in the
// same test: shapes whose brackets are inert must still let a genuine top-level
// 429 through, and shapes that leave one open must not. An implementation that
// simply refused every region containing a `[` would pass the negatives above and
// fail every row below it.
func TestClaudeProseBracketSafetyIsOrderedNotCounted(t *testing.T) {
	nested := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":429,"result":"nested, not top-level"}`
	// Regions that leave a bracket open once order is respected. Each is followed
	// by a whole-line 429 that is therefore one level deep and may not suppress.
	for _, tc := range []struct{ name, prose string }{
		{"a close before an open", "closes noise ] then opens an array ["},
		{"more closes than opens, but an open last", "] ] ] ["},
		{"a balanced tag then an opener", "[error] the child emitted ["},
		{"equal counts in the wrong order", "] [ ] ["},
		{"a nested opener left open", "[[]"},
	} {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			stdout := []byte(tc.prose + "\n" + nested + "\n]\n")
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q (%s), want not-a-limit: %q leaves a container open", got.Class, got.Reason, tc.prose)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
	// Narrowing. Every one of these closes everything it opens — or opens nothing
	// at all — so the envelope on the next line really is top-level and the gate
	// must not cost it anything. `] [ ]` is the pointed one: its counts match the
	// refused `] [` above, and only order tells them apart.
	for _, tc := range []struct{ name, prose string }{
		{"a balanced log tag", "[error] the child died"},
		{"closing-only noise", "he said ] and left"},
		{"closing-only noise, repeated", "] ] ]"},
		{"a close, an open, and its close", "] [ ]"},
		{"balanced nesting", "[a [b] c] done"},
		{"no bracket at all", "the child died"},
	} {
		t.Run("still classifies: "+tc.name, func(t *testing.T) {
			assertClaudeStdoutSuppresses(t, []byte(tc.prose+"\n"+f11Envelope429+"\n"), "api_error_status 429")
		})
	}
}

// The region gate on its own, so the bound is proven by NARROWING rather than
// only by the end-to-end rows: each unsafe answer names the shape it saw, and
// every inert row is asserted inert rather than merely "not the object answer".
func TestClaudeRegionRiskIsDecidedInOrder(t *testing.T) {
	cases := []struct {
		region string
		want   regionRisk
	}{
		{"the child died", regionInert},
		{"[error] tag", regionInert},
		{"] closing noise", regionInert},
		{"] ] ]", regionInert},
		{"] [ ]", regionInert},
		{"[a [b] c]", regionInert},
		{"] [", regionOpensContainer},
		{"] ] ] [", regionOpensContainer},
		{"[error] then [", regionOpensContainer},
		{"[[]", regionOpensContainer},
		{"[", regionOpensContainer},
		// An object opener wins over a bracket: it is the more specific answer and
		// the scan reports the shape it actually found.
		{`saw {"type":"result"} [`, regionOpensObject},
		{`[ {"type":"result"}`, regionOpensObject},
	}
	for _, tc := range cases {
		if got := claudeRegionHidesStructure(tc.region, 0, len(tc.region)); got != tc.want {
			t.Fatalf("claudeRegionHidesStructure(%q) = %v, want %v", tc.region, got, tc.want)
		}
	}
}

// An object start is a syntactic fact, and the scan may step over a `{` only
// where no JSON object can begin. Both halves are attacked: a `{` that cannot
// open an object must not stop the scan, and a `{` that can must not be stepped
// over.
func TestClaudeObjectStartIsDecidedSyntactically(t *testing.T) {
	cases := []struct {
		text  string
		at    int
		begin bool
	}{
		{`{"type":"result"}`, 0, true},
		{`{}`, 0, true},
		{`{ }`, 0, true},
		{"{\n\t\"type\":\"result\"}", 0, true},
		{`{not json at all}`, 0, false},
		{`{429}`, 0, false},
		{`{'type':'result'}`, 0, false},
		{`{`, 0, false},
		{`[{"a":1}]`, 0, false},
		{`[{"a":1}]`, 1, true},
		{`prose`, 0, false},
	}
	for _, tc := range cases {
		if got := claudeObjectBegins(tc.text, tc.at); got != tc.begin {
			t.Fatalf("claudeObjectBegins(%q, %d) = %v, want %v", tc.text, tc.at, got, tc.begin)
		}
	}
}

// --- F15: a closer inside string content may not make nesting shallower ------
//
// F11 stopped lexing prose because untrusted quoting cannot be allowed to swallow
// a real opener. Reading brackets RAW is how that distrust was implemented, but
// raw does not merely distrust quoting — it asserts the opposite, that no `]` is
// ever string content. The reviewer's transcript is the counterexample and it is
// valid RFC 8259 JSON:
//
//	["]",
//	{"type":"result",...,"api_error_status":429,...}
//	]
//
// The first line genuinely opens an array and its only `]` sits inside a JSON
// string. The raw walk cancels a real opener against a byte that closes nothing,
// calls the region inert, resumes on the next line and reads an array ELEMENT as
// a top-level result envelope — a false shared-subscription suppression from
// input that carries no top-level envelope at all.
//
// The gate now walks the region BOTH ways and refuses if either ends open. Each
// reading is attacked below in the direction it is the only defence for, so
// deleting either one turns a test red.

// f15ArrayTranscript is the reviewer's cycle-11 input, byte for byte.
const f15ArrayTranscript = "[\"]\",\n" +
	`{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":429,"result":"nested in an array, not top-level"}` + "\n]\n"

// The reviewer's reproduction driven the whole way through the production path:
// ClassifyClaudePrompt -> Classification.QuotaObservation -> Store.Observe. The
// input is valid JSON and contains no top-level result envelope, so nothing may
// be minted and no group record may exist.
func TestClaudeStringContentCannotCancelAProseArrayOpener(t *testing.T) {
	stdout := []byte(f15ArrayTranscript)
	if !json.Valid(stdout) {
		t.Fatal("the reviewer's transcript must stay valid RFC 8259 JSON, or the finding it encodes is a different one")
	}
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the 429 is an array element, not a top-level envelope", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "leaves a container open") {
		t.Fatalf("reason = %q, want the unclosed-container refusal rather than a fallback or an object verdict", got.Reason)
	}
	assertClaudeStdoutSuppressesNothing(t, stdout)
}

// Neither reading is authoritative, so each is attacked where it is the ONLY
// defence. Delete the string-aware walk and the first group admits; delete the
// raw walk and the second group admits. Every row is driven end to end, because
// a region verdict that never reaches the store proves nothing about suppression.
func TestClaudeProseBracketSafetyUsesBothReadings(t *testing.T) {
	nested := `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
		`"api_error_status":429,"result":"nested, not top-level"}`
	// Only the STRING-AWARE walk refuses these: every one of them ends raw-balanced
	// because a `]` inside string content cancelled a real opener outside it.
	for _, tc := range []struct{ name, prose string }{
		{"the reviewer's array opener", `["]",`},
		{"a closer inside string content, opener outside", `[ "]" `},
		{"an escaped quote does not end the string early", `["\"]",`},
		{"ordered: a real closer first, then a string-quoted one", `] ["]",`},
		{"the closer is inside a longer string", `["some ] text",`},
	} {
		t.Run("refused by the string walk: "+tc.name, func(t *testing.T) {
			stdout := []byte(tc.prose + "\n" + nested + "\n]\n")
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q (%s), want not-a-limit: %q leaves an array open outside its strings", got.Class, got.Reason, tc.prose)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
	// Only the RAW walk refuses these: the string-aware reading believes the
	// opener is quoted, and F11 is the reason that belief cannot be trusted. An
	// implementation that replaced raw with string-aware rather than adding to it
	// admits every one of them.
	for _, tc := range []struct{ name, prose string }{
		{"an unmatched quote before a real opener", `he said "hello [`},
		{"a quoted opener the string walk believes is content", `he said "[" and left`},
		{"a lone opener after quoted noise", `"[" [`},
	} {
		t.Run("refused by the raw walk: "+tc.name, func(t *testing.T) {
			stdout := []byte(tc.prose + "\n" + nested + "\n]\n")
			got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout})
			if got.Class != ClassNotLimit {
				t.Fatalf("class = %q (%s), want not-a-limit: %q may have opened an array under the raw reading", got.Class, got.Reason, tc.prose)
			}
			assertClaudeStdoutSuppressesNothing(t, stdout)
		})
	}
	// Narrowing. The union may only ADD refusals to the one family above; it may
	// not cost a genuine later envelope anything. Each of these closes everything
	// it opens under BOTH readings, so the 429 on the next line still decides.
	// `he said "]" then [x]` is the pointed one: it carries a bracket inside string
	// content and a real bracket pair, and a gate that refused on the mere
	// PRESENCE of a quoted closer would fail it.
	for _, tc := range []struct{ name, prose string }{
		{"a quoted closer with nothing open", `he said "]" and left`},
		{"a quoted closer beside a balanced pair", `he said "]" then [x]`},
		{"an unmatched quote swallows every bracket after it", `crash: " ] [ ]`},
		{"a balanced pair around a quoted closer", `[ "]" ]`},
		{"an escape sequence inside a closed string", `[ "\\" ]`},
		{"an unmatched quote with no bracket at all", `crash prose with an unmatched quote: "`},
	} {
		t.Run("still classifies: "+tc.name, func(t *testing.T) {
			assertClaudeStdoutSuppresses(t, []byte(tc.prose+"\n"+f11Envelope429+"\n"), "api_error_status 429")
		})
	}
}

// The gate on its own, so the bound is proven by NARROWING rather than only end
// to end. Each row names which reading decides it, which is what makes a deleted
// reading visible here rather than only three layers up.
func TestClaudeRegionRiskReadsBracketsBothWays(t *testing.T) {
	cases := []struct {
		region string
		want   regionRisk
	}{
		// Decided by the string-aware walk alone: raw ends balanced.
		{`["]",`, regionOpensContainer},
		{`[ "]" `, regionOpensContainer},
		{`["\"]",`, regionOpensContainer},
		{`] ["]",`, regionOpensContainer},
		{`["some ] text",`, regionOpensContainer},
		// Decided by the raw walk alone: the string-aware walk thinks it is quoted.
		{`he said "hello [`, regionOpensContainer},
		{`he said "[" and left`, regionOpensContainer},
		{`"[" [`, regionOpensContainer},
		// Closed under both readings, so the region stays inert and costs a later
		// envelope nothing.
		{`he said "]" and left`, regionInert},
		{`he said "]" then [x]`, regionInert},
		{`crash: " ] [ ]`, regionInert},
		{`[ "]" ]`, regionInert},
		{`[ "\\" ]`, regionInert},
		{`crash prose with an unmatched quote: "`, regionInert},
		{`["a","b"]`, regionInert},
	}
	for _, tc := range cases {
		if got := claudeRegionHidesStructure(tc.region, 0, len(tc.region)); got != tc.want {
			t.Fatalf("claudeRegionHidesStructure(%q) = %v, want %v", tc.region, got, tc.want)
		}
	}
}
