package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store"
)

type Mem struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]models.Session
	settings models.Settings
	outbox   map[uuid.UUID]models.OutboxEvent
	plans    map[string][]models.PlanSlot
	presets  map[uuid.UUID]models.Preset
	schedule map[int]uuid.UUID
	seq      int
}

func New() *Mem {
	return &Mem{
		sessions: map[uuid.UUID]models.Session{},
		settings: models.DefaultSettings(),
		outbox:   map[uuid.UUID]models.OutboxEvent{},
		plans:    map[string][]models.PlanSlot{},
		presets:  map[uuid.UUID]models.Preset{},
		schedule: map[int]uuid.UUID{},
	}
}

func (m *Mem) Sessions() store.SessionRepo  { return (*memSessions)(m) }
func (m *Mem) Settings() store.SettingsRepo { return (*memSettings)(m) }
func (m *Mem) Outbox() store.OutboxRepo     { return (*memOutbox)(m) }
func (m *Mem) Plans() store.PlanRepo        { return (*memPlans)(m) }
func (m *Mem) Presets() store.PresetRepo    { return (*memPresets)(m) }

type memPresets Mem

func (m *memPresets) List(_ context.Context) ([]models.Preset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.Preset, 0, len(m.presets))
	for _, p := range m.presets {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memPresets) GetByName(_ context.Context, name string) (*models.Preset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.presets {
		if p.Name == name {
			cp := p
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memPresets) Save(_ context.Context, p *models.Preset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.presets {
		if existing.Name == p.Name {
			p.ID = id
			m.presets[id] = *p
			return nil
		}
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	m.presets[p.ID] = *p
	return nil
}

func (m *memPresets) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.presets, id)
	for wd, pid := range m.schedule {
		if pid == id {
			delete(m.schedule, wd)
		}
	}
	return nil
}

func (m *memPresets) Schedule(_ context.Context) ([]models.PresetAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.PresetAssignment, 0, len(m.schedule))
	for wd, pid := range m.schedule {
		out = append(out, models.PresetAssignment{Weekday: wd, PresetID: pid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Weekday < out[j].Weekday })
	return out, nil
}

func (m *memPresets) Assign(_ context.Context, weekday int, presetID *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if presetID == nil {
		delete(m.schedule, weekday)
		return nil
	}
	m.schedule[weekday] = *presetID
	return nil
}

type memPlans Mem

func (m *memPlans) ListDay(_ context.Context, date string) ([]models.PlanSlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]models.PlanSlot(nil), m.plans[date]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Idx < out[j].Idx })
	return out, nil
}

func (m *memPlans) ReplaceDay(_ context.Context, date string, slots []models.PlanSlot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[date] = append([]models.PlanSlot(nil), slots...)
	return nil
}

func (m *memPlans) Upsert(_ context.Context, slot *models.PlanSlot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	day := m.plans[slot.Date]
	for i := range day {
		if day[i].Idx == slot.Idx {
			day[i] = *slot
			return nil
		}
	}
	m.plans[slot.Date] = append(day, *slot)
	return nil
}

func (m *Mem) InTx(ctx context.Context, fn func(r store.Repos) error) error {
	return fn(m)
}

func (m *Mem) OutboxEvents() []models.OutboxEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.OutboxEvent, 0, len(m.outbox))
	for _, e := range m.outbox {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Mem) OutboxTypes() []string {
	events := m.OutboxEvents()
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.EventType)
	}
	return types
}

func (m *Mem) SessionByID(id uuid.UUID) (models.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Mem) AllSessions() []models.Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

type memSessions Mem

func (m *memSessions) Create(ctx context.Context, s *models.Session) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.CreatedAt.IsZero() {
		mm.seq++
		s.CreatedAt = time.Now().Add(time.Duration(mm.seq) * time.Nanosecond)
	}
	mm.sessions[s.ID] = *s
	return nil
}

func (m *memSessions) Save(ctx context.Context, s *models.Session) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.sessions[s.ID] = *s
	return nil
}

func (m *memSessions) Get(ctx context.Context, id uuid.UUID) (*models.Session, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	s, ok := mm.sessions[id]
	if !ok {
		return nil, nil
	}
	dup := s
	return &dup, nil
}

func (m *memSessions) FindActive(ctx context.Context) (*models.Session, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	for _, s := range mm.sessions {
		if s.EndedAt == nil {
			dup := s
			return &dup, nil
		}
	}
	return nil, nil
}

func (m *memSessions) ListHanging(ctx context.Context) ([]models.Session, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	var out []models.Session
	for _, s := range mm.sessions {
		if s.EndedAt == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memSessions) ListRange(ctx context.Context, from, to time.Time) ([]models.Session, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	var out []models.Session
	for _, s := range mm.sessions {
		if !s.StartedAt.Before(from) && s.StartedAt.Before(to) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (m *memSessions) LastStarted(ctx context.Context) (*models.Session, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	var last *models.Session
	for _, s := range mm.sessions {
		dup := s
		if last == nil || dup.StartedAt.After(last.StartedAt) {
			last = &dup
		}
	}
	return last, nil
}

func (m *memSessions) CountCompletedFocusBetween(ctx context.Context, from, to time.Time) (int, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	n := 0
	for _, s := range mm.sessions {
		if s.Kind == models.KindFocus && s.Outcome != nil && *s.Outcome == models.OutcomeCompleted &&
			!s.StartedAt.Before(from) && s.StartedAt.Before(to) {
			n++
		}
	}
	return n, nil
}

type memSettings Mem

func (m *memSettings) Get(ctx context.Context) (models.Settings, error) {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return mm.settings, nil
}

func (m *memSettings) Save(ctx context.Context, s *models.Settings) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.settings = *s
	return nil
}

type memOutbox Mem

func (m *memOutbox) Insert(ctx context.Context, e *models.OutboxEvent) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		mm.seq++
		e.CreatedAt = time.Now().Add(time.Duration(mm.seq) * time.Nanosecond)
	}
	mm.outbox[e.ID] = *e
	return nil
}

func (m *memOutbox) Unpublished(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	mm := (*Mem)(m)
	events := mm.OutboxEvents()
	var out []models.OutboxEvent
	for _, e := range events {
		if e.PublishedAt == nil {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memOutbox) MarkPublished(ctx context.Context, id uuid.UUID, at time.Time) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	e, ok := mm.outbox[id]
	if !ok {
		return nil
	}
	e.PublishedAt = &at
	mm.outbox[id] = e
	return nil
}

func (m *memOutbox) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	mm := (*Mem)(m)
	mm.mu.Lock()
	defer mm.mu.Unlock()
	e, ok := mm.outbox[id]
	if !ok {
		return nil
	}
	e.Attempts++
	e.LastError = &message
	mm.outbox[id] = e
	return nil
}
