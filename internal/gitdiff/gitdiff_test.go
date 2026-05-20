package gitdiff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRef(t *testing.T) {
	cases := []struct {
		ref       string
		wantValid bool
	}{
		{"HEAD", true},
		{"HEAD^", true},
		{"HEAD~3", true},
		{"origin/main", true},
		{"refs/tags/v1.0", true},
		{"abc1234", true},
		{"HEAD@{1}", true},
		{"", false},
		{"head; rm -rf /", false},
		{"foo && bar", false},
		{"$(echo hi)", false},
		{"`pwd`", false},
		{"branch with space", false},
	}
	for _, tc := range cases {
		err := validateRef(tc.ref)
		got := err == nil
		if got != tc.wantValid {
			t.Errorf("validateRef(%q) valid=%v, want %v (err=%v)", tc.ref, got, tc.wantValid, err)
		}
		if !got && !errors.Is(err, ErrInvalidRef) {
			t.Errorf("error for %q should wrap ErrInvalidRef, got %v", tc.ref, err)
		}
	}
}

func TestPriority(t *testing.T) {
	cases := map[string]int{
		"cmd/check-spec/main.go":       1,
		"internal/arceus/arceus.go":    1,
		"README.md":                    1,
		"vendor/github.com/foo/bar.go": 2,
		"node_modules/x/y.js":          2,
		"dist/index.js":                2,
		"api/foo.pb.go":                2,
		"go.sum":                       2,
		"package-lock.json":            2,
	}
	for path, want := range cases {
		if got := priority(path); got != want {
			t.Errorf("priority(%q) = %d, want %d", path, got, want)
		}
	}
}

// fixtureRepo creates a temporary git repo with two commits and returns its path.
//
//	commit "base":  hello.go (text), big.bin (binary)
//	commit "head":  hello.go modified, new.go added, removed.go deleted, big.bin replaced
func fixtureRepo(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeBinary := func(path string, b []byte) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runGit("init", "-q", "-b", "main")
	write("hello.go", "package main\nfunc Hello() string { return \"hi\" }\n")
	write("removed.go", "package main\nfunc Removed() {}\n")
	writeBinary("big.bin", []byte{0, 1, 2, 3, 0, 0, 0, 4, 5})
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")
	baseSHA := strings.TrimSpace(runOutput(t, dir, "rev-parse", "HEAD"))

	write("hello.go", "package main\nfunc Hello() string { return \"hello\" }\n")
	write("new.go", "package main\nfunc New() {}\n")
	if err := os.Remove(filepath.Join(dir, "removed.go")); err != nil {
		t.Fatal(err)
	}
	writeBinary("big.bin", []byte{0, 0, 9, 9, 9})
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "head")
	headSHA := strings.TrimSpace(runOutput(t, dir, "rev-parse", "HEAD"))

	return dir, baseSHA, headSHA
}

func runOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func TestCollect_BasicShape(t *testing.T) {
	repo, base, head := fixtureRepo(t)
	c := New(repo)
	d, err := c.Collect(context.Background(), base, head, 0)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if d.BaseSHA != base || d.HeadSHA != head {
		t.Errorf("SHAs wrong: base=%s head=%s want %s %s", d.BaseSHA, d.HeadSHA, base, head)
	}
	if d.Truncated {
		t.Errorf("should not be truncated with default budget")
	}

	byPath := indexByPath(d.Files)
	if _, ok := byPath["hello.go"]; !ok {
		t.Errorf("hello.go missing from diff")
	}
	if _, ok := byPath["new.go"]; !ok {
		t.Errorf("new.go missing from diff")
	}
	if _, ok := byPath["removed.go"]; !ok {
		t.Errorf("removed.go missing from diff")
	}
	if bin, ok := byPath["big.bin"]; !ok {
		t.Errorf("big.bin missing from diff")
	} else {
		if !bin.Binary {
			t.Errorf("big.bin should be flagged Binary")
		}
		if bin.Diff != "" {
			t.Errorf("big.bin diff body should be empty when Binary, got %d bytes", len(bin.Diff))
		}
	}
}

func TestCollect_DiffBodyForTextFile(t *testing.T) {
	repo, base, head := fixtureRepo(t)
	c := New(repo)
	d, err := c.Collect(context.Background(), base, head, 0)
	if err != nil {
		t.Fatal(err)
	}
	hello := indexByPath(d.Files)["hello.go"]
	if !strings.Contains(hello.Diff, "-func Hello() string { return \"hi\" }") {
		t.Errorf("expected old line in diff, got: %s", hello.Diff)
	}
	if !strings.Contains(hello.Diff, "+func Hello() string { return \"hello\" }") {
		t.Errorf("expected new line in diff, got: %s", hello.Diff)
	}
}

func TestCollect_TruncationByBudget(t *testing.T) {
	repo, base, head := fixtureRepo(t)
	c := New(repo)
	// Tiny budget — at least one file should be omitted.
	d, err := c.Collect(context.Background(), base, head, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated {
		t.Errorf("expected Truncated=true with maxBytes=1")
	}
	omittedCount := 0
	for _, f := range d.Files {
		if f.Omitted {
			omittedCount++
		}
	}
	if omittedCount == 0 {
		t.Errorf("expected at least one omitted file")
	}
}

func TestCollect_InvalidRef(t *testing.T) {
	c := New(".")
	_, err := c.Collect(context.Background(), "head; rm -rf /", "HEAD", 0)
	if err == nil {
		t.Fatal("expected error for malicious ref")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("expected ErrInvalidRef, got %v", err)
	}
}

func indexByPath(files []FileDiff) map[string]FileDiff {
	m := make(map[string]FileDiff, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}
