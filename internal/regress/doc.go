// Package regress is this module's landing-gate regression net.
//
// It holds no production code and is linked into no binary. What it holds is
// one fast, cross-cutting check per class of failure this repository has
// already decided it cannot afford to reintroduce, each driven through the
// same entry point a caller would use and each paired with a demonstration
// that the check bites.
//
// # Why a separate package rather than more tests in the packages themselves
//
// The per-package suites are deep: they prove one port against its own
// fixtures, mutant by mutant, and they take as long as that deserves. This
// one is the opposite shape. It crosses the layers — a Layer-2 registration
// refusing on a Layer-1 fact, a runtime declaration, a limit-state file
// projected as a vendor verdict, a plan built through the registry — and it
// is kept small enough to sit in front of every landing without taxing one.
//
// The five classes, and the incident behind each:
//
//   - REGISTRATION. A vendor plugin naming an agentic system nobody
//     registered must be refused with BOTH ids in the message. Admitting it
//     leaves a model that resolves to a harness the binary does not carry,
//     and the failure then surfaces at launch, far from the declaration that
//     caused it.
//   - DECLARATION. A runtime is a declared (system x vendor) pair, and the
//     collision policy is F2: a matching redeclaration is idempotent, a
//     conflicting one is refused and the FIRST declaration stands. The ids
//     feed admitted-pair digests and limit-state filenames, so a silent
//     rebind orphans state.
//   - AVAILABILITY. The limit plane fails open: a state file nobody can find
//     reads as "provider healthy" with no error anywhere. So the verdict has
//     to be derived from bytes a different binary wrote, and the moved-key
//     disaster has to be demonstrated rather than described.
//   - PARITY. One golden per Layer-1 system, rebuilt through the real
//     registry and agentic.BuildPlan. It is a smoke, not the acceptance —
//     each plugin's own parity file remains that — and its job is to catch a
//     core change that breaks every system at once.
//   - MODEL FACTS. The rows live in TWO homes since v0.2.0 — the vendor
//     plugins, and the vendor-unresolved runtime declaration that carries the
//     rows no vendor owns — so a check walking only the registered vendors
//     reports green on 41 of 43 rows. And every fact those rows gained is
//     display and migration evidence: the day a lifecycle, a recommendation, a
//     score or a price reaches an admitted-pair digest, a truthful correction
//     silently changes who may spawn, which is the regression the extraction
//     source spent a task removing from its own ordered ceilings.
//
// Every test here is in a _test.go file; this file exists so the directory is
// an ordinary package for `go build ./...` and so the reason the package
// exists is written down next to it rather than in a Makefile comment.
package regress
