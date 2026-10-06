package plan

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Now       func() time.Time

	building sync.Mutex
	mu       sync.Mutex
	cached   dayCache
}

type dayCache struct {
	date   string
	rows   []models.PlanSlot
	blocks []int
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().Local()
	}
	return time.Now().Local()
}

func (s *Service) today() string {
	return s.now().Format("2006-01-02")
}

func (s *Service) remember(date string, rows []models.PlanSlot, blocks []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = dayCache{date: date, rows: append([]models.PlanSlot(nil), rows...), blocks: blocks}
}

func (s *Service) cachedRows() ([]models.PlanSlot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached.date != s.today() {
		return nil, false
	}
	return s.cached.rows, true
}

func (s *Service) Blocks() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached.date != s.today() {
		return nil
	}
	return append([]int(nil), s.cached.blocks...)
}

type SlotView struct {
	Idx                int             `json:"idx"`
	Task               *models.TaskRef `json:"task"`
	Label              *string         `json:"label"`
	FocusMinutes       *int            `json:"focus_minutes"`
	BreakMinutes       *int            `json:"break_minutes"`
	StartMinutes       *int            `json:"start_minutes"`
	EndMinutes         *int            `json:"end_minutes"`
	PeriodStartMinutes *int            `json:"period_start_minutes"`
	PeriodEndMinutes   *int            `json:"period_end_minutes"`
	WindowID           *string         `json:"window_id"`
	WindowTitle        *string         `json:"window_title"`
	Pinned             bool            `json:"pinned"`
	Done               bool            `json:"done"`
	Overflow           bool            `json:"overflow"`
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

func (s *Service) Day(ctx context.Context, refresh bool) ([]SlotView, error) {
	s.building.Lock()
	defer s.building.Unlock()
	return s.day(ctx, refresh)
}

func (s *Service) day(ctx context.Context, refresh bool) ([]SlotView, error) {
	now := s.now()
	date := now.Format("2006-01-02")
	settings := s.Settings()
	preset, err := s.presetFor(ctx, now)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.Plans().ListDay(ctx, date)
	if err != nil {
		return nil, err
	}
	due, dueErr := s.Tasks.DueToday(ctx, now)
	tasksOK := dueErr == nil
	rt := s.runtime(ctx, now)
	dayStart, _ := dayBounds(settings)

	build := func(proj projection) error {
		rows = buildDay(buildInput{
			date:     date,
			settings: settings,
			preset:   preset,
			due:      due,
			existing: rows,
			frozen:   rt.frozen(),
			from:     max(proj.from, dayStart),
		})
		return s.Store.Plans().ReplaceDay(ctx, date, rows)
	}
	rebuilt := false
	stale := needsLayout(rows, rt.frozen()) ||
		tasksOK && preset == nil && windowsChanged(rows, rt.frozen(), dayPeriods(settings, due), rt.now, durationsOf(settings))
	if (refresh || len(rows) == 0 || stale) && (tasksOK || len(rows) == 0) {
		if err := build(project(rows, rt)); err != nil {
			return nil, err
		}
		rebuilt = true
	}
	proj := project(rows, rt)
	if proj.displaced && tasksOK && !rebuilt {
		if err := build(proj); err != nil {
			return nil, err
		}
		proj = project(rows, rt)
	}
	s.remember(date, rows, planBlocks(rows, durationsOf(settings)))

	views := make([]SlotView, 0, len(rows))
	for i, sl := range rows {
		view := SlotView{
			Idx:                sl.Idx,
			Task:               sl.Task(),
			Label:              sl.Label,
			FocusMinutes:       secToMin(sl.FocusSeconds),
			BreakMinutes:       secToMin(sl.BreakSeconds),
			PeriodStartMinutes: sl.PeriodStartMin,
			PeriodEndMinutes:   sl.PeriodEndMin,
			WindowID:           sl.WindowID,
			WindowTitle:        sl.WindowTitle,
			Pinned:             sl.Pinned || sl.PinnedFocus || sl.PinnedBreak,
			Done:               sl.Idx < rt.completed,
			Overflow:           sl.Overflow,
		}
		if start := proj.starts[i]; start >= 0 {
			view.StartMinutes = &start
			if !view.Done {
				end := start + focusMinutes(sl, durationsOf(settings).focus)
				view.EndMinutes = &end
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func needsLayout(rows []models.PlanSlot, frozen int) bool {
	for _, sl := range rows {
		if sl.Idx >= frozen && !sl.Overflow && (sl.StartMin == nil || sl.PeriodEndMin == nil) {
			return true
		}
	}
	return false
}

func windowsChanged(rows []models.PlanSlot, frozen int, ps []period, now int, d durations) bool {
	planned := map[string]bool{}
	for _, sl := range rows {
		if sl.WindowID != nil && sl.PeriodEndMin != nil {
			planned[*sl.WindowID+"@"+strconv.Itoa(*sl.PeriodEndMin)] = true
		}
	}
	current := map[string]bool{}
	for _, p := range ps {
		id := p.windowID()
		if id == "" {
			continue
		}
		key := id + "@" + strconv.Itoa(p.end)
		current[key] = true
		if p.end-max(p.start, now) >= d.focus+d.short && !planned[key] {
			return true
		}
	}
	for _, sl := range rows {
		if sl.Idx >= frozen && sl.WindowID != nil && sl.PeriodEndMin != nil &&
			!current[*sl.WindowID+"@"+strconv.Itoa(*sl.PeriodEndMin)] {
			return true
		}
	}
	return false
}

func (s *Service) runtime(ctx context.Context, now time.Time) dayRuntime {
	rt := dayRuntime{now: minuteOfDay(now), completed: s.Completed(), lastFocusEnd: -1}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if sessions, err := s.Store.Sessions().ListRange(ctx, dayStart, now.Add(time.Second)); err == nil {
		sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].StartedAt.Before(sessions[j].StartedAt) })
		for _, ses := range sessions {
			if ses.Kind == models.KindFocus && ses.Outcome != nil && *ses.Outcome == models.OutcomeCompleted {
				rt.past = append(rt.past, minuteOfDay(ses.StartedAt.Local()))
			}
		}
		if n := len(sessions); n > 0 {
			last := sessions[n-1]
			if last.Kind == models.KindFocus && last.EndedAt != nil && last.Outcome != nil &&
				*last.Outcome == models.OutcomeCompleted && sameDay(*last.EndedAt, now) {
				rt.lastFocusEnd = minuteOfDay(last.EndedAt.Local())
			}
		}
	}
	if a, err := s.Store.Sessions().FindActive(ctx); err == nil && a != nil {
		rt.active = &activeSlot{
			startMin:     minuteOfDay(a.StartedAt.Local()),
			remainingMin: (a.RemainingSeconds(now) + 59) / 60,
			isFocus:      a.Kind == models.KindFocus,
		}
	}
	return rt
}

func minuteOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

func sameDay(a, b time.Time) bool {
	return a.Local().Format("2006-01-02") == b.Local().Format("2006-01-02")
}

func (s *Service) SetSlot(ctx context.Context, idx int, upd SlotUpdate) ([]SlotView, error) {
	s.building.Lock()
	defer s.building.Unlock()
	if idx < 0 {
		return nil, fmt.Errorf("bad slot index %d", idx)
	}
	date := s.today()
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
		sl.PinnedFocus = *upd.FocusMinutes > 0
		if sl.PinnedFocus {
			sl.FocusSeconds = minToSec(upd.FocusMinutes)
		}
	}
	if upd.BreakMinutes != nil {
		sl.PinnedBreak = *upd.BreakMinutes > 0
		if sl.PinnedBreak {
			sl.BreakSeconds = minToSec(upd.BreakMinutes)
		}
	}
	sl.Pinned = sl.TaskSource != nil || sl.Label != nil
	if err := s.Store.Plans().Upsert(ctx, &sl); err != nil {
		return nil, err
	}
	return s.day(ctx, true)
}

func (s *Service) SlotDurations(idx int) (focusSeconds, breakSeconds *int) {
	rows, ok := s.cachedRows()
	if !ok {
		var err error
		rows, err = s.Store.Plans().ListDay(context.Background(), s.today())
		if err != nil {
			return nil, nil
		}
	}
	for _, sl := range rows {
		if sl.Idx == idx {
			return sl.FocusSeconds, sl.BreakSeconds
		}
	}
	return nil, nil
}

func (s *Service) HandleFocusCompleted(ctx context.Context) (string, bool, error) {
	now := s.now()
	slots, err := s.Store.Plans().ListDay(ctx, now.Format("2006-01-02"))
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
	if focus, ok := s.lastCompletedFocusSeconds(ctx, now); ok {
		minutes = (focus + 30) / 60
	}
	if minutes > 0 {
		if err := s.Tasks.LogProgress(ctx, ext, minutes, now); err != nil {
			return ext, false, err
		}
	}
	nowMin := minuteOfDay(now)
	for _, sl := range slots {
		if sl.Idx >= completed && sl.TaskExternalID != nil && *sl.TaskExternalID == ext {
			return ext, false, nil
		}
		if sl.WindowID != nil && *sl.WindowID == ext && sl.PeriodEndMin != nil && *sl.PeriodEndMin > nowMin {
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
	due, err := s.Tasks.DueToday(ctx, s.now())
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
	if todayTasks, terr := s.Tasks.DueToday(ctx, s.now()); terr == nil {
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
	slots, err := s.Store.Plans().ListDay(ctx, s.today())
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
		if sl.Overflow {
			continue
		}
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
