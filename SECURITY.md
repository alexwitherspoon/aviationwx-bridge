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
