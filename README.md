# TaxonRouter

**Dual-binary GitHub automation for real operators.**

TaxonRouter is an Apache 2.0 open-source toolkit that brings together:

1. **`taxonrouter-mcp`** — MCP stdio server for programmatic GitHub ProjectsV2 management (cards, fields, options)
2. **`taxonrouter-auto-tagger`** — webhook daemon that classifies PRs and auto-applies labels + project fields

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    TaxonRouter (go.mod)                     │
│                                                             │
│  pkg/                                                       │
│    domain/      — shared pure types (no I/O)               │
│    github/      — shared GraphQL client (JWT+gh fallback)  │
│    rules/       — table-driven classifier (pure, I/O-free)  │
│                                                             │
│  internal/                                                   │
│    mcp/         — MCP tool handlers + stdio transport       │
│    classifier/  — PR classification engine (rules + LLM)      │
│    apply/       — GitHub write-back pipeline                 │
│    webhook/     — webhook receiver with HMAC verify          │
│    adapter/     — rules + hybrid rules+LLM engine           │
│    llm/         — LLM providers (OpenAI / Anthropic)        │
│                                                             │
│  cmd/                                                       │
│    taxonrouter-mcp/           — MCP binary                  │
│    taxonrouter-auto-tagger/   — webhook daemon binary        │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Binary install

```bash
go install github.com/LOUST-PRO/TaxonRouter/cmd/taxonrouter-mcp@latest
go install github.com/LOUST-PRO/TaxonRouter/cmd/taxonrouter-auto-tagger@latest
```

### MCP server (taxonrouter-mcp)

```bash
# Configure project ID
mkdir -p ~/.config/taxonrouter-mcp
cat > ~/.config/taxonrouter-mcp/config.toml <<'EOF'
project_id = "PVT_your_project_id_here"
default_limit = 20
max_limit = 100
cache_ttl = "5m"
EOF

# Run
taxonrouter-mcp
```

**Environment variables:**

| Variable | Description |
|---|---|
| `GITHUB_APP_ID` | GitHub App ID (enables JWT auth) |
| `GITHUB_APP_INSTALLATION_ID` | GitHub App installation ID |
| `GITHUB_APP_PRIVATE_KEY_FILE` | Path to `.pem` private key |
| `GITHUB_TOKEN` | PAT fallback (if no App credentials) |
| `LZT_GITHUB_PROJECTS_CONFIG` | Override config file path |

### Auto-tagger daemon (taxonrouter-auto-tagger)

```bash
# Environment
export WEBHOOK_SECRET="your_hmac_secret"
export GITHUB_TOKEN="ghp_your_token"
export GITHUB_PROJECT_NUMBER="42"
export LLM_PROVIDER="anthropic"   # or "openai" or "none" (rules-only)
export LLM_API_KEY="sk-ant-..."
export PORT="3013"

# Run
taxonrouter-auto-tagger
```

**Health endpoints:**
- `GET /healthz` — liveness
- `GET /readyz` — readiness + classifier smoke check
- `GET /admin/suggestions?since=RFC3339` — manual review queue

## GitHub App vs PAT

**GitHub App (recommended for production):**
```bash
export GITHUB_APP_ID=123456
export GITHUB_APP_INSTALLATION_ID=987654
export GITHUB_APP_PRIVATE_KEY_FILE=/path/to/app.private-key.pem
```
JWT auth is ~50ms faster per call and supports fine-grained repo permissions.

**PAT (fallback):**
```bash
export GITHUB_TOKEN="ghp_..."
```

## MCP Tools (taxonrouter-mcp)

### Read tools (intent: `read`)

| Tool | Description |
|---|---|
| `cards_list` | List project cards with field values. `limit` 1-100. |
| `field_options` | Get field metadata + option list. Cached 5min. |

### Write tools (intent: `mutate`)

| Tool | Description |
|---|---|
| `cards_add_existing` | Add a PR/issue to project by node ID. |
| `current_fields` | Read current field values on a card (drift detection). |
| `cards_update_fields` | Update fields. Accepts option names ("In Progress") or IDs ("47fc9ee4"). |

### Field name mapping (auto-tagger)

Classifier output (e.g. `Effort: S-hours`) is mapped to project field names via env:

```bash
GITHUB_PROJECT_FIELD_MAPPING="Effort:Size,Phase:Status"
GITHUB_PROJECT_FIELD_VALUE_MAPPING="Size:S-hours=S,Size:M-hours=M"
```

## GitHub Actions Integration

### Self-hosted runner (persistent daemon)

Run `taxonrouter-auto-tagger` as a systemd service on your self-hosted runner:

```bash
sudo systemctl enable taxonrouter-auto-tagger
```

The daemon listens for webhook events from GitHub and applies classification automatically.

### GitHub-hosted (event-driven via Actions)

For GitHub-hosted runners that don't support persistent processes, use the `taxonrouter-mcp` with GitHub Actions:

```yaml
# .github/workflows/taxonrouter.yml
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  classify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go install github.com/LOUST-PRO/TaxonRouter/cmd/taxonrouter-mcp@latest
      - env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          WEBHOOK_SECRET: ${{ secrets.WEBHOOK_SECRET }}
      - run: taxonrouter-mcp
        # Connect via Claude Code MCP broker or direct stdio
```

## Deployment

### Docker

```dockerfile
FROM gcr.io/distroless/static:nonroot
COPY taxonrouter-auto-tagger /taxonrouter-auto-tagger
COPY taxonrouter-mcp /taxonrouter-mcp
ENTRYPOINT ["/taxonrouter-auto-tagger"]
```

### Systemd service

```ini
[Unit]
Description=TaxonRouter Auto-Tagger
After=network.target

[Service]
ExecStart=/usr/local/bin/taxonrouter-auto-tagger
Environment="PORT=3013"
Environment="WEBHOOK_SECRET=..."
EnvironmentFile=/etc/taxonrouter/env
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

## Security

- HMAC-SHA256 webhook signature verification
- PEM key permission check at startup (0600 required)
- Field/project IDs validated via allowlist regex before any GraphQL call
- Read-only MCP tools are invisible in `read` intent; write tools require `mutate` intent
- No secrets in logs; all credentials from environment

## License

Apache 2.0 — see [LICENSE](LICENSE)
