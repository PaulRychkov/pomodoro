package plan

import "github.com/PaulRychkov/pomodoro/internal/models"

type slotDuration struct {
	focus int
	brk   int
}

type activeSlot struct {
	startMin     int
	remainingMin int
	isFocus      bool
}

// dayRuntime — факт дня в минутах от полуночи.
type dayRuntime struct {
	now       int
	completed int
	// past — старты сделанных помидоров по возрастанию.
	past   []int
	active *activeSlot
	// lastFocusEnd — конец последнего помидора, если после него ещё не было
	// перерыва (перерыв «ждёт»); -1 — нет.
	lastFocusEnd int
}

// frozen — слоты, которые пересборка не трогает: сделанные и идущий фокус.
func (rt dayRuntime) frozen() int {
	if rt.active != nil && rt.active.isFocus {
		return rt.completed + 1
	}
	return rt.completed
}

type projection struct {
	// starts — прогноз старта каждого слота; -1 — у слота нет места в дне.
	starts []int
	// from — с какой минуты свободен остаток дня (после идущего помидора
	// или перерыва).
	from int
	// displaced — несделанный слот уже не успевает закончиться в своём периоде.
	displaced bool
}

// project считает время каждого слота от факта: сделанные показывают реальный
// старт, идущий держит свой, остальные идут цепочкой от текущего момента, но
// не раньше своего планового времени и никогда не выходя за конец своего
// периода (окна, свободного промежутка, активного дня).
func project(rows []models.PlanSlot, rt dayRuntime) projection {
	p := projection{starts: make([]int, len(rows))}
	for i := range p.starts {
		p.starts[i] = -1
	}
	for i := 0; i < rt.completed && i < len(rows) && i < len(rt.past); i++ {
		p.starts[i] = rt.past[i]
	}
	next := rt.completed
	cursor := rt.now
	switch {
	case rt.active != nil && rt.active.isFocus:
		cursor = rt.now + rt.active.remainingMin
		if next < len(rows) {
			p.starts[next] = rt.active.startMin
			cursor += breakMinutes(rows[next], 0)
		}
		next++
	case rt.active != nil:
		cursor = rt.now + rt.active.remainingMin
	case rt.lastFocusEnd >= 0 && rt.completed > 0 && rt.completed <= len(rows):
		cursor = max(cursor, rt.lastFocusEnd+breakMinutes(rows[rt.completed-1], 0))
	}
	p.from = cursor
	for i := next; i < len(rows); i++ {
		sl := rows[i]
		if sl.Overflow || sl.StartMin == nil || sl.PeriodEndMin == nil {
			continue
		}
		focus := focusMinutes(sl, 0)
		start := max(cursor, *sl.StartMin)
		if focus <= 0 || start+focus > *sl.PeriodEndMin {
			p.displaced = true
			continue
		}
		p.starts[i] = start
		cursor = start + focus + breakMinutes(sl, 0)
	}
	return p
}
