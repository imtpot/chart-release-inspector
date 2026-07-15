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
*   🤖 **CI/CD Ready:** Deterministic JSON output and optional `--fail-on-update` exit code make it trivial to plug into GitOps and PR automation.

---

## 🤖 AI & Agent Friendly

`chart-release-inspector` is optimized for integration with LLM coding assistants, autonomous agents, and GitOps workflows:

*   **Structured Context for LLMs:** Returns version history, value diffs, and release notes in a clean, deterministic JSON format—perfect to feed directly into agent contexts or LLM prompts.
*   **Automated PR Summaries:** Run the inspector in CI/CD to let your AI agents automatically analyze upstream Helm changes, highlight key configuration shifts, and write high-quality pull request summaries.
*   **Predictable Scripting:** Simple semantic exit codes allow agents to make fast branching decisions (e.g., `0` = success, `20` = error; pass `--fail-on-update` to exit `10` on available updates) without parsing raw logs.

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
"github:imtpot/chart-release-inspector" = "0.5.0"
```
```sh
mise install
```
*(Note: If mise hides a newly published release, override with `mise install --minimum-release-age 0d github:imtpot/chart-release-inspector@0.5.0`)*

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

Audit multiple charts at once using a declarative YAML manifest (see [`charts.example.yaml`](charts.example.yaml)). The manifest can embed its own release-note mapping rules directly:

```sh
chart-release-inspector batch --file charts.example.yaml
```

Export as JSON for automation:
```sh
chart-release-inspector batch --file charts.example.yaml --output json > report.json
```

### Batch Manifest Chart Reference

Each entry under `charts:` supports the following fields:

*   `chart` (required): Helm chart name or OCI registry URL reference.
*   `version` (required): The current/installed version to inspect from.
*   `target_version` (optional): The specific version to inspect upgrade to. If omitted, queries the latest stable upstream version.
*   `repository` (optional): Helm repository URL (only for non-OCI charts).
*   `values_diff` (optional): Set to `true` to always compute and show the values diff for this chart.

---

## Smart Release Notes Mapping

When Helm chart repositories and upstream application source repositories differ, map them to fetch correct release notes.

Define rules directly under the `rules:` section in your batch manifest.

Validate a batch manifest file locally without network calls:
```sh
chart-release-inspector manifest validate charts.example.yaml
```

### Rule Reference

*   `chart` (required): Helm chart name or OCI registry suffix.
*   `repository` (required): Full GitHub repository URL of the application source.
*   `tag_template` (optional): Template to match upstream GitHub tags, e.g. `v{version}` or `controller-v{version}`.
*   `tag_source` (optional): Set to `app_version` (default, uses `appVersion` from `Chart.yaml`) or `chart_version` (uses chart's own version).

---

## GitHub API Rate Limits & Client Strategy

By default, the tool resolves release notes by querying the GitHub API (`api.github.com`). Since unauthenticated API requests are limited to 60/hour, the tool automatically falls back to web-scraping the public release page (`github.com/.../releases`) when rate-limited.

You can configure or force this behavior globally using the `--github-client` flag:

*   `--github-client auto` (default): Try REST API first, fallback to HTML scraping on error/rate limit.
*   `--github-client api`: REST API only; fails immediately if rate-limited or unauthorized. Useful in CI/CD with `GITHUB_TOKEN` to fail fast on authentication issues.
*   `--github-client html`: HTML scraper only; bypasses the GitHub API completely. Useful locally to preserve your API quota.

For higher API rate limits, set `GITHUB_TOKEN` (or `GH_TOKEN`) in your environment.

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
chart-release-inspector batch --file charts.example.yaml --fail-on-update
```

Get JSON output using `--output json`:
```sh
chart-release-inspector batch --file charts.example.yaml --output json
```

---

## Commands

Run `chart-release-inspector --help` to see all available commands and flags.

*   `inspect` — Inspect a single Helm repo or OCI chart.
*   `batch` — Inspect multiple charts defined in a YAML manifest.
*   `manifest validate` — Validate a batch manifest file locally.
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
