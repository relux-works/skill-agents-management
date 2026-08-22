package providerlimits

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- F14: an ARRAY buries an envelope exactly as an object does --------------
//
// The cycle-10 reviewer's finding. Every framing gate this module had counted
// BRACES: the head walk, the anchor and the walk back out to top level all
// tracked `{` and `}` and ignored `[` and `]`. So a position inside an array read
// as top level, and the nested object sitting in that array was accepted as the
// authoritative result envelope.
//
// The reproduction needs no malformed input and no lying caller. One RFC 8259
// valid top-level array whose first element is longer than ClassifyScanBytes is
// enough: the module's OWN bounded cut discards the `[`, and everything after it
// is whole-line JSON that a brace-only head walk places at depth zero.
//
// The array shape is the one the transcript fallback cannot excuse either — the
// window holds no top-level envelope at all, so §4.1's precedence has nothing to
// run on, and prose from inside an array element must not decide instead.

const arrayNestedEnvelope = `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":429,"result":"API Error: Request rejected (429) · You've reached your usage limit."}`

const arrayTopLevelEnvelope = `{"type":"result","is_error":true,"terminal_reason":"api_error",` +
	`"api_error_status":429,"result":"API Error: Request rejected (429) · You've reached your usage limit."}`

// oversizedArrayTranscript is the reviewer's F14 input: a valid top-level JSON
// array whose first element is a string long enough that the module's own
// ClassifyScanBytes cut falls inside it, followed by the nested envelope on a
// whole line of its own, followed by the array's close and whatever `after`
// lines the caller wants at genuine top level.
//
// The transcript is asserted VALID as a whole, because the finding is about
// well-formed provider output rather than about noise: an implementation may not
// dismiss this shape as something no emitter could produce.
func oversizedArrayTranscript(t *testing.T, after ...string) []byte {
	t.Helper()
	rest := ""
	if len(after) > 0 {
		rest = strings.Join(after, "\n") + "\n"
	}
	stdout := []byte("[\n\"" + strings.Repeat("a", ClassifyScanBytes+1024) + "\",\n" +
		arrayNestedEnvelope + "\n]\n" + rest)
	arrayEnd := len(stdout) - len(rest)
	if !json.Valid(stdout[:arrayEnd]) {
		t.Fatal("the array must be valid JSON, otherwise the reproduction proves nothing")
	}
	if len(stdout) <= ClassifyScanBytes {
		t.Fatalf("stdout = %d bytes, want more than the %d-byte scan window", len(stdout), ClassifyScanBytes)
	}
	return stdout
}

func TestClaudeArrayElementBeyondTheModuleCutIsNotATopLevelEnvelope(t *testing.T) {
	stdout := oversizedArrayTranscript(t)

	// The measurement the fix rests on, asserted rather than assumed: after the
	// module's own cut the retained window really does begin inside the array,
	// and a brace-only walk would have called the same position top level.
	cut := len(stdout) - ClassifyScanBytes
	start := cut + strings.IndexByte(string(stdout[cut:]), '\n') + 1
	at := claudeScanHeadFrom(stdout[:start], tokenState{})
	if string(at.containers) != "[" {
		t.Fatalf("head state = %q, want exactly one open ARRAY: the dropped head opens the array and nothing else", at.containers)
	}

	r := ClaudePromptResult{ExitCode: 1, Stdout: stdout}
	got := ClassifyClaudePrompt(r)
	if got.Class != ClassNotLimit {
		t.Fatalf("class = %q (%s), want not-a-limit: the 429 sits inside an array element, not at top level", got.Class, got.Reason)
	}
	if !strings.Contains(got.Reason, "provably top-level") {
		t.Fatalf("reason = %q, want the unproven-top-level verdict", got.Reason)
	}
	// The whole point of the finding is the write it produced, so the refusal is
	// asserted at the store and not only at the classifier.
	assertClaudeStdoutSuppressesNothingIn(t, r)
}

// Narrowing, self-cut path. The same oversized array, the same discarded `[`,
// and a GENUINE top-level envelope after the array closes. Refusing this one
// would be a module that stopped classifying rather than one that stopped
// guessing.
func TestClaudeTopLevelEnvelopeAfterTheArrayClosesStillSuppresses(t *testing.T) {
	stdout := oversizedArrayTranscript(t, arrayTopLevelEnvelope)

	f := newStoreFixture(t)
	identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	got := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: stdout, Now: f.clock.Now()})
	if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
		t.Fatalf("class = %q (%s), want the envelope after the array closes to decide", got.Class, got.Reason)
	}
	obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0000f14", Model: "claude-opus-5"})
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

// The same finding through the OTHER cut: a caller-declared tail whose anchor is
// truthful. An anchor that can only say "brace depth zero" describes this
// position as top level, so the shape survives any amount of caller honesty
// until the anchor can name the container KIND.
func TestClaudeAnchoredTailInsideAnArrayIsNotATopLevelEnvelope(t *testing.T) {
	full := []byte("[\n" + arrayNestedEnvelope + "\n]\n")
	if !json.Valid(full) {
		t.Fatal("the array must be valid JSON, otherwise the reproduction proves nothing")
	}
	// The caller retains everything after the array opener, and measures the
	// state at the first retained byte the way a runtime framing the stream
	// would: one open array, outside every string.
	cut := strings.IndexByte(string(full), '\n') + 1
	anchor := anchorFrom(claudeScanHeadFrom(full[:cut], tokenState{}))
	if len(anchor.Containers) != 1 || anchor.Containers[0] != ContainerArray {
		t.Fatalf("anchor = %+v, want exactly one open ARRAY", anchor)
	}

	t.Run("the array element is refused", func(t *testing.T) {
		r := ClaudePromptResult{
			ExitCode:        1,
			Stdout:          full[cut : len(full)-2], // drop the array's closing bracket
			StdoutTruncated: true,
			StdoutAnchor:    anchor,
		}
		got := ClassifyClaudePrompt(r)
		if got.Class != ClassNotLimit {
			t.Fatalf("class = %q (%s), want not-a-limit: the envelope is an array element", got.Class, got.Reason)
		}
		if !strings.Contains(got.Reason, "head puts its first byte inside an enclosing container") {
			t.Fatalf("reason = %q, want the measured nested verdict", got.Reason)
		}
		assertClaudeStdoutSuppressesNothingIn(t, r)
	})

	// Narrowing, anchored path: once the array closes inside the retained tail, a
	// top-level envelope after it decides as usual.
	t.Run("a top-level envelope after the array closes still suppresses", func(t *testing.T) {
		f := newStoreFixture(t)
		identity, err := IdentityFor(ProviderClaude, f.root+"/claude-home")
		if err != nil {
			t.Fatalf("IdentityFor: %v", err)
		}
		tail := append(append([]byte(nil), full[cut:]...), []byte(arrayTopLevelEnvelope+"\n")...)
		got := ClassifyClaudePrompt(ClaudePromptResult{
			ExitCode:        1,
			Stdout:          tail,
			StdoutTruncated: true,
			StdoutAnchor:    anchor,
			Now:             f.clock.Now(),
		})
		if got.Class != ClassProviderQuota || !strings.Contains(got.Reason, "api_error_status 429") {
			t.Fatalf("class = %q (%s), want the envelope after the array closes to decide", got.Class, got.Reason)
		}
		obs, ok := got.QuotaObservation(Evidence{RunID: "RUN-260803-0001f14", Model: "claude-opus-5"})
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

// Closers are TYPED. A single container counter would let a `}` in an array —
// or in prose the head walks — cancel the array's own level, which is the F14
// admission by a shorter route. The unit statement of that rule, in both
// directions so the walk is bounded rather than merely strict.
func TestScanHeadFromTracksContainersByKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		head string
		want containerStack
	}{
		{
			name: "an array opener is a level",
			head: `[`,
			want: "[",
		},
		{
			name: "a brace does not close an array",
			head: `[}`,
			want: "[",
		},
		{
			name: "a bracket does not close an object",
			head: `{]`,
			want: "{",
		},
		{
			name: "each closer closes its own kind",
			head: `[{}]`,
			want: "",
		},
		{
			name: "the stack keeps the order it was opened in",
			head: `{[`,
			want: "{[",
		},
		{
			name: "a closer with nothing open is prose noise",
			head: `] } ]`,
			want: "",
		},
		{
			name: "brackets inside a string are content, not structure",
			head: `{"k":"[[[["`,
			want: "{",
		},
		{
			name: "an unmatched closer never cancels a later opener",
			head: `] [`,
			want: "[",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeScanHeadFrom([]byte(tc.head), tokenState{}); got.containers != tc.want {
				t.Fatalf("containers = %q, want %q", got.containers, tc.want)
			}
		})
	}
}

// The walk back out to top level answers the same question from the other side,
// and it must agree with the head walk: a window that begins inside an array
// reaches top level at the array's own closer and at no earlier byte.
func TestReachTopLevelHonoursArrayNesting(t *testing.T) {
	t.Run("a brace inside the array does not reach top level", func(t *testing.T) {
		text := `{"k":"v"}` + "\n" + arrayNestedEnvelope + "\n"
		if _, ok := claudeReachTopLevel(text, tokenState{containers: "["}); ok {
			t.Fatal("top level was reached inside an array that never closes")
		}
	})
	t.Run("the array's own closer reaches top level", func(t *testing.T) {
		text := `{"k":"v"}` + "\n]\n" + arrayTopLevelEnvelope
		pos, ok := claudeReachTopLevel(text, tokenState{containers: "["})
		if !ok {
			t.Fatal("the array closes in this window, so top level is reached")
		}
		if strings.TrimSpace(text[pos:]) != arrayTopLevelEnvelope {
			t.Fatalf("resumed at %q, want the byte after the array's closer", text[pos:])
		}
	})
}
