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
	total := 0
	if preset != nil {
		total = len(preset.Slots)
	}
	if total == 0 {
		for _, b := range settings.DayBlocks {
			total += b
		}
	}
	existing, err := s.Store.Plans().ListDay(ctx, date)
	if err != nil {
		return nil, err
	}
	due, dueErr := s.Tasks.DueToday(ctx, time.Now().Local())
	tasksOK := dueErr == nil
	completed := s.Completed()
	if refresh || len(existing) != total {
		existing = buildSlots(date, total, preset, due, existing, settings, tasksOK, completed)
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

func buildSlots(date string, total int, preset *models.Preset, due []tasksclient.DueTask, existing []models.PlanSlot, settings models.Settings, tasksOK bool, frozen int) []models.PlanSlot {
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

	var work *tasksclient.TaskOption
	for i := range due {
		if due[i].StartTimeMin != nil {
			work = &due[i].Option
			break
		}
	}
	workAt := planWorkPositions(total, settings.DayBlocks, pinned, work)

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
	type need struct {
		task      tasksclient.TaskOption
		remaining int
	}
	var needs []need
	for _, d := range ordered {
		if work != nil && d.Option.ExternalID == work.ExternalID {
			continue
		}
		remaining := defaultFocusMin
		if d.EffortMin != nil && *d.EffortMin > 0 {
			remaining = *d.EffortMin
		}
		remaining -= pinnedMinutes[d.Option.ExternalID]
		if remaining > 0 {
			needs = append(needs, need{task: d.Option, remaining: remaining})
		}
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
		if workAt[i] && work != nil {
			sl.SetTask(&models.TaskRef{Source: work.Source, ExternalID: work.ExternalID, TitleSnapshot: work.Title})
			out = append(out, sl)
			continue
		}
		for ni < len(needs) && needs[ni].remaining <= 0 {
			ni++
		}
		if ni < len(needs) {
			t := needs[ni].task
			sl.SetTask(&models.TaskRef{Source: t.Source, ExternalID: t.ExternalID, TitleSnapshot: t.Title})
			needs[ni].remaining -= slotFocusMin(sl, i)
		}
		out = append(out, sl)
	}
	return out
}

func planWorkPositions(total int, dayBlocks []int, pinned map[int]models.PlanSlot, work *tasksclient.TaskOption) map[int]bool {
	workAt := map[int]bool{}
	if work == nil || total == 0 {
		return workAt
	}
	blocks := make([]int, 0, len(dayBlocks)+1)
	used := 0
	for _, b := range dayBlocks {
		if b <= 0 || used >= total {
			continue
		}
		if used+b > total {
			b = total - used
		}
		blocks = append(blocks, b)
		used += b
	}
	if used < total {
		blocks = append(blocks, total-used)
	}
	start := 0
	for _, n := range blocks {
		end := start + n
		quota := n * 2 / 3
		free := make([]int, 0, n)
		for i := start; i < end; i++ {
			if sl, ok := pinned[i]; ok {
				if sl.TaskExternalID != nil && *sl.TaskExternalID == work.ExternalID {
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
			if pos == start {
				continue
			}
			otherLeft := len(free) - fi - workLeft
			if workLeft > otherLeft {
				workAt[pos] = true
				workLeft--
			}
		}
		start = end
	}
	return workAt
}
