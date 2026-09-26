package plan

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type Service struct {
	Store     store.Store
	Tasks     *tasksclient.Client
	Settings  func() models.Settings
	Completed func() int
}

type SlotView struct {
	Idx          int             `json:"idx"`
	Task         *models.TaskRef `json:"task"`
	Label        *string         `json:"label"`
	FocusMinutes *int            `json:"focus_minutes"`
	BreakMinutes *int            `json:"break_minutes"`
	StartMinutes *int            `json:"start_minutes"`
	Pinned       bool            `json:"pinned"`
	Done         bool            `json:"done"`
	Overflow     bool            `json:"overflow"`
}

type SlotUpdate struct {
	Task         *models.TaskRef
	Label        *string
	ClearBinding bool
	FocusMinutes *int
	BreakMinutes *int
}

type ScheduleEntry struct {
	Weekday    int    `json:"weekday"`
	PresetName string `json:"preset_name"`
}

func today() string {
	return time.Now().Local().Format("2006-01-02")
}

func (s *Service) Day(ctx context.Context, refresh bool) ([]SlotView, error) {
	date := today()
	settings := s.Settings()
	preset, err := s.presetFor(ctx, time.Now().Local())
	if err != nil {
		return nil, err
	}
	existing, err := s.Store.Plans().ListDay(ctx, date)
	if err != nil {
		return nil, err
	}
	due, dueErr := s.Tasks.DueToday(ctx, time.Now().Local())
	tasksOK := dueErr == nil
	layout := computeDayLayout(settings, dayIntervals(due))
	committed := sum(settings.DayBlocks)
	if preset != nil {
		committed = len(preset.Slots)
	}
	if committed == 0 && layout != nil {
		committed = layout.total
	}
	total := committed
	if !tasksOK && len(existing) > 0 {
		total = len(existing)
	}
	completed := s.Completed()
	if !tasksOK && len(existing) > 0 {
		refresh = false
	}
	if refresh || len(existing) == 0 || len(existing) < committed {
		existing = buildSlots(date, total, preset, due, existing, settings, tasksOK, completed, layout, committed)
		if err := s.Store.Plans().ReplaceDay(ctx, date, existing); err != nil {
			return nil, err
		}
	}
	starts := s.projectDayStarts(ctx, existing, settings, completed)
	views := make([]SlotView, 0, total)
	for i, sl := range existing {
		view := SlotView{
			Idx:          sl.Idx,
			Task:         sl.Task(),
			Label:        sl.Label,
			FocusMinutes: secToMin(sl.FocusSeconds),
			BreakMinutes: secToMin(sl.BreakSeconds),
			Pinned:       sl.Pinned,
			Done:         sl.Idx < completed,
			Overflow:     committed > 0 && sl.Idx >= committed,
		}
		if i < len(starts) && starts[i] >= 0 {
			start := starts[i]
			view.StartMinutes = &start
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) projectDayStarts(ctx context.Context, slots []models.PlanSlot, settings models.Settings, completed int) []int {
	defFocus := settings.FocusDurationSeconds / 60
	if defFocus <= 0 {
		defFocus = 25
	}
	defBreak := settings.ShortBreakSeconds / 60
	if defBreak < 0 {
		defBreak = 0
	}
	durs := make([]slotDuration, len(slots))
	for i, sl := range slots {
		d := slotDuration{focus: defFocus, brk: defBreak}
		if m := secToMin(sl.FocusSeconds); m != nil && *m > 0 {
			d.focus = *m
		}
		if m := secToMin(sl.BreakSeconds); m != nil && *m > 0 {
			d.brk = *m
		}
		durs[i] = d
	}
	now := time.Now().Local()
	nowMin := now.Hour()*60 + now.Minute()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var past []int
	if sessions, err := s.Store.Sessions().ListRange(ctx, dayStart, now); err == nil {
		for _, ses := range sessions {
			if ses.Kind != models.KindFocus || ses.Outcome == nil || *ses.Outcome != models.OutcomeCompleted {
				continue
			}
			started := ses.StartedAt.Local()
			past = append(past, started.Hour()*60+started.Minute())
		}
	}
	if len(past) > completed {
		past = past[:completed]
	}

	var active *activeSlot
	if a, err := s.Store.Sessions().FindActive(ctx); err == nil && a != nil {
		started := a.StartedAt.Local()
		remaining := a.PlannedDurationSeconds/60 - int(now.Sub(a.StartedAt).Minutes())
		if remaining < 0 {
			remaining = 0
		}
		active = &activeSlot{
			startMin:     started.Hour()*60 + started.Minute(),
			remainingMin: remaining,
			isFocus:      a.Kind == models.KindFocus,
		}
	}
	return projectStarts(durs, past, nowMin, active)
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func (s *Service) SetSlot(ctx context.Context, idx int, upd SlotUpdate) ([]SlotView, error) {
	if idx < 0 {
		return nil, fmt.Errorf("bad slot index %d", idx)
	}
	date := today()
	existing, err := s.Store.Plans().ListDay(ctx, date)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 && idx >= len(existing) {
		return nil, fmt.Errorf("slot index %d is out of range: план дня содержит %d слотов", idx, len(existing))
	}
	sl := models.PlanSlot{Date: date, Idx: idx}
	for _, e := range existing {
		if e.Idx == idx {
			sl = e
			break
		}
	}
	switch {
	case upd.Task != nil:
		sl.SetTask(upd.Task)
		sl.Label = nil
	case upd.Label != nil && *upd.Label != "":
		sl.SetTask(nil)
		sl.Label = upd.Label
	case upd.ClearBinding:
		sl.SetTask(nil)
		sl.Label = nil
	}
	if upd.FocusMinutes != nil {
		sl.FocusSeconds = minToSec(upd.FocusMinutes)
	}
	if upd.BreakMinutes != nil {
		sl.BreakSeconds = minToSec(upd.BreakMinutes)
	}
	sl.Pinned = sl.TaskSource != nil || sl.Label != nil || sl.FocusSeconds != nil || sl.BreakSeconds != nil
	if err := s.Store.Plans().Upsert(ctx, &sl); err != nil {
		return nil, err
	}
	return s.Day(ctx, true)
}

func (s *Service) SlotDurations(idx int) (focusSeconds, breakSeconds *int) {
	slots, err := s.Store.Plans().ListDay(context.Background(), today())
	if err != nil {
		return nil, nil
	}
	for _, sl := range slots {
		if sl.Idx == idx {
			return sl.FocusSeconds, sl.BreakSeconds
		}
	}
	return nil, nil
}

func (s *Service) HandleFocusCompleted(ctx context.Context) (string, bool, error) {
	slots, err := s.Store.Plans().ListDay(ctx, today())
	if err != nil || len(slots) == 0 {
		return "", false, err
	}
	completed := s.Completed()
	lastIdx := completed - 1
	if lastIdx < 0 || lastIdx >= len(slots) {
		return "", false, nil
	}
	last := slots[lastIdx]
	if last.TaskExternalID == nil {
		return "", false, nil
	}
	ext := *last.TaskExternalID
	minutes := s.Settings().FocusDurationSeconds / 60
	if m := secToMin(last.FocusSeconds); m != nil && *m > 0 {
		minutes = *m
	}
	now := time.Now().Local()
	if focus, ok := s.lastCompletedFocusSeconds(ctx, now); ok {
		minutes = (focus + 30) / 60
	}
	if minutes > 0 {
		if err := s.Tasks.LogProgress(ctx, ext, minutes, now); err != nil {
			return ext, false, err
		}
	}
	for _, sl := range slots {
		if sl.Idx >= completed && sl.TaskExternalID != nil && *sl.TaskExternalID == ext {
			return ext, false, nil
		}
	}
	done, err := s.Tasks.CompleteTodayOccurrence(ctx, ext, now)
	return ext, done, err
}

func (s *Service) lastCompletedFocusSeconds(ctx context.Context, now time.Time) (int, bool) {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sessions, err := s.Store.Sessions().ListRange(ctx, dayStart, now.Add(time.Minute))
	if err != nil {
		return 0, false
	}
	for _, ses := range sessions {
		if ses.Kind == models.KindFocus && ses.Outcome != nil && *ses.Outcome == models.OutcomeCompleted {
			return ses.ElapsedFocusSeconds(), true
		}
	}
	return 0, false
}

func (s *Service) Candidates(ctx context.Context) ([]tasksclient.TaskOption, error) {
	due, err := s.Tasks.DueToday(ctx, time.Now().Local())
	if err != nil {
		return []tasksclient.TaskOption{}, nil
	}
	paths := map[string]string{}
	if topics, terr := s.Tasks.Topics(ctx); terr == nil {
		byID := map[string]tasksclient.Topic{}
		for _, t := range topics {
			byID[t.ID] = t
		}
		for _, t := range topics {
			path := t.Name
			seen := map[string]bool{t.ID: true}
			parent := t.ParentID
			for parent != nil && !seen[*parent] {
				p, ok := byID[*parent]
				if !ok {
					break
				}
				seen[p.ID] = true
				path = p.Name + " / " + path
				parent = p.ParentID
			}
			paths[t.ID] = path
		}
	}
	out := make([]tasksclient.TaskOption, 0, len(due))
	for _, d := range due {
		opt := d.Option
		if d.TopicID != nil {
			opt.TopicPath = paths[*d.TopicID]
		}
		out = append(out, opt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TopicPath != out[j].TopicPath {
			return out[i].TopicPath < out[j].TopicPath
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

type PickerNode struct {
	Kind       string       `json:"kind"`
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Source     string       `json:"source"`
	BindTaskID string       `json:"bind_task_id"`
	Today      bool         `json:"today"`
	Children   []PickerNode `json:"children"`
}

func (s *Service) PickerTree(ctx context.Context) ([]PickerNode, error) {
	due, err := s.Tasks.Pickable(ctx)
	if err != nil {
		return []PickerNode{}, nil
	}
	todaySet := map[string]bool{}
	if todayTasks, terr := s.Tasks.DueToday(ctx, time.Now().Local()); terr == nil {
		for _, d := range todayTasks {
			todaySet[d.Option.ExternalID] = true
		}
	}
	topics, err := s.Tasks.Topics(ctx)
	if err != nil {
		return []PickerNode{}, nil
	}

	source := ""
	tasksByTopic := map[string][]tasksclient.DueTask{}
	var orphan []tasksclient.DueTask
	for _, d := range due {
		if source == "" {
			source = d.Option.Source
		}
		if d.TopicID != nil {
			tasksByTopic[*d.TopicID] = append(tasksByTopic[*d.TopicID], d)
		} else {
			orphan = append(orphan, d)
		}
	}
	if source == "" {
		source = "tasks"
	}

	childTopics := map[string][]tasksclient.Topic{}
	valid := map[string]bool{}
	for _, t := range topics {
		valid[t.ID] = true
	}
	for _, t := range topics {
		key := ""
		if t.ParentID != nil && valid[*t.ParentID] {
			key = *t.ParentID
		}
		childTopics[key] = append(childTopics[key], t)
	}
	for _, list := range childTopics {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}

	generalTaskOf := func(topic tasksclient.Topic) *tasksclient.DueTask {
		list := tasksByTopic[topic.ID]
		for i := range list {
			if list[i].Option.Title == topic.Name {
				return &list[i]
			}
		}
		return nil
	}

	var build func(topic tasksclient.Topic, inheritedBind string) PickerNode
	build = func(topic tasksclient.Topic, inheritedBind string) PickerNode {
		bind := inheritedBind
		general := generalTaskOf(topic)
		if general != nil {
			bind = general.Option.ExternalID
		}
		node := PickerNode{Kind: "topic", ID: topic.ID, Name: topic.Name, BindTaskID: bind}
		if bind != "" {
			node.Source = source
			node.Today = todaySet[bind]
		}
		for _, ct := range childTopics[topic.ID] {
			node.Children = append(node.Children, build(ct, bind))
		}
		for _, d := range tasksByTopic[topic.ID] {
			if general != nil && d.Option.ExternalID == general.Option.ExternalID {
				continue
			}
			node.Children = append(node.Children, PickerNode{
				Kind: "task", ID: d.Option.ExternalID, Name: d.Option.Title,
				Source: source, BindTaskID: d.Option.ExternalID,
				Today: todaySet[d.Option.ExternalID],
			})
		}
		return node
	}

	var roots []PickerNode
	for _, t := range childTopics[""] {
		roots = append(roots, build(t, ""))
	}
	for _, d := range orphan {
		roots = append(roots, PickerNode{
			Kind: "task", ID: d.Option.ExternalID, Name: d.Option.Title,
			Source: source, BindTaskID: d.Option.ExternalID,
			Today: todaySet[d.Option.ExternalID],
		})
	}
	return roots, nil
}

func (s *Service) SavePreset(ctx context.Context, name string) ([]models.Preset, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("preset name is required")
	}
	slots, err := s.Store.Plans().ListDay(ctx, today())
	if err != nil {
		return nil, err
	}
	if len(slots) == 0 {
		return nil, fmt.Errorf("нет плана дня для сохранения")
	}
	settings := s.Settings()
	defFocus := settings.FocusDurationSeconds / 60
	defBreak := settings.ShortBreakSeconds / 60
	ps := make(models.PresetSlots, 0, len(slots))
	for _, sl := range slots {
		p := models.PresetSlot{FocusMinutes: defFocus, BreakMinutes: defBreak}
		if m := secToMin(sl.FocusSeconds); m != nil {
			p.FocusMinutes = *m
		}
		if m := secToMin(sl.BreakSeconds); m != nil {
			p.BreakMinutes = *m
		}
		ps = append(ps, p)
	}
	if err := s.Store.Presets().Save(ctx, &models.Preset{Name: name, Slots: ps}); err != nil {
		return nil, err
	}
	return s.Store.Presets().List(ctx)
}

func (s *Service) Presets(ctx context.Context) ([]models.Preset, error) {
	return s.Store.Presets().List(ctx)
}

func (s *Service) DeletePreset(ctx context.Context, name string) ([]models.Preset, error) {
	p, err := s.Store.Presets().GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if p != nil {
		if err := s.Store.Presets().Delete(ctx, p.ID); err != nil {
			return nil, err
		}
	}
	return s.Store.Presets().List(ctx)
}

func (s *Service) Schedule(ctx context.Context) ([]ScheduleEntry, error) {
	assignments, err := s.Store.Presets().Schedule(ctx)
	if err != nil {
		return nil, err
	}
	presets, err := s.Store.Presets().List(ctx)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	for _, p := range presets {
		names[p.ID] = p.Name
	}
	out := make([]ScheduleEntry, 0, len(assignments))
	for _, a := range assignments {
		if n, ok := names[a.PresetID]; ok {
			out = append(out, ScheduleEntry{Weekday: a.Weekday, PresetName: n})
		}
	}
	return out, nil
}

func (s *Service) AssignPreset(ctx context.Context, weekday int, presetName string) ([]ScheduleEntry, error) {
	if weekday < 1 || weekday > 7 {
		return nil, fmt.Errorf("weekday must be 1..7")
	}
	if presetName == "" {
		if err := s.Store.Presets().Assign(ctx, weekday, nil); err != nil {
			return nil, err
		}
		return s.Schedule(ctx)
	}
	p, err := s.Store.Presets().GetByName(ctx, presetName)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("пресет %q не найден", presetName)
	}
	if err := s.Store.Presets().Assign(ctx, weekday, &p.ID); err != nil {
		return nil, err
	}
	return s.Schedule(ctx)
}

func (s *Service) presetFor(ctx context.Context, now time.Time) (*models.Preset, error) {
	assignments, err := s.Store.Presets().Schedule(ctx)
	if err != nil {
		return nil, err
	}
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	for _, a := range assignments {
		if a.Weekday != weekday {
			continue
		}
		presets, err := s.Store.Presets().List(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range presets {
			if p.ID == a.PresetID {
				return &p, nil
			}
		}
	}
	return nil, nil
}

func secToMin(sec *int) *int {
	if sec == nil {
		return nil
	}
	m := *sec / 60
	return &m
}

func minToSec(min *int) *int {
	if min == nil || *min <= 0 {
		return nil
	}
	s := *min * 60
	return &s
}

type layoutBlock struct {
	start    int
	end      int
	windowID string
}

type slotShape struct {
	focus int
	brk   int
}

type dayLayout struct {
	total  int
	blocks []layoutBlock
	shapes []slotShape
}

func (l *dayLayout) shapeAt(idx int) (slotShape, bool) {
	if l == nil || idx < 0 || idx >= len(l.shapes) {
		return slotShape{}, false
	}
	return l.shapes[idx], true
}

type dayInterval struct {
	start   int
	length  int
	option  tasksclient.TaskOption
	blocked bool
}

func dayIntervals(due []tasksclient.DueTask) []dayInterval {
	var windows, blocked []dayInterval
	for _, d := range due {
		if d.StartTimeMin == nil || d.DurationMin == nil || *d.DurationMin <= 0 {
			continue
		}
		it := dayInterval{
			start:   *d.StartTimeMin,
			length:  *d.DurationMin,
			option:  d.Option,
			blocked: d.Blocked,
		}
		if it.blocked {
			blocked = append(blocked, it)
		} else {
			windows = append(windows, it)
		}
	}
	items := append([]dayInterval(nil), blocked...)
	for _, w := range windows {
		items = append(items, carveBlocked(w, blocked)...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })
	out := items[:0]
	prevEnd := -1
	for _, it := range items {
		start := it.start
		end := it.start + it.length
		if start < prevEnd {
			start = prevEnd
		}
		if end <= start {
			continue
		}
		it.start = start
		it.length = end - start
		out = append(out, it)
		prevEnd = end
	}
	return out
}

func carveBlocked(window dayInterval, blocked []dayInterval) []dayInterval {
	parts := []dayInterval{window}
	for _, b := range blocked {
		bStart, bEnd := b.start, b.start+b.length
		var next []dayInterval
		for _, p := range parts {
			pStart, pEnd := p.start, p.start+p.length
			if bEnd <= pStart || bStart >= pEnd {
				next = append(next, p)
				continue
			}
			if bStart > pStart {
				left := p
				left.length = bStart - pStart
				next = append(next, left)
			}
			if bEnd < pEnd {
				right := p
				right.start = bEnd
				right.length = pEnd - bEnd
				next = append(next, right)
			}
		}
		parts = next
	}
	return parts
}

func computeDayLayout(settings models.Settings, items []dayInterval) *dayLayout {
	if len(items) == 0 {
		return nil
	}
	focus := settings.FocusDurationSeconds / 60
	if focus <= 0 {
		focus = 25
	}
	short := settings.ShortBreakSeconds / 60
	if short < 0 {
		short = 0
	}
	long := settings.LongBreakSeconds / 60
	if long <= 0 {
		long = short
	}
	blockSize := 0
	for _, b := range settings.DayBlocks {
		if b > 0 {
			blockSize = b
			break
		}
	}
	if blockSize <= 0 {
		blockSize = 4
	}
	before := settings.PlanBeforeWindowMin
	if before < 0 {
		before = 0
	}
	after := settings.PlanAfterWindowMin
	if after < 0 {
		after = 0
	}
	type segment struct {
		length   int
		windowID string
	}
	var segments []segment
	segments = append(segments, segment{before, ""})
	for i, it := range items {
		if !it.blocked {
			segments = append(segments, segment{it.length, it.option.ExternalID})
		}
		if i+1 < len(items) {
			gap := items[i+1].start - (it.start + it.length)
			if gap > 0 {
				segments = append(segments, segment{gap, ""})
			}
		}
	}
	segments = append(segments, segment{after, ""})
	fitCfg := DefaultFitConfig(focus, short, long, blockSize)
	fitCfg.MinShort = short
	fitCfg.MinLong = long
	l := &dayLayout{}
	idx := 0
	for _, seg := range segments {
		fit := FitPeriod(seg.length, fitCfg)
		if fit.Count == 0 {
			continue
		}
		n := 0
		bStart := idx
		for k := 0; k < fit.Count; k++ {
			brk := fit.Short
			if (k+1)%blockSize == 0 && k+1 < fit.Count {
				brk = fit.Long
			}
			l.shapes = append(l.shapes, slotShape{focus: fit.Focus, brk: brk})
			idx++
			n++
			if n == blockSize {
				l.blocks = append(l.blocks, layoutBlock{bStart, idx, seg.windowID})
				bStart = idx
				n = 0
			}
		}
		if n > 0 {
			l.blocks = append(l.blocks, layoutBlock{bStart, idx, seg.windowID})
		}
	}
	l.total = idx
	return l
}

func buildSlots(date string, total int, preset *models.Preset, due []tasksclient.DueTask, existing []models.PlanSlot, settings models.Settings, tasksOK bool, frozen int, layout *dayLayout, committed int) []models.PlanSlot {
	defaultFocusMin := settings.FocusDurationSeconds / 60
	if defaultFocusMin <= 0 {
		defaultFocusMin = 25
	}
	valid := map[string]bool{}
	for _, d := range due {
		valid[d.Option.ExternalID] = true
	}
	slotFocusMin := func(sl models.PlanSlot, idx int) int {
		if m := secToMin(sl.FocusSeconds); m != nil && *m > 0 {
			return *m
		}
		if preset != nil && idx < len(preset.Slots) && preset.Slots[idx].FocusMinutes > 0 {
			return preset.Slots[idx].FocusMinutes
		}
		if sh, ok := layout.shapeAt(idx); ok && sh.focus > 0 {
			return sh.focus
		}
		return defaultFocusMin
	}
	pinned := map[int]models.PlanSlot{}
	pinnedMinutes := map[string]int{}
	for _, sl := range existing {
		if sl.Idx >= total {
			continue
		}
		fixed := sl.Idx < frozen
		if !tasksOK && (sl.TaskExternalID != nil || sl.Label != nil) {
			fixed = true
		}
		if !fixed && !sl.Pinned {
			continue
		}
		if !fixed && tasksOK && sl.TaskExternalID != nil && !valid[*sl.TaskExternalID] {
			continue
		}
		pinned[sl.Idx] = sl
		if sl.TaskExternalID != nil {
			pinnedMinutes[*sl.TaskExternalID] += slotFocusMin(sl, sl.Idx)
		}
	}

	windowByID := map[string]tasksclient.TaskOption{}
	for _, it := range dayIntervals(due) {
		if it.blocked {
			continue
		}
		windowByID[it.option.ExternalID] = it.option
	}
	windowAt := planWindowPositions(layout, pinned, windowByID, settings.WindowSharePercent)

	avgFocusMin := 0
	for i := 0; i < total; i++ {
		avgFocusMin += slotFocusMin(models.PlanSlot{Date: date, Idx: i}, i)
	}
	if total > 0 {
		avgFocusMin /= total
	}
	if avgFocusMin <= 0 {
		avgFocusMin = defaultFocusMin
	}

	ordered := append([]tasksclient.DueTask(nil), due...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return priorityWeightPercent(ordered[i].Priority) > priorityWeightPercent(ordered[j].Priority)
	})
	var needs []flexNeed
	for _, d := range ordered {
		if d.Blocked {
			continue
		}
		if _, isWin := windowByID[d.Option.ExternalID]; isWin {
			continue
		}
		remaining := defaultFocusMin
		if d.EffortMin != nil && *d.EffortMin > 0 {
			remaining = *d.EffortMin
		}
		remaining -= pinnedMinutes[d.Option.ExternalID]
		if remaining <= 0 {
			continue
		}
		poms := (remaining + avgFocusMin/2) / avgFocusMin
		if poms < 1 {
			poms = 1
		}
		needs = append(needs, flexNeed{
			task:      d.Option,
			remaining: remaining,
			slots:     poms,
			weight:    poms * priorityWeightPercent(d.Priority),
		})
	}

	committedSlots := total
	var freeMain []int
	for i := 0; i < total; i++ {
		if _, ok := pinned[i]; ok {
			continue
		}
		if windowAt[i] != "" {
			continue
		}
		freeMain = append(freeMain, i)
	}

	mainQuota := shareByWeight(len(freeMain), needs, func(n flexNeed) int { return n.weight })

	assign := map[int]tasksclient.TaskOption{}
	pos := 0
	for i := range needs {
		for k := 0; k < mainQuota[i] && pos < len(freeMain); k++ {
			assign[freeMain[pos]] = needs[i].task
			pos++
		}
	}

	var tail []tasksclient.TaskOption
	if committed > 0 {
		for i, n := range needs {
			for k := mainQuota[i]; k < n.slots && len(tail) < maxOverflowSlots; k++ {
				tail = append(tail, n.task)
			}
		}
	}
	for i, t := range tail {
		assign[total+i] = t
	}
	total += len(tail)

	out := make([]models.PlanSlot, 0, total)
	for i := 0; i < total; i++ {
		if sl, ok := pinned[i]; ok {
			out = append(out, sl)
			continue
		}
		sl := models.PlanSlot{Date: date, Idx: i}
		if sh, ok := layout.shapeAt(i); ok {
			sl.FocusSeconds = minToSec(&sh.focus)
			sl.BreakSeconds = minToSec(&sh.brk)
		}
		if preset != nil && i < len(preset.Slots) {
			sl.FocusSeconds = minToSec(&preset.Slots[i].FocusMinutes)
			sl.BreakSeconds = minToSec(&preset.Slots[i].BreakMinutes)
		}
		if id := windowAt[i]; id != "" && i < committedSlots {
			t := windowByID[id]
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
			out = append(out, sl)
			continue
		}
		if t, ok := assign[i]; ok {
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
		}
		out = append(out, sl)
	}
	return out
}

const maxOverflowSlots = 40

func priorityWeightPercent(priority int) int {
	if priority < 1 {
		priority = 1
	}
	if priority > 5 {
		priority = 5
	}
	return 100 + (priority-1)*50
}

func shareByWeight(free int, needs []flexNeed, weightOf func(flexNeed) int) []int {
	quotas := make([]int, len(needs))
	if free <= 0 || len(needs) == 0 {
		return quotas
	}
	sum := 0
	for _, n := range needs {
		if w := weightOf(n); w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		return quotas
	}
	left := free
	type share struct {
		idx    int
		weight int
		frac   int
	}
	var shares []share
	for i, n := range needs {
		w := weightOf(n)
		if w <= 0 {
			continue
		}
		q := free * w / sum
		if q > n.slots {
			q = n.slots
		}
		quotas[i] = q
		left -= q
		shares = append(shares, share{idx: i, weight: w, frac: free*w - q*sum})
	}

	sort.SliceStable(shares, func(a, b int) bool {
		if shares[a].frac != shares[b].frac {
			return shares[a].frac > shares[b].frac
		}
		return shares[a].weight > shares[b].weight
	})
	for pass := 0; left > 0 && pass < len(shares)+1; pass++ {
		progressed := false
		for _, s := range shares {
			if left <= 0 {
				break
			}
			if quotas[s.idx] >= needs[s.idx].slots {
				continue
			}
			quotas[s.idx]++
			left--
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return quotas
}

type flexNeed struct {
	task      tasksclient.TaskOption
	remaining int
	slots     int
	weight    int
}

func planWindowPositions(layout *dayLayout, pinned map[int]models.PlanSlot, works map[string]tasksclient.TaskOption, sharePercent int) map[int]string {
	windowAt := map[int]string{}
	if layout == nil || len(works) == 0 || sharePercent <= 0 {
		return windowAt
	}
	if sharePercent > 100 {
		sharePercent = 100
	}
	for _, b := range layout.blocks {
		if b.windowID == "" {
			continue
		}
		if _, ok := works[b.windowID]; !ok {
			continue
		}
		n := b.end - b.start
		quota := n * sharePercent / 100
		free := make([]int, 0, n)
		for i := b.start; i < b.end; i++ {
			if sl, ok := pinned[i]; ok {
				if sl.TaskExternalID != nil && *sl.TaskExternalID == b.windowID {
					quota--
				}
				continue
			}
			free = append(free, i)
		}
		workLeft := quota
		for fi, pos := range free {
			if workLeft <= 0 {
				break
			}
			if pos == b.start {
				continue
			}
			otherLeft := len(free) - fi - workLeft
			if workLeft > otherLeft {
				windowAt[pos] = b.windowID
				workLeft--
			}
		}
	}
	return windowAt
}
