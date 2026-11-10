# Explicit-only unmeasured ranking claims

Decision: 2026-11-10, Curator orchestrator, TASK-261110-dtkvqm.

Reason: a successful availability probe establishes that an exact model can
answer, but supplies no benchmark score or ordering evidence. Assigning a
number or copying a predecessor's score would invent a capability claim and
could change that predecessor's Tied flag.

A model declares exactly one of these alternatives:

- `Rank: *CapabilityRank`: the existing positive score and evidence contract.
- `Unmeasured: *UnmeasuredClaim`: `Kind == "unmeasured"`, ISO date `AsOf`,
  attributed `Source`, and nonblank `Observation`; `Rank` must be absent.

Absence is represented by nil, never score 0. Any rank alongside an unmeasured
claim refuses as `ErrRankInvalid`, even if its score is zero. A missing claim,
an unknown claim kind, a malformed date or missing attribution also refuses.
This changes `Model.Rank` and `benchdata.Model.Rank` to pointers. Existing ranked
rows retain their values and JSON bytes. Consumers accessing raw rows must
check Rank before dereferencing it. Both alternatives are deep-copied.

`Lineup` excludes unmeasured models before ordering, tie counting and numbering.
A lineup containing only unmeasured rows is empty. Consumers using Lineup for
Complete, preference order or top/default choice therefore never choose them.
This module has no Complete command and does not inject a default model into
BuildLaunch: the caller supplies the exact model id. Unmeasured models cannot
be aliases, alias targets or display recommendations.

An explicit unmeasured request with omitted effort uses its declared recommended
effort. Supplied effort is validated normally, as are system membership and
transport. Existing ranked models retain their required-effort refusal. The
unmeasured availability claim does not become a BugHuntRows observation; its
model and effort vocabulary do appear in RegistryFacts.

The first row is `claude-fable-5-2`, driven only by `claude-code`, with declared
low/medium/high/xhigh/max and recommended high. The vocabulary mirrors 5.1 as a
property, with no assertion about equal capability. Its availability evidence
is attributed to the Curator orchestrator on ivmbp, 2026-11-10 approximately
00:05Z: `claude -p --model claude-fable-5-2 "reply ok"` returned `ok`, exit 0.
The producer did not repeat that probe or infer benchmark, price or context data.

Validation uses production BuildLaunch plans, malformed claim counterexamples,
pi and invalid-effort refusals, and byte comparisons against all old model rows
and lineups captured before implementation at main
`25a9f97d84a6fb709b1c9a34dd623bc31c446751`. The two frozen files are
`pkg/vendorplugin/testdata/pre-unmeasured-rows.json` and
`pkg/vendorplugin/testdata/pre-unmeasured-lineups.json`; the latter includes
positions and Tied. Historical capture fixtures are not rewritten.
