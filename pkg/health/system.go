package health

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level represents a health status level
type Level string

const (
	LevelHealthy  Level = "healthy"  // Green - everything is fine
	LevelWarning  Level = "warning"  // Yellow - approaching limits
	LevelCritical Level = "critical" // Red - at or exceeding limits
)

// Thresholds for health levels (percentages)
const (
	CPUWarningThreshold   = 70.0
	CPUCriticalThreshold  = 90.0
	MemWarningThreshold   = 70.0
	MemCriticalThreshold  = 85.0
	DiskWarningThreshold  = 70.0
	DiskCriticalThreshold = 85.0
)

// SystemStats holds current system resource statistics
type SystemStats struct {
	// CPU
	CPUPercent    float64 `json:"cpu_percent"`
	CPULevel      Level   `json:"cpu_level"`
	NumGoroutines int     `json:"num_goroutines"`
	NumCPU        int     `json:"num_cpu"`

	// Memory
	MemUsedMB   float64 `json:"mem_used_mb"`
	MemTotalMB  float64 `json:"mem_total_mb"`
	MemPercent  float64 `json:"mem_percent"`
	MemLevel    Level   `json:"mem_level"`
	HeapAllocMB float64 `json:"heap_alloc_mb"`
	HeapSysMB   float64 `json:"heap_sys_mb"`
	GCPauseMs   float64 `json:"gc_pause_ms"`

	// Disk/Filesystem (for queue tmpfs)
	DiskUsedMB  float64 `json:"disk_used_mb"`
	DiskFreeMB  float64 `json:"disk_free_mb"`
	DiskTotalMB float64 `json:"disk_total_mb"`
	DiskPercent float64 `json:"disk_percent"`
	DiskLevel   Level   `json:"disk_level"`

	// Overall
	OverallLevel Level  `json:"overall_level"`
	Uptime       string `json:"uptime"`
}

// SystemMonitor tracks system resource usage over time
type SystemMonitor struct {
	mu            sync.RWMutex
	startTime     time.Time
	lastCPUTime   time.Time
	lastCPUTotal  uint64
	lastCPUIdle   uint64
	currentStats  SystemStats
	queueBasePath string
}

// NewSystemMonitor creates a new system monitor
func NewSystemMonitor(queueBasePath string) *SystemMonitor {
	m := &SystemMonitor{
		startTime:     time.Now(),
		queueBasePath: queueBasePath,
	}
	// Initialize CPU tracking
	m.lastCPUTotal, m.lastCPUIdle = readCPUStats()
	m.lastCPUTime = time.Now()
	return m
}

// GetStats returns current system statistics
func (m *SystemMonitor) GetStats() SystemStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := SystemStats{
		NumCPU:        runtime.NumCPU(),
		NumGoroutines: runtime.NumGoroutine(),
		Uptime:        time.Since(m.startTime).Round(time.Second).String(),
	}

	// CPU stats
	stats.CPUPercent = m.calculateCPUPercent()
	stats.CPULevel = getLevelFromPercent(stats.CPUPercent, CPUWarningThreshold, CPUCriticalThreshold)

	// Memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	stats.HeapAllocMB = float64(memStats.HeapAlloc) / (1024 * 1024)
	stats.HeapSysMB = float64(memStats.HeapSys) / (1024 * 1024)
	if memStats.NumGC > 0 {
		stats.GCPauseMs = float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6
	}

	// System memory (from /proc/meminfo on Linux, estimate on other platforms)
	stats.MemUsedMB, stats.MemTotalMB = getSystemMemory()
	if stats.MemTotalMB > 0 {
		stats.MemPercent = (stats.MemUsedMB / stats.MemTotalMB) * 100
	}
	stats.MemLevel = getLevelFromPercent(stats.MemPercent, MemWarningThreshold, MemCriticalThreshold)

	// Disk stats (tmpfs for queue)
	stats.DiskUsedMB, stats.DiskFreeMB, stats.DiskTotalMB = m.getDiskStats()
	if stats.DiskTotalMB > 0 {
		stats.DiskPercent = (stats.DiskUsedMB / stats.DiskTotalMB) * 100
	}
	stats.DiskLevel = getLevelFromPercent(stats.DiskPercent, DiskWarningThreshold, DiskCriticalThreshold)

	// Overall level is the worst of all levels
	stats.OverallLevel = worstLevel(stats.CPULevel, stats.MemLevel, stats.DiskLevel)

	m.currentStats = stats
	return stats
}

// calculateCPUPercent calculates CPU usage since last check
func (m *SystemMonitor) calculateCPUPercent() float64 {
	currentTotal, currentIdle := readCPUStats()
	now := time.Now()

	// Calculate deltas
	elapsed := now.Sub(m.lastCPUTime).Seconds()
	if elapsed < 0.1 {
		// Not enough time passed, return last value
		return m.currentStats.CPUPercent
	}

	totalDelta := currentTotal - m.lastCPUTotal
	idleDelta := currentIdle - m.lastCPUIdle

	// Update tracking
	m.lastCPUTotal = currentTotal
	m.lastCPUIdle = currentIdle
	m.lastCPUTime = now

	if totalDelta == 0 {
		return 0
	}

	// CPU% = (1 - idle/total) * 100
	cpuPercent := (1.0 - float64(idleDelta)/float64(totalDelta)) * 100
	if cpuPercent < 0 {
		cpuPercent = 0
	}
	if cpuPercent > 100 {
		cpuPercent = 100
	}

	return cpuPercent
}

// getDiskStats gets disk usage for the queue path
func (m *SystemMonitor) getDiskStats() (usedMB, freeMB, totalMB float64) {
	if m.queueBasePath == "" {
		return 0, 0, 0
	}

	// Use statfs to get filesystem stats
	// This is implemented in the queue package, so we do a simple file-based check here
	info, err := os.Stat(m.queueBasePath)
	if err != nil {
		return 0, 0, 0
	}
	_ = info // We need syscall for proper stats, handled by caller

	return 0, 0, 0 // Placeholder - actual values come from queue manager
}

// readCPUStats reads CPU statistics from /proc/stat (Linux)
func readCPUStats() (total, idle uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				// cpu user nice system idle iowait irq softirq steal guest guest_nice
				var total uint64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseUint(fields[i], 10, 64)
					total += v
				}
				idle, _ := strconv.ParseUint(fields[4], 10, 64)
				return total, idle
			}
		}
	}
	return 0, 0
}

// getSystemMemory reads system memory from /proc/meminfo (Linux)
func getSystemMemory() (usedMB, totalMB float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		// Non-Linux: estimate from Go runtime
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.Sys) / (1024 * 1024), float64(m.Sys*2) / (1024 * 1024)
	}

	var memTotal, memAvailable, memFree, buffers, cached uint64

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			memTotal = value
		case "MemAvailable:":
			memAvailable = value
		case "MemFree:":
			memFree = value
		case "Buffers:":
			buffers = value
		case "Cached:":
			cached = value
		}
	}

	// Total in MB
	totalMB = float64(memTotal) / 1024

	// Used = Total - Available (or Total - Free - Buffers - Cached if Available not present)
	if memAvailable > 0 {
		usedMB = float64(memTotal-memAvailable) / 1024
	} else {
		usedMB = float64(memTotal-memFree-buffers-cached) / 1024
	}

	return usedMB, totalMB
}

// getLevelFromPercent returns a health level based on percentage and thresholds
func getLevelFromPercent(percent, warningThreshold, criticalThreshold float64) Level {
	switch {
	case percent >= criticalThreshold:
		return LevelCritical
	case percent >= warningThreshold:
		return LevelWarning
	default:
		return LevelHealthy
	}
}

// worstLevel returns the worst (most critical) level from the given levels
func worstLevel(levels ...Level) Level {
	worst := LevelHealthy
	for _, l := range levels {
		if l == LevelCritical {
			return LevelCritical
		}
		if l == LevelWarning && worst == LevelHealthy {
			worst = LevelWarning
		}
	}
	return worst
}
