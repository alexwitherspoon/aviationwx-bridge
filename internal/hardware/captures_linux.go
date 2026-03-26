//go:build linux

package hardware

import "os"

func totalMemoryMB() int {
	return memTotalMBLinux()
}

func maxCPUFrequencyMHz() float64 {
	return linuxMaxCPUFrequencyMHz()
}

func memTotalMBLinux() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	return parseMemTotalMBFromMeminfo(data)
}

func linuxMaxCPUFrequencyMHz() float64 {
	paths := []string{
		"/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq",
		"/sys/devices/system/cpu/cpufreq/policy0/cpuinfo_max_freq",
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if mhz := cpufreqMaxMHzFromKHzFileContent(b); mhz > 0 {
			return mhz
		}
	}
	return linuxMaxCPUFrequencyMHzFromProcCPUInfo()
}

func linuxMaxCPUFrequencyMHzFromProcCPUInfo() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	return parseMaxMHzFromCPUInfo(data)
}
