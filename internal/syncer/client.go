package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

type Client struct {
	Service  *Service
	BaseURL  string
	Token    string
	Interval time.Duration
	Log      *zap.Logger
	http     *http.Client
}

func NewClient(svc *Service, baseURL, token string, interval time.Duration, log *zap.Logger) *Client {
	return &Client{
		Service:  svc,
		BaseURL:  baseURL,
		Token:    token,
		Interval: interval,
		Log:      log,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Run(ctx context.Context) {
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *Client) tick(ctx context.Context) {
	if err := c.push(ctx); err != nil {
		c.Log.Warn("sync push", zap.Error(err))
	}
	if err := c.pull(ctx); err != nil {
		c.Log.Warn("sync pull", zap.Error(err))
	}
}

func (c *Client) push(ctx context.Context) error {
	cursor := c.Service.GetState(ctx, "push_cursor")
	started := time.Now().UTC()
	changes, err := c.Service.Collect(ctx, cursor)
	if err != nil {
		return err
	}
	if len(changes.Sessions) == 0 && len(changes.Settings) == 0 && len(changes.Presets) == 0 && len(changes.Schedule) == 0 && len(changes.Tombstones) == 0 {
		return nil
	}
	body, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/sync/changes", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push status %d: %.200s", resp.StatusCode, raw)
	}
	c.Service.SetState(ctx, "push_cursor", started)
	c.Log.Info("sync push ok",
		zap.Int("sessions", len(changes.Sessions)), zap.Int("presets", len(changes.Presets)),
		zap.Int("tombstones", len(changes.Tombstones)))
	return nil
}

func (c *Client) pull(ctx context.Context) error {
	cursor := c.Service.GetState(ctx, "pull_cursor")
	u := c.BaseURL + "/api/v1/sync/changes?since=" + url.QueryEscape(cursor.UTC().Format(time.RFC3339Nano))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull status %d: %.200s", resp.StatusCode, raw)
	}
	var changes Changes
	if err := json.Unmarshal(raw, &changes); err != nil {
		return fmt.Errorf("parse changes: %w", err)
	}
	res, err := c.Service.Apply(ctx, changes, false)
	if err != nil {
		return err
	}
	c.Service.SetState(ctx, "pull_cursor", changes.ServerTime)
	if res.Upserted > 0 || res.Deleted > 0 {
		c.Log.Info("sync pull ok", zap.Int("upserted", res.Upserted), zap.Int("deleted", res.Deleted))
	}
	return nil
}
