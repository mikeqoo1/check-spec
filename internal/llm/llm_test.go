package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseOutput_RawJSON(t *testing.T) {
	text := `{"verdict":"APPROVE","summary":"all good"}`
	out, err := ParseOutput(text)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if out.Verdict != VerdictApprove {
		t.Errorf("Verdict = %q", out.Verdict)
	}
	if out.Summary != "all good" {
		t.Errorf("Summary = %q", out.Summary)
	}
}

func TestParseOutput_CodeFence(t *testing.T) {
	text := "```json\n{\"verdict\":\"REQUEST_CHANGES\"}\n```"
	out, err := ParseOutput(text)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if out.Verdict != VerdictRequestChanges {
		t.Errorf("Verdict = %q", out.Verdict)
	}
}

func TestParseOutput_BareFence(t *testing.T) {
	text := "```\n{\"verdict\":\"NEEDS_DISCUSSION\"}\n```"
	out, err := ParseOutput(text)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if out.Verdict != VerdictNeedsDiscussion {
		t.Errorf("Verdict = %q", out.Verdict)
	}
}

func TestParseOutput_ProsePrefix(t *testing.T) {
	text := "Sure, here is my verdict:\n{\"verdict\":\"APPROVE\",\"summary\":\"ok\"}"
	out, err := ParseOutput(text)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if out.Verdict != VerdictApprove {
		t.Errorf("Verdict = %q", out.Verdict)
	}
}

func TestParseOutput_FullSchema(t *testing.T) {
	text := `{
		"verdict": "REQUEST_CHANGES",
		"summary": "some criteria not met",
		"tasks": [
			{"index": 1, "reported": true, "actual": "done", "evidence": "main.go:10"},
			{"index": 2, "reported": true, "actual": "partial", "notes": "missing tests"}
		],
		"criteria": [
			{"index": 1, "status": "pass", "evidence": "see main.go"},
			{"index": 2, "status": "fail", "notes": "no edge case"}
		],
		"drift": {
			"undocumentedAdditions": ["added /health endpoint"],
			"missingFromImpl": ["spec required X"]
		},
		"openQuestions": ["should /health be authenticated?"]
	}`
	out, err := ParseOutput(text)
	if err != nil {
		t.Fatalf("ParseOutput: %v", err)
	}
	if len(out.Tasks) != 2 {
		t.Errorf("Tasks count = %d", len(out.Tasks))
	}
	if len(out.Criteria) != 2 || out.Criteria[1].Status != "fail" {
		t.Errorf("Criteria wrong: %+v", out.Criteria)
	}
	if len(out.Drift.UndocumentedAdditions) != 1 {
		t.Errorf("Drift wrong: %+v", out.Drift)
	}
	if len(out.OpenQuestions) != 1 {
		t.Errorf("OpenQuestions wrong: %v", out.OpenQuestions)
	}
}

func TestParseOutput_MissingVerdict(t *testing.T) {
	_, err := ParseOutput(`{"summary":"hi"}`)
	if err == nil {
		t.Fatal("expected error for missing verdict")
	}
}

func TestParseOutput_InvalidVerdict(t *testing.T) {
	_, err := ParseOutput(`{"verdict":"LGTM"}`)
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
}

func TestParseOutput_NoJSON(t *testing.T) {
	_, err := ParseOutput("I refuse to answer.")
	if err == nil {
		t.Fatal("expected error for non-JSON response")
	}
}

func TestParseOutput_Malformed(t *testing.T) {
	_, err := ParseOutput(`{"verdict": invalid`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestFakeJudge_Verify(t *testing.T) {
	want := Output{Verdict: VerdictApprove, Summary: "fake"}
	j := &FakeJudge{Output: want}
	got, err := j.Verify(context.Background(), Input{Model: "test"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Verdict != want.Verdict || got.Summary != want.Summary {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.Model != "test" {
		t.Errorf("Model should echo input, got %q", got.Model)
	}
	if j.LastInput.Model != "test" {
		t.Errorf("LastInput not captured: %+v", j.LastInput)
	}
}

func TestFakeJudge_Error(t *testing.T) {
	j := &FakeJudge{Err: errors.New("boom")}
	_, err := j.Verify(context.Background(), Input{Model: "test"})
	if err == nil || err.Error() != "boom" {
		t.Errorf("err = %v", err)
	}
}

func TestFakeJudge_MissingVerdict(t *testing.T) {
	j := &FakeJudge{Output: Output{Summary: "no verdict"}}
	_, err := j.Verify(context.Background(), Input{Model: "test"})
	if err == nil {
		t.Fatal("expected error for missing verdict")
	}
}

func TestAnthropicJudge_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body to ensure no panic
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-7",
			"content":[{"type":"text","text":"{\"verdict\":\"APPROVE\",\"summary\":\"all good\"}","citations":[]}],
			"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"server_tool_use":{"web_search_requests":0}},
			"container":null
		}`)
	}))
	defer server.Close()

	j := NewAnthropicJudge(AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	out, err := j.Verify(context.Background(), Input{
		Model:        "claude-opus-4-7",
		SystemPrompt: "you are a judge",
		UserPrompt:   "audit this",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Verdict != VerdictApprove {
		t.Errorf("Verdict = %q", out.Verdict)
	}
	if out.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", out.Model)
	}
}

func TestAnthropicJudge_RequiredFields(t *testing.T) {
	j := NewAnthropicJudge(AnthropicConfig{APIKey: "k", BaseURL: "http://example.invalid"})
	cases := []struct {
		name string
		in   Input
	}{
		{"missing model", Input{SystemPrompt: "s", UserPrompt: "u"}},
		{"missing system", Input{Model: "m", UserPrompt: "u"}},
		{"missing user", Input{Model: "m", SystemPrompt: "s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := j.Verify(context.Background(), tc.in); err == nil {
				t.Errorf("expected validation error")
			}
		})
	}
}

func TestAnthropicJudge_UserSegmentsBuildCachedBlocks(t *testing.T) {
	in := Input{
		Model:        "m",
		SystemPrompt: "s",
		UserSegments: []Segment{
			{Text: "first (cached)", Cache: true},
			{Text: "second", Cache: false},
		},
	}
	blocks := buildUserBlocks(in)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if blocks[0].OfText == nil || blocks[0].OfText.Text != "first (cached)" {
		t.Errorf("block[0] text wrong: %+v", blocks[0].OfText)
	}
	if blocks[1].OfText == nil || blocks[1].OfText.Text != "second" {
		t.Errorf("block[1] text wrong: %+v", blocks[1].OfText)
	}

	// Verify cache_control is marshalled only for the cached block.
	j0, err := blocks[0].OfText.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(j0), "cache_control") {
		t.Errorf("block[0] should marshal cache_control, got %s", j0)
	}
	j1, err := blocks[1].OfText.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(j1), "cache_control") {
		t.Errorf("block[1] should NOT marshal cache_control, got %s", j1)
	}
}
