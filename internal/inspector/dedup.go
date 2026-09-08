package inspector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// DeduplicateChangelogs groups results that carry byte-identical changelogs
// (e.g. several kustomize helmCharts entries tracking the same upstream
// application release) into shared ChangelogGroups, so the changelog body is
// emitted once instead of once per chart. Results with an empty changelog,
// or whose changelog is unique among the batch, are left untouched.
func DeduplicateChangelogs(result BatchResult) BatchResult {
	type bucket struct {
		key    string
		charts []int
	}

	order := make([]string, 0, len(result.Results))
	buckets := make(map[string]*bucket, len(result.Results))

	for i, res := range result.Results {
		if len(res.Changelog) == 0 {
			continue
		}
		key := changelogHash(res.Changelog)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{key: key}
			buckets[key] = b
			order = append(order, key)
		}
		b.charts = append(b.charts, i)
	}

	for _, key := range order {
		b := buckets[key]
		if len(b.charts) < 2 {
			continue
		}
		groupID := changelogGroupID(key)
		group := ChangelogGroup{
			ID:        groupID,
			Charts:    make([]string, 0, len(b.charts)),
			Changelog: result.Results[b.charts[0]].Changelog,
		}
		for _, idx := range b.charts {
			group.Charts = append(group.Charts, result.Results[idx].Chart)
			result.Results[idx].Changelog = nil
			result.Results[idx].ChangelogGroup = groupID
		}
		result.ChangelogGroups = append(result.ChangelogGroups, group)
	}

	return result
}

func changelogHash(entries []ChangelogEntry) string {
	// Marshaling is deterministic for a fixed struct field order, which
	// ChangelogEntry has, so this is safe to use as a grouping key.
	data, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func changelogGroupID(hash string) string {
	suffix := hash
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	return "cg-" + suffix
}
