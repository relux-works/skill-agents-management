// Package planningio owns filesystem and executable lookups used by the core
// planner and Codex. Keeping these operations behind one internal boundary lets
// a caller in this module observe attempted reads, including failed reads whose
// results are ignored. It does not change the public launch contract.
package planningio

import (
	"os"
	"path/filepath"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// Reader includes operations which can consult the filesystem. OpenFile also
// covers materialisation: an attempted write must not precede validation either.
// Calls count operations, not individual syscalls inside the host implementation.
type Reader interface {
	ReadFile(string) ([]byte, error)
	Open(string) (*os.File, error)
	OpenFile(string, int, os.FileMode) (*os.File, error)
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	Abs(string) (string, error)
	EvalSymlinks(string) (string, error)
	LookPath([]string, string) (string, error)
	IsRegularFile(string) bool
}

// Default is immutable in production. Package tests may replace it only in a
// sequential test, restoring it before parallel tests resume. It is internal to
// the module; consumers cannot replace the planner's filesystem authority.
var Default Reader = Host{}

// Host preserves the existing OS and launch-environment rules. Codex keeps
// its bounded native-catalog reader in the plugin, so importing the core does
// not pull in the catalog reader's platform-specific syscall dependency.
type Host struct{}

func (Host) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (Host) Open(path string) (*os.File, error)   { return os.Open(path) }
func (Host) OpenFile(path string, flag int, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, mode)
}
func (Host) Stat(path string) (os.FileInfo, error)              { return os.Stat(path) }
func (Host) Lstat(path string) (os.FileInfo, error)             { return os.Lstat(path) }
func (Host) Abs(path string) (string, error)                    { return filepath.Abs(path) }
func (Host) EvalSymlinks(path string) (string, error)           { return filepath.EvalSymlinks(path) }
func (Host) LookPath(env []string, name string) (string, error) { return launchenv.LookPath(env, name) }
func (Host) IsRegularFile(path string) bool                     { return launchenv.IsRegularFile(path) }
