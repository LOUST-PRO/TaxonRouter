# Support

## Getting Help

- **Questions**: Open a [Discussion](https://github.com/LOUST-PRO/TaxonRouter/discussions)
- **Bugs**: Open a [GitHub Issue](https://github.com/LOUST-PRO/TaxonRouter/issues)
- **Security**: See [SECURITY.md](./SECURITY.md)

## Resources

| Resource | Link |
|---|---|
| Architecture | See `pkg/domain/types.go` and `internal/` directories |
| Go Docs | Run `go doc ./...` after `make tidy` |
| Examples | See `cmd/` directories for entry points |

## Status

| Component | Status |
|---|---|
| `cmd/taxonrouter-mcp` | MCP server for PR classification |
| `cmd/taxonrouter-auto-tagger` | Webhook daemon for auto-labeling |

Monitor the [GitHub Actions CI](./.github/workflows/ci.yml) for build and test status.
