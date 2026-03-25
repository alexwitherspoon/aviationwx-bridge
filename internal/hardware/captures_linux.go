//go:build linux

package hardware

func totalMemoryMB() int {
	return memTotalMBLinux()
}

func maxCPUFrequencyMHz() float64 {
	return linuxMaxCPUFrequencyMHz()
}
