// Package runtimeenv is the codex-family parent-runtime environment strip,
// shared by every plugin whose harness must not inherit a parent Codex
// session.
//
// # Why it is shared rather than copied
//
// The extraction source has ONE of these:
//
//	func filterQwenRuntimeEnv(environ []string) []string {
//		return filterCodexRuntimeEnv(filterEnv(environ, "CLAUDECODE"))
//	}
//
// (skill-project-management, tools/board-cli/internal/spawn/adapter.go). The
// comment on that function says why in the source's own words — "keeps this
// single-sourced instead of restating the list" — and it was written the day
// qwen's filter was fixed. A port that gave the qwen plugin its own copy of the
// eleven keys would reintroduce exactly the drift that comment closed, and the
// drift would be invisible: a child that inherits one key too many still
// launches.
//
// The extraction follows the pattern this module already uses twice.
// internal/launchenv holds the PATH-lookup rule two plugins share, and
// internal/argvguard holds the argv guard's scanner, extracted from the codex
// plugin's guard the moment the second plugin needed the same discipline. Its
// package comment states the principle this package inherits: a second
// implementation of one rule, even one written by copying the first, is two
// definitions that drift the moment either is extended.
//
// # What lives here and what does not
//
// Here: the blocked KEY set, the whole-key filter, the pointer resolution that
// blocks the credentials the pointers name, and the PATH sanitizer.
//
// Not here: anything a single harness injects. The codex plugin's service-tier
// variable stays in the codex plugin, and the qwen plugin's extra CLAUDECODE
// strip stays in the qwen plugin — it is the one key that makes the qwen
// filter different from this one, and burying it in a shared helper would make
// two systems' contracts read as one.
package runtimeenv

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// The parent-runtime keys a codex-family child must not inherit. Each is
// spelled as its own constant so the blocked set below reads as a list of facts
// rather than a wall of strings.
const (
	// ThreadIDEnv, SessionEnv and CIEnv are the parent codex process's own
	// session identity. A child inheriting them attaches to the parent's
	// thread instead of starting its own.
	ThreadIDEnv = "CODEX_THREAD_ID"
	SessionEnv  = "CODEX_SESSION"
	CIEnv       = "CODEX_CI"
	// ManagedByNPMEnv and ManagedByBunEnv record how the PARENT was installed.
	// They are install provenance, and a child resolves its own.
	ManagedByNPMEnv = "CODEX_MANAGED_BY_NPM"
	ManagedByBunEnv = "CODEX_MANAGED_BY_BUN"
	// ManagedPackageRootEnv is the parent's managed npm package root. It is
	// also the variable the codex plugin's binary resolution READS, which is
	// why it is spelled here once and referenced there rather than written
	// twice.
	ManagedPackageRootEnv = "CODEX_MANAGED_PACKAGE_ROOT"
	// AppServerURLEnv and SessionManagerURLEnv point at the in-process
	// services the parent is attached to.
	AppServerURLEnv      = "TASK_BOARD_CODEX_APP_SERVER_URL"
	SessionManagerURLEnv = "TASK_BOARD_SESSION_MANAGER_URL"
	// AppServerTokenNameEnv and SessionManagerTokenNameEnv do not hold
	// credentials — they hold the NAME of the variable that does. Stripping
	// only the pointer would leave the token itself in the child, which is why
	// Filter reads each pointer and blocks what it names.
	AppServerTokenNameEnv      = "TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV"
	SessionManagerTokenNameEnv = "TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV"
	// SessionIDEnv is the parent's manager-side session.
	SessionIDEnv = "TASK_BOARD_SESSION_ID"
)

// keys is the fixed part of the blocked set, in the source's order.
//
// It is a package-level var rather than an inline literal so a caller's test
// can range over it — the codex and qwen plugins each narrow their filter one
// key at a time and show which golden entry that key is responsible for. A list
// nothing can narrow is a list whose entries nobody can tell apart.
var keys = []string{
	ThreadIDEnv,
	SessionEnv,
	CIEnv,
	ManagedByNPMEnv,
	ManagedByBunEnv,
	ManagedPackageRootEnv,
	AppServerURLEnv,
	AppServerTokenNameEnv,
	SessionManagerURLEnv,
	SessionManagerTokenNameEnv,
	SessionIDEnv,
}

// Keys returns the fixed blocked set, copied so a caller cannot rewrite the
// list every other caller reads.
func Keys() []string { return append([]string(nil), keys...) }

// Filter strips the parent codex runtime state from an environment and
// sanitizes PATH. It is the source's filterCodexRuntimeEnv.
//
// The two token-NAME pointers are resolved against the environment being
// filtered, and whatever they name is blocked too. Reading them from the INPUT
// rather than from the process is what makes this function total: the same
// environment decides both which keys are blocked and which entries survive, so
// the answer does not depend on when it is called.
func Filter(environ []string) []string {
	blocked := Keys()
	if tokenEnvName := launchenv.Value(environ, AppServerTokenNameEnv); tokenEnvName != "" {
		blocked = append(blocked, tokenEnvName)
	}
	if tokenEnvName := launchenv.Value(environ, SessionManagerTokenNameEnv); tokenEnvName != "" {
		blocked = append(blocked, tokenEnvName)
	}
	return SanitizePath(FilterKeys(environ, blocked...))
}

// FilterKeys returns a copy of environ with the named keys removed.
//
// Matching is on the WHOLE key, via a blocked-key map. That is not an
// implementation detail to tidy up: `strings.HasPrefix(key, "CODEX_")` is the
// plausible one-character-different reading of this function, and it silently
// steals every CODEX_-shaped variable an operator set for their own reasons.
// The parity goldens seed CODEX_LIKE_BUT_NOT, CLAUDECODE_LIKE_BUT_NOT and
// TASK_BOARD_LIKE_BUT_NOT precisely so a prefix port fails, and each plugin
// that uses this drives that defect through itself.
//
// An entry with no `=` is treated as a bare key, which is what the source does
// and what keeps a malformed entry from being silently reinterpreted as a
// value.
func FilterKeys(environ []string, blocked ...string) []string {
	set := make(map[string]struct{}, len(blocked))
	for _, key := range blocked {
		key = strings.TrimSpace(key)
		if key != "" {
			set[key] = struct{}{}
		}
	}

	var result []string
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			key = entry
		}
		if _, drop := set[key]; !drop {
			result = append(result, entry)
		}
	}
	return result
}

// SanitizePath rewrites the PATH entry, dropping the directories a parent codex
// runtime injected into it.
//
// This is the one strip the parity goldens CANNOT see: the harness that
// captured them excludes PATH from its environment diff, because the capture
// seeds PATH with a temp directory and its value differs run to run. The
// goldens' own README names the consequence — "a port that changes what it
// strips from PATH is outside what these goldens prove. That needs its own
// test." runtimeenv_test.go and each caller's own path test are that evidence.
func SanitizePath(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key != "PATH" {
			result = append(result, entry)
			continue
		}

		parts := filepath.SplitList(value)
		clean := parts[:0]
		for _, part := range parts {
			if IsRuntimePathEntry(part) {
				continue
			}
			clean = append(clean, part)
		}
		result = append(result, "PATH="+strings.Join(clean, string(os.PathListSeparator)))
	}
	return result
}

// IsRuntimePathEntry reports whether a PATH element was injected by a parent
// codex runtime: the per-invocation arg0 shim directory it creates under its
// own home, or a directory named codex-path.
//
// Both shapes point at the PARENT's install. A child that keeps them resolves
// the parent's binary through a wrapper that no longer has a live parent behind
// it.
func IsRuntimePathEntry(path string) bool {
	path = filepath.Clean(path)
	sep := string(filepath.Separator)
	if strings.Contains(path, sep+".codex"+sep+"tmp"+sep+"arg0"+sep) {
		return true
	}
	return filepath.Base(path) == "codex-path"
}
