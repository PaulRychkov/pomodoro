package plan

import (
	"testing"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

func settingsDay(start, end int, blocks ...int) models.Settings {
	s := models.DefaultSettings()
	s.FocusDurationSeconds = 1500
	s.ShortBreakSeconds = 300
	s.LongBreakSeconds = 900
	s.DayBlocks = blocks
	s.DayStartMin = start
	s.DayEndMin = end
	s.WindowSharePercent = 67
	return s
}

func dueTask(id, title string, effort int) tasksclient.DueTask {
	d := tasksclient.DueTask{Option: tasksclient.TaskOption{Source: "tasks", ExternalID: id, Title: title}}
	if effort > 0 {
		d.EffortMin = &effort
	}
	return d
}

func priorityTask(id, title string, effort, priority int) tasksclient.DueTask {
	d := dueTask(id, title, effort)
	d.Priority = priority
	return d
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

// pavelsDay — будний день Павла: зал утром, «Работа» 10:00–19:00 с обедом.
func pavelsDay() []tasksclient.DueTask {
	return []tasksclient.DueTask{
		blockedTask("prep", "Сборы в зал", 400, 20),
		blockedTask("gym", "Зал", 420, 40),
		blockedTask("back", "Дорога из зала", 460, 20),
		workTask("w", "Работа", 600, 540),
		blockedTask("lunch", "Обед", 840, 60),
		priorityTask("job", "Поиск работы", 120, 3),
		priorityTask("ai", "Работа с ИИ и другое", 75, 2),
		priorityTask("eng", "Английский", 60, 2),
		priorityTask("type", "Слепая печать", 40, 2),
	}
}

func build(settings models.Settings, due []tasksclient.DueTask, from int) []models.PlanSlot {
	return buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, from: from})
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

func countBy(slots []models.PlanSlot, id string) int {
	n := 0
	for _, x := range taskIDs(slots) {
		if x == id {
			n++
		}
	}
	return n
}

func scheduled(slots []models.PlanSlot) []models.PlanSlot {
	var out []models.PlanSlot
	for _, sl := range slots {
		if !sl.Overflow {
			out = append(out, sl)
		}
	}
	return out
}

func inWindow(slots []models.PlanSlot, id string) []models.PlanSlot {
	var out []models.PlanSlot
	for _, sl := range slots {
		if sl.WindowID != nil && *sl.WindowID == id {
			out = append(out, sl)
		}
	}
	return out
}

// checkInvariants — правила, которые план обязан соблюдать при любых
// настройках: помидор целиком внутри своего периода и активного дня, не
// пересекается с событиями без помидоров и с соседями, задача окна стоит
// только в своём окне, «не влезшие» — в хвосте и без времени.
func checkInvariants(t *testing.T, settings models.Settings, due []tasksclient.DueTask, slots []models.PlanSlot) {
	t.Helper()
	dayStart, dayEnd := dayBounds(settings)
	shortMin := settings.ShortBreakSeconds / 60
	windows := map[string][2]int{}
	var blocked [][2]int
	for _, d := range due {
		if d.StartTimeMin == nil {
			continue
		}
		iv := [2]int{*d.StartTimeMin, *d.StartTimeMin + *d.DurationMin}
		if d.Blocked {
			blocked = append(blocked, iv)
		} else {
			windows[d.Option.ExternalID] = iv
		}
	}
	seenOverflow := false
	prevEnd := -1
	for i, sl := range slots {
		if sl.Idx != i {
			t.Fatalf("слот %d имеет idx %d", i, sl.Idx)
		}
		if sl.Overflow {
			seenOverflow = true
			if sl.StartMin != nil || sl.PeriodEndMin != nil {
				t.Fatalf("не влезший слот %d не должен иметь времени: %+v", i, sl)
			}
			continue
		}
		if seenOverflow {
			t.Fatalf("слот %d с местом в дне стоит после не влезших", i)
		}
		if sl.StartMin == nil || sl.PeriodStartMin == nil || sl.PeriodEndMin == nil || sl.FocusSeconds == nil || sl.BreakSeconds == nil {
			t.Fatalf("слот %d без времени или длительностей: %+v", i, sl)
		}
		start, end := *sl.StartMin, *sl.StartMin+*sl.FocusSeconds/60
		if *sl.BreakSeconds/60 < shortMin {
			t.Fatalf("слот %d: перерыв %d короче настроенного %d", i, *sl.BreakSeconds/60, shortMin)
		}
		if start < *sl.PeriodStartMin || end > *sl.PeriodEndMin {
			t.Fatalf("слот %d %d–%d выходит за период %d–%d", i, start, end, *sl.PeriodStartMin, *sl.PeriodEndMin)
		}
		if start < dayStart || end > dayEnd {
			t.Fatalf("слот %d %d–%d выходит за активный день %d–%d", i, start, end, dayStart, dayEnd)
		}
		if start < prevEnd {
			t.Fatalf("слот %d начинается в %d, раньше конца предыдущего %d", i, start, prevEnd)
		}
		prevEnd = end
		for _, b := range blocked {
			if start < b[1] && end > b[0] {
				t.Fatalf("слот %d %d–%d попал на событие без помидоров %v", i, start, end, b)
			}
		}
		inside := ""
		for id, w := range windows {
			if start < w[1] && end > w[0] {
				inside = id
				if start < w[0] || end > w[1] {
					t.Fatalf("слот %d %d–%d частично вылезает из окна %s %v", i, start, end, id, w)
				}
			}
		}
		if got := ""; sl.WindowID != nil {
			got = *sl.WindowID
			if got != inside {
				t.Fatalf("слот %d помечен окном %q, а стоит в %q", i, got, inside)
			}
		} else if inside != "" {
			t.Fatalf("слот %d стоит в окне %s, но не помечен им", i, inside)
		}
		if sl.TaskExternalID != nil {
			if _, isWindow := windows[*sl.TaskExternalID]; isWindow && inside != *sl.TaskExternalID {
				t.Fatalf("задача окна %s поставлена вне своего окна: слот %d", *sl.TaskExternalID, i)
			}
		}
	}
}

func TestDayPeriodsCutEventsAndRespectBounds(t *testing.T) {
	ps := dayPeriods(settingsDay(360, 1200, 3), pavelsDay())
	want := []struct {
		start, end int
		window     string
		tailRest   bool
	}{
		{360, 400, "", false},
		{480, 600, "", true},
		{600, 840, "w", false},
		{900, 1140, "w", true},
		{1140, 1200, "", false},
	}
	if len(ps) != len(want) {
		t.Fatalf("периодов %d, ожидалось %d: %+v", len(ps), len(want), ps)
	}
	for i, w := range want {
		p := ps[i]
		if p.start != w.start || p.end != w.end || p.windowID() != w.window || p.tailRest != w.tailRest {
			t.Fatalf("период %d: %d–%d окно %q хвост %v, ожидалось %+v", i, p.start, p.end, p.windowID(), p.tailRest, w)
		}
	}
}

func TestDayBoundsClipWindowsAndEvents(t *testing.T) {
	due := []tasksclient.DueTask{
		workTask("w", "Работа", 480, 600),
		blockedTask("late", "Поздний звонок", 1150, 60),
	}
	ps := dayPeriods(settingsDay(540, 1170, 3), due)
	if len(ps) != 2 || ps[0].start != 540 || ps[0].end != 1080 || ps[0].windowID() != "w" || !ps[0].tailRest {
		t.Fatalf("окно обязано обрезаться началом дня: %+v", ps)
	}
	if ps[1].start != 1080 || ps[1].end != 1150 || ps[1].window != nil {
		t.Fatalf("после окна свободное время до события 19:10, событие обрезано концом дня: %+v", ps)
	}
}

func TestPavelsWeekdayPlan(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	slots := build(settings, due, 360)
	checkInvariants(t, settings, due, slots)
	main := scheduled(slots)
	// 6:00–6:40 — два помидора по 17 минут (больше чистого фокуса, чем один
	// на 25), 8:00–10:00 — четыре, по восемь в каждой половине работы, два вечером.
	if len(main) != 24 {
		t.Fatalf("день 6:00–20:00 вмещает 24 помидора, получено %d: %v", len(main), taskIDs(main))
	}
	if *main[0].StartMin != 360 || *main[0].FocusSeconds != 17*60 {
		t.Fatalf("первый помидор 6:00 на 17 минут, получено %d/%d", *main[0].StartMin, *main[0].FocusSeconds/60)
	}
	work := inWindow(main, "w")
	if len(work) != 16 {
		t.Fatalf("окно работы вмещает 16 помидоров, получено %d", len(work))
	}
	if *work[0].StartMin != 600 {
		t.Fatalf("работа начинается ровно в 10:00, первый помидор окна в %d", *work[0].StartMin)
	}
	if got := countBy(work, "w"); got != 11 {
		t.Fatalf("67%% от 16 помидоров окна — 11 работе, получено %d: %v", got, taskIDs(work))
	}
	last := main[len(main)-1]
	if end := *last.StartMin + *last.FocusSeconds/60; end > 1200 {
		t.Fatalf("последний помидор заканчивается в %d, позже 20:00", end)
	}
	for _, id := range []string{"prep", "gym", "back", "lunch"} {
		if countBy(slots, id) != 0 {
			t.Fatalf("событию %s без помидоров отдан слот: %v", id, taskIDs(slots))
		}
	}
	for _, id := range []string{"job", "ai", "eng", "type"} {
		if countBy(main, id) == 0 {
			t.Fatalf("задача %s не получила ни одного помидора: %v", id, taskIDs(main))
		}
	}
}

func TestWindowShareSpreadsOtherTasksEvenly(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	work := inWindow(scheduled(build(settings, due, 360)), "w")
	run := 0
	for _, sl := range work {
		if sl.TaskExternalID != nil && *sl.TaskExternalID == "w" {
			run = 0
			continue
		}
		run++
		if run > 1 {
			t.Fatalf("при доле 67%% другие задачи не должны идти подряд внутри окна: %v", taskIDs(work))
		}
	}
	first, second := 0, 0
	for _, sl := range work {
		if sl.TaskExternalID != nil && *sl.TaskExternalID == "w" {
			if *sl.PeriodStartMin == 600 {
				first++
			} else {
				second++
			}
		}
	}
	if first-second > 1 || second-first > 1 {
		t.Fatalf("работа делится между половинами окна поровну: до обеда %d, после %d", first, second)
	}
}

func TestWindowShareExtremes(t *testing.T) {
	due := pavelsDay()
	for _, tc := range []struct {
		share, want int
	}{{0, 0}, {34, 5}, {67, 11}, {100, 16}} {
		settings := settingsDay(360, 1200, 3)
		settings.WindowSharePercent = tc.share
		slots := build(settings, due, 360)
		checkInvariants(t, settings, due, slots)
		if got := countBy(inWindow(slots, "w"), "w"); got != tc.want {
			t.Fatalf("доля %d%%: работе %d помидоров окна, ожидалось %d", tc.share, got, tc.want)
		}
		if tc.share == 100 {
			for _, sl := range inWindow(slots, "w") {
				if sl.TaskExternalID == nil || *sl.TaskExternalID != "w" {
					t.Fatalf("при 100%% в окне работы не может быть других задач: %v", taskIDs(inWindow(slots, "w")))
				}
			}
		}
	}
}

func TestDayBoundsDefineCapacity(t *testing.T) {
	due := pavelsDay()
	count := func(start, end int) int {
		settings := settingsDay(start, end, 3)
		slots := build(settings, due, start)
		checkInvariants(t, settings, due, slots)
		return len(scheduled(slots))
	}
	base := count(360, 1200)
	if earlier := count(300, 1200); earlier <= base {
		t.Fatalf("ранний старт дня обязан добавлять помидоры: %d против %d", earlier, base)
	}
	if later := count(360, 1320); later <= base {
		t.Fatalf("поздний конец дня обязан добавлять помидоры: %d против %d", later, base)
	}
	if short := count(600, 840); short != 8 {
		t.Fatalf("день 10:00–14:00 — только первая половина работы, 8 помидоров, получено %d", short)
	}
}

func TestFlexibleTasksByEffortWithoutWindows(t *testing.T) {
	settings := settingsDay(540, 780, 4)
	due := []tasksclient.DueTask{
		dueTask("a", "A", 75),
		dueTask("b", "B", 50),
		dueTask("c", "C", 25),
	}
	slots := build(settings, due, 540)
	checkInvariants(t, settings, due, slots)
	want := map[string]int{"a": 3, "b": 2, "c": 1}
	for id, n := range want {
		if got := countBy(slots, id); got != n {
			t.Fatalf("%s: %d помидоров, ожидалось %d: %v", id, got, n, taskIDs(slots))
		}
	}
	for _, sl := range slots {
		if sl.Overflow {
			t.Fatalf("всё влезает в 4 часа, а есть не влезшие: %v", taskIDs(slots))
		}
	}
}

func TestOverflowGoesToTailWithoutTime(t *testing.T) {
	settings := settingsDay(540, 660, 4)
	due := []tasksclient.DueTask{
		priorityTask("big", "Большая", 300, 3),
		priorityTask("small", "Малая", 50, 1),
	}
	slots := build(settings, due, 540)
	checkInvariants(t, settings, due, slots)
	main := scheduled(slots)
	if len(main) == 0 || len(main) == len(slots) {
		t.Fatalf("ожидались и основные, и не влезшие: %v", taskIDs(slots))
	}
	if countBy(main, "big") <= countBy(main, "small") {
		t.Fatalf("весомая задача обязана получить больше мест в дне: %v", taskIDs(main))
	}
	if countBy(slots[len(main):], "big") == 0 {
		t.Fatalf("остаток большой задачи обязан попасть в не влезшие: %v", taskIDs(slots))
	}
}

func TestHigherPriorityGetsMoreSlots(t *testing.T) {
	settings := settingsDay(540, 660, 4)
	due := []tasksclient.DueTask{
		priorityTask("hi", "Важная", 100, 5),
		priorityTask("lo", "Обычная", 100, 1),
	}
	main := scheduled(build(settings, due, 540))
	if hi, lo := countBy(main, "hi"), countBy(main, "lo"); hi <= lo {
		t.Fatalf("приоритет 5 должен получить больше, чем 1: hi=%d lo=%d", hi, lo)
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

func TestBuildFromLateMomentKeepsWindowsAnchored(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	slots := build(settings, due, 9*60+20)
	checkInvariants(t, settings, due, slots)
	main := scheduled(slots)
	if *main[0].StartMin != 560 || *main[0].PeriodEndMin != 600 || main[0].WindowID != nil {
		t.Fatalf("в 9:20 до работы остаётся один помидор с 9:20: %+v", main[0])
	}
	if *main[1].StartMin != 600 || main[1].WindowID == nil {
		t.Fatalf("работа по-прежнему начинается в 10:00: %+v", main[1])
	}
	slots = build(settings, due, 9*60+40)
	if main := scheduled(slots); *main[0].StartMin != 600 {
		t.Fatalf("в 9:40 полный помидор с перерывом до работы не влезает: первый в %d", *main[0].StartMin)
	}
}

func TestBuildAfterDayEndSendsEverythingToOverflow(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	slots := build(settings, due, 1201)
	checkInvariants(t, settings, due, slots)
	if len(scheduled(slots)) != 0 {
		t.Fatalf("после 20:00 в дне нет места: %v", taskIDs(slots))
	}
	if countBy(slots, "job") == 0 {
		t.Fatalf("несделанные задачи обязаны остаться в не влезших: %v", taskIDs(slots))
	}
}

func TestFrozenDoneSlotsSurviveAndCountTowardWindowShare(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	first := build(settings, due, 360)
	done := 0
	for i, sl := range first {
		if sl.WindowID != nil && *sl.WindowID == "w" && *sl.PeriodStartMin == 600 {
			done = i + 4
			break
		}
	}
	gone, src := "gone-task", "tasks"
	first[0].TaskExternalID, first[0].TaskSource, first[0].TaskTitle = &gone, &src, &gone
	from := *first[done].StartMin
	slots := buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, existing: first, frozen: done, from: from})
	checkInvariants(t, settings, due, slots)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "gone-task" {
		t.Fatalf("сделанный слот обязан остаться как был: %+v", slots[0])
	}
	if got := countBy(inWindow(slots, "w"), "w"); got != 11 {
		t.Fatalf("пересборка посреди окна держит долю от всего окна: работе %d, ожидалось 11; %v", got, taskIDs(inWindow(slots, "w")))
	}
}

func TestPinnedTaskStaysAndCountsTowardEffort(t *testing.T) {
	settings := settingsDay(540, 780, 4)
	due := []tasksclient.DueTask{dueTask("a", "A", 50), dueTask("b", "B", 50)}
	src, b := "tasks", "b"
	existing := []models.PlanSlot{{Date: "2026-10-05", Idx: 0, TaskSource: &src, TaskExternalID: &b, TaskTitle: &b, Pinned: true}}
	slots := buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, existing: existing, from: 540})
	checkInvariants(t, settings, due, slots)
	if slots[0].TaskExternalID == nil || *slots[0].TaskExternalID != "b" || !slots[0].Pinned {
		t.Fatalf("закреплённый слот обязан остаться за B: %+v", slots[0])
	}
	if countBy(slots, "b") != 2 || countBy(slots, "a") != 2 {
		t.Fatalf("закреплённый помидор засчитывается в трудозатраты B: %v", taskIDs(slots))
	}
}

func TestPinnedTaskOfCompletedTaskIsReleased(t *testing.T) {
	settings := settingsDay(540, 660, 4)
	src, gone := "tasks", "done-task"
	existing := []models.PlanSlot{{Date: "2026-10-05", Idx: 0, TaskSource: &src, TaskExternalID: &gone, TaskTitle: &gone, Pinned: true}}
	due := []tasksclient.DueTask{dueTask("b", "B", 25)}
	slots := buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, existing: existing, from: 540})
	if countBy(slots, "done-task") != 0 {
		t.Fatalf("закрепление за выполненной задачей обязано сняться: %v", taskIDs(slots))
	}
}

func TestPinnedDurationsStayInsidePeriod(t *testing.T) {
	settings := settingsDay(540, 660, 4)
	due := []tasksclient.DueTask{dueTask("a", "A", 300)}
	fifty := 3000
	existing := []models.PlanSlot{{Date: "2026-10-05", Idx: 1, FocusSeconds: &fifty, PinnedFocus: true}}
	slots := buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, existing: existing, from: 540})
	checkInvariants(t, settings, due, slots)
	main := scheduled(slots)
	if *main[1].FocusSeconds != 3000 || !main[1].PinnedFocus {
		t.Fatalf("ручная длина 50 минут обязана сохраниться: %+v", main[1])
	}
	if len(main) >= 4 {
		t.Fatalf("с 50-минутным помидором в два часа не влезают 4 помидора: %d", len(main))
	}
}

func TestPresetSlotsNeverCrossPeriods(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	preset := &models.Preset{Name: "long"}
	for i := 0; i < 30; i++ {
		preset.Slots = append(preset.Slots, models.PresetSlot{FocusMinutes: 50, BreakMinutes: 10})
	}
	slots := buildDay(buildInput{date: "2026-10-05", settings: settings, preset: preset, due: due, from: 360})
	checkInvariants(t, settings, due, slots)
	main := scheduled(slots)
	if len(main) == 0 || len(main) >= 30 {
		t.Fatalf("50-минутные помидоры пресета частично влезают в день: %d", len(main))
	}
	for _, sl := range main {
		if *sl.FocusSeconds != 3000 {
			t.Fatalf("длина пресета обязана сохраниться: %+v", sl)
		}
	}
}

func TestPlanBlocksFollowLongBreaksAndPeriods(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	blocks := planBlocks(build(settings, pavelsDay(), 360), durationsOf(settings))
	want := []int{2, 3, 1, 3, 3, 2, 3, 3, 2, 2}
	if len(blocks) != len(want) {
		t.Fatalf("блоки %v, ожидалось %v", blocks, want)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("блоки %v, ожидалось %v", blocks, want)
		}
	}
}
