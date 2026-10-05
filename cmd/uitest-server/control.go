package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type resetRequest struct {
	Now      string          `json:"now"`
	Tasks    *fixture        `json:"tasks"`
	Settings json.RawMessage `json:"settings"`
}

// reset пересоздаёт весь стек с чистой БД. Часы и фейковый planner выставляются
// до сборки: engine.Init и план читают время и задачи сразу.
func (s *server) reset(req resetRequest) error {
	s.resetMu.Lock()
	defer s.resetMu.Unlock()

	settings, err := mergeSettings(req.Settings)
	if err != nil {
		return badRequest{err}
	}
	prevOffset := s.clock.Offset()
	if req.Now != "" {
		t, err := parseMoment(req.Now, s.loc)
		if err != nil {
			return badRequest{err}
		}
		s.clock.Set(t)
	}
	gen := s.tasks.Reset(mergeFixture(req.Tasks))

	next, err := newStack(stackConfig{
		dataDir:  s.dataDir,
		web:      s.web,
		tasksURL: fmt.Sprintf("%s/g%d", s.tasksBase, gen),
		clock:    s.clock,
		settings: settings,
		log:      s.log,
	})
	if err != nil {
		s.clock.restore(prevOffset)
		return err
	}

	s.mu.Lock()
	prev := s.cur
	s.cur = next
	s.mu.Unlock()
	if prev != nil {
		prev.close()
	}
	return nil
}

func mergeFixture(override *fixture) fixture {
	fx := defaultFixture()
	if override == nil {
		return fx
	}
	if override.Tasks != nil {
		fx.Tasks = override.Tasks
	}
	if override.Topics != nil {
		fx.Topics = override.Topics
	}
	return fx
}

type badRequest struct{ error }

func (s *server) tick() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur.engine.Tick(context.Background())
}

func (s *server) clockView() map[string]any {
	return map[string]any{
		"now":       s.clock.Now().Format(time.RFC3339Nano),
		"offset_ms": s.clock.Offset().Milliseconds(),
	}
}

func (s *server) registerControl(mux *http.ServeMux) {
	mux.HandleFunc("POST /__test/reset", func(w http.ResponseWriter, r *http.Request) {
		var req resetRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if err := s.reset(req); err != nil {
			writeControlError(w, err)
			return
		}
		s.mu.RLock()
		state := s.cur.engine.Snapshot()
		s.mu.RUnlock()
		out := s.clockView()
		out["state"] = state
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /__test/clock", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.clockView())
	})

	mux.HandleFunc("POST /__test/clock", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Now            string   `json:"now"`
			AdvanceSeconds *float64 `json:"advance_seconds"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		switch {
		case body.Now != "" && body.AdvanceSeconds != nil:
			writeControlError(w, badRequest{errors.New("pass either now or advance_seconds")})
			return
		case body.Now != "":
			t, err := parseMoment(body.Now, s.loc)
			if err != nil {
				writeControlError(w, badRequest{err})
				return
			}
			s.clock.Set(t)
		case body.AdvanceSeconds != nil:
			s.clock.Advance(time.Duration(*body.AdvanceSeconds * float64(time.Second)))
		default:
			writeControlError(w, badRequest{errors.New("now or advance_seconds is required")})
			return
		}
		// Tick сразу, а не через 300 мс: к ответу истёкшие сессии уже завершены.
		if err := s.tick(); err != nil {
			writeControlError(w, err)
			return
		}
		s.mu.RLock()
		state := s.cur.engine.Snapshot()
		s.mu.RUnlock()
		out := s.clockView()
		out["state"] = state
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /__test/tasks-log", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.tasks.Log())
	})

	mux.HandleFunc("DELETE /__test/tasks-log", func(w http.ResponseWriter, _ *http.Request) {
		s.tasks.ClearLog()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /__test/tasks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.tasks.Fixture())
	})

	mux.HandleFunc("PUT /__test/tasks", func(w http.ResponseWriter, r *http.Request) {
		var fx fixture
		if !decodeBody(w, r, &fx) {
			return
		}
		s.tasks.Replace(mergeFixture(&fx))
		w.WriteHeader(http.StatusNoContent)
	})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeControlError(w, badRequest{fmt.Errorf("invalid json: %w", err)})
		return false
	}
	return true
}

func writeControlError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var bad badRequest
	if errors.As(err, &bad) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
