package localmodels

import (
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// curatorEnginesProjectEnv carries the selected project directory to the
// local Pi system's read-only curator-engines status preflight.
const curatorEnginesProjectEnv = "CURATOR_ENGINES_PROJECT_DIR"

var (
	// ErrNoLocalPointer is returned when a (runtime, model) pair this
	// vendor was asked to spawn has no pointer in local-models.toml — the
	// guard against cross-profile model admission. It is the SAME generic
	// config-driven-refusal mechanism every other refusal in this vendor
	// uses, keyed by the runtime's own declared scope rather than by
	// whatever pointer happens to be first in the file.
	ErrNoLocalPointer = errors.New("localmodels: no local-models.toml pointer for this (runtime, model) pair")
	// ErrProfileConflict is returned when the caller's request already
	// carries a Profile that conflicts with this pair's declared one.
	ErrProfileConflict = errors.New("localmodels: caller's profile conflicts with the declared pointer")
	// ErrProjectConflict is returned when the caller's request already
	// carries a CURATOR_ENGINES_PROJECT_DIR entry that conflicts with this
	// pair's declared project.
	ErrProjectConflict = errors.New("localmodels: caller's CURATOR_ENGINES_PROJECT_DIR conflicts with the declared pointer")
)

// Spawn is local-models's half of one launch: it resolves the (runtime,
// model) pointer, then contributes Profile and CURATOR_ENGINES_PROJECT_DIR
// idempotently. It never sets launch.Runtime — that field is set uniformly
// by BuildLaunch itself, for every runtime, after Spawn returns.
func (v *Vendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	pointer, ok := pointerFor(v.cfg, sc.Runtime.ID, sc.Model.ID)
	if !ok {
		return agentic.LaunchRequest{}, fmt.Errorf("%w: runtime %s, model %s", ErrNoLocalPointer, sc.Runtime.ID, sc.Model.ID)
	}

	launch := vendorplugin.PassthroughLaunch(sc)
	// This local row declares an axis for native catalog compatibility. Hosted
	// vendor rows keep their existing selected-effort-only local binding rule.
	launch.Model.EffortVocabulary = append([]string(nil), sc.Model.Effort.Vocabulary...)
	if sc.Runtime.SystemID == "pi" {
		launch.SystemModelIdentity = pointer.PiModelIdentity
	}

	profile, err := mergeIdempotent("profile", launch.Profile, pointer.CuratorEnginesProfile, ErrProfileConflict)
	if err != nil {
		return agentic.LaunchRequest{}, err
	}
	launch.Profile = profile

	env, err := mergeEnvIdempotent(launch.Env, curatorEnginesProjectEnv, pointer.CuratorEnginesProject, ErrProjectConflict)
	if err != nil {
		return agentic.LaunchRequest{}, err
	}
	launch.Env = env

	return launch, nil
}

// mergeIdempotent applies the three-way rule (architecture decision §2.4)
// to a single scalar value: absent is filled with the declared value,
// present-and-identical is accepted silently, present-and-conflicting is
// refused naming both values and the field.
func mergeIdempotent(field, callerValue, declaredValue string, conflictErr error) (string, error) {
	if callerValue == "" {
		return declaredValue, nil
	}
	if callerValue == declaredValue {
		return callerValue, nil
	}
	return "", fmt.Errorf("%w: %s: caller supplied %q, the declared pointer names %q", conflictErr, field, callerValue, declaredValue)
}

// mergeEnvIdempotent applies the same three-way rule to one environment
// entry, ADDING to env rather than replacing it, and never touching any
// other entry.
func mergeEnvIdempotent(env []string, key, declaredValue string, conflictErr error) ([]string, error) {
	existing, present := launchenv.Lookup(env, key)
	switch {
	case !present:
		return append(append([]string(nil), env...), key+"="+declaredValue), nil
	case existing == declaredValue:
		return env, nil
	default:
		return nil, fmt.Errorf("%w: %s: caller's environment already carries %q, the declared pointer names %q", conflictErr, key, existing, declaredValue)
	}
}
