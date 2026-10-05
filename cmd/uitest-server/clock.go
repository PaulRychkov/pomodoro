package main

import (
	"fmt"
	"sync"
	"time"
)

// fakeClock идёт в реальном времени, но со сдвигом: так таймеры тикают сами,
// а тест может перепрыгнуть вперёд (advance) или поставить любой момент (set).
type fakeClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset).Round(0)
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = t.Sub(time.Now())
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

func (c *fakeClock) Offset() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offset
}

func (c *fakeClock) restore(offset time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = offset
}

// parseMoment понимает RFC3339, локальные «2026-10-05T05:50[:00]» (пробел вместо T
// тоже можно) и просто «05:50» — это время сегодняшнего (реального) дня в tz сервера.
func parseMoment(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04",
	} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			today := time.Now().In(loc)
			return time.Date(today.Year(), today.Month(), today.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse moment %q: expected RFC3339, YYYY-MM-DDTHH:MM[:SS] or HH:MM", s)
}
