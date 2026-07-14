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

func TestApplicationVersionFallsBackToChartVersion(t *testing.T) {
	if got := applicationVersion(chartVersion{Version: "1.2.3"}); got != "1.2.3" {
		t.Fatalf("applicationVersion() = %q, want 1.2.3", got)
	}
}

func TestLatestStableTagIgnoresPrereleases(t *testing.T) {
	got := latestStableTag([]string{"1.0.0", "1.1.0-rc.1", "1.0.1"}, "1.0.0")
	if got != "1.0.1" {
		t.Fatalf("latestStableTag() = %q, want 1.0.1", got)
	}
}

func TestInspectRequiresChartAndVersion(t *testing.T) {
	result := Inspect(t.Context(), Input{})
	if result.SchemaVersion != SchemaVersion || result.Status != StatusError {
		t.Fatalf("Inspect() contract = schema %d, status %q", result.SchemaVersion, result.Status)
	}
	if result.CurrentChartVersion != "" {
		t.Fatalf("Inspect().CurrentChartVersion = %q, want empty", result.CurrentChartVersion)
	}
	if result.Error != "--chart and --current-version are required" {
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

func TestResultJSONEmitsValuesDiffLines(t *testing.T) {
	encoded, err := json.Marshal(Result{
		SchemaVersion: SchemaVersion,
		ValuesDiff:    []string{"--- values.yaml (1.0.0)", "+replicas: 2"},
		Releases: []ReleaseNote{{
			BodyPreview: []string{"# Notes", "", "  code"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion int      `json:"schema_version"`
		ValuesDiff    []string `json:"values_diff"`
		Releases      []struct {
			BodyPreview []string `json:"body_preview"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != SchemaVersion || len(decoded.ValuesDiff) != 2 || decoded.ValuesDiff[1] != "+replicas: 2" || len(decoded.Releases) != 1 || decoded.Releases[0].BodyPreview[2] != "  code" {
		t.Fatalf("JSON contract = %#v", decoded)
	}
}

func TestHelmRepositorySourceRecognizesGitHubPages(t *testing.T) {
	if got := helmRepositorySource("https://grafana.github.io/helm-charts"); got != "https://github.com/grafana/helm-charts" {
		t.Fatalf("helmRepositorySource() = %q", got)
	}
}

func TestLoadReleaseNotesConfigValidatesAndMatchesChart(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "release-notes.yaml")
	contents := "rules:\n  - chart: example\n    provider: github\n    repository: https://github.com/example/project\n    tag_template: release-{version}\n    version: application\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadReleaseNotesConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	rule := config.RuleForChart("oci://registry.example/charts/example")
	if rule.Repository != "https://github.com/example/project" || rule.TagTemplate != "release-{version}" {
		t.Fatalf("RuleForChart() = %#v", rule)
	}
}

func TestLoadReleaseNotesConfigRejectsRepositoryShorthand(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "release-notes.yaml")
	contents := "rules:\n  - chart: example\n    repository: example/project\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseNotesConfig(filename); err == nil || !strings.Contains(err.Error(), "full GitHub URL") {
		t.Fatalf("LoadReleaseNotesConfig() error = %v, want full GitHub URL error", err)
	}
}

func TestLoadBatchManifestRejectsUnknownFields(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "charts.yaml")
	contents := "charts:\n  - chart: example\n    current_version: 1.0.0\n    unsupported: true\n"
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
		{Chart: "example", Repository: server.URL, CurrentVersion: "1.0.0"},
		{Chart: "", CurrentVersion: "1.0.0"},
	}}, ReleaseNotesConfig{}, 2000)

	if result.SchemaVersion != BatchSchemaVersion || result.Status != StatusError {
		t.Fatalf("InspectBatch() contract = %#v", result)
	}
	if len(result.Results) != 2 || result.Results[0].Status != StatusUpdate || result.Results[1].Status != StatusError {
		t.Fatalf("InspectBatch() results = %#v", result.Results)
	}
}

func TestGitHubReleaseNotesTraversesMatchingIntermediateVersions(t *testing.T) {
	originalAPIBase := githubAPIBase
	t.Cleanup(func() { githubAPIBase = originalAPIBase })
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("page") == "1" {
			writer.Header().Set("Link", "<https://example.test/releases?page=2>; rel=\"next\"")
			fmt.Fprint(writer, `[{"tag_name":"app-1.1.0","html_url":"https://example.test/1.1.0","body":"one"},{"tag_name":"app-1.2.0","html_url":"https://example.test/1.2.0","body":"two"}]`)
			return
		}
		fmt.Fprint(writer, `[{"tag_name":"app-1.3.0","html_url":"https://example.test/1.3.0","body":"three"}]`)
	}))
	defer server.Close()
	githubAPIBase = server.URL

	notes, notesErr := githubReleaseNotes(
		t.Context(),
		ReleaseNoteRule{Provider: "github", Repository: "https://github.com/example/project", TagTemplate: "app-{version}"},
		"", "1.0.0", "1.3.0", 2,
	)
	if notesErr != "" {
		t.Fatalf("githubReleaseNotes() error = %q", notesErr)
	}
	if len(notes) != 3 || notes[0].Version != "1.1.0" || notes[2].Version != "1.3.0" {
		t.Fatalf("githubReleaseNotes() = %#v", notes)
	}
	if len(notes[2].BodyPreview) != 1 || notes[2].BodyPreview[0] != "th" || !notes[2].Truncated {
		t.Fatalf("bounded release note = %#v", notes[2])
	}
}

func TestGitHubReleaseNotesFallsBackToPublicReleasePage(t *testing.T) {
	originalAPIBase, originalWebBase := githubAPIBase, githubWebBase
	t.Cleanup(func() {
		githubAPIBase = originalAPIBase
		githubWebBase = originalWebBase
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/example/project/releases" {
			http.Error(writer, "rate limited", http.StatusForbidden)
			return
		}
		fmt.Fprint(writer, `<div class="markdown-body"><p>Release fallback notes</p></div>`)
	}))
	defer server.Close()
	githubAPIBase, githubWebBase = server.URL, server.URL

	notes, notesErr := githubReleaseNotes(
		t.Context(),
		ReleaseNoteRule{Provider: "github", Repository: "https://github.com/example/project", TagTemplate: "v{version}"},
		"", "1.0.0", "1.1.0", 2000,
	)
	if len(notes) != 1 || len(notes[0].BodyPreview) != 1 || notes[0].BodyPreview[0] != "Release fallback notes" {
		t.Fatalf("fallback notes = %#v", notes)
	}
	if !strings.Contains(notesErr, "fallback") {
		t.Fatalf("fallback error = %q", notesErr)
	}
}

func TestGitHubReleaseNotesUsesEnvironmentToken(t *testing.T) {
	originalAPIBase := githubAPIBase
	t.Cleanup(func() { githubAPIBase = originalAPIBase })
	t.Setenv("GITHUB_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		fmt.Fprint(writer, `[{"tag_name":"v1.1.0","html_url":"https://example.test/1.1.0","body":"notes"}]`)
	}))
	defer server.Close()
	githubAPIBase = server.URL

	notes, notesErr := githubReleaseNotes(
		t.Context(), ReleaseNoteRule{Provider: "github", Repository: "https://github.com/example/project"},
		"", "1.0.0", "1.1.0", 2000,
	)
	if notesErr != "" || len(notes) != 1 {
		t.Fatalf("githubReleaseNotes() = %#v, %q", notes, notesErr)
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
		Chart: "example", Repository: server.URL, CurrentVersion: "1.0.0", IncludeDiff: true,
	})

	if result.Status != StatusUpdate || result.TargetChartVersion != "1.1.0" {
		t.Fatalf("Inspect() status/version = %q/%q", result.Status, result.TargetChartVersion)
	}
	if result.CurrentAppVersion != "v1.0.0" || result.TargetAppVersion != "v1.1.0" {
		t.Fatalf("Inspect() application transition = %q -> %q", result.CurrentAppVersion, result.TargetAppVersion)
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
		Chart: "oci://registry.example/charts/example", CurrentVersion: "1.0.0", IncludeDiff: true,
	})

	if result.SourceType != "oci_registry" || result.Status != StatusUpdate {
		t.Fatalf("Inspect() OCI source/status = %q/%q", result.SourceType, result.Status)
	}
	if result.TargetChartVersion != "1.1.0" || result.TargetAppVersion != "1.1.0" {
		t.Fatalf("Inspect() OCI target = %q / %q", result.TargetChartVersion, result.TargetAppVersion)
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

func fixturePullResult(version, appVersion string, archive []byte) *registry.PullResult {
	return &registry.PullResult{Chart: &registry.DescriptorPullSummaryWithMeta{
		DescriptorPullSummary: registry.DescriptorPullSummary{Data: archive},
		Meta:                  &chart.Metadata{Version: version, AppVersion: appVersion},
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
