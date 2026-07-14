# Chart Release Inspector

[![CI](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml/badge.svg)](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtpot/chart-release-inspector?display_name=tag&sort=semver)](https://github.com/imtpot/chart-release-inspector/releases)
[![License](https://img.shields.io/github/license/imtpot/chart-release-inspector)](LICENSE)

Know what changes before your Helm chart does.

`chart-release-inspector` is a standalone CLI for reviewing Helm repository and
OCI chart upgrades. It finds the selected chart version, resolves the matching
application version, compares packaged `values.yaml`, and collects relevant
GitHub release notes. It never applies an upgrade or contacts your Kubernetes
cluster.

Built for humans making an upgrade decision and for automation that needs a
small, stable JSON contract.

## Why Use It

- Review chart and application version changes together.
- See default `values.yaml` changes before updating your own values.
- Read upstream release notes using each project's tag conventions.
- Check many independent charts from one source-neutral YAML manifest.
- Use semantic exit codes and deterministic JSON in CI, bots, and IaC adapters.
- Support both classic Helm repositories and `oci://` charts with one command.

## Quick Start

Install with Go 1.26 or newer:

```sh
go install github.com/imtpot/chart-release-inspector/cmd/chart-release-inspector@latest
```

Inspect a specific upgrade, including the default values change:

```sh
chart-release-inspector inspect \
  --chart external-secrets \
  --repository https://charts.external-secrets.io \
  --current-version 2.1.0 \
  --target-version 2.2.0 \
  --values-diff
```

Omit `--target-version` to inspect the newest stable chart version. For OCI
charts, use the full reference; no separate repository is needed:

```sh
chart-release-inspector inspect \
  --chart oci://ghcr.io/grafana/helm-charts/grafana \
  --current-version 10.5.14 \
  --output json
```

## Install

Download a checksummed archive for Linux, macOS, or Windows from
[GitHub Releases](https://github.com/imtpot/chart-release-inspector/releases),
or install with Go as shown above.

### mise

Pin a release in project `mise.toml` for reproducible team installs:

```toml
[tools]
"github:imtpot/chart-release-inspector" = "0.2.0"
```

```sh
mise install
mise exec -- chart-release-inspector version
```

mise can intentionally hide a freshly published release until it reaches the
configured minimum release age. To install a new version immediately, use this
one-time override while retaining the version pin above:

```sh
mise install --minimum-release-age 0d github:imtpot/chart-release-inspector@0.2.0
```

To build a local checkout:

```sh
go build -o bin/chart-release-inspector ./cmd/chart-release-inspector
```

## Upgrade Review

The default terminal output is deliberately compact: version transitions first,
then an optional values diff and release notes. Use JSON when another tool needs
the report:

```sh
chart-release-inspector inspect \
  --chart external-secrets \
  --repository https://charts.external-secrets.io \
  --current-version 2.1.0 \
  --target-version 2.2.0 \
  --values-diff \
  --output json > external-secrets-2.2.0.json
```

`--output terminal` is the default. It respects `NO_COLOR`; use
`--color always` or `--color never` when scripting terminal output.

## Batch Checks

Keep chart checks in a portable manifest and run them in one command:

```sh
chart-release-inspector batch --file charts.yaml > report.json
```

```yaml
charts:
  - chart: external-secrets
    repository: https://charts.external-secrets.io
    current_version: 2.7.0
    values_diff: true
  - chart: oci://ghcr.io/grafana/helm-charts/grafana
    current_version: 10.5.14
```

Every manifest entry is inspected in order. A failing chart does not prevent
the other checks from completing. Batch always writes JSON to stdout, making it
easy to redirect, archive, or pass to another command. Start with the
ready-to-run [`charts.example.yaml`](charts.example.yaml).

## Release Notes

The inspector uses a chart's metadata by default. Add a release-notes rule when
an upstream project uses a different GitHub repository, tag prefix, or version
source:

```yaml
rules:
  - chart: ingress-nginx
    provider: github
    repository: kubernetes/ingress-nginx
    tag_template: controller-v{version}
    version: application
```

Use the configuration with either `inspect` or `batch`:

```sh
chart-release-inspector batch \
  --file charts.yaml \
  --release-notes-config release-notes.yaml \
  --release-note-limit 2000 > report.json
```

`version` defaults to `application`; set it to `chart` for projects that publish
chart-version release notes. Validate configuration without any network calls:

```sh
chart-release-inspector config validate release-notes.yaml
```

See [`release-notes.example.yaml`](release-notes.example.yaml) for additional
rules. Set `GITHUB_TOKEN` (preferred) or `GH_TOKEN` to raise GitHub API limits.
Tokens are read only from the environment and are never written to output.

## Automation Contract

All JSON output is pretty-printed and stable for automation. Individual inspect
reports use `schema_version: 4`; batch envelopes use `schema_version: 1`, and
their `results` entries use the individual schema.

| Status | Meaning | Exit code |
| --- | --- | --- |
| `current` | The selected target is already current. | `0` |
| `update_available` | A newer target is available. | `10` |
| `error` | Input validation or an upstream lookup failed. | `20` |

JSON is still emitted when a command exits with `20`. `values_diff` and release
note `body_preview` values are arrays of lines, not escaped multiline strings.
`body_characters` and `truncated` describe the complete upstream release body.

## Commands

```text
chart-release-inspector inspect [flags]
chart-release-inspector batch --file charts.yaml
chart-release-inspector config validate <release-notes.yaml>
chart-release-inspector version
```

| Flag | Description |
| --- | --- |
| `--chart` | Helm chart name or `oci://` chart reference. |
| `--repository` | Helm repository URL; required for a non-OCI chart. |
| `--current-version` | Installed or configured chart version. |
| `--target-version` | Target chart version; defaults to the newest stable release. |
| `--values-diff` | Compare packaged default `values.yaml` files. |
| `--release-note-limit` | Maximum returned release-note characters; `0` keeps the full body. |
| `--release-notes-config` | YAML configuration for chart-specific upstream release rules. |
| `--output` | `terminal` (default) or `json`; applies to `inspect`. |
| `--color` | `auto` (default), `always`, or `never`; applies to terminal output. |

## Contributing

Contributions that improve chart compatibility, release-note conventions,
documentation, and automation integration are welcome. Keep the tool neutral:
it should inspect charts, not depend on a particular deployment system.

```sh
go test ./...
go vet ./...
go build ./cmd/chart-release-inspector
```

GitHub Actions runs the same checks for pull requests and pushes to `main`.
Tags matching `vX.Y.Z` publish checksummed archives through GoReleaser.

## License

Licensed under the [Apache License 2.0](LICENSE).
