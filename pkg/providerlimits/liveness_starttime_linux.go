//go:build linux

package providerlimits

import (
	"fmt"
	"os"
	"time"
)

func kernelProcessStartTime(pid int) (time.Time, error) {
	return readLinuxProcessStartTime(pid, os.ReadFile)
}

func readLinuxProcessStartTime(pid int, readFile func(string) ([]byte, error)) (time.Time, error) {
	stat, err := readFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, err
	}
	procStat, err := readFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	auxv, err := readFile("/proc/self/auxv")
	if err != nil {
		return time.Time{}, err
	}
	clockTicksPerSecond, err := parseLinuxAuxvClockTicks(auxv)
	if err != nil {
		return time.Time{}, err
	}
	return linuxProcessStartTimeFromProcData(stat, procStat, clockTicksPerSecond)
}
