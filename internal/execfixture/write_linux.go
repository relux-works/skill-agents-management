//go:build linux

package execfixture

import "syscall"

func lockExecutableWrite() func() {
	// Go's fork path takes the write side. On Linux file creation uses
	// O_CLOEXEC atomically, so it does not recursively take this read lock.
	syscall.ForkLock.RLock()
	return syscall.ForkLock.RUnlock
}
