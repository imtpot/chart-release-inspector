package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/pterm/pterm"

	"chart-release-inspector/internal/inspector"
)

// Options controls the terminal rendering policy selected by the CLI adapter.
type Options struct {
	Color bool
	Width int
}

// Human prints a readable inspection report without changing the JSON contract.
func Human(writer io.Writer, result inspector.Result, options Options) error {
	if options.Color {
		fmt.Fprintln(writer, summaryPanel(result))
	} else {
		fmt.Fprintf(writer, "Source: %s\n", sourceLabel(result.SourceType))
		fmt.Fprintf(writer, "Chart: %s -> %s\n", result.CurrentChartVersion, result.TargetChartVersion)
		fmt.Fprintf(writer, "Application: %s -> %s\n", result.CurrentAppVersion, result.TargetAppVersion)
	}

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

// Warning writes a release-note diagnostic to the error stream.
func Warning(writer io.Writer, message string, color bool) {
	if message == "" {
		return
	}
	text := "Release notes: " + message
	if color {
		text = pterm.FgYellow.Sprint(text)
	}
	fprintln(writer, text)
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

func summaryPanel(result inspector.Result) string {
	labelStyle := pterm.NewStyle(pterm.Bold, pterm.FgLightCyan)
	currentStyle := pterm.NewStyle(pterm.Bold, pterm.FgYellow)
	targetStyle := pterm.NewStyle(pterm.Bold, pterm.FgGreen)
	content := strings.Join([]string{
		labelStyle.Sprint("Source:") + " " + sourceLabel(result.SourceType),
		labelStyle.Sprint("Chart:") + " " + currentStyle.Sprint(result.CurrentChartVersion) + " -> " + targetStyle.Sprint(result.TargetChartVersion),
		labelStyle.Sprint("Application:") + " " + currentStyle.Sprint(result.CurrentAppVersion) + " -> " + targetStyle.Sprint(result.TargetAppVersion),
	}, "\n")
	return pterm.DefaultBox.
		WithTitle(pterm.NewStyle(pterm.Bold, pterm.FgLightCyan).Sprint("Chart update")).
		WithBoxStyle(pterm.FgLightCyan.ToStyle()).
		Sprint(content)
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

func fprintln(writer io.Writer, text string) {
	fmt.Fprintln(writer, text)
}
