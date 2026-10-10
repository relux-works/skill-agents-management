// Package execfixture publishes executable test fixtures without exposing an
// open writer to an exec, including a concurrent test's forked child.
package execfixture

import (
	"os"
	"path/filepath"
)

// WriteFile writes, syncs and closes a private sibling before publishing it by
// rename. Replacements get a new inode: an executing old fixture is untouched.
// The Linux fork interlock also prevents a concurrent child from inheriting the
// temporary write descriptor before close-on-exec takes effect (ETXTBSY).
// Non-executable files retain os.WriteFile semantics for fixture tree copiers.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	if mode.Perm()&0o111 == 0 {
		return os.WriteFile(path, data, mode)
	}
	unlock := lockExecutableWrite()
	defer unlock()
	f, err := os.CreateTemp(filepath.Dir(path), ".exec-fixture-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
