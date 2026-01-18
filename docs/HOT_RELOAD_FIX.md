# Hot-Reload Fix & Daily Restart Guide

## Issue Summary

**Symptom**: When changing a camera's interval (e.g., from 60s to 30s) via the web UI, the capture worker stops working until the container is restarted.

**Root Cause**: The `AddCamera()` method in the orchestrator creates a new capture worker but doesn't automatically start it if the orchestrator has already been started. Workers are only started in bulk during `Orchestrator.Start()` at application startup.

**Impact**: Affects any camera configuration change that requires recreating the worker (interval changes, camera type changes, etc.)

---

## Solution 1: Fix Applied (v2.0.1+)

### The Fix

Modified `internal/scheduler/orchestrator.go` to auto-start workers when added after orchestrator is running:

```go
// If orchestrator has already been started, start this worker immediately
if !o.startTime.IsZero() {
    worker.Start()
    o.logger.Info("Capture worker started (hot-reload)", "camera", cameraID)
}
```

### Verification

1. Start the bridge
2. Edit a camera's interval via web UI (e.g., change from 60s to 30s)
3. Check logs for: `Capture worker started (hot-reload)`
4. Verify captures happen at the new interval without restart

### Expected Logs

```
time=06:15:30 level=INFO msg="Config event received" type=camera_updated camera=kspbcam1
time=06:15:30 level=INFO msg="Capture worker stopped" camera=kspbcam1
time=06:15:30 level=INFO msg="Camera removed" camera=kspbcam1
time=06:15:30 level=INFO msg="Camera added" camera=kspbcam1 interval_secs=30
time=06:15:30 level=INFO msg="Capture worker started (hot-reload)" camera=kspbcam1
time=06:15:30 level=INFO msg="Camera worker started successfully" camera=kspbcam1 type=rtsp interval=30
```

---

## Solution 2: Daily Restart (Workaround for v1.x-v2.0)

### Purpose

Provides a safety net for any configuration or state issues by restarting the container daily during off-peak hours.

### Installation

1. **Copy the script to your system:**

```bash
sudo curl -o /opt/aviationwx/daily-restart.sh \
  https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/daily-restart.sh
sudo chmod +x /opt/aviationwx/daily-restart.sh
```

2. **Add to crontab (runs at 3 AM daily):**

```bash
sudo crontab -e
```

Add this line:

```cron
0 3 * * * /opt/aviationwx/daily-restart.sh >> /var/log/aviationwx-restart.log 2>&1
```

3. **Verify crontab:**

```bash
sudo crontab -l
```

### Script Features

- **Graceful restart**: Uses `docker restart` (not kill)
- **Health check monitoring**: Waits up to 60 seconds for container to be healthy
- **Detailed logging**: Timestamped logs to `/var/log/aviationwx-restart.log`
- **Error handling**: Exits with error codes if container not found or doesn't become healthy

### Manual Test

Test the script without waiting for cron:

```bash
sudo /opt/aviationwx/daily-restart.sh
```

Expected output:

```
[2026-01-18 03:00:00] Starting daily restart of aviationwx-bridge
[2026-01-18 03:00:00] Restarting container...
[2026-01-18 03:00:02] Waiting for container to be healthy...
[2026-01-18 03:00:04] Health check 1/30: starting
[2026-01-18 03:00:06] Health check 2/30: starting
[2026-01-18 03:00:08] Health check 3/30: healthy
[2026-01-18 03:00:08] Container is healthy
```

### Viewing Logs

```bash
# View restart history
sudo tail -f /var/log/aviationwx-restart.log

# Check last restart
sudo tail -n 20 /var/log/aviationwx-restart.log
```

### Customization

**Change restart time:**

Edit crontab and modify the time (format: minute hour * * *):

```cron
# 2 AM instead of 3 AM
0 2 * * * /opt/aviationwx/daily-restart.sh >> /var/log/aviationwx-restart.log 2>&1

# 11 PM
0 23 * * * /opt/aviationwx/daily-restart.sh >> /var/log/aviationwx-restart.log 2>&1
```

**Disable daily restart (after upgrading to v2.0.1+):**

```bash
sudo crontab -e
# Comment out or delete the line:
# 0 3 * * * /opt/aviationwx/daily-restart.sh >> /var/log/aviationwx-restart.log 2>&1
```

---

## Recommended Approach

### For New Installations (v2.0.1+)

- ✅ **Hot-reload fix is included** - no daily restart needed
- ✅ Optional: Add daily restart as a safety net for other potential issues

### For Existing Installations (v1.x - v2.0)

1. **Immediate**: Add daily restart cron job (workaround)
2. **Long-term**: Update to v2.0.1+ to get hot-reload fix
3. **After upgrade**: Optionally remove daily restart if no longer needed

---

## Testing Hot-Reload Fix

### Test Scenario 1: Change Interval

1. Navigate to web UI: `http://your-bridge:1229`
2. Edit a camera
3. Change interval from 60s to 30s
4. Save
5. Check dashboard - next capture should show new interval
6. **No restart required!**

### Test Scenario 2: Change Camera Type

1. Edit a camera
2. Change from HTTP to RTSP (or vice versa)
3. Save
4. Check logs for "Capture worker started (hot-reload)"
5. Verify captures work immediately

### Test Scenario 3: Add New Camera

1. Click "Add Camera"
2. Fill in details
3. Save
4. Worker starts automatically (no restart needed)

---

## Troubleshooting

### Hot-Reload Not Working?

1. **Check version:**
   ```bash
   docker exec aviationwx-bridge /usr/local/bin/bridge --version
   ```
   Should be v2.0.1 or later

2. **Check logs for hot-reload message:**
   ```bash
   docker logs aviationwx-bridge | grep "hot-reload"
   ```

3. **Verify orchestrator is started:**
   ```bash
   docker logs aviationwx-bridge | grep "Orchestrator started"
   ```

### Daily Restart Not Running?

1. **Verify crontab:**
   ```bash
   sudo crontab -l | grep aviationwx
   ```

2. **Check cron service:**
   ```bash
   sudo systemctl status cron  # Debian/Ubuntu
   sudo systemctl status crond # RedHat/CentOS
   ```

3. **Check script permissions:**
   ```bash
   ls -l /opt/aviationwx/daily-restart.sh
   # Should show: -rwxr-xr-x (executable)
   ```

4. **Test script manually:**
   ```bash
   sudo /opt/aviationwx/daily-restart.sh
   ```

---

## Related Issues

- **Config data loss**: Fixed in v2.0 (ConfigService)
- **Upload credential mixup**: Fixed in v2.0 (per-camera uploaders)
- **Worker blocking**: Fixed in v2.0 (job overlap prevention)
- **Hot-reload interval changes**: Fixed in v2.0.1 (this fix)

---

## Version History

- **v1.0.x - v2.0**: Hot-reload broken for interval changes
- **v2.0.1**: Hot-reload fixed - workers auto-start after configuration changes
- **All versions**: Daily restart script provides safety net
