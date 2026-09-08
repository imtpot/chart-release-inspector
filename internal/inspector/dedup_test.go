package inspector

import "testing"

func TestDeduplicateChangelogsGroupsIdenticalChangelogs(t *testing.T) {
	shared := []ChangelogEntry{{
		Version: "1.1.0", URL: "https://example.test/releases/1.1.0",
		BodyPreview: []string{"# Breaking change"}, BodyCharacters: 42,
	}}
	unique := []ChangelogEntry{{
		Version: "2.0.0", URL: "https://example.test/releases/2.0.0",
		BodyPreview: []string{"# Other change"}, BodyCharacters: 10,
	}}

	result := BatchResult{
		Results: []Result{
			{Chart: "chart-a", Changelog: shared},
			{Chart: "chart-b", Changelog: append([]ChangelogEntry{}, shared...)},
			{Chart: "chart-c", Changelog: unique},
		},
	}

	got := DeduplicateChangelogs(result)

	if len(got.ChangelogGroups) != 1 {
		t.Fatalf("expected exactly one changelog group, got %d: %+v", len(got.ChangelogGroups), got.ChangelogGroups)
	}
	group := got.ChangelogGroups[0]
	if len(group.Charts) != 2 || group.Charts[0] != "chart-a" || group.Charts[1] != "chart-b" {
		t.Fatalf("unexpected group charts: %+v", group.Charts)
	}
	if len(group.Changelog) != 1 || group.Changelog[0].Version != "1.1.0" {
		t.Fatalf("unexpected group changelog: %+v", group.Changelog)
	}

	if got.Results[0].ChangelogGroup != group.ID || got.Results[0].Changelog != nil {
		t.Fatalf("chart-a was not deduplicated into the group: %+v", got.Results[0])
	}
	if got.Results[1].ChangelogGroup != group.ID || got.Results[1].Changelog != nil {
		t.Fatalf("chart-b was not deduplicated into the group: %+v", got.Results[1])
	}
	if got.Results[2].ChangelogGroup != "" || len(got.Results[2].Changelog) != 1 {
		t.Fatalf("chart-c should be left untouched since its changelog is unique: %+v", got.Results[2])
	}
}

func TestDeduplicateChangelogsSkipsEmptyChangelogs(t *testing.T) {
	result := BatchResult{
		Results: []Result{
			{Chart: "chart-a", Changelog: nil},
			{Chart: "chart-b", Changelog: []ChangelogEntry{}},
		},
	}

	got := DeduplicateChangelogs(result)

	if len(got.ChangelogGroups) != 0 {
		t.Fatalf("expected no changelog groups for empty changelogs, got %+v", got.ChangelogGroups)
	}
	for _, res := range got.Results {
		if res.ChangelogGroup != "" {
			t.Fatalf("result with an empty changelog should not be grouped: %+v", res)
		}
	}
}
