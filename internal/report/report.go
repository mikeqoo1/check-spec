// Package report combines the LLM verdict with surrounding context
// (change metadata, diff stats, model id) and renders it to markdown or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikeqoo1/check-spec/internal/arceus"
	"github.com/mikeqoo1/check-spec/internal/gitdiff"
	"github.com/mikeqoo1/check-spec/internal/llm"
)

// Report is the audit document — the union of LLM judgement and the
// metadata needed to reproduce / interpret it.
type Report struct {
	ChangeID      string         `json:"changeId"`
	ChangeTitle   string         `json:"changeTitle"`
	ChangeStatus  string         `json:"changeStatus,omitempty"`
	BaseRef       string         `json:"baseRef"`
	HeadRef       string         `json:"headRef"`
	BaseSHA       string         `json:"baseSHA"`
	HeadSHA       string         `json:"headSHA"`
	Model         string         `json:"model"`
	FilesAnalyzed int            `json:"filesAnalyzed"`
	DiffTruncated bool           `json:"diffTruncated,omitempty"`
	Verdict       string         `json:"verdict"`
	Summary       string         `json:"summary,omitempty"`
	Tasks         []TaskRow      `json:"tasks,omitempty"`
	Criteria      []CriterionRow `json:"criteria,omitempty"`
	Drift         llm.Drift      `json:"drift,omitzero"`
	OpenQuestions []string       `json:"openQuestions,omitempty"`
}

// TaskRow joins one llm.TaskVerdict with the originating arceus.Task text.
type TaskRow struct {
	Index    int    `json:"index"`
	Phase    string `json:"phase,omitempty"`
	Text     string `json:"text"`
	Reported bool   `json:"reported"`
	Actual   string `json:"actual"`
	Evidence string `json:"evidence,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// CriterionRow joins one llm.CriterionVerdict with its original criterion text.
type CriterionRow struct {
	Index    int    `json:"index"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Evidence string `json:"evidence,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// From combines a Change, a Diff, and an LLM Output into a Report.
//
// Task and criterion rows are joined by index. If the LLM returned a verdict
// for an index not present in the source change, that verdict is appended
// with empty Text — this signals likely model hallucination, but the data is
// preserved for human review.
func From(change *arceus.Change, diff *gitdiff.Diff, out llm.Output) Report {
	r := Report{
		ChangeID:      change.ID,
		ChangeTitle:   change.Title,
		ChangeStatus:  change.Meta.Status,
		BaseRef:       diff.BaseRef,
		HeadRef:       diff.HeadRef,
		BaseSHA:       diff.BaseSHA,
		HeadSHA:       diff.HeadSHA,
		Model:         out.Model,
		FilesAnalyzed: len(diff.Files),
		DiffTruncated: diff.Truncated,
		Verdict:       out.Verdict,
		Summary:       out.Summary,
		Drift:         out.Drift,
		OpenQuestions: out.OpenQuestions,
	}

	tasksByIdx := make(map[int]arceus.Task, len(change.ParsedTasks))
	for _, t := range change.ParsedTasks {
		tasksByIdx[t.Index] = t
	}
	for _, v := range out.Tasks {
		src := tasksByIdx[v.Index]
		r.Tasks = append(r.Tasks, TaskRow{
			Index: v.Index, Phase: src.Phase, Text: src.Text,
			Reported: v.Reported, Actual: v.Actual,
			Evidence: v.Evidence, Notes: v.Notes,
		})
	}

	critByIdx := make(map[int]arceus.AcceptanceCriterion, len(change.ParsedAcceptanceCriteria))
	for _, c := range change.ParsedAcceptanceCriteria {
		critByIdx[c.Index] = c
	}
	for _, v := range out.Criteria {
		src := critByIdx[v.Index]
		r.Criteria = append(r.Criteria, CriterionRow{
			Index: v.Index, Text: src.Text, Status: v.Status,
			Evidence: v.Evidence, Notes: v.Notes,
		})
	}
	return r
}

// HasFindings reports whether the report contains anything actionable for a reviewer
// (missing/partial tasks, fail/partial criteria, drift). Used by the CLI to derive
// exit codes.
func (r Report) HasFindings() bool {
	for _, t := range r.Tasks {
		if t.Actual != "done" {
			return true
		}
	}
	for _, c := range r.Criteria {
		if c.Status != "pass" {
			return true
		}
	}
	if len(r.Drift.UndocumentedAdditions) > 0 || len(r.Drift.MissingFromImpl) > 0 {
		return true
	}
	return false
}

// RenderMarkdown renders the report as a CommonMark document.
func RenderMarkdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Spec/Code Consistency Audit — %s\n\n", r.ChangeID)
	if r.ChangeTitle != "" {
		fmt.Fprintf(&b, "_%s_\n\n", r.ChangeTitle)
	}
	fmt.Fprintf(&b, "- **Verdict**: %s\n", r.Verdict)
	fmt.Fprintf(&b, "- **Model**: %s\n", emptyDash(r.Model))
	fmt.Fprintf(&b, "- **Base → Head**: %s (%s) → %s (%s)\n",
		r.BaseRef, shortSHA(r.BaseSHA), r.HeadRef, shortSHA(r.HeadSHA))
	fmt.Fprintf(&b, "- **Files analyzed**: %d\n", r.FilesAnalyzed)
	if r.DiffTruncated {
		b.WriteString("- **Note**: diff was truncated; some files omitted from prompt.\n")
	}
	b.WriteString("\n")

	if r.Summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(strings.TrimSpace(r.Summary))
		b.WriteString("\n\n")
	}

	b.WriteString("## Task implementation (from tasks.md)\n\n")
	if len(r.Tasks) == 0 {
		b.WriteString("_No task verdicts returned._\n\n")
	} else {
		b.WriteString("| # | Phase | Task | Reported | Actual | Evidence |\n")
		b.WriteString("|---|-------|------|----------|--------|----------|\n")
		for _, t := range r.Tasks {
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s |\n",
				t.Index,
				mdCell(t.Phase),
				mdCell(t.Text),
				reportedMark(t.Reported),
				mdCell(t.Actual),
				mdCell(t.Evidence),
			)
			if t.Notes != "" {
				fmt.Fprintf(&b, "|   |   |   |   | **notes** | %s |\n", mdCell(t.Notes))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Acceptance criteria (from spec.md)\n\n")
	if len(r.Criteria) == 0 {
		b.WriteString("_No acceptance criteria returned._\n\n")
	} else {
		for _, c := range r.Criteria {
			fmt.Fprintf(&b, "- **%s**: criterion %d — %s\n", strings.ToUpper(c.Status), c.Index, c.Text)
			if c.Evidence != "" {
				fmt.Fprintf(&b, "  - evidence: %s\n", c.Evidence)
			}
			if c.Notes != "" {
				fmt.Fprintf(&b, "  - notes: %s\n", c.Notes)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Drift findings\n\n")
	if len(r.Drift.UndocumentedAdditions) == 0 && len(r.Drift.MissingFromImpl) == 0 {
		b.WriteString("_No drift detected._\n\n")
	} else {
		if len(r.Drift.UndocumentedAdditions) > 0 {
			b.WriteString("**Undocumented additions** (in diff, not in spec):\n\n")
			for _, item := range r.Drift.UndocumentedAdditions {
				fmt.Fprintf(&b, "- %s\n", item)
			}
			b.WriteString("\n")
		}
		if len(r.Drift.MissingFromImpl) > 0 {
			b.WriteString("**Missing from implementation** (in spec, not in diff):\n\n")
			for _, item := range r.Drift.MissingFromImpl {
				fmt.Fprintf(&b, "- %s\n", item)
			}
			b.WriteString("\n")
		}
	}

	if len(r.OpenQuestions) > 0 {
		b.WriteString("## Open questions for human reviewer\n\n")
		for _, q := range r.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// RenderJSON marshals the report as pretty-printed JSON.
func RenderJSON(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "—"
	}
	return s
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func reportedMark(b bool) string {
	if b {
		return "[x]"
	}
	return "[ ]"
}

func shortSHA(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	if s == "" {
		return "—"
	}
	return s
}
