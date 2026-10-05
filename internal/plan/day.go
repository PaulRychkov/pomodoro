package plan

import (
	"sort"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

// period — отрезок активного дня, куда укладываются помидоры: свободное время
// между событиями или окно задачи с фиксированным временем («Работа»).
// Помидор никогда не пересекает границу своего периода.
type period struct {
	start     int
	end       int
	origStart int
	window    *tasksclient.TaskOption
	// tailRest — следующий период начинается вплотную: в конце этого
	// оставляем короткий перерыв, чтобы следующий начался вовремя.
	tailRest bool
}

func (p period) windowID() string {
	if p.window == nil {
		return ""
	}
	return p.window.ExternalID
}

func dayBounds(s models.Settings) (int, int) {
	start, end := s.DayStartMin, s.DayEndMin
	if start < 0 || end > 24*60 || end-start < 30 {
		return models.DefaultDayStartMin, models.DefaultDayEndMin
	}
	return start, end
}

// dayPeriods режет активный день на периоды: события без помидоров (обед, зал)
// вырезаются, окна с помидорами становятся отдельными периодами, остальное —
// свободное время. Всё, что за границами активного дня, отбрасывается.
func dayPeriods(settings models.Settings, due []tasksclient.DueTask) []period {
	dayStart, dayEnd := dayBounds(settings)
	var out []period
	add := func(p period) {
		if p.end <= p.start {
			return
		}
		if n := len(out); n > 0 && out[n-1].end == p.start {
			out[n-1].tailRest = true
		}
		p.origStart = p.start
		out = append(out, p)
	}
	cursor := dayStart
	for _, it := range dayIntervals(due) {
		start, end := max(it.start, cursor), min(it.start+it.length, dayEnd)
		if end <= start {
			continue
		}
		if start > cursor {
			add(period{start: cursor, end: start})
		}
		if !it.blocked {
			opt := it.option
			add(period{start: start, end: end, window: &opt})
		}
		cursor = end
	}
	if cursor < dayEnd {
		add(period{start: cursor, end: dayEnd})
	}
	return out
}

// clipPeriods оставляет от периодов только время начиная с минуты from.
func clipPeriods(ps []period, from int) []period {
	var out []period
	for _, p := range ps {
		if p.end <= from {
			continue
		}
		if p.start < from {
			p.start = from
		}
		out = append(out, p)
	}
	return out
}

type durations struct {
	focus int
	short int
	long  int
	block int
}

func durationsOf(s models.Settings) durations {
	d := durations{focus: s.FocusDurationSeconds / 60, short: s.ShortBreakSeconds / 60, long: s.LongBreakSeconds / 60}
	if d.focus <= 0 {
		d.focus = 25
	}
	if d.short <= 0 {
		d.short = 5
	}
	if d.long <= 0 {
		d.long = d.short
	}
	for _, b := range s.DayBlocks {
		if b > 0 {
			d.block = b
			break
		}
	}
	if d.block <= 0 {
		d.block = 4
	}
	return d
}

// shape — место помидора в дне: длительности, плановый старт и период.
type shape struct {
	focus  int
	brk    int
	start  int
	period int
	pin    *models.PlanSlot
}

func usableEnd(p period, d durations) int {
	if p.tailRest {
		return p.end - d.short
	}
	return p.end
}

// fitLayout укладывает помидоры в каждый период ровно: длина фокуса ужимается
// не ниже 2/3 настроенной, перерывы не короче настроенных, длинный перерыв —
// после каждого блока внутри периода.
func fitLayout(ps []period, d durations) []shape {
	cfg := DefaultFitConfig(d.focus, d.short, d.long, d.block)
	cfg.MinShort = d.short
	cfg.MinLong = d.long
	var out []shape
	for pi, p := range ps {
		fit := FitPeriod(usableEnd(p, d)-p.start, cfg)
		t := p.start
		for k := 0; k < fit.Count; k++ {
			brk := fit.Short
			switch {
			case k+1 == fit.Count:
				// После последнего помидора периода — обычный короткий
				// перерыв: следующий период начнётся по своему времени.
				brk = d.short
			case (k+1)%d.block == 0:
				brk = fit.Long
			}
			out = append(out, shape{focus: fit.Focus, brk: brk, start: t, period: pi})
			t += fit.Focus + brk
		}
	}
	return out
}

// packLayout раскладывает помидоры заданных длин по порядку: не влезающий в
// период помидор переносится в следующий, не влезающие в день отбрасываются.
func packLayout(ps []period, durs []slotDuration, d durations) []shape {
	var out []shape
	pi := 0
	t := 0
	if len(ps) > 0 {
		t = ps[0].start
	}
	for _, sd := range durs {
		for pi < len(ps) && t+sd.focus > usableEnd(ps[pi], d) {
			pi++
			if pi < len(ps) {
				t = ps[pi].start
			}
		}
		if pi == len(ps) {
			break
		}
		out = append(out, shape{focus: sd.focus, brk: sd.brk, start: t, period: pi})
		t += sd.focus + sd.brk
	}
	return out
}

// applyPins привязывает ручные правки слотов к местам дня и, если ручные
// длительности не помещаются в период, освобождает место: сначала убирает
// последние незакреплённые помидоры периода. Возвращает раскладку и правки,
// которым места не нашлось.
func applyPins(shapes []shape, ps []period, base int, pinned map[int]models.PlanSlot, d durations) ([]shape, []models.PlanSlot) {
	for i := range shapes {
		sl, ok := pinned[base+i]
		if !ok {
			continue
		}
		shapes[i].pin = &sl
		if sl.PinnedFocus && sl.FocusSeconds != nil && *sl.FocusSeconds >= 60 {
			shapes[i].focus = *sl.FocusSeconds / 60
		}
		if sl.PinnedBreak && sl.BreakSeconds != nil && *sl.BreakSeconds >= 60 {
			shapes[i].brk = *sl.BreakSeconds / 60
		}
	}
	var lost []models.PlanSlot
	for i, sl := range pinned {
		if i >= base+len(shapes) {
			lost = append(lost, sl)
		}
	}
	sort.Slice(lost, func(a, b int) bool { return lost[a].Idx < lost[b].Idx })

	var out []shape
	for lo := 0; lo < len(shapes); {
		hi := lo
		for hi < len(shapes) && shapes[hi].period == shapes[lo].period {
			hi++
		}
		group := append([]shape(nil), shapes[lo:hi]...)
		p := ps[shapes[lo].period]
		for len(group) > 0 && !chainFits(group, p, d) {
			drop := len(group) - 1
			for j := len(group) - 1; j >= 0; j-- {
				if group[j].pin == nil {
					drop = j
					break
				}
			}
			if group[drop].pin != nil {
				lost = append(lost, *group[drop].pin)
			}
			group = append(group[:drop], group[drop+1:]...)
		}
		out = append(out, group...)
		lo = hi
	}
	return out, lost
}

// chainFits перестраивает старты помидоров периода по их длительностям и
// проверяет, что последний фокус заканчивается внутри периода.
func chainFits(group []shape, p period, d durations) bool {
	t := group[0].start
	for i := range group {
		group[i].start = t
		t += group[i].focus + group[i].brk
	}
	last := group[len(group)-1]
	return last.start+last.focus <= usableEnd(p, d)
}

// windowShare решает, какие помидоры окна достаются его задаче: доля
// window_share_percent считается от всех помидоров окна за день (включая уже
// сделанные), остальные отдаются другим задачам и разносятся по окну равномерно.
func windowShare(shapes []shape, ps []period, frozen []models.PlanSlot, sharePercent int) map[int]bool {
	toWindow := map[int]bool{}
	if sharePercent <= 0 {
		return toWindow
	}
	if sharePercent > 100 {
		sharePercent = 100
	}
	byWindow := map[string][]int{}
	var order []string
	for i, sh := range shapes {
		id := ps[sh.period].windowID()
		if id == "" {
			continue
		}
		if _, seen := byWindow[id]; !seen {
			order = append(order, id)
		}
		byWindow[id] = append(byWindow[id], i)
	}
	for _, id := range order {
		idxs := byWindow[id]
		total := len(idxs)
		have := 0
		for _, sl := range frozen {
			if sl.WindowID != nil && *sl.WindowID == id {
				total++
				if sl.TaskExternalID != nil && *sl.TaskExternalID == id {
					have++
				}
			}
		}
		var free []int
		for _, i := range idxs {
			pin := shapes[i].pin
			if pin == nil || (pin.TaskExternalID == nil && pin.Label == nil) {
				free = append(free, i)
				continue
			}
			if pin.TaskExternalID != nil && *pin.TaskExternalID == id {
				have++
			}
		}
		need := (total*sharePercent+50)/100 - have
		need = max(0, min(need, len(free)))
		for j := range free {
			if (j+1)*need/len(free) > j*need/len(free) {
				toWindow[free[j]] = true
			}
		}
	}
	return toWindow
}

type buildInput struct {
	date     string
	settings models.Settings
	preset   *models.Preset
	due      []tasksclient.DueTask
	existing []models.PlanSlot
	// frozen — сколько первых слотов уже сделано или идёт: их не трогаем.
	frozen int
	// from — минута, с которой раскладывается остаток дня.
	from int
}

// buildDay строит план дня: сделанные слоты остаются как есть, остаток дня от
// from раскладывается по периодам, окна получают свою долю, гибкие задачи —
// оставшиеся места по трудозатратам и приоритету, излишек уходит в «не влезшие».
func buildDay(in buildInput) []models.PlanSlot {
	d := durationsOf(in.settings)
	valid := map[string]bool{}
	for _, t := range in.due {
		valid[t.Option.ExternalID] = true
	}
	byIdx := map[int]models.PlanSlot{}
	for _, sl := range in.existing {
		byIdx[sl.Idx] = sl
	}
	out := make([]models.PlanSlot, 0, len(in.existing))
	for i := 0; i < in.frozen; i++ {
		sl, ok := byIdx[i]
		if !ok {
			sl = models.PlanSlot{Date: in.date, Idx: i}
		}
		out = append(out, sl)
	}
	frozen := append([]models.PlanSlot(nil), out...)

	pinned := map[int]models.PlanSlot{}
	for _, sl := range in.existing {
		if sl.Idx < in.frozen || !(sl.Pinned || sl.PinnedFocus || sl.PinnedBreak) {
			continue
		}
		if sl.TaskExternalID != nil && !valid[*sl.TaskExternalID] {
			sl.SetTask(nil)
			sl.Pinned = false
			if !sl.PinnedFocus && !sl.PinnedBreak {
				continue
			}
		}
		pinned[sl.Idx] = sl
	}

	ps := clipPeriods(dayPeriods(in.settings, in.due), in.from)
	var shapes []shape
	if in.preset != nil {
		var durs []slotDuration
		for i := in.frozen; i < len(in.preset.Slots); i++ {
			slot := in.preset.Slots[i]
			sd := slotDuration{focus: slot.FocusMinutes, brk: slot.BreakMinutes}
			if sd.focus <= 0 {
				sd.focus = d.focus
			}
			if sd.brk <= 0 {
				sd.brk = d.short
			}
			durs = append(durs, sd)
		}
		shapes = packLayout(ps, durs, d)
	} else {
		shapes = fitLayout(ps, d)
	}
	shapes, lost := applyPins(shapes, ps, in.frozen, pinned, d)

	planned := map[string]int{}
	for _, sl := range frozen {
		if sl.TaskExternalID != nil {
			planned[*sl.TaskExternalID] += focusMinutes(sl, d.focus)
		}
	}
	for _, sh := range shapes {
		if sh.pin != nil && sh.pin.TaskExternalID != nil {
			planned[*sh.pin.TaskExternalID] += sh.focus
		}
	}
	for _, sl := range lost {
		if sl.TaskExternalID != nil {
			planned[*sl.TaskExternalID] += focusMinutes(sl, d.focus)
		}
	}

	toWindow := windowShare(shapes, ps, frozen, in.settings.WindowSharePercent)
	var pool []int
	poolFocus := 0
	for i, sh := range shapes {
		if toWindow[i] || (sh.pin != nil && (sh.pin.TaskExternalID != nil || sh.pin.Label != nil)) {
			continue
		}
		pool = append(pool, i)
		poolFocus += sh.focus
	}
	avgFocus := d.focus
	if len(pool) > 0 {
		avgFocus = max(1, poolFocus/len(pool))
	}

	windows := map[string]bool{}
	for _, p := range dayPeriods(in.settings, in.due) {
		if id := p.windowID(); id != "" {
			windows[id] = true
		}
	}
	ordered := append([]tasksclient.DueTask(nil), in.due...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return priorityWeightPercent(ordered[i].Priority) > priorityWeightPercent(ordered[j].Priority)
	})
	var needs []flexNeed
	for _, t := range ordered {
		if t.Blocked || t.StartTimeMin != nil || windows[t.Option.ExternalID] {
			continue
		}
		remaining := d.focus
		if t.EffortMin != nil && *t.EffortMin > 0 {
			remaining = *t.EffortMin
		}
		remaining -= planned[t.Option.ExternalID]
		if remaining <= 0 {
			continue
		}
		poms := max(1, (remaining+avgFocus/2)/avgFocus)
		needs = append(needs, flexNeed{
			task:      t.Option,
			remaining: remaining,
			slots:     poms,
			weight:    poms * priorityWeightPercent(t.Priority),
		})
	}
	quota := shareByWeight(len(pool), needs, func(n flexNeed) int { return n.weight })
	assigned := map[int]tasksclient.TaskOption{}
	pos := 0
	for i := range needs {
		for k := 0; k < quota[i] && pos < len(pool); k++ {
			assigned[pool[pos]] = needs[i].task
			pos++
		}
	}

	for i, sh := range shapes {
		p := ps[sh.period]
		sl := models.PlanSlot{Date: in.date, Idx: in.frozen + i}
		if sh.pin != nil {
			sl.Label = sh.pin.Label
			sl.SetTask(sh.pin.Task())
			sl.Pinned = sh.pin.Pinned
			sl.PinnedFocus = sh.pin.PinnedFocus
			sl.PinnedBreak = sh.pin.PinnedBreak
		}
		focusSec, brkSec := sh.focus*60, sh.brk*60
		sl.FocusSeconds, sl.BreakSeconds = &focusSec, &brkSec
		start, pStart, pEnd := sh.start, p.origStart, p.end
		sl.StartMin, sl.PeriodStartMin, sl.PeriodEndMin = &start, &pStart, &pEnd
		if p.window != nil {
			id, title := p.window.ExternalID, p.window.Title
			sl.WindowID, sl.WindowTitle = &id, &title
			if toWindow[i] {
				sl.SetTask(&models.TaskRef{Source: p.window.Source, ExternalID: id, TitleSnapshot: title})
			}
		}
		if t, ok := assigned[i]; ok {
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
		}
		out = append(out, sl)
	}

	next := len(out)
	addOverflow := func(sl models.PlanSlot) {
		if next-len(frozen)-len(shapes) >= maxOverflowSlots {
			return
		}
		sl.Date, sl.Idx, sl.Overflow = in.date, next, true
		sl.StartMin, sl.PeriodStartMin, sl.PeriodEndMin, sl.WindowID, sl.WindowTitle = nil, nil, nil, nil, nil
		out = append(out, sl)
		next++
	}
	for _, sl := range lost {
		if sl.TaskExternalID == nil && sl.Label == nil {
			continue
		}
		addOverflow(sl)
	}
	for i, n := range needs {
		for k := quota[i]; k < n.slots; k++ {
			sl := models.PlanSlot{}
			sl.SetTask(&models.TaskRef{Source: n.task.Source, ExternalID: n.task.ExternalID, TitleSnapshot: n.task.Title})
			addOverflow(sl)
		}
	}
	return out
}

func focusMinutes(sl models.PlanSlot, def int) int {
	if sl.FocusSeconds != nil && *sl.FocusSeconds >= 60 {
		return *sl.FocusSeconds / 60
	}
	return def
}

func breakMinutes(sl models.PlanSlot, def int) int {
	if sl.BreakSeconds != nil && *sl.BreakSeconds >= 60 {
		return *sl.BreakSeconds / 60
	}
	return def
}

// planBlocks — блоки плана по порядку: блок кончается длинным перерывом или
// границей периода. Слоты без места в дне в блоки не входят.
func planBlocks(rows []models.PlanSlot, d durations) []int {
	var blocks []int
	n := 0
	for i, sl := range rows {
		if sl.Overflow {
			continue
		}
		n++
		end := i+1 == len(rows) || rows[i+1].Overflow || !samePeriod(sl, rows[i+1]) ||
			breakMinutes(sl, d.short) >= d.long && d.long > d.short
		if end {
			blocks = append(blocks, n)
			n = 0
		}
	}
	return blocks
}

func samePeriod(a, b models.PlanSlot) bool {
	return eqInt(a.PeriodEndMin, b.PeriodEndMin) && eqStr(a.WindowID, b.WindowID)
}

func eqInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func eqStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
