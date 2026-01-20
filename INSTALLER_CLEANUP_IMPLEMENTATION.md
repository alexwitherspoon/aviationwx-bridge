# Installer Cleanup System - Implementation Summary

## Overview

Enhanced the `install.sh` script with a robust automatic cleanup system for deprecated host-level components. This ensures clean upgrades and prevents orphaned files when components are removed or renamed in future versions.

## What Was Implemented

### 1. Unified Deprecation Function

**New function:** `remove_deprecated_items()`

**Cleans up:**
- ✅ Deprecated scripts from `/usr/local/bin/`
- ✅ Deprecated systemd services and timers
- ✅ Deprecated cron jobs from crontab

**Features:**
- Centralized deprecation tracking with inline comments
- Version annotations for each deprecated item
- Counts and reports removed components
- Handles missing items gracefully

**Location:** `scripts/install.sh` lines 371-435

### 2. Enhanced Uninstall Function

**Improvements:**
- Uses arrays for both current and deprecated components
- Single loop to handle all removals
- Comprehensive cleanup of all aviationwx-related cron jobs
- Removes both current and historical components

**Location:** `scripts/install.sh` lines 491-549

### 3. Integration into Install Flow

**Order of operations:**
1. Install Docker (if needed)
2. Setup data directory
3. Install/update current host scripts
4. Setup/update systemd services
5. **✨ Remove deprecated items** ← NEW STEP
6. Bootstrap container
7. Complete

## How It Works

### Deprecation Lists

Three separate arrays track different types of deprecated components:

```bash
# Scripts
deprecated_scripts=(
    "aviationwx-daily-restart"         # v2.0: Replaced by watchdog
    "aviationwx-daily-restart.sh"      # v2.0: Alternative name
)

# Systemd units
deprecated_systemd=(
    "aviationwx-daily-restart.service"
    "aviationwx-daily-restart.timer"
)

# Cron patterns
deprecated_cron_patterns=(
    "aviationwx-daily-restart"
)
```

### Cleanup Process

1. **Check existence** - Only attempts removal if item exists
2. **Stop services** - Stops systemd units before removal
3. **Disable units** - Prevents auto-start on boot
4. **Remove files** - Deletes from filesystem
5. **Reload daemon** - Updates systemd state
6. **Report results** - Logs count of removed items

### For Future Maintainers

When deprecating a component, add it to the appropriate array with a version comment:

```bash
"component-name"           # vX.Y: Why it was removed/replaced
```

**Recommended retention:** Keep in list for 2 major versions or 1 year.

## Testing

### Syntax Validation

```bash
bash -n scripts/install.sh
✓ Syntax check passed
```

### Manual Testing Needed

To fully test the cleanup system on a real Pi:

1. **Create fake deprecated files:**
   ```bash
   sudo touch /usr/local/bin/aviationwx-daily-restart
   sudo touch /etc/systemd/system/aviationwx-daily-restart.service
   ```

2. **Run installer:**
   ```bash
   curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
   ```

3. **Verify cleanup:**
   ```bash
   ls /usr/local/bin/aviationwx-daily-restart  # Should not exist
   systemctl status aviationwx-daily-restart.service  # Should not exist
   ```

## Benefits

### For Users

✅ **Clean upgrades** - No orphaned files accumulating over time
✅ **Automatic** - No manual cleanup needed
✅ **Safe** - Only removes known deprecated items
✅ **Transparent** - Logs all cleanup actions

### For Maintainers

✅ **Simple to use** - Just add to deprecation lists
✅ **Self-documenting** - Version comments explain why
✅ **Centralized** - One place for all deprecations
✅ **Future-proof** - Easy to extend for new component types

## Current Deprecated Items

As of v2.0:

| Component | Type | Replaced By | Version |
|-----------|------|-------------|---------|
| `aviationwx-daily-restart` | Script | Watchdog | v2.0 |
| `aviationwx-daily-restart.sh` | Script | Watchdog | v2.0 |
| `aviationwx-daily-restart.service` | Systemd | Watchdog | v2.0 |
| `aviationwx-daily-restart.timer` | Systemd | Watchdog | v2.0 |

## Documentation

Created comprehensive maintenance guide:
- **Location:** `docs/MAINTENANCE_GUIDE.md`
- **Contents:**
  - How to deprecate scripts, services, and cron jobs
  - How to add new components
  - Release process checklist
  - Testing procedures
  - Best practices

## Files Modified

1. `scripts/install.sh`
   - Added `remove_deprecated_items()` function
   - Enhanced `uninstall()` function
   - Integrated cleanup into install flow

2. `docs/MAINTENANCE_GUIDE.md` (NEW)
   - Complete guide for future maintainers
   - Deprecation procedures
   - Testing instructions

## Next Steps

- [ ] Test on staging Pi with mock deprecated files
- [ ] Verify uninstall removes everything
- [ ] Include in next release notes
- [ ] Update CHANGELOG.md

## Impact

**Risk Level:** ✅ Low
- Only removes items from known deprecated lists
- Fails gracefully if items don't exist
- Uses `|| true` to prevent install failure
- Tested with bash syntax checker

**User Impact:** ✅ Positive
- Cleaner systems over time
- No manual intervention needed
- Automatic during normal upgrades

---

**Implementation Date:** 2026-01-20
**Implemented By:** AviationWX Bridge Maintenance
**Status:** ✅ Complete, ready for testing
