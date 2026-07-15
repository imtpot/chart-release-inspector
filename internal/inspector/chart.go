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

	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/repo/v1"

	"github.com/Masterminds/semver/v3"
)

type ociRegistryClient interface {
	Tags(ref string) ([]string, error)
	Pull(ref string, options ...registry.PullOption) (*registry.PullResult, error)
}

var newOCIRegistryClient = func() (ociRegistryClient, error) {
	return registry.NewClient()
}

func inspectRepository(ctx context.Context, input Input) (chartVersion, chartVersion, error) {
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
		targetEntry = latestStable(entries)
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
		current.Values, _ = downloadValues(ctx, input.Repository, current.URLs)
		target.Values, _ = downloadValues(ctx, input.Repository, target.URLs)
	}
	return current, target, nil
}

func inspectOCI(ctx context.Context, input Input) (chartVersion, chartVersion, error) {
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
		targetVersion = latestStableTag(tags)
	}
	if targetVersion == "" {
		return chartVersion{}, chartVersion{}, errors.New("no compatible OCI tags found")
	}
	if olderThan(targetVersion, input.Version) {
		return chartVersion{}, chartVersion{}, fmt.Errorf("target chart version %s is older than configured version %s", targetVersion, input.Version)
	}

	current, err := pullOCI(client, input.Chart, input.Version, input.IncludeDiff)
	if err != nil {
		return chartVersion{}, chartVersion{}, err
	}
	target, err := pullOCI(client, input.Chart, targetVersion, input.IncludeDiff)
	if err != nil {
		return chartVersion{}, chartVersion{}, err
	}
	return current, target, nil
}

func pullOCI(client ociRegistryClient, chart, version string, includeDiff bool) (chartVersion, error) {
	pulled, err := client.Pull(fmt.Sprintf("%s:%s", chart, version))
	if err != nil {
		return chartVersion{}, fmt.Errorf("pull OCI chart %s:%s: %w", chart, version, err)
	}
	metadata := pulled.Chart.Meta
	var values []byte
	if includeDiff {
		values, _ = chartArchiveFile(pulled.Chart.Data, "values.yaml")
	}
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

func latestStable(entries repo.ChartVersions) *repo.ChartVersion {
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

func latestStableTag(tags []string) string {
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

func downloadValues(ctx context.Context, repository string, urls []string) ([]byte, error) {
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

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
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
