# Migration Guide: v1.x → v2.0

This guide covers migrating from v1.x architecture (daily cron restarts) to v2.0 architecture (watchdog-based auto-recovery).

## What's Changing

### Old Architecture (v1.x)
- Daily cron job at 3 AM for container restarts
- Manual update checks via `supervisor.sh`
- Docker `--restart=unless-stopped` policy
- No automated host-level health monitoring

### New Architecture (v2.0)
- Progressive watchdog with 30-minute escalation
- Boot-time update checks
- Daily update checks (instead of 6-hourly)
- Docker `--restart=no` (watchdog handles restarts)
- Host-level NTP, network, Docker daemon monitoring
- Automated recovery (service restarts → reboot)
- User-facing CLI (`aviationwx` command)
- Emergency recovery tool

## Migration Paths

### Automatic Migration

For most users, simply re-running the install script will migrate:

```bash
curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
```

The install script will:
1. ✅ Detect existing installation
2. ✅ Install new host scripts
3. ✅ Create systemd services
4. ✅ Remove old cron job
5. ✅ Update container with new restart policy
6. ✅ Start watchdog
7. ✅ Preserve all configuration and data

### Manual Migration

If you prefer manual control:

#### Step 1: Backup Configuration

```bash
sudo cp -r /data/aviationwx /data/aviationwx-backup-$(date +%Y%m%d)
```

#### Step 2: Install New Scripts

```bash
cd /tmp
wget https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/aviationwx
wget https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/aviationwx-supervisor.sh
wget https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/aviationwx-watchdog.sh
wget https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/aviationwx-recovery.sh
wget https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/aviationwx-container-start.sh

sudo mv aviationwx* /usr/local/bin/
sudo chmod +x /usr/local/bin/aviationwx*
```

#### Step 3: Create Systemd Services

Create each service file as shown in the install script, then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable aviationwx-boot-update.service
sudo systemctl enable aviationwx-container.service
sudo systemctl enable aviationwx-daily-update.timer
sudo systemctl enable aviationwx-watchdog.timer
```

#### Step 4: Remove Old Cron Job

```bash
sudo crontab -l | grep -v aviationwx-daily-restart | sudo crontab -
sudo rm -f /usr/local/bin/aviationwx-daily-restart
```

#### Step 5: Remove Old Supervisor Timer

```bash
sudo systemctl stop aviationwx-supervisor.timer
sudo systemctl disable aviationwx-supervisor.timer
sudo rm -f /etc/systemd/system/aviationwx-supervisor.timer
sudo rm -f /etc/systemd/system/aviationwx-supervisor.service
sudo systemctl daemon-reload
```

#### Step 6: Recreate Container with New Policy

```bash
sudo docker stop aviationwx-bridge
sudo docker rm aviationwx-bridge

# Let boot-update handle pulling and starting
sudo systemctl start aviationwx-boot-update.service
sudo systemctl start aviationwx-container.service
```

#### Step 7: Start Watchdog

```bash
sudo systemctl start aviationwx-watchdog.timer
```

#### Step 8: Verify

```bash
sudo aviationwx status
```

## Rollback Plan

If you encounter issues and need to rollback:

### Rollback to v1.x

```bash
# Stop new services
sudo systemctl stop aviationwx-watchdog.timer
sudo systemctl stop aviationwx-daily-update.timer
sudo systemctl disable aviationwx-boot-update.service
sudo systemctl disable aviationwx-container.service

# Restore old supervisor (if you have it)
sudo systemctl enable aviationwx-supervisor.timer
sudo systemctl start aviationwx-supervisor.timer

# Restore old cron job
echo "0 3 * * * /usr/local/bin/aviationwx-daily-restart >> /data/aviationwx/restart.log 2>&1" | sudo crontab -

# Recreate container with old restart policy
sudo docker stop aviationwx-bridge
sudo docker rm aviationwx-bridge

sudo docker run -d \
    --name aviationwx-bridge \
    --restart=unless-stopped \
    -p 1229:1229 \
    -v /data/aviationwx:/data \
    --tmpfs /dev/shm:size=200m \
    ghcr.io/alexwitherspoon/aviationwx-bridge:latest
```

## Configuration Changes

### Update Channel (New in v2.0)

Add to `/data/aviationwx/global.json`:

```json
{
  "update_channel": "latest"
}
```

Or for edge releases:

```json
{
  "update_channel": "edge"
}
```

Default is `"latest"` if not specified.

### Existing Configurations

All existing camera configurations and settings are **fully compatible** and require no changes.

## Verifying Migration

### Check Services

```bash
sudo systemctl status aviationwx-boot-update.service
sudo systemctl status aviationwx-container.service
sudo systemctl list-timers | grep aviationwx
```

Should show:
- ✅ `aviationwx-daily-update.timer` (next run in ~24h)
- ✅ `aviationwx-watchdog.timer` (next run in ~1min)

### Check Container

```bash
sudo aviationwx status
```

Should show:
- Container running
- Health: healthy
- Version: v2.0.0 or later

### Check Watchdog

```bash
sudo journalctl -u aviationwx-watchdog.service -n 20
```

Should show watchdog checks running every minute.

### Test CLI

```bash
sudo aviationwx status        # Show status
sudo aviationwx logs bridge   # Show container logs
sudo aviationwx recovery      # Enter recovery menu (exit without changes)
```

## Common Issues

### Issue: Container keeps restarting

**Old behavior:** Daily restart at 3 AM
**New behavior:** No scheduled restarts, only on health failure

If you see frequent restarts, check:

```bash
sudo aviationwx logs bridge 50
```

The watchdog will only restart if the container is unhealthy for multiple checks.

### Issue: Update not happening

Check daily update timer:

```bash
sudo systemctl status aviationwx-daily-update.timer
sudo journalctl -u aviationwx-daily-update.service
```

Force an update:

```bash
sudo aviationwx update
```

### Issue: Watchdog too aggressive

Check watchdog state:

```bash
cat /data/aviationwx/watchdog-state.json
```

If false positives, increase thresholds in `/usr/local/bin/aviationwx-watchdog.sh`:

```bash
readonly NTP_RESTART_AT=20     # Default: 15 minutes
readonly NETWORK_RESTART_AT=15 # Default: 10 minutes
readonly DOCKER_RESTART_AT=20  # Default: 15 minutes
readonly REBOOT_AT=30          # Default: 25 minutes
```

Then:

```bash
sudo systemctl restart aviationwx-watchdog.timer
```

## Support

If you encounter issues during migration:

1. Check logs: `sudo aviationwx logs supervisor`
2. Check watchdog: `tail -f /data/aviationwx/watchdog.log`
3. Use recovery tool: `sudo aviationwx recovery`
4. Report issue: https://github.com/alexwitherspoon/aviationwx-bridge/issues

## Testing Migration

If you want to test the migration without affecting production:

```bash
# Set dry-run mode
export AVIATIONWX_DRY_RUN=true

# Run supervisor
sudo -E /usr/local/bin/aviationwx-supervisor.sh boot-update

# Run watchdog
sudo -E /usr/local/bin/aviationwx-watchdog.sh

# All actions will be logged but not executed
```

## FAQ

### Q: Will my cameras stop capturing during migration?

**A:** There will be a brief interruption (10-30 seconds) when the container is recreated. Images in the queue are stored in tmpfs and will be lost during restart, but this is by design for fail-forward recovery.

### Q: Do I need to reconfigure my cameras?

**A:** No. All camera configurations are preserved. The file-per-camera format is fully compatible.

### Q: What happens to my upload credentials?

**A:** All credentials are preserved in the per-camera JSON files.

### Q: Can I switch back to v1.x after migration?

**A:** Yes, use the rollback procedure above. Your v1.x data is fully compatible with v2.0 and vice versa.

### Q: When will the first update check happen?

**A:** Immediately at boot (via `aviationwx-boot-update.service`), then daily via the timer.

### Q: How do I know if watchdog is working?

**A:** Check logs: `tail -f /data/aviationwx/watchdog.log` or `sudo journalctl -u aviationwx-watchdog.service -f`

### Q: Can I disable the watchdog?

**A:** Yes, but not recommended:

```bash
sudo systemctl stop aviationwx-watchdog.timer
sudo systemctl disable aviationwx-watchdog.timer
```

To re-enable:

```bash
sudo systemctl enable aviationwx-watchdog.timer
sudo systemctl start aviationwx-watchdog.timer
```
