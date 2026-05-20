package arceus

import (
	"regexp"
	"strings"
)

var (
	// Matches a markdown heading line, capturing the heading text.
	// Indented headings are not recognized (Arceus templates always start at column 0).
	headingRE = regexp.MustCompile(`^##+\s+(.*?)\s*$`)

	// Matches a top-level checkbox list item: "- [ ] text" or "- [x] text".
	// Nested (indented) items are intentionally ignored.
	taskLineRE = regexp.MustCompile(`^- \[([ xX])\]\s+(.+?)\s*$`)
)

// ParseTasks extracts checkbox items from a tasks.md document.
//
// Each item is tagged with the most recent "## ..." heading as its Phase.
// Items appearing before any heading get an empty Phase.
func ParseTasks(md string) []Task {
	var out []Task
	var phase string
	idx := 0
	for _, line := range strings.Split(md, "\n") {
		if m := headingRE.FindStringSubmatch(line); m != nil {
			phase = m[1]
			continue
		}
		if m := taskLineRE.FindStringSubmatch(line); m != nil {
			idx++
			out = append(out, Task{
				Index:   idx,
				Phase:   phase,
				Text:    m[2],
				Checked: isChecked(m[1]),
			})
		}
	}
	return out
}

// ParseAcceptance extracts checkbox items from the acceptance criteria
// section of a spec.md document.
//
// The section is identified by a heading containing "驗收條件" or whose
// text equals "Acceptance criteria" (case-insensitive). Collection stops
// at the next heading.
func ParseAcceptance(md string) []AcceptanceCriterion {
	inSection := false
	var out []AcceptanceCriterion
	idx := 0
	for _, line := range strings.Split(md, "\n") {
		if m := headingRE.FindStringSubmatch(line); m != nil {
			inSection = isAcceptanceHeading(m[1])
			continue
		}
		if !inSection {
			continue
		}
		if m := taskLineRE.FindStringSubmatch(line); m != nil {
			idx++
			out = append(out, AcceptanceCriterion{
				Index:   idx,
				Text:    m[2],
				Checked: isChecked(m[1]),
			})
		}
	}
	return out
}

func isChecked(marker string) bool {
	return marker == "x" || marker == "X"
}

func isAcceptanceHeading(text string) bool {
	t := strings.TrimSpace(text)
	if strings.Contains(t, "驗收條件") {
		return true
	}
	return strings.EqualFold(t, "Acceptance criteria")
}
