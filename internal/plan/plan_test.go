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
	slots := buildSlots("2026-07-16", 8, nil, due, nil, settingsWith(4, 4), true)
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
	slots := buildSlots("2026-07-16", 3, preset, due, nil, settingsWith(3), true)
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
	slots := buildSlots("2026-07-16", 3, nil, nil, existing, settingsWith(3), false)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "a" {
		t.Fatalf("pinned slot lost while tasks API unavailable: %+v", slots[0])
	}
}

func TestBuildSlotsDropsPinnedForCompletedTask(t *testing.T) {
	ext := "done-task"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	slots := buildSlots("2026-07-16", 2, nil, []tasksclient.DueTask{dueTask("b", "B", 25)}, existing, settingsWith(2), true)
	if slots[0].TaskExternalID != nil && *slots[0].TaskExternalID == "done-task" {
		t.Fatalf("pinned slot with completed task must be released: %+v", slots[0])
	}
}

func TestBuildSlotsSubtractsPinnedEffort(t *testing.T) {
	ext := "a"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 2, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	due := []tasksclient.DueTask{dueTask("a", "A", 50)}
	slots := buildSlots("2026-07-16", 4, nil, due, existing, settingsWith(4), true)
	got := taskIDs(slots)
	want := []string{"a", "", "a", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}
