package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	KindFocus = "focus"
	KindBreak = "break"

	OutcomeCompleted   = "completed"
	OutcomeAbandoned   = "abandoned"
	OutcomeInterrupted = "interrupted"
)

type IntList []int

func (l IntList) Value() (driver.Value, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("marshal int list: %w", err)
	}
	return string(b), nil
}

func (l *IntList) Scan(src any) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, l)
	case string:
		return json.Unmarshal([]byte(v), l)
	case nil:
		*l = nil
		return nil
	default:
		return fmt.Errorf("scan int list: unsupported type %T", src)
	}
}

type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		m = JSONMap{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal json map: %w", err)
	}
	return string(b), nil
}

func (m *JSONMap) Scan(src any) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, m)
	case string:
		return json.Unmarshal([]byte(v), m)
	case nil:
		*m = JSONMap{}
		return nil
	default:
		return fmt.Errorf("scan json map: unsupported type %T", src)
	}
}

type OverlayConfig struct {
	Size           int     `json:"size"`
	DigitsSize     int     `json:"digits_size"`
	CircleOpacity  float64 `json:"circle_opacity"`
	DigitsOpacity  float64 `json:"digits_opacity"`
	ButtonsOpacity float64 `json:"buttons_opacity"`
	ShowTime       bool    `json:"show_time"`
	PosX           *int    `json:"pos_x,omitempty"`
	PosY           *int    `json:"pos_y,omitempty"`
}

func DefaultOverlay() OverlayConfig {
	return OverlayConfig{Size: 180, DigitsSize: 32, CircleOpacity: 0.9, DigitsOpacity: 0.95, ButtonsOpacity: 0.9, ShowTime: true}
}

func (o *OverlayConfig) Normalize() {
	if o.CircleOpacity == 0 {
		o.CircleOpacity = 0.9
	}
	if o.DigitsOpacity == 0 {
		o.DigitsOpacity = o.CircleOpacity
	}
	if o.ButtonsOpacity == 0 {
		o.ButtonsOpacity = o.CircleOpacity
	}
	if o.DigitsSize == 0 {
		o.DigitsSize = max(16, o.Size/6)
	}
}

func (o OverlayConfig) Value() (driver.Value, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("marshal overlay config: %w", err)
	}
	return string(b), nil
}

func (o *OverlayConfig) Scan(src any) error {
	*o = DefaultOverlay()
	var err error
	switch v := src.(type) {
	case []byte:
		err = json.Unmarshal(v, o)
	case string:
		err = json.Unmarshal([]byte(v), o)
	case nil:
	default:
		return fmt.Errorf("scan overlay config: unsupported type %T", src)
	}
	if err != nil {
		return err
	}
	o.Normalize()
	return nil
}

type Session struct {
	ID                     uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Kind                   string     `gorm:"type:session_kind;not null" json:"kind"`
	StartedAt              time.Time  `gorm:"not null" json:"started_at"`
	EndedAt                *time.Time `json:"ended_at"`
	PlannedDurationSeconds int        `gorm:"not null" json:"planned_duration_seconds"`
	Outcome                *string    `gorm:"type:session_outcome" json:"outcome"`
	PausedAt               *time.Time `json:"paused_at"`
	PausedTotalSeconds     int        `gorm:"not null;default:0" json:"paused_total_seconds"`
	Label                  *string    `json:"label"`
	TaskSource             *string    `json:"task_source"`
	TaskExternalID         *string    `json:"task_external_id"`
	TaskTitleSnapshot      *string    `json:"task_title_snapshot"`
	RelabeledAt            *time.Time `json:"relabeled_at"`
	CreatedAt              time.Time  `json:"created_at"`
}

func (Session) TableName() string { return "sessions" }

func (s Session) RemainingSeconds(now time.Time) int {
	if s.EndedAt != nil {
		return 0
	}
	elapsed := now.Sub(s.StartedAt) - time.Duration(s.PausedTotalSeconds)*time.Second
	if s.PausedAt != nil {
		elapsed -= now.Sub(*s.PausedAt)
	}
	remaining := s.PlannedDurationSeconds - int(elapsed/time.Second)
	if remaining < 0 {
		return 0
	}
	return remaining
}

type Settings struct {
	ID                   int           `gorm:"primaryKey" json:"-"`
	FocusDurationSeconds int           `gorm:"not null" json:"focus_duration_seconds"`
	ShortBreakSeconds    int           `gorm:"not null" json:"short_break_seconds"`
	LongBreakSeconds     int           `gorm:"not null" json:"long_break_seconds"`
	DayBlocks            IntList       `gorm:"type:jsonb;not null" json:"day_blocks"`
	AutoStartBreak       bool          `gorm:"not null" json:"auto_start_break"`
	AutoStartFocus       bool          `gorm:"not null" json:"auto_start_focus"`
	SoundEnabled         bool          `gorm:"not null" json:"sound_enabled"`
	SoundFile            *string       `json:"sound_file"`
	Overlay              OverlayConfig `gorm:"type:jsonb;not null" json:"overlay"`
	CreatedAt            time.Time     `json:"-"`
	UpdatedAt            time.Time     `json:"-"`
}

func (Settings) TableName() string { return "settings" }

func DefaultSettings() Settings {
	return Settings{
		ID:                   1,
		FocusDurationSeconds: 1500,
		ShortBreakSeconds:    300,
		LongBreakSeconds:     900,
		DayBlocks:            IntList{4, 4},
		AutoStartBreak:       true,
		AutoStartFocus:       false,
		SoundEnabled:         true,
		Overlay:              DefaultOverlay(),
	}
}

type OutboxEvent struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EventType     string    `gorm:"not null"`
	AggregateType string    `gorm:"not null"`
	AggregateID   uuid.UUID `gorm:"type:uuid;not null"`
	Payload       JSONMap   `gorm:"type:jsonb;not null"`
	CreatedAt     time.Time `gorm:"not null"`
	PublishedAt   *time.Time
	Attempts      int `gorm:"not null;default:0"`
	LastError     *string
}

func (OutboxEvent) TableName() string { return "events_outbox" }

type TaskRef struct {
	Source        string `json:"source"`
	ExternalID    string `json:"external_id"`
	TitleSnapshot string `json:"title_snapshot"`
}

func (s Session) TaskRef() *TaskRef {
	if s.TaskSource == nil || s.TaskExternalID == nil {
		return nil
	}
	title := ""
	if s.TaskTitleSnapshot != nil {
		title = *s.TaskTitleSnapshot
	}
	return &TaskRef{Source: *s.TaskSource, ExternalID: *s.TaskExternalID, TitleSnapshot: title}
}
