package plan

import (
	"testing"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

func settingsWith(blocks ...int) models.Settings {
	s := models.DefaultSettings()
	s.FocusDurationSeconds = 1500
	s.ShortBreakSeconds = 300
	s.DayBlocks = blocks
	return s
}

func dueTask(id, title string, effort int) tasksclient.DueTask {
	d := tasksclient.DueTask{Option: tasksclient.TaskOption{Source: "tasks", ExternalID: id, Title: title}}
	if effort > 0 {
		d.EffortMin = &effort
	}
	return d
}

func taskIDs(slots []models.PlanSlot) []string {
	out := make([]string, len(slots))
	for i, sl := range slots {
		if sl.TaskExternalID != nil {
			out[i] = *sl.TaskExternalID
		}
	}
	return out
}

func TestBuildSlotsAllocatesByEffort(t *testing.T) {
	due := []tasksclient.DueTask{
		dueTask("a", "A", 75),
		dueTask("b", "B", 50),
		dueTask("c", "C", 25),
	}
	slots := buildSlots("2026-07-16", 8, nil, due, nil, settingsWith(4, 4), true, 0, nil)
	got := taskIDs(slots)
	want := []string{"a", "a", "a", "b", "b", "c", "", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsUsesPresetDurationsForAllocation(t *testing.T) {
	preset := &models.Preset{Name: "long", Slots: models.PresetSlots{
		{FocusMinutes: 50, BreakMinutes: 10},
		{FocusMinutes: 50, BreakMinutes: 10},
		{FocusMinutes: 50, BreakMinutes: 10},
	}}
	due := []tasksclient.DueTask{dueTask("a", "A", 100)}
	slots := buildSlots("2026-07-16", 3, preset, due, nil, settingsWith(3), true, 0, nil)
	got := taskIDs(slots)
	want := []string{"a", "a", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q", i, got[i], want[i])
		}
	}
	if slots[0].FocusSeconds == nil || *slots[0].FocusSeconds != 3000 {
		t.Fatalf("preset focus not applied: %v", slots[0].FocusSeconds)
	}
	if slots[0].BreakSeconds == nil || *slots[0].BreakSeconds != 600 {
		t.Fatalf("preset break not applied: %v", slots[0].BreakSeconds)
	}
}

func TestBuildSlotsKeepsPinnedWhenTasksUnavailable(t *testing.T) {
	ext := "a"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	slots := buildSlots("2026-07-16", 3, nil, nil, existing, settingsWith(3), false, 0, nil)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "a" {
		t.Fatalf("pinned slot lost while tasks API unavailable: %+v", slots[0])
	}
}

func TestBuildSlotsDropsPinnedForCompletedTask(t *testing.T) {
	ext := "done-task"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	slots := buildSlots("2026-07-16", 2, nil, []tasksclient.DueTask{dueTask("b", "B", 25)}, existing, settingsWith(2), true, 0, nil)
	if slots[0].TaskExternalID != nil && *slots[0].TaskExternalID == "done-task" {
		t.Fatalf("pinned slot with completed task must be released: %+v", slots[0])
	}
}

func workTask(id, title string, startMin, durMin int) tasksclient.DueTask {
	d := dueTask(id, title, 0)
	d.StartTimeMin = &startMin
	d.DurationMin = &durMin
	return d
}

func settingsStudy(before, after int, blocks ...int) models.Settings {
	s := settingsWith(blocks...)
	s.StudyBeforeWorkMin = before
	s.StudyAfterWorkMin = after
	return s
}

func TestBuildSlotsWorkQuotaBlocksOf3(t *testing.T) {
	settings := settingsStudy(30, 30, 3)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, workWindowsOf(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout)
	got := taskIDs(slots)
	want := []string{"a", "a", "w", "w", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsWorkQuotaBlocksOf4(t *testing.T) {
	settings := settingsStudy(0, 0, 4)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 115),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, workWindowsOf(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout)
	got := taskIDs(slots)
	want := []string{"a", "w", "a", "w"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestComputeDayLayoutFullWeekday(t *testing.T) {
	settings := settingsStudy(80, 90, 3)
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 540)}
	layout := computeDayLayout(settings, workWindowsOf(due))
	if layout.total != 21 {
		t.Fatalf("weekday capacity: got %d slots, want 21 (blocks: %+v)", layout.total, layout.blocks)
	}
	works := map[string]tasksclient.TaskOption{"w": due[0].Option}
	workAt := planWorkPositions(layout, map[int]models.PlanSlot{}, works, 67)
	if len(workAt) != 10 {
		t.Fatalf("work slots: got %d, want 10 (%v)", len(workAt), workAt)
	}
	if workAt[0] != "" || workAt[1] != "" || workAt[2] != "" {
		t.Fatalf("morning study slots must stay free of work: %v", workAt)
	}
	for i := layout.total - 3; i < layout.total; i++ {
		if workAt[i] != "" {
			t.Fatalf("evening study slots must stay free of work: %v", workAt)
		}
	}
}

func TestBuildSlotsNoWorkWithoutWorkTask(t *testing.T) {
	due := []tasksclient.DueTask{dueTask("a", "A", 200)}
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0, nil)
	for i, sl := range slots {
		if sl.TaskExternalID == nil || *sl.TaskExternalID != "a" {
			t.Fatalf("slot %d must go to study task without work: %v", i, taskIDs(slots))
		}
	}
}

func TestBuildSlotsPinnedWorkCountsTowardQuota(t *testing.T) {
	w := "w"
	src := "tasks"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 2, TaskExternalID: &w, TaskSource: &src, TaskTitle: &w, Pinned: true},
	}
	settings := settingsStudy(30, 30, 3)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, workWindowsOf(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, existing, settings, true, 0, layout)
	got := taskIDs(slots)
	want := []string{"a", "a", "w", "w", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
	if !slots[2].Pinned {
		t.Fatalf("pinned work slot must stay pinned: %+v", slots[2])
	}
}

func TestBuildSlotsTwoWorkWindows(t *testing.T) {
	settings := settingsStudy(30, 30, 3)
	due := []tasksclient.DueTask{
		workTask("w1", "Работа", 540, 100),
		workTask("w2", "Зал", 700, 100),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, workWindowsOf(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout)
	got := taskIDs(slots)
	want := []string{"a", "a", "w1", "w1", "a", "a", "a", "w2", "w2", "a"}
	if len(got) != len(want) {
		t.Fatalf("total: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsScalesStudyLoad(t *testing.T) {
	due := []tasksclient.DueTask{
		dueTask("a", "A", 200),
		dueTask("b", "B", 100),
		dueTask("c", "C", 50),
	}
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0, nil)
	got := taskIDs(slots)
	want := []string{"a", "a", "a", "b", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsScalingKeepsEveryTask(t *testing.T) {
	due := []tasksclient.DueTask{
		dueTask("a", "A", 500),
		dueTask("b", "B", 20),
		dueTask("c", "C", 10),
	}
	slots := buildSlots("2026-07-16", 4, nil, due, nil, settingsWith(4), true, 0, nil)
	got := taskIDs(slots)
	counts := map[string]int{}
	for _, id := range got {
		counts[id]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] == 0 {
			t.Fatalf("task %s dropped from plan: %v", id, got)
		}
	}
}

func TestBuildSlotsFrozenDoneSlotsSurvive(t *testing.T) {
	old := "gone-task"
	src := "tasks"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &old, TaskSource: &src, TaskTitle: &old},
		{Date: "2026-07-16", Idx: 1},
		{Date: "2026-07-16", Idx: 2},
	}
	due := []tasksclient.DueTask{dueTask("a", "A", 100)}
	slots := buildSlots("2026-07-16", 3, nil, due, existing, settingsWith(3), true, 1, nil)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "gone-task" {
		t.Fatalf("done slot must stay frozen: %+v", slots[0])
	}
	if slots[1].TaskExternalID == nil || *slots[1].TaskExternalID != "a" {
		t.Fatalf("tail must be redistributed: %v", taskIDs(slots))
	}
}

func TestBuildSlotsSubtractsPinnedEffort(t *testing.T) {
	ext := "a"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 2, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	due := []tasksclient.DueTask{dueTask("a", "A", 50)}
	slots := buildSlots("2026-07-16", 4, nil, due, existing, settingsWith(4), true, 0, nil)
	got := taskIDs(slots)
	want := []string{"a", "", "a", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}
