package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/imtpot/chart-release-inspector/internal/inspector"
)

func TestHumanPlainOutputIncludesReleaseNotes(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.Result{
		SourceType:          "helm_repository",
		CurrentChartVersion: "1.0.0",
		TargetChartVersion:  "1.1.0",
		CurrentAppVersion:   "v1.0.0",
		TargetAppVersion:    "v1.1.0",
		Releases: []inspector.ReleaseNote{{
			Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
			BodyPreview: []string{"# Breaking change"}, BodyCharacters: 42, Truncated: true,
		}},
	}, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Source: Helm repository",
		"Chart: 1.0.0 -> 1.1.0",
		"Application: v1.0.0 -> v1.1.0",
		"Release notes",
		"1.1.0: https://example.test/releases/1.1.0",
		"Breaking change",
		"[Preview truncated; 42 characters total]",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Human() output is missing %q:\n%s", expected, output.String())
		}
	}
}

func TestHumanOutputIncludesValuesDiffWithoutReleaseNotes(t *testing.T) {
	changed := true
	var output bytes.Buffer
	err := Human(&output, inspector.Result{
		SourceType:          "helm_repository",
		CurrentChartVersion: "1.0.0",
		TargetChartVersion:  "1.1.0",
		CurrentAppVersion:   "v1.0.0",
		TargetAppVersion:    "v1.1.0",
		ValuesDiffChanged:   &changed,
		ValuesDiff:          []string{"--- values.yaml (1.0.0)", "+++ values.yaml (1.1.0)", "@@ -1 +1 @@", "-replicas: 1", "+replicas: 2"},
	}, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Values diff",
		"--- values.yaml (1.0.0)",
		"+replicas: 2",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Human() output is missing %q:\n%s", expected, output.String())
		}
	}
}

func TestRenderMarkdownPreservesFencedCodeWithoutPadding(t *testing.T) {
	markdown := "# Breaking change\n\n```yaml\nselector:\n  mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip\n```\n\n<!-- Generated metadata -->\n\nWhat's Changed"
	got := renderMarkdown(markdown, false)
	for _, expected := range []string{
		"Breaking change",
		"  selector:",
		"    mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		"What's Changed",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("renderMarkdown() output is missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "Generated metadata") {
		t.Fatalf("renderMarkdown() retained an HTML comment:\n%s", got)
	}
}

func TestHumanColorOutputRendersMarkdown(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.Result{
		SourceType:          "helm_repository",
		CurrentChartVersion: "1.0.0",
		TargetChartVersion:  "1.1.0",
		CurrentAppVersion:   "v1.0.0",
		TargetAppVersion:    "v1.1.0",
		Releases: []inspector.ReleaseNote{{
			Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
			BodyPreview: []string{"# Breaking change", "", "Use `layerSelector`."},
		}},
	}, Options{Color: true, Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Breaking change") || !strings.Contains(output.String(), "layerSelector") {
		t.Fatalf("Human() did not render Markdown:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("Human() did not render ANSI colors:\n%s", output.String())
	}
}
