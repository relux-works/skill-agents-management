package muse

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Like Codex's launch catalogs, immutable projections live for the process's
// plan lifetime. No shared settings are written. Auth is linked, never read.
var networkConfigs = struct {
	sync.Mutex
	roots     map[string]string
	artifacts map[string]networkArtifact
	requests  map[string]string
}{roots: map[string]string{}, artifacts: map[string]networkArtifact{}, requests: map[string]string{}}

type networkArtifact struct {
	files map[string][]byte
	links map[string]string
}

func materializeNetworkConfig(source string, rendered []byte, settings map[string]any, managedSource string) (string, error) {
	networkConfigs.Lock()
	defer networkConfigs.Unlock()
	key := fmt.Sprintf("%s:%x", source, sha256.Sum256(rendered))
	if root := networkConfigs.roots[key]; root != "" {
		return root, verifyNetworkArtifact(root, networkConfigs.artifacts[root])
	}
	root, err := os.MkdirTemp("", "agents-management-muse-network-")
	if err != nil {
		return "", err
	}
	successful := false
	defer func() {
		if !successful {
			_ = os.RemoveAll(root)
		}
	}()
	dir := filepath.Join(root, "muse")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	artifact := networkArtifact{files: map[string][]byte{}, links: map[string]string{}}
	// Config siblings include auth and trust only in the supported projection.
	// Refuse an unknown file rather than drop it or link a hidden launch source.
	entries, err := os.ReadDir(source)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "settings.json":
			continue
		case "auth.json", "trust.json":
			target := filepath.Join(source, entry.Name())
			path := filepath.Join(dir, entry.Name())
			if err := os.Symlink(target, path); err != nil {
				return "", err
			}
			artifact.links[path] = target
		default:
			// A named managed hook file is copied below, not passed through.
			if filepath.Join(source, entry.Name()) == managedSource {
				continue
			}
			return "", fmt.Errorf("unknown config sibling")
		}
	}
	if managed, ok := settings["managed_hooks_path"].(map[string]any); ok {
		data, err := json.Marshal(managed)
		if err != nil {
			return "", err
		}
		path := filepath.Join(dir, "network-managed-hooks.json")
		artifact.files[path] = data
		settings["managed_hooks_path"] = path
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	artifact.files[filepath.Join(dir, "settings.json")] = data
	for path, data := range artifact.files {
		if err := os.WriteFile(path, data, 0o400); err != nil {
			return "", err
		}
	}
	networkConfigs.roots[key] = root
	networkConfigs.artifacts[root] = artifact
	successful = true
	return root, nil
}
func verifyNetworkArtifact(root string, artifact networkArtifact) error {
	for _, dir := range []string{root, filepath.Join(root, "muse")} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("private config directory changed")
		}
	}
	for path, data := range artifact.files {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
			return fmt.Errorf("private config file changed")
		}
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, data) {
			return fmt.Errorf("private config bytes changed")
		}
	}
	for path, target := range artifact.links {
		current, err := os.Readlink(path)
		if err != nil || current != target {
			return fmt.Errorf("config link changed")
		}
	}
	return nil
}

type networkExecSeal struct {
	root, home, workDir, dataHome string
	binary                        string
	argv                          []string
	artifact                      networkArtifact
}

func (*System) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	if _, managed := plan.NetworkProvenanceSnapshot(); !managed {
		return nil, nil
	}
	root := envValue(plan.Env, "XDG_CONFIG_HOME")
	networkConfigs.Lock()
	artifact, ok := networkConfigs.artifacts[root]
	networkConfigs.Unlock()
	if !ok {
		return nil, networkRefusal("private config not bound to plan")
	}
	dataHome := envValue(plan.Env, "XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(envValue(plan.Env, "HOME"), ".local", "share")
	}
	seal := &networkExecSeal{root: root, home: envValue(plan.Env, "HOME"), workDir: plan.WorkDir, dataHome: dataHome, artifact: artifact, binary: plan.Binary, argv: append([]string(nil), plan.Argv...)}
	if err := seal.VerifyBeforeExec(plan); err != nil {
		return nil, err
	}
	return seal, nil
}
func (seal *networkExecSeal) VerifyBeforeExec(plan agentic.Plan) error {
	if hasDuplicateSensitiveSelector(plan.Env) {
		return networkRefusal("duplicate sensitive selector after planning")
	}
	if plan.Binary != seal.binary || !slices.Equal(plan.Argv, seal.argv) || envValue(plan.Env, "XDG_CONFIG_HOME") != seal.root || envValue(plan.Env, "HOME") != seal.home || plan.WorkDir != seal.workDir || envValue(plan.Env, museNoAutoUpdateEnv) != "1" {
		return networkRefusal("config selection changed after planning")
	}
	dataHome := envValue(plan.Env, "XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(envValue(plan.Env, "HOME"), ".local", "share")
	}
	if dataHome != seal.dataHome {
		return networkRefusal("data selection changed after planning")
	}
	if err := verifyNetworkArtifact(seal.root, seal.artifact); err != nil {
		return networkRefusal("private config changed after planning")
	}
	for _, path := range uncoveredConfigPaths(seal.workDir, seal.dataHome) {
		if err := requireAbsent(path); err != nil {
			return networkRefusal("uncovered source appeared after planning")
		}
	}
	return nil
}
