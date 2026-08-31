package providerlimits

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// The cross-provider spread cursor (design §4.3, §5.2).
//
// It cannot live in an identity file, because it rotates over the surviving
// groups of EVERY provider in play, and an identity file is per provider. It
// gets its own machine-scoped file with its own lock.
//
// Entries are keyed by the sorted provider set so an exclusive window and a
// mixed window never share a cursor: rotating a two-provider mixed policy would
// otherwise leave a position no single-provider window can honour, and the two
// would fight over one slot.
//
// The cursor carries NO correctness weight. Losing it restarts the rotation and
// nothing else, which is why every read path here is fail-open — a missing,
// unreadable, corrupt or newer-schema file yields "no position" rather than an
// error that could block a spawn.

// SpreadCursorSchemaVersion is the on-disk version of the cursor file. It
// tracks the package schema version so a downgrade is refused by the same rule
// the identity files use.
const SpreadCursorSchemaVersion = SchemaVersion

// spreadCursorFile is the on-disk shape:
//
//	{ "version": 3, "cursor": { "<provider-set>": "<identity>:<group>" } }
type spreadCursorFile struct {
	Version int               `json:"version"`
	Cursor  map[string]string `json:"cursor"`
}

// SpreadCursorKey is the per-provider-set entry key: lower-cased, de-duplicated
// and sorted, so the key an exclusive(codex) window writes can never collide
// with the key mixed(claude, codex) writes.
func SpreadCursorKey(providers []string) string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(providers))
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		if _, dup := seen[provider]; dup {
			continue
		}
		seen[provider] = struct{}{}
		normalized = append(normalized, provider)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ",")
}

// SpreadCursorValue is the stored position: the identity that owned the group,
// then the group. The identity qualifies it because the same group name under a
// different account is a different quota pool, so a home switch must not
// inherit the previous account's turn.
func SpreadCursorValue(identity, group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	return strings.TrimSpace(identity) + ":" + group
}

// SpreadCursorStore reads and writes the machine-scoped cursor file.
type SpreadCursorStore struct {
	layout      Layout
	lockTimeout time.Duration
	now         func() time.Time
	warn        func(string)
}

// SpreadCursorOptions configures a cursor store. Every field is optional.
type SpreadCursorOptions struct {
	Layout      Layout
	LockTimeout time.Duration
	Now         func() time.Time
	Warn        func(string)
}

// NewSpreadCursorStore builds a cursor store over one layout.
func NewSpreadCursorStore(opts SpreadCursorOptions) *SpreadCursorStore {
	store := &SpreadCursorStore{
		layout:      opts.Layout,
		lockTimeout: opts.LockTimeout,
		now:         opts.Now,
		warn:        opts.Warn,
	}
	if store.lockTimeout <= 0 {
		store.lockTimeout = DefaultLockTimeout
	}
	if store.now == nil {
		store.now = func() time.Time { return time.Now().UTC() }
	}
	return store
}

func (s *SpreadCursorStore) warnf(format string, args ...any) {
	if s == nil || s.warn == nil {
		return
	}
	s.warn(fmt.Sprintf(format, args...))
}

// Read returns the stored position for one provider set, or "" when there is
// none.
//
// Every failure is reported as "no position": a rotation that restarts costs at
// most one uneven turn, while an error here would take a spawn down for a
// record that carries no correctness weight.
func (s *SpreadCursorStore) Read(providers []string) string {
	if s == nil {
		return ""
	}
	file, ok := s.load()
	if !ok {
		return ""
	}
	return strings.TrimSpace(file.Cursor[SpreadCursorKey(providers)])
}

// Write records the position for one provider set.
//
// It is idempotent BY CONSTRUCTION: the value written is the group that was
// actually launched into, not an increment. A preflight that runs more than
// once for one spawn therefore consumes exactly one cursor turn no matter how
// many times it writes, which is what design §5.7 requires of a re-resolution.
func (s *SpreadCursorStore) Write(providers []string, value string) error {
	if s == nil {
		return nil
	}
	key := SpreadCursorKey(providers)
	if key == "" || strings.TrimSpace(value) == "" {
		return nil
	}
	path := s.layout.SpreadCursorFile()
	lockErr := withFileLock(s.layout.SpreadCursorLockFile(), s.lockTimeout, s.now, s.warnf, func() error {
		file, ok := s.load()
		if !ok && file == nil {
			// Absent or unusable content. A newer-schema file is returned even
			// with ok=false precisely so the version check below still sees it:
			// treating "could not use it" as "start fresh" is what would write
			// the downgrade this refuses.
			file = &spreadCursorFile{Version: SpreadCursorSchemaVersion, Cursor: map[string]string{}}
		}
		if file.Version > SpreadCursorSchemaVersion {
			// A newer binary owns this file. Refusing the write costs a restarted
			// rotation; writing a downgrade would corrupt the newer binary's state.
			s.warnf("provider-limits spread cursor %s has schema version %d, newer than this binary understands (%d); refusing to write it", path, file.Version, SpreadCursorSchemaVersion)
			return nil
		}
		file.Version = SpreadCursorSchemaVersion
		if file.Cursor == nil {
			file.Cursor = map[string]string{}
		}
		if file.Cursor[key] == value {
			return nil
		}
		file.Cursor[key] = value
		return writeJSONAtomic(path, file)
	})
	if lockErr != nil {
		if errors.Is(lockErr, ErrLockTimeout) {
			// Skip the write, never fail the spawn. The next selection reads the
			// unadvanced position and repeats one group; that is strictly cheaper
			// than refusing to launch over a rotation hint.
			s.warnf("provider-limits spread cursor lock timed out; the rotation position for [%s] was not advanced", key)
			return nil
		}
		return lockErr
	}
	return nil
}

// load reads the cursor file. ok=false means "there is no usable position",
// which every caller treats as a restart of the rotation.
func (s *SpreadCursorStore) load() (*spreadCursorFile, bool) {
	path := s.layout.SpreadCursorFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			// A read FAILURE is not an absence, and it is reported as its own
			// fact: the position is unknown, not empty.
			s.warnf("provider-limits spread cursor %s unreadable (%v); the rotation restarts", path, err)
		}
		return nil, false
	}
	var file spreadCursorFile
	if err := json.Unmarshal(data, &file); err != nil {
		s.warnf("provider-limits spread cursor %s is corrupt (%v); the rotation restarts", path, err)
		return nil, false
	}
	if file.Cursor == nil {
		file.Cursor = map[string]string{}
	}
	if file.Version > SpreadCursorSchemaVersion {
		s.warnf("provider-limits spread cursor %s has schema version %d, newer than this binary understands (%d); the rotation restarts", path, file.Version, SpreadCursorSchemaVersion)
		// The file is still returned so Write can see the version and refuse the
		// downgrade; the position is deliberately dropped.
		return &spreadCursorFile{Version: file.Version, Cursor: map[string]string{}}, false
	}
	return &file, true
}
