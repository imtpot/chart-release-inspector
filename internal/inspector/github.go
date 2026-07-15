package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var githubAPIBase = "https://api.github.com"
var githubWebBase = "https://github.com"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

func githubReleaseNotes(
	ctx context.Context,
	rule ReleaseNoteRule,
	source, currentVersion, targetVersion string,
	excerptLimit int,
	skip bool,
	githubClient string,
) ([]ReleaseNote, string) {
	if skip || rule.Provider == "none" || targetVersion == "" {
		return []ReleaseNote{}, ""
	}
	if rule.Repository != "" {
		source = rule.Repository
	}
	repository := githubRepository(source)
	if repository == "" {
		return []ReleaseNote{}, ""
	}

	if githubClient == "html" {
		fallback, fallbackErr := githubReleasePage(ctx, repository, targetVersion, rule.TagTemplate, excerptLimit)
		if fallbackErr == nil {
			return fallback, ""
		}
		return []ReleaseNote{}, fmt.Sprintf("HTML release page scraping failed: %v", fallbackErr)
	}

	releases, err := listGitHubReleases(ctx, repository)
	if err != nil {
		if githubClient == "api" {
			return []ReleaseNote{}, fmt.Sprintf("GitHub API error: %v", err)
		}
		// auto mode: fallback to HTML scraping
		fallback, fallbackErr := githubReleasePage(ctx, repository, targetVersion, rule.TagTemplate, excerptLimit)
		if fallbackErr == nil {
			return fallback, fmt.Sprintf("%v; used GitHub release page fallback", err)
		}
		return []ReleaseNote{}, fmt.Sprintf("%v; GitHub release page fallback failed: %v", err, fallbackErr)
	}
	return releasesBetween(releases, currentVersion, targetVersion, rule.TagTemplate, excerptLimit), ""
}

func githubRepository(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host != "github.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

func listGitHubReleases(ctx context.Context, repository string) ([]githubRelease, error) {
	client := &http.Client{}
	var releases []githubRelease
	for page := 1; ; page++ {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", githubAPIBase, repository, page),
			nil,
		)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("User-Agent", "chart-release-inspector")
		if token := githubToken(); token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("GitHub releases API returned HTTP %d", response.StatusCode)
		}
		var pageReleases []githubRelease
		decodeErr := json.NewDecoder(response.Body).Decode(&pageReleases)
		linkHeader := response.Header.Get("Link")
		response.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		releases = append(releases, pageReleases...)
		if !strings.Contains(linkHeader, "rel=\"next\"") {
			return releases, nil
		}
	}
}

func releasesBetween(
	releases []githubRelease, current, target, tagTemplate string, excerptLimit int,
) []ReleaseNote {
	currentVersion, currentErr := parseVersion(current)
	targetVersion, targetErr := parseVersion(target)
	if currentErr != nil || targetErr != nil {
		return []ReleaseNote{}
	}
	type versionedRelease struct {
		release githubRelease
		version *semver.Version
	}
	selected := []versionedRelease{}
	for _, release := range releases {
		versionText, matched := releaseVersion(release.TagName, tagTemplate)
		if !matched {
			continue
		}
		version, err := parseVersion(versionText)
		if err != nil || version.Prerelease() != "" || !currentVersion.LessThan(version) || targetVersion.LessThan(version) {
			continue
		}
		selected = append(selected, versionedRelease{release: release, version: version})
	}
	sort.Slice(selected, func(left, right int) bool { return selected[left].version.LessThan(selected[right].version) })
	result := make([]ReleaseNote, 0, len(selected))
	for _, selectedRelease := range selected {
		excerpt, truncated := truncateRunes(selectedRelease.release.Body, excerptLimit)
		result = append(result, ReleaseNote{
			Version: selectedRelease.version.Original(), URL: selectedRelease.release.HTMLURL,
			BodyPreview: previewLines(excerpt), BodyCharacters: len([]rune(selectedRelease.release.Body)), Truncated: truncated,
		})
	}
	return result
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

func releaseVersion(tag, template string) (string, bool) {
	if template == "" {
		return strings.TrimPrefix(tag, "v"), true
	}
	prefix, suffix, found := strings.Cut(template, "{version}")
	if !found {
		return "", false
	}
	pattern := "^" + regexp.QuoteMeta(prefix) + "(.+)" + regexp.QuoteMeta(suffix) + "$"
	match := regexp.MustCompile(pattern).FindStringSubmatch(tag)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func githubReleasePage(
	ctx context.Context, repository, version, template string, excerptLimit int,
) ([]ReleaseNote, error) {
	tag, ok := releaseTag(version, template)
	if !ok {
		return nil, errors.New("invalid release tag template")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/%s/releases/tag/%s", githubWebBase, repository, tag),
		nil,
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "chart-release-inspector")
	if token := githubToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release page returned HTTP %d", response.StatusCode)
	}
	page, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	htmlContent := extractMarkdownBody(string(page))
	if htmlContent == "" {
		return nil, errors.New("release page did not contain release notes")
	}
	htmlContent = strings.ReplaceAll(htmlContent, "\r", "")

	// Convert <pre><code> blocks to markdown code blocks with placeholders for newlines
	preCodeRegexp := regexp.MustCompile(`(?is)<pre[^>]*>\s*<code[^>]*>(.*?)</code>\s*</pre>`)
	htmlContent = preCodeRegexp.ReplaceAllStringFunc(htmlContent, func(block string) string {
		sub := preCodeRegexp.FindStringSubmatch(block)
		if len(sub) == 2 {
			codeContent := strings.ReplaceAll(sub[1], "\n", "__PRE_NEWLINE__")
			return "__PRE_NEWLINE____PRE_NEWLINE__```__PRE_NEWLINE__" + codeContent + "__PRE_NEWLINE__```__PRE_NEWLINE____PRE_NEWLINE__"
		}
		return block
	})

	// Convert standalone <pre> blocks as well, just in case
	preRegexp := regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)
	htmlContent = preRegexp.ReplaceAllStringFunc(htmlContent, func(block string) string {
		sub := preRegexp.FindStringSubmatch(block)
		if len(sub) == 2 {
			if strings.Contains(sub[1], "```") {
				return block // already converted
			}
			codeContent := strings.ReplaceAll(sub[1], "\n", "__PRE_NEWLINE__")
			return "__PRE_NEWLINE____PRE_NEWLINE__```__PRE_NEWLINE__" + codeContent + "__PRE_NEWLINE__```__PRE_NEWLINE____PRE_NEWLINE__"
		}
		return block
	})

	htmlContent = strings.ReplaceAll(htmlContent, "\n", " ")
	htmlContent = regexp.MustCompile(`(?i)<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`).ReplaceAllString(htmlContent, "$2")
	htmlContent = regexp.MustCompile(`(?i)<strong>(.*?)</strong>`).ReplaceAllString(htmlContent, "**$1**")
	htmlContent = regexp.MustCompile(`(?i)<em>(.*?)</em>`).ReplaceAllString(htmlContent, "*$1*")
	htmlContent = regexp.MustCompile(`(?i)<code>(.*?)</code>`).ReplaceAllString(htmlContent, "`$1`")
	htmlContent = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(htmlContent, "\n")
	htmlContent = regexp.MustCompile(`(?i)<li>`).ReplaceAllString(htmlContent, "- ")
	htmlContent = regexp.MustCompile(`(?i)</(li|tr)>`).ReplaceAllString(htmlContent, "\n")
	htmlContent = regexp.MustCompile(`(?i)</(p|div|h[1-6]|pre|ul|ol)>`).ReplaceAllString(htmlContent, "\n\n")

	body := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(htmlContent, "")
	body = strings.ReplaceAll(body, "__PRE_NEWLINE__", "\n")
	body = html.UnescapeString(body)

	var lines []string
	previousBlank := false
	inCodeBlock := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			lines = append(lines, line)
			previousBlank = false
			continue
		}
		if inCodeBlock {
			trimmed := strings.TrimRight(line, " \t\r")
			lines = append(lines, trimmed)
			previousBlank = false
		} else {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if previousBlank || len(lines) == 0 {
					continue
				}
				previousBlank = true
			} else {
				previousBlank = false
			}
			lines = append(lines, trimmed)
		}
	}
	bodyCleaned := strings.Join(lines, "\n")
	excerpt, truncated := truncateRunes(bodyCleaned, excerptLimit)
	return []ReleaseNote{{
		Version:     version,
		URL:         fmt.Sprintf("%s/%s/releases/tag/%s", githubWebBase, repository, tag),
		BodyPreview: previewLines(excerpt), BodyCharacters: len([]rune(bodyCleaned)), Truncated: truncated,
	}}, nil
}

func releaseTag(version, template string) (string, bool) {
	if template == "" {
		return "v" + strings.TrimPrefix(version, "v"), true
	}
	if !strings.Contains(template, "{version}") {
		return "", false
	}
	return strings.ReplaceAll(template, "{version}", strings.TrimPrefix(version, "v")), true
}

func githubToken() string {
	return firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
}

func extractMarkdownBody(htmlContent string) string {
	idx := strings.Index(htmlContent, `class="markdown-body`)
	if idx == -1 {
		return ""
	}
	startIdx := strings.LastIndex(htmlContent[:idx], "<div")
	if startIdx == -1 {
		return ""
	}

	content := htmlContent[startIdx:]
	depth := 0
	pos := 0

	for pos < len(content) {
		if strings.HasPrefix(content[pos:], "<div ") || strings.HasPrefix(content[pos:], "<div>") {
			depth++
			pos += 4
		} else if strings.HasPrefix(content[pos:], "</div>") {
			depth--
			pos += 6
			if depth == 0 {
				return content[:pos]
			}
		} else {
			pos++
		}
	}
	return ""
}
