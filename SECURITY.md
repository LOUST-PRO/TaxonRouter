# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x.y   | :white_check_mark: |

## Reporting a Vulnerability

To report a security vulnerability, please DO NOT open a public GitHub Issue.

Instead, contact the maintainers privately:

1. **Email**: `security@loust.pro`
2. **GitHub Private Vulnerability Reporting**: Use the "Security" tab → "Advisories" → "Report a vulnerability"

Please include as much of the following as possible:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested mitigation (if any)

## Disclosure Policy

- We aim to respond within **48 hours** to acknowledge receipt.
- We aim to publish a fix within **14 days**.
- We follow a **90-day coordinated disclosure** timeline.
- Public disclosure will be coordinated with the reporter.

## Security-relevant Scope

| Component | Notes |
|---|---|
| `pkg/github/client.go` | Handles GitHub API tokens — treat as secret |
| `internal/webhook/webhook.go` | Receives untrusted external payloads |
| `internal/llm/` | External LLM calls — do not trust unvalidated LLM output |

For threat modeling context, see the architecture in `pkg/domain/types.go` and `internal/classifier/`.
