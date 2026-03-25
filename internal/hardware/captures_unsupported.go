//go:build !linux && !darwin

package hardware

func totalMemoryMB() int {
	return 0
}

func maxCPUFrequencyMHz() float64 {
	return 0
}
