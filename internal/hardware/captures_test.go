package hardware

import (
	"testing"
)

func TestComputeDefaultMaxConcurrentCaptures(t *testing.T) {
	tests := []struct {
		name     string
		totalMB  int
		numCPU   int
		mhz      float64
		expected int
	}{
		{
			name:     "512mb_four_core_1ghz_slow",
			totalMB:  512,
			numCPU:   4,
			mhz:      1000,
			expected: 1,
		},
		{
			name:     "512mb_four_core_3ghz_fast_memory_only",
			totalMB:  512,
			numCPU:   4,
			mhz:      3000,
			expected: 1,
		},
		{
			name:     "1024mb_four_core_1ghz_slow_min_mem_and_cpu",
			totalMB:  1024,
			numCPU:   4,
			mhz:      1500,
			expected: 2,
		},
		{
			name:     "4096mb_four_core_1_5ghz_slow_cpu_caps_at_2",
			totalMB:  4096,
			numCPU:   4,
			mhz:      1500,
			expected: 2,
		},
		{
			name:     "8192mb_eight_core_3ghz_fast_hits_cap",
			totalMB:  8192,
			numCPU:   8,
			mhz:      3500,
			expected: 10,
		},
		{
			name:     "16384mb_eight_core_3ghz_fast",
			totalMB:  16384,
			numCPU:   8,
			mhz:      3000,
			expected: 10,
		},
		{
			name:     "unknown_freq_uses_memory_only",
			totalMB:  2048,
			numCPU:   2,
			mhz:      0,
			expected: 4,
		},
		{
			name:     "zero_mem_defaults_to_one_slot",
			totalMB:  0,
			numCPU:   8,
			mhz:      1000,
			expected: 1,
		},
		{
			name:     "zero_mem_unknown_freq_still_one_slot",
			totalMB:  0,
			numCPU:   8,
			mhz:      0,
			expected: 1,
		},
		{
			name:     "single_core_slow",
			totalMB:  2048,
			numCPU:   1,
			mhz:      1200,
			expected: 1,
		},
		{
			name:     "freq_exactly_2000_treated_as_fast_memory_only",
			totalMB:  2048,
			numCPU:   2,
			mhz:      2000,
			expected: 4,
		},
		{
			name:     "freq_just_below_slow_threshold_uses_cpu_cap",
			totalMB:  8192,
			numCPU:   8,
			mhz:      1999,
			expected: 4,
		},
		{
			name:     "mem_slots_exceed_cap_clamped_to_ten",
			totalMB:  65536,
			numCPU:   32,
			mhz:      3000,
			expected: 10,
		},
		{
			name:     "negative_total_mb_clamped_to_one_mem_slot",
			totalMB:  -100,
			numCPU:   8,
			mhz:      3000,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeDefaultMaxConcurrentCaptures(tt.totalMB, tt.numCPU, tt.mhz)
			if got != tt.expected {
				t.Fatalf("computeDefaultMaxConcurrentCaptures(%d MB, %d CPUs, %.1f MHz) = %d, want %d",
					tt.totalMB, tt.numCPU, tt.mhz, got, tt.expected)
			}
		})
	}
}

func TestParseMemTotalMBFromMeminfo(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name: "typical_memtotal_kb",
			content: `MemTotal:        1024000 kB
MemFree:          512000 kB
`,
			expected: 1000,
		},
		{name: "empty", content: "", expected: 0},
		{name: "no_memtotal_line", content: "MemFree: 100 kB\n", expected: 0},
		{name: "malformed_value", content: "MemTotal:        xyz kB\n", expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMemTotalMBFromMeminfo([]byte(tt.content))
			if got != tt.expected {
				t.Fatalf("parseMemTotalMBFromMeminfo(...) = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCPUFreqMaxMHzFromKHzFileContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected float64
	}{
		{name: "raspberry_pi_style", content: "1500000\n", expected: 1500},
		{name: "with_whitespace", content: "  2400000  \n", expected: 2400},
		{name: "invalid", content: "nope", expected: 0},
		{name: "zero", content: "0", expected: 0},
		{name: "negative", content: "-1", expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cpufreqMaxMHzFromKHzFileContent([]byte(tt.content))
			if got != tt.expected {
				t.Fatalf("cpufreqMaxMHzFromKHzFileContent(...) = %g, want %g", got, tt.expected)
			}
		})
	}
}

func TestParseMaxMHzFromCPUInfo(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected float64
	}{
		{
			name: "arm_cpu_max_mhz",
			content: `
processor	: 0
CPU max MHz	: 1500.0000
`,
			expected: 1500,
		},
		{
			name: "x86_cpu_mhz_multiple_takes_max",
			content: `
cpu MHz		: 1200.000
cpu MHz		: 2400.000
`,
			expected: 2400,
		},
		{
			name:     "empty",
			content:  "",
			expected: 0,
		},
		{
			name:     "no_mhz_lines",
			content:  "processor\t: 0\n",
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMaxMHzFromCPUInfo([]byte(tt.content))
			if got != tt.expected {
				t.Fatalf("parseMaxMHzFromCPUInfo(...) = %g, want %g", got, tt.expected)
			}
		})
	}
}
