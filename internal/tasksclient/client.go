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
	ID       string `json:"id"`
	Title    string `json:"title"`
	Progress string `json:"progress"`
	IsActive *bool  `json:"is_active"`
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
