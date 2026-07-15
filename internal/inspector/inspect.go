package inspector

import (
	"context"
	"strings"
	"sync"
)

// Inspect resolves a chart update and fetches its application changelog.
func Inspect(ctx context.Context, input Input) Result {
	result := Result{
		Chart:        input.Chart,
		Status:       StatusError,
		ChartVersion: input.Version,
		Changelog:    []ChangelogEntry{},
	}
	if input.Chart == "" || input.Version == "" {
		result.Error = "--chart and --version are required"
		return result
	}

	var current, target chartVersion
	var err error
	if strings.HasPrefix(input.Chart, "oci://") {
		result.SourceType = "oci_registry"
		current, target, err = inspectOCI(ctx, input)
	} else {
		result.SourceType = "helm_repository"
		if input.Repository == "" {
			result.Error = "--repository is required for a non-OCI chart"
			return result
		}
		current, target, err = inspectRepository(ctx, input)
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.TargetChartVersion = target.Version
	result.AppVersion = applicationVersion(current)
	result.TargetAppVersion = applicationVersion(target)

	if sameVersion(input.Version, target.Version) {
		result.Status = StatusCurrent
	} else {
		result.Status = StatusUpdate
	}

	if input.IncludeDiff {
		result.ValuesDiffChanged = boolPtr(false)
		if len(current.Values) == 0 || len(target.Values) == 0 {
			result.ValuesDiffError = "could not download chart values"
		} else {
			result.ValuesDiff = diffLines(unifiedDiff(current.Values, target.Values, input.Version, target.Version))
			result.ValuesDiffChanged = boolPtr(len(result.ValuesDiff) > 0)
		}
	}

	if input.IncludeChangelog && result.AppVersion != "" && result.TargetAppVersion != "" && !sameVersion(result.AppVersion, result.TargetAppVersion) {
		changelogRepo := firstNonEmpty(input.AppRepository, target.Source, helmRepositorySource(input.Repository))
		result.Changelog, result.ChangelogError = fetchChangelog(
			ctx,
			changelogRepo,
			result.AppVersion,
			result.TargetAppVersion,
			input.ChangelogLimit,
		)
	}

	return result
}

// InspectBatch runs each manifest entry concurrently and aggregates their statuses.
func InspectBatch(
	ctx context.Context,
	manifest BatchManifest,
	changelogLimit int,
	globalValuesDiff bool,
	includeChangelog bool,
) BatchResult {
	result := BatchResult{
		SchemaVersion: BatchSchemaVersion,
		Status:        StatusCurrent,
		Results:       make([]Result, len(manifest.Charts)),
	}

	var wg sync.WaitGroup
	for i, chart := range manifest.Charts {
		wg.Add(1)
		go func(index int, c BatchChart) {
			defer wg.Done()
			result.Results[index] = Inspect(ctx, Input{
				Chart:            c.Chart,
				Repository:       c.Repository,
				Version:          c.Version,
				TargetVersion:    c.TargetVersion,
				IncludeDiff:      globalValuesDiff,
				IncludeChangelog: includeChangelog,
				ChangelogLimit:   changelogLimit,
				AppRepository:    c.AppRepository,
			})
		}(i, chart)
	}
	wg.Wait()

	for _, chartResult := range result.Results {
		switch chartResult.Status {
		case StatusError:
			result.Status = StatusError
		case StatusUpdate:
			if result.Status == StatusCurrent {
				result.Status = StatusUpdate
			}
		}
	}

	return result
}

func boolPtr(value bool) *bool {
	return &value
}
