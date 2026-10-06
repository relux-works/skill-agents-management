package agentic

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/relux-works/skill-agents-management/internal/releasegrammar"
)

// AuxRole is closed: callers cannot introduce a command or a named process.
type AuxRole string

const (
	ClaudeVersionProbe AuxRole = "claude-version-probe"
	ClaudeGoalProbe    AuxRole = "claude-goal-probe"
)

var (
	ErrAuxRoleUnknown = errors.New("agentic: unknown auxiliary role")
	ErrAuxRefused     = errors.New("agentic: auxiliary refused")
)

// AuxTimeoutError identifies the timed-out role while retaining auxiliary
// refusal classification through errors.Is(err, ErrAuxRefused).
type AuxTimeoutError struct {
	Role AuxRole
}

func (e *AuxTimeoutError) Error() string {
	return fmt.Sprintf("agentic: auxiliary %s timed out", e.Role)
}
func (e *AuxTimeoutError) Unwrap() error { return ErrAuxRefused }

// AuxiliaryPlanner is an optional system capability. Its argv is module-owned,
// not a caller projection. BuildAuxiliaryPlans is the production dispatcher.
type AuxiliaryPlanner interface {
	AuxiliaryArgv(AuxRole) ([]string, error)
}

type auxiliaryBasis struct {
	planner       AuxiliaryPlanner
	workDir, home string
}

// AuxPlan carries a private seal and an observable, detached process. The host
// MUST enforce HardTimeout and kill and reap the entire process group before
// reporting NoSurvivors, including on timeout or refusal.
type AuxPlan struct {
	Role               AuxRole
	Process            Plan
	HardTimeout        time.Duration
	RequireNoSurvivors bool
	seal               *auxSeal
}
type auxSeal struct {
	role     AuxRole
	process  Plan
	primary  Plan
	identity auxBinaryIdentity
	timeout  time.Duration
}

func cloneAuxProcess(p Plan) Plan {
	p.Argv = slices.Clone(p.Argv)
	p.Env = slices.Clone(p.Env)
	p.Session = cloneSession(p.Session)
	p.Stdin.Bytes = slices.Clone(p.Stdin.Bytes)
	return p
}
func sameAuxProcess(a, b Plan) bool {
	return a.Mode == b.Mode && a.Stdin.Attached == b.Stdin.Attached && bytes.Equal(a.Stdin.Bytes, b.Stdin.Bytes) && sessionsEqual(a.Session, b.Session) && a.System == b.System && a.Binary == b.Binary && a.WorkDir == b.WorkDir && a.Home == b.Home && slices.Equal(a.Argv, b.Argv) && slices.Equal(a.Env, b.Env)
}

type auxBinaryIdentity struct {
	path   string
	info   os.FileInfo
	digest [32]byte
}

func readAuxBinaryIdentity(p Plan) (auxBinaryIdentity, error) {
	if path, err := launchBoundaryExecutable(p.Binary, p.WorkDir, p.Env); err != nil {
		return auxBinaryIdentity{}, err
	} else {
		f, err := os.Open(path)
		if err != nil {
			return auxBinaryIdentity{}, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return auxBinaryIdentity{}, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return auxBinaryIdentity{}, ErrAuxRefused
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return auxBinaryIdentity{}, err
		}
		return auxBinaryIdentity{path: path, info: info, digest: sha256.Sum256(data)}, nil
	}
}
func sameAuxBinary(a, b auxBinaryIdentity) bool {
	return a.path == b.path && os.SameFile(a.info, b.info) && a.digest == b.digest
}

// Private predicate seams let behavioral tests narrow one rejected class
// without rewriting source or changing the public API.
var auxProcessMatches = sameAuxProcess
var auxPolicyMatches = func(p AuxPlan) bool { return p.HardTimeout == p.seal.timeout && p.RequireNoSurvivors }
var auxAdmissionMatches = func(p Plan) bool { return p.WorkDir == p.auxiliaryBasis.workDir && p.Home == p.auxiliaryBasis.home }
var auxResultTyped = validAuxResult
var auxRoleAllowed = knownAuxRole
var auxBinaryMatches = sameAuxBinary
var auxTimeoutTyped = func(status AuxStatus) bool { return status == AuxTimedOut }
var auxStatusSucceeded = func(status AuxStatus) bool { return status == AuxSucceeded }

func knownAuxRole(role AuxRole) bool { return role == ClaudeVersionProbe || role == ClaudeGoalProbe }

// BuildAuxiliaryPlans takes only the already-admitted primary. It neither
// executes nor accepts new caller argv, env, cwd, home or binary inputs.
// Both probes are required when a host opts into this Claude preflight.
func BuildAuxiliaryPlans(primary Plan) ([]AuxPlan, error) {
	if primary.auxiliaryBasis == nil || primary.baseProcess == nil {
		return nil, ErrAuxRefused
	}
	if !auxAdmissionMatches(primary) {
		return nil, ErrAuxRefused
	}
	if !isFinalizedVerifier(primary.execVerifier) {
		if err := primary.baseProcess.verify(primary); err != nil {
			return nil, err
		}
	}
	if err := primary.VerifyBeforeExec(); err != nil {
		return nil, err
	}
	if identity, err := readAuxBinaryIdentity(primary); err != nil {
		return nil, err
	} else {
		plans := make([]AuxPlan, 0, 2)
		for _, role := range []AuxRole{ClaudeVersionProbe, ClaudeGoalProbe} {
			if argv, err := primary.auxiliaryBasis.planner.AuxiliaryArgv(role); err != nil {
				return nil, err
			} else {
				process := Plan{System: primary.System, Binary: primary.Binary, Argv: slices.Clone(argv), Env: slices.Clone(primary.Env), WorkDir: primary.WorkDir, Home: primary.Home}
				timeout := 15 * time.Second
				if role == ClaudeGoalProbe {
					timeout = 60 * time.Second
				}
				seal := &auxSeal{role: role, process: cloneAuxProcess(process), primary: cloneAuxProcess(primary), identity: identity, timeout: timeout}
				plans = append(plans, AuxPlan{Role: role, Process: process, HardTimeout: timeout, RequireNoSurvivors: true, seal: seal})
			}
		}
		return plans, nil
	}
}

// VerifyBeforeExec must be called at each auxiliary start site. The auxiliary
// seal also retains the primary's verifier; no primary argv is used for aux.
func (p AuxPlan) VerifyBeforeExec() error {
	if !auxRoleAllowed(p.Role) {
		return ErrAuxRoleUnknown
	}
	if p.seal == nil || p.Role != p.seal.role || !auxProcessMatches(p.Process, p.seal.process) || !auxPolicyMatches(p) {
		return ErrAuxRefused
	}
	if err := p.seal.primary.VerifyBeforeExec(); err != nil {
		return err
	}
	if identity, err := readAuxBinaryIdentity(p.Process); err != nil {
		return err
	} else {
		if !auxBinaryMatches(identity, p.seal.identity) {
			return fmt.Errorf("%w: binary changed", ErrAuxRefused)
		}
		return nil
	}
}

type AuxStatus uint8

const (
	AuxSucceeded AuxStatus = iota + 1
	AuxTimedOut
	AuxFailed
)

// AuxResult is a host assertion, never raw output. Version and GoalReady are
// consumed for admission only; they cannot enter the primary argv or env.
type AuxResult struct {
	Role        AuxRole
	Status      AuxStatus
	NoSurvivors bool
	Version     string
	GoalReady   bool
}

// VerifyPrimaryAfterAux must be called immediately before the primary start,
// then primary.VerifyBeforeExec. Missing, duplicate, failed, timed-out or
// unreaped probes refuse. Host execution and truthful result decoding remain
// host responsibilities; this in-process projection is not a wire attestation.
func VerifyPrimaryAfterAux(primary Plan, plans []AuxPlan, results []AuxResult) error {
	if len(plans) != 2 || len(results) != 2 {
		return ErrAuxRefused
	}
	seen := make(map[AuxRole]bool, 2)
	for _, p := range plans {
		if err := p.VerifyBeforeExec(); err != nil {
			return err
		}
		if seen[p.Role] || !auxProcessMatches(primary, p.seal.primary) {
			return ErrAuxRefused
		}
		seen[p.Role] = true
		var found *AuxResult
		for i := range results {
			if results[i].Role == p.Role {
				if found != nil {
					return ErrAuxRefused
				}
				found = &results[i]
			}
		}
		if found == nil {
			return ErrAuxRefused
		}
		if !auxStatusSucceeded(found.Status) || !found.NoSurvivors {
			if auxTimeoutTyped(found.Status) {
				return &AuxTimeoutError{Role: p.Role}
			}
			return ErrAuxRefused
		}
		if !auxResultTyped(p.Role, *found) {
			return ErrAuxRefused
		}
	}
	return primary.VerifyBeforeExec()
}

func validAuxResult(role AuxRole, result AuxResult) bool {
	switch role {
	case ClaudeVersionProbe:
		return releasegrammar.IsReleaseTriple(result.Version) && !result.GoalReady
	case ClaudeGoalProbe:
		return result.GoalReady && result.Version == ""
	default:
		return false
	}
}
