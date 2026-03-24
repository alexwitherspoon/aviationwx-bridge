package hardware

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Budget: ~500 MB total RAM per concurrent capture (e.g. 512 MB system → 1; 1 GiB → 2).
const mbPerConcurrentCapture = 500

// Below this max CPU frequency (MHz), concurrent captures are also limited to about
// one per two logical CPUs (slow cores benefit from fewer overlapping captures).
const slowCPUMaxMHz = 2000

// defaultMaxConcurrentCapturesCap matches scheduler maxConcurrentCapturesCap / web UI.
const defaultMaxConcurrentCapturesCap = 10

// DefaultMaxConcurrentCaptures returns a default when the user has not set max_concurrent_captures.
// It uses total RAM (~500 MB per slot) and, on CPUs reporting max frequency below 2 GHz,
// the minimum of that and (logical CPUs / 2). If total RAM cannot be determined, the
// memory-based slot count is 1 (conservative). Unknown frequency is treated as "fast" so
// desktops without cpufreq are not over-restricted.
func DefaultMaxConcurrentCaptures() int {
	return computeDefaultMaxConcurrentCaptures(totalMemoryMB(), runtime.NumCPU(), maxCPUFrequencyMHz())
}

func computeDefaultMaxConcurrentCaptures(totalMB, numCPU int, maxFreqMHz float64) int {
	memSlots := totalMB / mbPerConcurrentCapture
	if memSlots < 1 {
		memSlots = 1
	}
	if memSlots > defaultMaxConcurrentCapturesCap {
		memSlots = defaultMaxConcurrentCapturesCap
	}

	// Unknown frequency: memory only (typical for dev laptops without cpufreq data).
	if maxFreqMHz <= 0 || maxFreqMHz >= slowCPUMaxMHz {
		return memSlots
	}

	cpuSlots := numCPU / 2
	if cpuSlots < 1 {
		cpuSlots = 1
	}
	if cpuSlots > defaultMaxConcurrentCapturesCap {
		cpuSlots = defaultMaxConcurrentCapturesCap
	}
	if cpuSlots < memSlots {
		return cpuSlots
	}
	return memSlots
}

// totalMemoryMB and maxCPUFrequencyMHz are implemented per-GOOS in captures_*.go
// (Darwin uses sysctl APIs that are not in golang.org/x/sys/unix on Linux.)

func memTotalMBLinux() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	return parseMemTotalMBFromMeminfo(data)
}

// parseMemTotalMBFromMeminfo returns MemTotal in MB from /proc/meminfo-style content, or 0 if missing/invalid.
func parseMemTotalMBFromMeminfo(data []byte) int {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return int(memKB / 1024)
				}
			}
			break
		}
	}
	return 0
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

// cpufreqMaxMHzFromKHzFileContent parses a single integer kHz line from cpufreq sysfs (e.g. "1500000\n").
func cpufreqMaxMHzFromKHzFileContent(b []byte) float64 {
	s := strings.TrimSpace(string(b))
	khz, err := strconv.ParseInt(s, 10, 64)
	if err != nil || khz <= 0 {
		return 0
	}
	return float64(khz) / 1000.0
}

func linuxMaxCPUFrequencyMHzFromProcCPUInfo() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	return parseMaxMHzFromCPUInfo(data)
}

// parseMaxMHzFromCPUInfo returns the highest reported MHz from /proc/cpuinfo-style content (ARM/x86 variants).
func parseMaxMHzFromCPUInfo(data []byte) float64 {
	var best float64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"cpu MHz", "CPU max MHz"} {
			if strings.HasPrefix(line, prefix) {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err != nil {
					continue
				}
				if v > best {
					best = v
				}
			}
		}
	}
	return best
}
