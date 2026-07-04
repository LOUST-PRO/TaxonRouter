package classifier

import "regexp"

// DefaultTitleRules returns the v1 title rules.
func DefaultTitleRules() []TitleRule {
	return []TitleRule{
		{Prefix: "fix/", Label: "fix", Phase: "fix", Weight: 0.5},
		{Prefix: "feat/", Label: "enhancement", Phase: "feat", Weight: 0.5},
		{Prefix: "hardening/", Label: "hardening", Phase: "hardening", Weight: 0.5},
		{Prefix: "experiment/", Label: "experiment", Phase: "experiment", Weight: 0.3},
		{Prefix: "lab/", Label: "lab", Phase: "lab", Weight: 0.3},
		{Prefix: "audit/", Label: "audit", Phase: "audit", Weight: 0.5},
		{Prefix: "chore/", Label: "chore", Phase: "chore", Weight: 0.3},
		{Prefix: "refactor/", Label: "refactor", Phase: "refactor", Weight: 0.3},
		{Prefix: "review/", Label: "review", Phase: "review", Weight: 0.2},
		{Prefix: "spike/", Label: "spike", Phase: "spike", Weight: 0.2},
		{Prefix: "baremetal/", Label: "baremetal", Phase: "baremetal", Weight: 0.4},
		{TitleRE: regexp.MustCompile(`(?i)^(?:fix|bug|hotfix)[\s:]`), Label: "fix", Phase: "fix", Weight: 0.4},
		{TitleRE: regexp.MustCompile(`(?i)^(?:feat|feature)[\s:]`), Label: "enhancement", Phase: "feat", Weight: 0.4},
		{TitleRE: regexp.MustCompile(`(?i)^(?:harden|security)[\s:]`), Label: "hardening", Phase: "hardening", Weight: 0.4},
	}
}

// DefaultBodyRules returns the v1 body keyword rules.
func DefaultBodyRules() []BodyRule {
	securityRE := regexp.MustCompile(`(?i)\b(security|vuln|exploit|cve|xss|sql.?inject|csrf|rce|path.?traversal|auth.?bypass|priv.?esc)\b`)
	breakingRE := regexp.MustCompile(`(?i)\b(breaking|breaking.?change|api.?change| backwards.?incompatible)\b`)
	deprecationRE := regexp.MustCompile(`(?i)\b(deprecate|deprecated|remove.?in|will.?remove)\b`)
	perfRE := regexp.MustCompile(`(?i)\b(perf|performance|optimi[zs]|latency|throughput)\b`)
	docsRE := regexp.MustCompile(`(?i)\b(docs?|documentation|readme|changelog)\b`)
	testsRE := regexp.MustCompile(`(?i)\b(tests?|testing|test.?cov|unit.?test|e2e)\b`)
	proxyRE := regexp.MustCompile(`(?i)\b(proxy|gate.?way|gost|quic|tunnel)\b`)
	infraRE := regexp.MustCompile(`(?i)\b(infra|infrastructure|nginx|systemd|deploy|docker|kubernetes)\b`)
	dbRE := regexp.MustCompile(`(?i)\b(schema|migration|postgres|mysql|mongo|redis|orm|database)\b`)
	ciRE := regexp.MustCompile(`(?i)\b(ci|cd|pipeline|github.?action|gitlab.?ci|jenkins)\b`)
	hardeningRE := regexp.MustCompile(`(?i)\b(harden|security.?control|tls|ssl|cert|mfa|2fa|rate.?limit)\b`)

	return []BodyRule{
		{WordRE: securityRE, Label: "security", Category: "security", Weight: 0.6},
		{WordRE: breakingRE, Label: "breaking", Category: "breaking", Weight: 0.6},
		{WordRE: deprecationRE, Label: "deprecation", Category: "deprecation", Weight: 0.5},
		{WordRE: perfRE, Label: "performance", Category: "performance", Weight: 0.5},
		{WordRE: docsRE, Label: "docs", Category: "docs", Weight: 0.3},
		{WordRE: testsRE, Label: "tests", Category: "tests", Weight: 0.3},
		{WordRE: proxyRE, Label: "proxy-affects", Category: "proxy", Weight: 0.5},
		{WordRE: infraRE, Label: "infra", Category: "infra", Weight: 0.4},
		{WordRE: dbRE, Label: "db-schema", Category: "database", Weight: 0.5},
		{WordRE: ciRE, Label: "ci", Category: "ci", Weight: 0.3},
		{WordRE: hardeningRE, Label: "hardening", Category: "security", Weight: 0.5},
	}
}

// DefaultPathRules returns the v1 path rules.
func DefaultPathRules() []PathRule {
	return []PathRule{
		{Glob: "cmd/lzt-hub*/**", Label: "proxy-affects", ProxyAffects: "cmd", Weight: 0.5},
		{Glob: "cmd/lzt-*/**", Label: "proxy-affects", ProxyAffects: "cmd", Weight: 0.4},
		{Glob: "crates/lzt-hub*/**", Label: "proxy-affects", ProxyAffects: "core", Weight: 0.5},
		{Glob: "proxy/**", Label: "proxy-affects", ProxyAffects: "proxy", Weight: 0.5},
		{Glob: "internal/proxy/**", Label: "proxy-affects", ProxyAffects: "proxy", Weight: 0.5},
		{Glob: "internal/gateway/**", Label: "proxy-affects", ProxyAffects: "gateway", Weight: 0.5},
		{Glob: "**/*.go", Label: "lang:go", Lang: "Go", Weight: 0.3},
		{Glob: "**/*.rs", Label: "lang:rust", Lang: "Rust", Weight: 0.3},
		{Glob: "**/*.ts", Label: "lang:ts", Lang: "TypeScript", Weight: 0.3},
		{Glob: "**/*.tsx", Label: "lang:ts", Lang: "TypeScript", Weight: 0.3},
		{Glob: "**/*.py", Label: "lang:python", Lang: "Python", Weight: 0.3},
		{Glob: "**/*.sh", Label: "lang:shell", Lang: "Shell", Weight: 0.2},
		{Glob: "nginx/**", Label: "infra", TouchesInfra: "nginx", Weight: 0.4},
		{Glob: "systemd/**", Label: "infra", TouchesInfra: "systemd", Weight: 0.4},
		{Glob: "docker/**", Label: "infra", TouchesInfra: "docker", Weight: 0.4},
		{Glob: "compose/**", Label: "infra", TouchesInfra: "compose", Weight: 0.4},
		{Glob: "Dockerfile*", Label: "infra", TouchesInfra: "docker", Weight: 0.4},
		{Glob: "*.service", Label: "infra", TouchesInfra: "systemd", Weight: 0.4},
		{Glob: "**/schema.prisma", Label: "db-schema", DBSchema: "prisma", Weight: 0.5},
		{Glob: "**/migrations/**", Label: "db-schema", DBSchema: "sql", Weight: 0.5},
		{Glob: "**/*.sql", Label: "db-schema", DBSchema: "sql", Weight: 0.4},
		{Glob: ".github/workflows/**", Label: "ci", TouchesCI: "github-actions", Weight: 0.3},
		{Glob: ".gitlab-ci.yml", Label: "ci", TouchesCI: "gitlab-ci", Weight: 0.3},
		{Glob: "Makefile", Label: "ci", TouchesCI: "make", Weight: 0.2},
		{Glob: "*.md", Label: "docs-only", Weight: 0.2},
		{Glob: "docs/**", Label: "docs-only", Weight: 0.2},
	}
}

// DefaultEffortRules returns the v1 effort rules.
func DefaultEffortRules() []EffortRule {
	return []EffortRule{
		{MaxLOC: 49, Label: "S-hours", Effort: "S-hours", Weight: 0.3},
		{MaxLOC: 199, Label: "M-days", Effort: "M-days", Weight: 0.3},
		{MaxLOC: 499, Label: "L-days", Effort: "L-days", Weight: 0.3},
		{MaxLOC: 1499, Label: "XL-weeks", Effort: "XL-weeks", Weight: 0.3},
		{MaxLOC: 99999, Label: "XXL-months", Effort: "XXL-months", Weight: 0.3},
	}
}
