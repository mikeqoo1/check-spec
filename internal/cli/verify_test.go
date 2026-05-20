package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mikeqoo1/check-spec/internal/llm"
)

// fixtureChange writes a complete .arceus/changes/<id>/ skeleton into repo.
func fixtureChange(t *testing.T, repo, id string) {
	t.Helper()
	dir := filepath.Join(repo, ".arceus", "changes", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("meta.json", `{
  "id": "`+id+`",
  "title": "Add hello world",
  "status": "active",
  "createdAt": "2026-05-20T00:00:00.000Z",
  "updatedAt": "2026-05-20T00:00:00.000Z"
}`)
	write("proposal.md", "# Add hello world\n\nDemo change.\n")
	write("spec.md", `# Spec

## 需求描述

Print "hello".

## 驗收條件

- [ ] Running prints "hello"
- [ ] Exit code is 0
`)
	write("tasks.md", `# Tasks

## Phase 0 — Bootstrap

- [x] Add main.go
- [x] go mod init
`)
	write("decisions.md", "# Decisions\n\n(none)\n")
}

// fixtureGitRepo creates a temp git repo with two commits — base (empty) and head (adds main.go).
func fixtureGitRepo(t *testing.T) (repo, base, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	writeRepoFile := func(p, c string) {
		full := filepath.Join(repo, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runGit("init", "-q", "-b", "main")
	writeRepoFile("README.md", "# initial\n")
	runGit("add", "README.md")
	runGit("commit", "-q", "-m", "base")
	base = strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))

	writeRepoFile("main.go", "package main\n\nfunc main() { println(\"hello\") }\n")
	runGit("add", "main.go")
	runGit("commit", "-q", "-m", "add hello")
	head = strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	return
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func newAppWithFakeJudge(out llm.Output, stdout, stderr *bytes.Buffer) *App {
	return &App{
		JudgeFactory: func(_ string) (llm.Judge, error) {
			return &llm.FakeJudge{Output: out}, nil
		},
		Stdout: stdout,
		Stderr: stderr,
	}
}

// sanitizeReport replaces refs/SHAs with placeholders for stable comparison.
func sanitizeReport(s string) string {
	s = regexp.MustCompile(`\([0-9a-f]{7,40}\)`).ReplaceAllString(s, "(<SHA>)")
	s = regexp.MustCompile(`"baseSHA": "[0-9a-f]+"`).ReplaceAllString(s, `"baseSHA": "<SHA>"`)
	s = regexp.MustCompile(`"headSHA": "[0-9a-f]+"`).ReplaceAllString(s, `"headSHA": "<SHA>"`)
	return s
}

func TestVerify_E2E_PassExit0(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{
		Verdict: llm.VerdictApprove,
		Summary: "all good",
		Tasks: []llm.TaskVerdict{
			{Index: 1, Reported: true, Actual: "done", Evidence: "main.go:3"},
			{Index: 2, Reported: true, Actual: "done", Evidence: "go.mod (assumed via repo init)"},
		},
		Criteria: []llm.CriterionVerdict{
			{Index: 1, Status: "pass", Evidence: "main.go:3"},
			{Index: 2, Status: "pass", Evidence: "implicit"},
		},
		Model: "fake-model",
	}, stdout, stderr)

	code := app.Run([]string{"verify",
		"--change", "test-change",
		"--repo", repo,
		"--base", base,
		"--head", head,
		"--model", "fake-model",
	})
	if code != ExitOK {
		t.Errorf("exit code = %d, want %d (ExitOK); stderr:\n%s", code, ExitOK, stderr)
	}
	clean := sanitizeReport(stdout.String())
	for _, want := range []string{
		"# Spec/Code Consistency Audit — test-change",
		"**Verdict**: APPROVE",
		"**Model**: fake-model",
		"_No drift detected._",
	} {
		if !strings.Contains(clean, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestVerify_E2E_DriftExit1(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{
		Verdict: llm.VerdictRequestChanges,
		Summary: "exit code untested",
		Tasks: []llm.TaskVerdict{
			{Index: 1, Reported: true, Actual: "done"},
			{Index: 2, Reported: true, Actual: "partial", Notes: "go mod init not committed"},
		},
		Criteria: []llm.CriterionVerdict{
			{Index: 1, Status: "pass"},
			{Index: 2, Status: "fail", Notes: "no test verifies exit code"},
		},
		Drift: llm.Drift{
			UndocumentedAdditions: []string{"README.md was modified (out of scope)"},
		},
		Model: "fake-model",
	}, stdout, stderr)

	code := app.Run([]string{"verify",
		"--change", "test-change",
		"--repo", repo,
		"--base", base,
		"--head", head,
	})
	if code != ExitFindings {
		t.Errorf("exit code = %d, want %d (ExitFindings); stderr:\n%s", code, ExitFindings, stderr)
	}
	clean := sanitizeReport(stdout.String())
	if !strings.Contains(clean, "**Verdict**: REQUEST_CHANGES") {
		t.Errorf("verdict line missing in output")
	}
	if !strings.Contains(clean, "**Undocumented additions**") {
		t.Errorf("drift section missing")
	}
}

func TestVerify_E2E_ChangeNotFound(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "real-change")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{Verdict: llm.VerdictApprove}, stdout, stderr)

	code := app.Run([]string{"verify",
		"--change", "ghost-change",
		"--repo", repo,
		"--base", base,
		"--head", head,
	})
	if code != ExitError {
		t.Errorf("exit code = %d, want %d (ExitError)", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "real-change") {
		t.Errorf("expected stderr to list available changes, got:\n%s", stderr)
	}
}

func TestVerify_E2E_JSONFormat(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{
		Verdict: llm.VerdictApprove, Summary: "ok",
		Tasks:    []llm.TaskVerdict{{Index: 1, Reported: true, Actual: "done"}},
		Criteria: []llm.CriterionVerdict{{Index: 1, Status: "pass"}},
		Model:    "fake-model",
	}, stdout, stderr)

	app.Run([]string{"verify",
		"--change", "test-change",
		"--repo", repo,
		"--base", base, "--head", head,
		"--format", "json",
	})

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("JSON output not valid: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, stdout)
	}
	if got["verdict"] != "APPROVE" {
		t.Errorf("verdict = %v", got["verdict"])
	}
	if got["changeId"] != "test-change" {
		t.Errorf("changeId = %v", got["changeId"])
	}
}

func TestVerify_E2E_OutputFile(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")
	outFile := filepath.Join(t.TempDir(), "audit.md")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{Verdict: llm.VerdictApprove, Model: "fake"}, stdout, stderr)
	app.Run([]string{"verify",
		"--change", "test-change",
		"--repo", repo,
		"--base", base, "--head", head,
		"--output", outFile,
	})
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty when --output is set, got %d bytes", stdout.Len())
	}
	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("APPROVE")) {
		t.Errorf("output file does not contain verdict")
	}
}

func TestVerify_E2E_InvalidFormat(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := newAppWithFakeJudge(llm.Output{Verdict: llm.VerdictApprove}, stdout, stderr)
	code := app.Run([]string{"verify",
		"--change", "test-change",
		"--repo", repo, "--base", base, "--head", head,
		"--format", "yaml",
	})
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
}

func TestVerify_JudgeFactoryError(t *testing.T) {
	repo, base, head := fixtureGitRepo(t)
	fixtureChange(t, repo, "test-change")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		JudgeFactory: func(_ string) (llm.Judge, error) {
			return nil, errors.New("ANTHROPIC_API_KEY missing")
		},
		Stdout: stdout, Stderr: stderr,
	}
	code := app.Run([]string{"verify", "--change", "test-change", "--repo", repo, "--base", base, "--head", head})
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
}

func TestList_E2E(t *testing.T) {
	repo := t.TempDir()
	fixtureChange(t, repo, "first")
	fixtureChange(t, repo, "second")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{Stdout: stdout, Stderr: stderr}
	code := app.Run([]string{"list", "--repo", repo})
	if code != ExitOK {
		t.Errorf("exit = %d, stderr: %s", code, stderr)
	}
	want := "first\nsecond\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestList_E2E_Empty(t *testing.T) {
	repo := t.TempDir()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{Stdout: stdout, Stderr: stderr}
	code := app.Run([]string{"list", "--repo", repo})
	if code != ExitOK {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "(no changes") {
		t.Errorf("empty placeholder missing: %q", stdout.String())
	}
}

func TestVersion_E2E(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{Stdout: stdout, Stderr: stderr}
	code := app.Run([]string{"version"})
	if code != ExitOK {
		t.Errorf("exit = %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "check-spec ") {
		t.Errorf("version output: %q", stdout.String())
	}
}

// Compile-time check that context import is used.
var _ = context.Background
