# AGENTS.md

Guidance for coding agents working in this repository.

## Project

`chart-release-inspector` is a standalone Go CLI for inspecting Helm repository
and OCI chart releases. It must remain independent of deployment tools and
project-specific configuration formats.

- The CLI entry point is `cmd/chart-release-inspector/main.go`.
- Chart inspection and JSON contracts belong in `internal/inspector/`.
- Terminal rendering belongs in `internal/render/` and must not affect JSON
  output.
- Keep batch manifests source-neutral. Callers should adapt their own
  configuration into `BatchManifest` rather than adding project dependencies.

## CLI Contract

- Preserve stable JSON field names and `schema_version` values. Bump a schema
  version only for a deliberate incompatible contract change.
- `inspect --output json` and `batch` write JSON to stdout; diagnostics go to
  stderr.
- Preserve semantic exit codes: `0` for success, `10` for updates available
  (only when `--fail-on-update` is set), and `20` for errors. Batch must
  continue inspecting remaining entries after an individual chart error.
- Do not print credentials, tokens, or complete release-note bodies beyond the
  configured limit.

## Development

Use Go 1.26 or newer. Before finishing a change, run the relevant checks:

```sh
go test ./...
go vet ./...
go build ./cmd/chart-release-inspector
```

Add focused unit tests for parser, inspection, or CLI contract changes. Tests
must not require live Helm registries, GitHub credentials, or network access.

## Releases

GitHub Actions releases tags matching `vX.Y.Z` through GoReleaser. Do not
force-update release tags after publishing has started. Validate the release
workflow and generated artifacts before updating downstream version pins.