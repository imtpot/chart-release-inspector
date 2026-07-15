package inspector

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

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

func diffLines(diff string) []string {
	diff = strings.TrimSuffix(diff, "\n")
	if diff == "" {
		return nil
	}
	return strings.Split(diff, "\n")
}

func truncateRunes(value string, limit int) (string, bool) {
	characters := []rune(value)
	if limit == 0 || len(characters) <= limit {
		return value, false
	}
	return string(characters[:limit]), true
}

func previewLines(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	preview := make([]string, 0, len(lines))
	inCodeBlock := false
	inComment := false
	previousBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			preview = append(preview, line)
			previousBlank = false
			continue
		}
		if !inCodeBlock && strings.HasPrefix(trimmed, "<!--") {
			inComment = !strings.Contains(trimmed, "-->")
			continue
		}
		if !inCodeBlock && inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if !inCodeBlock && trimmed == "" {
			if previousBlank || len(preview) == 0 {
				continue
			}
			previousBlank = true
		} else {
			previousBlank = false
		}
		preview = append(preview, line)
	}
	for len(preview) > 0 && strings.TrimSpace(preview[len(preview)-1]) == "" {
		preview = preview[:len(preview)-1]
	}
	return preview
}
