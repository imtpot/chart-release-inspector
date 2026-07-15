package inspector

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
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
