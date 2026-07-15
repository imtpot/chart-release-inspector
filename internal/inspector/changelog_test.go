package inspector

import (
	"strings"
	"testing"
)

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

func TestHelmRepositorySourceRecognizesGitHubPages(t *testing.T) {
	if got := helmRepositorySource("https://grafana.github.io/helm-charts"); got != "https://github.com/grafana/helm-charts" {
		t.Fatalf("helmRepositorySource() = %q", got)
	}
}
