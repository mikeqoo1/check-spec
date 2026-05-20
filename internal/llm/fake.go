package llm

import (
	"context"
	"errors"
)

// FakeJudge returns a fixed Output, regardless of input.
//
// Used by golden-file E2E tests and any caller that needs deterministic
// behavior. When Err is non-nil, Verify returns it without consulting Output.
type FakeJudge struct {
	Output Output
	Err    error

	// LastInput records the most recent Verify input — useful for prompt-shape tests.
	LastInput Input
}

// Verify implements Judge.
func (f *FakeJudge) Verify(_ context.Context, in Input) (Output, error) {
	f.LastInput = in
	if f.Err != nil {
		return Output{}, f.Err
	}
	out := f.Output
	if out.Verdict == "" {
		return Output{}, errors.New("FakeJudge.Output.Verdict is required")
	}
	if out.Model == "" {
		out.Model = in.Model
	}
	return out, nil
}
