# GitHub Release Template

This document defines the format for GitHub releases and the machine-readable metadata used by the auto-update system.

## Release Format

Each release should follow this structure:

```markdown
## What's Changed

- Feature: Brief description
- Fix: Brief description
- Breaking: Major change description (if any)

## AVIATIONWX_METADATA

```json
{
  "min_host_version": "2.0",
  "deprecates": ["v1.0.0", "v1.1.0"],
  "edge_stable_commit": "abc123def456"
}
```

## Full Changelog

https://github.com/alexwitherspoon/aviationwx-bridge/compare/v1.9.0...v2.0.0
```

## Metadata Fields

### `min_host_version` (required)

The minimum host script version required to run this release. If the host scripts are older than this version, they will be automatically updated before the container update proceeds.

**Example:**
```json
"min_host_version": "2.0"
```

**Usage:**
- Set to `"2.0"` for all releases requiring the new watchdog/supervisor architecture
- Bump only when host scripts have breaking changes or critical new features
- Format: Semantic version string without 'v' prefix

### `deprecates` (optional)

Array of version tags that are explicitly deprecated and should trigger an update.

**Example:**
```json
"deprecates": ["v1.0.0", "v1.1.0", "v1.2.0"]
```

**Usage:**
- Include versions with critical security issues
- Include versions with data corruption bugs
- Include versions incompatible with current API/services
- Format: Full tag names including 'v' prefix

### `edge_stable_commit` (optional, edge releases only)

For edge releases, the git commit hash that passed stability testing.

**Example:**
```json
"edge_stable_commit": "abc123def456789"
```

**Usage:**
- Set on edge pre-releases after 24h of successful testing
- Allows edge failback logic to determine if an edge release is stable enough
- If missing or mismatched, edge channel will failback to latest

## Release Checklist

When creating a new release:

- [ ] Update `CHANGELOG.md` with all changes
- [ ] Tag follows semantic versioning: `vMAJOR.MINOR.PATCH`
- [ ] Release notes include `AVIATIONWX_METADATA` block
- [ ] `min_host_version` set correctly
- [ ] `deprecates` includes any versions with known critical issues
- [ ] For edge releases: add `edge_stable_commit` after stability verification
- [ ] Docker image is built and published automatically by CI
- [ ] Test installation on clean Pi Zero 2 W (if major release)

## Example Releases

### Stable Release

```markdown
# v2.1.0 - Enhanced Camera Support

## What's Changed

- Feature: Add support for ONVIF PTZ controls
- Feature: New web UI for camera positioning
- Fix: EXIF timestamp handling for cameras without RTC
- Fix: Memory leak in RTSP stream handling

## AVIATIONWX_METADATA

```json
{
  "min_host_version": "2.0",
  "deprecates": ["v2.0.0", "v2.0.1"]
}
```

## Full Changelog

https://github.com/alexwitherspoon/aviationwx-bridge/compare/v2.0.2...v2.1.0
```

### Edge Pre-Release

```markdown
# v2.2.0-edge.1 - Experimental Multi-Upload

## What's Changed

⚠️ **This is an EDGE release - may be unstable**

- Feature: Experimental support for multiple upload destinations per camera
- Breaking: Upload config format changed (auto-migrated)

## AVIATIONWX_METADATA

```json
{
  "min_host_version": "2.0"
}
```

**Note:** `edge_stable_commit` will be added after 24h stability verification.

## Testing

This release has passed CI but needs real-world testing. If you experience issues, the system will automatically failback to the latest stable release.

## Full Changelog

https://github.com/alexwitherspoon/aviationwx-bridge/compare/v2.1.0...v2.2.0-edge.1
```

### Edge Release (After Stability Verification)

```markdown
# v2.2.0-edge.2 - Experimental Multi-Upload (Stable)

## What's Changed

⚠️ **This is an EDGE release**

- Feature: Experimental support for multiple upload destinations per camera
- Breaking: Upload config format changed (auto-migrated)
- Fix: Handle upload retry logic correctly for multiple destinations

## AVIATIONWX_METADATA

```json
{
  "min_host_version": "2.0",
  "edge_stable_commit": "7f3a2b8c9d1e4f5g"
}
```

✅ **Stability verified** - This edge release has been running successfully for 24+ hours in production.

## Full Changelog

https://github.com/alexwitherspoon/aviationwx-bridge/compare/v2.1.0...v2.2.0-edge.2
```

## Notes

- The `AVIATIONWX_METADATA` block must be valid JSON
- The metadata is parsed by `aviationwx-supervisor.sh` using `grep` and basic parsing
- Keep metadata simple - complex logic should be in the host scripts, not the metadata
- Edge releases should be marked as "pre-release" in GitHub
- Stable releases should NOT be marked as "pre-release"
