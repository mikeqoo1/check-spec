package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicJudge calls the Anthropic Messages API.
//
// Retries (429 / 5xx) are handled by the SDK via option.WithMaxRetries.
type AnthropicJudge struct {
	client anthropic.Client
}

// AnthropicConfig configures an AnthropicJudge.
//
// APIKey, when empty, falls back to ANTHROPIC_API_KEY (handled by the SDK).
// BaseURL, when non-empty, overrides the API endpoint — used in tests.
type AnthropicConfig struct {
	APIKey     string
	BaseURL    string
	MaxRetries int // 0 keeps SDK default
}

// NewAnthropicJudge constructs an AnthropicJudge.
func NewAnthropicJudge(cfg AnthropicConfig) *AnthropicJudge {
	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.MaxRetries > 0 {
		opts = append(opts, option.WithMaxRetries(cfg.MaxRetries))
	}
	return &AnthropicJudge{client: anthropic.NewClient(opts...)}
}

// Verify implements Judge.
func (j *AnthropicJudge) Verify(ctx context.Context, in Input) (Output, error) {
	if in.Model == "" {
		return Output{}, errors.New("Input.Model is required")
	}
	if in.SystemPrompt == "" {
		return Output{}, errors.New("Input.SystemPrompt is required")
	}
	if in.UserPrompt == "" && len(in.UserSegments) == 0 {
		return Output{}, errors.New("Input.UserPrompt or Input.UserSegments is required")
	}

	maxTokens := int64(in.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	params := anthropic.MessageNewParams{
		Model:     in.Model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: in.SystemPrompt}},
		Messages: []anthropic.MessageParam{
			{
				Role:    anthropic.MessageParamRoleUser,
				Content: buildUserBlocks(in),
			},
		},
	}

	resp, err := j.client.Messages.New(ctx, params)
	if err != nil {
		return Output{}, fmt.Errorf("anthropic: %w", err)
	}

	text := extractText(resp)
	out, err := ParseOutput(text)
	if err != nil {
		return Output{}, err
	}
	out.Model = in.Model
	return out, nil
}

func buildUserBlocks(in Input) []anthropic.ContentBlockParamUnion {
	if len(in.UserSegments) > 0 {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(in.UserSegments))
		for _, seg := range in.UserSegments {
			tb := anthropic.TextBlockParam{Text: seg.Text}
			if seg.Cache {
				tb.CacheControl = anthropic.CacheControlEphemeralParam{
					TTL: anthropic.CacheControlEphemeralTTLTTL5m,
				}
			}
			blocks = append(blocks, anthropic.ContentBlockParamUnion{OfText: &tb})
		}
		return blocks
	}
	return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(in.UserPrompt)}
}

func extractText(m *anthropic.Message) string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// ParseOutput parses the LLM's text response into an Output struct.
//
// It tolerates two common variants of "return JSON" disobedience:
//  1. The model wraps JSON in a ```json ... ``` code fence.
//  2. The model prefixes the JSON with a sentence or two of prose.
//
// Exported for use by the fake judge and tests.
func ParseOutput(text string) (Output, error) {
	jsonText, err := extractJSONObject(text)
	if err != nil {
		return Output{}, fmt.Errorf("extract JSON: %w", err)
	}
	var out Output
	dec := json.NewDecoder(strings.NewReader(jsonText))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		// Retry once with unknown fields allowed — model may have hallucinated
		// extra keys but the core verdict can still be useful.
		if uerr := json.Unmarshal([]byte(jsonText), &out); uerr != nil {
			return Output{}, fmt.Errorf("unmarshal: %w (raw head: %s)", uerr, truncateForError(jsonText))
		}
	}
	if out.Verdict == "" {
		return Output{}, errors.New("verdict field missing or empty")
	}
	if !isValidVerdict(out.Verdict) {
		return Output{}, fmt.Errorf("invalid verdict %q (want APPROVE | REQUEST_CHANGES | NEEDS_DISCUSSION)", out.Verdict)
	}
	return out, nil
}

func extractJSONObject(text string) (string, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || start >= end {
		return "", fmt.Errorf("no JSON object found in %d-byte response", len(text))
	}
	return text[start : end+1], nil
}

func isValidVerdict(v string) bool {
	switch v {
	case VerdictApprove, VerdictRequestChanges, VerdictNeedsDiscussion:
		return true
	}
	return false
}

func truncateForError(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
