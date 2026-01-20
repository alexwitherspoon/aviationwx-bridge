# v2.0.1 Hotfix Release - Post-Mortem

## Timeline

**2026-01-20 10:11 PST** - Bug discovered on production Raspberry Pi  
**2026-01-20 10:20 PST** - Root cause identified  
**2026-01-20 10:25 PST** - Fix committed and pushed  
**2026-01-20 10:30 PST** - v2.0.1 released

## The Bug

### Symptoms
New installations failed with:
```
grep: Invalid range end
invalid reference format
[ACTION] Rolling back to last known good: latest
```

### Root Cause
The `determine_target_version()` function in `scripts/aviationwx-supervisor.sh` was writing log messages to stdout, which were being captured as part of the return value.

**Expected output:**
```bash
target_version=$(determine_target_version "latest")
# Should capture: "v2.0.0"
```

**Actual output:**
```bash
target_version=$(determine_target_version "latest")
# Actually captured: "[2026-01-20T10:11:56-08:00] [INFO] Determining target version for channel: latest\nv2.0.0"
```

This corrupted the Docker image tag, making `docker pull` commands fail.

### Impact
- **Severity**: CRITICAL (P0)
- **Scope**: ALL new installations since v2.0.0 release
- **Duration**: ~2 hours between v2.0.0 release and discovery
- **Affected Users**: Anyone attempting a fresh install or update
- **Running Systems**: UNAFFECTED (existing v2.0.0 containers continued working)

## The Fix

### Code Changes
File: `scripts/aviationwx-supervisor.sh`  
Lines: 5 changes in `determine_target_version()` function

**Before:**
```bash
log_event "INFO" "Determining target version for channel: $channel"
```

**After:**
```bash
log_event "INFO" "Determining target version for channel: $channel" >&2
```

All log messages redirected to stderr using `>&2`, ensuring only the version tag goes to stdout.

### Why This Happened
1. **Shell Best Practice**: Functions that return values via `echo` should redirect all other output to stderr
2. **Testing Gap**: The supervisor script wasn't tested with real GitHub API calls in CI
3. **Timing**: The bug only manifested when calling the function with command substitution

### Why It Wasn't Caught Earlier
- ✅ Syntax validation passed (bash -n)
- ✅ CI didn't test the full supervisor flow
- ✅ Local Docker testing used existing images
- ❌ No integration test for fresh installation flow
- ❌ No test for supervisor script stdout/stderr separation

## Testing Performed

### Pre-Release
- ✅ Syntax validation: `bash -n scripts/aviationwx-supervisor.sh`
- ✅ Git commit and push successful
- ✅ CI pipeline passed (test fix)

### Post-Release
- ⏳ User will test on Raspberry Pi with fresh install

## Lessons Learned

### What Went Wrong
1. **Insufficient Integration Testing**: The supervisor script wasn't tested end-to-end
2. **Silent Failure Mode**: The bug only showed up during actual installation
3. **Rapid Release**: v2.0.0 was released without real-world installation verification

### What Went Right
1. **Fast Discovery**: Bug found within 2 hours of release
2. **Quick Fix**: Root cause identified and fixed within 10 minutes
3. **Clean Rollback**: Existing systems unaffected, installer pulls from main
4. **Good Separation**: Host scripts vs Docker images allowed quick hotfix

### Action Items for Future

#### Immediate (v2.1)
- [ ] Add integration test for supervisor script with mocked GitHub API
- [ ] Add smoke test for `determine_target_version()` stdout cleanliness
- [ ] Add pre-release checklist requiring fresh install test

#### Medium-Term (v2.2)
- [ ] Implement staging environment with real Pi hardware
- [ ] Add automated installer tests in CI
- [ ] Create "canary" update channel for gradual rollouts

#### Long-Term (v3.0)
- [ ] Consider Go rewrite of supervisor (stronger typing)
- [ ] Implement health metrics/telemetry for early warning
- [ ] Add automated rollback triggers

## Communication

### User Notification
- ✅ GitHub Release notes include clear action items
- ✅ Release marked as "Critical Hotfix"
- ✅ Instructions for updating provided
- ⏳ Consider email to known users (if contact list exists)

### Documentation Updates
- ✅ CHANGELOG.md updated
- ✅ Release notes include "Action Required" section
- ⏳ Update docs/DEPLOYMENT.md with troubleshooting

## Metrics

| Metric | Value |
|--------|-------|
| **Time to Discovery** | 2 hours |
| **Time to Fix** | 10 minutes |
| **Time to Release** | 20 minutes |
| **Lines Changed** | 5 |
| **Files Changed** | 1 |
| **Breaking Change** | No |
| **Rollback Required** | No |
| **User Action Required** | Yes (re-run installer) |

## Related Issues

- Related: Installer cleanup system (#4243b50)
- Related: Queue test fix (#db20ee5)
- Caused by: v2.0.0 release

## Sign-Off

**Fixed By**: AI Assistant  
**Verified By**: Pending user confirmation  
**Released**: 2026-01-20 10:30 PST  
**Status**: Deployed to main, GitHub release published  

---

## Appendix: Full Error Log

```
admin@KSPB-AviationWX-Bridge:~ $ curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
...
[INFO] Current: latest, Target: [2026-01-20T10:11:56-08:00] [INFO] Determining target version for channel: latest
v2.0.0
grep: Invalid range end
[2026-01-20T10:11:57-08:00] [ACTION] Pulling ghcr.io/alexwitherspoon/aviationwx-bridge:[2026-01-20T10:11:56-08:00] [INFO] Determining target version for channel: latest
v2.0.0
[2026-01-20T10:11:57-08:00] [ACTION] Pull image
invalid reference format
[2026-01-20T10:11:57-08:00] [ERROR] Failed to pull image
[2026-01-20T10:11:57-08:00] [ERROR] Update failed, rolling back
[2026-01-20T10:11:57-08:00] [ACTION] Rolling back to last known good: latest
```
