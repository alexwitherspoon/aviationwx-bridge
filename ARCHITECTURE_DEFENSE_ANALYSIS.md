# AviationWX Bridge - Defensive Architecture Analysis

**Date**: 2026-01-18  
**Version**: v2.0.1 (+ edge commits)  
**Reviewer**: AI Assistant  
**Scope**: Production readiness for minimal-maintenance deployment on resource-constrained devices

---

## Executive Summary

This analysis evaluates AviationWX Bridge's defensive architecture with focus on **host protection**, **auto-recovery**, **state persistence**, and **operational safety** for long-running deployments on devices like Raspberry Pi Zero 2 W (512MB RAM).

### Overall Assessment: **MODERATE-HIGH RISK** ⚠️

**Strengths**:
- Good process isolation (Docker, non-root user)
- Memory protection (soft limits, GC pressure management)
- State separation (config on volume, queue in tmpfs)
- Auto-recovery (Docker restart, health checks, supervisor)

**Critical Gaps**:
- ❌ **No host-level health monitoring** (NTP/network checks don't trigger recovery)
- ❌ **Single-PID failure domain** (all services in one process)
- ❌ **Limited OOM protection** (no cgroup hard limits)
- ❌ **Host reboot on critical failure not implemented**
- ⚠️ **Image processing can block web UI** (shared GOMAXPROCS)

---

## Part 1: Current Architecture Review

### 1.1 Process & Isolation Model

```
┌─────────────────────────────────────────────────────────────┐
│                   Host OS (Raspberry Pi)                      │
│  ┌────────────────────────────────────────────────────────┐  │
│  │          Docker Container (aviationwx-bridge)          │  │
│  │  ┌──────────────────────────────────────────────────┐  │  │
│  │  │          Single Go Process (PID 1)                │  │  │
│  │  │                                                   │  │  │
│  │  │  ┌──────────────┐  ┌───────────────────────┐    │  │  │
│  │  │  │ HTTP Server  │  │  Background Workers   │    │  │  │
│  │  │  │  (Web UI)    │  │  - Capture (N×)       │    │  │  │
│  │  │  │  Port 1229   │  │  - Upload (1×)        │    │  │  │
│  │  │  │              │  │  - Queue Monitor      │    │  │  │
│  │  │  │              │  │  - NTP Health         │    │  │  │
│  │  │  └──────────────┘  └───────────────────────┘    │  │  │
│  │  │                                                   │  │  │
│  │  │  Shared Memory Space (350MB soft limit)         │  │  │
│  │  └──────────────────────────────────────────────────┘  │  │
│  │                                                         │  │
│  │  Volume: /data (persistent config)                     │  │
│  │  tmpfs:  /dev/shm/aviationwx (200MB queue)             │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  Supervisor: aviationwx-supervisor.service (daily checks)    │
│  Daily Restart: /etc/cron.d/aviationwx-restart (3 AM)        │
└─────────────────────────────────────────────────────────────┘
```

**Risk Assessment**:
- ✅ **Good**: Docker isolation, non-root user, read-only capabilities
- ✅ **Good**: State separation (ephemeral queue, persistent config)
- ❌ **Bad**: Single process = single point of failure
- ❌ **Bad**: Web UI and background work share same CPU/memory pool
- ⚠️ **Concern**: PID 1 responsibility (signal handling, zombie reaping)

---

### 1.2 Memory Protection Layers

#### Layer 1: Application-Level (Resource Limiter)
**File**: `internal/resource/limiter.go`

```go
// Soft throttling based on memory pressure
MemoryPressureThresholdMB: 200MB  // Start throttling
MaxThrottleDelay: 2 seconds        // Max work delay
```

**Mechanism**: Adaptive throttling, semaphores for concurrent operations
**Protection**: Slows down background work to prevent memory spikes

#### Layer 2: Go Runtime (Soft Memory Limit)
**File**: `cmd/bridge/main.go:32`

```go
debug.SetMemoryLimit(350 * 1024 * 1024) // 350MB
```

**Mechanism**: Triggers more aggressive GC when approaching limit
**Protection**: Prevents gradual heap growth from hitting OOM

#### Layer 3: Queue Manager (Emergency Thinning)
**File**: `internal/queue/manager.go:178-225`

```go
// Check heap usage and trigger emergency cleanup
if mem.HeapAlloc > maxHeapBytes {
    q.EmergencyThin(0.3) // Keep only 30%
    runtime.GC()         // Force GC
}
```

**Mechanism**: Deletes oldest queued images, forces GC
**Protection**: Last resort before OOM

#### Layer 4: Docker (MISSING - No Hard Limit)
**File**: `docker/docker-compose.yml`

```yaml
# Optional: enforce limits in Docker
# deploy:
#   resources:
#     limits:
#       memory: 400M  # <-- COMMENTED OUT
```

**❌ CRITICAL GAP**: No cgroup memory limit means:
- OOM killer can target other host processes
- No guaranteed boundary at 400MB
- Potential host instability on Pi with 512MB total RAM

---

### 1.3 State Persistence & Data Loss Prevention

| Component | Storage | Persistence | Recovery |
|-----------|---------|-------------|----------|
| **Configuration** | `/data/global.json`, `/data/cameras/*.json` | Docker volume | ✅ Survives restarts, automatic backups (`.bak`) |
| **Image Queue** | `/dev/shm/aviationwx` (tmpfs) | Ephemeral (RAM) | ❌ Lost on restart, **BY DESIGN** |
| **Logs** | Docker logs (10MB max) | Ephemeral | ⚠️ Rotated, compressed, survives short restarts |
| **Preview Cache** | In-memory `lastCaptures` map | Ephemeral | ❌ Lost on restart |
| **Worker State** | In-memory (next capture times, stats) | Ephemeral | ❌ Lost on restart, rebuilt from config |

**Design Philosophy**: **Acceptable Data Loss**
- Queue images are **temporary** (captured again in ~60s)
- Config is **persistent** and **backed up automatically**
- Logs are **diagnostic only** (not operational data)

**Risk Assessment**:
- ✅ **Good**: Critical data (config) is persistent
- ✅ **Good**: Queue loss is acceptable (images recapture quickly)
- ⚠️ **Concern**: No explicit queue high-water mark for "guaranteed delivery"
- ⚠️ **Concern**: No metrics on queue age (how long images wait)

---

### 1.4 Auto-Recovery Mechanisms

#### Mechanism 1: Docker Health Check
**File**: `docker/Dockerfile:71-72`

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:1229/healthz || exit 1
```

**Triggers**: HTTP server responds to `/healthz` endpoint
**Action**: Docker restarts container after 3 failed checks (90 seconds)
**Gap**: Only checks web server, not background workers or queue health

#### Mechanism 2: Docker Restart Policy
**File**: `docker/docker-compose.yml:12`

```yaml
restart: unless-stopped
```

**Triggers**: Container exits (panic, OOM, fatal error)
**Action**: Immediate restart, preserves `/data` volume
**Gap**: Doesn't distinguish between recoverable errors and persistent bugs

#### Mechanism 3: Panic Recovery
**File**: `cmd/bridge/main.go:90-97`

```go
defer func() {
    if r := recover(); r != nil {
        fmt.Fprintf(os.Stderr, "FATAL PANIC: %v\n", r)
        fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
        os.Exit(2)  // Non-zero exit triggers Docker restart
    }
}()
```

**Triggers**: Any unhandled panic in main goroutine
**Action**: Log stack trace, exit with code 2, Docker restarts
**Gap**: Panics in background goroutines are caught but main continues

#### Mechanism 4: Graceful Shutdown
**File**: `cmd/bridge/main.go:232-258`

```go
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
// ... wait for signal ...
orchestrator.Stop()     // Stop workers gracefully
webServer.Stop(ctx)     // 10-second timeout
```

**Triggers**: SIGINT, SIGTERM
**Action**: Orderly shutdown (workers stop, connections drain)
**Gap**: No state snapshot on shutdown (queue is lost)

#### Mechanism 5: Supervisor (Update Management)
**File**: `scripts/supervisor.sh`

**Runs**: Daily via systemd timer (`aviationwx-supervisor.timer`)
**Actions**:
- Check for updates (GitHub releases)
- Pull new image if available
- Rolling update with health check
- Rollback on failure

**Gap**: Only runs daily, doesn't monitor runtime health

#### Mechanism 6: Daily Restart (NEW in v2.0.1)
**File**: `scripts/daily-restart.sh`, installed by `install.sh`

**Runs**: 3 AM daily via `/etc/cron.d/aviationwx-restart`
**Actions**:
- Graceful container stop
- Remove old container
- Start new container
- Wait for health check (120s timeout)
- Log to `/data/aviationwx/restart.log`

**Purpose**: Safety net for edge cases (goroutine leaks, memory fragmentation)
**Gap**: Fixed time, doesn't adapt to capture schedules

---

### 1.5 Host Health Monitoring (MISSING)

**Current Implementation**: ❌ **None**

The application monitors **internal** health:
- ✅ CPU usage (process-level)
- ✅ Memory usage (heap)
- ✅ Queue depth
- ✅ NTP offset
- ✅ Upload success/failure

But does NOT monitor **host-level** health:
- ❌ Network connectivity (beyond upload failures)
- ❌ NTP synchronization **failure recovery** (only reports status)
- ❌ Disk space on host (only `/dev/shm`)
- ❌ Thermal throttling
- ❌ System load average
- ❌ Host-level OOM events

**Critical Scenario**:
```
1. NTP service on host fails (timedatectl shows "not synchronized")
2. Bridge reports NTP offset in dashboard (⚠️ warning)
3. Images continue to upload with incorrect timestamps
4. No automatic remediation (no host reboot, no restart)
5. User discovers days later when timestamps are wrong
```

**Similar Risk**: Network connectivity loss
```
1. Network interface goes down (hardware issue, driver crash)
2. Upload worker fails repeatedly (exponential backoff)
3. Queue fills up, images start getting dropped
4. No host-level recovery attempt (no reboot, no network restart)
```

---

## Part 2: Critical Failure Scenarios

### Scenario 1: Out-of-Memory (OOM) Kill

**Trigger**: Memory leak, image processing spike, or undersized `GOMAXPROCS`

**Current Behavior**:
1. Application heap grows beyond 350MB (soft limit)
2. Go runtime triggers aggressive GC (pauses increase)
3. Queue manager detects heap > 400MB, thins queue to 30%
4. If still growing, Linux OOM killer intervenes
5. Docker restarts container (`restart: unless-stopped`)
6. **Queue is lost** (tmpfs cleared)
7. Config survives (on volume)

**Host Protection**: ⚠️ **PARTIAL**
- ✅ Docker isolation prevents killing other processes
- ❌ No cgroup memory limit means OOM killer has more options
- ❌ No pre-OOM detection (only post-failure recovery)

**Recommendation**: Add Docker memory limit

```yaml
deploy:
  resources:
    limits:
      memory: 400M     # Hard limit
      cpus: '3.0'      # Leave 1 CPU for OS
    reservations:
      memory: 200M     # Minimum guarantee
```

---

### Scenario 2: Image Processing Goroutine Leak

**Trigger**: Bug in capture/upload worker, context not cancelled properly

**Current Behavior**:
1. Worker goroutines accumulate (100 → 200 → 300...)
2. Resource limiter detects goroutine pressure > 100
3. Throttling increases (up to 2-second delays)
4. Memory grows (each goroutine has stack + buffers)
5. Eventually hits OOM or GC thrashing
6. Docker health check fails (web server too slow)
7. Container restarts after 90 seconds

**Host Protection**: ✅ **GOOD**
- Health check catches slow/hung web server
- Restart recovers from leak
- Config survives

**Gap**: 90-second window where web UI is unresponsive

**Recommendation**: Add goroutine count to `/healthz` endpoint

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    numGoroutines := runtime.NumGoroutine()
    if numGoroutines > 500 {  // Critical threshold
        http.Error(w, "Too many goroutines", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "OK (goroutines: %d)\n", numGoroutines)
}
```

---

### Scenario 3: Web UI Blocked by Background Work

**Trigger**: Multiple cameras capturing at same time, heavy EXIF processing

**Current Setup**:
```go
// Reserve one CPU core for web UI
runtime.GOMAXPROCS(numCPU - 1)  // 4-core Pi → 3 for all work
```

**Problem**: Web UI goroutines and background workers **share the same GOMAXPROCS pool**

**Current Behavior**:
1. 4 cameras trigger capture simultaneously
2. Image decode (CPU-bound) saturates 3 cores
3. Web UI HTTP handler tries to respond
4. Go scheduler prioritizes long-running work over short HTTP handlers
5. Web UI feels sluggish (multi-second response times)

**Host Protection**: ✅ **GOOD** (doesn't crash)
**User Experience**: ❌ **BAD** (UI unresponsive during heavy work)

**Recommendation**: Use semaphore for image processing (already implemented!)

```go
// internal/resource/limiter.go (already exists)
MaxConcurrentImageProcessing: 1  // Serialize heavy work
```

But need to verify it's **actually used** in capture flow.

---

### Scenario 4: NTP Failure + No Recovery

**Trigger**: NTP service fails on host, `timedatectl` shows "not synchronized"

**Current Behavior**:
1. Bridge's `TimeHealth` checks NTP via SNTP (UDP port 123)
2. Detects offset > 5 seconds, sets `healthy: false`
3. Dashboard shows "⚠️ Time offset: 15000ms"
4. **No automatic action** (no restart, no host reboot)
5. Images continue to upload with incorrect timestamps
6. Timestamps could be minutes/hours off

**Host Protection**: ❌ **NONE** (time drift is not detected as critical)

**Recommendation**: Add automated recovery:

```go
// Pseudo-code for time health check
if !timeHealth.Healthy && offsetMs > 10000 {  // 10+ seconds off
    log.Error("Critical time drift detected, requesting host NTP restart")
    // Option 1: Restart NTP service (requires host access)
    // Option 2: Exit container, let Docker restart (simple)
    // Option 3: Trigger external watchdog (advanced)
    os.Exit(3)  // Exit code 3 = time sync failure
}
```

Then handle in supervisor:

```bash
# In supervisor.sh or host watchdog
EXIT_CODE=$(docker inspect aviationwx-bridge --format='{{.State.ExitCode}}')
if [ "$EXIT_CODE" == "3" ]; then
    log_warn "Time sync failure detected, restarting NTP"
    systemctl restart systemd-timesyncd  # Or chrony, ntpd
    sleep 5
fi
```

---

### Scenario 5: Network Interface Failure

**Trigger**: Wi-Fi driver crash, Ethernet cable unplugged on headless Pi

**Current Behavior**:
1. Upload worker attempts FTPS connection
2. Connection fails (timeout or refused)
3. Exponential backoff (1s → 2s → 4s → 8s... up to 5 minutes)
4. Queue fills up
5. Queue manager thins queue (emergency cleanup)
6. **No host-level recovery** (no network restart, no reboot)

**Host Protection**: ⚠️ **PARTIAL**
- Queue doesn't overflow (emergency thinning works)
- Container stays alive (no crash)
- But images are being dropped silently

**Recommendation**: Detect persistent network failure and trigger host recovery

```go
// In upload worker backoff logic
if consecutiveFailures > 10 && lastSuccess > 30 minutes ago {
    log.Error("Persistent network failure, attempting recovery")
    
    // Option 1: Test external connectivity
    if !canReachGoogle() && !canReachCloudflare() {
        // Network is definitely down, not just FTP server
        triggerHostReboot()
    }
    
    // Option 2: Request supervisor to restart network
    writeFile("/data/network-recovery-needed", time.Now())
}
```

---

### Scenario 6: Thermal Throttling (Pi-Specific)

**Trigger**: Pi CPU temperature exceeds 80°C, kernel throttles clock speed

**Current Behavior**:
1. CPU throttles from 1.4 GHz to 600 MHz
2. Image processing takes 3× longer
3. Capture workers can't keep up with 60-second intervals
4. Queue starts filling up
5. **No detection** (app doesn't monitor host temperature)
6. User may notice slow web UI, but no alerts

**Host Protection**: ✅ **GOOD** (thermal throttling prevents hardware damage)
**Operational Visibility**: ❌ **NONE** (no thermal metrics in dashboard)

**Recommendation**: Monitor host temperature, adjust workload

```go
// Read from /sys/class/thermal/thermal_zone0/temp (Linux-specific)
func getHostTemperature() (float64, error) {
    data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
    if err != nil {
        return 0, err
    }
    milliC, _ := strconv.Atoi(strings.TrimSpace(string(data)))
    return float64(milliC) / 1000.0, nil
}

// In system monitor
if temp > 75.0 {
    // Reduce GOMAXPROCS, increase throttle delays
    runtime.GOMAXPROCS(2)  // Drop to 2 cores
    resourceLimiter.SetMaxThrottleDelay(5 * time.Second)
}
```

---

## Part 3: Recommendations by Priority

### Priority 1: CRITICAL (Host Protection)

#### 1.1 Add Docker Memory Hard Limit ❌ MISSING
**Risk**: OOM can kill host processes, destabilize Pi
**Fix**: Add cgroup memory limit

```yaml
# docker/docker-compose.yml
deploy:
  resources:
    limits:
      memory: 400M
```

**Testing**: Run stress test, verify OOM killer targets container, not host

---

#### 1.2 Add Persistent Network/Time Failure Recovery ❌ MISSING
**Risk**: Silent data corruption (wrong timestamps), data loss (dropped images)
**Fix**: Implement watchdog-style recovery

**Option A**: Exit container with specific code, supervisor handles recovery
```go
// In timeHealth or uploadWorker
if criticalFailure() {
    log.Error("Critical failure detected, requesting host recovery")
    os.Exit(3)  // Exit code 3 = request host intervention
}
```

**Option B**: External watchdog service (more robust)
```bash
# /etc/systemd/system/aviationwx-watchdog.service
[Service]
ExecStart=/usr/local/bin/aviationwx-watchdog.sh
Restart=always
```

```bash
# watchdog.sh checks:
# - Is NTP synchronized? (timedatectl status | grep "synchronized: yes")
# - Can we ping 8.8.8.8? (network connectivity)
# - Is container healthy? (docker inspect --format='{{.State.Health.Status}}')
# If any fail for > 5 minutes: sudo reboot
```

---

#### 1.3 Add Process Separation (Multi-Container) ⚠️ ARCHITECTURAL
**Risk**: Single process failure kills entire application
**Fix**: Split into multiple containers

```yaml
# docker-compose-robust.yml
services:
  bridge-web:
    image: ghcr.io/alexwitherspoon/aviationwx-bridge:latest
    command: ["bridge", "--web-only"]  # Web UI only
    ports:
      - "1229:1229"
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 100M  # Small footprint
          cpus: '1.0'
  
  bridge-workers:
    image: ghcr.io/alexwitherspoon/aviationwx-bridge:latest
    command: ["bridge", "--workers-only"]  # Background workers only
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 300M
          cpus: '3.0'
    depends_on:
      - bridge-web
```

**Benefits**:
- Web UI survives worker crashes
- Separate memory limits (web: 100MB, workers: 300MB)
- Independent restart policies
- Easier debugging (logs separated)

**Drawbacks**:
- Requires IPC (shared volume or network for status)
- More complex deployment
- Higher memory overhead (2 Go runtimes)

**Recommendation**: **Defer to v3.0**, but design for it now

---

### Priority 2: HIGH (Operational Safety)

#### 2.1 Add Host-Level Health Metrics ⚠️ LIMITED
**Current**: Only process-level metrics (heap, goroutines)
**Needed**: Host-level metrics (temperature, network, NTP, disk)

```go
// pkg/health/host_health.go (NEW)
type HostHealth struct {
    TemperatureC    float64  `json:"temperature_c"`
    NetworkReachable bool    `json:"network_reachable"`
    NTPSynchronized bool     `json:"ntp_synchronized"`
    DiskSpaceFreeMB float64  `json:"disk_space_free_mb"`
    LoadAverage1m   float64  `json:"load_average_1m"`
}

func GetHostHealth() HostHealth {
    // Read from /sys/class/thermal, ping 8.8.8.8, check timedatectl, etc.
}
```

**Expose in Dashboard**: Show warnings when:
- Temperature > 75°C
- Network unreachable > 5 minutes
- NTP not synchronized
- Disk space < 10%

---

#### 2.2 Add Queue Age Metrics ⚠️ MISSING
**Current**: Queue depth (count), size (bytes)
**Needed**: Queue age (oldest image timestamp)

```go
// internal/queue/queue.go
func (q *Queue) GetOldestImageAge() time.Duration {
    q.mu.RLock()
    defer q.mu.RUnlock()
    
    if len(q.images) == 0 {
        return 0
    }
    oldest := q.images[0].ObservationTime
    return time.Since(oldest)
}
```

**Alert if**: Oldest image > 10 minutes (indicates persistent upload failure)

---

#### 2.3 Improve Health Check Granularity ⚠️ BASIC
**Current**: Only checks web server responds
**Needed**: Check worker health, queue health

```go
// /healthz endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    // Check 1: Web server alive (implicit)
    
    // Check 2: Goroutine count
    numGoroutines := runtime.NumGoroutine()
    if numGoroutines > 500 {
        http.Error(w, "Too many goroutines", http.StatusServiceUnavailable)
        return
    }
    
    // Check 3: Memory pressure
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    heapMB := m.HeapAlloc / (1024 * 1024)
    if heapMB > 380 {  // 95% of soft limit
        http.Error(w, "Memory pressure critical", http.StatusServiceUnavailable)
        return
    }
    
    // Check 4: Workers running
    orchStatus := s.orchestrator.GetStatus()
    if orchStatus.RunningWorkers == 0 {
        http.Error(w, "No workers running", http.StatusServiceUnavailable)
        return
    }
    
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "OK (heap: %dMB, goroutines: %d, workers: %d)\n",
        heapMB, numGoroutines, orchStatus.RunningWorkers)
}
```

---

### Priority 3: MEDIUM (Observability)

#### 3.1 Add Metrics Export (Prometheus/OpenTelemetry) 🔮 FUTURE
**Current**: Metrics only in web dashboard
**Needed**: Export to external monitoring (Grafana, Datadog, etc.)

```go
// /metrics endpoint (Prometheus format)
GET /metrics
# HELP aviationwx_heap_bytes Current heap allocation
# TYPE aviationwx_heap_bytes gauge
aviationwx_heap_bytes 180000000

# HELP aviationwx_queue_images Images waiting to upload
# TYPE aviationwx_queue_images gauge
aviationwx_queue_images{camera="cam1"} 5
aviationwx_queue_images{camera="cam2"} 2
```

**Defer to**: v2.2 or v3.0

---

#### 3.2 Add Alerting Webhooks 🔮 FUTURE
**Current**: User must check dashboard
**Needed**: Push notifications on critical events

```json
{
  "webhook_url": "https://hooks.slack.com/...",
  "alerts": {
    "ntp_failure": true,
    "network_failure": true,
    "oom_threshold": true
  }
}
```

**Defer to**: v3.0

---

#### 3.3 Add State Snapshots on Shutdown ⚠️ NICE-TO-HAVE
**Current**: Queue is lost on restart
**Potential**: Save queue manifest on graceful shutdown

```go
// On SIGTERM
func (o *Orchestrator) Stop() {
    // Save queue state to /data/queue-snapshot.json
    snapshot := struct {
        Timestamp time.Time
        Queues    map[string][]string  // camera → filenames
    }{
        Timestamp: time.Now(),
        Queues:    o.queueManager.GetSnapshot(),
    }
    json.WriteFile("/data/queue-snapshot.json", snapshot)
    
    // Then stop normally
}

// On startup
func (o *Orchestrator) Start() {
    // Restore queue from snapshot if < 5 minutes old
    if snapshot := loadSnapshot(); snapshot.Age() < 5*time.Minute {
        o.queueManager.RestoreSnapshot(snapshot)
    }
}
```

**Benefit**: Survive quick restarts without losing queue
**Risk**: Stale queue after long downtime
**Defer to**: v2.2 (low priority)

---

## Part 4: Decision Matrix

### Should We Implement Multi-Container Architecture?

**Pros**:
- ✅ Web UI survives worker crashes
- ✅ Independent memory limits
- ✅ Clearer failure domains
- ✅ Easier debugging

**Cons**:
- ❌ More complex deployment
- ❌ Higher memory overhead (2× Go runtime)
- ❌ IPC complexity (shared state)
- ❌ Not needed on larger systems

**Recommendation**: **Not for Pi Zero 2 W**, but design APIs for future split

**Rationale**:
- Pi has only 512MB RAM, can't afford 2× Go runtime overhead
- Single container is simpler for edge deployments
- Current panic recovery + Docker restart is "good enough"
- If we add external watchdog (Priority 1.2), we gain most benefits

**Action**: Keep single-container, but:
1. Ensure clean separation of concerns in code
2. Design `/healthz` to check worker health
3. Document multi-container option for larger systems

---

### Should We Add External Watchdog?

**YES** - This is the most cost-effective reliability improvement

**Watchdog Responsibilities**:
1. Monitor host-level health (NTP, network, temperature)
2. Check container health beyond `/healthz` (queue age, worker liveness)
3. Trigger recovery actions:
   - Restart NTP service
   - Restart network interface
   - Reboot host (last resort)

**Implementation**:
- Separate systemd service (`aviationwx-watchdog.service`)
- Runs every 60 seconds
- Logs to `/data/aviationwx/watchdog.log`
- Can survive container restarts

**Example**:
```bash
#!/bin/bash
# aviationwx-watchdog.sh

# Check NTP
if ! timedatectl status | grep -q "synchronized: yes"; then
    log "NTP not synchronized, restarting timesyncd"
    systemctl restart systemd-timesyncd
    sleep 5
fi

# Check network
if ! ping -c 1 -W 2 8.8.8.8 &>/dev/null; then
    failures=$((failures + 1))
    if [ $failures -gt 5 ]; then
        log "Network down for 5 minutes, restarting interface"
        ifconfig wlan0 down && ifconfig wlan0 up
        failures=0
    fi
fi

# Check container health
if ! docker exec aviationwx-bridge wget -qO- http://localhost:1229/healthz &>/dev/null; then
    log "Container unhealthy, Docker will restart"
fi

# Check queue age (via API)
oldest_age=$(curl -s http://localhost:1229/api/status | jq '.queue_oldest_age_seconds')
if [ "$oldest_age" -gt 600 ]; then
    log "Queue stale (oldest: ${oldest_age}s), may indicate network issue"
fi
```

---

## Part 5: Final Recommendations

### Implement Immediately (v2.0.2 or v2.1.0)

1. **Add Docker memory hard limit** (1 line change)
   ```yaml
   deploy:
     resources:
       limits:
         memory: 400M
   ```

2. **Add external watchdog script** (new script + systemd unit)
   - Monitor NTP, network, temperature
   - Trigger host recovery on persistent failures

3. **Improve `/healthz` endpoint** (10 lines of code)
   - Check goroutine count
   - Check memory pressure
   - Check worker count

4. **Add queue age metrics** (20 lines of code)
   - Track oldest image in queue
   - Expose in `/api/status`
   - Alert if > 10 minutes

### Plan for v2.2

5. **Add host-level health metrics**
   - Temperature, network, NTP, disk space
   - Display in dashboard

6. **Add thermal throttling detection**
   - Adjust GOMAXPROCS dynamically
   - Show warning in UI

### Plan for v3.0 (Major Architectural Change)

7. **Multi-container option** (for non-Pi deployments)
   - Separate web and workers
   - Shared state via volume or Redis

8. **Metrics export** (Prometheus)
   - `/metrics` endpoint
   - Grafana dashboard templates

---

## Conclusion

**Current State**: The system is **reasonably robust** for a single-process application:
- ✅ Good memory protection (multi-layered)
- ✅ Good state separation (persistent config, ephemeral queue)
- ✅ Good crash recovery (Docker restart, health checks)

**Critical Gaps**:
- ❌ No Docker memory hard limit (host at risk)
- ❌ No host-level health monitoring (NTP, network, thermal)
- ❌ No automatic recovery for persistent failures

**Priority Actions**:
1. Add Docker memory limit (trivial, high impact)
2. Add external watchdog (moderate effort, very high impact)
3. Improve health check granularity (easy, good impact)

With these changes, the system will be **production-ready** for minimal-maintenance edge deployments. The single-container architecture is **appropriate for Pi Zero 2 W**, and the external watchdog provides host-level protection without architectural complexity.
