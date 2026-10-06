package plan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store/memstore"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type fakeTasks struct {
	mu        sync.Mutex
	due       []tasksclient.DueTask
	completed []string
}

func (f *fakeTasks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/occurrences":
		var out []map[string]any
		for _, d := range f.due {
			task := map[string]any{
				"id": d.Option.ExternalID, "title": d.Option.Title, "priority": d.Priority,
				"effort_minutes": d.EffortMin, "start_time_minutes": d.StartTimeMin,
				"estimated_duration_minutes": d.DurationMin, "requires_pomodoro": !d.Blocked,
			}
			out = append(out, map[string]any{"id": "occ-" + d.Option.ExternalID, "task_id": d.Option.ExternalID, "status": "pending", "task": task})
		}
		_ = json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
		f.completed = append(f.completed, strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/complete"), "/api/v1/occurrences/occ-"))
		_, _ = w.Write([]byte("{}"))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/progress"):
		_, _ = w.Write([]byte("{}"))
	default:
		http.NotFound(w, r)
	}
}

type serviceFixture struct {
	svc      *Service
	st       *memstore.Mem
	tasks    *fakeTasks
	now      time.Time
	settings models.Settings
}

func newFixture(t *testing.T, hhmm string) *serviceFixture {
	t.Helper()
	f := &serviceFixture{st: memstore.New(), tasks: &fakeTasks{due: pavelsDay()}, settings: settingsDay(360, 1200, 3)}
	srv := httptest.NewServer(f.tasks)
	t.Cleanup(srv.Close)
	f.setClock(t, hhmm)
	f.svc = &Service{
		Store:    f.st,
		Tasks:    tasksclient.New(srv.URL, "tasks"),
		Settings: func() models.Settings { return f.settings },
		Completed: func() int {
			from := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), 0, 0, 0, 0, time.Local)
			n, _ := f.st.Sessions().CountCompletedFocusBetween(context.Background(), from, from.Add(24*time.Hour))
			return n
		},
		Now: func() time.Time { return f.now },
	}
	return f
}

func (f *serviceFixture) setClock(t *testing.T, hhmm string) {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-05 "+hhmm, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	f.now = at
}

func (f *serviceFixture) focusDone(t *testing.T, hhmm string, minutes int) {
	t.Helper()
	start, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-05 "+hhmm, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	end := start.Add(time.Duration(minutes) * time.Minute)
	outcome := models.OutcomeCompleted
	ses := &models.Session{ID: uuid.New(), Kind: models.KindFocus, StartedAt: start, EndedAt: &end,
		PlannedDurationSeconds: minutes * 60, Outcome: &outcome}
	if err := f.st.Sessions().Create(context.Background(), ses); err != nil {
		t.Fatal(err)
	}
}

func (f *serviceFixture) day(t *testing.T, refresh bool) []SlotView {
	t.Helper()
	views, err := f.svc.Day(context.Background(), refresh)
	if err != nil {
		t.Fatal(err)
	}
	return views
}

func clock(m int) string {
	return time.Date(0, 1, 1, m/60, m%60, 0, 0, time.UTC).Format("15:04")
}

func checkViews(t *testing.T, views []SlotView, dayEnd int) {
	t.Helper()
	for _, v := range views {
		if v.Done || v.StartMinutes == nil {
			continue
		}
		if v.Overflow {
			t.Fatalf("у не влезшего слота %d есть время %s", v.Idx, clock(*v.StartMinutes))
		}
		end := *v.EndMinutes
		if v.PeriodEndMinutes == nil || end > *v.PeriodEndMinutes || end > dayEnd {
			t.Fatalf("слот %d %s–%s вылезает за свой период %v или конец дня", v.Idx, clock(*v.StartMinutes), clock(end), v.PeriodEndMinutes)
		}
	}
}

func TestServiceBuildsAnchoredDayAndCaches(t *testing.T) {
	f := newFixture(t, "05:30")
	views := f.day(t, false)
	checkViews(t, views, 1200)
	if views[0].StartMinutes == nil || *views[0].StartMinutes != 360 {
		t.Fatalf("первый помидор по плану в 6:00: %+v", views[0])
	}
	if blocks := f.svc.Blocks(); len(blocks) == 0 || sum(blocks) != 24 {
		t.Fatalf("блоки плана в кеше обязаны покрывать 24 помидора: %v", blocks)
	}
	focus, brk := f.svc.SlotDurations(0)
	if focus == nil || *focus != 17*60 || brk == nil {
		t.Fatalf("длительности первого слота из кеша: %v %v", focus, brk)
	}
}

func TestServiceRecalculatesWhenLateWithoutTouchingWork(t *testing.T) {
	f := newFixture(t, "05:30")
	before := f.day(t, false)
	f.setClock(t, "08:40")
	after := f.day(t, false)
	checkViews(t, after, 1200)
	var morning, firstWork *SlotView
	for i := range after {
		v := &after[i]
		if v.StartMinutes == nil {
			continue
		}
		if v.WindowID == nil && morning == nil && *v.StartMinutes >= 8*60 {
			morning = v
		}
		if v.WindowID != nil && firstWork == nil {
			firstWork = v
		}
	}
	if morning == nil || *morning.StartMinutes != 8*60+40 {
		t.Fatalf("в 8:40 ближайший помидор — сейчас: %+v", morning)
	}
	if firstWork == nil || *firstWork.StartMinutes != 600 {
		t.Fatalf("работа остаётся с 10:00: %+v", firstWork)
	}
	if countViews(after, "w") != countViews(before, "w") {
		t.Fatalf("опоздание утром не меняет число помидоров работы: было %d, стало %d", countViews(before, "w"), countViews(after, "w"))
	}
	for _, v := range after {
		if v.StartMinutes != nil && v.WindowID == nil && *v.StartMinutes < 600 && *v.EndMinutes > 600 {
			t.Fatalf("утренний помидор залез в работу: %+v", v)
		}
	}
}

func countViews(views []SlotView, id string) int {
	n := 0
	for _, v := range views {
		if v.Task != nil && v.Task.ExternalID == id {
			n++
		}
	}
	return n
}

func TestServiceDoneSlotsShowFactualTimesInOrder(t *testing.T) {
	f := newFixture(t, "05:30")
	f.day(t, false)
	f.focusDone(t, "06:02", 25)
	f.setClock(t, "06:30")
	f.day(t, false)
	f.focusDone(t, "08:01", 22)
	f.setClock(t, "08:24")
	views := f.day(t, false)
	if !views[0].Done || !views[1].Done || views[2].Done {
		t.Fatalf("два помидора сделаны: %+v", views[:3])
	}
	if *views[0].StartMinutes != 6*60+2 || *views[1].StartMinutes != 8*60+1 {
		t.Fatalf("сделанные показывают реальные старты по порядку: %d, %d", *views[0].StartMinutes, *views[1].StartMinutes)
	}
	if *views[2].StartMinutes < 8*60+23+5 {
		t.Fatalf("следующий помидор — после перерыва: %s", clock(*views[2].StartMinutes))
	}
	checkViews(t, views, 1200)
}

func TestServiceAfterDayEndEverythingOverflows(t *testing.T) {
	f := newFixture(t, "05:30")
	f.day(t, false)
	f.setClock(t, "20:30")
	views := f.day(t, false)
	for _, v := range views {
		if v.StartMinutes != nil {
			t.Fatalf("после 20:00 ни один помидор не получает время: %+v", v)
		}
	}
	if len(f.svc.Blocks()) != 0 {
		t.Fatalf("после конца дня в плане не остаётся блоков: %v", f.svc.Blocks())
	}
}

func TestServiceSetSlotPinsAndRebuilds(t *testing.T) {
	f := newFixture(t, "05:30")
	f.day(t, false)
	label := "Чтение"
	thirty := 30
	views, err := f.svc.SetSlot(context.Background(), 2, SlotUpdate{Label: &label, FocusMinutes: &thirty})
	if err != nil {
		t.Fatal(err)
	}
	checkViews(t, views, 1200)
	if views[2].Label == nil || *views[2].Label != label || !views[2].Pinned || *views[2].FocusMinutes != 30 {
		t.Fatalf("ручная правка слота обязана сохраниться: %+v", views[2])
	}
	reset := -1
	views, err = f.svc.SetSlot(context.Background(), 2, SlotUpdate{FocusMinutes: &reset})
	if err != nil {
		t.Fatal(err)
	}
	if *views[2].FocusMinutes == 30 {
		t.Fatalf("-1 возвращает длину плана: %+v", views[2])
	}
}

func TestServiceDoesNotCloseWindowBeforeItEnds(t *testing.T) {
	f := newFixture(t, "05:30")
	views := f.day(t, false)
	var workIdx int
	for i, v := range views {
		if v.Task != nil && v.Task.ExternalID == "w" {
			workIdx = i
		}
	}
	for i := 0; i <= workIdx; i++ {
		f.focusDone(t, "06:00", 1)
	}
	f.setClock(t, "18:30")
	if _, _, err := f.svc.HandleFocusCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.tasks.completed) != 0 {
		t.Fatalf("«Работу» нельзя закрывать до 19:00: %v", f.tasks.completed)
	}
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func TestServicePicksUpWindowsWhenTasksComeBack(t *testing.T) {
	f := newFixture(t, "05:30")
	due := f.tasks.due
	f.tasks.due = nil
	views := f.day(t, false)
	for _, v := range views {
		if v.WindowID != nil {
			t.Fatalf("без задач окон в плане нет: %+v", v)
		}
	}
	f.tasks.due = due
	views = f.day(t, false)
	if countViews(views, "w") == 0 {
		t.Fatalf("появившееся окно «Работа» обязано попасть в план без ручного обновления")
	}
	checkViews(t, views, 1200)
	again := f.day(t, false)
	if len(again) != len(views) || countViews(again, "w") != countViews(views, "w") {
		t.Fatalf("без изменений план стабилен между вызовами")
	}
}
