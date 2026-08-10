package inspector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	ociregistry "oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
)

// kserve publishes OCI tags with a "v" prefix (e.g. "v0.19.0"). Helm's own
// registry.Client.Tags() drops those via semver.StrictNewVersion, which is
// exactly what produced "no compatible OCI tags found" for such charts.
// collectOCITags must return the raw tags unfiltered so latestStableTag can
// apply its own "v"-tolerant parsing.
func TestCollectOCITagsPreservesVPrefixedTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/charts/example/tags/list" {
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		if err := json.NewEncoder(writer).Encode(map[string][]string{
			"tags": {"v0.17.0-rc0", "v0.19.0", "v0.20.0"},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	parsedRef, err := ociregistry.ParseReference(strings.TrimPrefix(server.URL, "http://") + "/charts/example")
	if err != nil {
		t.Fatal(err)
	}
	repository, err := remote.NewRepository(parsedRef.String())
	if err != nil {
		t.Fatal(err)
	}
	repository.PlainHTTP = true

	tags, err := collectOCITags(context.Background(), repository)
	if err != nil {
		t.Fatalf("collectOCITags() error: %v", err)
	}

	want := []string{"v0.17.0-rc0", "v0.19.0", "v0.20.0"}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("collectOCITags() = %v, want %v", tags, want)
	}
	if got := latestStableTag(tags); got != "v0.20.0" {
		t.Fatalf("latestStableTag(collectOCITags()) = %q, want v0.20.0", got)
	}
}
