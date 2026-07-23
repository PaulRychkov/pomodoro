package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/store/memstore"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type env struct {
	router *gin.Engine
	engine *engine.Engine
	mem    *memstore.Mem
}

func newEnv(t *testing.T, mutate func(*models.Settings)) *env {
	t.Helper()
	mem := memstore.New()
	if mutate != nil {
		s, _ := mem.Settings().Get(context.Background())
		mutate(&s)
		if err := mem.Settings().Save(context.Background(), &s); err != nil {
			t.Fatalf("save settings: %v", err)
		}
	}
	e := engine.New(mem, engine.SystemClock())
	if err := e.Init(context.Background()); err != nil {
		t.Fatalf("init engine: %v", err)
	}
	pl := &plan.Service{
		Store:     mem,
		Tasks:     tasksclient.New("http://127.0.0.1:1", "tasks"),
		Settings:  e.Settings,
		Completed: func() int { return e.Snapshot().CompletedToday },
	}
	h := New(e, mem, pl, zap.NewNop())
	return &env{router: h.Router(), engine: e, mem: mem}
}

func (e *env) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return out
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	body := decode[map[string]map[string]string](t, w)
	return body["error"]["code"]
}

func TestHealthz(t *testing.T) {
	e := newEnv(t, nil)
	w := e.do(t, http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestStartSessionFlow(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })

	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"kind": "focus", "label": "читаю"})
	if w.Code != http.StatusCreated {
		t.Fatalf("start status = %d body=%s", w.Code, w.Body.String())
	}
	created := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)
	if created.Session.Kind != models.KindFocus || created.Session.Label == nil || *created.Session.Label != "читаю" {
		t.Fatalf("created session = %+v", created.Session)
	}

	w = e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"kind": "focus"})
	if w.Code != http.StatusConflict || errorCode(t, w) != "session_active" {
		t.Fatalf("second start = %d %s", w.Code, w.Body.String())
	}

	w = e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"kind": "sprint"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad kind status = %d", w.Code)
	}
}

func TestStartRejectsLabelAndTaskTogether(t *testing.T) {
	e := newEnv(t, nil)
	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{
		"label": "x",
		"task":  map[string]string{"source": "tasks", "external_id": "42"},
	})
	if w.Code != http.StatusBadRequest || errorCode(t, w) != "invalid_input" {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestPauseResumeStop(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })
	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{})
	created := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)
	id := created.Session.ID.String()

	steps := []struct {
		path     string
		wantCode int
		wantErr  string
	}{
		{"/pause", http.StatusOK, ""},
		{"/pause", http.StatusConflict, "already_paused"},
		{"/resume", http.StatusOK, ""},
		{"/resume", http.StatusConflict, "not_paused"},
		{"/stop", http.StatusOK, ""},
	}
	for _, st := range steps {
		w := e.do(t, http.MethodPost, "/api/v1/sessions/"+id+st.path, nil)
		if w.Code != st.wantCode {
			t.Fatalf("%s status = %d, want %d (%s)", st.path, w.Code, st.wantCode, w.Body.String())
		}
		if st.wantErr != "" && errorCode(t, w) != st.wantErr {
			t.Fatalf("%s code = %s, want %s", st.path, errorCode(t, w), st.wantErr)
		}
	}

	w = e.do(t, http.MethodPost, "/api/v1/sessions/"+id+"/stop", nil)
	if w.Code != http.StatusConflict || errorCode(t, w) != "not_active" {
		t.Fatalf("stop ended = %d %s", w.Code, w.Body.String())
	}

	w = e.do(t, http.MethodPost, "/api/v1/sessions/"+uuid.NewString()+"/stop", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stop missing = %d", w.Code)
	}

	w = e.do(t, http.MethodPost, "/api/v1/sessions/not-a-uuid/stop", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("stop bad id = %d", w.Code)
	}
}

func TestStopWithOutcomeCompleted(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })
	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{})
	created := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)

	w = e.do(t, http.MethodPost, "/api/v1/sessions/"+created.Session.ID.String()+"/stop",
		map[string]string{"outcome": "completed"})
	if w.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", w.Code, w.Body.String())
	}
	resp := decode[struct {
		State engine.State `json:"state"`
	}](t, w)
	if resp.State.CompletedToday != 1 || resp.State.Phase != engine.PhaseIdle {
		t.Fatalf("state = %+v", resp.State)
	}
}

func TestPatchSessionRelabel(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })
	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"label": "старая"})
	created := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)
	id := created.Session.ID.String()
	e.do(t, http.MethodPost, "/api/v1/sessions/"+id+"/stop", map[string]string{"outcome": "completed"})

	w = e.do(t, http.MethodPatch, "/api/v1/sessions/"+id, map[string]any{
		"task": map[string]string{"source": "tasks", "external_id": "abc", "title_snapshot": "Выучить Go"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", w.Code, w.Body.String())
	}
	resp := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)
	if resp.Session.TaskExternalID == nil || *resp.Session.TaskExternalID != "abc" || resp.Session.RelabeledAt == nil {
		t.Fatalf("session = %+v", resp.Session)
	}

	w = e.do(t, http.MethodPatch, "/api/v1/sessions/"+uuid.NewString(), map[string]any{"label": "x"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("patch missing = %d", w.Code)
	}
}

func TestPatchBreakRejected(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })
	w := e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"kind": "break"})
	created := decode[struct {
		Session models.Session `json:"session"`
	}](t, w)
	id := created.Session.ID.String()
	e.do(t, http.MethodPost, "/api/v1/sessions/"+id+"/stop", nil)

	w = e.do(t, http.MethodPatch, "/api/v1/sessions/"+id, map[string]any{"label": "x"})
	if w.Code != http.StatusUnprocessableEntity || errorCode(t, w) != "not_focus" {
		t.Fatalf("patch break = %d %s", w.Code, w.Body.String())
	}
}

func TestActiveSessionAndList(t *testing.T) {
	e := newEnv(t, func(s *models.Settings) { s.AutoStartBreak = false })

	w := e.do(t, http.MethodGet, "/api/v1/sessions/active", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("active = %d", w.Code)
	}
	idle := decode[struct {
		Session *models.Session `json:"session"`
	}](t, w)
	if idle.Session != nil {
		t.Fatalf("idle active session = %+v", idle.Session)
	}

	e.do(t, http.MethodPost, "/api/v1/sessions", map[string]any{"label": "x"})
	w = e.do(t, http.MethodGet, "/api/v1/sessions/active", nil)
	got := decode[struct {
		Session *models.Session `json:"session"`
		State   engine.State    `json:"state"`
	}](t, w)
	if got.Session == nil || got.State.Phase != engine.PhaseFocus {
		t.Fatalf("active = %s", w.Body.String())
	}

	w = e.do(t, http.MethodGet, "/api/v1/sessions", nil)
	list := decode[struct {
		Sessions []models.Session `json:"sessions"`
	}](t, w)
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(list.Sessions))
	}

	w = e.do(t, http.MethodGet, "/api/v1/sessions?from=bad", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad from = %d", w.Code)
	}

	w = e.do(t, http.MethodGet, "/api/v1/sessions?from=2026-07-06&to=2026-07-06", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("date range = %d", w.Code)
	}
}

func TestSettingsGetPut(t *testing.T) {
	e := newEnv(t, nil)

	w := e.do(t, http.MethodGet, "/api/v1/settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get settings = %d", w.Code)
	}
	current := decode[models.Settings](t, w)
	if current.FocusDurationSeconds != 1500 {
		t.Fatalf("focus = %d, want 1500", current.FocusDurationSeconds)
	}

	current.FocusDurationSeconds = 3000
	current.DayBlocks = models.IntList{3, 3, 2}
	w = e.do(t, http.MethodPut, "/api/v1/settings", current)
	if w.Code != http.StatusOK {
		t.Fatalf("put settings = %d %s", w.Code, w.Body.String())
	}
	saved := decode[models.Settings](t, w)
	if saved.FocusDurationSeconds != 3000 || len(saved.DayBlocks) != 3 {
		t.Fatalf("saved = %+v", saved)
	}

	bad := saved
	bad.DayBlocks = models.IntList{}
	w = e.do(t, http.MethodPut, "/api/v1/settings", bad)
	if w.Code != http.StatusBadRequest || errorCode(t, w) != "invalid_input" {
		t.Fatalf("bad settings = %d %s", w.Code, w.Body.String())
	}
}

func TestStateEndpoint(t *testing.T) {
	e := newEnv(t, nil)
	w := e.do(t, http.MethodGet, "/api/v1/state", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("state = %d", w.Code)
	}
	st := decode[engine.State](t, w)
	if st.Phase != engine.PhaseIdle || st.DayTotal != 8 {
		t.Fatalf("state = %+v", st)
	}
}
