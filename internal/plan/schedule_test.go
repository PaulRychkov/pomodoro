package plan

import (
	"testing"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

func ptr(v int) *int { return &v }

// row — слот с местом в дне: старт, длительности и период в минутах.
func row(idx, start, focus, brk, pStart, pEnd int) models.PlanSlot {
	return models.PlanSlot{
		Idx: idx, StartMin: ptr(start), FocusSeconds: ptr(focus * 60), BreakSeconds: ptr(brk * 60),
		PeriodStartMin: ptr(pStart), PeriodEndMin: ptr(pEnd),
	}
}

// morning — 8:00–10:00 четыре помидора, работа с 10:00 два помидора.
func morning() []models.PlanSlot {
	work := "w"
	rows := []models.PlanSlot{
		row(0, 480, 25, 5, 480, 600),
		row(1, 510, 25, 5, 480, 600),
		row(2, 540, 25, 5, 480, 600),
		row(3, 570, 25, 5, 480, 600),
		row(4, 600, 25, 5, 600, 840),
		row(5, 630, 25, 5, 600, 840),
	}
	rows[4].WindowID, rows[5].WindowID = &work, &work
	return rows
}

func idle(now int) dayRuntime {
	return dayRuntime{now: now, lastFocusEnd: -1}
}

func TestProjectKeepsPlannedTimesWhenAhead(t *testing.T) {
	p := project(morning(), idle(7*60))
	want := []int{480, 510, 540, 570, 600, 630}
	for i, w := range want {
		if p.starts[i] != w {
			t.Fatalf("слот %d: старт %d, ожидалось %d (%v)", i, p.starts[i], w, p.starts)
		}
	}
	if p.displaced {
		t.Fatalf("утром до начала ничего не вытеснено")
	}
}

func TestProjectLateStartShiftsOnlyInsidePeriod(t *testing.T) {
	p := project(morning(), idle(8*60+6))
	if p.starts[0] != 486 || p.starts[1] != 516 || p.starts[2] != 546 {
		t.Fatalf("опоздание на 6 минут сдвигает хвост периода: %v", p.starts)
	}
	if p.starts[3] != -1 || !p.displaced {
		t.Fatalf("последний помидор не успевает до 10:00 и должен быть вытеснен, а не залезть в работу: %v", p.starts)
	}
	if p.starts[4] != 600 {
		t.Fatalf("работа начинается в 10:00 при любом опоздании утром: %v", p.starts)
	}
}

func TestProjectWindowSlotsNeverStartEarly(t *testing.T) {
	rt := idle(9*60 + 10)
	rt.completed = 4
	rt.past = []int{480, 510, 540, 570}
	p := project(morning(), rt)
	if p.starts[4] != 600 {
		t.Fatalf("утро сделано к 9:10, но помидор работы всё равно в 10:00: %v", p.starts)
	}
	for i, w := range rt.past {
		if p.starts[i] != w {
			t.Fatalf("сделанный слот %d показывает факт %d, получено %d", i, w, p.starts[i])
		}
	}
}

func TestProjectRunningFocusAndItsBreak(t *testing.T) {
	rt := idle(8*60 + 20)
	rt.active = &activeSlot{startMin: 8*60 + 2, remainingMin: 7, isFocus: true}
	p := project(morning(), rt)
	if p.starts[0] != 482 {
		t.Fatalf("идущий помидор держит свой реальный старт: %v", p.starts)
	}
	if p.starts[1] != 8*60+20+7+5 {
		t.Fatalf("следующий — после остатка фокуса и перерыва: %v", p.starts)
	}
	if p.from != 8*60+32 {
		t.Fatalf("остаток дня свободен с 8:32, получено %d", p.from)
	}
}

func TestProjectPausedFocusUsesRemaining(t *testing.T) {
	rt := idle(8*60 + 40)
	rt.active = &activeSlot{startMin: 8 * 60, remainingMin: 15, isFocus: true}
	p := project(morning(), rt)
	if p.starts[1] != 8*60+40+15+5 {
		t.Fatalf("на паузе остаток фокуса не тает: следующий в %d, ожидалось %d", p.starts[1], 8*60+60)
	}
}

func TestProjectPendingBreakDelaysNextSlot(t *testing.T) {
	rt := idle(8*60 + 26)
	rt.completed = 1
	rt.past = []int{480}
	rt.lastFocusEnd = 8*60 + 25
	p := project(morning(), rt)
	if p.starts[1] != 8*60+30 {
		t.Fatalf("после помидора сначала перерыв: следующий в %d, ожидалось 8:30", p.starts[1])
	}
}

func TestProjectBreakInProgress(t *testing.T) {
	rt := idle(8*60 + 27)
	rt.completed = 1
	rt.past = []int{480}
	rt.active = &activeSlot{startMin: 8*60 + 25, remainingMin: 8}
	p := project(morning(), rt)
	if p.starts[1] != 8*60+35 {
		t.Fatalf("затянутый перерыв сдвигает следующий помидор: %v", p.starts)
	}
}

func TestProjectOverflowHasNoTime(t *testing.T) {
	rows := append(morning(), models.PlanSlot{Idx: 6, Overflow: true})
	p := project(rows, idle(7*60))
	if p.starts[6] != -1 || p.displaced {
		t.Fatalf("не влезший слот без времени и не считается вытесненным: %v", p.starts)
	}
}
