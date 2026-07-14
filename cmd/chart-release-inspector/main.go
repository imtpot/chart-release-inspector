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

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "inspect":
		inspect(os.Args[2:])
	case "batch":
		batch(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "config":
		validateConfig(os.Args[2:])
	default:
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func inspect(args []string) {
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
	_ = flags.Parse(args)

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
		if err := writeJSON(result); err != nil {
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

func batch(args []string) {
	flags := flag.NewFlagSet("batch", flag.ExitOnError)
	filename := flags.String("file", "", "YAML batch manifest")
	releaseNoteLimit := flags.Int("release-note-limit", 2000, "maximum release-note characters; 0 keeps the complete body")
	releaseNotesConfig := flags.String("release-notes-config", "", "YAML file with chart-specific upstream release rules")
	_ = flags.Parse(args)

	if *filename == "" {
		writeBatchError("--file is required")
		return
	}
	manifest, err := inspector.LoadBatchManifest(*filename)
	if err != nil {
		writeBatchError(err.Error())
		return
	}
	config := inspector.ReleaseNotesConfig{}
	if *releaseNotesConfig != "" {
		config, err = inspector.LoadReleaseNotesConfig(*releaseNotesConfig)
		if err != nil {
			writeBatchError(err.Error())
			return
		}
	}
	result := inspector.InspectBatch(context.Background(), manifest, config, *releaseNoteLimit)
	if err := writeJSON(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(exitCodeForStatus(result.Status))
}

func writeBatchError(message string) {
	result := inspector.BatchResult{
		SchemaVersion: inspector.BatchSchemaVersion,
		Status:        inspector.StatusError,
		Error:         message,
		Results:       []inspector.Result{},
	}
	if err := writeJSON(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(exitError)
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func validateConfig(args []string) {
	if len(args) != 2 || args[0] != "validate" {
		fmt.Fprintln(os.Stderr, "usage: chart-release-inspector config validate <release-notes.yaml>")
		os.Exit(2)
	}
	ruleCount, err := validateConfigFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	fmt.Printf("valid release notes config: %d rule(s)\n", ruleCount)
}

func validateConfigFile(filename string) (int, error) {
	config, err := inspector.LoadReleaseNotesConfig(filename)
	if err != nil {
		return 0, err
	}
	return len(config.Rules), nil
}

func printUsage(writer *os.File) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  chart-release-inspector inspect [flags]")
	fmt.Fprintln(writer, "  chart-release-inspector batch --file charts.yaml")
	fmt.Fprintln(writer, "  chart-release-inspector config validate <release-notes.yaml>")
	fmt.Fprintln(writer, "  chart-release-inspector version")
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
	return exitCodeForStatus(result.Status)
}

func exitCodeForStatus(status string) int {
	switch status {
	case inspector.StatusCurrent:
		return exitCurrent
	case inspector.StatusUpdate:
		return exitUpdateAvailable
	default:
		return exitError
	}
}
