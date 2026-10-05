package agentic

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Imported process verification for hosted launch consumers.
//
// The D4 Phase 1 daemon plan (§5, "Verifier plumbing") imports the exec guard
// with ImportSeal, binds the exact final process the launch plan carries, and
// re-verifies both immediately before every start. ImportSeal returns an
// ExecPlanVerifier while the plan verifier stays private, so the daemon needs
// a module-owned wrapper: NewImportedProcess binds the exact final process
// (absolute binary, ordered argv, full ordered env, cwd, home, stdin) to the
// imported verifier, and ImportedProcess.VerifyBeforeExec refuses on any
// drift of those six facts before delegating to the imported verifier for
// its own checks (catalog swap, binary hash, artifact digests).
//
// The module still owns no exec: the wrapper compares plans and re-reads
// sealed artifacts, and the consumer starts its own process. There is no
// os/exec anywhere under pkg/agentic; exec_boundary_test.go holds that line.
var (
	// ErrImportedVerifierMissing refuses an imported process with no
	// verifier: a sealed importer that answered (nil, nil), or a zero
	// ImportedProcess that never went through NewImportedProcess.
	ErrImportedVerifierMissing = errors.New("agentic: imported seal yielded no verifier")
	// ErrImportedVerifierMismatch refuses a verifier bound to a different
	// process than the one being imported: a process naming another
	// system, or a sealed or finalized binding whose binary or argv
	// differs from the given process. The binding is read from the seal
	// itself, never from an optional verifier capability.
	ErrImportedVerifierMismatch = errors.New("agentic: verifier binds a different process")
	// ErrImportedProcessMalformed refuses an exact final process whose
	// shape cannot represent the contract's final process: a binary, cwd
	// or home that is not absolute, argv carrying argv[0], an env entry
	// without NAME=value shape, with an invalid name, with a NUL byte, or
	// naming a duplicate, or a stdin payload outside the null/empty/bytes
	// representation.
	ErrImportedProcessMalformed = errors.New("agentic: imported process shape is invalid")
	// ErrImportedProcessChanged refuses a process that differs from the
	// imported one: another binary, other argv, a changed full ordered
	// environment (names, order, values and duplicates all bind), another
	// cwd or home, or changed stdin. Artifact drift keeps the imported
	// verifier's own refusal.
	ErrImportedProcessChanged = errors.New("agentic: process differs from the imported process")
)

// ImportedProcess is an exact final process bound to its imported exec-guard
// verifier. The zero value refuses every verification: only
// NewImportedProcess produces a usable one.
type ImportedProcess struct {
	verifier ExecPlanVerifier
	system   SystemID
	mode     LaunchMode
	binary   string
	argv     []string
	env      []string
	workDir  string
	home     string
	stdin    StdinPayload
}

// NewImportedProcess imports the exec guard for sys and binds the exact
// final process to the resulting verifier. The process shape refuses first:
// binary, cwd and home must be absolute, argv must exclude argv[0], every
// env entry must be well-formed NAME=value with no NUL and no duplicate
// names, and stdin must follow the null/empty/bytes representation. The
// seal half refuses exactly as ImportSeal does — unknown schema or version,
// malformed or tampered data, an unsealed marker for a plugin that declares
// a sealer, sealed data the plugin cannot import — and those refusals
// propagate unchanged. On top of that, construction refuses typed when the
// importer yields no verifier, when the process names another system, and
// when the seal's own binding — the sealed payload's process, or the
// finalized binding — names another binary or argv. An unsealed verifier
// binds no process facts, so it cannot mismatch; only the system check
// applies.
//
// The process is deep-copied: mutating the caller's slices afterwards cannot
// move the binding. Keys travel to ImportSeal for finalized bindings, over
// the same private ephemeral channel.
func NewImportedProcess(sys System, seal Seal, process Plan, keys ...SealCommitmentKey) (ImportedProcess, error) {
	if sys == nil {
		return ImportedProcess{}, errors.New("agentic: cannot import a process without a system")
	}
	if err := validateImportedProcessShape(process); err != nil {
		return ImportedProcess{}, err
	}
	var verifier ExecPlanVerifier
	if imported, err := ImportSeal(sys, seal, keys...); err != nil {
		return ImportedProcess{}, err
	} else {
		verifier = imported
	}
	if verifier == nil {
		return ImportedProcess{}, fmt.Errorf("%w: system %q", ErrImportedVerifierMissing, string(sys.ID()))
	}
	if process.System != sys.ID() {
		return ImportedProcess{}, fmt.Errorf("%w: process names system %q", ErrImportedVerifierMismatch, string(process.System))
	}
	bound, hasBinding := importedSealProcessBinding(seal)
	if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) {
		return ImportedProcess{}, fmt.Errorf("%w: %s binary or argv differs", ErrImportedVerifierMismatch, bound.kind)
	}
	return ImportedProcess{
		verifier: verifier,
		system:   sys.ID(),
		mode:     process.Mode,
		binary:   process.Binary,
		argv:     slices.Clone(process.Argv),
		env:      slices.Clone(process.Env),
		workDir:  process.WorkDir,
		home:     process.Home,
		stdin:    cloneStdin(process.Stdin),
	}, nil
}

// validateImportedProcessShape refuses an exact final process whose shape
// cannot represent the contract's final process (hosted-launch-contract-r2
// §3: binary, cwd and home absolute; argv excluding argv[0]; full ordered
// NAME=value env with no duplicates, malformed entries or NULs; stdin null,
// empty or bytes). Verification compares the bound process exactly, so an
// invalid shape must refuse here, where the binding is established.
func validateImportedProcessShape(process Plan) error {
	if !filepath.IsAbs(process.Binary) {
		return fmt.Errorf("%w: binary %q is not absolute", ErrImportedProcessMalformed, process.Binary)
	}
	if !filepath.IsAbs(process.WorkDir) {
		return fmt.Errorf("%w: working directory %q is not absolute", ErrImportedProcessMalformed, process.WorkDir)
	}
	if !filepath.IsAbs(process.Home) {
		return fmt.Errorf("%w: home %q is not absolute", ErrImportedProcessMalformed, process.Home)
	}
	if len(process.Argv) > 0 && process.Argv[0] == process.Binary {
		return fmt.Errorf("%w: argv carries argv[0]", ErrImportedProcessMalformed)
	}
	seen := make(map[string]bool, len(process.Env))
	for index, entry := range process.Env {
		if strings.IndexByte(entry, 0) >= 0 {
			return fmt.Errorf("%w: env entry %d carries a NUL byte", ErrImportedProcessMalformed, index)
		}
		name, _, found := strings.Cut(entry, "=")
		if !found {
			return fmt.Errorf("%w: env entry %d carries no value", ErrImportedProcessMalformed, index)
		}
		if !validEnvironmentName(name) {
			return fmt.Errorf("%w: env entry %d names %q", ErrImportedProcessMalformed, index, name)
		}
		if seen[name] {
			return fmt.Errorf("%w: env entry %q sets the same variable twice", ErrImportedProcessMalformed, name)
		}
		seen[name] = true
	}
	if !process.Stdin.Attached && len(process.Stdin.Bytes) > 0 {
		return fmt.Errorf("%w: stdin reports %d bytes while detached", ErrImportedProcessMalformed, len(process.Stdin.Bytes))
	}
	return nil
}

// importedSealBinding is the exact process a seal binds: the sealed
// payload's own process for sealed guards, the finalized binding for
// finalized unsealed guards. An unsealed guard without a binding binds no
// process facts.
type importedSealBinding struct {
	kind   string
	binary string
	argv   []string
}

// importedSealProcessBinding reports the exact process the seal itself
// binds, without consulting the imported verifier's capabilities: a
// verifier-only importer cannot hide the binding it was given, and a
// finalized binding no exporter reports still refuses at construction.
// ImportSeal has already integrity-checked the payload this reads.
func importedSealProcessBinding(seal Seal) (importedSealBinding, bool) {
	if seal.Data.Kind == SealKindSealed && seal.Data.Sealed != nil {
		return importedSealBinding{kind: "sealed", binary: seal.Data.Sealed.Binary, argv: seal.Data.Sealed.Argv}, true
	}
	if seal.Data.Binding != nil {
		return importedSealBinding{kind: "finalized", binary: seal.Data.Binding.Binary, argv: seal.Data.Binding.Argv}, true
	}
	return importedSealBinding{}, false
}

// Process returns the bound exact process: the importing system, the launch
// mode, and the six bound facts. Every slice is freshly copied, so mutating
// the result cannot move the binding. The plan carries NO Session, whatever
// the process given to NewImportedProcess carried: Session is verified
// in-process only and is unverified across ExportSeal/ImportSeal (see
// session.go), so an imported plan never holds a record a consumer could take
// for verified.
func (p ImportedProcess) Process() Plan {
	return Plan{
		System:  p.system,
		Mode:    p.mode,
		Binary:  p.binary,
		Argv:    slices.Clone(p.argv),
		Env:     slices.Clone(p.env),
		Stdin:   cloneStdin(p.stdin),
		WorkDir: p.workDir,
		Home:    p.home,
	}
}

// VerifyBeforeExec verifies the given process against the imported exact
// process and then delegates to the imported verifier. Consumers MUST call
// this immediately before starting their own process using the plan's binary
// and argv. Only the six bound facts are compared — binary, ordered argv,
// full ordered env, cwd, home, stdin — so plan metadata the daemon renews
// (provenance, model identity) never trips the comparison. The delegate call
// receives the verified process and performs the plugin's own checks, such
// as re-reading sealed artifacts.
func (p ImportedProcess) VerifyBeforeExec(plan Plan) error {
	if p.verifier == nil {
		return fmt.Errorf("%w: no verifier bound", ErrImportedVerifierMissing)
	}
	if plan.Binary != p.binary {
		return fmt.Errorf("%w: binary differs", ErrImportedProcessChanged)
	}
	if !slices.Equal(plan.Argv, p.argv) {
		return fmt.Errorf("%w: argv differs (got %d elements, imported %d)", ErrImportedProcessChanged, len(plan.Argv), len(p.argv))
	}
	if !slices.Equal(plan.Env, p.env) {
		return fmt.Errorf("%w: environment differs", ErrImportedProcessChanged)
	}
	if plan.WorkDir != p.workDir {
		return fmt.Errorf("%w: working directory differs", ErrImportedProcessChanged)
	}
	if plan.Home != p.home {
		return fmt.Errorf("%w: home differs", ErrImportedProcessChanged)
	}
	if plan.Stdin.Attached != p.stdin.Attached || !bytes.Equal(plan.Stdin.Bytes, p.stdin.Bytes) {
		return fmt.Errorf("%w: stdin differs", ErrImportedProcessChanged)
	}
	return p.verifier.VerifyBeforeExec(plan)
}
