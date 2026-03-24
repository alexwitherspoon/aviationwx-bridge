package main

import (
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/scheduler"
)

func TestEvalCaptureReadiness(t *testing.T) {
	grace := 10 * time.Minute
	minStale := 15 * time.Minute
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	t.Run("orchestrator_not_running", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{Running: false}, now)
		if ok || msg != "orchestrator not running" {
			t.Fatalf("want not ready / orchestrator not running; ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("no_cameras", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 0,
		}, now)
		if !ok || msg != "" {
			t.Fatalf("want ready; ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("inside_grace", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      5 * time.Minute,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: time.Hour,
					},
					LastSuccess: time.Time{},
				},
			},
		}, now)
		if !ok || msg != "" {
			t.Fatalf("want ready during grace; ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("never_captured_still_within_first_capture_window", func(t *testing.T) {
		interval := 60 * time.Minute
		threshold := maxDuration(minStale, 3*interval) // 180m
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      100 * time.Minute,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: interval,
					},
					LastSuccess: time.Time{},
				},
			},
		}, now)
		if !ok || msg != "" {
			t.Fatalf("want ready before threshold uptime=%v threshold=%v; ok=%v msg=%q", 100*time.Minute, threshold, ok, msg)
		}
	})

	t.Run("never_captured_past_threshold", func(t *testing.T) {
		interval := 60 * time.Minute
		threshold := maxDuration(minStale, 3*interval)
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      200 * time.Minute,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: interval,
					},
					LastSuccess: time.Time{},
				},
			},
		}, now)
		if ok || !strings.Contains(msg, "no successful capture yet") || !strings.Contains(msg, "a") {
			t.Fatalf("want not ready with reason; ok=%v msg=%q threshold=%v", ok, msg, threshold)
		}
	})

	t.Run("success_stale", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      2 * time.Hour,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: 60 * time.Second,
					},
					LastSuccess: now.Add(-2 * time.Hour),
				},
			},
		}, now)
		if ok || !strings.Contains(msg, "last success") {
			t.Fatalf("want stale not ready; ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("success_fresh", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      2 * time.Hour,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: 60 * time.Second,
					},
					LastSuccess: now.Add(-5 * time.Minute),
				},
			},
		}, now)
		if !ok || msg != "" {
			t.Fatalf("want ready; ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("zero_interval_uses_default", func(t *testing.T) {
		ok, msg := evalCaptureReadiness(grace, minStale, scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 1,
			Uptime:      2 * time.Hour,
			CameraStats: []scheduler.CameraStatus{
				{
					CameraID: "a",
					CaptureStats: scheduler.CaptureStats{
						Interval: 0,
					},
					LastSuccess: now.Add(-10 * time.Minute),
				},
			},
		}, now)
		if !ok || msg != "" {
			t.Fatalf("want ready (default 60s interval -> 180s threshold); ok=%v msg=%q", ok, msg)
		}
	})
}
