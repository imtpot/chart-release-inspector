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

var (
	githubAPIBase = "https://api.github.com"
	githubWebBase = "https://github.com"
	// githubHTTPClient is overridable in tests.
	githubHTTPClient = http.DefaultClient
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

var semverRegex = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?`)

// fetchChangelog resolves the application changelog for all releases between currentVersion
// and targetVersion. It fetches the GitHub releases through the API; if that fails, it falls
// back to scraping the rendered HTML release page for the target version.
func fetchChangelog(
	ctx context.Context,
	source, currentVersion, targetVersion string,
	excerptLimit int,
) ([]ChangelogEntry, string) {
	repository := githubRepository(source)
	if repository == "" || targetVersion == "" || currentVersion == "" {
		return []ChangelogEntry{}, ""
	}

	releases, err := listGitHubReleases(ctx, repository)
	if err == nil {
		entries := filterReleases(releases, currentVersion, targetVersion, excerptLimit)
		if len(entries) > 0 {
			return entries, ""
		}
		return []ChangelogEntry{}, ""
	}

	// Fallback: API failed, try scraping the targetVersion release page.
	// Since tag format might be v1.2.3, 1.2.3, mimir-3.1.2, or controller-v1.15.1,
	// we generate a list of likely tag candidates based on the repository name.
	vVersion := "v" + strings.TrimPrefix(targetVersion, "v")
	rawVersion := strings.TrimPrefix(targetVersion, "v")

	tagsToTry := []string{vVersion, rawVersion}

	parts := strings.Split(repository, "/")
	if len(parts) >= 2 {
		repoName := strings.ToLower(parts[1])
		tagsToTry = append(tagsToTry, repoName+"-"+vVersion)
		tagsToTry = append(tagsToTry, repoName+"-"+rawVersion)

		// Handle common monorepo prefix variations
		if strings.Contains(repoName, "mimir") {
			tagsToTry = append(tagsToTry, "mimir-"+vVersion)
			tagsToTry = append(tagsToTry, "mimir-"+rawVersion)
		}
		if strings.Contains(repoName, "ingress-nginx") {
			tagsToTry = append(tagsToTry, "controller-"+vVersion)
			tagsToTry = append(tagsToTry, "controller-"+rawVersion)
		}
	}

	var lastScrapeErr error
	for _, tag := range tagsToTry {
		fallback, scrapeErr := scrapeGitHubReleasePage(ctx, repository, tag, targetVersion, excerptLimit)
		if scrapeErr == nil {
			return []ChangelogEntry{fallback}, fmt.Sprintf("GitHub API: %v; used release page fallback for tag %s", err, tag)
		}
		lastScrapeErr = scrapeErr
	}

	return []ChangelogEntry{}, fmt.Sprintf("GitHub API: %v; release page fallback failed: %v", err, lastScrapeErr)
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
	var releases []githubRelease
	for page := 1; ; page++ {
		request, err := newGitHubRequest(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", githubAPIBase, repository, page))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		response, err := githubHTTPClient.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("GitHub API returned HTTP %d", response.StatusCode)
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

func extractVersion(tag string) (*semver.Version, error) {
	match := semverRegex.FindString(tag)
	if match == "" {
		return nil, fmt.Errorf("no semver found in tag %q", tag)
	}
	return parseVersion(match)
}

func filterReleases(releases []githubRelease, current, target string, excerptLimit int) []ChangelogEntry {
	currentVer, currentErr := parseVersion(current)
	targetVer, targetErr := parseVersion(target)
	if currentErr != nil || targetErr != nil {
		return nil
	}

	type versionedRelease struct {
		release githubRelease
		version *semver.Version
	}
	seen := map[string]struct{}{}
	var selected []versionedRelease

	for _, release := range releases {
		ver, err := extractVersion(release.TagName)
		if err != nil {
			continue
		}
		// Skip prereleases, and filter by range (current < version <= target)
		if ver.Prerelease() != "" || !currentVer.LessThan(ver) || targetVer.LessThan(ver) {
			continue
		}
		key := ver.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, versionedRelease{release: release, version: ver})
	}

	sort.Slice(selected, func(left, right int) bool {
		return selected[left].version.LessThan(selected[right].version)
	})

	entries := make([]ChangelogEntry, 0, len(selected))
	for _, sel := range selected {
		excerpt, truncated := truncateRunes(sel.release.Body, excerptLimit)
		entries = append(entries, ChangelogEntry{
			Version:        sel.version.Original(),
			URL:            sel.release.HTMLURL,
			BodyPreview:    previewLines(excerpt),
			BodyCharacters: len([]rune(sel.release.Body)),
			Truncated:      truncated,
		})
	}
	return entries
}

// scrapeGitHubReleasePage fetches the rendered GitHub release page for the
// target version and extracts its markdown body, used when the API path fails
// or returns an empty body.
func scrapeGitHubReleasePage(ctx context.Context, repository, tag, displayVersion string, excerptLimit int) (ChangelogEntry, error) {
	request, err := newGitHubRequest(ctx, fmt.Sprintf("%s/%s/releases/tag/%s", githubWebBase, repository, tag))
	if err != nil {
		return ChangelogEntry{}, err
	}
	response, err := githubHTTPClient.Do(request)
	if err != nil {
		return ChangelogEntry{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ChangelogEntry{}, fmt.Errorf("GitHub release page returned HTTP %d", response.StatusCode)
	}
	page, err := io.ReadAll(response.Body)
	if err != nil {
		return ChangelogEntry{}, err
	}

	htmlContent := extractMarkdownBody(string(page))
	if htmlContent == "" {
		return ChangelogEntry{}, errors.New("release page did not contain release notes")
	}
	htmlContent = strings.ReplaceAll(htmlContent, "\r", "")

	// Convert <pre><code> blocks to markdown code blocks with placeholders for newlines.
	preCodeRegexp := regexp.MustCompile(`(?is)<pre[^>]*>\s*<code[^>]*>(.*?)</code>\s*</pre>`)
	htmlContent = preCodeRegexp.ReplaceAllStringFunc(htmlContent, func(block string) string {
		sub := preCodeRegexp.FindStringSubmatch(block)
		if len(sub) == 2 {
			codeContent := strings.ReplaceAll(sub[1], "\n", "__PRE_NEWLINE__")
			return "__PRE_NEWLINE____PRE_NEWLINE__```__PRE_NEWLINE__" + codeContent + "__PRE_NEWLINE__```__PRE_NEWLINE____PRE_NEWLINE__"
		}
		return block
	})

	// Convert standalone <pre> blocks as well, just in case.
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
	return ChangelogEntry{
		Version:     strings.TrimPrefix(displayVersion, "v"),
		URL:         fmt.Sprintf("%s/%s/releases/tag/%s", githubWebBase, repository, tag),
		BodyPreview: previewLines(excerpt), BodyCharacters: len([]rune(bodyCleaned)), Truncated: truncated,
	}, nil
}

func githubToken() string {
	return firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
}

// newGitHubRequest builds a GET request with the tool's User-Agent and an
// optional Bearer token from GITHUB_TOKEN/GH_TOKEN, shared by the API and
// HTML-scraping paths.
func newGitHubRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "chart-release-inspector")
	if token := githubToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
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
