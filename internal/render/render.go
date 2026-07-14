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
	Color bool
	Width int
}

// Human prints a readable inspection report without changing the JSON contract.
// It dynamically chooses detailed single-chart rendering or a summary table.
func Human(writer io.Writer, result inspector.BatchResult, options Options) error {
	if result.Error != "" {
		fmt.Fprintf(writer, "Error: %s\n", result.Error)
		return nil
	}
	if len(result.Results) == 1 {
		return renderSingle(writer, result.Results[0], options)
	}
	return renderTable(writer, result, options)
}

// Warning writes a release-note diagnostic to the error stream.
func Warning(writer io.Writer, message string, color bool) {
	if message == "" {
		return
	}
	text := "Release notes: " + message
	if color {
		text = pterm.FgYellow.Sprint(text)
	}
	fmt.Fprintln(writer, text)
}

func renderSingle(writer io.Writer, result inspector.Result, options Options) error {
	if result.Error != "" {
		fmt.Fprintf(writer, "Error: %s\n", result.Error)
		return nil
	}
	renderSingleSummary(writer, result, options)
	renderValuesDiff(writer, result, options)
	if len(result.Releases) == 0 {
		return nil
	}
	section := "Release notes"
	if options.Color {
		section = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(section)
	}
	fmt.Fprintf(writer, "\n%s\n", section)
	for _, release := range result.Releases {
		if err := renderRelease(writer, release, options); err != nil {
			return err
		}
	}
	return nil
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
	fmt.Fprintf(writer, "\n%s\n", section)
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

func renderRelease(writer io.Writer, release inspector.ReleaseNote, options Options) error {
	label := fmt.Sprintf("%s: %s", release.Version, release.URL)
	if options.Color {
		label = pterm.NewStyle(pterm.Bold, pterm.FgGreen).Sprint(label)
	}
	fmt.Fprintf(writer, "\n%s\n", label)
	if len(release.BodyPreview) > 0 {
		fmt.Fprintln(writer, renderMarkdown(strings.Join(release.BodyPreview, "\n"), options.Color))
	}
	if release.Truncated {
		message := fmt.Sprintf("[Preview truncated; %d characters total]", release.BodyCharacters)
		if options.Color {
			message = pterm.FgGray.Sprint(message)
		}
		fmt.Fprintln(writer, message)
	}
	return nil
}

func renderSingleSummary(writer io.Writer, result inspector.Result, options Options) {
	source := sourceLabel(result.SourceType)
	chart := result.ChartVersion + " -> " + result.TargetChartVersion
	application := result.AppVersion + " -> " + result.TargetAppVersion
	if options.Color {
		labelStyle := pterm.NewStyle(pterm.Bold, pterm.FgLightCyan)
		currentStyle := pterm.NewStyle(pterm.Bold, pterm.FgYellow)
		targetStyle := pterm.NewStyle(pterm.Bold, pterm.FgGreen)
		chart = currentStyle.Sprint(result.ChartVersion) + " -> " + targetStyle.Sprint(result.TargetChartVersion)
		application = currentStyle.Sprint(result.AppVersion) + " -> " + targetStyle.Sprint(result.TargetAppVersion)
		source = labelStyle.Sprint("Source:") + " " + source
		chart = labelStyle.Sprint("Chart:") + " " + chart
		application = labelStyle.Sprint("Application:") + " " + application
	} else {
		source = "Source: " + source
		chart = "Chart: " + chart
		application = "Application: " + application
	}
	fmt.Fprintln(writer, source)
	fmt.Fprintln(writer, chart)
	fmt.Fprintln(writer, application)
}

func sourceLabel(sourceType string) string {
	switch sourceType {
	case "helm_repository":
		return "Helm repository"
	case "oci_registry":
		return "OCI registry"
	default:
		return sourceType
	}
}

func renderMarkdown(markdown string, color bool) string {
	var rendered strings.Builder
	inCodeBlock := false
	inComment := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
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
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			line = "  " + line
			if color {
				line = pterm.FgGray.Sprint(line)
			}
		} else if strings.HasPrefix(trimmed, "#") {
			line = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if color {
				line = pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint(line)
			}
		} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			line = "- " + strings.TrimSpace(trimmed[2:])
		}
		rendered.WriteString(line)
		rendered.WriteByte('\n')
	}
	return strings.TrimSpace(rendered.String())
}
