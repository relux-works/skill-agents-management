//go:build unix && !darwin && !linux

package providerlimits

import "time"

// Platforms without a kernel reader retain the bounded ps compatibility path.
func kernelProcessStartTime(int) (time.Time, error) {
	return time.Time{}, errKernelStartTimeUnavailable
}
