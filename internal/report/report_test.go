package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikeqoo1/check-spec/internal/arceus"
	"github.com/mikeqoo1/check-spec/internal/gitdiff"
	"github.com/mikeqoo1/check-spec/internal/llm"
)

func sampleInputs() (*arceus.Change, *gitdiff.Diff, llm.Output) {
	change := &arceus.Change{
		ID:    "sample-change",
		Title: "Sample change",
		Meta:  arceus.Meta{Status: "active"},
		ParsedTasks: []arceus.Task{
			{Index: 1, Phase: "Phase 0", Text: "init mod", Checked: true},
			{Index: 2, Phase: "Phase 0", Text: "add main.go", Checked: true},
			{Index: 3, Phase: "Phase 1", Text: "add tests", Checked: false},
		},
		ParsedAcceptanceCriteria: []arceus.AcceptanceCriterion{
			{Index: 1, Text: "prints hello", Checked: false},
			{Index: 2, Text: "exit 0", Checked: true},
		},
	}
	diff := &gitdiff.Diff{
		BaseRef: "origin/main", HeadRef: "HEAD",
		BaseSHA: "aaaaaaa1111111", HeadSHA: "bbbbbbb2222222",
		Files: []gitdiff.FileDiff{
			{Path: "main.go"}, {Path: "go.mod"},
		},
	}
	out := llm.Output{
		Verdict: llm.VerdictRequestChanges,
		Summary: "main.go implements hello but no test coverage",
		Tasks: []llm.TaskVerdict{
			{Index: 1, Reported: true, Actual: "done", Evidence: "go.mod"},
			{Index: 2, Reported: true, Actual: "done", Evidence: "main.go:1"},
			{Index: 3, Reported: false, Actual: "missing", Notes: "no _test.go file"},
		},
		Criteria: []llm.CriterionVerdict{
			{Index: 1, Status: "pass", Evidence: "main.go:5"},
			{Index: 2, Status: "fail", Notes: "exit code not verified by any test"},
		},
		Drift: llm.Drift{
			UndocumentedAdditions: []string{"main.go adds --debug flag not mentioned in spec"},
		},
		OpenQuestions: []string{"Should --debug be exposed in v0.1?"},
		Model:         "claude-opus-4-7",
	}
	return change, diff, out
}

func TestFrom_JoinsTaskAndCriterionText(t *testing.T) {
	change, diff, out := sampleInputs()
	r := From(change, diff, out)

	if r.ChangeID != "sample-change" || r.Model != "claude-opus-4-7" {
		t.Errorf("metadata wrong: %+v", r)
	}
	if r.FilesAnalyzed != 2 {
		t.Errorf("FilesAnalyzed = %d", r.FilesAnalyzed)
	}
	if len(r.Tasks) != 3 {
		t.Fatalf("Tasks len = %d", len(r.Tasks))
	}
	if r.Tasks[2].Text != "add tests" {
		t.Errorf("Tasks[2].Text = %q, want join with arceus.Task", r.Tasks[2].Text)
	}
	if r.Tasks[2].Phase != "Phase 1" {
		t.Errorf("Tasks[2].Phase = %q", r.Tasks[2].Phase)
	}
	if r.Criteria[1].Text != "exit 0" {
		t.Errorf("Criteria[1].Text = %q", r.Criteria[1].Text)
	}
}

func TestFrom_UnknownIndexPreserved(t *testing.T) {
	change, diff, out := sampleInputs()
	out.Tasks = append(out.Tasks, llm.TaskVerdict{Index: 99, Actual: "done"})
	r := From(change, diff, out)
	if got := r.Tasks[3].Text; got != "" {
		t.Errorf("expected empty Text for hallucinated index, got %q", got)
	}
	if r.Tasks[3].Index != 99 {
		t.Errorf("expected Index=99 to be preserved, got %d", r.Tasks[3].Index)
	}
}

func TestHasFindings(t *testing.T) {
	change, diff, out := sampleInputs()
	r := From(change, diff, out)
	if !r.HasFindings() {
		t.Errorf("should have findings (missing task, failed criterion, drift)")
	}

	// All-clean case
	clean := llm.Output{Verdict: llm.VerdictApprove}
	r2 := From(change, diff, clean)
	if r2.HasFindings() {
		t.Errorf("clean report should not have findings")
	}
}

func TestRenderMarkdown_ContainsAllSections(t *testing.T) {
	change, diff, out := sampleInputs()
	r := From(change, diff, out)
	md := RenderMarkdown(r)

	wantSubstrings := []string{
		"# Spec/Code Consistency Audit — sample-change",
		"**Verdict**: REQUEST_CHANGES",
		"**Model**: claude-opus-4-7",
		"**Files analyzed**: 2",
		"## Summary",
		"main.go implements hello",
		"## Task implementation (from tasks.md)",
		"| 1 | Phase 0 | init mod | [x] | done | go.mod |",
		"| 3 | Phase 1 | add tests | [ ] | missing | — |",
		"## Acceptance criteria (from spec.md)",
		"**PASS**: criterion 1 — prints hello",
		"**FAIL**: criterion 2 — exit 0",
		"## Drift findings",
		"**Undocumented additions**",
		"--debug flag",
		"## Open questions for human reviewer",
		"Should --debug be exposed",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n----\n%s", want, md)
		}
	}
}

func TestRenderMarkdown_EmptyTasksAndCriteria(t *testing.T) {
	change := &arceus.Change{ID: "empty", Meta: arceus.Meta{Status: "active"}}
	diff := &gitdiff.Diff{BaseRef: "main", HeadRef: "HEAD"}
	out := llm.Output{Verdict: llm.VerdictApprove, Summary: "nothing to do"}
	r := From(change, diff, out)
	md := RenderMarkdown(r)

	if !strings.Contains(md, "_No task verdicts returned._") {
		t.Errorf("expected empty-tasks placeholder")
	}
	if !strings.Contains(md, "_No acceptance criteria returned._") {
		t.Errorf("expected empty-criteria placeholder")
	}
	if !strings.Contains(md, "_No drift detected._") {
		t.Errorf("expected no-drift placeholder")
	}
}

func TestRenderMarkdown_PipeCharsEscaped(t *testing.T) {
	change := &arceus.Change{
		ID: "pipes", Meta: arceus.Meta{Status: "active"},
		ParsedTasks: []arceus.Task{{Index: 1, Text: "use foo | bar pipe"}},
	}
	diff := &gitdiff.Diff{BaseRef: "main", HeadRef: "HEAD"}
	out := llm.Output{Verdict: "APPROVE", Tasks: []llm.TaskVerdict{
		{Index: 1, Reported: true, Actual: "done"},
	}}
	r := From(change, diff, out)
	md := RenderMarkdown(r)
	if !strings.Contains(md, `use foo \| bar pipe`) {
		t.Errorf("pipe characters should be escaped in markdown table cells")
	}
}

func TestRenderJSON_MarshalsAndUnmarshals(t *testing.T) {
	change, diff, out := sampleInputs()
	r := From(change, diff, out)
	b, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !strings.Contains(string(b), `"verdict": "REQUEST_CHANGES"`) {
		t.Errorf("verdict not in JSON output:\n%s", b)
	}
	var got Report
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if got.Verdict != r.Verdict || len(got.Tasks) != len(r.Tasks) {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestShortSHA(t *testing.T) {
	if shortSHA("") != "—" {
		t.Errorf("empty SHA should render as em-dash")
	}
	if shortSHA("abc") != "abc" {
		t.Errorf("short SHA passthrough wrong")
	}
	if shortSHA("abcdef1234567890") != "abcdef1" {
		t.Errorf("long SHA truncate wrong")
	}
}
