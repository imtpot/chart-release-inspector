# Chart Release Inspector

[![CI](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml/badge.svg)](https://github.com/imtpot/chart-release-inspector/actions/workflows/ci.yml)

Chart Release Inspector checks Helm repository and OCI chart updates with the
Helm Go SDK. It resolves chart and application versions, compares packaged
`values.yaml`, and collects matching GitHub release notes. It does not depend on
Pulumi configuration or files outside this repository.

## Install

Download a binary from the GitHub Releases page, or build with Go 1.26 or newer:

```sh
go install github.com/imtpot/chart-release-inspector/cmd/chart-release-inspector@latest
```

For a local checkout:

```sh
go build -o bin/chart-release-inspector ./cmd/chart-release-inspector
```

## Usage

```sh
go run ./cmd/chart-release-inspector inspect \
  --chart external-secrets \
  --repository https://charts.external-secrets.io \
  --current-version 2.1.0 \
  --target-version 2.2.0 \
  --values-diff \
  --output json
```

OCI charts do not need `--repository`:

```sh
go run ./cmd/chart-release-inspector inspect \
  --chart oci://ghcr.io/grafana/helm-charts/grafana \
  --current-version 12.4.5 \
  --output json
```

The JSON schema uses tool-neutral names: `source_type` is either
`helm_repository` or `oci_registry`; `current_*` and `target_*` identify the
version transition. It also reports errors, generic GitHub release-note
previews, and an optional default `values.yaml` diff. It has no Pulumi coupling.

Every JSON result includes `schema_version: 4` and one of these statuses:

- `current`: the requested version is already current; process exit code `0`.
- `update_available`: a newer target was found; process exit code `10`.
- `error`: invalid input or an upstream lookup failed; process exit code `20`.

The CLI always emits JSON when `--output json` is selected, including failures.
`values_diff` and release `body_preview` are arrays of lines, which preserve
Markdown and YAML indentation without JSON-escaped multiline strings.
`body_characters` and `truncated` describe the complete upstream body.

The default `--output terminal` uses PTerm. Terminal color is `--color auto` by
default, respects `NO_COLOR`, and can be forced with `--color always` or
disabled with `--color never`.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/chart-release-inspector
```

Tagging a release as `vX.Y.Z` builds checksummed binaries for Linux, macOS, and
Windows through GitHub Actions and GoReleaser.
For GitHub release traversal, set `GITHUB_TOKEN` (preferred) or `GH_TOKEN`.
The token is read only from the environment and sent as a Bearer token to both
the GitHub API and the public-release fallback; it is never written to output.

```sh
export GITHUB_TOKEN="$(gh auth token)"
./bin/chart-release-inspector inspect ...
```

Use `--release-notes-config release-notes.yaml` to pass optional upstream rules.
Rules are tool-neutral and match by chart name; an example is available in
`release-notes.example.yaml`. A rule can select `version: application` (the
default) or `version: chart`, and can provide a GitHub tag template such as
`controller-v{version}`. The inspector lists all matching stable releases in
the resolved version interval. When GitHub's REST API is unavailable or rate
limited, it falls back to the target release's public page and sets
`release_notes_error` to describe that degraded result.