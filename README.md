# Chart Release Inspector

[![CI](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml/badge.svg)](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtpot/chart-release-inspector?display_name=tag&sort=semver)](https://github.com/imtpot/chart-release-inspector/releases)
[![License](https://img.shields.io/github/license/imtpot/chart-release-inspector)](LICENSE)

**Know what changes before your Helm chart does.**

`chart-release-inspector` is a standalone CLI for reviewing Helm repository and OCI chart upgrades. It resolves chart versions, detects application transitions, diffs packaged `values.yaml`, and aggregates upstream GitHub release notes—all **without** contacting your Kubernetes cluster or applying any changes.

Designed for humans making upgrade decisions and CI/CD pipelines that require a stable JSON contract.

---

## Why Use It

*   🔒 **Zero-Cluster Access:** Safe by design. It never connects to your Kubernetes cluster or needs cluster credentials.
*   🔍 **Default `values.yaml` Diffing:** Instantly see changes in default values before merging your configuration.
*   📦 **OCI & Classic Helm Support:** A unified interface for classic Helm repositories and `oci://` registries.
*   📝 **Smart Upstream Release Notes:** Automatically retrieves and maps upstream release notes from GitHub, adjusting for custom repo mappings and tag formats.
*   🤖 **CI/CD Ready:** Exit codes indicate update availability (`10`) or errors (`20`). Deterministic JSON output makes it trivial to plug into GitOps and PR automation.

---

## 🤖 AI & Agent Friendly

`chart-release-inspector` is optimized for integration with LLM coding assistants, autonomous agents, and GitOps workflows:

*   **Structured Context for LLMs:** Returns version history, value diffs, and release notes in a clean, deterministic JSON format—perfect to feed directly into agent contexts or LLM prompts.
*   **Automated PR Summaries:** Run the inspector in CI/CD to let your AI agents automatically analyze upstream Helm changes, highlight key configuration shifts, and write high-quality pull request summaries.
*   **Predictable Scripting:** Simple semantic exit codes allow agents to make fast branching decisions (e.g., `0` = skip, `10` = update/raise PR, `20` = alert developer) without parsing raw logs.

---

## Quick Start

### 1. Install

**Via Go:**
```sh
go install github.com/imtpot/chart-release-inspector/cmd/chart-release-inspector@latest
```

**Via [mise](https://mise.jdx.dev/):**

Pin the version in your `mise.toml` for reproducible team installs:
```toml
[tools]
"github:imtpot/chart-release-inspector" = "0.2.0"
```
```sh
mise install
```
*(Note: If mise hides a newly published release, override with `mise install --minimum-release-age 0d github:imtpot/chart-release-inspector@0.2.0`)*

*Or download a pre-built binary for Linux, macOS, or Windows from [GitHub Releases](https://github.com/imtpot/chart-release-inspector/releases).*

---

### 2. Inspect a Chart Upgrade

See version transitions and default value changes:

```sh
chart-release-inspector inspect \
  --chart external-secrets \
  --repository https://charts.external-secrets.io \
  --version 2.1.0 \
  --values-diff
```

For **OCI charts**, pass the registry reference directly:

```sh
chart-release-inspector inspect \
  --chart oci://ghcr.io/grafana/helm-charts/grafana \
  --version 10.5.14
```

---

### 3. Batch Checks

Audit multiple charts at once using a declarative YAML file:

```yaml
# charts.yaml
charts:
  - chart: external-secrets
    repository: https://charts.external-secrets.io
    version: 2.7.0
    values_diff: true
  - chart: oci://ghcr.io/grafana/helm-charts/grafana
    version: 10.5.14
```

Run the batch check (individual failures won't block the remaining checks):
```sh
chart-release-inspector batch --file charts.yaml > report.json
```

---

## Smart Release Notes Mapping

When Helm chart repositories and upstream application source repositories differ, map them to fetch correct release notes:

```yaml
# release-notes.yaml
rules:
  - chart: mimir-distributed
    repository: https://github.com/grafana/mimir
    tag_template: mimir-{version}
```

Use it in your checks:
```sh
chart-release-inspector batch \
  --file charts.yaml \
  --release-notes-config release-notes.yaml \
  --release-note-limit 2000
```

Validate configuration locally without network calls:
```sh
chart-release-inspector config validate release-notes.yaml
```

---

## Automation Contract

Integrate it cleanly into your CI/CD pipelines using deterministic JSON and exit codes:

| Exit Code | Meaning |
| :---: | :--- |
| **`0`** | Lookup completed successfully (current or update available). |
| **`10`** | Updates available (only with `--fail-on-update`). |
| **`20`** | Upstream lookup or validation failed. |

By default, both `current` and `update_available` exit with `0`. Pass `--fail-on-update` to exit with `10` when updates are available — useful for CI/CD gates:

```sh
chart-release-inspector batch --file charts.yaml --fail-on-update
```

Get JSON output using `--output json`:
```sh
chart-release-inspector inspect --chart external-secrets ... --output json
```

---

## Commands

Run `chart-release-inspector --help` to see all available commands and flags.

*   `inspect` — Inspect a single Helm repo or OCI chart.
*   `batch` — Inspect multiple charts defined in a YAML manifest.
*   `config validate` — Validate a release notes config file locally.
*   `version` — Print binary version.

---

## Contributing & License

Contributions that improve chart compatibility, release-note conventions, and automation integration are welcome. 

```sh
go test ./...
go vet ./...
go build ./cmd/chart-release-inspector
```

Licensed under the [Apache License 2.0](LICENSE).
