package providerlimits

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// --- F10: the boundary state includes the escape bit -------------------------
//
// A ring buffer cuts at a byte offset, and one of the offsets it can cut at is
// between a backslash and the character that backslash escapes. The first
// retained byte is then an escape PAYLOAD: whatever it is, it is not structure.
//
// TailAnchor used to carry depth and string membership but not that fact, and
// both scans re-initialised the escape bit to false. So the payload — commonly a
// quote, since `\"` is the escape a transcript full of prose actually contains —
// was read as the content string's terminator. String state inverted at the
// first byte and stayed inverted, the outer object's real close was read as
// content, and a genuine top-level result envelope later in the same retained
// tail was never seen. A truly exhausted group stayed unsuppressed and every
// concurrent spawn kept hitting the provider.

const escapeBoundaryEnvelope = `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":429,"result":"API Error: Request rejected (429) · You've reached your usage limit."}`

// escapeBoundaryTranscript builds a conforming Claude assistant object whose
// content string contains an escaped quote, and returns the index of the
// BACKSLASH of that escape. Cutting at that index retains the backslash; cutting
// one byte later retains only its payload, which is the F10 boundary.
//
// The content string also carries a full copy of the 429 envelope, so
// envelope-shaped bytes exist inside a JSON string in every variant. That is
// what makes the negative meaningful: reading the boundary wrongly does not
// merely lose evidence, it can promote provider CONTENT to provider structure.
//
// closeOuter picks the two shapes that matter. When it is true the assistant
// object closes and a genuine TOP-LEVEL 429 envelope follows it — the evidence
// that must survive the cut. When it is false the child was killed before the
// closing brace, so nothing in the transcript is top-level at all and the only
// envelope-shaped bytes present are the ones inside the string.
//
// tailFromPayload, when positive, sizes the filler so that the tail beginning at
// the payload byte is EXACTLY that many bytes. The escaping of the content is
// done by encoding/json rather than by hand, so the transcript is real provider
// JSON rather than a shape that merely looks like it.
func escapeBoundaryTranscript(t *testing.T, tailFromPayload int, closeOuter bool) (full []byte, backslash int) {
	t.Helper()
	build := func(filler int) ([]byte, int) {
		t.Helper()
		content := `prefix"` + strings.Repeat("a", filler) + escapeBoundaryEnvelope
		encoded, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("encoding the content string: %v", err)
		}
		text := `{"type":"assistant","content":` + string(encoded)
		if closeOuter {
			text += "}\n" + escapeBoundaryEnvelope + "\n"
		}
		index := strings.Index(text, `prefix\"`)
		if index < 0 {
			t.Fatal("the content string must contain an escaped quote, otherwise there is no boundary to cut at")
		}
		return []byte(text), index + len("prefix")
	}

	filler := 4096
	if tailFromPayload > 0 {
		probe, at := build(0)
		fixed := len(probe) - (at + 1)
		if tailFromPayload < fixed {
			t.Fatalf("a %d-byte tail cannot hold the %d fixed bytes of this shape", tailFromPayload, fixed)
		}
		filler = tailFromPayload - fixed
	}
	full, backslash = build(filler)
	if full[backslash] != '\\' || full[backslash+1] != '"' {
		t.Fatalf("the boundary is not an escaped quote: %q", full[backslash:backslash+2])
	}
	if closeOuter {
		outer := full[:bytes.IndexByte(full, '\n')]
		if !json.Valid(outer) {
			t.Fatal("the assistant object must be valid provider JSON, otherwise the reproduction proves nothing")
		}
	} else if !json.Valid(append(append([]byte(nil), full...), '}')) {
		t.Fatal("the killed transcript must be a prefix of valid provider JSON")
	}
	if tailFromPayload > 0 && len(full)-(backslash+1) != tailFromPayload {
		t.Fatalf("tail from the payload = %d bytes, want %d", len(full)-(backslash+1), tailFromPayload)
	}
	return full, backslash
}

// measuredAnchor is the anchor a runtime that framed the stream while writing it
// would have produced for a cut at index. It is COMPUTED from the dropped bytes
// rather than written by hand: an anchor that did not have to be true would test
// the scanner against the test's own opinion instead of against the transcript.
func measuredAnchor(t *testing.T, full []byte, cut int) *TailAnchor {
	t.Helper()
	at := claudeScanHeadFrom(full[:cut], tokenState{})
	return anchorFrom(at)
}

// The reviewer's cycle-7 reproduction, at the exact retained size production
// uses, driven the whole way through the production path:
// ClassifyClaudePrompt -> QuotaObservation -> Store.Observe.
func TestClaudeAnchoredTailCarriesTheEscapeStateAcrossTheCut(t *testing.T) {
	full, backslash := escapeBoundaryTranscript(t, ClassifyScanBytes, true)
	tail := full[backslash+1:]
	if len(tail) != ClassifyScanBytes {
		t.Fatalf("tail = %d bytes, want exactly the retained window", len(tail))
	}

	anchor := measuredAnchor(t, full, backslash+1)
	// The measurement is the point of the test, so it is asserted rather than
	// assumed: the retained tail really does begin one level deep, inside a
	// string, on the payload of an escape whose backslash was dropped.
	if len(anchor.Containers) != 1 || anchor.Containers[0] != ContainerObject || !anchor.InString || !anchor.Escaped {
		t.Fatalf("anchor = %+v, want one open object, inside a string, on an escape payload", anchor)
	}

	f := newStoreFixture(t)
	identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	got := ClassifyClaudePrompt(ClaudePromptResult{
		ExitCode:        1,
		Stdout:          tail,
		StdoutTruncated: true,
		StdoutAnchor:    anchor,
		Now:             f.clock.Now(),
	})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want provider_quota from the genuine top-level envelope after the cut", got.Class, got.Reason)
	}
	obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0000f10", Model: "claude-opus-5"})
	if !ok {
		t.Fatal("a provider_quota classification must mint an observation")
	}
	if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	record, exists := f.store.GroupRecordFor(identity, "claude-max")
	if !exists || record.State != StateSuppressed {
		t.Fatalf("record = %+v (exists=%v), want the group suppressed by the 429", record, exists)
	}
}

// The fix is proven by NARROWING it. Carrying the escape bit must not turn into
// "an anchored tail always finds its envelope", and it must not make the cut
// offset part of the answer.
func TestClaudeEscapeBoundaryStateIsNarrowedNotDeleted(t *testing.T) {
	// Two cuts one byte apart, on the same transcript, straddling the escape.
	// One retains the backslash and one retains only its payload, so the escape
	// bit is false in the first anchor and true in the second. The transcript
	// they describe is identical, so the verdict must be too — an implementation
	// that carries the bit only on one of the two paths fails here.
	t.Run("cuts immediately before and after the escape agree", func(t *testing.T) {
		full, backslash := escapeBoundaryTranscript(t, 0, true)
		before := measuredAnchor(t, full, backslash)
		after := measuredAnchor(t, full, backslash+1)
		if before.Escaped {
			t.Fatalf("the cut before the backslash must not be an escape payload: %+v", before)
		}
		if !after.Escaped {
			t.Fatalf("the cut after the backslash must be an escape payload: %+v", after)
		}
		reasons := make([]string, 0, 2)
		for _, cut := range []struct {
			name   string
			at     int
			anchor *TailAnchor
		}{
			{"before the backslash", backslash, before},
			{"after the backslash", backslash + 1, after},
		} {
			f := newStoreFixture(t)
			identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
			if err != nil {
				t.Fatalf("IdentityFor: %v", err)
			}
			got := ClassifyClaudePrompt(ClaudePromptResult{
				ExitCode:        1,
				Stdout:          full[cut.at:],
				StdoutTruncated: true,
				StdoutAnchor:    cut.anchor,
				Now:             f.clock.Now(),
			})
			if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
				t.Fatalf("cut %s: class = %q (%s), want provider_quota", cut.name, got.Class, got.Reason)
			}
			obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0001f10", Model: "claude-opus-5"})
			if !ok {
				t.Fatalf("cut %s: a provider_quota classification must mint an observation", cut.name)
			}
			if err := f.store.Observe(identity, "claude-max", nil, obs); err != nil {
				t.Fatalf("cut %s: Observe: %v", cut.name, err)
			}
			if record, exists := f.store.GroupRecordFor(identity, "claude-max"); !exists || record.State != StateSuppressed {
				t.Fatalf("cut %s: record = %+v (exists=%v), want a suppressed group", cut.name, record, exists)
			}
			reasons = append(reasons, got.Reason)
		}
		if reasons[0] != reasons[1] {
			t.Fatalf("the two cuts disagree about the same transcript:\n  before: %s\n  after:  %s", reasons[0], reasons[1])
		}
	})

	// The negative the fix must keep. The same escape boundary, but the child was
	// killed before the assistant object closed, so the only envelope-shaped
	// bytes in the tail are the ones INSIDE its content string. Absolute depth
	// never returns to zero, nothing is top-level, and no group record may be
	// written. A scanner that carried the escape bit but stopped measuring depth
	// would suppress on provider content here.
	t.Run("envelope-shaped bytes inside an unclosed object still write nothing", func(t *testing.T) {
		full, backslash := escapeBoundaryTranscript(t, 0, false)
		tail := full[backslash+1:]
		anchor := measuredAnchor(t, full, backslash+1)
		if !anchor.Escaped || len(anchor.Containers) != 1 || anchor.Containers[0] != ContainerObject {
			t.Fatalf("anchor = %+v, want the same boundary as the positive", anchor)
		}
		r := ClaudePromptResult{ExitCode: 1, Stdout: tail, StdoutTruncated: true, StdoutAnchor: anchor}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the envelope bytes are string content", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "provably top-level") {
			t.Fatalf("reason = %q, want the unproven-top-level verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})

	// An escape outside a string is not a weaker measurement, it is an impossible
	// one: JSON has no escapes there. Such an anchor is discarded whole, exactly
	// as an unknown container kind is, so the tail is refused rather than scanned
	// from a state its caller has already contradicted.
	t.Run("an escaped-but-not-in-string anchor is discarded, not partly believed", func(t *testing.T) {
		r := ClaudePromptResult{
			ExitCode:        1,
			Stdout:          []byte(escapeBoundaryEnvelope + "\n"),
			StdoutTruncated: true,
			StdoutAnchor:    &TailAnchor{InString: false, Escaped: true},
		}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit || !strings.Contains(got.Reason, "declared tail with no anchor") {
			t.Fatalf("class = %q (%s), want the unanchored verdict for a contradictory anchor", got.Class, got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})

	// The escape bit reaches the scan through the HEAD WALK as well as through
	// the no-cut path: a tail larger than the retained window is cut again by
	// this module, and the walk over the dropped bytes must start from the
	// caller's escape state too. Here the extra bytes are inside the same content
	// string, so a walk seeded without the bit closes that string one byte in and
	// hands the scan an inverted state.
	t.Run("the head walk starts from the escape state too", func(t *testing.T) {
		full, backslash := escapeBoundaryTranscript(t, ClassifyScanBytes, true)
		// One byte past the retained window, so claudeFrameWindow cuts and walks.
		tail := full[backslash+1:]
		oversized := append([]byte(`\`), tail...)
		anchor := measuredAnchor(t, full, backslash)
		got := ClassifyClaudePrompt(ClaudePromptResult{
			ExitCode:        1,
			Stdout:          oversized,
			StdoutTruncated: true,
			StdoutAnchor:    anchor,
		})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want provider_quota through the head-walk path", got.Class, got.Reason)
		}
	})
}

// The state is threaded as one value, and the scanner honours every field of it.
// This is the unit-level statement of the same contract the reproductions assert
// end to end: a seeded escape suppresses the structural meaning of exactly one
// byte, no more and no less.
func TestScanHeadFromHonoursTheSeededEscapeState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		head  string
		start tokenState
		want  tokenState
	}{
		{
			name:  "a seeded escape consumes the quote that would otherwise close the string",
			head:  `"`,
			start: tokenState{containers: "{", inString: true, escaped: true},
			want:  tokenState{containers: "{", inString: true},
		},
		{
			name:  "without the seed the same quote closes the string",
			head:  `"`,
			start: tokenState{containers: "{", inString: true},
			want:  tokenState{containers: "{"},
		},
		{
			name:  "the seed spends itself on one byte only",
			head:  `""`,
			start: tokenState{containers: "{", inString: true, escaped: true},
			want:  tokenState{containers: "{"},
		},
		{
			name:  "a backslash inside a string arms the escape for the next window",
			head:  `\`,
			start: tokenState{containers: "{{", inString: true},
			want:  tokenState{containers: "{{", inString: true, escaped: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeScanHeadFrom([]byte(tc.head), tc.start); got != tc.want {
				t.Fatalf("state = %+v, want %+v", got, tc.want)
			}
		})
	}
}
