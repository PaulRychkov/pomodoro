package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fakeTask struct {
	ID                       string  `json:"id"`
	Title                    string  `json:"title"`
	TopicID                  *string `json:"topic_id"`
	Progress                 string  `json:"progress"`
	IsActive                 *bool   `json:"is_active"`
	StartTimeMinutes         *int    `json:"start_time_minutes"`
	EstimatedDurationMinutes *int    `json:"estimated_duration_minutes"`
	EffortMinutes            *int    `json:"effort_minutes"`
	Priority                 int     `json:"priority"`
	RequiresPomodoro         *bool   `json:"requires_pomodoro"`
}

type fakeTopic struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

type fixture struct {
	Tasks  []fakeTask  `json:"tasks"`
	Topics []fakeTopic `json:"topics"`
}

// UnmarshalJSON принимает и объект {tasks, topics}, и просто массив задач.
// Пропущенный ключ оставляет соответствующую часть пустой — вызывающий решает,
// чем её заменить (см. mergeFixture).
func (f *fixture) UnmarshalJSON(b []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(b)), "[") {
		return json.Unmarshal(b, &f.Tasks)
	}
	type plain fixture
	return json.Unmarshal(b, (*plain)(f))
}

type occurrence struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"task_id"`
	Date            string    `json:"date"`
	Status          string    `json:"status"`
	ProgressMinutes int       `json:"progress_minutes"`
	Task            *fakeTask `json:"task"`
}

type tasksLogEntry struct {
	At           time.Time `json:"at"`
	Kind         string    `json:"kind"` // progress | complete
	OccurrenceID string    `json:"occurrence_id"`
	TaskID       string    `json:"task_id"`
	Date         string    `json:"date"`
	Minutes      int       `json:"minutes,omitempty"`
}

// fakeTasks — подмена task-planner: у каждой задачи на каждый запрошенный день
// есть одно вхождение; complete переводит его в completed, progress копит минуты.
type fakeTasks struct {
	clock *fakeClock
	loc   *time.Location

	mu       sync.Mutex
	gen      int
	fx       fixture
	done     map[string]bool
	progress map[string]int
	log      []tasksLogEntry
}

func newFakeTasks(clock *fakeClock, loc *time.Location) *fakeTasks {
	return &fakeTasks{clock: clock, loc: loc}
}

// Reset начинает новое поколение: клиенты, созданные для прошлого стека, ходят по
// /__tasks/g<N>/... и после reset получают 410 — запоздавшая горутина старого стека
// не успеет записать вызов в журнал следующего теста.
func (t *fakeTasks) Reset(fx fixture) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gen++
	t.fx = fx
	t.done = map[string]bool{}
	t.progress = map[string]int{}
	t.log = nil
	return t.gen
}

// Replace меняет фикстуру, не трогая журнал вызовов и выполненные вхождения.
func (t *fakeTasks) Replace(fx fixture) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fx = fx
}

func (t *fakeTasks) Log() []tasksLogEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]tasksLogEntry{}, t.log...)
}

func (t *fakeTasks) ClearLog() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.log = nil
}

func (t *fakeTasks) Fixture() fixture {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fx
}

func (t *fakeTasks) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/topics", t.topics)
	mux.HandleFunc("GET /api/v1/tasks", t.tasks)
	mux.HandleFunc("GET /api/v1/occurrences", t.occurrences)
	mux.HandleFunc("POST /api/v1/occurrences/{id}/progress", t.logProgress)
	mux.HandleFunc("POST /api/v1/occurrences/{id}/complete", t.complete)
	return http.StripPrefix("/__tasks", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := genPrefix.FindStringSubmatch(r.URL.Path); m != nil {
			if n, _ := strconv.Atoi(m[1]); n != t.generation() {
				writeJSON(w, http.StatusGone, map[string]string{"error": "stale tasks client: stack was reset"})
				return
			}
			r.URL.Path = strings.TrimPrefix(r.URL.Path, m[0])
		}
		mux.ServeHTTP(w, r)
	}))
}

var genPrefix = regexp.MustCompile(`^/g(\d+)(/.*)$`)

func (t *fakeTasks) generation() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gen
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (t *fakeTasks) topics(w http.ResponseWriter, _ *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	writeJSON(w, http.StatusOK, nonNil(t.fx.Topics))
}

func (t *fakeTasks) tasks(w http.ResponseWriter, _ *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	writeJSON(w, http.StatusOK, nonNil(t.fx.Tasks))
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

const (
	occPrefix = "occ_"
	dateLen   = len("2006-01-02")
)

func occID(date, taskID string) string { return occPrefix + date + "_" + taskID }

func splitOccID(id string) (date, taskID string, ok bool) {
	if !strings.HasPrefix(id, occPrefix) || len(id) < len(occPrefix)+dateLen+2 || id[len(occPrefix)+dateLen] != '_' {
		return "", "", false
	}
	return id[len(occPrefix) : len(occPrefix)+dateLen], id[len(occPrefix)+dateLen+1:], true
}

func (t *fakeTasks) occurrences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := time.ParseInLocation("2006-01-02", q.Get("from"), t.loc)
	if err != nil {
		from = t.clock.Now().In(t.loc)
		from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, t.loc)
	}
	to, err := time.ParseInLocation("2006-01-02", q.Get("to"), t.loc)
	if err != nil || to.Before(from) {
		to = from
	}
	if to.Sub(from) > 31*24*time.Hour {
		to = from.Add(31 * 24 * time.Hour)
	}
	status := q.Get("status")

	t.mu.Lock()
	defer t.mu.Unlock()
	out := []occurrence{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for i := range t.fx.Tasks {
			task := t.fx.Tasks[i]
			id := occID(date, task.ID)
			st := "pending"
			if t.done[id] {
				st = "completed"
			}
			if status != "" && status != st {
				continue
			}
			out = append(out, occurrence{ID: id, TaskID: task.ID, Date: date, Status: st, ProgressMinutes: t.progress[id], Task: &task})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (t *fakeTasks) find(id string) (date, taskID string, ok bool) {
	date, taskID, ok = splitOccID(id)
	if !ok {
		return "", "", false
	}
	for _, task := range t.fx.Tasks {
		if task.ID == taskID {
			return date, taskID, true
		}
	}
	return "", "", false
}

func (t *fakeTasks) logProgress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id := r.PathValue("id")
	t.mu.Lock()
	defer t.mu.Unlock()
	date, taskID, ok := t.find(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "occurrence not found"})
		return
	}
	t.progress[id] += body.Minutes
	t.log = append(t.log, tasksLogEntry{At: t.clock.Now(), Kind: "progress", OccurrenceID: id, TaskID: taskID, Date: date, Minutes: body.Minutes})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "task_id": taskID, "progress_minutes": t.progress[id]})
}

func (t *fakeTasks) complete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t.mu.Lock()
	defer t.mu.Unlock()
	date, taskID, ok := t.find(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "occurrence not found"})
		return
	}
	t.done[id] = true
	t.log = append(t.log, tasksLogEntry{At: t.clock.Now(), Kind: "complete", OccurrenceID: id, TaskID: taskID, Date: date})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "task_id": taskID, "status": "completed"})
}

func ptr[T any](v T) *T { return &v }

// defaultFixture — будний день пользователя: события без помидоров, окно «Работа»
// и гибкие задачи с трудоёмкостью в минутах.
func defaultFixture() fixture {
	career, learning, ai := "topic-career", "topic-learning", "topic-ai"
	event := func(id, title string, start, dur int) fakeTask {
		return fakeTask{
			ID: id, Title: title, Progress: "pending", IsActive: ptr(true),
			StartTimeMinutes: ptr(start), EstimatedDurationMinutes: ptr(dur),
			Priority: 1, RequiresPomodoro: ptr(false),
		}
	}
	flexible := func(id, title string, topic *string, effort, prio int) fakeTask {
		return fakeTask{
			ID: id, Title: title, TopicID: topic, Progress: "pending", IsActive: ptr(true),
			EffortMinutes: ptr(effort), Priority: prio, RequiresPomodoro: ptr(true),
		}
	}
	return fixture{
		Topics: []fakeTopic{
			{ID: career, Name: "Карьера"},
			{ID: learning, Name: "Развитие"},
			{ID: ai, Name: "ИИ", ParentID: &learning},
		},
		Tasks: []fakeTask{
			event("ev-gym-prep", "Сборы в зал", 400, 20),
			event("ev-gym", "Зал", 420, 40),
			event("ev-gym-back", "Дорога из зала", 460, 20),
			event("ev-lunch", "Обед", 840, 60),
			{
				ID: "win-work", Title: "Работа", TopicID: &career, Progress: "pending", IsActive: ptr(true),
				StartTimeMinutes: ptr(600), EstimatedDurationMinutes: ptr(540),
				Priority: 3, RequiresPomodoro: ptr(true),
			},
			flexible("job-search", "Поиск работы", &career, 120, 3),
			flexible("ai-other", "Работа с ИИ и другое", &ai, 75, 2),
			flexible("english", "Английский", &learning, 60, 2),
			flexible("typing", "Слепая печать", &learning, 40, 2),
		},
	}
}
