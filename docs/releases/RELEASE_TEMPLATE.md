# GitHub Release Template

This document defines the format for GitHub releases and the machine-readable metadata used by the auto-update system.

## Release Format

Each release should follow this structure:

```markdown
## What's Changed

- Feature: Brief description
- Fix: Brief description
- Breaking: Major change description (if any)

<!-- AVIATIONWX_RELEASE_META {"version": "2.11.0", "min_host_version": "2.3", "critical": true, "force_update": false, "rollback_safe": true, "deprecates": ["2.10.0", "2.10.1"]} -->

## Full Changelog

https://github.com/alexwitherspoon/aviationwx.org-bridge/compare/v2.10.1...v2.11.0
```

CI (`.github/workflows/release.yml`) emits a default `AVIATIONWX_RELEASE_META` block when a tag is pushed. Edit the published release notes after CI if you need `deprecates`, `critical`, or extra operator context.

## Metadata block

Use an HTML comment so the JSON is machine-readable but unobtrusive in rendered release notes:

```html
<!-- AVIATIONWX_RELEASE_META {"version": "2.11.0", "min_host_version": "2.3", ...} -->
```

The JSON must be on one line inside the comment.

### Fields

| Field | Required | Parsed by supervisor | Description |
|-------|----------|----------------------|-------------|
| `version` | Yes | No | Container release version (no `v` prefix) |
| `min_host_version` | Yes | **Yes** | Minimum `aviationwx-supervisor.sh` version (`SCRIPT_VERSION`). Host scripts update before the container when older. |
| `deprecates` | No | **Yes** | Semver strings **without** `v` prefix (e.g. `"2.10.0"`). Listed in release notes; supervisor skips upgrading **to** a deprecated target version. |
| `critical` | No | No | Operator signal for security-sensitive releases. Not enforced by `aviationwx-supervisor.sh` today. |
| `force_update` | No | No | Operator signal to prioritize fleet rollout. Not enforced by the supervisor today. |
| `rollback_safe` | No | No | Documented expectation; rollback uses `last-known-good.txt` and health checks. |
| `edge_stable_commit` | Edge only | No | Git commit hash after edge stability verification |

**Removed in 2.11.0:** `upload_ssh_host_keys_sha256` in release metadata. SFTP host key trust uses the HTTPS roster at `https://{upload.host}/.well-known/aviationwx-upload-ssh-host-keys.json` (see `SECURITY.md`).

### What the supervisor enforces

From `scripts/aviationwx-supervisor.sh`:

- **Boot** and **daily** (`aviationwx-daily-update.timer`, midnight local + up to 30 min jitter) run the same upgrade check.
- Target version comes from the GitHub release for the bridge's `update_channel` (`global.json`, default `latest`).
- Applies when installed semver is behind target.
- **`MIN_RELEASE_AGE_HOURS=2`**: skips releases younger than 2 hours unless `force-update`, web UI trigger, or boot after watchdog recovery.
- **`min_host_version`**: pulls newer host scripts from `main` before container update when needed.
- **`deprecates`**: if the target version appears in the list, the upgrade is skipped.
- Post-update health failure rolls back to `last-known-good.txt`.

`critical` and `force_update` belong in release notes for operators; do not assume the supervisor reads them until host script support is added.

## Release Checklist

When creating a new release:

- [ ] Update `CHANGELOG.md` with all changes
- [ ] Tag follows semantic versioning: `vMAJOR.MINOR.PATCH`
- [ ] Release notes include `AVIATIONWX_RELEASE_META` (edit after CI if needed)
- [ ] `min_host_version` matches current `SCRIPT_VERSION` in `aviationwx-supervisor.sh` when host behavior changed
- [ ] `deprecates` lists semver strings without `v` prefix
- [ ] For edge releases: add `edge_stable_commit` after stability verification
- [ ] Docker image is built and published automatically by CI
- [ ] Test installation on a clean low-memory SBC, e.g. a 1GB Pi 4-class board (if major release)

## Example: stable release

```markdown
## AviationWX.org Bridge v2.11.0

Restores SFTP SSH host key verification via HTTPS roster.

<!-- AVIATIONWX_RELEASE_META {"version": "2.11.0", "min_host_version": "2.3", "critical": true, "force_update": false, "rollback_safe": true, "deprecates": ["2.10.0", "2.10.1"]} -->

### What's changed

- Upload: HTTPS SFTP host key roster client with on-disk cache
- Web console: Settings panel for SSH host key status

## Full Changelog

https://github.com/alexwitherspoon/aviationwx.org-bridge/compare/v2.10.1...v2.11.0
```

## Notes

- Keep metadata JSON simple; complex logic belongs in host scripts.
- Edge releases should be marked as pre-release in GitHub.
- Stable releases should not be marked as pre-release.
