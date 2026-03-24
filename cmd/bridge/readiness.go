package main

import (
	"fmt"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/scheduler"
)

// evalCaptureReadiness implements /readyz camera checks. now is injectable for tests.
//
// grace: no staleness checks until orchestrator uptime exceeds this.
// minStale: minimum window; per-camera threshold is max(minStale, 3*capture interval).
// For cameras with no LastSuccess yet, not-ready applies only after uptime exceeds that
// threshold (same window as for stale successes), avoiding spurious 503s for long-interval cameras.
func evalCaptureReadiness(grace, minStale time.Duration, orch scheduler.OrchestratorStatus, now time.Time) (bool, string) {
	if !orch.Running {
		return false, "orchestrator not running"
	}
	if orch.CameraCount == 0 {
		return true, ""
	}
	if orch.Uptime < grace {
		return true, ""
	}

	for _, cs := range orch.CameraStats {
		interval := cs.CaptureStats.Interval
		if interval <= 0 {
			interval = 60 * time.Second
		}
		threshold := maxDuration(minStale, 3*interval)

		if cs.LastSuccess.IsZero() {
			if orch.Uptime > threshold {
				return false, fmt.Sprintf("camera %s: no successful capture yet after %s uptime (threshold %s)",
					cs.CameraID, orch.Uptime.Round(time.Second), threshold)
			}
			continue
		}

		since := now.Sub(cs.LastSuccess)
		if since > threshold {
			return false, fmt.Sprintf("camera %s: last success %s ago (threshold %s)",
				cs.CameraID, since.Round(time.Second), threshold)
		}
	}

	return true, ""
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
