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
	slots := buildSlots("2026-07-16", 8, nil, due, nil, settingsWith(4, 4), true, 0)
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
	slots := buildSlots("2026-07-16", 3, preset, due, nil, settingsWith(3), true, 0)
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
	slots := buildSlots("2026-07-16", 3, nil, nil, existing, settingsWith(3), false, 0)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "a" {
		t.Fatalf("pinned slot lost while tasks API unavailable: %+v", slots[0])
	}
}

func TestBuildSlotsDropsPinnedForCompletedTask(t *testing.T) {
	ext := "done-task"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	slots := buildSlots("2026-07-16", 2, nil, []tasksclient.DueTask{dueTask("b", "B", 25)}, existing, settingsWith(2), true, 0)
	if slots[0].TaskExternalID != nil && *slots[0].TaskExternalID == "done-task" {
		t.Fatalf("pinned slot with completed task must be released: %+v", slots[0])
	}
}

func workTask(id, title string, startMin int) tasksclient.DueTask {
	d := dueTask(id, title, 0)
	d.StartTimeMin = &startMin
	return d
}

func TestBuildSlotsWorkQuotaBlocksOf3(t *testing.T) {
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540),
		dueTask("a", "A", 100),
	}
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0)
	got := taskIDs(slots)
	want := []string{"a", "w", "w", "a", "w", "w"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsWorkQuotaBlocksOf4(t *testing.T) {
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540),
		dueTask("a", "A", 200),
	}
	slots := buildSlots("2026-07-16", 8, nil, due, nil, settingsWith(4, 4), true, 0)
	got := taskIDs(slots)
	want := []string{"a", "w", "a", "w", "a", "w", "a", "w"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSlotsNoWorkWithoutWorkTask(t *testing.T) {
	due := []tasksclient.DueTask{dueTask("a", "A", 200)}
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0)
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
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540),
		dueTask("a", "A", 100),
	}
	slots := buildSlots("2026-07-16", 3, nil, due, existing, settingsWith(3), true, 0)
	got := taskIDs(slots)
	want := []string{"a", "w", "w"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
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
	slots := buildSlots("2026-07-16", 3, nil, due, existing, settingsWith(3), true, 1)
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
	slots := buildSlots("2026-07-16", 4, nil, due, existing, settingsWith(4), true, 0)
	got := taskIDs(slots)
	want := []string{"a", "", "a", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}
