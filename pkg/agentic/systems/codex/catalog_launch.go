package codex

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Each process owns a private temp directory. Content-addressed files are
// shared only for identical bytes, preserving ID/snapshot argv parity. Never
// overwrite a published file: verify it on reuse and immediately before exec.
// Files remain available for the lifetime of the returned plans.
var launchCatalogs = struct {
	sync.Mutex
	root string
}{}

func materializeCatalog(data []byte, providerID string) (string, error) {
	launchCatalogs.Lock()
	defer launchCatalogs.Unlock()
	if launchCatalogs.root == "" {
		root, err := os.MkdirTemp("", "agents-management-codex-")
		if err != nil {
			return "", localCatalogRefusal(agentic.LocalProviderReadFailed, "catalog.json", providerID)
		}
		launchCatalogs.root = root
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	path := filepath.Join(launchCatalogs.root, "catalog-"+digest+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err == nil {
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return "", localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)
		}
	} else if !os.IsExist(err) {
		return "", localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)
	}
	if err := verifyLaunchCatalog(path, digest, providerID); err != nil {
		return "", err
	}
	return path, nil
}

func verifyLaunchCatalog(path, digest, providerID string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
		return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm() != 0o700 {
		return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)
	}
	data, err := readRegularCatalog(path)
	if err != nil {
		return localCatalogRefusal(agentic.LocalProviderReadFailed, path, providerID)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return localCatalogRefusal(agentic.LocalProviderConflicting, path, providerID)
	}
	return nil
}

// ErrCodexTempDirChanged refuses a plan whose TMPDIR selection no longer
// matches the seal: a changed value, a removed entry, or a duplicated one.
// Duplicates refuse even when every copy carries the sealed value, because
// the child resolves them last-wins and a second entry is a second writer.
// Absence and duplicates AT SEAL TIME bind nothing (they are not a
// selection), so only a plan sealed with exactly one TMPDIR entry is
// checked; every other plan verifies exactly as before.
var ErrCodexTempDirChanged = errors.New("codex: sealed temp dir changed after planning")

type catalogExecSeal struct {
	path, digest, providerID string
	binary                   string
	argv                     []string
	// tmpdir is the sealed TMPDIR selection, meaningful only when
	// tmpdirBound. An imported seal holds a keyed commitment here instead
	// of the value (tmpdirCommitted) and commits the presented value
	// before comparing, so exported guards never carry the directory.
	tmpdir          string
	tmpdirBound     bool
	tmpdirCommitted bool
}

// sealedTempDirSelection reads the plan's TMPDIR selection: the value when
// the environment carries exactly one TMPDIR entry, unbound otherwise. Bare
// entries without '=' carry no value and are ignored, the same reading the
// launch layer applies everywhere else.
func sealedTempDirSelection(env []string) (string, bool) {
	var first string
	var count int
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name != TempDirEnv {
			continue
		}
		if count == 0 {
			first = value
		}
		count++
	}
	return first, count == 1
}

// verifySealedTempDir re-checks the sealed TMPDIR selection against the
// presented environment. An unbound seal checks nothing: a plan sealed
// without a selection verifies exactly as before.
func verifySealedTempDir(seal *catalogExecSeal, env []string) error {
	if !seal.tmpdirBound {
		return nil
	}
	var first string
	var count int
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name != TempDirEnv {
			continue
		}
		if count == 0 {
			first = value
		}
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: want exactly one TMPDIR entry, found %d", ErrCodexTempDirChanged, count)
	}
	presented := first
	if seal.tmpdirCommitted {
		presented = agentic.CommitSealLiteral(TempDirEnv, first)
	}
	if presented != seal.tmpdir {
		return fmt.Errorf("%w: TMPDIR value differs from the sealed selection", ErrCodexTempDirChanged)
	}
	return nil
}

func (seal *catalogExecSeal) VerifyBeforeExec(plan agentic.Plan) error {
	if seal.path == "" {
		// A TempDir-only seal binds no catalog: the TMPDIR selection is
		// the whole seal. Exact-process binding for such plans arrives
		// through FinalizePlan's generic final-process commitments.
		return verifySealedTempDir(seal, plan.Env)
	}
	if plan.Binary != seal.binary || !slices.Equal(plan.Argv, seal.argv) {
		return localCatalogRefusal(agentic.LocalProviderConflicting, seal.path, seal.providerID)
	}
	if err := verifySealedTempDir(seal, plan.Env); err != nil {
		return err
	}
	return verifyLaunchCatalog(seal.path, seal.digest, seal.providerID)
}

// SealExecPlan binds the binary, exact argv and catalog digest to the plan,
// plus the plan's TMPDIR selection when it carries exactly one TMPDIR
// entry. A plan with neither a local catalog nor a TMPDIR selection seals
// to nothing, exactly as before.
// The consumer invokes Plan.VerifyBeforeExec immediately before its own Start.
func (*System) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	tmpdir, tmpdirBound := sealedTempDirSelection(plan.Env)
	launchCatalogs.Lock()
	root := launchCatalogs.root
	launchCatalogs.Unlock()
	for _, arg := range plan.Argv {
		if !strings.HasPrefix(arg, "model_catalog_json=") {
			continue
		}
		path, err := strconv.Unquote(strings.TrimPrefix(arg, "model_catalog_json="))
		if err != nil {
			return nil, localCatalogRefusal(agentic.LocalProviderMalformed, "catalog.json", "")
		}
		// Only locally materialized argv reaches this branch; hosted composition
		// may contain the same native key and keeps its existing behavior.
		if filepath.Dir(path) != root || root == "" {
			continue
		}
		digest := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "catalog-"), ".json")
		return &catalogExecSeal{path: path, digest: digest, binary: plan.Binary, argv: append([]string(nil), plan.Argv...), tmpdir: tmpdir, tmpdirBound: tmpdirBound}, nil
	}
	if !tmpdirBound {
		return nil, nil
	}
	return &catalogExecSeal{binary: plan.Binary, argv: append([]string(nil), plan.Argv...), tmpdir: tmpdir, tmpdirBound: true}, nil
}
