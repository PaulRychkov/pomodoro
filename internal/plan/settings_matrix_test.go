package plan

import (
	"fmt"
	"testing"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

func matrixSettings(focusMin, shortMin, longMin, before, after, share int, blocks ...int) models.Settings {
	s := models.DefaultSettings()
	s.FocusDurationSeconds = focusMin * 60
	s.ShortBreakSeconds = shortMin * 60
	s.LongBreakSeconds = longMin * 60
	s.DayBlocks = blocks
	s.PlanBeforeWindowMin = before
	s.PlanAfterWindowMin = after
	s.WindowSharePercent = share
	return s
}

func matrixDue() []tasksclient.DueTask {
	return []tasksclient.DueTask{
		workTask("w", "Работа", 540, 240),
		blockedTask("gym", "Зал", 420, 40),
		priorityTask("job", "Поиск работы", 690, 3),
		priorityTask("eng", "Английский", 60, 2),
		priorityTask("type", "Слепая печать", 40, 2),
	}
}

func TestSettingsMatrixKeepsPlanConsistent(t *testing.T) {
	focuses := []int{15, 25, 30, 50}
	shorts := []int{3, 5, 10}
	longs := []int{10, 15, 20}
	blockSets := [][]int{{1}, {3, 3}, {4, 4}, {3, 3, 3, 3, 3}, {3, 3, 3, 3, 3, 3, 3, 3, 1}}
	pads := [][2]int{{0, 0}, {80, 90}, {120, 0}}
	shares := []int{0, 34, 67, 100}

	due := matrixDue()
	for _, focus := range focuses {
		for _, short := range shorts {
			for _, long := range longs {
				for _, blocks := range blockSets {
					for _, pad := range pads {
						for _, share := range shares {
							settings := matrixSettings(focus, short, long, pad[0], pad[1], share, blocks...)
							name := fmt.Sprintf("focus%d_short%d_long%d_blocks%v_pad%d-%d_share%d",
								focus, short, long, blocks, pad[0], pad[1], share)
							t.Run(name, func(t *testing.T) {
								checkPlan(t, settings, due)
							})
						}
					}
				}
			}
		}
	}
}

func checkPlan(t *testing.T, settings models.Settings, due []tasksclient.DueTask) {
	t.Helper()
	layout := computeDayLayout(settings, dayIntervals(due))
	if layout == nil {
		t.Fatalf("день с окнами обязан давать раскладку")
	}
	focusMin := settings.FocusDurationSeconds / 60
	shortMin := settings.ShortBreakSeconds / 60
	longMin := settings.LongBreakSeconds / 60
	cfg := DefaultFitConfig(focusMin, shortMin, longMin, settings.DayBlocks[0])
	for i, sh := range layout.shapes {
		if sh.focus > focusMin {
			t.Fatalf("слот %d: фокус %d длиннее настроенного %d", i, sh.focus, focusMin)
		}
		if sh.focus < cfg.MinFocus {
			t.Fatalf("слот %d: фокус %d короче нижней границы %d", i, sh.focus, cfg.MinFocus)
		}
		if sh.brk < shortMin {
			t.Fatalf("слот %d: перерыв %d короче настроенного короткого %d", i, sh.brk, shortMin)
		}
	}

	committed := sum(settings.DayBlocks)
	slots := buildSlots("2026-08-01", committed, nil, due, nil, settings, true, 0, layout, committed)
	if len(slots) < committed {
		t.Fatalf("основных слотов %d, обещано блоками %d", len(slots), committed)
	}
	if len(slots) > committed+maxOverflowSlots {
		t.Fatalf("хвост не влезших превысил предел: всего %d слотов при %d основных", len(slots), committed)
	}
	for i, sl := range slots {
		if sl.Idx != i {
			t.Fatalf("слот %d имеет индекс %d — нумерация плана порвана", i, sl.Idx)
		}
		if sl.FocusSeconds != nil && *sl.FocusSeconds <= 0 {
			t.Fatalf("слот %d получил неположительную длину фокуса %d", i, *sl.FocusSeconds)
		}
	}
	if countBy(slots, "gym") != 0 {
		t.Fatalf("событие без помидоров попало в план: %v", taskIDs(slots))
	}
	work := countBy(slots[:committed], "w")
	if settings.WindowSharePercent == 0 && work != 0 {
		t.Fatalf("нулевая доля окна обязана убрать работу из основных слотов, получено %d", work)
	}
	if settings.WindowSharePercent == 100 && committed >= layout.total && work == 0 {
		t.Fatalf("план покрывает день целиком, окно обязано получить слоты: %v", taskIDs(slots[:committed]))
	}
}

func TestBlocksShorterThanDayCutTailWithWindow(t *testing.T) {
	due := matrixDue()
	settings := matrixSettings(15, 5, 15, 80, 90, 100, 3, 3)
	layout := computeDayLayout(settings, dayIntervals(due))
	committed := sum(settings.DayBlocks)
	if committed >= layout.total {
		t.Fatalf("кейс требует блоков короче ёмкости дня: блоки %d, ёмкость %d", committed, layout.total)
	}
	slots := buildSlots("2026-08-01", committed, nil, due, nil, settings, true, 0, layout, committed)
	if countBy(slots, "w") != 0 {
		t.Fatalf("Д17 исправлен: окно снова попадает в план (%v) — тест пора переписать на новое поведение", taskIDs(slots))
	}
}

func TestWindowShareIsMonotonic(t *testing.T) {
	due := matrixDue()
	for _, focus := range []int{15, 25, 50} {
		for _, blocks := range [][]int{{3, 3}, {3, 3, 3, 3, 3}} {
			prev := -1
			for _, share := range []int{0, 34, 67, 100} {
				settings := matrixSettings(focus, 5, 15, 80, 90, share, blocks...)
				layout := computeDayLayout(settings, dayIntervals(due))
				committed := sum(settings.DayBlocks)
				slots := buildSlots("2026-08-01", committed, nil, due, nil, settings, true, 0, layout, committed)
				work := countBy(slots[:committed], "w")
				if work < prev {
					t.Fatalf("focus=%d blocks=%v: доля %d%% дала %d слотов работы против %d на меньшей доле",
						focus, blocks, share, work, prev)
				}
				prev = work
			}
		}
	}
}

func TestPaddingsChangeCapacity(t *testing.T) {
	due := matrixDue()
	capacity := func(before, after int) int {
		settings := matrixSettings(25, 5, 15, before, after, 67, 3, 3, 3, 3, 3)
		return computeDayLayout(settings, dayIntervals(due)).total
	}
	none := capacity(0, 0)
	morning := capacity(80, 0)
	both := capacity(80, 90)
	if morning <= none || both <= morning {
		t.Fatalf("отступы обязаны наращивать ёмкость дня: 0/0=%d, 80/0=%d, 80/90=%d", none, morning, both)
	}
}

func TestBlocksDefineMainSlotsForEverySetting(t *testing.T) {
	due := matrixDue()
	for _, blocks := range [][]int{{1}, {4, 4}, {3, 3, 3, 3, 3}, {3, 3, 3, 3, 3, 3, 3, 3, 1}} {
		settings := matrixSettings(25, 5, 15, 80, 90, 67, blocks...)
		layout := computeDayLayout(settings, dayIntervals(due))
		committed := sum(settings.DayBlocks)
		slots := buildSlots("2026-08-01", committed, nil, due, nil, settings, true, 0, layout, committed)
		main := 0
		for _, sl := range slots {
			if sl.Idx < committed {
				main++
			}
		}
		if main != committed {
			t.Fatalf("блоки %v обещают %d основных слотов, в плане %d", blocks, committed, main)
		}
	}
}
