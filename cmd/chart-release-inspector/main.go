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
	exitCurrent         = 0
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
	case "manifest":
		validateManifest(os.Args[2:])
	default:
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

type Config struct {
	Chart            string
	Repository       string
	Version          string
	TargetVersion    string
	ValuesDiff       bool
	Filename         string
	ReleaseNoteLimit int
	ReleaseNotes     bool
	FailOnUpdate     bool
	Output           string
	ColorMode        string
	Color            bool
	GitHubClient     string
}

func (c *Config) RegisterShared(flags *flag.FlagSet) {
	flags.IntVar(&c.ReleaseNoteLimit, "release-note-limit", 2000, "maximum release-note characters; 0 keeps the complete body")
	flags.BoolVar(&c.ReleaseNotes, "release-notes", true, "fetch and display release notes")
	flags.BoolVar(&c.ValuesDiff, "values-diff", false, "compare packaged values.yaml")
	flags.BoolVar(&c.FailOnUpdate, "fail-on-update", false, "exit with code 10 when updates are available")
	flags.StringVar(&c.Output, "output", "terminal", "output format: terminal or json")
	flags.StringVar(&c.ColorMode, "color", "auto", "color mode: auto, always, or never")
	flags.StringVar(&c.GitHubClient, "github-client", "auto", "GitHub client for release notes: auto, api, or html")
}

func (c *Config) ParseShared() error {
	if !validOutput(c.Output) {
		return fmt.Errorf("--output must be terminal or json")
	}
	if c.GitHubClient != "auto" && c.GitHubClient != "api" && c.GitHubClient != "html" {
		return fmt.Errorf("invalid --github-client: must be auto, api, or html")
	}
	color, err := useColor(c.ColorMode)
	if err != nil {
		return err
	}
	c.Color = color
	return nil
}

func inspect(args []string) {
	c := &Config{}
	flags := flag.NewFlagSet("inspect", flag.ExitOnError)
	flags.StringVar(&c.Chart, "chart", "", "Helm chart name or oci:// reference")
	flags.StringVar(&c.Repository, "repository", "", "Helm repository URL")
	flags.StringVar(&c.Version, "version", "", "chart version")
	flags.StringVar(&c.TargetVersion, "target-version", "", "target chart version")
	c.RegisterShared(flags)
	_ = flags.Parse(args)

	if err := c.ParseShared(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}

	if c.Chart == "" || c.Version == "" {
		fmt.Fprintln(os.Stderr, "--chart and --version are required")
		os.Exit(exitError)
	}

	manifest := inspector.BatchManifest{
		Charts: []inspector.BatchChart{
			{
				Chart:         c.Chart,
				Repository:    c.Repository,
				Version:       c.Version,
				TargetVersion: c.TargetVersion,
				ValuesDiff:    c.ValuesDiff,
			},
		},
	}
	runAndRender(manifest, c)
}

func batch(args []string) {
	c := &Config{}
	flags := flag.NewFlagSet("batch", flag.ExitOnError)
	flags.StringVar(&c.Filename, "file", "", "YAML batch manifest")
	c.RegisterShared(flags)
	_ = flags.Parse(args)

	if c.Filename == "" {
		writeBatchError("--file is required")
		return
	}
	if err := c.ParseShared(); err != nil {
		writeBatchError(err.Error())
		return
	}

	manifest, err := inspector.LoadBatchManifest(c.Filename)
	if err != nil {
		writeBatchError(err.Error())
		return
	}
	runAndRender(manifest, c)
}

func runAndRender(manifest inspector.BatchManifest, c *Config) {
	result := inspector.InspectBatch(context.Background(), manifest, c.ReleaseNoteLimit, !c.ReleaseNotes, c.ValuesDiff, c.GitHubClient)
	if c.Output == "json" {
		if err := writeJSON(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(exitCodeForStatus(result.Status, c.FailOnUpdate))
	}
	if err := render.Human(os.Stdout, result, render.Options{Color: c.Color, Width: terminalWidth()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	if len(result.Results) == 1 {
		render.Warning(os.Stderr, result.Results[0].ReleaseNotesError, c.Color)
	}
	os.Exit(exitCodeForStatus(result.Status, c.FailOnUpdate))
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

func validateManifest(args []string) {
	if len(args) != 2 || args[0] != "validate" {
		fmt.Fprintln(os.Stderr, "usage: chart-release-inspector manifest validate <charts.yaml>")
		os.Exit(2)
	}
	manifest, err := inspector.LoadBatchManifest(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	fmt.Printf("valid batch manifest: %d chart(s), %d rule(s)\n", len(manifest.Charts), len(manifest.Rules))
}

func printUsage(writer *os.File) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  chart-release-inspector inspect [flags]")
	fmt.Fprintln(writer, "  chart-release-inspector batch --file charts.yaml")
	fmt.Fprintln(writer, "  chart-release-inspector manifest validate <charts.yaml>")
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

func exitCodeForStatus(status string, failOnUpdate bool) int {
	switch status {
	case inspector.StatusCurrent:
		return exitCurrent
	case inspector.StatusUpdate:
		if failOnUpdate {
			return exitUpdateAvailable
		}
		return exitCurrent
	default:
		return exitError
	}
}
