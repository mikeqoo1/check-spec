package prompt

import (
	"strings"
	"testing"

	"github.com/mikeqoo1/check-spec/internal/arceus"
	"github.com/mikeqoo1/check-spec/internal/gitdiff"
)

func sampleChange() *arceus.Change {
	return &arceus.Change{
		ID:    "sample",
		Title: "Sample",
		Meta:  arceus.Meta{Status: "active"},
		Proposal: `# Sample
why this change
`,
		Spec: `# Spec

## 驗收條件

- [ ] prints hello
- [x] exit 0
`,
		Tasks: `# Tasks

## Phase 0 — Bootstrap

- [x] init
- [ ] more
`,
		Decisions: `# Decisions
none
`,
		ParsedTasks: []arceus.Task{
			{Index: 1, Phase: "Phase 0 — Bootstrap", Text: "init", Checked: true},
			{Index: 2, Phase: "Phase 0 — Bootstrap", Text: "more", Checked: false},
		},
		ParsedAcceptanceCriteria: []arceus.AcceptanceCriterion{
			{Index: 1, Text: "prints hello", Checked: false},
			{Index: 2, Text: "exit 0", Checked: true},
		},
	}
}

func sampleDiff() *gitdiff.Diff {
	return &gitdiff.Diff{
		BaseRef: "origin/main", HeadRef: "HEAD",
		BaseSHA: "aaa111", HeadSHA: "bbb222",
		Files: []gitdiff.FileDiff{
			{Path: "main.go", Status: "M", Added: 3, Deleted: 1,
				Diff: "@@ -1,1 +1,3 @@\n-old\n+new1\n+new2\n"},
			{Path: "image.png", Status: "M", Binary: true},
			{Path: "huge.txt", Status: "M", Added: 9999, Omitted: true},
		},
	}
}

func TestSystemPrompt_Shape(t *testing.T) {
	if len(SystemPrompt) < 200 {
		t.Errorf("system prompt suspiciously short: %d chars", len(SystemPrompt))
	}
	for _, want := range []string{"APPROVE", "REQUEST_CHANGES", "NEEDS_DISCUSSION", "JSON", "verdict"} {
		if !strings.Contains(SystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestBuild_TwoSegmentsCacheable(t *testing.T) {
	sys, segs := Build(sampleChange(), sampleDiff())
	if sys != SystemPrompt {
		t.Errorf("system prompt mismatch")
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}
	if !segs[0].Cache {
		t.Errorf("segment[0] should have Cache=true")
	}
	if segs[1].Cache {
		t.Errorf("segment[1] should have Cache=false")
	}
}

func TestBuild_StaticContainsProposalAndSpec(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	static := segs[0].Text
	for _, want := range []string{"<change_id>sample</change_id>", "<proposal>", "<spec>", "acceptance_criteria_indexed"} {
		if !strings.Contains(static, want) {
			t.Errorf("static segment missing %q", want)
		}
	}
	// tasks/decisions/diff should NOT appear in static segment
	for _, banned := range []string{"<tasks_md>", "<decisions>", "<git_diff"} {
		if strings.Contains(static, banned) {
			t.Errorf("static segment should not contain %q", banned)
		}
	}
}

func TestBuild_DynamicContainsTasksDecisionsDiff(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	dynamic := segs[1].Text
	for _, want := range []string{"<tasks_md>", "<tasks_indexed>", "<decisions>", "<git_diff"} {
		if !strings.Contains(dynamic, want) {
			t.Errorf("dynamic segment missing %q", want)
		}
	}
}

func TestBuild_IndexedTasksRenderedWithPhaseAndCheck(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	dynamic := segs[1].Text
	if !strings.Contains(dynamic, "1. [x] [Phase 0 — Bootstrap] init") {
		t.Errorf("expected checked task line not found in:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "2. [ ] [Phase 0 — Bootstrap] more") {
		t.Errorf("expected unchecked task line not found")
	}
}

func TestBuild_IndexedCriteriaRendered(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	static := segs[0].Text
	if !strings.Contains(static, "1. [ ] prints hello") {
		t.Errorf("criterion 1 not rendered in static segment")
	}
	if !strings.Contains(static, "2. [x] exit 0") {
		t.Errorf("criterion 2 not rendered")
	}
}

func TestBuild_DiffSectionShowsBinaryAndOmitted(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	dynamic := segs[1].Text
	if !strings.Contains(dynamic, "image.png") || !strings.Contains(dynamic, "binary=true") {
		t.Errorf("binary file metadata missing")
	}
	if !strings.Contains(dynamic, "(binary file — content not shown)") {
		t.Errorf("binary placeholder missing")
	}
	if !strings.Contains(dynamic, "huge.txt") || !strings.Contains(dynamic, "omitted=true") {
		t.Errorf("omitted file metadata missing")
	}
	if !strings.Contains(dynamic, "(file content omitted due to truncation budget)") {
		t.Errorf("truncation placeholder missing")
	}
}

func TestBuild_DiffSectionIncludesShas(t *testing.T) {
	_, segs := Build(sampleChange(), sampleDiff())
	dynamic := segs[1].Text
	for _, want := range []string{`base="origin/main"`, `head="HEAD"`, `base_sha="aaa111"`, `head_sha="bbb222"`} {
		if !strings.Contains(dynamic, want) {
			t.Errorf("expected %q in diff section", want)
		}
	}
}

func TestBuild_TruncatedNoteAppears(t *testing.T) {
	change := sampleChange()
	diff := sampleDiff()
	diff.Truncated = true
	_, segs := Build(change, diff)
	if !strings.Contains(segs[1].Text, "diff was truncated") {
		t.Errorf("truncation banner missing when Truncated=true")
	}
}
