// Package cli wires the Cobra commands and is the only place that
// composes the loader / git collector / LLM judge / report renderer.
package cli

import (
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/mikeqoo1/check-spec/internal/llm"
)

// Exit codes documented in spec.md:
//
//	0  audit passed — verdict APPROVE with no findings
//	1  audit found drift / unimplemented tasks / failing criteria
//	2  execution error (invalid args, missing files, LLM error, etc.)
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitError    = 2
)

// App holds the wiring overrides used in tests.
//
// Production code calls NewDefault(). Tests can inject a fake Judge or
// override stdout/stderr.
type App struct {
	JudgeFactory func(model string) (llm.Judge, error)
	Stdout       io.Writer
	Stderr       io.Writer
	NowNanoSeed  int64 // reserved for future deterministic time use
}

// NewDefault returns an App wired to real Anthropic SDK and OS streams.
func NewDefault() *App {
	return &App{
		JudgeFactory: defaultJudgeFactory,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}
}

// Execute parses os.Args and runs the matching command, returning a
// process exit code.
func Execute() int {
	app := NewDefault()
	return app.Run(os.Args[1:])
}

// Run runs the CLI with the given arguments. Exported for E2E tests.
func (a *App) Run(args []string) int {
	cmd := a.newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(a.Stdout)
	cmd.SetErr(a.Stderr)
	if err := cmd.Execute(); err != nil {
		// Special: verify returns ErrFindings to signal exit 1 distinctly.
		if err == errFindings {
			return ExitFindings
		}
		slog.Error("command failed", "err", err)
		return ExitError
	}
	return ExitOK
}

func (a *App) newRootCmd() *cobra.Command {
	var verbose bool
	root := &cobra.Command{
		Use:           "check-spec",
		Short:         "Audit Arceus-generated code against its spec",
		Long:          "check-spec compares an Arceus .arceus/changes/<id>/ proposal against the git diff implementing it, using an LLM as a third-party judge.",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			lvl := slog.LevelInfo
			if verbose {
				lvl = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(a.Stderr, &slog.HandlerOptions{Level: lvl})))
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	root.AddCommand(a.newVerifyCmd())
	root.AddCommand(a.newListCmd())
	root.AddCommand(a.newVersionCmd())
	return root
}
