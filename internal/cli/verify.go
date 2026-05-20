package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mikeqoo1/check-spec/internal/arceus"
	"github.com/mikeqoo1/check-spec/internal/gitdiff"
	"github.com/mikeqoo1/check-spec/internal/llm"
	"github.com/mikeqoo1/check-spec/internal/prompt"
	"github.com/mikeqoo1/check-spec/internal/report"
)

// errFindings is returned from RunE when the audit produced actionable
// findings. The root Execute path translates this into ExitFindings without
// printing it as an error.
var errFindings = errors.New("audit findings")

type verifyOpts struct {
	change       string
	base         string
	head         string
	repo         string
	output       string
	format       string
	model        string
	maxDiffBytes int
}

func defaultJudgeFactory(_ string) (llm.Judge, error) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is required (set the env var or use --help for details)")
	}
	return llm.NewAnthropicJudge(llm.AnthropicConfig{
		APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		MaxRetries: 3,
	}), nil
}

func (a *App) newVerifyCmd() *cobra.Command {
	opts := &verifyOpts{}
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Audit a change's implementation against its spec",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runVerify(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.change, "change", "", "change id under .arceus/changes/ (required)")
	cmd.Flags().StringVar(&opts.base, "base", "origin/main", "base git ref for the diff")
	cmd.Flags().StringVar(&opts.head, "head", "HEAD", "head git ref for the diff")
	cmd.Flags().StringVar(&opts.repo, "repo", ".", "path to the repository root")
	cmd.Flags().StringVar(&opts.output, "output", "", "write report to this file (default stdout)")
	cmd.Flags().StringVar(&opts.format, "format", "markdown", "output format: markdown or json")
	cmd.Flags().StringVar(&opts.model, "model", "claude-opus-4-7", "LLM model id")
	cmd.Flags().IntVar(&opts.maxDiffBytes, "max-diff-bytes", gitdiff.DefaultMaxBytes, "soft cap on concatenated diff bytes sent to the LLM")
	_ = cmd.MarkFlagRequired("change")
	return cmd
}

func (a *App) runVerify(ctx context.Context, opts *verifyOpts) error {
	if opts.format != "markdown" && opts.format != "json" {
		return fmt.Errorf("--format must be 'markdown' or 'json', got %q", opts.format)
	}

	slog.Debug("loading change", "id", opts.change, "repo", opts.repo)
	change, err := arceus.Load(opts.repo, opts.change)
	if err != nil {
		if errors.Is(err, arceus.ErrChangeNotFound) {
			available, _ := arceus.List(opts.repo)
			if len(available) == 0 {
				return fmt.Errorf("%w (no changes under %s/.arceus/changes/)", err, opts.repo)
			}
			return fmt.Errorf("%w (available: %s)", err, strings.Join(available, ", "))
		}
		return err
	}

	slog.Debug("collecting diff", "base", opts.base, "head", opts.head)
	collector := gitdiff.New(opts.repo)
	diff, err := collector.Collect(ctx, opts.base, opts.head, opts.maxDiffBytes)
	if err != nil {
		return fmt.Errorf("collect diff: %w", err)
	}
	slog.Info("diff collected",
		"files", len(diff.Files), "truncated", diff.Truncated,
		"base", diff.BaseSHA[:min(7, len(diff.BaseSHA))],
		"head", diff.HeadSHA[:min(7, len(diff.HeadSHA))],
	)

	judge, err := a.JudgeFactory(opts.model)
	if err != nil {
		return err
	}

	sys, segs := prompt.Build(change, &diff)
	slog.Debug("dispatching to judge", "model", opts.model, "segments", len(segs))
	out, err := judge.Verify(ctx, llm.Input{
		Model:        opts.model,
		SystemPrompt: sys,
		UserSegments: segs,
	})
	if err != nil {
		return fmt.Errorf("judge: %w", err)
	}

	rep := report.From(change, &diff, out)
	if err := a.writeReport(rep, opts); err != nil {
		return err
	}
	slog.Info("audit complete", "verdict", rep.Verdict, "findings", rep.HasFindings())

	if rep.Verdict != llm.VerdictApprove || rep.HasFindings() {
		return errFindings
	}
	return nil
}

func (a *App) writeReport(r report.Report, opts *verifyOpts) error {
	var payload []byte
	var err error
	if opts.format == "json" {
		payload, err = report.RenderJSON(r)
		if err != nil {
			return fmt.Errorf("render json: %w", err)
		}
		payload = append(payload, '\n')
	} else {
		payload = []byte(report.RenderMarkdown(r))
	}

	if opts.output == "" {
		_, err = a.Stdout.Write(payload)
		return err
	}
	if err := os.WriteFile(opts.output, payload, 0o644); err != nil { //nolint:gosec // audit report is user-readable by intent
		return fmt.Errorf("write %s: %w", opts.output, err)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
