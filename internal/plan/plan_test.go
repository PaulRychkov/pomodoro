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
	slots := buildSlots("2026-07-16", 8, nil, due, nil, settingsWith(4, 4), true, 0, nil, 0)
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
	slots := buildSlots("2026-07-16", 3, preset, due, nil, settingsWith(3), true, 0, nil, 0)
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
	slots := buildSlots("2026-07-16", 3, nil, nil, existing, settingsWith(3), false, 0, nil, 0)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "a" {
		t.Fatalf("pinned slot lost while tasks API unavailable: %+v", slots[0])
	}
}

func TestBuildSlotsDropsPinnedForCompletedTask(t *testing.T) {
	ext := "done-task"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	slots := buildSlots("2026-07-16", 2, nil, []tasksclient.DueTask{dueTask("b", "B", 25)}, existing, settingsWith(2), true, 0, nil, 0)
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

func blockedTask(id, title string, startMin, durMin int) tasksclient.DueTask {
	d := workTask(id, title, startMin, durMin)
	d.Blocked = true
	return d
}

func settingsStudy(before, after int, blocks ...int) models.Settings {
	s := settingsWith(blocks...)
	s.PlanBeforeWindowMin = before
	s.PlanAfterWindowMin = after
	return s
}

func TestBuildSlotsWorkQuotaBlocksOf3(t *testing.T) {
	settings := settingsStudy(30, 30, 3)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
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
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
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
	layout := computeDayLayout(settings, dayIntervals(due))
	if layout.total != 23 {
		t.Fatalf("weekday capacity: got %d slots, want 23 (blocks: %+v)", layout.total, layout.blocks)
	}
	works := map[string]tasksclient.TaskOption{"w": due[0].Option}
	windowAt := planWindowPositions(layout, map[int]models.PlanSlot{}, works, 67)
	if len(windowAt) != 11 {
		t.Fatalf("work slots: got %d, want 11 (%v)", len(windowAt), windowAt)
	}
	if windowAt[0] != "" || windowAt[1] != "" || windowAt[2] != "" {
		t.Fatalf("morning study slots must stay free of work: %v", windowAt)
	}
	for i := layout.total - 3; i < layout.total; i++ {
		if windowAt[i] != "" {
			t.Fatalf("evening study slots must stay free of work: %v", windowAt)
		}
	}
}

func TestBuildSlotsNoWorkWithoutWorkTask(t *testing.T) {
	due := []tasksclient.DueTask{dueTask("a", "A", 200)}
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0, nil, 0)
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
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, existing, settings, true, 0, layout, 0)
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
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
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
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(3, 3), true, 0, nil, 0)
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
	slots := buildSlots("2026-07-16", 6, nil, due, nil, settingsWith(4), true, 0, nil, 4)
	got := taskIDs(slots)
	counts := map[string]int{}
	for _, id := range got {
		counts[id]++
	}
	if counts["a"] <= counts["b"] {
		t.Fatalf("задача на 500 минут должна получить больше слотов, чем на 20: %v", got)
	}
	tail := taskIDs(slots[4:])
	hasA := false
	for _, id := range tail {
		if id == "a" {
			hasA = true
		}
	}
	if !hasA {
		t.Fatalf("остаток самой объёмной задачи обязан попасть в не влезшие: %v", tail)
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
	slots := buildSlots("2026-07-16", 3, nil, due, existing, settingsWith(3), true, 1, nil, 0)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "gone-task" {
		t.Fatalf("done slot must stay frozen: %+v", slots[0])
	}
	if slots[1].TaskExternalID == nil || *slots[1].TaskExternalID != "a" {
		t.Fatalf("tail must be redistributed: %v", taskIDs(slots))
	}
}

func TestBuildSlotsKeepsAutoBindingsWhenTasksUnavailable(t *testing.T) {
	src, a := "tasks", "a"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 0, TaskExternalID: &a, TaskSource: &src, TaskTitle: &a},
		{Date: "2026-07-16", Idx: 1, TaskExternalID: &a, TaskSource: &src, TaskTitle: &a},
	}
	slots := buildSlots("2026-07-16", 2, nil, nil, existing, settingsWith(2), false, 0, nil, 0)
	for i, sl := range slots {
		if sl.TaskExternalID != nil {
			continue
		}
		t.Fatalf("слот %d потерял задачу при недоступном tasks API: %v", i, taskIDs(slots))
	}
}

func priorityTask(id, title string, effort, priority int) tasksclient.DueTask {
	d := dueTask(id, title, effort)
	d.Priority = priority
	return d
}

func countBy(slots []models.PlanSlot, id string) int {
	n := 0
	for _, x := range taskIDs(slots) {
		if x == id {
			n++
		}
	}
	return n
}

func TestOverflowTakesLowerWeightedRemainder(t *testing.T) {
	settings := settingsStudy(80, 90, 3, 3, 3, 3, 3)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 540),
		priorityTask("job", "Поиск работы", 330, 3),
		priorityTask("eng", "Английский", 60, 2),
		priorityTask("type", "Слепая печать", 30, 2),
	}
	layout := computeDayLayout(settings, dayIntervals(due))
	committed := 15
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, committed)
	if len(slots) <= committed {
		t.Fatalf("нужен план длиннее блоков, получено %d при committed=%d", len(slots), committed)
	}
	tail := taskIDs(slots[committed:])
	jobInTail := 0
	for _, id := range tail {
		if id == "job" {
			jobInTail++
		}
	}
	if jobInTail == 0 {
		t.Fatalf("в не влезшие должен попасть остаток «поиска работы»: %v", tail)
	}
	for _, id := range []string{"job", "eng", "type"} {
		if countBy(slots, id) == 0 {
			t.Fatalf("задача %s пропала из плана целиком: %v", id, taskIDs(slots))
		}
	}
}

func TestHigherPriorityGetsMoreSlots(t *testing.T) {
	due := []tasksclient.DueTask{
		priorityTask("hi", "Важная", 100, 5),
		priorityTask("lo", "Обычная", 100, 1),
	}
	slots := buildSlots("2026-07-16", 4, nil, due, nil, settingsWith(4), true, 0, nil, 4)
	hi, lo := countBy(slots[:4], "hi"), countBy(slots[:4], "lo")
	if hi <= lo {
		t.Fatalf("в основных слотах приоритет 5 должен получить больше, чем 1: hi=%d lo=%d", hi, lo)
	}
}

func TestPlanShowsEveryRequiredPomodoro(t *testing.T) {
	settings := settingsStudy(80, 90, 3, 3, 3, 3, 3)
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 540),
		priorityTask("job", "Поиск работы", 330, 3),
		priorityTask("eng", "Английский", 60, 2),
		priorityTask("type", "Слепая печать", 30, 2),
	}
	layout := computeDayLayout(settings, dayIntervals(due))
	committed := 15
	slots := buildSlots("2026-07-16", committed, nil, due, nil, settings, true, 0, layout, committed)

	if len(slots) < committed {
		t.Fatalf("основных слотов должно быть не меньше %d, получено %d", committed, len(slots))
	}
	want := map[string]int{"job": 14, "eng": 3, "type": 1}
	for id, need := range want {
		if got := countBy(slots, id); got != need {
			t.Fatalf("задача %s: в плане %d помидоров, требуется %d (всего слотов %d)", id, got, need, len(slots))
		}
	}
	if countBy(slots[:committed], "job") == 0 {
		t.Fatalf("самая весомая задача обязана попасть в основные слоты: %v", taskIDs(slots[:committed]))
	}
}

func TestPriorityWeightPercentScale(t *testing.T) {
	want := map[int]int{1: 100, 2: 150, 3: 200, 4: 250, 5: 300}
	for p, w := range want {
		if got := priorityWeightPercent(p); got != w {
			t.Fatalf("приоритет %d: вес %d%%, ожидалось %d%%", p, got, w)
		}
	}
	if priorityWeightPercent(0) != 100 || priorityWeightPercent(9) != 300 {
		t.Fatalf("значения вне шкалы должны прижиматься к границам")
	}
}

func TestBuildSlotsReactsToWindowShare(t *testing.T) {
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 540, 540),
		dueTask("a", "A", 900),
	}
	countWork := func(share int) int {
		settings := settingsStudy(80, 90, 3)
		settings.WindowSharePercent = share
		layout := computeDayLayout(settings, dayIntervals(due))
		slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
		n := 0
		for _, id := range taskIDs(slots) {
			if id == "w" {
				n++
			}
		}
		return n
	}
	full, third := countWork(67), countWork(34)
	if third >= full {
		t.Fatalf("снижение доли окна не уменьшило слоты работы: 67%%=%d, 34%%=%d", full, third)
	}
	if none := countWork(0); none != 0 {
		t.Fatalf("нулевая доля окна должна убрать работу из плана, получено %d слотов", none)
	}
}

func TestBuildSlotsReactsToFocusDuration(t *testing.T) {
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 540)}
	total := func(focusMin int) int {
		settings := settingsStudy(80, 90, 4)
		settings.FocusDurationSeconds = focusMin * 60
		return computeDayLayout(settings, dayIntervals(due)).total
	}
	short, long := total(25), total(50)
	if long >= short {
		t.Fatalf("удлинение помидора не сократило число слотов: 25м=%d, 50м=%d", short, long)
	}
}

func TestDayLayoutAdaptsSlotLengthToPeriod(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 40)}
	layout := computeDayLayout(settings, dayIntervals(due))
	if layout.total != 2 {
		t.Fatalf("в 40-минутный период должно лечь 2 помидора, получено %d", layout.total)
	}
	for i := 0; i < layout.total; i++ {
		sh, ok := layout.shapeAt(i)
		if !ok || sh.focus != 17 || sh.brk != 6 {
			t.Fatalf("слот %d: %+v, ожидалось 17 минут фокуса и 6 перерыва", i, sh)
		}
	}
}

func TestDayLayoutKeepsLongBreakAtBlockEnd(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 240)}
	layout := computeDayLayout(settings, dayIntervals(due))
	if layout.total != 8 {
		t.Fatalf("в 4 часа должно лечь 8 помидоров, получено %d", layout.total)
	}
	first, _ := layout.shapeAt(0)
	blockEnd, _ := layout.shapeAt(2)
	if first.brk != 5 {
		t.Fatalf("короткий перерыв внутри блока: %d, ожидалось 5", first.brk)
	}
	if blockEnd.brk != 15 {
		t.Fatalf("на границе блока нужен длинный перерыв, получено %d", blockEnd.brk)
	}
}

func TestBlockedEventTakesTimeOutOfDay(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	withPomodoros := []tasksclient.DueTask{
		workTask("gym", "Зал", 420, 40),
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	blocked := []tasksclient.DueTask{
		blockedTask("gym", "Зал", 420, 40),
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	full := computeDayLayout(settings, dayIntervals(withPomodoros))
	cut := computeDayLayout(settings, dayIntervals(blocked))
	if full.total != 8 {
		t.Fatalf("зал с помидорами даёт 8 слотов, получено %d", full.total)
	}
	if cut.total != 6 {
		t.Fatalf("зал без помидоров обязан забрать свои 40 минут, ожидалось 6 слотов, получено %d", cut.total)
	}
	for _, b := range cut.blocks {
		if b.windowID == "gym" {
			t.Fatalf("на заблокированное время слоты не ставятся: %+v", cut.blocks)
		}
	}
}

func TestBlockedEventInsideWindowSplitsIt(t *testing.T) {
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 600, 540),
		blockedTask("lunch", "Обед", 840, 60),
	}
	items := dayIntervals(due)
	var work []dayInterval
	for _, it := range items {
		if it.option.ExternalID == "w" {
			work = append(work, it)
		}
	}
	if len(work) != 2 {
		t.Fatalf("обед посреди работы режет окно на две части, получено %+v", items)
	}
	if work[0].start != 600 || work[0].length != 240 || work[1].start != 900 || work[1].length != 240 {
		t.Fatalf("части окна: %+v, ожидались 10:00-14:00 и 15:00-19:00", work)
	}
	total := 0
	for _, it := range items {
		total += it.length
	}
	if total != 540 {
		t.Fatalf("окно и обед вместе занимают 9 часов, получено %d минут", total)
	}
}

func TestPavelsWeekdayLayout(t *testing.T) {
	settings := settingsStudy(30, 60, 3, 3, 3, 3, 3, 3, 3, 2)
	settings.LongBreakSeconds = 900
	settings.WindowSharePercent = 67
	due := []tasksclient.DueTask{
		blockedTask("prep", "Сборы в зал", 400, 20),
		blockedTask("gym", "Зал", 420, 40),
		blockedTask("back", "Дорога из зала", 460, 20),
		workTask("w", "Работа", 600, 540),
		blockedTask("lunch", "Обед", 840, 60),
		priorityTask("job", "Поиск работы", 120, 3),
		priorityTask("eng", "Английский", 60, 2),
		priorityTask("type", "Слепая печать", 40, 2),
		priorityTask("ai", "Работа с ИИ и другое", 75, 2),
	}
	layout := computeDayLayout(settings, dayIntervals(due))
	if layout.total != 23 {
		t.Fatalf("день 6:00-20:00 вмещает 23 помидора, получено %d", layout.total)
	}
	if sh, _ := layout.shapeAt(0); sh.focus != 25 {
		t.Fatalf("до сборов в зал полный помидор, получено %d минут", sh.focus)
	}
	for _, idx := range []int{21, 22} {
		if sh, _ := layout.shapeAt(idx); sh.focus != 25 {
			t.Fatalf("после работы полные помидоры, слот %d = %d минут", idx, sh.focus)
		}
	}
	slots := buildSlots("2026-09-23", 23, nil, due, nil, settings, true, 0, layout, 23)
	want := map[string]int{"w": 10, "job": 5, "eng": 3, "type": 2, "ai": 3}
	for id, n := range want {
		if got := countBy(slots, id); got != n {
			t.Fatalf("%s: %d слотов, ожидалось %d; план %v", id, got, n, taskIDs(slots))
		}
	}
	for _, id := range []string{"prep", "gym", "back", "lunch"} {
		if countBy(slots, id) != 0 {
			t.Fatalf("событию %s без помидоров отдан слот: %v", id, taskIDs(slots))
		}
	}
}

func TestBlockedEventGetsNoSlots(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	due := []tasksclient.DueTask{
		blockedTask("gym", "Зал", 420, 40),
		workTask("w", "Работа", 540, 100),
		dueTask("a", "A", 500),
	}
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
	for i, id := range taskIDs(slots) {
		if id == "gym" {
			t.Fatalf("слот %d отдан событию без помидоров: %v", i, taskIDs(slots))
		}
	}
	if countBy(slots, "w") == 0 || countBy(slots, "a") == 0 {
		t.Fatalf("остальные задачи должны остаться в плане: %v", taskIDs(slots))
	}
}

func TestBuildSlotsAppliesFittedDurations(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 40)}
	layout := computeDayLayout(settings, dayIntervals(due))
	slots := buildSlots("2026-07-16", layout.total, nil, due, nil, settings, true, 0, layout, 0)
	for i, sl := range slots {
		if sl.FocusSeconds == nil || *sl.FocusSeconds != 17*60 {
			t.Fatalf("слот %d не получил подогнанную длину фокуса: %v", i, sl.FocusSeconds)
		}
		if sl.BreakSeconds == nil || *sl.BreakSeconds != 6*60 {
			t.Fatalf("слот %d не получил подогнанный перерыв: %v", i, sl.BreakSeconds)
		}
		if sl.Pinned {
			t.Fatalf("слот %d закреплён подгоном, хотя пользователь его не трогал", i)
		}
	}
}

func TestPresetDurationsWinOverFit(t *testing.T) {
	settings := settingsStudy(0, 0, 3)
	due := []tasksclient.DueTask{workTask("w", "Работа", 540, 40)}
	layout := computeDayLayout(settings, dayIntervals(due))
	preset := &models.Preset{Name: "long", Slots: models.PresetSlots{{FocusMinutes: 50, BreakMinutes: 10}}}
	slots := buildSlots("2026-07-16", layout.total, preset, due, nil, settings, true, 0, layout, 0)
	if slots[0].FocusSeconds == nil || *slots[0].FocusSeconds != 3000 {
		t.Fatalf("пресет должен побеждать подгон: %v", slots[0].FocusSeconds)
	}
	if slots[1].FocusSeconds == nil || *slots[1].FocusSeconds != 17*60 {
		t.Fatalf("слот вне пресета должен остаться подогнанным: %v", slots[1].FocusSeconds)
	}
}

func TestBuildSlotsSubtractsPinnedEffort(t *testing.T) {
	ext := "a"
	existing := []models.PlanSlot{
		{Date: "2026-07-16", Idx: 2, TaskExternalID: &ext, TaskSource: &ext, TaskTitle: &ext, Pinned: true},
	}
	due := []tasksclient.DueTask{dueTask("a", "A", 50)}
	slots := buildSlots("2026-07-16", 4, nil, due, existing, settingsWith(4), true, 0, nil, 0)
	got := taskIDs(slots)
	want := []string{"a", "", "a", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d: got %q want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}
