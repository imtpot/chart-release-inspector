package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/imtpot/chart-release-inspector/internal/inspector"
	"github.com/imtpot/chart-release-inspector/internal/render"
)

const (
	exitCurrent         = iota
	exitUpdateAvailable = 10
	exitError           = 20
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "inspect" {
		fmt.Fprintln(os.Stderr, "usage: chart-release-inspector inspect [flags]")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("inspect", flag.ExitOnError)
	chart := flags.String("chart", "", "Helm chart name or oci:// reference")
	repository := flags.String("repository", "", "Helm repository URL")
	currentVersion := flags.String("current-version", "", "current chart version")
	targetVersion := flags.String("target-version", "", "target chart version")
	valuesDiff := flags.Bool("values-diff", false, "compare packaged values.yaml")
	releaseNoteLimit := flags.Int("release-note-limit", 2000, "maximum release-note characters; 0 keeps the complete body")
	releaseNotesConfig := flags.String("release-notes-config", "", "YAML file with chart-specific upstream release rules")
	output := flags.String("output", "terminal", "output format: terminal or json")
	colorMode := flags.String("color", "auto", "color mode: auto, always, or never")
	_ = flags.Parse(os.Args[2:])

	input := inspector.Input{
		Chart: *chart, Repository: *repository, CurrentVersion: *currentVersion,
		TargetVersion: *targetVersion, IncludeDiff: *valuesDiff,
		ReleaseNoteLimit: *releaseNoteLimit,
	}
	if *releaseNotesConfig != "" {
		config, err := inspector.LoadReleaseNotesConfig(*releaseNotesConfig)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(exitError)
		}
		input.ReleaseNoteRule = config.RuleForChart(*chart)
	}
	if !validOutput(*output) {
		fmt.Fprintln(os.Stderr, "--output must be terminal or json")
		os.Exit(exitError)
	}
	color, err := useColor(*colorMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	result := inspector.Inspect(context.Background(), input)
	if *output == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(exitCode(result))
	}
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		os.Exit(exitError)
	}
	if err := render.Human(os.Stdout, result, render.Options{Color: color, Width: terminalWidth()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	render.Warning(os.Stderr, result.ReleaseNotesError, color)
	os.Exit(exitCode(result))
}

func validOutput(output string) bool {
	return output == "json" || output == "terminal"
}

func useColor(mode string) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
			return false, nil
		}
		return term.IsTerminal(int(os.Stdout.Fd())), nil
	default:
		return false, fmt.Errorf("--color must be auto, always, or never")
	}
}

func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 40 {
		return 80
	}
	return width
}

func exitCode(result inspector.Result) int {
	switch result.Status {
	case inspector.StatusCurrent:
		return exitCurrent
	case inspector.StatusUpdate:
		return exitUpdateAvailable
	default:
		return exitError
	}
}
