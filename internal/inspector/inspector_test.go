package inspector

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/registry"
)

func TestLatestStableTagIgnoresPrereleases(t *testing.T) {
	got := latestStableTag([]string{"1.0.0", "1.1.0-rc.1", "1.0.1"})
	if got != "1.0.1" {
		t.Fatalf("latestStableTag() = %q, want 1.0.1", got)
	}
}

func TestInspectRequiresChartAndVersion(t *testing.T) {
	result := Inspect(t.Context(), Input{})
	if result.Status != StatusError {
		t.Fatalf("Inspect() status = %q", result.Status)
	}
	if result.ChartVersion != "" {
		t.Fatalf("Inspect().ChartVersion = %q, want empty", result.ChartVersion)
	}
	if result.Error != "--chart and --version are required" {
		t.Fatalf("Inspect().Error = %q", result.Error)
	}
}

func TestTruncateRunesPreservesUnicodeBoundaries(t *testing.T) {
	excerpt, truncated := truncateRunes("one two", 3)
	if excerpt != "one" || !truncated {
		t.Fatalf("truncateRunes() = (%q, %t), want (one, true)", excerpt, truncated)
	}
}

func TestPreviewLinesNormalizesLineEndingsAndPreservesIndentation(t *testing.T) {
	lines := previewLines("# Notes\r\n\r\n<!-- generated metadata -->\r\n\r\n```yaml\r\n  <!-- preserved code -->\r\n  key: value\r\n```\r\n")
	if got := strings.Join(lines, "\n"); got != "# Notes\n\n```yaml\n  <!-- preserved code -->\n  key: value\n```" {
		t.Fatalf("previewLines() = %q", got)
	}
}

func TestResultJSONEmitsChangelogEntries(t *testing.T) {
	encoded, err := json.Marshal(Result{
		ValuesDiff: []string{"--- values.yaml (1.0.0)", "+replicas: 2"},
		Changelog: []ChangelogEntry{{
			BodyPreview: []string{"# Notes", "", "  code"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ValuesDiff []string `json:"values_diff"`
		Changelog  []struct {
			BodyPreview []string `json:"body_preview"`
		} `json:"changelog"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.ValuesDiff) != 2 || decoded.ValuesDiff[1] != "+replicas: 2" || len(decoded.Changelog) != 1 || decoded.Changelog[0].BodyPreview[2] != "  code" {
		t.Fatalf("JSON contract = %#v", decoded)
	}
}

func TestHelmRepositorySourceRecognizesGitHubPages(t *testing.T) {
	if got := helmRepositorySource("https://grafana.github.io/helm-charts"); got != "https://github.com/grafana/helm-charts" {
		t.Fatalf("helmRepositorySource() = %q", got)
	}
}

func TestLoadBatchManifestRejectsInvalidAppRepository(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "charts.yaml")
	contents := "charts:\n  - chart: example\n    version: 1.0.0\n    app_repository: example/project\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBatchManifest(filename); err == nil || !strings.Contains(err.Error(), "full GitHub URL") {
		t.Fatalf("LoadBatchManifest() error = %v, want full GitHub URL error", err)
	}
}

func TestLoadBatchManifestRejectsUnknownFields(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "charts.yaml")
	contents := "charts:\n  - chart: example\n    version: 1.0.0\n    unsupported: true\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBatchManifest(filename); err == nil || !strings.Contains(err.Error(), "field unsupported") {
		t.Fatalf("LoadBatchManifest() error = %v, want unknown field error", err)
	}
}

func TestInspectBatchAggregatesResultsInManifestOrder(t *testing.T) {
	archives := map[string][]byte{
		"example-1.0.0.tgz": chartFixture(t, "example", "1.0.0", "v1.0.0", "enabled: false\n"),
		"example-1.1.0.tgz": chartFixture(t, "example", "1.1.0", "v1.1.0", "enabled: true\n"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/index.yaml" {
			fmt.Fprint(writer, "apiVersion: v1\nentries:\n  example:\n    - name: example\n      version: 1.1.0\n      appVersion: v1.1.0\n      urls: [example-1.1.0.tgz]\n    - name: example\n      version: 1.0.0\n      appVersion: v1.0.0\n      urls: [example-1.0.0.tgz]\n")
			return
		}
		archive, found := archives[request.URL.Path[1:]]
		if !found {
			http.NotFound(writer, request)
			return
		}
		writer.Write(archive)
	}))
	defer server.Close()

	result := InspectBatch(t.Context(), BatchManifest{Charts: []BatchChart{
		{Chart: "example", Repository: server.URL, Version: "1.0.0"},
		{Chart: "", Version: "1.0.0"},
	}}, 2000, false, true)

	if result.SchemaVersion != BatchSchemaVersion || result.Status != StatusError {
		t.Fatalf("InspectBatch() contract = %#v", result)
	}
	if len(result.Results) != 2 || result.Results[0].Status != StatusUpdate || result.Results[1].Status != StatusError {
		t.Fatalf("InspectBatch() results = %#v", result.Results)
	}
}

type fakeOCIRegistry struct {
	tags   []string
	charts map[string]*registry.PullResult
}

func (client fakeOCIRegistry) Tags(string) ([]string, error) {
	return client.tags, nil
}

func (client fakeOCIRegistry) Pull(reference string, _ ...registry.PullOption) (*registry.PullResult, error) {
	pulled, found := client.charts[reference]
	if !found {
		return nil, errors.New("fixture chart not found")
	}
	return pulled, nil
}

func fixturePullResult(version, appVersion string, archive []byte, sources ...string) *registry.PullResult {
	return &registry.PullResult{Chart: &registry.DescriptorPullSummaryWithMeta{
		DescriptorPullSummary: registry.DescriptorPullSummary{Data: archive},
		Meta:                  &chart.Metadata{Version: version, AppVersion: appVersion, Sources: sources},
	}}
}

func chartFixture(t *testing.T, name, version, appVersion, values string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	files := map[string]string{
		name + "/Chart.yaml":  fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\nappVersion: %s\n", name, version, appVersion),
		name + "/values.yaml": values,
	}
	for filename, content := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: filename, Mode: 0o600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestInspectReturnsAppChangelog(t *testing.T) {
	originalFactory := newOCIRegistryClient
	originalAPIBase := githubAPIBase
	t.Cleanup(func() {
		newOCIRegistryClient = originalFactory
		githubAPIBase = originalAPIBase
	})

	newOCIRegistryClient = func() (ociRegistryClient, error) {
		return fakeOCIRegistry{
			tags: []string{"10.5.10", "10.5.15"},
			charts: map[string]*registry.PullResult{
				"oci://registry.example/charts/mycharts:10.5.10": fixturePullResult("10.5.10", "v2.0.0", chartFixture(t, "mycharts", "10.5.10", "v2.0.0", ""), "https://github.com/org/myapp"),
				"oci://registry.example/charts/mycharts:10.5.15": fixturePullResult("10.5.15", "v2.1.0", chartFixture(t, "mycharts", "10.5.15", "v2.1.0", ""), "https://github.com/org/myapp"),
			},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/org/myapp/releases" {
			fmt.Fprint(writer, `[{"tag_name":"v2.1.0","html_url":"https://github.com/org/myapp/releases/tag/v2.1.0","body":"app release notes"}]`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	githubAPIBase = server.URL

	result := Inspect(t.Context(), Input{
		Chart:            "oci://registry.example/charts/mycharts",
		Version:          "10.5.10",
		IncludeChangelog: true,
		AppRepository:    "https://github.com/org/myapp",
	})

	if result.Status != StatusUpdate {
		t.Fatalf("Inspect() status = %q, want update_available", result.Status)
	}
	if len(result.Changelog) != 1 || result.Changelog[0].BodyPreview[0] != "app release notes" {
		t.Fatalf("expected app changelog, got %#v", result.Changelog)
	}
	if result.ChangelogError != "" {
		t.Fatalf("unexpected changelog error: %q", result.ChangelogError)
	}
}

func TestInspectSkipsChangelogWhenDisabled(t *testing.T) {
	originalFactory := newOCIRegistryClient
	originalAPIBase := githubAPIBase
	t.Cleanup(func() {
		newOCIRegistryClient = originalFactory
		githubAPIBase = originalAPIBase
	})

	newOCIRegistryClient = func() (ociRegistryClient, error) {
		return fakeOCIRegistry{
			tags: []string{"10.5.10", "10.5.15"},
			charts: map[string]*registry.PullResult{
				"oci://registry.example/charts/mycharts:10.5.10": fixturePullResult("10.5.10", "v2.0.0", chartFixture(t, "mycharts", "10.5.10", "v2.0.0", ""), "https://github.com/org/myapp"),
				"oci://registry.example/charts/mycharts:10.5.15": fixturePullResult("10.5.15", "v2.1.0", chartFixture(t, "mycharts", "10.5.15", "v2.1.0", ""), "https://github.com/org/myapp"),
			},
		}, nil
	}

	// Any hit to the GitHub API would fail the test; the disabled flag must skip it.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "must not be called", http.StatusInternalServerError)
	}))
	defer server.Close()
	githubAPIBase = server.URL

	result := Inspect(t.Context(), Input{
		Chart:            "oci://registry.example/charts/mycharts",
		Version:          "10.5.10",
		IncludeChangelog: false,
		AppRepository:    "https://github.com/org/myapp",
	})

	if result.Status != StatusUpdate {
		t.Fatalf("Inspect() status = %q, want update_available", result.Status)
	}
	if len(result.Changelog) != 0 || result.ChangelogError != "" {
		t.Fatalf("changelog should be skipped, got entries=%d error=%q", len(result.Changelog), result.ChangelogError)
	}
}

func TestInspectChangelogFallsBackToHTMLWhenAPIFails(t *testing.T) {
	originalFactory := newOCIRegistryClient
	originalAPIBase := githubAPIBase
	originalWebBase := githubWebBase
	t.Cleanup(func() {
		newOCIRegistryClient = originalFactory
		githubAPIBase = originalAPIBase
		githubWebBase = originalWebBase
	})

	newOCIRegistryClient = func() (ociRegistryClient, error) {
		return fakeOCIRegistry{
			tags: []string{"10.5.10", "10.5.15"},
			charts: map[string]*registry.PullResult{
				"oci://registry.example/charts/mycharts:10.5.10": fixturePullResult("10.5.10", "v2.0.0", chartFixture(t, "mycharts", "10.5.10", "v2.0.0", ""), "https://github.com/org/myapp"),
				"oci://registry.example/charts/mycharts:10.5.15": fixturePullResult("10.5.15", "v2.1.0", chartFixture(t, "mycharts", "10.5.15", "v2.1.0", ""), "https://github.com/org/myapp"),
			},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// API path 404s; the HTML release page serves the rendered body.
		if strings.Contains(request.URL.Path, "/releases/tag/v2.1.0") {
			fmt.Fprint(writer, `<html><body><div class="markdown-body"><p>app release notes</p></div></body></html>`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	githubAPIBase = server.URL
	githubWebBase = server.URL

	result := Inspect(t.Context(), Input{
		Chart:            "oci://registry.example/charts/mycharts",
		Version:          "10.5.10",
		IncludeChangelog: true,
		AppRepository:    "https://github.com/org/myapp",
	})

	if len(result.Changelog) != 1 || result.Changelog[0].BodyPreview[0] != "app release notes" {
		t.Fatalf("expected HTML-fallback changelog, got %#v", result.Changelog)
	}
	if result.ChangelogError == "" || !strings.Contains(result.ChangelogError, "fallback") {
		t.Fatalf("expected fallback telemetry, got %q", result.ChangelogError)
	}
}

func TestPullOCISkipsValuesExtractionWhenDiffDisabled(t *testing.T) {
	client := fakeOCIRegistry{
		charts: map[string]*registry.PullResult{
			"oci://registry.example/charts/example:1.0.0": fixturePullResult("1.0.0", "v1.0.0", chartFixture(t, "example", "1.0.0", "v1.0.0", "enabled: false\n")),
		},
	}

	withDiff, err := pullOCI(client, "oci://registry.example/charts/example", "1.0.0", true)
	if err != nil {
		t.Fatalf("pullOCI(includeDiff=true) error: %v", err)
	}
	if string(withDiff.Values) != "enabled: false\n" {
		t.Fatalf("pullOCI(includeDiff=true) values = %q, want values.yaml content", withDiff.Values)
	}

	withoutDiff, err := pullOCI(client, "oci://registry.example/charts/example", "1.0.0", false)
	if err != nil {
		t.Fatalf("pullOCI(includeDiff=false) error: %v", err)
	}
	if withoutDiff.Values != nil {
		t.Fatalf("pullOCI(includeDiff=false) values = %q, want nil", withoutDiff.Values)
	}
}

func TestInspectRepositoryUsesHelmIndexAndChartFixtures(t *testing.T) {
	archives := map[string][]byte{
		"example-1.0.0.tgz": chartFixture(t, "example", "1.0.0", "v1.0.0", "enabled: false\n"),
		"example-1.1.0.tgz": chartFixture(t, "example", "1.1.0", "v1.1.0", "enabled: true\n"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/index.yaml" {
			fmt.Fprint(writer, "apiVersion: v1\nentries:\n  example:\n    - name: example\n      version: 1.1.0\n      appVersion: v1.1.0\n      urls: [example-1.1.0.tgz]\n    - name: example\n      version: 1.0.0\n      appVersion: v1.0.0\n      urls: [example-1.0.0.tgz]\n")
			return
		}
		archive, found := archives[request.URL.Path[1:]]
		if !found {
			http.NotFound(writer, request)
			return
		}
		writer.Write(archive)
	}))
	defer server.Close()

	result := Inspect(t.Context(), Input{
		Chart: "example", Repository: server.URL, Version: "1.0.0", IncludeDiff: true,
	})

	if result.Status != StatusUpdate || result.TargetChartVersion != "1.1.0" {
		t.Fatalf("Inspect() status/version = %q/%q", result.Status, result.TargetChartVersion)
	}
	if result.AppVersion != "v1.0.0" || result.TargetAppVersion != "v1.1.0" {
		t.Fatalf("Inspect() application transition = %q -> %q", result.AppVersion, result.TargetAppVersion)
	}
	if result.ValuesDiffChanged == nil || !*result.ValuesDiffChanged || len(result.ValuesDiff) == 0 {
		t.Fatalf("Inspect() did not produce a values diff: %#v", result)
	}
}

func TestInspectOCIFixtureUsesHelmRegistryContract(t *testing.T) {
	originalFactory := newOCIRegistryClient
	t.Cleanup(func() { newOCIRegistryClient = originalFactory })
	archive := chartFixture(t, "example", "1.1.0", "", "enabled: true\n")
	newOCIRegistryClient = func() (ociRegistryClient, error) {
		return fakeOCIRegistry{
			tags: []string{"1.0.0", "1.1.0", "1.2.0-rc.1"},
			charts: map[string]*registry.PullResult{
				"oci://registry.example/charts/example:1.0.0": fixturePullResult("1.0.0", "v1.0.0", chartFixture(t, "example", "1.0.0", "v1.0.0", "enabled: false\n")),
				"oci://registry.example/charts/example:1.1.0": fixturePullResult("1.1.0", "", archive),
			},
		}, nil
	}

	result := Inspect(t.Context(), Input{
		Chart: "oci://registry.example/charts/example", Version: "1.0.0", IncludeDiff: true,
	})

	if result.SourceType != "oci_registry" || result.Status != StatusUpdate {
		t.Fatalf("Inspect() OCI source/status = %q/%q", result.SourceType, result.Status)
	}
	if result.TargetChartVersion != "1.1.0" || result.TargetAppVersion != "1.1.0" {
		t.Fatalf("Inspect() OCI target = %q / %q", result.TargetChartVersion, result.TargetAppVersion)
	}
}

func TestInspectReturnsMultipleAppReleasesWithVariousTagFormats(t *testing.T) {
	originalFactory := newOCIRegistryClient
	originalAPIBase := githubAPIBase
	t.Cleanup(func() {
		newOCIRegistryClient = originalFactory
		githubAPIBase = originalAPIBase
	})

	newOCIRegistryClient = func() (ociRegistryClient, error) {
		return fakeOCIRegistry{
			tags: []string{"10.5.10", "10.5.15"},
			charts: map[string]*registry.PullResult{
				"oci://registry.example/charts/mycharts:10.5.10": fixturePullResult("10.5.10", "v2.0.0", chartFixture(t, "mycharts", "10.5.10", "v2.0.0", ""), "https://github.com/org/myapp"),
				"oci://registry.example/charts/mycharts:10.5.15": fixturePullResult("10.5.15", "v2.0.3", chartFixture(t, "mycharts", "10.5.15", "v2.0.3", ""), "https://github.com/org/myapp"),
			},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/org/myapp/releases" {
			// Return a mix of tag formats, prereleases, and versions inside/outside the target range (2.0.0 < v <= 2.0.3)
			fmt.Fprint(writer, `[
				{"tag_name":"2.1.0","html_url":"https://github.com/org/myapp/releases/tag/2.1.0","body":"too new"},
				{"tag_name":"v2.0.3","html_url":"https://github.com/org/myapp/releases/tag/v2.0.3","body":"v2.0.3 notes"},
				{"tag_name":"2.0.2","html_url":"https://github.com/org/myapp/releases/tag/2.0.2","body":"2.0.2 notes"},
				{"tag_name":"app-v2.0.1","html_url":"https://github.com/org/myapp/releases/tag/app-v2.0.1","body":"app-v2.0.1 notes"},
				{"tag_name":"2.0.1-rc.1","html_url":"https://github.com/org/myapp/releases/tag/2.0.1-rc.1","body":"prerelease skipped"},
				{"tag_name":"v2.0.0","html_url":"https://github.com/org/myapp/releases/tag/v2.0.0","body":"current version skipped"}
			]`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	githubAPIBase = server.URL

	result := Inspect(t.Context(), Input{
		Chart:            "oci://registry.example/charts/mycharts",
		Version:          "10.5.10",
		IncludeChangelog: true,
		AppRepository:    "https://github.com/org/myapp",
	})

	if len(result.Changelog) != 3 {
		t.Fatalf("expected 3 changelog entries, got %d: %#v", len(result.Changelog), result.Changelog)
	}

	// Should be sorted in ascending order of version: 2.0.1 -> 2.0.2 -> 2.0.3
	if result.Changelog[0].Version != "2.0.1" || result.Changelog[0].BodyPreview[0] != "app-v2.0.1 notes" {
		t.Fatalf("expected 2.0.1 at index 0, got version %q notes %q", result.Changelog[0].Version, result.Changelog[0].BodyPreview[0])
	}
	if result.Changelog[1].Version != "2.0.2" || result.Changelog[1].BodyPreview[0] != "2.0.2 notes" {
		t.Fatalf("expected 2.0.2 at index 1, got version %q notes %q", result.Changelog[1].Version, result.Changelog[1].BodyPreview[0])
	}
	if result.Changelog[2].Version != "2.0.3" || result.Changelog[2].BodyPreview[0] != "v2.0.3 notes" {
		t.Fatalf("expected 2.0.3 at index 2, got version %q notes %q", result.Changelog[2].Version, result.Changelog[2].BodyPreview[0])
	}
}
