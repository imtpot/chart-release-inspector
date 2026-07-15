package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/imtpot/chart-release-inspector/internal/inspector"
)

func TestHumanPlainOutputIncludesChangelog(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.1.0",
			Changelog: []inspector.ChangelogEntry{{
				Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
				BodyPreview: []string{"# Breaking change"}, BodyCharacters: 42, Truncated: true,
			}},
		}},
	}, Options{IncludeChangelog: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Chart",
		"Chart Version",
		"App Version",
		"Status",
		"1.0.0 -> 1.1.0",
		"v1.0.0 -> v1.1.0",
		"Changelog",
		"1.1.0: https://example.test/releases/1.1.0",
		"Breaking change",
		"[Preview truncated; 42 characters total]",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Human() output is missing %q:\n%s", expected, output.String())
		}
	}
}

func TestHumanOutputIncludesValuesDiffWithoutChangelog(t *testing.T) {
	changed := true
	var output bytes.Buffer
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.1.0",
			ValuesDiffChanged:  &changed,
			ValuesDiff:         []string{"--- values.yaml (1.0.0)", "+++ values.yaml (1.1.0)", "@@ -1 +1 @@", "-replicas: 1", "+replicas: 2"},
		}},
	}, Options{})
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
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.1.0",
			Changelog: []inspector.ChangelogEntry{{
				Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
				BodyPreview: []string{"# Breaking change", "", "Use `layerSelector`."},
			}},
		}},
	}, Options{Color: true, IncludeChangelog: true})
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

func TestHumanOutputShowsNoChangelogMessage(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.0.0", // app version unchanged
			Status:             inspector.StatusUpdate,
			Changelog:          []inspector.ChangelogEntry{},
		}},
	}, Options{IncludeChangelog: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No changelog found.") {
		t.Fatalf("Human() did not print 'No changelog found.':\n%s", output.String())
	}
}

func TestHumanPlainOutputHasNoAnsiWhenColorDisabled(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.1.0",
			Status:             inspector.StatusUpdate,
			Changelog: []inspector.ChangelogEntry{{
				Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
				BodyPreview: []string{"# Breaking change"},
			}},
		}},
	}, Options{IncludeChangelog: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("Human() emitted ANSI escapes with color disabled:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Chart Version") {
		t.Fatalf("Human() missing table header:\n%s", output.String())
	}
}

func TestHumanOutputOmitsChangelogSectionWhenDisabled(t *testing.T) {
	var output bytes.Buffer
	err := Human(&output, inspector.BatchResult{
		Results: []inspector.Result{{
			SourceType:         "helm_repository",
			ChartVersion:       "1.0.0",
			TargetChartVersion: "1.1.0",
			AppVersion:         "v1.0.0",
			TargetAppVersion:   "v1.1.0",
			Status:             inspector.StatusUpdate,
			Changelog:          []inspector.ChangelogEntry{},
		}},
	}, Options{IncludeChangelog: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Changelog") || strings.Contains(output.String(), "No changelog found") {
		t.Fatalf("Human() printed a changelog section when disabled:\n%s", output.String())
	}
}
