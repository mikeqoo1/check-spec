// Package prompt assembles the system+user prompt for the audit Judge.
//
// The user prompt is split into two segments so that Anthropic prompt
// caching can amortize the proposal/spec prefix across multiple verify
// calls within the 5-minute cache TTL:
//
//	segment 0 (cached):     proposal.md + spec.md
//	segment 1 (not cached): tasks.md + decisions.md + git diff
package prompt

import (
	"fmt"
	"strings"

	"github.com/mikeqoo1/check-spec/internal/arceus"
	"github.com/mikeqoo1/check-spec/internal/gitdiff"
	"github.com/mikeqoo1/check-spec/internal/llm"
)

// SystemPrompt is the role + schema + verdict-rules instruction sent as
// the system role. Backticks keep the JSON schema readable.
const SystemPrompt = `You are an independent third-party software-engineering auditor.

Your single job: given a change proposal (proposal.md, spec.md, tasks.md, decisions.md) and the git diff supposedly produced from it, decide whether the implementation matches what was promised.

You do NOT recommend refactors or suggest improvements. You judge consistency only.

You MUST respond with a single JSON object. No prose. No code fences. No leading commentary. The schema is:

{
  "verdict":   "APPROVE" | "REQUEST_CHANGES" | "NEEDS_DISCUSSION",
  "summary":   "one-paragraph plain-English summary of what was implemented vs promised",
  "tasks":     [ { "index": <1-based int matching tasks_indexed>, "reported": <bool — checkbox state in tasks.md>, "actual": "done" | "partial" | "missing", "evidence": "<file:line or short note>", "notes": "<optional>" } ],
  "criteria":  [ { "index": <1-based int matching acceptance_criteria_indexed>, "status": "pass" | "fail" | "partial", "evidence": "<file:line>", "notes": "<optional>" } ],
  "drift":     { "undocumentedAdditions": [ "<short description with file path>" ], "missingFromImpl": [ "<short description>" ] },
  "openQuestions": [ "<question the human reviewer should answer>" ]
}

Verdict rules:
- APPROVE: every listed task is "done", every listed criterion is "pass", drift lists are empty or trivially small (e.g. README touch-up only).
- REQUEST_CHANGES: any task is "missing" or "partial", any criterion is "fail" or "partial", or non-trivial drift exists.
- NEEDS_DISCUSSION: the spec is too ambiguous to judge, evidence is insufficient (e.g. diff was heavily truncated), or you cannot reasonably decide without human input.

A checkbox marked [x] in tasks.md is the self-report of the implementer; your role is to verify it against the diff, not to trust it. If the diff is empty for a task that was marked [x], call it "missing".

If a criterion or task is partially satisfied, prefer "partial" over forcing a binary call — and cite the exact gap in "notes".`

// Build assembles the user prompt as cache-aware segments.
//
// The system prompt is constant and returned for convenience.
func Build(change *arceus.Change, diff *gitdiff.Diff) (system string, segments []llm.Segment) {
	staticPart := renderProposal(change) + renderSpec(change)
	dynamicPart := renderTasks(change) + renderDecisions(change) + renderDiff(diff)
	return SystemPrompt, []llm.Segment{
		{Text: staticPart, Cache: true},
		{Text: dynamicPart, Cache: false},
	}
}

func renderProposal(c *arceus.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<change_id>%s</change_id>\n", c.ID)
	fmt.Fprintf(&b, "<change_title>%s</change_title>\n", c.Title)
	fmt.Fprintf(&b, "<change_status>%s</change_status>\n\n", c.Meta.Status)
	fmt.Fprintf(&b, "<proposal>\n%s\n</proposal>\n\n", strings.TrimSpace(c.Proposal))
	return b.String()
}

func renderSpec(c *arceus.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<spec>\n%s\n</spec>\n\n", strings.TrimSpace(c.Spec))
	if len(c.ParsedAcceptanceCriteria) > 0 {
		b.WriteString("<acceptance_criteria_indexed>\n")
		for _, ac := range c.ParsedAcceptanceCriteria {
			fmt.Fprintf(&b, "  %d. [%s] %s\n", ac.Index, checkMark(ac.Checked), ac.Text)
		}
		b.WriteString("</acceptance_criteria_indexed>\n\n")
	}
	return b.String()
}

func renderTasks(c *arceus.Change) string {
	var b strings.Builder
	b.WriteString("<tasks_md>\n")
	b.WriteString(strings.TrimSpace(c.Tasks))
	b.WriteString("\n</tasks_md>\n\n<tasks_indexed>\n")
	for _, t := range c.ParsedTasks {
		phase := t.Phase
		if phase == "" {
			phase = "(no phase)"
		}
		fmt.Fprintf(&b, "  %d. [%s] [%s] %s\n", t.Index, checkMark(t.Checked), phase, t.Text)
	}
	b.WriteString("</tasks_indexed>\n\n")
	return b.String()
}

func renderDecisions(c *arceus.Change) string {
	return fmt.Sprintf("<decisions>\n%s\n</decisions>\n\n", strings.TrimSpace(c.Decisions))
}

func renderDiff(d *gitdiff.Diff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<git_diff base=%q head=%q base_sha=%q head_sha=%q>\n",
		d.BaseRef, d.HeadRef, d.BaseSHA, d.HeadSHA)
	if d.Truncated {
		b.WriteString("(NOTE: diff was truncated for size; some files are omitted below.)\n\n")
	}
	for _, f := range d.Files {
		fmt.Fprintf(&b, "--- file: %s (status=%s, +%d/-%d, binary=%t, omitted=%t)\n",
			f.Path, f.Status, f.Added, f.Deleted, f.Binary, f.Omitted)
		switch {
		case f.Binary:
			b.WriteString("(binary file — content not shown)\n\n")
		case f.Omitted:
			b.WriteString("(file content omitted due to truncation budget)\n\n")
		default:
			b.WriteString(f.Diff)
			if !strings.HasSuffix(f.Diff, "\n") {
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("</git_diff>\n")
	return b.String()
}

func checkMark(checked bool) string {
	if checked {
		return "x"
	}
	return " "
}
