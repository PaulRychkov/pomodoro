package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store"
)

var (
	ErrSessionActive   = errors.New("session already active")
	ErrNoActiveSession = errors.New("no active session")
	ErrAlreadyPaused   = errors.New("session already paused")
	ErrNotPaused       = errors.New("session is not paused")
	ErrNotFound        = errors.New("session not found")
	ErrNotActive       = errors.New("session is not active")
	ErrNotFocus        = errors.New("only focus sessions carry a binding")
	ErrInvalidInput    = errors.New("invalid input")
)

const tickInterval = 300 * time.Millisecond

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func SystemClock() Clock { return systemClock{} }

type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseFocus      Phase = "focus"
	PhaseShortBreak Phase = "short_break"
	PhaseLongBreak  Phase = "long_break"
)

type Binding struct {
	Label *string         `json:"label"`
	Task  *models.TaskRef `json:"task"`
}

func (b Binding) validate() error {
	if b.Label != nil && b.Task != nil {
		return fmt.Errorf("%w: label and task are mutually exclusive", ErrInvalidInput)
	}
	if b.Label != nil && *b.Label == "" {
		return fmt.Errorf("%w: label must not be empty", ErrInvalidInput)
	}
	if b.Task != nil && (b.Task.Source == "" || b.Task.ExternalID == "") {
		return fmt.Errorf("%w: task requires source and external_id", ErrInvalidInput)
	}
	return nil
}

type State struct {
	Phase            Phase           `json:"phase"`
	NextPhase        Phase           `json:"next_phase"`
	Paused           bool            `json:"paused"`
	SessionID        *uuid.UUID      `json:"session_id"`
	StartedAt        *time.Time      `json:"started_at"`
	PausedAt         *time.Time      `json:"paused_at"`
	PausedTotal      int             `json:"paused_total_seconds"`
	PlannedSeconds   int             `json:"planned_seconds"`
	RemainingSeconds int             `json:"remaining_seconds"`
	Label            *string         `json:"label"`
	Task             *models.TaskRef `json:"task"`
	CompletedToday   int             `json:"completed_today"`
	DayBlocks        []int           `json:"day_blocks"`
	BlockIndex       int             `json:"block_index"`
	PosInBlock       int             `json:"pos_in_block"`
	BlockSize        int             `json:"block_size"`
	DayTotal         int             `json:"day_total"`
	DayComplete      bool            `json:"day_complete"`
	SoundEnabled     bool            `json:"sound_enabled"`
}

type Notifier func(state State, reason string)

type Engine struct {
	mu             sync.Mutex
	store          store.Store
	clock          Clock
	notify         Notifier
	settings       models.Settings
	active         *models.Session
	nextPhase      Phase
	completedToday int
	dayKey         string
	lastBinding    Binding
}

func New(st store.Store, clock Clock) *Engine {
	return &Engine{
		store:     st,
		clock:     clock,
		notify:    func(State, string) {},
		nextPhase: PhaseFocus,
		settings:  models.DefaultSettings(),
	}
}

func (e *Engine) SetNotifier(n Notifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n != nil {
		e.notify = n
	}
}

func (e *Engine) Init(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.markHangingInterrupted(ctx); err != nil {
		return err
	}

	settings, err := e.store.Settings().Get(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	e.settings = settings

	now := e.clock.Now()
	if err := e.recountDayLocked(ctx, now); err != nil {
		return err
	}

	last, err := e.store.Sessions().LastStarted(ctx)
	if err != nil {
		return err
	}
	e.nextPhase = PhaseFocus
	if last != nil && last.Kind == models.KindFocus &&
		last.Outcome != nil && *last.Outcome == models.OutcomeCompleted &&
		sameLocalDay(last.StartedAt, now) && e.completedToday > 0 {
		e.nextPhase = breakPhaseAfter(e.settings.DayBlocks, e.completedToday)
	}
	return nil
}

func (e *Engine) markHangingInterrupted(ctx context.Context) error {
	hanging, err := e.store.Sessions().ListHanging(ctx)
	if err != nil {
		return err
	}
	now := e.clock.Now()
	for i := range hanging {
		s := hanging[i]
		if s.PausedAt != nil {
			s.PausedTotalSeconds += int(now.Sub(*s.PausedAt) / time.Second)
			s.PausedAt = nil
		}
		endedAt := now
		s.EndedAt = &endedAt
		outcome := models.OutcomeInterrupted
		s.Outcome = &outcome
		err := e.store.InTx(ctx, func(r store.Repos) error {
			if err := r.Sessions().Save(ctx, &s); err != nil {
				return err
			}
			if s.Kind == models.KindFocus {
				return r.Outbox().Insert(ctx, finishedEvent("pomodoro.interrupted", &s))
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("mark session interrupted: %w", err)
		}
	}
	return nil
}

func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.Tick(ctx)
		}
	}
}

func (e *Engine) Tick(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.clock.Now()
	if e.dayKey != localDayKey(now) {
		if err := e.recountDayLocked(ctx, now); err != nil {
			return err
		}
		e.notifyLocked("day_rolled")
	}
	if e.active == nil || e.active.PausedAt != nil {
		return nil
	}
	if e.active.RemainingSeconds(now) > 0 {
		return nil
	}
	endAt := e.active.StartedAt.Add(
		time.Duration(e.active.PlannedDurationSeconds+e.active.PausedTotalSeconds) * time.Second)
	return e.completeLocked(ctx, endAt)
}

func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *Engine) StartNext(ctx context.Context, b Binding) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active != nil {
		return e.snapshotLocked(), ErrSessionActive
	}
	if e.nextPhase == PhaseShortBreak || e.nextPhase == PhaseLongBreak {
		return e.startBreakLocked(ctx)
	}
	return e.startFocusLocked(ctx, b)
}

func (e *Engine) StartFocus(ctx context.Context, b Binding) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.startFocusLocked(ctx, b)
}

func (e *Engine) StartBreak(ctx context.Context) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.startBreakLocked(ctx)
}

func (e *Engine) startFocusLocked(ctx context.Context, b Binding) (State, error) {
	if e.active != nil {
		return e.snapshotLocked(), ErrSessionActive
	}
	if err := b.validate(); err != nil {
		return e.snapshotLocked(), err
	}
	now := e.clock.Now()
	s := &models.Session{
		ID:                     uuid.New(),
		Kind:                   models.KindFocus,
		StartedAt:              now,
		PlannedDurationSeconds: e.settings.FocusDurationSeconds,
		Label:                  b.Label,
	}
	if b.Task != nil {
		s.TaskSource = &b.Task.Source
		s.TaskExternalID = &b.Task.ExternalID
		if b.Task.TitleSnapshot != "" {
			title := b.Task.TitleSnapshot
			s.TaskTitleSnapshot = &title
		}
	}
	err := e.store.InTx(ctx, func(r store.Repos) error {
		if err := r.Sessions().Create(ctx, s); err != nil {
			return err
		}
		return r.Outbox().Insert(ctx, startedEvent(s))
	})
	if err != nil {
		return e.snapshotLocked(), fmt.Errorf("start focus: %w", err)
	}
	e.active = s
	e.nextPhase = PhaseFocus
	e.lastBinding = b
	e.notifyLocked("started")
	return e.snapshotLocked(), nil
}

func (e *Engine) startBreakLocked(ctx context.Context) (State, error) {
	if e.active != nil {
		return e.snapshotLocked(), ErrSessionActive
	}
	phase := e.nextPhase
	if phase != PhaseShortBreak && phase != PhaseLongBreak {
		phase = PhaseShortBreak
	}
	planned := e.settings.ShortBreakSeconds
	if phase == PhaseLongBreak {
		planned = e.settings.LongBreakSeconds
	}
	now := e.clock.Now()
	s := &models.Session{
		ID:                     uuid.New(),
		Kind:                   models.KindBreak,
		StartedAt:              now,
		PlannedDurationSeconds: planned,
	}
	if err := e.store.Sessions().Create(ctx, s); err != nil {
		return e.snapshotLocked(), fmt.Errorf("start break: %w", err)
	}
	e.active = s
	e.nextPhase = phase
	e.notifyLocked("started")
	return e.snapshotLocked(), nil
}

func (e *Engine) Pause(ctx context.Context) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pauseLocked(ctx)
}

func (e *Engine) PauseSession(ctx context.Context, id uuid.UUID) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.requireActiveLocked(ctx, id); err != nil {
		return e.snapshotLocked(), err
	}
	return e.pauseLocked(ctx)
}

func (e *Engine) pauseLocked(ctx context.Context) (State, error) {
	if e.active == nil {
		return e.snapshotLocked(), ErrNoActiveSession
	}
	if e.active.PausedAt != nil {
		return e.snapshotLocked(), ErrAlreadyPaused
	}
	now := e.clock.Now()
	e.active.PausedAt = &now
	if err := e.store.Sessions().Save(ctx, e.active); err != nil {
		e.active.PausedAt = nil
		return e.snapshotLocked(), fmt.Errorf("pause session: %w", err)
	}
	e.notifyLocked("paused")
	return e.snapshotLocked(), nil
}

func (e *Engine) Resume(ctx context.Context) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.resumeLocked(ctx)
}

func (e *Engine) ResumeSession(ctx context.Context, id uuid.UUID) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.requireActiveLocked(ctx, id); err != nil {
		return e.snapshotLocked(), err
	}
	return e.resumeLocked(ctx)
}

func (e *Engine) resumeLocked(ctx context.Context) (State, error) {
	if e.active == nil {
		return e.snapshotLocked(), ErrNoActiveSession
	}
	if e.active.PausedAt == nil {
		return e.snapshotLocked(), ErrNotPaused
	}
	now := e.clock.Now()
	pausedAt := *e.active.PausedAt
	delta := int(now.Sub(pausedAt) / time.Second)
	e.active.PausedTotalSeconds += delta
	e.active.PausedAt = nil
	if err := e.store.Sessions().Save(ctx, e.active); err != nil {
		e.active.PausedTotalSeconds -= delta
		e.active.PausedAt = &pausedAt
		return e.snapshotLocked(), fmt.Errorf("resume session: %w", err)
	}
	e.notifyLocked("resumed")
	return e.snapshotLocked(), nil
}

func (e *Engine) Stop(ctx context.Context, outcome string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopLocked(ctx, outcome)
}

func (e *Engine) StopSession(ctx context.Context, id uuid.UUID, outcome string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.requireActiveLocked(ctx, id); err != nil {
		return e.snapshotLocked(), err
	}
	return e.stopLocked(ctx, outcome)
}

func (e *Engine) requireActiveLocked(ctx context.Context, id uuid.UUID) error {
	if e.active != nil && e.active.ID == id {
		return nil
	}
	s, err := e.store.Sessions().Get(ctx, id)
	if err != nil {
		return err
	}
	if s == nil {
		return ErrNotFound
	}
	return ErrNotActive
}

func (e *Engine) stopLocked(ctx context.Context, outcome string) (State, error) {
	if e.active == nil {
		return e.snapshotLocked(), ErrNoActiveSession
	}
	now := e.clock.Now()
	if outcome == models.OutcomeCompleted {
		if err := e.completeLocked(ctx, now); err != nil {
			return e.snapshotLocked(), err
		}
		return e.snapshotLocked(), nil
	}
	if outcome != "" && outcome != models.OutcomeAbandoned {
		return e.snapshotLocked(), fmt.Errorf("%w: outcome must be abandoned or completed", ErrInvalidInput)
	}
	s := e.active
	if s.PausedAt != nil {
		s.PausedTotalSeconds += int(now.Sub(*s.PausedAt) / time.Second)
		s.PausedAt = nil
	}
	s.EndedAt = &now
	oc := models.OutcomeAbandoned
	s.Outcome = &oc
	err := e.store.InTx(ctx, func(r store.Repos) error {
		if err := r.Sessions().Save(ctx, s); err != nil {
			return err
		}
		if s.Kind == models.KindFocus {
			return r.Outbox().Insert(ctx, finishedEvent("pomodoro.abandoned", s))
		}
		return nil
	})
	if err != nil {
		return e.snapshotLocked(), fmt.Errorf("stop session: %w", err)
	}
	e.active = nil
	e.nextPhase = PhaseFocus
	e.notifyLocked("stopped")
	return e.snapshotLocked(), nil
}

func (e *Engine) completeLocked(ctx context.Context, endAt time.Time) error {
	s := e.active
	if s == nil {
		return ErrNoActiveSession
	}
	if s.PausedAt != nil {
		s.PausedTotalSeconds += int(endAt.Sub(*s.PausedAt) / time.Second)
		s.PausedAt = nil
	}
	if endAt.Before(s.StartedAt) {
		endAt = s.StartedAt
	}
	s.EndedAt = &endAt
	oc := models.OutcomeCompleted
	s.Outcome = &oc
	err := e.store.InTx(ctx, func(r store.Repos) error {
		if err := r.Sessions().Save(ctx, s); err != nil {
			return err
		}
		if s.Kind == models.KindFocus {
			return r.Outbox().Insert(ctx, finishedEvent("pomodoro.completed", s))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete session: %w", err)
	}
	wasFocus := s.Kind == models.KindFocus
	e.active = nil
	if wasFocus {
		if localDayKey(s.StartedAt) == e.dayKey {
			e.completedToday++
		}
		e.nextPhase = breakPhaseAfter(e.settings.DayBlocks, e.completedToday)
	} else {
		e.nextPhase = PhaseFocus
	}
	e.notifyLocked("completed")

	if wasFocus && e.settings.AutoStartBreak {
		if _, err := e.startBreakLocked(ctx); err != nil {
			return err
		}
	} else if !wasFocus && e.settings.AutoStartFocus {
		if _, err := e.startFocusLocked(ctx, e.lastBinding); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) Relabel(ctx context.Context, id uuid.UUID, b Binding) (*models.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := b.validate(); err != nil {
		return nil, err
	}
	s, err := e.store.Sessions().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrNotFound
	}
	if s.Kind != models.KindFocus {
		return nil, ErrNotFocus
	}
	old := bindingPayload(s)
	s.Label = b.Label
	s.TaskSource = nil
	s.TaskExternalID = nil
	s.TaskTitleSnapshot = nil
	if b.Task != nil {
		s.TaskSource = &b.Task.Source
		s.TaskExternalID = &b.Task.ExternalID
		if b.Task.TitleSnapshot != "" {
			title := b.Task.TitleSnapshot
			s.TaskTitleSnapshot = &title
		}
	}
	if s.EndedAt != nil {
		now := e.clock.Now()
		s.RelabeledAt = &now
		err = e.store.InTx(ctx, func(r store.Repos) error {
			if err := r.Sessions().Save(ctx, s); err != nil {
				return err
			}
			return r.Outbox().Insert(ctx, relabeledEvent(s, old))
		})
	} else {
		err = e.store.Sessions().Save(ctx, s)
	}
	if err != nil {
		return nil, fmt.Errorf("relabel session: %w", err)
	}
	if e.active != nil && e.active.ID == s.ID {
		e.active = s
		e.lastBinding = b
	}
	e.notifyLocked("relabeled")
	return s, nil
}

func (e *Engine) Settings() models.Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.settings
}

func (e *Engine) UpdateSettings(ctx context.Context, s models.Settings) (models.Settings, error) {
	s.Overlay.Normalize()
	if err := ValidateSettings(s); err != nil {
		return models.Settings{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s.ID = 1
	if err := e.store.Settings().Save(ctx, &s); err != nil {
		return models.Settings{}, err
	}
	e.settings = s
	if e.active == nil && (e.nextPhase == PhaseShortBreak || e.nextPhase == PhaseLongBreak) && e.completedToday > 0 {
		e.nextPhase = breakPhaseAfter(s.DayBlocks, e.completedToday)
	}
	e.notifyLocked("settings")
	return e.settings, nil
}

func ValidateSettings(s models.Settings) error {
	if s.FocusDurationSeconds <= 0 || s.ShortBreakSeconds <= 0 || s.LongBreakSeconds <= 0 {
		return fmt.Errorf("%w: durations must be positive", ErrInvalidInput)
	}
	if len(s.DayBlocks) == 0 {
		return fmt.Errorf("%w: day_blocks must not be empty", ErrInvalidInput)
	}
	for _, b := range s.DayBlocks {
		if b < 1 || b > 16 {
			return fmt.Errorf("%w: each day block must contain 1..16 pomodoros", ErrInvalidInput)
		}
	}
	if len(s.DayBlocks) > 8 {
		return fmt.Errorf("%w: at most 8 day blocks", ErrInvalidInput)
	}
	if s.Overlay.Size < 60 || s.Overlay.Size > 600 {
		return fmt.Errorf("%w: overlay size must be 60..600", ErrInvalidInput)
	}
	if s.Overlay.DigitsSize < 8 || s.Overlay.DigitsSize > 240 {
		return fmt.Errorf("%w: overlay digits size must be 8..240", ErrInvalidInput)
	}
	for name, v := range map[string]float64{
		"circle_opacity":  s.Overlay.CircleOpacity,
		"digits_opacity":  s.Overlay.DigitsOpacity,
		"buttons_opacity": s.Overlay.ButtonsOpacity,
	} {
		if v < 0.05 || v > 1 {
			return fmt.Errorf("%w: overlay %s must be 0.05..1", ErrInvalidInput, name)
		}
	}
	return nil
}

func (e *Engine) snapshotLocked() State {
	now := e.clock.Now()
	st := State{
		Phase:          PhaseIdle,
		NextPhase:      e.nextPhase,
		CompletedToday: e.completedToday,
		DayBlocks:      append([]int{}, e.settings.DayBlocks...),
		DayTotal:       sum(e.settings.DayBlocks),
		SoundEnabled:   e.settings.SoundEnabled,
	}
	block, pos := blockPos(e.settings.DayBlocks, e.completedToday)
	st.BlockIndex = block
	st.PosInBlock = pos
	st.BlockSize = blockSize(e.settings.DayBlocks, block)
	st.DayComplete = st.DayTotal > 0 && e.completedToday >= st.DayTotal

	if e.active != nil {
		if e.active.Kind == models.KindFocus {
			st.Phase = PhaseFocus
		} else {
			st.Phase = e.nextPhase
		}
		id := e.active.ID
		started := e.active.StartedAt
		st.SessionID = &id
		st.StartedAt = &started
		st.PausedAt = e.active.PausedAt
		st.Paused = e.active.PausedAt != nil
		st.PausedTotal = e.active.PausedTotalSeconds
		st.PlannedSeconds = e.active.PlannedDurationSeconds
		st.RemainingSeconds = e.active.RemainingSeconds(now)
		st.Label = e.active.Label
		st.Task = e.active.TaskRef()
	}
	return st
}

func (e *Engine) notifyLocked(reason string) {
	e.notify(e.snapshotLocked(), reason)
}

func (e *Engine) recountDayLocked(ctx context.Context, now time.Time) error {
	from, to := LocalDayBounds(now)
	n, err := e.store.Sessions().CountCompletedFocusBetween(ctx, from, to)
	if err != nil {
		return err
	}
	e.completedToday = n
	e.dayKey = localDayKey(now)
	if e.active == nil {
		e.nextPhase = PhaseFocus
	}
	return nil
}

func breakPhaseAfter(blocks []int, completed int) Phase {
	if completed <= 0 {
		return PhaseShortBreak
	}
	block, pos := blockPos(blocks, completed-1)
	if pos == blockSize(blocks, block)-1 {
		return PhaseLongBreak
	}
	return PhaseShortBreak
}

func blockPos(blocks []int, idx int) (int, int) {
	if len(blocks) == 0 {
		blocks = []int{4}
	}
	block := 0
	for {
		size := blockSize(blocks, block)
		if idx < size {
			return block, idx
		}
		idx -= size
		block++
	}
}

func blockSize(blocks []int, block int) int {
	if len(blocks) == 0 {
		return 4
	}
	if block >= len(blocks) {
		return blocks[len(blocks)-1]
	}
	return blocks[block]
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func localDayKey(t time.Time) string {
	return t.Local().Format("2006-01-02")
}

func LocalDayBounds(t time.Time) (time.Time, time.Time) {
	local := t.Local()
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return start, start.Add(24 * time.Hour)
}

func sameLocalDay(a, b time.Time) bool {
	return localDayKey(a) == localDayKey(b)
}
