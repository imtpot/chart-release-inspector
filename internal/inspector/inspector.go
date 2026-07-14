package inspector

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/pmezard/go-difflib/difflib"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/repo/v1"
)

// Input describes a chart update to inspect without binding to an IaC tool.
type Input struct {
	Chart            string
	Repository       string
	Version          string
	TargetVersion    string
	IncludeDiff      bool
	ReleaseNoteLimit int
	ReleaseNoteRule  ReleaseNoteRule
	SkipReleaseNotes bool
}

const (
	StatusCurrent = "current"
	StatusUpdate  = "update_available"
	StatusError   = "error"
)

// ReleaseNote is a bounded upstream release-note payload for automation.
type ReleaseNote struct {
	Version        string   `json:"version"`
	URL            string   `json:"url"`
	BodyPreview    []string `json:"body_preview"`
	BodyCharacters int      `json:"body_characters"`
	Truncated      bool     `json:"truncated"`
}

// Result is the machine-readable contract emitted by the standalone CLI.
type Result struct {
	Chart             string        `json:"chart,omitempty"`
	Status            string        `json:"status"`
	SourceType        string        `json:"source_type"`
	ChartVersion      string        `json:"chart_version"`
	TargetChartVersion string        `json:"target_chart_version,omitempty"`
	AppVersion        string        `json:"app_version,omitempty"`
	TargetAppVersion  string        `json:"target_app_version,omitempty"`
	Error             string        `json:"error,omitempty"`
	ValuesDiff        []string      `json:"values_diff,omitempty"`
	ValuesDiffChanged *bool         `json:"values_diff_changed,omitempty"`
	ValuesDiffError   string        `json:"values_diff_error,omitempty"`
	ReleaseNotesError string        `json:"release_notes_error,omitempty"`
	Releases          []ReleaseNote `json:"releases"`
}

type chartVersion struct {
	Version    string
	AppVersion string
	Source     string
	URLs       []string
	Values     []byte
}

type ociRegistryClient interface {
	Tags(ref string) ([]string, error)
	Pull(ref string, options ...registry.PullOption) (*registry.PullResult, error)
}

var newOCIRegistryClient = func() (ociRegistryClient, error) {
	return registry.NewClient()
}

// Inspect resolves a standard Helm repository or OCI chart using the Helm SDK.
func Inspect(ctx context.Context, input Input) Result {
	result := Result{
		Chart:         input.Chart,
		Status:        StatusError,
		ChartVersion:  input.Version,
		Releases:      []ReleaseNote{},
	}
	if input.Chart == "" || input.Version == "" {
		result.Error = "--chart and --version are required"
		return result
	}

	var current, target chartVersion
	var err error
	if strings.HasPrefix(input.Chart, "oci://") {
		result.SourceType = "oci_registry"
		current, target, err = inspectOCI(input)
	} else {
		result.SourceType = "helm_repository"
		if input.Repository == "" {
			result.Error = "--repository is required for a non-OCI chart"
			return result
		}
		current, target, err = inspectRepository(input)
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.TargetChartVersion = target.Version
	result.AppVersion = applicationVersion(current)
	result.TargetAppVersion = applicationVersion(target)
	currentReleaseVersion := result.AppVersion
	targetReleaseVersion := result.TargetAppVersion
	if input.ReleaseNoteRule.Version == "chart" {
		currentReleaseVersion = result.ChartVersion
		targetReleaseVersion = result.TargetChartVersion
	}
	result.Releases, result.ReleaseNotesError = githubReleaseNotes(
		ctx,
		input.ReleaseNoteRule,
		firstNonEmpty(target.Source, helmRepositorySource(input.Repository)),
		currentReleaseVersion,
		targetReleaseVersion,
		input.ReleaseNoteLimit,
		input.SkipReleaseNotes,
	)
	if input.IncludeDiff {
		changed := false
		result.ValuesDiffChanged = &changed
		if sameVersion(input.Version, target.Version) {
			result.Status = StatusCurrent
			return result
		}
		if len(current.Values) == 0 || len(target.Values) == 0 {
			result.ValuesDiffError = "could not download chart values"
			return result
		}
		result.ValuesDiff = diffLines(unifiedDiff(current.Values, target.Values, input.Version, target.Version))
		changed = len(result.ValuesDiff) > 0
		result.ValuesDiffChanged = &changed
	}
	if sameVersion(input.Version, target.Version) {
		result.Status = StatusCurrent
	} else {
		result.Status = StatusUpdate
	}
	return result
}

func diffLines(diff string) []string {
	diff = strings.TrimSuffix(diff, "\n")
	if diff == "" {
		return nil
	}
	return strings.Split(diff, "\n")
}

func inspectRepository(input Input) (chartVersion, chartVersion, error) {
	settings := cli.New()
	chartRepo, err := repo.NewChartRepository(
		&repo.Entry{Name: "chart-release-inspector", URL: input.Repository},
		getter.All(settings),
	)
	if err != nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("create chart repository: %w", err)
	}
	indexPath, err := chartRepo.DownloadIndexFile()
	if err != nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("download Helm index: %w", err)
	}
	defer os.Remove(indexPath)
	index, err := repo.LoadIndexFile(indexPath)
	if err != nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("load Helm index: %w", err)
	}
	entries := index.Entries[input.Chart]
	if len(entries) == 0 {
		return chartVersion{}, chartVersion{}, fmt.Errorf("chart %q not found in repository", input.Chart)
	}
	currentEntry := findEntry(entries, input.Version)
	if currentEntry == nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("configured chart version %s not found", input.Version)
	}
	targetEntry := findEntry(entries, input.TargetVersion)
	if input.TargetVersion == "" {
		targetEntry = latestStable(entries, input.Version)
	}
	if targetEntry == nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("target chart version %s not found", input.TargetVersion)
	}
	if olderThan(targetEntry.Version, input.Version) {
		return chartVersion{}, chartVersion{}, fmt.Errorf("target chart version %s is older than configured version %s", targetEntry.Version, input.Version)
	}
	current := fromRepositoryEntry(currentEntry)
	target := fromRepositoryEntry(targetEntry)
	if input.IncludeDiff {
		current.Values, _ = downloadValues(input.Repository, current.URLs)
		target.Values, _ = downloadValues(input.Repository, target.URLs)
	}
	return current, target, nil
}

func inspectOCI(input Input) (chartVersion, chartVersion, error) {
	client, err := newOCIRegistryClient()
	if err != nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("create OCI client: %w", err)
	}
	reference := strings.TrimPrefix(input.Chart, "oci://")
	tags, err := client.Tags(reference)
	if err != nil {
		return chartVersion{}, chartVersion{}, fmt.Errorf("list OCI tags: %w", err)
	}
	targetVersion := input.TargetVersion
	if targetVersion == "" {
		targetVersion = latestStableTag(tags, input.Version)
	}
	if targetVersion == "" {
		return chartVersion{}, chartVersion{}, errors.New("no compatible OCI tags found")
	}
	if olderThan(targetVersion, input.Version) {
		return chartVersion{}, chartVersion{}, fmt.Errorf("target chart version %s is older than configured version %s", targetVersion, input.Version)
	}
	current, err := pullOCI(client, input.Chart, input.Version)
	if err != nil {
		return chartVersion{}, chartVersion{}, err
	}
	target, err := pullOCI(client, input.Chart, targetVersion)
	if err != nil {
		return chartVersion{}, chartVersion{}, err
	}
	return current, target, nil
}

func pullOCI(client ociRegistryClient, chart, version string) (chartVersion, error) {
	pulled, err := client.Pull(fmt.Sprintf("%s:%s", chart, version))
	if err != nil {
		return chartVersion{}, fmt.Errorf("pull OCI chart %s:%s: %w", chart, version, err)
	}
	metadata := pulled.Chart.Meta
	values, _ := chartArchiveFile(pulled.Chart.Data, "values.yaml")
	return chartVersion{
		Version: metadata.Version, AppVersion: metadata.AppVersion,
		Source: firstNonEmpty(firstString(metadata.Sources), metadata.Home), Values: values,
	}, nil
}

func fromRepositoryEntry(entry *repo.ChartVersion) chartVersion {
	return chartVersion{
		Version: entry.Version, AppVersion: entry.AppVersion,
		Source: firstNonEmpty(firstString(entry.Sources), entry.Home), URLs: entry.URLs,
	}
}

func findEntry(entries repo.ChartVersions, version string) *repo.ChartVersion {
	if version == "" {
		return nil
	}
	for _, entry := range entries {
		if sameVersion(entry.Version, version) {
			return entry
		}
	}
	return nil
}

func latestStable(entries repo.ChartVersions, current string) *repo.ChartVersion {
	var selected *repo.ChartVersion
	for _, entry := range entries {
		parsed, err := parseVersion(entry.Version)
		if err != nil || parsed.Prerelease() != "" {
			continue
		}
		if selected == nil || olderThan(selected.Version, entry.Version) {
			selected = entry
		}
	}
	return selected
}

func latestStableTag(tags []string, current string) string {
	sort.Slice(tags, func(left, right int) bool { return olderThan(tags[left], tags[right]) })
	for index := len(tags) - 1; index >= 0; index-- {
		version, err := parseVersion(tags[index])
		if err == nil && version.Prerelease() == "" {
			return tags[index]
		}
	}
	return ""
}

func applicationVersion(chart chartVersion) string {
	if chart.AppVersion != "" {
		return chart.AppVersion
	}
	return chart.Version
}

func parseVersion(version string) (*semver.Version, error) {
	return semver.NewVersion(strings.TrimPrefix(version, "v"))
}

func sameVersion(left, right string) bool {
	leftVersion, leftErr := parseVersion(left)
	rightVersion, rightErr := parseVersion(right)
	return leftErr == nil && rightErr == nil && leftVersion.Equal(rightVersion)
}

func olderThan(left, right string) bool {
	leftVersion, leftErr := parseVersion(left)
	rightVersion, rightErr := parseVersion(right)
	return leftErr == nil && rightErr == nil && leftVersion.LessThan(rightVersion)
}

func downloadValues(repository string, urls []string) ([]byte, error) {
	if len(urls) == 0 {
		return nil, errors.New("chart archive URL is missing")
	}
	archiveURL, err := url.Parse(urls[0])
	if err != nil {
		return nil, err
	}
	if !archiveURL.IsAbs() {
		base, parseErr := url.Parse(strings.TrimSuffix(repository, "/") + "/")
		if parseErr != nil {
			return nil, parseErr
		}
		archiveURL = base.ResolveReference(archiveURL)
	}
	response, err := http.Get(archiveURL.String())
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download chart archive: HTTP %d", response.StatusCode)
	}
	archive, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return chartArchiveFile(archive, "values.yaml")
}

func chartArchiveFile(archive []byte, filename string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, readErr := tarReader.Next()
		if errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("%s not found in chart archive", filename)
		}
		if readErr != nil {
			return nil, readErr
		}
		if path.Base(header.Name) == filename {
			return io.ReadAll(tarReader)
		}
	}
}

func unifiedDiff(current, target []byte, currentVersion, targetVersion string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(current)),
		B:        difflib.SplitLines(string(target)),
		FromFile: fmt.Sprintf("values.yaml (%s)", currentVersion),
		ToFile:   fmt.Sprintf("values.yaml (%s)", targetVersion),
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return diff
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncateRunes(value string, limit int) (string, bool) {
	characters := []rune(value)
	if limit == 0 || len(characters) <= limit {
		return value, false
	}
	return string(characters[:limit]), true
}

func helmRepositorySource(repository string) string {
	parsed, err := url.Parse(repository)
	if err != nil {
		return ""
	}
	hostParts := strings.Split(parsed.Host, ".")
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(hostParts) >= 3 && strings.Join(hostParts[1:], ".") == "github.io" && len(pathParts) > 0 {
		return fmt.Sprintf("https://github.com/%s/%s", hostParts[0], pathParts[0])
	}
	return ""
}
