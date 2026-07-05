# Contributing to TaxonRouter

Thank you for your interest in contributing to TaxonRouter.

## How to Contribute

### Reporting Issues

- **Bugs**: Open a [GitHub Issue](https://github.com/LOUST-PRO/TaxonRouter/issues) with `bug` label.
- **Features**: Open a [GitHub Issue](https://github.com/LOUST-PRO/TaxonRouter/issues) with `enhancement` label.
- **Security**: See [SECURITY.md](./SECURITY.md). **Do not** open public issues for security vulnerabilities.

### Pull Requests

1. **Fork** the repository and create a branch from `main`.
2. **Small, focused PRs** are preferred over large, sweeping changes.
3. For PRs that touch multiple subsystems, open separate PRs per concern.
4. All PRs must pass CI before review.
5. After opening the PR, ensure the checklist in the PR template is complete.

### Development Setup

```bash
# Clone your fork
git clone git@github.com:YOUR_USER/TaxonRouter.git
cd TaxonRouter

# Install dependencies
make tidy

# Run tests
make test

# Build both binaries
make build
```

### Code Style

- Run `make lint` before committing.
- Go code follows `gofmt` and `goimports` conventions.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).

### Commit Signing

All commits must be signed. See [GitHub's documentation on commit signing](https://docs.github.com/en/authentication/managing-commit-signature-verification).

```bash
# Configure signing key
git config --global user.signingkey YOUR_SIGNING_KEY

# Sign commits automatically
git config --global commit.gpgsign true
```

## License

By contributing, you agree your contributions will be licensed under the Apache License 2.0.
