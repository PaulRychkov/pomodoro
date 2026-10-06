package plan

import (
	"fmt"
	"testing"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

func matrixSettings(focusMin, shortMin, longMin, start, end, share int, blocks ...int) models.Settings {
	s := settingsDay(start, end, blocks...)
	s.FocusDurationSeconds = focusMin * 60
	s.ShortBreakSeconds = shortMin * 60
	s.LongBreakSeconds = longMin * 60
	s.WindowSharePercent = share
	return s
}

func TestSettingsMatrixKeepsPlanConsistent(t *testing.T) {
	due := pavelsDay()
	for _, focus := range []int{15, 25, 50} {
		for _, brk := range [][2]int{{3, 10}, {5, 15}, {10, 20}} {
			for _, blocks := range [][]int{{1}, {3}, {4, 4}} {
				for _, bounds := range [][2]int{{360, 1200}, {300, 1380}, {540, 1080}, {700, 760}} {
					for _, share := range []int{0, 34, 67, 100} {
						settings := matrixSettings(focus, brk[0], brk[1], bounds[0], bounds[1], share, blocks...)
						name := fmt.Sprintf("focus%d_break%v_blocks%v_day%v_share%d", focus, brk, blocks, bounds, share)
						t.Run(name, func(t *testing.T) {
							for from := bounds[0] - 30; from <= bounds[1]+10; from += 37 {
								slots := build(settings, due, max(from, bounds[0]))
								checkInvariants(t, settings, due, slots)
								checkWindowShare(t, settings, slots)
								rt := idle(max(from, bounds[0]))
								if p := project(slots, rt); p.displaced {
									t.Fatalf("план, собранный в %s, сразу вытеснен прогнозом: %v", clock(from), p.starts)
								}
							}
						})
					}
				}
			}
		}
	}
}

func checkWindowShare(t *testing.T, settings models.Settings, slots []models.PlanSlot) {
	t.Helper()
	window := inWindow(slots, "w")
	if len(window) == 0 {
		return
	}
	want := (len(window)*settings.WindowSharePercent + 50) / 100
	if got := countBy(window, "w"); got != want {
		t.Fatalf("доля окна %d%% из %d помидоров — %d работе, получено %d: %v",
			settings.WindowSharePercent, len(window), want, got, taskIDs(window))
	}
}

func TestRebuildAfterProgressKeepsShare(t *testing.T) {
	settings := settingsDay(360, 1200, 3)
	due := pavelsDay()
	slots := build(settings, due, 360)
	for done := 0; done < len(scheduled(slots)); done += 3 {
		if slots[done].StartMin == nil {
			break
		}
		from := *slots[done].StartMin + 7
		next := buildDay(buildInput{date: "2026-10-05", settings: settings, due: due, existing: slots, frozen: done, from: from})
		for i := 0; i < done; i++ {
			if taskIDs(next)[i] != taskIDs(slots)[i] {
				t.Fatalf("пересборка в %s изменила сделанный слот %d", clock(from), i)
			}
		}
		rest := next[done:]
		checkInvariants(t, settings, due, append(make([]models.PlanSlot, 0), renumber(rest)...))
		window := inWindow(scheduled(next), "w")
		if len(window) > 0 {
			want := (len(window)*settings.WindowSharePercent + 50) / 100
			if got := countBy(window, "w"); got < want-1 || got > want+1 {
				t.Fatalf("после %d сделанных доля окна уехала: %d из %d", done, got, len(window))
			}
		}
	}
}

func renumber(slots []models.PlanSlot) []models.PlanSlot {
	out := make([]models.PlanSlot, len(slots))
	for i, sl := range slots {
		sl.Idx = i
		out[i] = sl
	}
	return out
}
