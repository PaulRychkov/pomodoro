package tasksclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TaskOption struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	TopicPath  string `json:"topic_path"`
}

type Topic struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

type Client struct {
	baseURL string
	source  string
	http    *http.Client
}

func New(baseURL, source string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		source:  source,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/")
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

type rawTask struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	TopicID          *string `json:"topic_id"`
	Progress         string  `json:"progress"`
	IsActive         *bool   `json:"is_active"`
	StartTimeMin     *int    `json:"start_time_minutes"`
	EffortMinutes    *int    `json:"effort_minutes"`
	Priority         int     `json:"priority"`
	RequiresPomodoro *bool   `json:"requires_pomodoro"`
}

type DueTask struct {
	Option    TaskOption
	TopicID   *string
	EffortMin *int
	Priority  int
}

func (c *Client) Topics(ctx context.Context) ([]Topic, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/topics", nil)
	if err != nil {
		return nil, fmt.Errorf("build topics request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch topics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch topics: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read topics response: %w", err)
	}
	var out []Topic
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode topics response: %w", err)
	}
	return out, nil
}

type rawOccurrence struct {
	ID     string   `json:"id"`
	TaskID string   `json:"task_id"`
	Status string   `json:"status"`
	Task   *rawTask `json:"task"`
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]TaskOption, error) {
	if limit <= 0 {
		limit = 20
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tasks", nil)
	if err != nil {
		return nil, fmt.Errorf("build tasks request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch tasks: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read tasks response: %w", err)
	}
	raw, err := decodeTasks(body)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]TaskOption, 0, limit)
	for _, t := range raw {
		if t.ID == "" || t.Title == "" {
			continue
		}
		if t.IsActive != nil && !*t.IsActive {
			continue
		}
		if t.Progress == "completed" || t.Progress == "cancelled" {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(t.Title), needle) {
			continue
		}
		out = append(out, TaskOption{Source: c.source, ExternalID: t.ID, Title: t.Title})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *Client) Pickable(ctx context.Context) ([]DueTask, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tasks", nil)
	if err != nil {
		return nil, fmt.Errorf("build tasks request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch tasks: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read tasks response: %w", err)
	}
	raw, err := decodeTasks(body)
	if err != nil {
		return nil, err
	}
	out := make([]DueTask, 0, len(raw))
	for _, t := range raw {
		if t.ID == "" || t.Title == "" {
			continue
		}
		if t.IsActive != nil && !*t.IsActive {
			continue
		}
		if t.Progress == "completed" || t.Progress == "cancelled" {
			continue
		}
		if t.StartTimeMin != nil {
			continue
		}
		out = append(out, DueTask{
			Option:    TaskOption{Source: c.source, ExternalID: t.ID, Title: t.Title},
			TopicID:   t.TopicID,
			EffortMin: t.EffortMinutes,
			Priority:  t.Priority,
		})
	}
	return out, nil
}

func (c *Client) DueToday(ctx context.Context, now time.Time) ([]DueTask, error) {
	day := now.Format("2006-01-02")
	url := fmt.Sprintf("%s/api/v1/occurrences?from=%s&to=%s&status=pending", c.baseURL, day, day)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build occurrences request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch occurrences: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch occurrences: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read occurrences response: %w", err)
	}
	occs, err := decodeOccurrences(body)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []DueTask
	for _, o := range occs {
		t := o.Task
		if t == nil || t.ID == "" || t.Title == "" || seen[t.ID] {
			continue
		}
		if t.IsActive != nil && !*t.IsActive {
			continue
		}
		if t.Progress == "completed" || t.Progress == "cancelled" {
			continue
		}
		if t.RequiresPomodoro != nil && !*t.RequiresPomodoro {
			continue
		}
		if t.StartTimeMin != nil {
			continue
		}
		seen[t.ID] = true
		out = append(out, DueTask{
			Option:    TaskOption{Source: c.source, ExternalID: t.ID, Title: t.Title},
			TopicID:   t.TopicID,
			EffortMin: t.EffortMinutes,
			Priority:  t.Priority,
		})
	}
	return out, nil
}

func (c *Client) LogProgress(ctx context.Context, taskID string, minutes int, now time.Time) error {
	occ, err := c.todayOccurrence(ctx, taskID, now)
	if err != nil || occ == nil {
		return err
	}
	body := strings.NewReader(fmt.Sprintf(`{"minutes":%d}`, minutes))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/occurrences/%s/progress", c.baseURL, occ.ID), body)
	if err != nil {
		return fmt.Errorf("build progress request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("log progress: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("log progress: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) todayOccurrence(ctx context.Context, taskID string, now time.Time) (*rawOccurrence, error) {
	day := now.Format("2006-01-02")
	url := fmt.Sprintf("%s/api/v1/occurrences?from=%s&to=%s&status=pending", c.baseURL, day, day)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build occurrences request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch occurrences: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch occurrences: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read occurrences response: %w", err)
	}
	occs, err := decodeOccurrences(body)
	if err != nil {
		return nil, err
	}
	for i := range occs {
		if occs[i].TaskID == taskID && occs[i].ID != "" {
			return &occs[i], nil
		}
	}
	return nil, nil
}

func (c *Client) CompleteTodayOccurrence(ctx context.Context, taskID string, now time.Time) (bool, error) {
	day := now.Format("2006-01-02")
	url := fmt.Sprintf("%s/api/v1/occurrences?from=%s&to=%s&status=pending", c.baseURL, day, day)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("build occurrences request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetch occurrences: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch occurrences: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return false, fmt.Errorf("read occurrences response: %w", err)
	}
	occs, err := decodeOccurrences(body)
	if err != nil {
		return false, err
	}
	for _, o := range occs {
		if o.TaskID != taskID || o.ID == "" {
			continue
		}
		creq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/occurrences/%s/complete", c.baseURL, o.ID), strings.NewReader("{}"))
		if err != nil {
			return false, fmt.Errorf("build complete request: %w", err)
		}
		creq.Header.Set("Content-Type", "application/json")
		cresp, err := c.http.Do(creq)
		if err != nil {
			return false, fmt.Errorf("complete occurrence: %w", err)
		}
		cresp.Body.Close()
		if cresp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("complete occurrence: unexpected status %d", cresp.StatusCode)
		}
		return true, nil
	}
	return false, nil
}

func decodeOccurrences(body []byte) ([]rawOccurrence, error) {
	var list []rawOccurrence
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Occurrences []rawOccurrence `json:"occurrences"`
		Items       []rawOccurrence `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decode occurrences response: %w", err)
	}
	if wrapped.Occurrences != nil {
		return wrapped.Occurrences, nil
	}
	return wrapped.Items, nil
}

func decodeTasks(body []byte) ([]rawTask, error) {
	var list []rawTask
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Tasks []rawTask `json:"tasks"`
		Items []rawTask `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decode tasks response: %w", err)
	}
	if wrapped.Tasks != nil {
		return wrapped.Tasks, nil
	}
	return wrapped.Items, nil
}
