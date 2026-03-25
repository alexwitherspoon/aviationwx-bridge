//go:build darwin

package hardware

import "golang.org/x/sys/unix"

func totalMemoryMB() int {
	v, err := unix.SysctlUint64("hw.memsize")
	if err != nil || v == 0 {
		return 0
	}
	return int(v / (1024 * 1024))
}

func maxCPUFrequencyMHz() float64 {
	// Hz on Intel; often 0 on Apple Silicon — treat as unknown (memory-only rule).
	v, err := unix.SysctlUint64("hw.cpufrequency_max")
	if err != nil || v == 0 {
		v, err = unix.SysctlUint64("hw.cpufrequency")
	}
	if err != nil || v == 0 {
		return 0
	}
	return float64(v) / 1e6
}
