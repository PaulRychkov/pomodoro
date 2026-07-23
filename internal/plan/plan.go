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
	Pinned       bool            `json:"pinned"`
	Done         bool            `json:"done"`
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
	layout := computeDayLayout(settings, workWindowsOf(due))
	total := 0
	if preset != nil {
		total = len(preset.Slots)
	}
	if total == 0 && layout != nil {
		total = layout.total
	}
	if total == 0 {
		for _, b := range settings.DayBlocks {
			total += b
		}
	}
	if !tasksOK && len(existing) > 0 {
		total = len(existing)
	}
	completed := s.Completed()
	if refresh || len(existing) != total {
		existing = buildSlots(date, total, preset, due, existing, settings, tasksOK, completed, layout)
		if err := s.Store.Plans().ReplaceDay(ctx, date, existing); err != nil {
			return nil, err
		}
	}
	views := make([]SlotView, 0, total)
	for _, sl := range existing {
		views = append(views, SlotView{
			Idx:          sl.Idx,
			Task:         sl.Task(),
			Label:        sl.Label,
			FocusMinutes: secToMin(sl.FocusSeconds),
			BreakMinutes: secToMin(sl.BreakSeconds),
			Pinned:       sl.Pinned,
			Done:         sl.Idx < completed,
		})
	}
	return views, nil
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
	if err := s.Tasks.LogProgress(ctx, ext, minutes, now); err != nil {
		return ext, false, err
	}
	for _, sl := range slots {
		if sl.Idx >= completed && sl.TaskExternalID != nil && *sl.TaskExternalID == ext {
			return ext, false, nil
		}
	}
	done, err := s.Tasks.CompleteTodayOccurrence(ctx, ext, now)
	return ext, done, err
}

func (s *Service) Candidates(ctx context.Context) ([]tasksclient.TaskOption, error) {
	due, err := s.Tasks.DueToday(ctx, time.Now().Local())
	if err != nil {
		return nil, err
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
		return nil, err
	}
	todaySet := map[string]bool{}
	if todayTasks, terr := s.Tasks.DueToday(ctx, time.Now().Local()); terr == nil {
		for _, d := range todayTasks {
			todaySet[d.Option.ExternalID] = true
		}
	}
	topics, err := s.Tasks.Topics(ctx)
	if err != nil {
		return nil, err
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
	start  int
	end    int
	workID string
}

type dayLayout struct {
	total  int
	blocks []layoutBlock
}

func workWindowsOf(due []tasksclient.DueTask) []tasksclient.DueTask {
	var wins []tasksclient.DueTask
	for _, d := range due {
		if d.StartTimeMin == nil || d.DurationMin == nil || *d.DurationMin <= 0 {
			continue
		}
		wins = append(wins, d)
	}
	sort.SliceStable(wins, func(i, j int) bool { return *wins[i].StartTimeMin < *wins[j].StartTimeMin })
	out := wins[:0]
	prevEnd := -1
	for _, w := range wins {
		start := *w.StartTimeMin
		end := start + *w.DurationMin
		if start < prevEnd {
			start = prevEnd
		}
		if end <= start {
			continue
		}
		length := end - start
		w.DurationMin = &length
		w.StartTimeMin = &start
		out = append(out, w)
		prevEnd = end
	}
	return out
}

func computeDayLayout(settings models.Settings, wins []tasksclient.DueTask) *dayLayout {
	if len(wins) == 0 {
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
	before := settings.StudyBeforeWorkMin
	if before < 0 {
		before = 0
	}
	after := settings.StudyAfterWorkMin
	if after < 0 {
		after = 0
	}
	type segment struct {
		length int
		workID string
	}
	var segments []segment
	segments = append(segments, segment{before, ""})
	for i, w := range wins {
		segments = append(segments, segment{*w.DurationMin, w.Option.ExternalID})
		if i+1 < len(wins) {
			gap := *wins[i+1].StartTimeMin - (*w.StartTimeMin + *w.DurationMin)
			if gap > 0 {
				segments = append(segments, segment{gap, ""})
			}
		}
	}
	segments = append(segments, segment{after, ""})
	l := &dayLayout{}
	idx := 0
	for _, seg := range segments {
		t := 0
		n := 0
		bStart := idx
		for t+focus <= seg.length {
			idx++
			n++
			t += focus
			if n == blockSize {
				l.blocks = append(l.blocks, layoutBlock{bStart, idx, seg.workID})
				bStart = idx
				n = 0
				t += long
			} else {
				t += short
			}
		}
		if n > 0 {
			l.blocks = append(l.blocks, layoutBlock{bStart, idx, seg.workID})
		}
	}
	l.total = idx
	return l
}

func buildSlots(date string, total int, preset *models.Preset, due []tasksclient.DueTask, existing []models.PlanSlot, settings models.Settings, tasksOK bool, frozen int, layout *dayLayout) []models.PlanSlot {
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
		return defaultFocusMin
	}
	pinned := map[int]models.PlanSlot{}
	pinnedMinutes := map[string]int{}
	for _, sl := range existing {
		if sl.Idx >= total {
			continue
		}
		fixed := sl.Idx < frozen
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

	wins := workWindowsOf(due)
	workByID := map[string]tasksclient.TaskOption{}
	for _, w := range wins {
		workByID[w.Option.ExternalID] = w.Option
	}
	workAt := planWorkPositions(layout, pinned, workByID, settings.WorkSharePercent)

	ordered := append([]tasksclient.DueTask(nil), due...)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := ordered[i].Priority, ordered[j].Priority
		if pi == 0 {
			pi = 100
		}
		if pj == 0 {
			pj = 100
		}
		return pi < pj
	})
	var needs []studyNeed
	for _, d := range ordered {
		if _, isWin := workByID[d.Option.ExternalID]; isWin {
			continue
		}
		remaining := defaultFocusMin
		if d.EffortMin != nil && *d.EffortMin > 0 {
			remaining = *d.EffortMin
		}
		remaining -= pinnedMinutes[d.Option.ExternalID]
		if remaining > 0 {
			needs = append(needs, studyNeed{task: d.Option, remaining: remaining, slots: -1})
		}
	}

	freeStudy := 0
	capacityMin := 0
	for i := 0; i < total; i++ {
		if _, ok := pinned[i]; ok {
			continue
		}
		if workAt[i] != "" {
			continue
		}
		freeStudy++
		capacityMin += slotFocusMin(models.PlanSlot{Date: date, Idx: i}, i)
	}
	sumNeed := 0
	for _, n := range needs {
		sumNeed += n.remaining
	}
	scaled := sumNeed > capacityMin
	if scaled {
		quotas := scaleStudyQuotas(freeStudy, needs)
		for i := range needs {
			needs[i].slots = quotas[i]
		}
	}
	needDone := func(n studyNeed) bool {
		if scaled {
			return n.slots <= 0
		}
		return n.remaining <= 0
	}

	out := make([]models.PlanSlot, 0, total)
	ni := 0
	for i := 0; i < total; i++ {
		if sl, ok := pinned[i]; ok {
			out = append(out, sl)
			continue
		}
		sl := models.PlanSlot{Date: date, Idx: i}
		if preset != nil && i < len(preset.Slots) {
			sl.FocusSeconds = minToSec(&preset.Slots[i].FocusMinutes)
			sl.BreakSeconds = minToSec(&preset.Slots[i].BreakMinutes)
		}
		if id := workAt[i]; id != "" {
			t := workByID[id]
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
			out = append(out, sl)
			continue
		}
		for ni < len(needs) && needDone(needs[ni]) {
			ni++
		}
		if ni < len(needs) {
			t := needs[ni].task
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
			if scaled {
				needs[ni].slots--
			} else {
				needs[ni].remaining -= slotFocusMin(sl, i)
			}
		}
		out = append(out, sl)
	}
	return out
}

type studyNeed struct {
	task      tasksclient.TaskOption
	remaining int
	slots     int
}

func scaleStudyQuotas(free int, needs []studyNeed) []int {
	quotas := make([]int, len(needs))
	if free <= 0 || len(needs) == 0 {
		return quotas
	}
	if free <= len(needs) {
		order := make([]int, len(needs))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return needs[order[a]].remaining > needs[order[b]].remaining })
		for k := 0; k < free; k++ {
			quotas[order[k]] = 1
		}
		return quotas
	}
	sum := 0
	for _, n := range needs {
		sum += n.remaining
	}
	if sum <= 0 {
		return quotas
	}
	tot := 0
	rems := make([]int, len(needs))
	for i, n := range needs {
		q := free * n.remaining / sum
		rems[i] = free*n.remaining - q*sum
		if q < 1 {
			q = 1
			rems[i] = -1
		}
		quotas[i] = q
		tot += q
	}
	order := make([]int, len(needs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rems[order[a]] > rems[order[b]] })
	for k := 0; tot < free; k = (k + 1) % len(order) {
		quotas[order[k]]++
		tot++
	}
	for tot > free {
		big := 0
		for i := range quotas {
			if quotas[i] > quotas[big] {
				big = i
			}
		}
		if quotas[big] <= 1 {
			break
		}
		quotas[big]--
		tot--
	}
	return quotas
}

func planWorkPositions(layout *dayLayout, pinned map[int]models.PlanSlot, works map[string]tasksclient.TaskOption, sharePercent int) map[int]string {
	workAt := map[int]string{}
	if layout == nil || len(works) == 0 || sharePercent <= 0 {
		return workAt
	}
	if sharePercent > 100 {
		sharePercent = 100
	}
	for _, b := range layout.blocks {
		if b.workID == "" {
			continue
		}
		if _, ok := works[b.workID]; !ok {
			continue
		}
		n := b.end - b.start
		quota := n * sharePercent / 100
		free := make([]int, 0, n)
		for i := b.start; i < b.end; i++ {
			if sl, ok := pinned[i]; ok {
				if sl.TaskExternalID != nil && *sl.TaskExternalID == b.workID {
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
				workAt[pos] = b.workID
				workLeft--
			}
		}
	}
	return workAt
}
