//go:build unix

package providerquota

import "golang.org/x/sys/unix"

func processAlive(pid int) bool {
	err := unix.Kill(pid, 0)
	return err == nil || err != unix.ESRCH
}
