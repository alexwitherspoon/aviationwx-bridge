# AviationWX Bridge - Maintenance Guide

This guide covers maintenance procedures for contributors and maintainers.

## Table of Contents

1. [Deprecating Components](#deprecating-components)
2. [Adding New Host Scripts](#adding-new-host-scripts)
3. [Updating Systemd Services](#updating-systemd-services)
4. [Release Process](#release-process)

---

## Deprecating Components

The installer (`scripts/install.sh`) includes automatic cleanup of deprecated components. When you remove or rename host-level components, follow this process:

### 1. Deprecating a Script

**Example:** Removing `aviationwx-old-tool.sh` in v2.5

1. **Update `remove_deprecated_items()` function:**

```bash
# Deprecated scripts (removed in various versions)
local deprecated_scripts=(
    "aviationwx-daily-restart"         # v2.0: Replaced by watchdog
    "aviationwx-daily-restart.sh"      # v2.0: Alternative name
    "aviationwx-old-tool.sh"           # v2.5: Functionality moved to CLI
    # Add future deprecated scripts here with version comment
)
```

2. **Update `uninstall()` function:**

```bash
# Deprecated scripts (from older versions)
local deprecated_scripts=(
    "aviationwx-daily-restart"
    "aviationwx-daily-restart.sh"
    "aviationwx-old-tool.sh"           # v2.5
)
```

3. **Document in CHANGELOG.md:**

```markdown
### Removed
- `aviationwx-old-tool.sh` - functionality moved to `aviationwx` CLI
```

### 2. Deprecating a Systemd Service

**Example:** Removing `aviationwx-old-service.timer` in v2.5

1. **Update `remove_deprecated_items()` function:**

```bash
# Deprecated systemd services/timers
local deprecated_systemd=(
    "aviationwx-daily-restart.service"
    "aviationwx-daily-restart.timer"
    "aviationwx-old-service.timer"     # v2.5: Merged into watchdog
    "aviationwx-old-service.service"   # v2.5: Merged into watchdog
    # Add future deprecated services here with version comment
)
```

2. **Update `uninstall()` function:**

```bash
# Deprecated systemd services/timers (from older versions)
local deprecated_systemd=(
    "aviationwx-daily-restart.service"
    "aviationwx-daily-restart.timer"
    "aviationwx-old-service.timer"     # v2.5
    "aviationwx-old-service.service"   # v2.5
)
```

3. **Document in CHANGELOG.md**

### 3. Deprecating a Cron Pattern

**Example:** Removing a cron job that was manually added

1. **Update `remove_deprecated_items()` function:**

```bash
# Deprecated cron patterns (will be removed from crontab)
local deprecated_cron_patterns=(
    "aviationwx-daily-restart"
    "aviationwx-old-cron-job"         # v2.5
    # Add future deprecated cron patterns here
)
```

### 4. Renaming a Component

**Example:** Renaming `aviationwx-tool.sh` to `aviationwx-new-tool.sh`

1. **Add the old name to deprecated list** (so it gets removed)
2. **Add the new name to current list** (so it gets installed)
3. **Update documentation**
4. **Create a migration note in the release**

---

## Adding New Host Scripts

When adding a new host-level script:

### 1. Create the Script

Place it in `scripts/` directory:
- Follow bash best practices
- Use `set -euo pipefail`
- Add usage/help comments
- Make it executable: `chmod +x scripts/new-script.sh`

### 2. Update Installer

Add to `install_host_scripts()` function:

```bash
local scripts=(
    "aviationwx"
    "aviationwx-supervisor.sh"
    "aviationwx-watchdog.sh"
    "aviationwx-recovery.sh"
    "aviationwx-container-start.sh"
    "aviationwx-new-script.sh"         # NEW in v2.x
)
```

### 3. Update Uninstaller

Add to `uninstall()` function:

```bash
# Current scripts
local current_scripts=(
    "aviationwx"
    "aviationwx-supervisor.sh"
    "aviationwx-watchdog.sh"
    "aviationwx-recovery.sh"
    "aviationwx-container-start.sh"
    "aviationwx-new-script.sh"         # NEW in v2.x
)
```

### 4. Document

- Add to `README.md` if user-facing
- Add to `docs/DEPLOYMENT.md` if ops-related
- Document in CHANGELOG.md

---

## Updating Systemd Services

### Modifying Existing Services

1. **Edit the service definition** in `setup_systemd()` function
2. **Test locally** with Docker deployment
3. **Document changes** in CHANGELOG.md
4. **Release note**: Mention that services will be updated on next install

### Adding New Services

1. **Create service definition** in `setup_systemd()`:

```bash
# New monitoring service
cat > /etc/systemd/system/aviationwx-new-monitor.service << 'EOF'
[Unit]
Description=AviationWX New Monitor
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-new-script.sh
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
```

2. **Enable in the reload section:**

```bash
systemctl daemon-reload
systemctl enable aviationwx-boot-update.service
systemctl enable aviationwx-container.service
systemctl enable aviationwx-daily-update.timer
systemctl enable aviationwx-watchdog.timer
systemctl enable aviationwx-new-monitor.service  # NEW
```

3. **Add to uninstall** function

---

## Release Process

### Pre-Release Checklist

- [ ] All tests pass: `go test ./...`
- [ ] CI passes: Check GitHub Actions
- [ ] Docker builds: `cd docker && docker compose up --build`
- [ ] Local testing complete: See `docs/LOCAL_TESTING.md`
- [ ] CHANGELOG.md updated with all changes
- [ ] Version bumped in relevant files
- [ ] Deprecated components added to cleanup lists

### Creating a Release

1. **Tag the release:**
   ```bash
   git tag -a v2.x.0 -m "Release v2.x.0"
   git push origin v2.x.0
   ```

2. **GitHub Release:**
   - Use `docs/RELEASE_TEMPLATE.md` structure
   - Include `AVIATIONWX_METADATA` JSON
   - Mark deprecated releases if needed

3. **Verify CI/CD:**
   - Docker images built for all platforms
   - Images tagged correctly: `v2.x.0`, `2.x`, `2`, `latest`

### Post-Release

- [ ] Verify images on ghcr.io
- [ ] Test fresh install on clean Pi
- [ ] Test upgrade from previous version
- [ ] Monitor issue tracker for reports

---

## Deprecation Timeline

### Keep in Deprecated List

**Minimum:** 2 major versions or 1 year, whichever is longer

**Example:**
- Component removed in v2.0 (Jan 2026)
- Keep in deprecated list through v4.0 or Jan 2027

**Why?** Users may skip versions when upgrading. A user on v1.5 upgrading to v3.0 should still get cleanup.

### When to Remove from Deprecated List

Once a component has been in the deprecated list for sufficient time:

1. **Comment it out** with removal date:
   ```bash
   # "aviationwx-old-tool.sh"     # v2.0 deprecated, removed v4.0 (2027-06)
   ```

2. **Remove completely** after another major version

---

## Testing Deprecation

### Test Cleanup Works

1. **Create fake deprecated files:**
   ```bash
   sudo touch /usr/local/bin/aviationwx-daily-restart
   sudo touch /etc/systemd/system/aviationwx-daily-restart.service
   ```

2. **Run installer:**
   ```bash
   curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash
   ```

3. **Verify removed:**
   ```bash
   ls /usr/local/bin/aviationwx-daily-restart  # Should not exist
   systemctl status aviationwx-daily-restart.service  # Should not exist
   ```

### Test Uninstall

1. **Install:**
   ```bash
   curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash
   ```

2. **Uninstall:**
   ```bash
   curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash -s uninstall
   ```

3. **Verify clean:**
   ```bash
   ls /usr/local/bin/aviationwx*  # Should be empty
   systemctl list-unit-files | grep aviationwx  # Should be empty
   docker ps -a | grep aviationwx  # Should be empty
   ```

---

## Best Practices

### Scripts

- ✅ Always use absolute paths
- ✅ Check exit codes: `set -e`
- ✅ Use `|| true` for non-critical commands
- ✅ Log all actions with timestamps
- ✅ Test with `bash -n script.sh` before commit

### Systemd

- ✅ Use `After=` for ordering
- ✅ Use `Requires=` for dependencies
- ✅ Set reasonable timeouts
- ✅ Use `journald` for logging
- ✅ Test enable/disable/restart

### Documentation

- ✅ Comment WHY not WHAT
- ✅ Add version numbers to deprecations
- ✅ Update all relevant docs
- ✅ Include migration notes in releases

---

## Troubleshooting

### Installer Fails Mid-Run

**Symptom:** Installer exits with error during systemd setup

**Solution:**
```bash
# Check systemd status
systemctl daemon-reload
systemctl status aviationwx-*

# Check logs
journalctl -u aviationwx-* -n 100

# Retry
curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash
```

### Deprecated Component Not Removed

**Symptom:** Old script still exists after upgrade

**Solution:**
1. Check it's in the deprecated list
2. Manually verify function is called
3. Check permissions (must run as root)
4. Manually remove: `sudo rm -f /usr/local/bin/old-script`

### Uninstall Doesn't Remove Everything

**Symptom:** Some components remain after uninstall

**Solution:**
1. Check uninstall function includes the component
2. Run deprecation cleanup: Update deprecated lists in uninstall
3. File an issue with details

---

## Questions?

- File an issue: https://github.com/alexwitherspoon/aviationwx-bridge/issues
- Email: contact@aviationwx.org
