package providerlimits

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OutcomeClass is the classifier's verdict. There are two limit classes and they
// are disjoint on purpose: only ClassProviderQuota is evidence about the shared
// subscription, and only ClassProviderQuota can be turned into an Observation.
type OutcomeClass string

const (
	// ClassNotLimit means the failure is not a limit of any kind.
	ClassNotLimit OutcomeClass = ""
	// ClassProviderQuota means the provider reported its own shared
	// subscription exhausted. This is the only class that may suppress a group.
	ClassProviderQuota OutcomeClass = "provider_quota"
	// ClassRunBudget means a cap this runtime itself imposed on one run was
	// exhausted. It proves nothing about the shared subscription and must never
	// reach a group record.
	ClassRunBudget OutcomeClass = "run_budget"
)

// ClassifyScanBytes is the tail of a transcript the classifiers read. It matches
// the buffer the spawn runtime already keeps.
const ClassifyScanBytes = 256 << 10

// ResetHintNote is stored beside every captured hint so a reader of the state
// file cannot mistake the hint for an authority.
const ResetHintNote = "diagnostic only; may shorten a backoff step, never extend or suppress"

// ResetHint is the provider's own prose about when access returns. It is
// diagnostic. Raw is always kept; Parsed is set only when the text parsed into a
// future instant, and Used records whether that instant actually bound a
// backoff step through min().
type ResetHint struct {
	Raw    string     `json:"raw"`
	Parsed *time.Time `json:"parsed,omitempty"`
	Used   bool       `json:"used"`
	Note   string     `json:"note,omitempty"`
}

// Evidence is the caller-supplied provenance of one observation. It is written
// into the group record so an operator can see which run, model and log
// produced a suppression.
type Evidence struct {
	RunID   string `json:"run_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
	Model   string `json:"model,omitempty"`
	Marker  string `json:"marker,omitempty"`
	Log     string `json:"log,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// Classification is one classifier's result.
type Classification struct {
	Class     OutcomeClass
	Provider  string
	Marker    string
	Excerpt   string
	ResetHint *ResetHint
	// Reason names the rule that fired. It exists so a negative verdict is
	// explainable rather than merely silent.
	Reason string
}

// IsProviderQuota reports shared-subscription exhaustion.
func (c Classification) IsProviderQuota() bool { return c.Class == ClassProviderQuota }

// IsRunBudget reports exhaustion of a cap this runtime imposed.
func (c Classification) IsRunBudget() bool { return c.Class == ClassRunBudget }

// Observation is the only value Store.Observe accepts. Its validity gate is
// unexported, so an Observation can be produced solely by
// Classification.QuotaObservation on a provider_quota classification. A
// hand-built Observation from another package therefore carries quota=false and
// is refused by Observe: the run_budget class has no path to a group record.
type Observation struct {
	quota bool

	Provider  string
	Evidence  Evidence
	ResetHint *ResetHint
	// At overrides the store clock for this observation. Zero means "now".
	At time.Time
}

// IsProviderQuota reports whether this observation carries the gate Observe
// requires.
func (o Observation) IsProviderQuota() bool { return o.quota }

// QuotaObservation converts a classification into an Observation. It succeeds
// only for ClassProviderQuota; every other class — notably ClassRunBudget —
// returns ok=false and a zero Observation that Observe refuses.
//
// It also refuses a classification naming a provider this module cannot
// classify. Limit detection is scoped to the providers whose real error shapes
// were captured, and a provider with no classifier must be unsuppressible on
// every path, not merely on the ones that happen to be provider-fixed.
func (c Classification) QuotaObservation(ev Evidence) (Observation, bool) {
	if c.Class != ClassProviderQuota {
		return Observation{}, false
	}
	if !HasClassifierForRuntime(c.Provider) {
		return Observation{}, false
	}
	if ev.Marker == "" {
		ev.Marker = c.Marker
	}
	if ev.Excerpt == "" {
		ev.Excerpt = c.Excerpt
	}
	return Observation{
		quota:     true,
		Provider:  c.Provider,
		Evidence:  ev,
		ResetHint: c.ResetHint,
	}, true
}

// --- Codex, prompt mode -----------------------------------------------------

// CodexPromptResult is one finished `codex exec` child.
type CodexPromptResult struct {
	ExitCode int
	// Log is the captured child transcript. Only the last ClassifyScanBytes are
	// scanned; the caller may pass the whole file or a tail.
	Log []byte
	// LogTruncated declares that Log is already a TAIL of a longer transcript,
	// so its first line may be a fragment. A caller that keeps a ring buffer
	// must set it. Without it a fragment cut mid-line would be read as a line,
	// and the cut itself would supply the line-start anchor the marker requires.
	// It is a declaration rather than a guess because a tail is not detectable
	// from its own bytes.
	LogTruncated bool
	// Now anchors a relative reset hint ("try again in 90 seconds"). Zero means
	// time.Now().
	Now time.Time
}

// codexUsageMarker is matched at line start, after apostrophe normalisation and
// case folding, with the ERROR: prefix required. Quoted or narrated text — this
// repository's own logs and design documents are full of it — is not a provider
// error.
const codexUsageMarker = "error: you've hit your usage limit"

// ClassifyCodexPrompt classifies a plain-text `codex exec` failure.
func ClassifyCodexPrompt(r CodexPromptResult) Classification {
	if r.ExitCode == 0 {
		return Classification{Provider: ProviderCodex, Reason: "exit code 0 is never classified"}
	}
	raw := tailLines(r.Log, ClassifyScanBytes, r.LogTruncated)
	normalized := normalizeApostrophes(raw)
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(strings.ToLower(line), codexUsageMarker) {
			continue
		}
		return Classification{
			Class:     ClassProviderQuota,
			Provider:  ProviderCodex,
			Marker:    "you've hit your usage limit",
			Excerpt:   strings.TrimSpace(line),
			ResetHint: codexResetHint(line, r.now()),
			Reason:    "anchored codex usage-limit marker",
		}
	}
	return Classification{Provider: ProviderCodex, Reason: "no anchored codex usage-limit marker"}
}

func (r CodexPromptResult) now() time.Time {
	if r.Now.IsZero() {
		return time.Now()
	}
	return r.Now
}

var codexRetryAt = regexp.MustCompile(`(?i)\bor try again (at|in) (.+)$`)

func codexResetHint(line string, now time.Time) *ResetHint {
	match := codexRetryAt.FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	raw := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[0]), "."))
	hint := &ResetHint{Raw: raw, Note: ResetHintNote}
	body := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[2]), "."))
	if strings.EqualFold(match[1], "in") {
		hint.Parsed = parseRelativeInstant(body, now)
	} else {
		hint.Parsed = parseAbsoluteInstant(body, now)
	}
	return hint
}

// --- Claude, prompt mode ----------------------------------------------------

// ClaudePromptResult is one finished `claude -p --output-format json` child.
type ClaudePromptResult struct {
	ExitCode int
	Stdout   []byte
	// StdoutTruncated declares that Stdout is already a TAIL of a longer
	// transcript, so it may begin inside a JSON string. A caller that keeps a
	// ring buffer must set it; see claudeFrameWindow for why a headless window
	// cannot be scanned from a guessed token state. It is a declaration rather
	// than a guess because a tail is not detectable from its own bytes.
	StdoutTruncated bool
	// StdoutAnchor optionally supplies the JSON token state at Stdout[0] for a
	// declared tail. Without it a tail has NO top level (F9): see TailAnchor.
	// It is ignored when StdoutTruncated is false, where byte 0 is absolute
	// depth zero by construction.
	StdoutAnchor *TailAnchor
	// Now anchors a time-of-day reset hint ("your limit resets at 10:30am").
	// Zero means time.Now().
	Now time.Time
}

// TailAnchor is the JSON token state at the FIRST byte of a declared tail,
// measured by the caller that framed the stream while it was being written.
//
// It exists because a declared tail cannot establish top level from its own
// bytes, and the module must not pretend otherwise (F9). The inference it
// replaces argued: a finished child closes what it opens, so a window whose
// relative depth ends at zero must have begun at absolute depth zero. The
// premise is false for exactly the input this module is built to classify — a
// child that DIED. A killed write leaves the transcript ending inside an open
// object, so a tail can begin one level deep, carry a balanced nested object
// shaped like a 429 result envelope, and end at relative depth zero with no
// top-level envelope anywhere in it.
//
// No byte in such a window distinguishes it from a genuine top-level one, so
// the anchor has to come from somewhere that KNOWS: a caller that scanned the
// stream as it wrote it and therefore measured the state at the point it began
// retaining. A caller that cannot measure it supplies nothing, and its tail is
// reported unproven rather than guessed at.
//
// The anchor carries the COMPLETE lexer state at that byte, not a summary of it.
// Depth and InString alone are not the state: a cut may fall immediately after an
// unescaped backslash, which makes Stdout[0] an escaped character rather than a
// structural one. A scanner restarted with escaped=false there reads the escaped
// quote as a string terminator, inverts its string state for the rest of the
// window, and misses a genuine top-level envelope that follows (F10). Escaped is
// that dropped bit, so no part of the boundary state is left to be re-guessed.
//
// Containers is the OPEN CONTAINER STACK rather than a brace count, because JSON
// nests in two shapes and only one of them is made of braces (F14). A count of
// braces reads a position inside `[ ... , {envelope} ]` as top level, since the
// array that encloses it contributes nothing to that count — so a nested object
// shaped like a 429 result envelope is admitted as the authoritative one. Arrays
// are structure exactly as objects are, and the anchor carries both or it carries
// nothing useful.
//
// A stack rather than a depth number, because closers are typed: `}` closes an
// object and `]` closes an array, and a `}` while the innermost open container is
// an array closes nothing. A single counter cannot express that, so prose noise in
// one shape would cancel a real opener of the other.
//
// An element that is neither ContainerObject nor ContainerArray is not a weaker
// anchor, it is a broken one, and the whole anchor is discarded as if none had
// been supplied. Escaped without InString is broken in the same way: JSON has no
// escapes outside a string, so such a pair cannot have been measured and is
// discarded rather than partly believed.
type TailAnchor struct {
	// Containers is the stack of JSON containers still open at Stdout[0],
	// OUTERMOST FIRST. Empty or nil means the tail begins at top level.
	Containers []ContainerKind
	// InString reports that Stdout[0] lies inside a JSON string.
	InString bool
	// Escaped reports that Stdout[0] is ESCAPED BY THE BYTE IMMEDIATELY BEFORE
	// IT — that is, the dropped byte at Stdout[-1] was a backslash acting as an
	// escape introducer inside a string, so Stdout[0] is that escape's payload
	// and carries no structural meaning whatever it happens to be.
	//
	// It is meaningful only together with InString, and it describes the FIRST
	// RETAINED byte rather than the dropped backslash: a caller that cut between
	// `\` and `"` sets Containers to the enclosing containers, InString to true,
	// and Escaped to true.
	Escaped bool
}

// ContainerKind is one open JSON container: an object or an array. It is the
// opening byte itself so the stack reads as the transcript does.
type ContainerKind byte

const (
	// ContainerObject is an object opened by `{` and closed only by `}`.
	ContainerObject ContainerKind = '{'
	// ContainerArray is an array opened by `[` and closed only by `]`.
	ContainerArray ContainerKind = '['
)

func (k ContainerKind) valid() bool {
	return k == ContainerObject || k == ContainerArray
}

func (a *TailAnchor) usable() bool {
	if a == nil || (a.Escaped && !a.InString) {
		return false
	}
	for _, kind := range a.Containers {
		if !kind.valid() {
			return false
		}
	}
	return true
}

// state is the anchor as the scanner consumes it. The conversion exists so a
// caller's three fields become one value that is threaded whole.
func (a *TailAnchor) state() tokenState {
	stack := make([]byte, 0, len(a.Containers))
	for _, kind := range a.Containers {
		stack = append(stack, byte(kind))
	}
	return tokenState{containers: containerStack(stack), inString: a.InString, escaped: a.Escaped}
}

// containerStack is the open container stack as the scanner carries it: one byte
// per level, OUTERMOST FIRST, each byte the container's opening byte.
//
// It is a string rather than a slice so tokenState stays a comparable VALUE that
// can be copied, compared and threaded without any caller sharing a backing array
// with a scan still running. The byte walks that consume it work on a local slice
// and convert back once, so appending stays amortised.
type containerStack string

func (s containerStack) empty() bool { return len(s) == 0 }

// containerCloses reports whether closer closes a container opened by opener.
// `}` closes an object and `]` closes an array; nothing else closes either, and
// crossing them is not a close at all.
func containerCloses(opener, closer byte) bool {
	return (opener == '{' && closer == '}') || (opener == '[' && closer == ']')
}

// containerWalk applies one non-string byte to a container stack and returns the
// stack after it.
//
// Two rules, and both of them refuse to invent structure:
//
//   - An opener pushes its own kind, so an array and an object are never
//     interchangeable levels (F14).
//   - A closer pops only the container it actually closes. A `}` at an empty
//     stack, or a `}` while the innermost open container is an array, closes
//     NOTHING: it is noise in a transcript that also carries plain text, and the
//     one thing it must never do is make the scan believe it has returned to a
//     shallower level than it is really at.
func containerWalk(stack []byte, c byte) []byte {
	switch c {
	case '{', '[':
		return append(stack, c)
	case '}', ']':
		if n := len(stack); n > 0 && containerCloses(stack[n-1], c) {
			return stack[:n-1]
		}
	}
	return stack
}

// tokenState is the COMPLETE JSON lexer state at one byte position: the open
// container stack, whether that byte lies inside a string, and whether it is the
// payload of an escape introduced by the byte before it.
//
// It is one struct rather than three parameters because F10 was exactly a
// partial copy of this state across a boundary — depth and string membership
// were carried, the escape bit was re-initialised to false, and the scanner
// silently disagreed with the transcript from the first retained byte onward.
// Passing the state as a unit makes dropping one field a compile-time change
// rather than a plausible-looking line. F14 was the same defect in the other
// direction: the state was threaded whole, but it was INCOMPLETE — it described
// object nesting and said nothing about array nesting, so a position inside an
// array read as top level.
type tokenState struct {
	containers containerStack
	inString   bool
	escaped    bool
}

// topLevel is THE definition of "this position can begin a top-level value":
// outside every container and outside every string.
//
// It is a free function taking the two facts rather than a method, so the live
// scanner and the frozen state can both answer the question WITHOUT either one
// restating it. There is exactly one definition of top-level-ness in this module
// and this is it.
func topLevel(openContainers int, inString bool) bool {
	return openContainers == 0 && !inString
}

// atTopLevel reports that this position is outside every container and every
// string, which is the only position a top-level value can begin at.
func (s tokenState) atTopLevel() bool { return topLevel(len(s.containers), s.inString) }

// nested reports that this position is inside something — a container, a string,
// or both — so nothing found from here is top-level until that structure closes.
func (s tokenState) nested() bool { return !s.atTopLevel() }

// quoting is how a scan interprets `"`. It is the ONE input-handling difference
// between reading a measured JSON span and reading an untrusted prose region, and
// it is a parameter of the single scanner rather than a reason to write a second
// one.
type quoting int

const (
	// quotingTrusted reads `"` as a string delimiter and honours backslash escapes
	// inside strings. This is JSON as written, and every measured span uses it.
	quotingTrusted quoting = iota
	// quotingIgnored reads `"` as an ordinary byte, so every bracket counts as
	// structure. It exists for prose regions, whose quoting proved untrustworthy
	// (F11): an unmatched quote in narration must not be allowed to swallow a real
	// container opener.
	quotingIgnored
)

// tokenScanner is THE scanner of this module. Every path that needs to know
// whether a position is nested, contained, or top level advances one of these and
// reads its state; there is no second implementation and no path-specific
// shortcut.
//
// That is not tidiness. This module has been through a series of defects — F8,
// F10, F12, F14, F15 — that were each the SAME defect discovered in a different
// copy of this walk: a copy that dropped the escape bit, a copy that counted
// instead of ordering, a copy that tracked braces and not brackets, a copy that
// read brackets raw and so let string content cancel a real opener. Repairing the
// copies one at a time is what made it a series. One scanner is what ends it: a
// rule fixed here is fixed on every path by construction, because there is
// nowhere else for a path to get a different answer.
//
// The stack is kept as a local slice for the life of the scan so appends amortise;
// tokenState keeps it as a comparable string so it can be threaded across
// boundaries without sharing a backing array with a scan still running.
type tokenScanner struct {
	at    tokenState
	stack []byte
	q     quoting
}

// newTokenScanner starts a scan at the given state. The starting state is a
// parameter rather than a constant because a scan does not always begin at the
// start of a transcript: an anchored tail begins at the caller's measured
// position, and assuming a zero state there is exactly the F10/F14 defect.
func newTokenScanner(at tokenState, q quoting) *tokenScanner {
	return &tokenScanner{
		at:    at,
		stack: append(make([]byte, 0, len(at.containers)+8), at.containers...),
		q:     q,
	}
}

// step applies exactly one byte to the scan.
//
// Under quotingTrusted a string swallows every byte until its unescaped closing
// quote, so brackets inside string CONTENT are content (F15) and `"\""` does not
// end its string early (F10). Under quotingIgnored there is no string at all and
// every byte reaches the container walk (F11).
//
// Containers are tracked by KIND and unmatched closers are ignored, both of which
// live in containerWalk: an array and an object are never interchangeable levels
// (F14), and a closer that closes nothing open is prose noise that must never make
// the scan believe it has returned to a shallower level than it is really at
// (F12's zero clamp).
func (sc *tokenScanner) step(c byte) {
	if sc.q == quotingTrusted {
		if sc.at.inString {
			switch {
			case sc.at.escaped:
				sc.at.escaped = false
			case c == '\\':
				sc.at.escaped = true
			case c == '"':
				sc.at.inString = false
			}
			return
		}
		if c == '"' {
			sc.at.inString = true
			return
		}
	}
	sc.stack = containerWalk(sc.stack, c)
}

// atTopLevel reports whether the scan is currently at a position where a
// top-level value can begin. It defers to topLevel, so it cannot drift from
// tokenState's answer to the same question.
func (sc *tokenScanner) atTopLevel() bool { return topLevel(len(sc.stack), sc.at.inString) }

// containersClosed reports that the scan has closed every container it opened,
// saying nothing about string state.
//
// It is deliberately weaker than atTopLevel and has exactly one caller: a prose
// region, which ends at a newline. RFC 8259 puts a newline outside every string,
// and the stream scan resumes there, so a quote left open by narration does not
// survive the region — but a container the region opened would still enclose
// everything after it.
func (sc *tokenScanner) containersClosed() bool { return len(sc.stack) == 0 }

// state freezes the scan into the comparable value that crosses boundaries.
func (sc *tokenScanner) state() tokenState {
	sc.at.containers = containerStack(sc.stack)
	return sc.at
}

func (r ClaudePromptResult) now() time.Time {
	if r.Now.IsZero() {
		return time.Now()
	}
	return r.Now
}

// claudeEnvelope is the subset of the prompt-mode result envelope classification
// depends on. api_error_status is kept raw so an absent field and an explicit
// null are distinguishable — the null case is the client-exhausted-its-retries
// shape, and it is the only case that falls through to text.
type claudeEnvelope struct {
	Type           string          `json:"type"`
	IsError        *bool           `json:"is_error"`
	TerminalReason string          `json:"terminal_reason"`
	APIErrorStatus json.RawMessage `json:"api_error_status"`
	Result         json.RawMessage `json:"result"`
}

// claudeLimitPhrases is the text fallback, reached only when the envelope
// carries no api_error_status or did not parse at all.
var claudeLimitPhrases = []string{
	"you've reached your usage limit",
	"you've hit your limit",
	"usage limit reached",
	"usage credit limit reached",
	"credit balance is too low",
	"/upgrade to increase your usage limit",
}

// ClassifyClaudePrompt classifies a `claude -p --output-format json` failure.
//
// Precedence is explicit because the envelope is only partially authoritative:
// the parsed envelope decides first, api_error_status is the authoritative
// discriminator when present, and text is consulted only when the status is
// absent or null. subtype is never consulted — it reads "success" on every
// observed failure.
func ClassifyClaudePrompt(r ClaudePromptResult) Classification {
	if r.ExitCode == 0 {
		return Classification{Provider: ProviderClaude, Reason: "exit code 0 is never classified"}
	}
	out := Classification{Provider: ProviderClaude}
	win := claudeFrameWindow(r.Stdout, r.StdoutTruncated, r.StdoutAnchor)
	stdout := win.text

	envelope, parse, topLevel := parseClaudeEnvelope(win)
	switch parse {
	case claudeParseTruncatedValue:
		// A top-level value opened and the window ended inside it. The bytes that
		// would have completed it were never seen, so this is a FAILURE TO READ an
		// envelope, not evidence that none was written — and the fields a killed
		// write does reach are exactly the authoritative ones: `api_error_status`
		// precedes `result` in every captured envelope (§1.4), so the prose in a
		// half-written envelope is routinely accompanied by a status that
		// contradicts it. Licensing the fallback here would let that prose decide
		// over a discriminator sitting in the same broken object.
		out.Reason = "the window ends inside an incomplete top-level JSON value, so no envelope was read; transcript fallback is not permitted"
		return out
	case claudeParseMalformedValue:
		// A top-level value opened and did not parse. Whatever follows it may be
		// its interior, so no later span can be placed and no prose in the window
		// can be read as provider error output.
		out.Reason = "a top-level JSON value in the window is malformed, so the transcript's framing after it is unknown; transcript fallback is not permitted"
		return out
	case claudeParseUnplaceableObject:
		// An object begins after prose on the same line. A line that NARRATES an
		// envelope and a line that emits one are the same bytes, so it is not read
		// as structure — and it is not stepped over either, because doing so would
		// leave the prose around it to decide with an envelope in the window unread.
		out.Reason = "an object begins after prose on the same line, so it cannot be placed as a top-level value; transcript fallback is not permitted"
		return out
	case claudeParseUnclosedContainer:
		// A prose region left a square bracket open, so everything after it is
		// inside an array the scan never read. Same answer as the narrated object
		// and for the same reason: what follows cannot be placed, and stepping over
		// the opener would hand the window's prose to the fallback with a nested
		// envelope in it unread.
		out.Reason = "a prose region leaves a container open, so nothing after it can be placed as a top-level value; transcript fallback is not permitted"
		return out
	case claudeParseUnprovenTopLevel:
		// The window could be framed but no position in it has a known absolute
		// nesting, so an object closing at scanner depth zero may be a NESTED
		// one — the F8, F9 and F14 reproductions. §4.1 asks for the last complete
		// TOP-LEVEL object, so an unproven one is not a weaker version of that
		// answer, it is no answer. No fallback either, for the same reason an
		// unframable window gets none: prose in a window whose structure we
		// cannot place is not evidence about what the provider said.
		out.Reason = "scan window is truncated and no span in it is provably top-level: " + topLevel.describe() + "; transcript fallback is not permitted"
		return out
	case claudeParseUnframable:
		// The tail was cut at a byte count and contains no line boundary, so
		// there is no position in it whose JSON token state is known. Any
		// framing would be a guess, and a wrong guess hides an authoritative
		// envelope. Absence of a readable envelope in an unframable window is
		// not evidence that no envelope was written — the same argument this
		// module already applies to an unreadable api_error_status and to a
		// complete-but-undecodable envelope. No fallback: prose inside a window
		// we cannot frame is exactly the input that must not suppress.
		out.Reason = "scan window is truncated and contains no line boundary, so no JSON token position in it is known; transcript fallback is not permitted"
		return out
	case claudeParseNonResult:
		// A complete object parsed and it is not a result envelope. §4.1 permits
		// the transcript fallback only when the JSON did not parse AT ALL; a
		// parsed object of another type falls to "anything else ⇒ not a limit".
		// Relabelling it as unparsed would let prose anywhere in the transcript
		// decide whenever the run happened to end on a non-result line.
		out.Reason = "last complete JSON object is not a result envelope; transcript fallback is not permitted"
		return out
	case claudeParseUnreadableResult:
		// Same reasoning as a present-but-unreadable api_error_status: the
		// authoritative envelope is there and unreadable, which is not evidence
		// that it was absent.
		out.Reason = "last complete JSON object is a result envelope whose fields did not decode; transcript fallback is not permitted"
		return out
	case claudeParseResult:
		if envelope.IsError == nil || !*envelope.IsError {
			out.Reason = "result envelope reports is_error false"
			return out
		}
		if envelope.TerminalReason != "api_error" {
			out.Reason = "terminal_reason is not api_error"
			return out
		}
		result := decodeJSONString(envelope.Result)
		status, form := decodeAPIErrorStatus(envelope.APIErrorStatus)
		switch form {
		case statusMalformed:
			// Present, non-null, and not an integer. This is NOT the absent case:
			// the authoritative discriminator is there and unreadable, which is
			// evidence that something is wrong with the envelope, not evidence
			// that the field was omitted. Text fallback is permitted only when
			// the field is absent or null, so an unreadable status stops here
			// rather than letting limit prose decide.
			out.Reason = "api_error_status is present and not an integer (" + truncate(rawStatusText(envelope.APIErrorStatus), 64) + "); text fallback is not permitted"
			return out
		case statusInteger:
			switch {
			case status == 429:
				out.Class = ClassProviderQuota
				out.Marker = "api_error_status 429"
				out.Excerpt = truncate(result, 512)
				out.ResetHint = claudeResetHint(result, r.now())
				out.Reason = "api_error_status 429"
			case status == 400 && strings.Contains(strings.ToLower(result), "credit balance is too low"):
				out.Class = ClassProviderQuota
				out.Marker = "credit balance is too low"
				out.Excerpt = truncate(result, 512)
				out.Reason = "api_error_status 400 with credit-balance text"
			default:
				out.Reason = "api_error_status " + strconv.Itoa(status) + " is not a limit"
			}
			return out
		}
		// Status absent or explicitly null — and nothing else, because a present
		// unreadable status returned above. Text over `result` is the only
		// signal left; this is the 529-retry-exhausted shape.
		if phrase, hit := matchClaudePhrase(result); hit {
			out.Class = ClassProviderQuota
			out.Marker = phrase
			out.Excerpt = truncate(result, 512)
			out.ResetHint = claudeResetHint(result, r.now())
			out.Reason = "text fallback on " + form.describe() + " api_error_status"
			return out
		}
		out.Reason = form.describe() + " api_error_status and no limit phrase in result"
		return out
	}

	// claudeParseNone only: no complete top-level JSON object was found in a
	// window that WAS framable. Fall back to text over that same window — never
	// over bytes outside it, which have no known token state.
	if phrase, hit := matchClaudePhrase(stdout); hit {
		out.Class = ClassProviderQuota
		out.Marker = phrase
		out.Excerpt = truncate(strings.TrimSpace(lastLineContaining(stdout, phrase)), 512)
		out.ResetHint = claudeResetHint(stdout, r.now())
		out.Reason = "text fallback on unparsed envelope"
		return out
	}
	out.Reason = "no result envelope and no limit phrase"
	return out
}

func matchClaudePhrase(text string) (string, bool) {
	folded := strings.ToLower(normalizeApostrophes(text))
	for _, phrase := range claudeLimitPhrases {
		if strings.Contains(folded, phrase) {
			return phrase, true
		}
	}
	return "", false
}

var claudeResetAt = regexp.MustCompile(`(?i)\b(?:your limit resets at|limit resets at|try again at|try again in) ([^.\n]+)`)

func claudeResetHint(text string, now time.Time) *ResetHint {
	match := claudeResetAt.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	raw := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[0]), "."))
	hint := &ResetHint{Raw: raw, Note: ResetHintNote}
	body := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[1]), "."))
	if parsed := parseRelativeInstant(body, now); parsed != nil {
		hint.Parsed = parsed
		return hint
	}
	if parsed := parseAbsoluteInstant(body, now); parsed != nil {
		hint.Parsed = parsed
		return hint
	}
	hint.Parsed = parseClockInstant(body, now)
	return hint
}

// --- Managed (session-manager) paths ---------------------------------------

// ManagedSignal is a provider status the session-manager path already produces.
type ManagedSignal string

const (
	// ManagedSignalUsageLimited is the provider reporting its own subscription
	// exhausted.
	ManagedSignalUsageLimited ManagedSignal = "usage_limited"
	// ManagedSignalBudgetLimited is the provider reporting exhaustion of the
	// token budget this runtime sent on a board goal.
	ManagedSignalBudgetLimited ManagedSignal = "budget_limited"
)

// ManagedSignalForCodexGoalStatus maps the Codex app-server thread-goal status
// onto a classifier signal. Unknown statuses map to nothing.
func ManagedSignalForCodexGoalStatus(status string) (ManagedSignal, bool) {
	switch status {
	case "usageLimited":
		return ManagedSignalUsageLimited, true
	case "budgetLimited":
		return ManagedSignalBudgetLimited, true
	default:
		return "", false
	}
}

// ClassifyManaged classifies a managed provider status.
//
// budget_limited is classified as run_budget and stops there: it reports
// exhaustion of a cap this runtime itself sent on one run's goal, so it is not
// proof that the shared subscription is exhausted. Routing it into a group
// record would let one run with a small budget suppress a provider for every
// unrelated spawn on the machine.
//
// This is the only classifier that takes its provider as an argument, which
// makes it the one place where an unsupported provider could otherwise mint
// quota evidence. It is gated: limit detection is scoped to the providers whose
// real rate-limit error shapes were captured, so a provider with no classifier
// yields no limit of any class. That is what makes "no Qwen failure can ever
// suppress anything" a property of the code rather than of the call sites.
func ClassifyManaged(provider string, signal ManagedSignal) Classification {
	if !HasClassifierForRuntime(provider) {
		return Classification{
			Provider: provider,
			Reason:   "provider " + provider + " has no limit classifier; limit detection is scoped to the providers with captured error shapes",
		}
	}
	switch signal {
	case ManagedSignalUsageLimited:
		return Classification{
			Class:    ClassProviderQuota,
			Provider: provider,
			Marker:   "managed usage_limited",
			Reason:   "managed provider status usage_limited",
		}
	case ManagedSignalBudgetLimited:
		return Classification{
			Class:    ClassRunBudget,
			Provider: provider,
			Marker:   "managed budget_limited",
			Reason:   "managed provider status budget_limited reports a runtime-imposed cap, not the subscription",
		}
	default:
		return Classification{Provider: provider, Reason: "managed provider status carries no limit signal"}
	}
}

// --- shared text helpers ----------------------------------------------------

func tailString(buf []byte, limit int) string {
	if len(buf) > limit {
		buf = buf[len(buf)-limit:]
	}
	return string(buf)
}

// tailLines returns the last limit bytes of buf with a leading PARTIAL line
// removed.
//
// A fixed-byte cut can land in the middle of a line, and the remainder of that
// line then looks like a line of its own. An anchored line-start match would
// then accept a marker that was never at a line start — the cut itself would
// have manufactured the anchor. A fragment is not a line, so it is dropped.
// When buf was not truncated its first byte IS a line start and nothing is
// dropped. A truncated window with no line boundary at all yields nothing,
// because none of it is a line.
func tailLines(buf []byte, limit int, headless bool) string {
	if len(buf) <= limit {
		if !headless {
			return string(buf)
		}
		// The caller declared a tail, so byte 0 is not a line start even though
		// this function did not cut anything.
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return ""
		}
		return string(buf[i+1:])
	}
	cut := len(buf) - limit
	if buf[cut-1] == '\n' {
		// The cut fell on a line boundary, so the window's first byte is a real
		// line start. Nothing was made partial and nothing is dropped.
		return string(buf[cut:])
	}
	tail := buf[cut:]
	i := bytes.IndexByte(tail, '\n')
	if i < 0 {
		return ""
	}
	return string(tail[i+1:])
}

// claudeScanWindow is the region of a transcript that may be framed, together
// with whether its first byte is a position whose JSON token state is KNOWN.
type claudeScanWindow struct {
	text string
	// truncated records that bytes before text were dropped, so the window is
	// headless and anything found in it must be framed conservatively.
	truncated bool
	// framable records that text begins at a position this module can scan from
	// at all. Without it, no scan of text means anything.
	framable bool
	// headRead records that the token state at text[0] is KNOWN — either because
	// the bytes before it were available and scanned, or because the caller
	// anchored them. It is false only for a caller-declared tail with no anchor,
	// whose start this process neither saw nor was told about.
	headRead bool
	// start is the complete JSON lexer state at text[0]: the absolute open
	// container stack, string membership, and whether text[0] is an escape
	// payload. It is one value so no scan can be seeded with part of it (F10),
	// and the stack is typed so an enclosing ARRAY is part of it too (F14).
	start tokenState
	// startFollowsRawNewline records that text[0] is the byte immediately after
	// a raw newline in the transcript. Together with startInString it says more
	// than either alone: RFC 8259 forbids an unescaped control character inside a
	// string, so a raw newline found inside one is proof the EMITTER is
	// non-conforming, and nothing about its escaping can be trusted afterwards.
	// An anchored position carries no such implication — a ring buffer may begin
	// anywhere, and a measured string state there is simply a fact to scan from.
	startFollowsRawNewline bool
}

// claudeFrameWindow bounds the scan and re-establishes JSON token framing after
// an arbitrary byte cut.
//
// The cost bound is a byte count, but framing cannot be: a cut at byte
// len-256Ki can land inside a JSON string, and a scanner started there with
// inString=false reads the string's CLOSING quote as an opening one and inverts
// its state for the rest of the window. That is how a complete authoritative
// envelope after the cut became invisible and let prose suppress a group.
//
// The recovery is an invariant rather than a heuristic. RFC 8259 forbids an
// unescaped control character inside a string, so a raw newline byte can never
// occur inside one: the byte immediately after the first raw newline in the
// window is provably OUTSIDE any string. Framing therefore restarts there. The
// window is aligned to that boundary rather than to the byte count, and the
// worst case — a truncated window with no newline in it at all — is reported as
// unframable instead of being scanned from a guessed state.
//
// # The head, and where its starting state comes from
//
// Newline recovery restores STRING state. It cannot restore container nesting,
// and nesting is what decides whether an object is top-level (F8, F9, F14).
// Nesting means BOTH shapes — an object one level up and an array one level up
// bury an envelope equally well — and it is only ever obtained by reading the
// bytes before the window from a state that is itself known, which is possible in
// exactly two situations:
//
//   - the buffer is intact, so its byte 0 is absolute depth zero and outside
//     every string by construction; or
//   - the caller declared a tail AND anchored it, so byte 0's state is the
//     measurement it supplies (see TailAnchor).
//
// In both cases claudeScanHeadFrom walks the dropped bytes and the result is
// exact. In neither case is anything argued from the window's own bytes: a
// declared tail with no anchor is framable but has no known nesting, and parsing
// refuses it outright rather than guessing.
//
// The cost is one non-allocating byte loop over the bytes the window drops, and
// none at all in production, where the caller keeps a ring buffer no larger than
// ClassifyScanBytes and there is nothing before the window to walk.
func claudeFrameWindow(stdout []byte, headless bool, anchor *TailAnchor) claudeScanWindow {
	// An intact buffer carries its own anchor: byte 0 of a whole transcript is
	// absolute depth zero and outside every string by construction. A declared
	// tail has one only if the caller measured it.
	if !headless {
		anchor = &TailAnchor{}
	} else if !anchor.usable() {
		anchor = nil
	}
	if len(stdout) <= ClassifyScanBytes {
		if anchor != nil {
			// Nothing was cut here, so the window's first byte IS the anchored
			// byte and its token state needs no recovery — including its escape
			// bit, which is the caller's measurement of the byte it dropped.
			return claudeScanWindow{
				text:      string(stdout),
				truncated: headless,
				framable:  true,
				headRead:  true,
				start:     anchor.state(),
			}
		}
		// An unanchored tail. Nothing was cut here, but the buffer is still
		// headless, so it gets the same recovery as one this function cut.
		i := bytes.IndexByte(stdout, '\n')
		if i < 0 {
			return claudeScanWindow{text: string(stdout), truncated: true}
		}
		return claudeScanWindow{text: string(stdout[i+1:]), truncated: true, framable: true}
	}
	cut := len(stdout) - ClassifyScanBytes
	start := cut
	if stdout[cut-1] != '\n' {
		// The cut fell mid-line, so the window gives up that partial line and
		// begins after the first raw newline instead.
		i := bytes.IndexByte(stdout[cut:], '\n')
		if i < 0 {
			return claudeScanWindow{text: string(stdout[cut:]), truncated: true}
		}
		start = cut + i + 1
	}
	if anchor == nil {
		return claudeScanWindow{text: string(stdout[start:]), truncated: true, framable: true}
	}
	return claudeScanWindow{
		text:                   string(stdout[start:]),
		truncated:              true,
		framable:               true,
		headRead:               true,
		start:                  claudeScanHeadFrom(stdout[:start], anchor.state()),
		startFollowsRawNewline: start > 0 && stdout[start-1] == '\n',
	}
}

// claudeScanHeadFrom returns the complete JSON lexer state at the END of head —
// that is, at the first byte of the window that follows it — given the state at
// head[0].
//
// The starting state is a parameter rather than a constant because the head is
// not always the start of the transcript: for an anchored tail it begins at the
// caller's measured position instead. Assuming a zero state there is the same
// mistake as assuming it for the window itself, and that includes the escape
// bit: head[0] may itself be an escape payload (F10).
//
// Containers are tracked by KIND, not counted (F14): a `[` in the head opens a
// level exactly as a `{` does, and a head that ends inside an array must not hand
// the window a top-level start merely because its braces balanced.
//
// An unmatched closer is ignored rather than applied: a `}` at an empty stack, or
// one that does not match the innermost open container, is not structure — it is
// noise in a transcript that also carries plain text. Ignoring it is what makes a
// stray brace in crash output harmless rather than a phantom close.
func claudeScanHeadFrom(head []byte, at tokenState) tokenState {
	sc := newTokenScanner(at, quotingTrusted)
	for _, c := range head {
		sc.step(c)
	}
	return sc.state()
}

// normalizeApostrophes folds the typographic apostrophes a vendor may emit onto
// the ASCII one the markers are written with.
func normalizeApostrophes(s string) string {
	if !strings.ContainsAny(s, "‘’ʼʹ′") {
		return s
	}
	replacer := strings.NewReplacer(
		"‘", "'",
		"’", "'",
		"ʼ", "'",
		"ʹ", "'",
		"′", "'",
	)
	return replacer.Replace(s)
}

func truncate(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

func lastLineContaining(text, folded string) string {
	lines := strings.Split(normalizeApostrophes(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(lines[i]), folded) {
			return lines[i]
		}
	}
	return ""
}

// claudeParseKind is the outcome of looking for the authoritative envelope.
//
// Seven outcomes, not two. "No JSON at all, in a window we could actually read
// END TO END" is the only one that may reach the transcript fallback, so every
// other outcome has to be distinguishable from it. Collapsing them into "did not
// parse" is one defect with many faces, and this module has now met all of them:
// a malformed status read as an absent one, an unreadable owner read as a live
// one, a truncated trailing write read as nothing-parsed, a window cut mid-string
// read as no-envelope, and prose containing one stray quote read as a JSON string
// that swallowed the envelope after it. Each one ended the same way — prose
// driving a suppression an authoritative envelope contradicted. The rule that
// covers all of them: NOT READ is never the same as NOT THERE.
type claudeParseKind int

const (
	// claudeParseNone is no complete top-level JSON object in a window that was
	// framable AND scanned cleanly from end to end. This is the only
	// fallback-eligible outcome.
	claudeParseNone claudeParseKind = iota
	// claudeParseUnframable is a truncated window with no position whose JSON
	// token state is known, so nothing in it can be read as structure — and its
	// prose cannot be read as evidence either.
	claudeParseUnframable
	// claudeParseUnprovenTopLevel is a truncated window whose token state IS
	// known but whose container nesting does not prove where top level is, so an
	// object closing at scanner depth zero may be a NESTED one. Not
	// fallback-eligible: see claudeTopLevelVerdict.
	claudeParseUnprovenTopLevel
	// claudeParseTruncatedValue is a window that ends inside a top-level JSON
	// value the writer never finished. Not fallback-eligible: the value's own
	// authoritative fields were not read, and in this envelope they precede the
	// prose (§1.4).
	claudeParseTruncatedValue
	// claudeParseMalformedValue is a top-level JSON value that opened and then
	// failed to parse. Not fallback-eligible: everything after it may be its
	// interior, so nothing later in the window can be placed.
	claudeParseMalformedValue
	// claudeParseUnplaceableObject is an object beginning after prose on the same
	// line. Not fallback-eligible: the window holds an object this scan will
	// neither read as an envelope nor pretend is absent.
	claudeParseUnplaceableObject
	// claudeParseUnclosedContainer is a prose region that leaves a square bracket
	// open. Not fallback-eligible for the same reason and by a different route:
	// every whole-line object after it is one level deep, so the scan cannot place
	// what follows and will not step over the opener to pretend otherwise.
	claudeParseUnclosedContainer
	// claudeParseNonResult is a complete JSON object that is not a result
	// envelope (an assistant/system/stream line, say).
	claudeParseNonResult
	// claudeParseUnreadableResult is a complete result envelope whose fields did
	// not decode into claudeEnvelope.
	claudeParseUnreadableResult
	// claudeParseResult is a usable result envelope.
	claudeParseResult
)

// parseClaudeEnvelope finds the last COMPLETE top-level JSON object in the
// framable part of the window and reports what it was.
//
// Top level is MEASURED or it is not established. There is no inferred arm: a
// window whose start position this process neither read nor was told about has
// no absolute depth, and no argument from its own bytes can supply one (F9).
func parseClaudeEnvelope(win claudeScanWindow) (claudeEnvelope, claudeParseKind, claudeTopLevelVerdict) {
	if !win.framable {
		return claudeEnvelope{}, claudeParseUnframable, claudeTopLevelProven
	}
	if !win.headRead {
		// A declared tail with no anchor. Its relative brace balance is not an
		// argument about absolute depth, because the transcript of a KILLED child
		// does not end outside every object — which is the only premise that
		// could have turned balance into a proof. See TailAnchor.
		return claudeEnvelope{}, claudeParseUnprovenTopLevel, claudeTopLevelUnanchoredTail
	}
	if win.start.inString && win.startFollowsRawNewline {
		// A raw newline inside a JSON string cannot be produced by a conforming
		// emitter, so this is not merely "the window starts inside a string" —
		// it is proof that the escaping in this transcript means nothing, and a
		// scan of it would be reading provider content as structure.
		return claudeEnvelope{}, claudeParseUnprovenTopLevel, claudeTopLevelHeadShowsInString
	}
	// The window began inside an enclosing container or string when its measured
	// start says so. "No top-level object here" is then not the same statement as
	// "no envelope was written", and prose there is provider CONTENT rather than
	// provider error output, so the fallback is refused on both branches below.
	startedNested := win.start.nested()
	nestedVerdict := claudeTopLevelHeadShowsNested
	if win.start.containers.empty() {
		nestedVerdict = claudeTopLevelHeadShowsInString
	}

	scan := scanClaudeStream(win.text, win.start)
	switch scan.outcome {
	case streamNeverTopLevel:
		return claudeEnvelope{}, claudeParseUnprovenTopLevel, nestedVerdict
	case streamMalformedValue:
		return claudeEnvelope{}, claudeParseMalformedValue, claudeTopLevelProven
	case streamUnplaceableObject:
		return claudeEnvelope{}, claudeParseUnplaceableObject, claudeTopLevelProven
	case streamUnclosedContainer:
		return claudeEnvelope{}, claudeParseUnclosedContainer, claudeTopLevelProven
	}
	if !scan.found {
		if scan.outcome == streamTruncatedValue {
			return claudeEnvelope{}, claudeParseTruncatedValue, claudeTopLevelProven
		}
		if startedNested {
			return claudeEnvelope{}, claudeParseUnprovenTopLevel, nestedVerdict
		}
		return claudeEnvelope{}, claudeParseNone, claudeTopLevelProven
	}
	// scan.object came out of encoding/json, so it is valid JSON by construction
	// and the only remaining questions are about its SHAPE. A `type` that is
	// absent, not a string, or not "result" is an object of another kind.
	span := scan.object
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal([]byte(span), &probe); err != nil || decodeJSONString(probe.Type) != "result" {
		return claudeEnvelope{}, claudeParseNonResult, claudeTopLevelProven
	}
	var envelope claudeEnvelope
	if err := json.Unmarshal([]byte(span), &envelope); err != nil {
		return claudeEnvelope{}, claudeParseUnreadableResult, claudeTopLevelProven
	}
	return envelope, claudeParseResult, claudeTopLevelProven
}

// claudeTopLevelVerdict says why an object closing at scanner depth zero could
// not be taken for a TOP-LEVEL object.
//
// This is a distinct question from framing. A raw newline proves the scanner is
// outside every JSON string (claudeFrameWindow); it proves nothing about brace
// depth. A pretty-printed transcript can put a nested object on whole lines of
// its own, so line occupancy cannot answer it either — the cycle-5 reproduction
// is one valid outer assistant object whose nested whole-line child is shaped
// exactly like a 429 result envelope. §4.1 asks for the last complete TOP-LEVEL
// object; a nested one is not a smaller version of that answer, it is a
// different answer.
//
// Every remaining verdict is MEASURED. The two inferred ones this type used to
// carry — "it closes a brace it never opened" and "it ends inside an unclosed
// object" — were the visible half of an argument whose other half was unsound
// (F9), so they are gone with it: an unanchored tail is refused before any of
// its bytes are weighed.
type claudeTopLevelVerdict int

const (
	// claudeTopLevelProven means scanner depth zero is absolute depth zero.
	claudeTopLevelProven claudeTopLevelVerdict = iota
	// claudeTopLevelUnanchoredTail means the window is a caller-declared tail
	// with no TailAnchor, so no position in it has a known absolute depth.
	claudeTopLevelUnanchoredTail
	// claudeTopLevelHeadShowsNested means the window's start was measured and it
	// lies inside an enclosing container that never closes inside the window.
	claudeTopLevelHeadShowsNested
	// claudeTopLevelHeadShowsInString means the window's start was measured and
	// it lies inside a JSON string, so its "structure" is provider content — a
	// non-conforming emitter's raw control character, not framing.
	claudeTopLevelHeadShowsInString
)

func (v claudeTopLevelVerdict) describe() string {
	switch v {
	case claudeTopLevelUnanchoredTail:
		return "it is a declared tail with no anchor, so no position in it has a known absolute depth"
	case claudeTopLevelHeadShowsNested:
		return "the transcript's own head puts its first byte inside an enclosing container"
	case claudeTopLevelHeadShowsInString:
		return "the transcript's own head puts its first byte inside a JSON string"
	default:
		return "top level is proven"
	}
}

// claudeStreamOutcome says how much of the window the scan could account for.
//
// It exists because "did the scan find an envelope" and "did the scan understand
// the window" are different questions, and the second one is the gate on the
// transcript fallback (F11). A scan that gave up half way through has not shown
// that no envelope was written; it has shown that it stopped reading.
type claudeStreamOutcome int

const (
	// streamClean means every byte of the window was accounted for: each region
	// was either a complete JSON value decoded at a position of known absolute
	// depth, or a region this scan proved it never entered.
	streamClean claudeStreamOutcome = iota
	// streamTruncatedValue means a top-level value opened and the window ended
	// inside it. Values completed BEFORE it stand — nothing can follow the end of
	// the buffer, so the last one really is the last (F6).
	streamTruncatedValue
	// streamMalformedValue means a top-level value opened and then failed to
	// parse. Everything after it may be its interior, so no span found after it
	// can be placed, and the spans found before it can no longer be called the
	// LAST ones. Nothing is returned.
	streamMalformedValue
	// streamUnplaceableObject means an object begins after prose on the same
	// line. Emitting an envelope and narrating one produce the same bytes there,
	// so it is neither read as structure nor stepped over silently.
	streamUnplaceableObject
	// streamUnclosedContainer means a prose region ended with a square bracket
	// still open. The scan stops rather than resume inside an array it never read.
	streamUnclosedContainer
	// streamNeverTopLevel means the window began nested or inside a string and
	// the enclosing structure never closed inside it.
	streamNeverTopLevel
)

// claudeStreamScan is one pass of scanClaudeStream.
type claudeStreamScan struct {
	// object is the source of the last complete top-level JSON OBJECT. It is
	// meaningful only when found is true.
	object  string
	found   bool
	outcome claudeStreamOutcome
}

// scanClaudeStream reads the window as what it actually is — a MIXED stream of
// top-level JSON values and free prose — and returns the last complete top-level
// JSON object in it, together with how much of the window it could account for.
//
// # Why not a brace-and-quote scan (F11)
//
// The window was scanned as if it were pure JSON, with every `"` toggling string
// state. Prose is not JSON: one unmatched quote in a crash line — `unmatched
// quote: "` — put that scan inside a phantom string, and it stayed there for the
// rest of the window, so the complete `401` envelope on the NEXT LINE was never
// seen and limit prose inside that same envelope drove a suppression the envelope
// itself contradicted. No amount of extra lexer state fixes that, because the
// input is not a JSON document and no JSON lexer can be right about it.
//
// So values are parsed by encoding/json, at positions whose absolute depth is
// known, and prose is SKIPPED rather than lexed. A stray quote, brace or
// backslash in prose is never given structural meaning, so it can no longer
// poison anything beyond its own line.
//
// # The one framing assumption, stated
//
// A top-level JSON value begins at the first non-whitespace byte of a line, or
// immediately after a value that ended on the same line. Everything else is
// prose, and prose changes no JSON state. Two invariants keep the assumption
// honest, and both are CHECKED rather than assumed:
//
//   - RFC 8259 forbids a raw newline inside a string, so resuming at the byte
//     after a newline is provably outside every string.
//   - A region is stepped over only when nothing in it can OPEN structure: no
//     byte of it can begin a JSON object (claudeObjectBegins, evaluated across
//     the region's end so a pretty-printed opener is not hidden by the line
//     boundary) and it leaves no square bracket unclosed. A region that could
//     have opened structure stops the scan instead, so no resumption point is
//     ever inside a container the scan did not read.
//
// # Absolute depth, not relative balance (F8, F9)
//
// start is a MEASUREMENT, so the scan knows where top level is instead of
// arguing for it. When the window begins nested or inside a string the enclosing
// structure is walked byte-wise first — inside a JSON value the bytes ARE JSON,
// so lexing them is exact — and value parsing begins only once absolute depth
// returns to zero. A window whose enclosing structure never closes yields
// streamNeverTopLevel and no span, which is the F8/F9 refusal.
//
// The escape bit of the start state is honoured by that walk for the same reason
// depth is (F10): a cut immediately after an unescaped backslash makes text[0] an
// escape payload, and a walk seeded without it reads the escaped quote as the
// string's terminator.
//
// An unmatched closer is ignored in the walk: a closing byte that closes nothing
// open is not structure, it is noise in a transcript that also carries plain text.
func scanClaudeStream(text string, start tokenState) claudeStreamScan {
	pos, ok := claudeReachTopLevel(text, start)
	if !ok {
		return claudeStreamScan{outcome: streamNeverTopLevel}
	}
	scan := claudeStreamScan{outcome: streamClean}
	for pos < len(text) {
		at := skipJSONSpace(text, pos)
		if at >= len(text) {
			return scan
		}
		if claudeObjectBegins(text, at) {
			decoder := json.NewDecoder(strings.NewReader(text[at:]))
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				// Structure OPENED here, so the scan may not step over it, and what
				// decides whether the spans already found are still worth anything
				// is whether anything follows AT ALL — not which error the decoder
				// chose. A killed write reports unexpected EOF when the buffer stops
				// mid-token and a syntax error when it stops one byte later, on the
				// log's own trailing newline inside the unterminated string; those
				// two are the same event.
				if skipJSONSpace(text, claudeDecodeFailure(err, at, len(text))) >= len(text) {
					// Nothing but whitespace after the failure, so no later
					// top-level object can exist and the last complete one before
					// it really is the last one (F6).
					scan.outcome = streamTruncatedValue
					return scan
				}
				// Content follows an object that did not parse, and it may be that
				// object's interior — a raw control character inside a string is
				// exactly how envelope-shaped provider CONTENT gets out. An earlier
				// span can no longer be called the LAST top-level object, so
				// nothing is returned.
				return claudeStreamScan{outcome: streamMalformedValue}
			}
			scan.object, scan.found = string(value), true
			end := at + int(decoder.InputOffset())
			if end <= at {
				// The decoder reports the offset just past the value it read, so
				// this cannot happen for a value of at least one byte. Guarding it
				// keeps a decoder change from turning into a hang.
				end = at + 1
			}
			pos = end
			continue
		}
		// Not an object start, so nothing is opened here whatever these bytes are.
		// The rest of the line is prose and is stepped over — but only after
		// checking that it hides no object of its own. An object that begins after
		// prose on the same line cannot be placed: a transcript line that NARRATES
		// an envelope and a line that emits one are the same bytes, and this module
		// already refuses to let narrated text decide (§4.1, the Codex line-start
		// anchor). Skipping such a line silently would leave its prose to the
		// fallback with the envelope it quotes unread.
		lineEnd := claudeLineEnd(text, at)
		switch claudeRegionHidesStructure(text, at, lineEnd) {
		case regionOpensObject:
			return claudeStreamScan{outcome: streamUnplaceableObject}
		case regionOpensContainer:
			return claudeStreamScan{outcome: streamUnclosedContainer}
		}
		if lineEnd >= len(text) {
			return scan
		}
		// Resume after the newline, which RFC 8259 puts outside every string.
		pos = lineEnd + 1
	}
	return scan
}

// claudeObjectBegins reports whether a JSON object can begin at text[at].
//
// It is a syntactic fact rather than a guess: an object is `{`, optional
// whitespace, then either `}` or the opening quote of its first key. A `{`
// followed by anything else — `panic: {not json at all}` — is not an object
// under any reading, so no emitter wrote one there and the scan may step over it.
func claudeObjectBegins(text string, at int) bool {
	if at >= len(text) || text[at] != '{' {
		return false
	}
	next := skipJSONSpace(text, at+1)
	if next >= len(text) {
		return false
	}
	return text[next] == '"' || text[next] == '}'
}

// claudeRegionHidesStructure reports whether text[from:to] — a region the scan is
// about to step over — could have opened a container the scan would then be
// inside without knowing it.
//
// Two ways it can. An OBJECT may begin anywhere in it, which is the narrated
// envelope. Or a SQUARE BRACKET may be left open, and an array opened on a prose
// line puts every whole-line object after it one level deep — the F8 shape by a
// different route.
//
// # It is the same scanner (F8, F12, F14, F15)
//
// The containment question here is the SAME question the measured paths ask, so
// it is answered by the same tokenScanner reading the same tokenState. This
// function contributes no notion of structure of its own: order, the zero clamp
// on unmatched closers, container KIND, and escape handling all arrive from the
// one scanner. Every defect in this family came from a private copy of that walk
// drifting from it, so the copy is gone.
//
// # Two readings, one scanner
//
// What a prose region genuinely needs is different INPUT handling, not different
// structure. Its quoting cannot be TRUSTED — an unmatched quote in narration must
// not swallow a real opener, which is why F11 stopped believing prose quotes. But
// ignoring quotes does not merely distrust them, it asserts the opposite: that no
// `]` is ever string CONTENT. That fails toward admission. In `["]",` the array is
// genuinely open and its only `]` sits inside a JSON string, so a quote-ignoring
// walk cancels a real opener against a byte that closes nothing, calls the region
// inert, and lets the scan read the next line's array ELEMENT as a top-level
// envelope (F15).
//
// So neither reading is authoritative, and the region is scanned twice — once
// with quotingIgnored, once with quotingTrusted — and refused if EITHER ends with
// a container open. Both runs are the same scanner under its two declared input
// modes:
//
//   - opener seen only when quotes are ignored (`] [`, or a real `[` after an
//     unmatched prose quote) — quotingTrusted may believe it is inside a string;
//     quotingIgnored refuses. F11/F12 preserved.
//   - opener seen only when quotes are trusted (`["]",`) — quotingIgnored cancels
//     it against string content; quotingTrusted refuses. F15 closed.
//   - closed under both (`[error]`, `] closing noise`, `he said "]" then [x]`) —
//     inert, so a genuine later envelope still decides.
//
// The union can only ADD refusals, which is the only direction a gate guarding a
// shared-subscription suppression may fail in. A refusal costs a transcript
// nothing it was entitled to: a top-level array has no top-level result envelope
// in it either way.
//
// It reads containersClosed rather than atTopLevel because the region ends at a
// newline the scan resumes after, and RFC 8259 puts a newline outside every
// string: narration that leaves a quote open does not enclose the next line, but
// a container it opened would.
//
// claudeObjectBegins is a separate and independent test, and not a second notion
// of structure: it asks whether an EMITTER WROTE A VALUE here, which the container
// walk cannot answer because a narrated envelope like `saw {"type":"result"} once`
// is perfectly balanced. It is evaluated against the WHOLE text so its one-token
// lookahead crosses the region's end — a `{` ending a prose line whose first key
// sits on the next line is an object opener, and slicing the region first would
// hide exactly that — and it stays quote-blind for the same reason quotingIgnored
// exists: a narrated envelope inside untrusted quotes is still a region this scan
// must not step over.
func claudeRegionHidesStructure(text string, from, to int) regionRisk {
	for i := from; i < to; i++ {
		if claudeObjectBegins(text, i) {
			return regionOpensObject
		}
	}
	for _, q := range [...]quoting{quotingIgnored, quotingTrusted} {
		sc := newTokenScanner(tokenState{}, q)
		for i := from; i < to; i++ {
			sc.step(regionByte(text[i]))
		}
		if !sc.containersClosed() {
			return regionOpensContainer
		}
	}
	return regionInert
}

// regionByte maps one prose-region byte to the byte the scanner should see. It is
// input handling, like quoting, and not a structure rule: the scanner's container
// walk is unchanged and still the only thing deciding nesting.
//
// Braces are neutralised because the object test above has ALREADY decided every
// brace in this region. It returned regionOpensObject for each `{` that could open
// one, so every brace still reaching here is provably not a JSON object opener —
// JSON requires `{` to be followed by `"` or `}` — and a byte that opens nothing
// must not push a level. `panic: {not json at all` costs the envelope behind it
// nothing, which is the narrowing this module has always had.
//
// Brackets carry no such syntactic tell: `[` may be followed by any value, so
// nothing in the region can prove a given `[` opened nothing. They therefore stay
// structural, and that asymmetry is exactly why every attack in this family — F8,
// F12, F14, F15 — was array-shaped.
func regionByte(c byte) byte {
	if c == '{' || c == '}' {
		return ' '
	}
	return c
}

// regionRisk is what a prose region about to be stepped over could have opened.
// The two unsafe answers are kept apart so the refusal a caller reports names the
// shape it actually saw rather than the other one.
type regionRisk int

const (
	// regionInert opens nothing: the scan may resume after it.
	regionInert regionRisk = iota
	// regionOpensObject means a JSON object can begin inside the region.
	regionOpensObject
	// regionOpensContainer means the region ends with a square bracket still open.
	regionOpensContainer
)

// claudeLineEnd returns the index of the newline at or after pos, or len(text)
// when the region runs to the end of the window.
func claudeLineEnd(text string, pos int) int {
	if i := strings.IndexByte(text[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(text)
}

// claudeDecodeFailure returns the index in the window just past the byte the
// decoder rejected, for a value that began at valueStart.
//
// A syntax error carries that offset. Every other failure — unexpected EOF above
// all — means the decoder consumed the rest of the input, so the failure point is
// the end of the window.
func claudeDecodeFailure(err error, valueStart, windowEnd int) int {
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		return windowEnd
	}
	at := valueStart + int(syntax.Offset)
	if at < valueStart {
		return valueStart
	}
	if at > windowEnd {
		return windowEnd
	}
	return at
}

// skipJSONSpace returns the index of the first byte at or after pos that JSON
// does not treat as whitespace.
func skipJSONSpace(text string, pos int) int {
	for pos < len(text) {
		switch text[pos] {
		case ' ', '\t', '\r', '\n':
			pos++
		default:
			return pos
		}
	}
	return len(text)
}

// claudeReachTopLevel walks the enclosing structure a measured start places the
// window inside, and returns the index of the first byte at absolute depth zero
// and outside every string.
//
// The walk is a byte scan because that is what a position inside a JSON value
// requires: there is no value boundary to hand a decoder, and the bytes there are
// JSON rather than prose, so lexing them is exact. A window already at top level
// needs no walk and returns index zero.
func claudeReachTopLevel(text string, at tokenState) (int, bool) {
	if at.atTopLevel() {
		return 0, true
	}
	sc := newTokenScanner(at, quotingTrusted)
	for i := 0; i < len(text); i++ {
		sc.step(text[i])
		if sc.atTopLevel() {
			return i + 1, true
		}
	}
	return 0, false
}

func decodeJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// apiErrorStatusForm is the shape of the envelope's api_error_status field.
//
// Four forms, not two. Conflating "the field is unreadable" with "the field is
// not there" is what let a malformed authoritative discriminator hand the
// decision to limit prose: usability and meaning are different questions, and
// only absence is an answer to the second one.
type apiErrorStatusForm int

const (
	// statusAbsent is the key not being in the object at all.
	statusAbsent apiErrorStatusForm = iota
	// statusNull is the key present with an explicit JSON null — the observed
	// client-exhausted-its-retries shape.
	statusNull
	// statusInteger is a usable status: it alone decides.
	statusInteger
	// statusMalformed is present, non-null, and not an integer. It decides
	// nothing and permits nothing.
	statusMalformed
)

func (f apiErrorStatusForm) describe() string {
	switch f {
	case statusNull:
		return "null"
	case statusInteger:
		return "integer"
	case statusMalformed:
		return "present-but-unreadable"
	default:
		return "absent"
	}
}

// decodeAPIErrorStatus reports the integer status and which of the four forms
// the raw field carried. The integer is meaningful only for statusInteger.
func decodeAPIErrorStatus(raw json.RawMessage) (int, apiErrorStatusForm) {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "":
		return 0, statusAbsent
	case "null":
		return 0, statusNull
	}
	var status int
	if err := json.Unmarshal(raw, &status); err != nil {
		return 0, statusMalformed
	}
	return status, statusInteger
}

// rawStatusText renders the unreadable field for the operator-facing reason,
// with whitespace collapsed so a multi-line value cannot break one log line.
func rawStatusText(raw json.RawMessage) string {
	return strings.Join(strings.Fields(string(raw)), " ")
}

var (
	ordinalSuffix   = regexp.MustCompile(`(?i)\b(\d{1,2})(st|nd|rd|th)\b`)
	relativeAmount  = regexp.MustCompile(`(?i)^(\d+)\s*(second|sec|minute|min|hour|hr|day)s?$`)
	clockOfDay      = regexp.MustCompile(`(?i)^(\d{1,2})(?::(\d{2}))?\s*(am|pm)$`)
	clock24OfDay    = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	absoluteLayouts = []string{
		"Jan 2, 2006 3:04 PM",
		"Jan 2, 2006 3:04PM",
		"January 2, 2006 3:04 PM",
		"January 2, 2006 3:04PM",
		"Jan 2, 2006 15:04",
		"January 2, 2006 15:04",
		"Jan 2 2006 3:04 PM",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05Z07:00",
	}
)

// parseAbsoluteInstant parses the vendor's absolute date prose. Failure is
// expected and harmless: classification never depends on parsing a human date.
func parseAbsoluteInstant(body string, now time.Time) *time.Time {
	candidate := ordinalSuffix.ReplaceAllString(body, "$1")
	candidate = strings.TrimSpace(candidate)
	for _, layout := range absoluteLayouts {
		if parsed, err := time.ParseInLocation(layout, candidate, now.Location()); err == nil {
			return &parsed
		}
	}
	return nil
}

// parseRelativeInstant parses "90 seconds", "2 hours" and friends against now.
func parseRelativeInstant(body string, now time.Time) *time.Time {
	match := relativeAmount.FindStringSubmatch(strings.TrimSpace(body))
	if match == nil {
		return nil
	}
	amount, err := strconv.Atoi(match[1])
	if err != nil || amount < 0 {
		return nil
	}
	var unit time.Duration
	switch strings.ToLower(match[2]) {
	case "second", "sec":
		unit = time.Second
	case "minute", "min":
		unit = time.Minute
	case "hour", "hr":
		unit = time.Hour
	case "day":
		unit = 24 * time.Hour
	default:
		return nil
	}
	parsed := now.Add(time.Duration(amount) * unit)
	return &parsed
}

// parseClockInstant resolves a bare time of day ("10:30am") to its next
// occurrence after now, in now's location.
func parseClockInstant(body string, now time.Time) *time.Time {
	body = strings.TrimSpace(body)
	hour, minute := -1, 0
	if match := clockOfDay.FindStringSubmatch(body); match != nil {
		h, err := strconv.Atoi(match[1])
		if err != nil || h < 1 || h > 12 {
			return nil
		}
		if match[2] != "" {
			m, err := strconv.Atoi(match[2])
			if err != nil || m > 59 {
				return nil
			}
			minute = m
		}
		hour = h % 12
		if strings.EqualFold(match[3], "pm") {
			hour += 12
		}
	} else if match := clock24OfDay.FindStringSubmatch(body); match != nil {
		h, err := strconv.Atoi(match[1])
		if err != nil || h > 23 {
			return nil
		}
		m, err := strconv.Atoi(match[2])
		if err != nil || m > 59 {
			return nil
		}
		hour, minute = h, m
	}
	if hour < 0 {
		return nil
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return &candidate
}
