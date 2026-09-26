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
	FocusSeconds           *int       `json:"focus_seconds"`
	CreditTwelfths         *int       `json:"credit_twelfths"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (Session) TableName() string { return "sessions" }

const FullCreditTwelfths = 12

var creditSteps = []int{0, 3, 4, 6, 8, 9, 12}

func CreditTwelfthsFor(focusSeconds, plannedSeconds int) int {
	if plannedSeconds <= 0 || focusSeconds <= 0 {
		return 0
	}
	if focusSeconds >= plannedSeconds {
		return FullCreditTwelfths
	}
	best := creditSteps[0]
	bestDist := -1
	for _, step := range creditSteps {
		dist := abs(focusSeconds*FullCreditTwelfths - step*plannedSeconds)
		if bestDist < 0 || dist <= bestDist {
			best, bestDist = step, dist
		}
	}
	return best
}

func CreditLabel(twelfths int) string {
	switch twelfths {
	case 0:
		return "0"
	case 3:
		return "1/4"
	case 4:
		return "1/3"
	case 6:
		return "1/2"
	case 8:
		return "2/3"
	case 9:
		return "3/4"
	case FullCreditTwelfths:
		return "1"
	}
	return fmt.Sprintf("%d/12", twelfths)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (s Session) ElapsedFocusSeconds() int {
	if s.FocusSeconds != nil {
		return *s.FocusSeconds
	}
	if s.EndedAt == nil {
		return 0
	}
	sec := int(s.EndedAt.Sub(s.StartedAt)/time.Second) - s.PausedTotalSeconds
	if sec < 0 {
		return 0
	}
	return sec
}

func (s Session) Credit() int {
	if s.CreditTwelfths != nil {
		return *s.CreditTwelfths
	}
	if s.Kind == KindFocus && s.Outcome != nil && *s.Outcome == OutcomeCompleted {
		return FullCreditTwelfths
	}
	return 0
}

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
	PlanBeforeWindowMin  int           `gorm:"column:plan_before_window_minutes;not null;default:80" json:"plan_before_window_minutes"`
	PlanAfterWindowMin   int           `gorm:"column:plan_after_window_minutes;not null;default:90" json:"plan_after_window_minutes"`
	WindowSharePercent   int           `gorm:"column:window_share_percent;not null;default:67" json:"window_share_percent"`
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
		PlanBeforeWindowMin:  80,
		PlanAfterWindowMin:   90,
		WindowSharePercent:   67,
		AutoStartBreak:       false,
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

type PlanSlot struct {
	Date           string    `gorm:"primaryKey" json:"date"`
	Idx            int       `gorm:"primaryKey" json:"idx"`
	TaskSource     *string   `json:"task_source"`
	TaskExternalID *string   `json:"task_external_id"`
	TaskTitle      *string   `json:"task_title"`
	Label          *string   `json:"label"`
	FocusSeconds   *int      `json:"focus_seconds"`
	BreakSeconds   *int      `json:"break_seconds"`
	Pinned         bool      `gorm:"not null;default:false" json:"pinned"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (PlanSlot) TableName() string { return "plan_slots" }

func (s PlanSlot) Task() *TaskRef {
	if s.TaskSource == nil || s.TaskExternalID == nil {
		return nil
	}
	title := ""
	if s.TaskTitle != nil {
		title = *s.TaskTitle
	}
	return &TaskRef{Source: *s.TaskSource, ExternalID: *s.TaskExternalID, TitleSnapshot: title}
}

func (s *PlanSlot) SetTask(t *TaskRef) {
	if t == nil {
		s.TaskSource, s.TaskExternalID, s.TaskTitle = nil, nil, nil
		return
	}
	src, ext, title := t.Source, t.ExternalID, t.TitleSnapshot
	s.TaskSource, s.TaskExternalID, s.TaskTitle = &src, &ext, &title
}

type PresetSlot struct {
	FocusMinutes int `json:"focus_minutes"`
	BreakMinutes int `json:"break_minutes"`
}

type PresetSlots []PresetSlot

func (s PresetSlots) Value() (driver.Value, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal preset slots: %w", err)
	}
	return string(b), nil
}

func (s *PresetSlots) Scan(value any) error {
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	case nil:
		*s = nil
		return nil
	}
	return fmt.Errorf("unsupported preset slots type %T", value)
}

func (PresetSlots) GormDataType() string { return "jsonb" }

type Preset struct {
	ID        uuid.UUID   `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name      string      `gorm:"not null;uniqueIndex" json:"name"`
	Slots     PresetSlots `gorm:"type:jsonb;not null" json:"slots"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (Preset) TableName() string { return "presets" }

type PresetAssignment struct {
	Weekday   int       `gorm:"primaryKey" json:"weekday"`
	PresetID  uuid.UUID `gorm:"type:uuid;not null" json:"preset_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (PresetAssignment) TableName() string { return "preset_schedule" }

type SyncTombstone struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	Table     string    `gorm:"column:table_name;not null" json:"table"`
	RowID     string    `gorm:"column:row_id;not null" json:"row_id"`
	DeletedAt time.Time `gorm:"not null" json:"deleted_at"`
}

func (SyncTombstone) TableName() string { return "sync_tombstones" }

type SyncState struct {
	Key   string `gorm:"primaryKey"`
	Value string `gorm:"not null"`
}

func (SyncState) TableName() string { return "sync_state" }
