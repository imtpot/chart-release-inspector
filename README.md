# Chart Release Inspector

[![CI](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml/badge.svg)](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml)

`chart-release-inspector` checks whether a Helm chart has an available update.
It works with Helm repositories and OCI registries, resolves the chart's
application version, optionally compares packaged `values.yaml`, and can collect
matching GitHub release notes.

## Install

Use a published binary from GitHub Releases, or install the command with Go 1.26
or newer:

```sh
go install github.com/imtpot/chart-release-inspector/cmd/chart-release-inspector@latest
```

To build from a checkout:

```sh
go build -o bin/chart-release-inspector ./cmd/chart-release-inspector
```

## Quick Start

Inspect a chart from a Helm repository:

```sh
chart-release-inspector inspect \
  --chart external-secrets \
  --repository https://charts.external-secrets.io \
  --current-version 2.1.0 \
  --target-version 2.2.0 \
  --values-diff
```

Inspect an OCI chart. OCI references include their registry, so
`--repository` is not needed:

```sh
chart-release-inspector inspect \
  --chart oci://ghcr.io/grafana/helm-charts/grafana \
  --current-version 12.4.5 \
  --output json
```

When `--target-version` is omitted, the inspector selects the newest available
stable chart version. Pass `--target-version` to inspect a specific release.

## Command Reference

```text
chart-release-inspector inspect [flags]
chart-release-inspector config validate <release-notes.yaml>
chart-release-inspector version
```

| Flag | Description |
| --- | --- |
| `--chart` | Required chart name for a Helm repository, or an `oci://` chart reference. |
| `--repository` | Helm repository URL. Required for non-OCI charts. |
| `--current-version` | Installed or currently configured chart version. |
| `--target-version` | Specific target chart version. Defaults to the newest stable version. |
| `--values-diff` | Compare the default packaged `values.yaml` files. |
| `--release-note-limit` | Maximum number of release-note characters to return. `0` keeps the full body. |
| `--release-notes-config` | YAML file containing chart-specific upstream release-note rules. |
| `--output` | `terminal` (default) or `json`. |
| `--color` | `auto` (default), `always`, or `never`. |

Terminal output uses PTerm and follows `NO_COLOR`. JSON output contains no
terminal formatting.

`config validate` checks a release-notes configuration without contacting Helm
or GitHub. `version` prints the binary version; development builds report `dev`.

## Automation Contract

Every JSON response includes `schema_version: 4` and a `status` value:

| Status | Meaning | Exit code |
| --- | --- | --- |
| `current` | The selected target is already current. | `0` |
| `update_available` | A newer target is available. | `10` |
| `error` | Input validation or an upstream lookup failed. | `20` |

JSON is emitted even when the command exits with `20`. `values_diff` and each
release's `body_preview` are arrays of lines, avoiding escaped multiline YAML
and Markdown. The `body_characters` and `truncated` fields describe the complete
upstream release body.

## Release Notes

GitHub release notes are optional. Configure a chart-to-upstream mapping with
`--release-notes-config`:

```yaml
rules:
  - chart: ingress-nginx
    provider: github
    repository: kubernetes/ingress-nginx
    tag_template: controller-v{version}
    version: application
```

`version` is `application` by default and can be set to `chart`. The tag
template receives the resolved version as `{version}`. See
[`release-notes.example.yaml`](release-notes.example.yaml) for more examples.

Set `GITHUB_TOKEN` (preferred) or `GH_TOKEN` to raise GitHub API limits:

```sh
export GITHUB_TOKEN="$(gh auth token)"
chart-release-inspector inspect ...
```

The token is read only from the environment and is never written to command
output. If GitHub API traversal is unavailable, the inspector falls back to the
target release's public page and reports the degraded result in
`release_notes_error`.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/chart-release-inspector
```

GitHub Actions runs the same checks for pull requests and pushes to `main`.
Pushing a tag in the form `vX.Y.Z` invokes GoReleaser to publish checksummed
Linux, macOS, and Windows archives.
