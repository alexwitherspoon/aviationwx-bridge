package resource

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
	})

	if l == nil {
		t.Fatal("expected non-nil limiter")
	}

	stats := l.GetStats()
	if stats.MaxImageProcessing != 2 {
		t.Errorf("expected MaxImageProcessing=2, got %d", stats.MaxImageProcessing)
	}
	if stats.MaxExifOperations != 1 {
		t.Errorf("expected MaxExifOperations=1, got %d", stats.MaxExifOperations)
	}
}

func TestDefaultLimiter(t *testing.T) {
	l := DefaultLimiter()
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}

	stats := l.GetStats()
	if stats.MaxImageProcessing < 1 {
		t.Errorf("expected MaxImageProcessing >= 1, got %d", stats.MaxImageProcessing)
	}
	if stats.MaxExifOperations != 1 {
		t.Errorf("expected MaxExifOperations=1, got %d", stats.MaxExifOperations)
	}
}

func TestImageProcessingSemaphore(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
	})

	ctx := context.Background()

	// Acquire two slots (should succeed immediately)
	if err := l.AcquireImageProcessing(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := l.AcquireImageProcessing(ctx); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	// Third acquire should block, use TryAcquire to test without blocking
	if l.TryAcquireImageProcessing() {
		t.Error("expected TryAcquire to fail when at capacity")
		l.ReleaseImageProcessing()
	}

	// Release one and try again
	l.ReleaseImageProcessing()
	if !l.TryAcquireImageProcessing() {
		t.Error("expected TryAcquire to succeed after release")
	}

	// Clean up
	l.ReleaseImageProcessing()
	l.ReleaseImageProcessing()
}

func TestExifOperationsSemaphore(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
	})

	ctx := context.Background()

	// Acquire one slot (should succeed)
	if err := l.AcquireExifOperation(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second should fail with TryAcquire
	if l.TryAcquireExifOperation() {
		t.Error("expected TryAcquire to fail when at capacity")
		l.ReleaseExifOperation()
	}

	// Release and try again
	l.ReleaseExifOperation()
	if !l.TryAcquireExifOperation() {
		t.Error("expected TryAcquire to succeed after release")
	}
	l.ReleaseExifOperation()
}

func TestAcquireWithContextCancellation(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 1,
		MaxConcurrentExifOperations:  1,
	})

	// Acquire the only slot
	ctx := context.Background()
	if err := l.AcquireImageProcessing(ctx); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Try to acquire with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := l.AcquireImageProcessing(cancelledCtx)
	if err == nil {
		t.Error("expected error when context is cancelled")
		l.ReleaseImageProcessing()
	}

	l.ReleaseImageProcessing()
}

func TestAcquireWithTimeout(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 1,
		MaxConcurrentExifOperations:  1,
	})

	// Acquire the only slot
	ctx := context.Background()
	if err := l.AcquireImageProcessing(ctx); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Try to acquire with short timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.AcquireImageProcessing(timeoutCtx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error when context times out")
		l.ReleaseImageProcessing()
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected to wait at least 10ms, waited %v", elapsed)
	}

	l.ReleaseImageProcessing()
}

func TestConcurrentAcquireRelease(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 3,
		MaxConcurrentExifOperations:  1,
	})

	const numGoroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	var totalOps atomic.Int64
	ctx := context.Background()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				if err := l.AcquireImageProcessing(ctx); err != nil {
					t.Errorf("acquire failed: %v", err)
					return
				}
				totalOps.Add(1)
				// Simulate work
				time.Sleep(time.Microsecond)
				l.ReleaseImageProcessing()
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * opsPerGoroutine)
	if totalOps.Load() != expected {
		t.Errorf("expected %d total ops, got %d", expected, totalOps.Load())
	}

	stats := l.GetStats()
	if stats.ImageAcquireCount != expected {
		t.Errorf("expected %d acquires in stats, got %d", expected, stats.ImageAcquireCount)
	}
}

func TestThrottleDelay(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
		MemoryPressureThresholdMB:    1, // Very low threshold to trigger throttling
		GoroutinePressureThreshold:   1, // Very low threshold
		MaxThrottleDelay:             100 * time.Millisecond,
		PressureCheckInterval:        10 * time.Millisecond,
	})

	// With such low thresholds, we should get some throttle delay
	delay := l.GetThrottleDelay()
	// Can't predict exact value, but it should be non-negative
	if delay < 0 {
		t.Errorf("expected non-negative delay, got %v", delay)
	}
}

func TestPressureCalculation(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
		MemoryPressureThresholdMB:    100000, // Very high threshold
		GoroutinePressureThreshold:   100000, // Very high threshold
	})

	// With very high thresholds, pressure should be near 0
	pressure := l.calculatePressure()
	if pressure > 0.1 {
		t.Errorf("expected low pressure with high thresholds, got %v", pressure)
	}

	// Check that IsUnderPressure returns false
	if l.IsUnderPressure() {
		t.Error("expected IsUnderPressure to be false with high thresholds")
	}
}

func TestGetStats(t *testing.T) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 2,
		MaxConcurrentExifOperations:  1,
	})

	ctx := context.Background()

	// Do some operations
	l.AcquireImageProcessing(ctx)
	l.ReleaseImageProcessing()
	l.AcquireExifOperation(ctx)
	l.ReleaseExifOperation()

	stats := l.GetStats()

	if stats.ImageAcquireCount != 1 {
		t.Errorf("expected ImageAcquireCount=1, got %d", stats.ImageAcquireCount)
	}
	if stats.ExifAcquireCount != 1 {
		t.Errorf("expected ExifAcquireCount=1, got %d", stats.ExifAcquireCount)
	}
	if stats.NumCPU < 1 {
		t.Errorf("expected NumCPU >= 1, got %d", stats.NumCPU)
	}
	if stats.NumGoroutines < 1 {
		t.Errorf("expected NumGoroutines >= 1, got %d", stats.NumGoroutines)
	}
}

func TestYieldToHigherPriority(t *testing.T) {
	// This is a simple test to ensure the function doesn't panic
	// The actual behavior is hard to test directly
	YieldToHigherPriority()
}

func TestConfigDefaults(t *testing.T) {
	// Test that zero values get defaults applied
	l := NewLimiter(Config{})

	stats := l.GetStats()
	if stats.MaxImageProcessing < 1 {
		t.Errorf("expected MaxImageProcessing >= 1, got %d", stats.MaxImageProcessing)
	}
	if stats.MaxExifOperations != 1 {
		t.Errorf("expected MaxExifOperations=1, got %d", stats.MaxExifOperations)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxConcurrentImageProcessing < 1 {
		t.Errorf("expected MaxConcurrentImageProcessing >= 1, got %d", cfg.MaxConcurrentImageProcessing)
	}
	if cfg.MaxConcurrentExifOperations != 1 {
		t.Errorf("expected MaxConcurrentExifOperations=1, got %d", cfg.MaxConcurrentExifOperations)
	}
	if cfg.MemoryPressureThresholdMB != 200 {
		t.Errorf("expected MemoryPressureThresholdMB=200, got %d", cfg.MemoryPressureThresholdMB)
	}
	if cfg.GoroutinePressureThreshold != 100 {
		t.Errorf("expected GoroutinePressureThreshold=100, got %d", cfg.GoroutinePressureThreshold)
	}
	if cfg.MaxThrottleDelay != 2*time.Second {
		t.Errorf("expected MaxThrottleDelay=2s, got %v", cfg.MaxThrottleDelay)
	}
	if cfg.PressureCheckInterval != time.Second {
		t.Errorf("expected PressureCheckInterval=1s, got %v", cfg.PressureCheckInterval)
	}
}

func BenchmarkAcquireRelease(b *testing.B) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 4,
		MaxConcurrentExifOperations:  1,
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.AcquireImageProcessing(ctx)
		l.ReleaseImageProcessing()
	}
}

func BenchmarkTryAcquireRelease(b *testing.B) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 4,
		MaxConcurrentExifOperations:  1,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if l.TryAcquireImageProcessing() {
			l.ReleaseImageProcessing()
		}
	}
}

func BenchmarkGetThrottleDelay(b *testing.B) {
	l := NewLimiter(Config{
		MaxConcurrentImageProcessing: 4,
		MaxConcurrentExifOperations:  1,
		PressureCheckInterval:        time.Millisecond,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.GetThrottleDelay()
	}
}

func TestGetTotalMemoryMB(t *testing.T) {
	memMB := getTotalMemoryMB()
	
	// Should be able to detect memory on Linux (where /proc/meminfo exists)
	// On non-Linux systems (macOS, Windows), will return 0
	if memMB < 0 {
		t.Errorf("expected non-negative memory, got %d", memMB)
	}
	
	// If running on Linux, should detect some memory
	// Pi Zero 2 W has ~416MB, Pi 4 has 1GB-8GB
	t.Logf("Detected total memory: %d MB", memMB)
}

func TestDefaultConfig_LowMemory(t *testing.T) {
	// Test the adaptive behavior
	cfg := DefaultConfig()
	
	// Max image processing should be 1 or more
	if cfg.MaxConcurrentImageProcessing < 1 {
		t.Errorf("expected MaxConcurrentImageProcessing >= 1, got %d", cfg.MaxConcurrentImageProcessing)
	}
	
	// Exif should always be serialized
	if cfg.MaxConcurrentExifOperations != 1 {
		t.Errorf("expected MaxConcurrentExifOperations=1, got %d", cfg.MaxConcurrentExifOperations)
	}
	
	totalMem := getTotalMemoryMB()
	t.Logf("Total memory: %d MB", totalMem)
	t.Logf("Max concurrent image processing: %d", cfg.MaxConcurrentImageProcessing)
	
	// Verify the logic: < 1GB = serialize, >= 1GB = parallelize
	if totalMem > 0 && totalMem < 1024 {
		if cfg.MaxConcurrentImageProcessing != 1 {
			t.Errorf("expected serialized processing on low-memory device, got %d", cfg.MaxConcurrentImageProcessing)
		}
	}
}
