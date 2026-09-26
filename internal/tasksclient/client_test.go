package tasksclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchFiltersAndMaps(t *testing.T) {
	body := `[
		{"id":"a1","title":"Выучить Go","progress":"in_process","is_active":true},
		{"id":"a2","title":"Прочитать RFC 8984","progress":"needs_action","is_active":true},
		{"id":"a3","title":"Старая задача","progress":"completed","is_active":true},
		{"id":"a4","title":"Выключенная","progress":"needs_action","is_active":false},
		{"id":"","title":"Без id"}
	]`
	srv := newServer(t, http.StatusOK, body)
	c := New(srv.URL, "tasks")

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"empty query returns actionable", "", []string{"Выучить Go", "Прочитать RFC 8984"}},
		{"substring match", "rfc", []string{"Прочитать RFC 8984"}},
		{"cyrillic match", "выучить", []string{"Выучить Go"}},
		{"no match", "docker", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Search(context.Background(), tt.query, 10)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results %v, want %d", len(got), got, len(tt.want))
			}
			for i, w := range tt.want {
				if got[i].Title != w || got[i].Source != "tasks" {
					t.Fatalf("result[%d] = %+v, want title %s", i, got[i], w)
				}
			}
		})
	}
}

func TestDueTodayMarksFixedEventsWithoutPomodoros(t *testing.T) {
	body := `[
		{"id":"o1","task_id":"job","task":{"id":"job","title":"Поиск работы","progress":"needs_action","effort_minutes":690,"requires_pomodoro":true}},
		{"id":"o2","task_id":"work","task":{"id":"work","title":"Работа","progress":"needs_action","start_time_minutes":540,"estimated_duration_minutes":240,"requires_pomodoro":true}},
		{"id":"o3","task_id":"gym","task":{"id":"gym","title":"Зал","progress":"needs_action","start_time_minutes":420,"estimated_duration_minutes":40,"requires_pomodoro":false}},
		{"id":"o4","task_id":"read","task":{"id":"read","title":"Почитать когда-нибудь","progress":"needs_action","requires_pomodoro":false}}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/occurrences" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	got, err := New(srv.URL, "tasks").DueToday(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("due today: %v", err)
	}
	blocked := map[string]bool{}
	for _, d := range got {
		blocked[d.Option.ExternalID] = d.Blocked
	}
	if len(got) != 3 {
		t.Fatalf("гибкая задача без помидоров в план не идёт: %+v", blocked)
	}
	if blocked["job"] || blocked["work"] {
		t.Fatalf("задачи с помидорами не блокируют время: %+v", blocked)
	}
	if !blocked["gym"] {
		t.Fatalf("событие с фиксированным временем без помидоров обязано блокировать время: %+v", blocked)
	}
}

func TestSearchWrappedResponse(t *testing.T) {
	srv := newServer(t, http.StatusOK, `{"tasks":[{"id":"x","title":"Обёрнутая"}]}`)
	c := New(srv.URL, "ext")
	got, err := c.Search(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].ExternalID != "x" || got[0].Source != "ext" {
		t.Fatalf("got = %+v", got)
	}
}

func TestSearchLimit(t *testing.T) {
	srv := newServer(t, http.StatusOK, `[
		{"id":"1","title":"t1"},{"id":"2","title":"t2"},{"id":"3","title":"t3"}
	]`)
	c := New(srv.URL, "tasks")
	got, err := c.Search(context.Background(), "", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestSearchErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"server error", http.StatusInternalServerError, "boom"},
		{"invalid json", http.StatusOK, "{not json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, tt.status, tt.body)
			c := New(srv.URL, "tasks")
			if _, err := c.Search(context.Background(), "", 10); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}
