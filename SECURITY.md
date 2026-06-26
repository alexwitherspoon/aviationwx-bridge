# Security Policy

AviationWX.org Bridge is safety-critical field software (image freshness, accurate timestamps, per-camera degradation). We take security reports seriously.

## Supported versions

Security fixes are released for the **current minor release line** (latest patch on `main` and the most recent tagged release). Older releases may not receive backports unless the issue is critical.

| Version | Supported |
| ------- | --------- |
| 2.9.x   | Yes       |
| 2.8.x and older | No |

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately to **[contact@aviationwx.org](mailto:contact@aviationwx.org)** with:

- A clear description of the issue and likely impact
- Steps to reproduce (version, config redacted, logs if helpful)
- Whether you believe it affects timestamp integrity, uploads, or local console access

We will acknowledge receipt as soon as practical (typically within a few business days), investigate, and coordinate a fix and disclosure timeline. We appreciate responsible disclosure.

## Scope

**In scope**

- This repository: bridge daemon, web console, queue/upload path, install/host scripts, container image build
- Issues that could mislead pilots or operators via forged or stale imagery, credential leakage, or unauthorized local control

**Out of scope**

- The aviationwx.org website or services not maintained in this repo (report those to the same contact address and we will route if needed)
- Deployments intentionally exposed to the public internet without firewall or VPN hardening (the bridge is designed for a **trusted LAN**; see [deployment security](docs/DEPLOYMENT.md#security))

## Safe harbor

We do not pursue legal action against researchers who follow this policy and act in good faith.

## Automated analysis

Static analysis runs via [CodeQL](.github/workflows/codeql.yml) for **Go** and **JavaScript** (web console) on `main`, pull requests, and a weekly schedule. Findings appear under the repository **Security** tab when GitHub Advanced Security features are available for the repo.

### Code scanning notes

| Finding | Handling |
| ------- | -------- |
| SFTP host keys | TOFU in `{config_dir}/ssh_known_hosts`. Trusted roster from `https://{upload.host}/.well-known/aviationwx-upload-ssh-host-keys.json` (TLS-verified, cached in `{config_dir}/upload_ssh_trusted_keys.json`). When HTTPS is unreachable, the bridge uses the last cached roster only. Mismatched keys fail closed; trusted roster rotations update the pin without operator action. |
| Camera config paths | Camera IDs are restricted to alphanumeric characters and hyphens; config file paths must stay under `cameras/`. |
| Web console password in `sessionStorage` | Accepted for LAN-only Basic Auth UX; cleared on 401. XSS on the console remains the residual risk. |
| Update dialog `confirm()` text | Version strings are shown in a native `confirm()` (plain text, not HTML); tag-stripping is defensive only. |
| Log tail allocation | `/api/logs?tail=` is capped at 1000 lines in the handler and in `GetLast`. The buffer builds the tail with bounded `append` after clamping, using a constant capacity hint (not `make` sized from the request), which satisfies CodeQL `go/uncontrolled-allocation-size`. |
| SSH host key probe | `ProbeSSHHostKeyFingerprint` accepts any host key to read the live fingerprint for the Settings UI (same role as `ssh-keyscan`). SFTP uploads use `hostKeyStore` TOFU plus HTTPS roster verification. CodeQL `go/insecure-hostkeycallback` is suppressed on the probe callback with an inline annotation. |
