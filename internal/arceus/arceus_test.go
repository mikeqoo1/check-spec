package arceus

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const fixtureRepo = "testdata/repo"

func TestLoad_Success(t *testing.T) {
	c, err := Load(fixtureRepo, "sample-change")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.ID != "sample-change" {
		t.Errorf("ID = %q, want %q", c.ID, "sample-change")
	}
	if c.Title != "Sample change for testing" {
		t.Errorf("Title = %q", c.Title)
	}
	if c.Meta.Status != "active" {
		t.Errorf("Meta.Status = %q, want active", c.Meta.Status)
	}
	if c.Proposal == "" || c.Spec == "" || c.Tasks == "" || c.Decisions == "" {
		t.Errorf("raw markdown fields should be populated")
	}
	if got, want := len(c.ParsedTasks), 4; got != want {
		t.Errorf("ParsedTasks count = %d, want %d", got, want)
	}
	if got, want := len(c.ParsedAcceptanceCriteria), 3; got != want {
		t.Errorf("ParsedAcceptanceCriteria count = %d, want %d", got, want)
	}
}

func TestLoad_NotFound(t *testing.T) {
	_, err := Load(fixtureRepo, "no-such-change")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrChangeNotFound) {
		t.Errorf("error = %v, want wrapped ErrChangeNotFound", err)
	}
}

func TestLoad_EmptyID(t *testing.T) {
	_, err := Load(fixtureRepo, "")
	if err == nil {
		t.Fatal("expected error for empty changeID")
	}
}

func TestLoad_MissingMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	changeDir := filepath.Join(dir, ChangesDirName, "broken")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(changeDir, "meta.json"), `{"id":"broken","title":"x","status":"draft","createdAt":"2026-05-20T00:00:00Z","updatedAt":"2026-05-20T00:00:00Z"}`)
	writeFile(t, filepath.Join(changeDir, "proposal.md"), "# p")
	writeFile(t, filepath.Join(changeDir, "spec.md"), "# s")
	writeFile(t, filepath.Join(changeDir, "tasks.md"), "# t")
	// decisions.md intentionally missing

	_, err := Load(dir, "broken")
	if err == nil {
		t.Fatal("expected error for missing decisions.md")
	}
}

func TestLoad_InvalidMeta(t *testing.T) {
	dir := t.TempDir()
	changeDir := filepath.Join(dir, ChangesDirName, "bad-meta")
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(changeDir, "meta.json"), `{not valid json`)
	for _, f := range []string{"proposal.md", "spec.md", "tasks.md", "decisions.md"} {
		writeFile(t, filepath.Join(changeDir, f), "# header")
	}
	_, err := Load(dir, "bad-meta")
	if err == nil {
		t.Fatal("expected error for invalid meta.json")
	}
}

func TestList_Success(t *testing.T) {
	ids, err := List(fixtureRepo)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	want := []string{"another-change", "sample-change"}
	if len(ids) != len(want) {
		t.Fatalf("List returned %v, want %v", ids, want)
	}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("List[%d] = %q, want %q", i, ids[i], w)
		}
	}
}

func TestList_ArchiveExcluded(t *testing.T) {
	ids, err := List(fixtureRepo)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == "archive" {
			t.Errorf("archive should not appear in List output")
		}
	}
}

func TestList_NoChangesDir(t *testing.T) {
	dir := t.TempDir()
	ids, err := List(dir)
	if err != nil {
		t.Errorf("List on empty repo should not error, got %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil, got %v", ids)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
