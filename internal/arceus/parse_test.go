package arceus

import "testing"

func TestParseTasks_Empty(t *testing.T) {
	if got := ParseTasks(""); len(got) != 0 {
		t.Errorf("empty input got %d tasks", len(got))
	}
}

func TestParseTasks_NoPhase(t *testing.T) {
	md := "- [ ] one\n- [x] two\n"
	tasks := ParseTasks(md)
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].Phase != "" {
		t.Errorf("Phase = %q, want empty", tasks[0].Phase)
	}
	if tasks[0].Checked || !tasks[1].Checked {
		t.Errorf("checked state wrong: %+v", tasks)
	}
	if tasks[0].Text != "one" || tasks[1].Text != "two" {
		t.Errorf("text wrong: %+v", tasks)
	}
}

func TestParseTasks_MultiPhase(t *testing.T) {
	md := `# Title

## Phase 0 — Bootstrap

- [x] init
- [x] dirs

## Phase 1 — Feature

- [ ] code
- [ ] test
`
	tasks := ParseTasks(md)
	if len(tasks) != 4 {
		t.Fatalf("got %d tasks, want 4", len(tasks))
	}
	if tasks[0].Phase != "Phase 0 — Bootstrap" {
		t.Errorf("tasks[0].Phase = %q", tasks[0].Phase)
	}
	if tasks[3].Phase != "Phase 1 — Feature" {
		t.Errorf("tasks[3].Phase = %q", tasks[3].Phase)
	}
	if tasks[0].Index != 1 || tasks[3].Index != 4 {
		t.Errorf("indices wrong: %+v", tasks)
	}
}

func TestParseTasks_IndentedItemsIgnored(t *testing.T) {
	md := "## P\n- [ ] top\n  - [ ] nested\n  - [x] nested checked\n"
	tasks := ParseTasks(md)
	if len(tasks) != 1 {
		t.Errorf("nested items should be ignored, got %d tasks", len(tasks))
	}
}

func TestParseTasks_UppercaseX(t *testing.T) {
	md := "- [X] caps\n"
	tasks := ParseTasks(md)
	if len(tasks) != 1 || !tasks[0].Checked {
		t.Errorf("uppercase X should mean checked: %+v", tasks)
	}
}

func TestParseAcceptance_Chinese(t *testing.T) {
	md := `# Spec

## 需求描述

words

## 驗收條件

- [ ] one
- [x] two

## 技術假設

- something
- [ ] not-a-criterion
`
	got := ParseAcceptance(md)
	if len(got) != 2 {
		t.Fatalf("got %d criteria, want 2 — got %+v", len(got), got)
	}
	if got[0].Text != "one" || got[1].Text != "two" {
		t.Errorf("text wrong: %+v", got)
	}
	if got[0].Checked || !got[1].Checked {
		t.Errorf("checked state wrong: %+v", got)
	}
}

func TestParseAcceptance_English(t *testing.T) {
	md := "## Acceptance Criteria\n\n- [ ] alpha\n- [x] beta\n"
	got := ParseAcceptance(md)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestParseAcceptance_NoSection(t *testing.T) {
	md := "## Other\n\n- [ ] not collected\n"
	got := ParseAcceptance(md)
	if len(got) != 0 {
		t.Errorf("got %d, want 0", len(got))
	}
}

func TestParseAcceptance_HeadingTerminates(t *testing.T) {
	md := `## 驗收條件
- [ ] in section
## 下一節
- [ ] not in section
`
	got := ParseAcceptance(md)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1 — %+v", len(got), got)
	}
	if got[0].Text != "in section" {
		t.Errorf("text = %q", got[0].Text)
	}
}
