// Package arceus loads the .arceus/changes/<id>/ folder produced by the
// Arceus Claude Code plugin and exposes its content as Go structs.
//
// The folder convention this package assumes:
//
//	.arceus/changes/<id>/
//	    proposal.md
//	    spec.md
//	    tasks.md
//	    decisions.md
//	    meta.json
package arceus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ChangesDirName is the conventional path (relative to repo root) where
// Arceus stores change proposals.
const ChangesDirName = ".arceus/changes"

// ErrChangeNotFound indicates the requested change folder does not exist.
var ErrChangeNotFound = errors.New("change not found")

// Meta mirrors meta.json written by the Arceus CLI.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Task is one checkbox item in tasks.md.
type Task struct {
	Index   int    // 1-based, in document order across all phases
	Phase   string // most recent "## ..." heading text, empty if none precedes
	Text    string // checkbox label (markdown allowed)
	Checked bool
}

// AcceptanceCriterion is one checkbox item in spec.md's acceptance section.
type AcceptanceCriterion struct {
	Index   int
	Text    string
	Checked bool
}

// Change is the in-memory view of a single .arceus/changes/<id>/ folder.
type Change struct {
	ID        string
	Title     string
	Meta      Meta
	Proposal  string // raw markdown
	Spec      string
	Tasks     string
	Decisions string

	// Parsed views computed at Load time.
	ParsedTasks              []Task
	ParsedAcceptanceCriteria []AcceptanceCriterion
}

// Load reads .arceus/changes/<changeID>/ from repoRoot.
// All four markdown files plus meta.json must exist.
func Load(repoRoot, changeID string) (*Change, error) {
	if changeID == "" {
		return nil, errors.New("changeID is required")
	}
	dir := filepath.Join(repoRoot, ChangesDirName, changeID)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrChangeNotFound, changeID)
		}
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	meta, err := readMeta(dir)
	if err != nil {
		return nil, err
	}

	proposal, err := readFile(dir, "proposal.md")
	if err != nil {
		return nil, err
	}
	spec, err := readFile(dir, "spec.md")
	if err != nil {
		return nil, err
	}
	tasksMD, err := readFile(dir, "tasks.md")
	if err != nil {
		return nil, err
	}
	decisions, err := readFile(dir, "decisions.md")
	if err != nil {
		return nil, err
	}

	return &Change{
		ID:                       changeID,
		Title:                    meta.Title,
		Meta:                     meta,
		Proposal:                 proposal,
		Spec:                     spec,
		Tasks:                    tasksMD,
		Decisions:                decisions,
		ParsedTasks:              ParseTasks(tasksMD),
		ParsedAcceptanceCriteria: ParseAcceptance(spec),
	}, nil
}

// List returns the change folder names under .arceus/changes/ in repoRoot,
// sorted lexicographically. The "archive" subfolder is excluded.
// If .arceus/changes/ does not exist, the result is (nil, nil).
func List(repoRoot string) ([]string, error) {
	dir := filepath.Join(repoRoot, ChangesDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "archive" {
			continue
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

func readMeta(dir string) (Meta, error) {
	p := filepath.Join(dir, "meta.json")
	b, err := os.ReadFile(p) //nolint:gosec // dir is the validated change folder, name is constant
	if err != nil {
		return Meta{}, fmt.Errorf("read meta.json: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Meta{}, fmt.Errorf("parse meta.json: %w", err)
	}
	return m, nil
}

func readFile(dir, name string) (string, error) {
	p := filepath.Join(dir, name)
	b, err := os.ReadFile(p) //nolint:gosec // dir + name are package-internal, not user-controlled
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(b), nil
}
