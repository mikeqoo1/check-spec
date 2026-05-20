// Package gitdiff collects the unified diff between two git refs by
// shelling out to the system `git` binary.
//
// It splits the result into per-file entries so that downstream consumers
// (the LLM prompt builder) can prioritize, truncate, and reason about
// individual files. Binary files are excluded.
package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultMaxBytes is the default truncation budget for the concatenated
// per-file diff payload.
const DefaultMaxBytes = 200_000

// refRE allows characters that legitimately appear in git refs/revisions
// (HEAD, HEAD~3, HEAD^, origin/main, abc1234, refs/tags/v1.0).
// This is intentionally restrictive — anything outside it is treated as
// untrusted input and rejected before we hand it to git.
var refRE = regexp.MustCompile(`^[A-Za-z0-9_./^~@{}\-]+$`)

// ErrInvalidRef is returned when a user-supplied ref contains characters
// outside the allowed set.
var ErrInvalidRef = errors.New("invalid git ref")

// FileDiff describes the change to one file between two refs.
type FileDiff struct {
	Path    string // current path (post-rename)
	OldPath string // pre-rename path; empty when unchanged
	Status  string // single-letter status: A M D R C T U (from git --name-status)
	Added   int    // line-add count from numstat
	Deleted int    // line-del count from numstat
	Binary  bool
	Diff    string // unified diff for this single file; empty when Binary or omitted
	Omitted bool   // true when this file was dropped by truncation
}

// Diff is the bundle of per-file changes between BaseSHA and HeadSHA.
type Diff struct {
	BaseRef    string
	HeadRef    string
	BaseSHA    string
	HeadSHA    string
	Files      []FileDiff
	Truncated  bool
	TotalBytes int
}

// Collector runs git commands inside RepoRoot.
type Collector struct {
	RepoRoot string
}

// New returns a Collector rooted at repoRoot.
func New(repoRoot string) *Collector {
	return &Collector{RepoRoot: repoRoot}
}

// Collect gathers the diff between base..head, with a soft cap of maxBytes
// applied to the concatenated per-file Diff payload. Binary files are
// always excluded.
//
// File ordering follows priority: non-vendor / non-generated paths come
// first so that they survive truncation when the budget is tight.
//
// When maxBytes <= 0 the default DefaultMaxBytes is used.
func (c *Collector) Collect(ctx context.Context, base, head string, maxBytes int) (Diff, error) {
	if err := validateRef(base); err != nil {
		return Diff{}, fmt.Errorf("base: %w", err)
	}
	if err := validateRef(head); err != nil {
		return Diff{}, fmt.Errorf("head: %w", err)
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	baseSHA, err := c.revParse(ctx, base)
	if err != nil {
		return Diff{}, fmt.Errorf("resolve base ref %q: %w", base, err)
	}
	headSHA, err := c.revParse(ctx, head)
	if err != nil {
		return Diff{}, fmt.Errorf("resolve head ref %q: %w", head, err)
	}

	files, err := c.listFiles(ctx, base, head)
	if err != nil {
		return Diff{}, err
	}

	// Stable, priority-aware ordering.
	sort.SliceStable(files, func(i, j int) bool {
		pi, pj := priority(files[i].Path), priority(files[j].Path)
		if pi != pj {
			return pi < pj
		}
		return files[i].Path < files[j].Path
	})

	out := Diff{
		BaseRef: base, HeadRef: head,
		BaseSHA: baseSHA, HeadSHA: headSHA,
	}

	for _, f := range files {
		if f.Binary {
			f.Diff = ""
			out.Files = append(out.Files, f)
			continue
		}
		body, err := c.fileDiff(ctx, base, head, f.Path)
		if err != nil {
			return Diff{}, fmt.Errorf("diff %s: %w", f.Path, err)
		}
		if out.TotalBytes+len(body) > maxBytes {
			out.Truncated = true
			f.Omitted = true
			f.Diff = ""
			out.Files = append(out.Files, f)
			continue
		}
		f.Diff = body
		out.TotalBytes += len(body)
		out.Files = append(out.Files, f)
	}
	return out, nil
}

func validateRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRef)
	}
	if !refRE.MatchString(ref) {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}

func (c *Collector) revParse(ctx context.Context, ref string) (string, error) {
	out, err := c.run(ctx, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("rev-parse returned empty for %q", ref)
	}
	return sha, nil
}

// listFiles merges output from `git diff --name-status` and
// `git diff --numstat` into a single per-file record set.
func (c *Collector) listFiles(ctx context.Context, base, head string) ([]FileDiff, error) {
	rangeArg := base + ".." + head

	nameOut, err := c.run(ctx, "diff", "--name-status", "-z", rangeArg)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w", err)
	}
	nameEntries, err := parseNameStatus(nameOut)
	if err != nil {
		return nil, err
	}

	numOut, err := c.run(ctx, "diff", "--numstat", "-z", rangeArg)
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat: %w", err)
	}
	numEntries, err := parseNumstat(numOut)
	if err != nil {
		return nil, err
	}

	byPath := make(map[string]*FileDiff, len(nameEntries))
	for i := range nameEntries {
		e := nameEntries[i]
		byPath[e.Path] = &e
	}
	for _, n := range numEntries {
		fd, ok := byPath[n.Path]
		if !ok {
			byPath[n.Path] = &FileDiff{
				Path: n.Path, Added: n.Added, Deleted: n.Deleted, Binary: n.Binary,
			}
			continue
		}
		fd.Added = n.Added
		fd.Deleted = n.Deleted
		fd.Binary = n.Binary
	}

	out := make([]FileDiff, 0, len(byPath))
	for _, fd := range byPath {
		out = append(out, *fd)
	}
	return out, nil
}

func (c *Collector) fileDiff(ctx context.Context, base, head, path string) (string, error) {
	return c.run(ctx, "diff", "--no-color", base+".."+head, "--", path)
}

// run invokes git with the given args inside c.RepoRoot and returns stdout.
func (c *Collector) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are constants or pre-validated refs
	cmd.Dir = c.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// parseNameStatus parses NUL-separated `git diff --name-status -z` output.
//
// Format per entry: <STATUS>\0<PATH>\0       for A/M/D/T/U
//
//	R<score>\0<OLD>\0<NEW>\0 for R (rename) and C (copy)
func parseNameStatus(out string) ([]FileDiff, error) {
	parts := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var entries []FileDiff
	for i := 0; i < len(parts); {
		if parts[i] == "" {
			i++
			continue
		}
		status := parts[i]
		switch status[0] {
		case 'R', 'C':
			if i+2 >= len(parts) {
				return nil, fmt.Errorf("malformed name-status near %q", status)
			}
			entries = append(entries, FileDiff{
				Status:  string(status[0]),
				OldPath: parts[i+1],
				Path:    parts[i+2],
			})
			i += 3
		default:
			if i+1 >= len(parts) {
				return nil, fmt.Errorf("malformed name-status near %q", status)
			}
			entries = append(entries, FileDiff{
				Status: status,
				Path:   parts[i+1],
			})
			i += 2
		}
	}
	return entries, nil
}

// parseNumstat parses NUL-separated `git diff --numstat -z` output.
//
// Format per entry: <ADDED>\t<DELETED>\t<PATH>\0  (or with rename src+dst)
// Binary files report "-" for both ADDED and DELETED.
func parseNumstat(out string) ([]FileDiff, error) {
	parts := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var entries []FileDiff
	for i := 0; i < len(parts); {
		if parts[i] == "" {
			i++
			continue
		}
		// Each entry's first chunk holds "ADDED\tDELETED\tPATH" — unless this
		// is a rename in which case PATH is empty and the next two NUL-separated
		// chunks are old/new paths.
		fields := strings.SplitN(parts[i], "\t", 3)
		if len(fields) < 3 {
			return nil, fmt.Errorf("malformed numstat entry: %q", parts[i])
		}
		added, deleted, binary := parseCount(fields[0]), parseCount(fields[1]), fields[0] == "-"
		path := fields[2]
		if path == "" {
			// Rename: next two chunks are old and new paths.
			if i+2 >= len(parts) {
				return nil, fmt.Errorf("malformed numstat rename near %q", parts[i])
			}
			path = parts[i+2]
			i += 3
		} else {
			i++
		}
		entries = append(entries, FileDiff{
			Path: path, Added: added, Deleted: deleted, Binary: binary,
		})
	}
	return entries, nil
}

func parseCount(s string) int {
	if s == "-" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

// priority returns a sort key — lower value means higher priority (kept under truncation).
func priority(path string) int {
	switch {
	case strings.HasPrefix(path, "vendor/"),
		strings.HasPrefix(path, "node_modules/"),
		strings.HasPrefix(path, "dist/"),
		strings.HasSuffix(path, ".pb.go"),
		strings.HasSuffix(path, ".gen.go"),
		strings.HasSuffix(path, "_generated.go"),
		strings.HasSuffix(path, "package-lock.json"),
		strings.HasSuffix(path, "yarn.lock"),
		strings.HasSuffix(path, "pnpm-lock.yaml"),
		strings.HasSuffix(path, "go.sum"):
		return 2
	default:
		return 1
	}
}
