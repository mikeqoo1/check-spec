// Package llm defines the Judge abstraction and its implementations.
//
// A Judge takes a system/user prompt pair, sends it to an LLM, and returns
// a structured Output describing the audit verdict.
//
// The package exposes two implementations:
//
//   - AnthropicJudge: calls the Anthropic Messages API via the official SDK.
//   - FakeJudge: returns a pre-canned Output, used by tests and golden files.
package llm

import "context"

// Segment is one chunk of the user-prompt that may opt into prompt caching.
//
// For AnthropicJudge, segments with Cache=true get cache_control: ephemeral
// attached so that subsequent calls (within the 5-minute TTL) avoid
// re-billing tokens for the static prefix.
type Segment struct {
	Text  string
	Cache bool
}

// Input is the full request payload to Judge.Verify.
//
// Provide either UserPrompt OR UserSegments. When both are set, UserSegments
// takes precedence — UserPrompt is treated as a convenience for the single-segment case.
type Input struct {
	Model        string // model ID, e.g. "claude-opus-4-7"
	MaxTokens    int    // defaults to 8192 when <= 0
	SystemPrompt string
	UserPrompt   string
	UserSegments []Segment
}

// Output is the parsed verdict produced by a Judge.
//
// Verdict is one of: APPROVE, REQUEST_CHANGES, NEEDS_DISCUSSION.
type Output struct {
	Verdict       string             `json:"verdict"`
	Summary       string             `json:"summary,omitempty"`
	Tasks         []TaskVerdict      `json:"tasks,omitempty"`
	Criteria      []CriterionVerdict `json:"criteria,omitempty"`
	Drift         Drift              `json:"drift,omitzero"`
	OpenQuestions []string           `json:"openQuestions,omitempty"`
	Model         string             `json:"model,omitempty"` // echoed back by the Judge
}

// TaskVerdict is the judge's per-task assessment against tasks.md.
//
// Actual is one of: "done", "partial", "missing".
type TaskVerdict struct {
	Index    int    `json:"index"`
	Reported bool   `json:"reported"`
	Actual   string `json:"actual"`
	Evidence string `json:"evidence,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// CriterionVerdict is the judge's per-acceptance-criterion assessment.
//
// Status is one of: "pass", "fail", "partial".
type CriterionVerdict struct {
	Index    int    `json:"index"`
	Status   string `json:"status"`
	Evidence string `json:"evidence,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// Drift captures anything in the diff that is not justified by spec, or
// any spec requirement that is not visible in the diff.
type Drift struct {
	UndocumentedAdditions []string `json:"undocumentedAdditions,omitempty"`
	MissingFromImpl       []string `json:"missingFromImpl,omitempty"`
}

// Judge runs a single audit pass.
type Judge interface {
	Verify(ctx context.Context, in Input) (Output, error)
}

// Valid verdict constants.
const (
	VerdictApprove         = "APPROVE"
	VerdictRequestChanges  = "REQUEST_CHANGES"
	VerdictNeedsDiscussion = "NEEDS_DISCUSSION"
)
