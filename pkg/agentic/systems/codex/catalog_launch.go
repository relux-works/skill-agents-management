package codex

import (
	"crypto/sha256"
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

type catalogExecSeal struct {
	path, digest, providerID string
	binary                   string
	argv                     []string
}

func (seal *catalogExecSeal) VerifyBeforeExec(plan agentic.Plan) error {
	if plan.Binary != seal.binary || !slices.Equal(plan.Argv, seal.argv) {
		return localCatalogRefusal(agentic.LocalProviderConflicting, seal.path, seal.providerID)
	}
	return verifyLaunchCatalog(seal.path, seal.digest, seal.providerID)
}

// SealExecPlan binds the binary, exact argv and catalog digest to the plan.
// The consumer invokes Plan.VerifyBeforeExec immediately before its own Start.
func (*System) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
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
		return &catalogExecSeal{path: path, digest: digest, binary: plan.Binary, argv: append([]string(nil), plan.Argv...)}, nil
	}
	return nil, nil
}
