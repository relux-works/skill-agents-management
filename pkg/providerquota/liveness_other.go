//go:build !unix

package providerquota

// Without a reliable signal-zero probe, a positive PID might be live. Keep
// its lock; a consumer can provide a platform-specific OwnerAlive function.
func processAlive(pid int) bool { return pid > 0 }
