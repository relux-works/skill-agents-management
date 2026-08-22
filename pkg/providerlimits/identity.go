package providerlimits

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// A provider home is a HARNESS configuration home, and the harness declares it.
//
// The extraction source kept its own table here: a per-runtime row naming the
// environment variable that repoints the home and the conventional dot-directory
// to fall back to. This port does NOT carry that table, and the reason is
// invariant 5 rather than tidiness — the fact already lives in this module,
// declared by the agentic system plugin that runs the harness:
//
//	pkg/agentic/systems/codex:  HomeEnvVar "CODEX_HOME",         DefaultHome "~/.codex"
//	pkg/agentic/systems/claude: HomeEnvVar "CLAUDE_CONFIG_DIR",  DefaultHome "~/.claude"
//
// The claude plugin's own comment says why those two fields exist at all: "so a
// caller keying limit state by the resolved home has something to resolve".
// This is that caller. A second copy here would be the shadow table
// pkg/agentic/singlesource_guard_test.go fails the build over, and it would be
// the expensive kind: a home that drifted between the launch and the limit
// lookup keys state under a name the launch never uses, silently.
//
// # Two consequences worth stating, because both differ from a hand-typed table
//
// A runtime whose system declares NO home rule — gemini, agy, muse, qwen, all
// of which carry empty HomeEnvVar and DefaultHome — has no provider home, and
// DefaultProviderHome reports that rather than guessing one. That is exactly
// the source's own position on qwen: no evidence in either tree establishes
// which variable the real qwen-code CLI reads, and inventing one would be a
// guess the source's table comment exists to refuse.
//
// A runtime the frozen table does not ship but an operator DECLARED — the
// source's worked example is qwen-codex, alibaba models under the codex
// harness — resolves through its declaration to the codex system and therefore
// to CODEX_HOME and ~/.codex. The source special-cased that with a table row
// and a paragraph explaining that the account home is a property of the BINARY
// reading it; here it falls out of resolving through the binary's own plugin,
// with no row to write.

// Identity is one provider home on this machine.
//
// Home is the normalised path; nothing inside it is ever opened. HomeDisplay is
// the same path with $HOME collapsed to ~, and it is the only form any surface
// prints.
type Identity struct {
	Key         string `json:"identity"`
	Provider    string `json:"provider"`
	Home        string `json:"-"`
	HomeDisplay string `json:"home_display"`
}

// IdentityKey is hex(sha256(provider || 0x00 || normalizedHome))[:16].
//
// The hash input is the path string only. Nothing inside the home is read, so
// the key can never derive from — or leak — a credential. That is the whole
// reason the key is a path hash rather than a token fingerprint.
//
// THIS FUNCTION IS THE ON-DISK IDENTITY. Its output names the state file, so a
// change to the provider spelling, the separator byte, the field order or the
// truncation length orphans every suppression on every machine, silently and
// with no error anywhere — see docs/architecture.md invariant 2, and
// crossbinary_test.go, which pins it against real pre-extraction state files.
func IdentityKey(provider, normalizedHome string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + normalizedHome))
	return hex.EncodeToString(sum[:])[:16]
}

// DefaultProviderHome is the provider home before normalisation, resolved
// through the default registries: the harness's home environment variable when
// set and non-empty, else the harness's conventional dot-directory.
func DefaultProviderHome(runtime string) (string, error) {
	return DefaultProviderHomeIn(vendorplugin.Default, agentic.Default, runtime)
}

// DefaultProviderHomeIn is DefaultProviderHome against explicit registries.
//
// It walks two hops, and each hop is somebody else's single source: the runtime
// declaration says which agentic system drives this runtime, and that system's
// capabilities say which environment variable it reads and where it looks by
// default. Nothing in between is stored here.
//
// The errors name which hop failed, because the operator's next move differs:
// an undeclared runtime is a declaration to write, an unregistered system is a
// plugin to compile in, and a system with no home rule is a fact nobody has
// established yet and no edit here can invent.
func DefaultProviderHomeIn(vendors *vendorplugin.Registry, systems *agentic.Registry, runtime string) (string, error) {
	if strings.TrimSpace(runtime) == "" {
		return "", fmt.Errorf("providerlimits: a runtime is required to resolve a provider home")
	}
	declaration, ok := vendors.RuntimeDeclarationOf(vendorplugin.RuntimeID(runtime))
	if !ok {
		return "", fmt.Errorf("providerlimits: no provider home is defined for provider %q: it is not a declared runtime, so nothing names the harness whose configuration home it would be", runtime)
	}
	system, ok := systems.Lookup(declaration.System)
	if !ok {
		return "", fmt.Errorf("providerlimits: no provider home is defined for provider %q: its agentic system %q has no registered plugin, and the home rule is that plugin's declaration", runtime, declaration.System)
	}
	capabilities := system.Capabilities()
	if strings.TrimSpace(capabilities.HomeEnvVar) == "" && strings.TrimSpace(capabilities.DefaultHome) == "" {
		return "", fmt.Errorf("providerlimits: no provider home is defined for provider %q: its agentic system %q declares neither a home environment variable nor a default home, and this package will not guess one", runtime, declaration.System)
	}
	if capabilities.HomeEnvVar != "" {
		if envValue := os.Getenv(capabilities.HomeEnvVar); strings.TrimSpace(envValue) != "" {
			return envValue, nil
		}
	}
	if strings.TrimSpace(capabilities.DefaultHome) == "" {
		return "", fmt.Errorf("providerlimits: no provider home is defined for provider %q: its agentic system %q declares %s as the home variable and no default, and %s is unset", runtime, declaration.System, capabilities.HomeEnvVar, capabilities.HomeEnvVar)
	}
	return capabilities.DefaultHome, nil
}

// ResolveIdentity resolves the identity the current process environment points
// at for one provider.
func ResolveIdentity(provider string) (Identity, error) {
	home, err := DefaultProviderHome(provider)
	if err != nil {
		return Identity{}, err
	}
	return IdentityFor(provider, home)
}

// IdentityFor resolves an explicit provider home.
func IdentityFor(provider, home string) (Identity, error) {
	if strings.TrimSpace(provider) == "" {
		return Identity{}, fmt.Errorf("providerlimits: provider is required")
	}
	if strings.TrimSpace(home) == "" {
		return Identity{}, fmt.Errorf("providerlimits: provider home is required for provider %q", provider)
	}
	normalized, err := NormalizeProviderHome(home)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		Key:         IdentityKey(provider, normalized),
		Provider:    provider,
		Home:        normalized,
		HomeDisplay: CollapseHome(normalized),
	}, nil
}

// NormalizeProviderHome applies, in order: ~ expansion, Abs, Clean,
// EvalSymlinks (keeping the cleaned path on error), and trailing-separator
// removal.
//
// Case is deliberately NOT folded. On a case-insensitive filesystem two
// spellings of one home therefore produce two identities: a duplicated record,
// which is a lost optimisation and never a wrong suppression. Folding case would
// be wrong on case-sensitive volumes, so the harmless error is the one taken.
//
// The path is never opened and its contents are never listed. EvalSymlinks may
// fail — a home that does not exist yet is normal — and that failure is not an
// error.
func NormalizeProviderHome(home string) (string, error) {
	expanded, err := expandTilde(home)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("providerlimits: resolving provider home %q: %w", home, err)
	}
	cleaned := filepath.Clean(absolute)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = filepath.Clean(resolved)
	}
	return trimTrailingSeparator(cleaned), nil
}

func expandTilde(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~"+string(filepath.Separator)) && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("providerlimits: expanding %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func trimTrailingSeparator(path string) string {
	for len(path) > 1 && os.IsPathSeparator(path[len(path)-1]) {
		path = path[:len(path)-1]
	}
	return path
}

// CollapseHome renders a path with $HOME replaced by ~. It is a path, never a
// secret, and it is the only home representation any surface prints.
func CollapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = trimTrailingSeparator(filepath.Clean(home))
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// --- layout -----------------------------------------------------------------

// Layout is the machine-scoped state root for this package.
type Layout struct {
	// Root is <state-home>/task-board/provider-limits.
	Root string
}

// DefaultLayout resolves the platform user-state directory: XDG_STATE_HOME is
// authoritative when present, os.UserConfigDir is the portable fallback. This
// follows the session manager's precedent so all machine-scoped runtime state
// lives under one root.
func DefaultLayout() (Layout, error) {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil {
			return Layout{}, fmt.Errorf("providerlimits: resolving platform user state directory: %w", err)
		}
		root = configRoot
	}
	return LayoutAt(root), nil
}

// LayoutAt is the deterministic layout constructor used by tests and custom
// launchers. stateRoot is the parent of task-board/provider-limits.
func LayoutAt(stateRoot string) Layout {
	return Layout{Root: filepath.Join(stateRoot, "task-board", "provider-limits")}
}

// StateFile is one identity's group record file.
func (l Layout) StateFile(identity string) string {
	return filepath.Join(l.Root, identity+".state.json")
}

// LockFile is the O_EXCL lock guarding one identity's state file. It is a
// sibling of the file it guards, so the locking unit is one identity: two
// providers never contend, and two repositories sharing one home serialise.
func (l Layout) LockFile(identity string) string {
	return l.StateFile(identity) + ".lock"
}

// IndexFile is the identity index. It is a discovery aid only; the per-identity
// files are authoritative.
func (l Layout) IndexFile() string { return filepath.Join(l.Root, "identities.json") }

// IndexLockFile guards the index.
func (l Layout) IndexLockFile() string { return l.IndexFile() + ".lock" }

// SpreadCursorFile holds the cross-provider spread cursor. It cannot live in an
// identity file because it rotates over the surviving groups of every provider
// in play.
func (l Layout) SpreadCursorFile() string { return filepath.Join(l.Root, "spread-cursor.json") }

// SpreadCursorLockFile guards the spread cursor.
func (l Layout) SpreadCursorLockFile() string { return l.SpreadCursorFile() + ".lock" }

// SimulateFile holds the dev-only armed fault-injection token.
func (l Layout) SimulateFile() string { return filepath.Join(l.Root, "simulate.json") }

// SimulateLockFile guards the armed token.
func (l Layout) SimulateLockFile() string { return l.SimulateFile() + ".lock" }

const stateFileSuffix = ".state.json"

// --- identity index ---------------------------------------------------------

// IndexEntry is one identity's index record.
type IndexEntry struct {
	Provider    string    `json:"provider"`
	HomeDisplay string    `json:"home_display"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// IdentityIndex is the on-disk index.
type IdentityIndex struct {
	Version    int                   `json:"version"`
	Identities map[string]IndexEntry `json:"identities"`
}

func newIdentityIndex() *IdentityIndex {
	return &IdentityIndex{Version: SchemaVersion, Identities: map[string]IndexEntry{}}
}

// readIndex loads the index. A missing, corrupt or newer-schema index is
// rebuilt by scanning the per-identity files, which are authoritative — the
// index can never be the reason a spawn is blocked.
func (s *Store) readIndex() *IdentityIndex {
	path := s.layout.IndexFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.warnf("provider-limits identity index unreadable (%v); rebuilding from per-identity files", err)
		}
		return s.rebuildIndex()
	}
	index := newIdentityIndex()
	if err := json.Unmarshal(data, index); err != nil {
		s.warnf("provider-limits identity index at %s is corrupt (%v); rebuilding from per-identity files", path, err)
		return s.rebuildIndex()
	}
	if index.Version > SchemaVersion {
		s.warnf("provider-limits identity index at %s has schema version %d, newer than this binary understands; rebuilding from per-identity files", path, index.Version)
		return s.rebuildIndex()
	}
	if index.Identities == nil {
		index.Identities = map[string]IndexEntry{}
	}
	return index
}

// rebuildIndex scans the identity state files and reconstructs index entries
// from them.
func (s *Store) rebuildIndex() *IdentityIndex {
	index := newIdentityIndex()
	entries, err := os.ReadDir(s.layout.Root)
	if err != nil {
		return index
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), stateFileSuffix) {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), stateFileSuffix)
		path := filepath.Join(s.layout.Root, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file identityStateFile
		if err := json.Unmarshal(data, &file); err != nil {
			continue
		}
		seen := s.now()
		if info, err := os.Stat(path); err == nil {
			seen = info.ModTime()
		}
		index.Identities[key] = IndexEntry{
			Provider:    file.Provider,
			HomeDisplay: file.HomeDisplay,
			FirstSeen:   seen,
			LastSeen:    seen,
		}
	}
	return index
}

// touchIndex upserts one identity's index entry. Index maintenance is
// best-effort: a failure warns and is never returned to the caller, because the
// index carries no correctness weight.
func (s *Store) touchIndex(identity Identity) {
	err := s.withLock(s.layout.IndexLockFile(), func() error {
		index := s.readIndex()
		now := s.now()
		entry, ok := index.Identities[identity.Key]
		if !ok {
			entry = IndexEntry{FirstSeen: now}
		}
		entry.Provider = identity.Provider
		entry.HomeDisplay = identity.HomeDisplay
		entry.LastSeen = now
		index.Identities[identity.Key] = entry
		index.Version = SchemaVersion
		return writeJSONAtomic(s.layout.IndexFile(), index)
	})
	if err != nil {
		s.warnf("provider-limits identity index not updated for %s: %v", identity.Key, err)
	}
}

// ListIndexedIdentities returns every indexed identity, newest first by last
// seen. It reads and never writes.
func (s *Store) ListIndexedIdentities() []Identity {
	index := s.readIndex()
	keys := make([]string, 0, len(index.Identities))
	for key := range index.Identities {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := index.Identities[keys[i]], index.Identities[keys[j]]
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.After(b.LastSeen)
		}
		return keys[i] < keys[j]
	})
	out := make([]Identity, 0, len(keys))
	for _, key := range keys {
		entry := index.Identities[key]
		out = append(out, Identity{
			Key:         key,
			Provider:    entry.Provider,
			HomeDisplay: entry.HomeDisplay,
		})
	}
	return out
}
