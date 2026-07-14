package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"go.yaml.in/yaml/v3"
)

var githubAPIBase = "https://api.github.com"
var githubWebBase = "https://github.com"

// ReleaseNotesConfig contains optional upstream-specific release conventions.
type ReleaseNotesConfig struct {
	Rules []ReleaseNoteRule `yaml:"rules"`
}

// ReleaseNoteRule matches a chart and describes its upstream release tags.
type ReleaseNoteRule struct {
	Chart       string `yaml:"chart"`
	Provider    string `yaml:"provider"`
	Repository  string `yaml:"repository"`
	TagTemplate string `yaml:"tag_template"`
	Version     string `yaml:"version"`
}

// LoadReleaseNotesConfig reads rules independent from any deployment system.
func LoadReleaseNotesConfig(filename string) (ReleaseNotesConfig, error) {
	contents, err := os.ReadFile(filename)
	if err != nil {
		return ReleaseNotesConfig{}, fmt.Errorf("read release notes config: %w", err)
	}
	var config ReleaseNotesConfig
	if err := yaml.Unmarshal(contents, &config); err != nil {
		return ReleaseNotesConfig{}, fmt.Errorf("parse release notes config: %w", err)
	}
	for _, rule := range config.Rules {
		if rule.Chart == "" {
			return ReleaseNotesConfig{}, fmt.Errorf("release note rule is missing chart")
		}
		if rule.Provider != "" && rule.Provider != "github" && rule.Provider != "none" {
			return ReleaseNotesConfig{}, fmt.Errorf("unsupported release note provider %q", rule.Provider)
		}
		if rule.Provider == "github" && rule.Repository == "" {
			return ReleaseNotesConfig{}, fmt.Errorf("GitHub release note rule for %q is missing repository", rule.Chart)
		}
		if rule.Repository != "" && githubRepository(rule.Repository) == "" {
			return ReleaseNotesConfig{}, fmt.Errorf(
				"release note repository for %q must be a full GitHub URL",
				rule.Chart,
			)
		}
		if rule.Version != "" && rule.Version != "application" && rule.Version != "chart" {
			return ReleaseNotesConfig{}, fmt.Errorf("release note rule for %q has invalid version type %q", rule.Chart, rule.Version)
		}
	}
	return config, nil
}

// RuleForChart returns the first matching rule for a Helm chart reference.
func (config ReleaseNotesConfig) RuleForChart(chart string) ReleaseNoteRule {
	name := chartName(chart)
	for _, rule := range config.Rules {
		if rule.Chart == name {
			return rule
		}
	}
	return ReleaseNoteRule{}
}

func chartName(chart string) string {
	return strings.TrimSuffix(chart[strings.LastIndex(chart, "/")+1:], "/")
}

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
) ([]ReleaseNote, string) {
	if rule.Provider == "none" || targetVersion == "" {
		return []ReleaseNote{}, ""
	}
	if rule.Repository != "" {
		source = rule.Repository
	}
	repository := githubRepository(source)
	if repository == "" {
		return []ReleaseNote{}, ""
	}
	releases, err := listGitHubReleases(ctx, repository)
	if err != nil {
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
	match := regexp.MustCompile(`(?s)<div[^>]+class="[^"]*markdown-body[^"]*"[^>]*>(.*?)</div>`).FindSubmatch(page)
	if len(match) != 2 {
		return nil, errors.New("release page did not contain release notes")
	}
	body := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(string(match[1]), " "))
	body = strings.Join(strings.Fields(body), " ")
	excerpt, truncated := truncateRunes(body, excerptLimit)
	return []ReleaseNote{{
		Version:     version,
		URL:         fmt.Sprintf("%s/%s/releases/tag/%s", githubWebBase, repository, tag),
		BodyPreview: previewLines(excerpt), BodyCharacters: len([]rune(body)), Truncated: truncated,
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
