package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/pterm/pterm"

	"github.com/imtpot/chart-release-inspector/internal/inspector"
)

// Options controls the terminal rendering policy selected by the CLI adapter.
type Options struct {
	Color            bool
	IncludeChangelog bool
}

// Human prints a readable inspection report without changing the JSON contract.
// It displays a status summary table and prints values diffs and release notes underneath.
func Human(writer io.Writer, result inspector.BatchResult, options Options) error {
	if result.Error != "" {
		fmt.Fprintf(writer, "Error: %s\n", result.Error)
		return nil
	}
	// pterm's table renderer reads its global color setting, not options.Color,
	// so mirror the requested mode onto pterm before rendering.
	if options.Color {
		pterm.EnableColor()
	} else {
		pterm.DisableColor()
	}

	if err := renderTable(writer, result, options); err != nil {
		return err
	}

	isSingle := len(result.Results) == 1
	groups := make(map[string]inspector.ChangelogGroup, len(result.ChangelogGroups))
	for _, group := range result.ChangelogGroups {
		groups[group.ID] = group
	}

	for _, res := range result.Results {
		if res.Error != "" {
			continue
		}
		if res.Status == inspector.StatusUpdate || isSingle {
			hasDiff := res.ValuesDiffChanged != nil
			group, isGrouped := groups[res.ChangelogGroup]
			hasChangelog := len(res.Changelog) > 0 || isGrouped
			showChangelog := options.IncludeChangelog

			if hasDiff || (showChangelog && hasChangelog) || (showChangelog && isSingle) {
				if !isSingle {
					header := fmt.Sprintf("--- Details for %s ---", res.Chart)
					if options.Color {
						header = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(header)
					}
					fmt.Fprintln(writer, header)
				}
				detailsPrinted := false
				if hasDiff {
					renderValuesDiff(writer, res, options)
					detailsPrinted = true
				}
				if showChangelog {
					if detailsPrinted {
						fmt.Fprintln(writer)
					}
					section := "Changelog"
					if options.Color {
						section = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(section)
					}
					fmt.Fprintln(writer, section)
					switch {
					case isGrouped:
						if err := renderGroupedChangelog(writer, res.Chart, group, options); err != nil {
							return err
						}
					case hasChangelog:
						for i, entry := range res.Changelog {
							if i > 0 {
								fmt.Fprintln(writer)
							}
							if err := renderEntry(writer, entry, options); err != nil {
								return err
							}
						}
					case res.ChangelogError == "":
						message := "No changelog found."
						if options.Color {
							message = pterm.FgGray.Sprint(message)
						}
						fmt.Fprintln(writer, message)
					default:
						message := "No changelog found: " + res.ChangelogError
						if options.Color {
							message = pterm.FgYellow.Sprint(message)
						}
						fmt.Fprintln(writer, message)
					}
				}
				if !isSingle {
					fmt.Fprintln(writer)
				}
			}
		}
	}
	return nil
}

// renderGroupedChangelog prints the shared changelog once, for the first
// chart in the group, and a short cross-reference for the rest — avoiding
// repeating the same release notes for every chart that shares them.
func renderGroupedChangelog(writer io.Writer, chart string, group inspector.ChangelogGroup, options Options) error {
	if len(group.Charts) == 0 || chart != group.Charts[0] {
		reference := "identical to " + group.Charts[0] + "'s changelog above"
		if options.Color {
			reference = pterm.FgGray.Sprint(reference)
		}
		fmt.Fprintln(writer, reference)
		return nil
	}

	others := group.Charts[1:]
	note := fmt.Sprintf("(shared with: %s)", strings.Join(others, ", "))
	if options.Color {
		note = pterm.FgGray.Sprint(note)
	}
	fmt.Fprintln(writer, note)
	for i, entry := range group.Changelog {
		if i > 0 {
			fmt.Fprintln(writer)
		}
		if err := renderEntry(writer, entry, options); err != nil {
			return err
		}
	}
	return nil
}

// Warning writes a changelog diagnostic to the error stream.
func Warning(writer io.Writer, message string, color bool) {
	if message == "" {
		return
	}
	text := "Changelog: " + message
	if color {
		text = pterm.FgYellow.Sprint(text)
	}
	fmt.Fprintf(writer, "\n%s\n", text)
}

func renderTable(writer io.Writer, result inspector.BatchResult, options Options) error {
	tableData := pterm.TableData{
		{"Chart", "Chart Version", "App Version", "Status"},
	}
	for _, res := range result.Results {
		chart := res.ChartVersion
		if res.TargetChartVersion != "" && res.TargetChartVersion != res.ChartVersion {
			if options.Color {
				chart = pterm.FgYellow.Sprint(res.ChartVersion) + " -> " + pterm.FgGreen.Sprint(res.TargetChartVersion)
			} else {
				chart = chart + " -> " + res.TargetChartVersion
			}
		}
		app := res.AppVersion
		if res.TargetAppVersion != "" && res.TargetAppVersion != res.AppVersion {
			if options.Color {
				app = pterm.FgYellow.Sprint(res.AppVersion) + " -> " + pterm.FgGreen.Sprint(res.TargetAppVersion)
			} else {
				app = app + " -> " + res.TargetAppVersion
			}
		}
		status := res.Status
		if options.Color {
			switch status {
			case inspector.StatusCurrent:
				status = pterm.FgGray.Sprint(status)
			case inspector.StatusUpdate:
				status = pterm.FgGreen.Sprint(status)
			case inspector.StatusError:
				status = pterm.FgRed.Sprint(status)
			}
		}
		tableData = append(tableData, []string{res.Chart, chart, app, status})
	}

	pterm.DefaultTable.WithHasHeader().WithData(tableData).WithWriter(writer).Render()

	for _, res := range result.Results {
		if res.Error != "" {
			fmt.Fprintf(writer, "\nError in %s: %s\n", res.Chart, res.Error)
		}
	}
	return nil
}

func renderValuesDiff(writer io.Writer, result inspector.Result, options Options) {
	if result.ValuesDiffChanged == nil && result.ValuesDiffError == "" {
		return
	}
	section := "Values diff"
	if options.Color {
		section = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(section)
	}
	fmt.Fprintln(writer, section)
	if result.ValuesDiffError != "" {
		message := "Unable to compare values.yaml: " + result.ValuesDiffError
		if options.Color {
			message = pterm.FgYellow.Sprint(message)
		}
		fmt.Fprintln(writer, message)
		return
	}
	if result.ValuesDiffChanged == nil || !*result.ValuesDiffChanged {
		fmt.Fprintln(writer, "No values.yaml changes.")
		return
	}
	for _, line := range result.ValuesDiff {
		if options.Color {
			switch {
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				line = pterm.FgGreen.Sprint(line)
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				line = pterm.FgRed.Sprint(line)
			case strings.HasPrefix(line, "@@"):
				line = pterm.FgYellow.Sprint(line)
			}
		}
		fmt.Fprintln(writer, line)
	}
}

func renderEntry(writer io.Writer, entry inspector.ChangelogEntry, options Options) error {
	label := fmt.Sprintf("%s: %s", entry.Version, entry.URL)
	if options.Color {
		label = pterm.NewStyle(pterm.Bold, pterm.FgGreen).Sprint(label)
	}
	fmt.Fprintln(writer, label)
	if len(entry.BodyPreview) > 0 {
		fmt.Fprintln(writer, renderMarkdown(strings.Join(entry.BodyPreview, "\n"), options.Color))
	}
	if entry.Truncated {
		message := fmt.Sprintf("[Preview truncated; %d characters total]", entry.BodyCharacters)
		if options.Color {
			message = pterm.FgGray.Sprint(message)
		}
		fmt.Fprintln(writer, message)
	}
	return nil
}

func renderMarkdown(markdown string, color bool) string {
	var rendered strings.Builder
	inCodeBlock := false
	inComment := false

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)

		// Skip HTML comments
		if strings.HasPrefix(trimmed, "<!--") {
			inComment = !strings.Contains(trimmed, "-->")
			continue
		}
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}

		// Track and retain code blocks
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if color {
				line = pterm.FgGray.Sprint(line)
			}
			rendered.WriteString(line + "\n")
			continue
		}

		if inCodeBlock {
			line = "  " + line
			if color {
				line = pterm.FgGray.Sprint(line)
			}
		} else {
			// Color markdown headers
			if strings.HasPrefix(trimmed, "#") {
				if color {
					line = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(line)
				}
			}
		}

		rendered.WriteString(line + "\n")
	}

	return strings.TrimSpace(rendered.String())
}
