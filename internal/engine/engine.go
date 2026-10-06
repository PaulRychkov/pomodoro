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
	Phase              Phase           `json:"phase"`
	NextPhase          Phase           `json:"next_phase"`
	Paused             bool            `json:"paused"`
	SessionID          *uuid.UUID      `json:"session_id"`
	StartedAt          *time.Time      `json:"started_at"`
	PausedAt           *time.Time      `json:"paused_at"`
	PausedTotal        int             `json:"paused_total_seconds"`
	PlannedSeconds     int             `json:"planned_seconds"`
	RemainingSeconds   int             `json:"remaining_seconds"`
	Label              *string         `json:"label"`
	Task               *models.TaskRef `json:"task"`
	NextPlannedSeconds int             `json:"next_planned_seconds"`
	CompletedToday     int             `json:"completed_today"`
	CreditToday        float64         `json:"credit_today"`
	DayBlocks          []int           `json:"day_blocks"`
	BlockIndex         int             `json:"block_index"`
	PosInBlock         int             `json:"pos_in_block"`
	BlockSize          int             `json:"block_size"`
	DayTotal           int             `json:"day_total"`
	DayComplete        bool            `json:"day_complete"`
	SoundEnabled       bool            `json:"sound_enabled"`
	SettingsStamp      string          `json:"settings_stamp"`
}

type Notifier func(state State, reason string)

type DurationProvider func(pomodoroIdx int) (focusSeconds, breakSeconds *int)

type BlocksProvider func() []int

type Engine struct {
	mu             sync.Mutex
	store          store.Store
	clock          Clock
	notify         Notifier
	durations      DurationProvider
	blocks         BlocksProvider
	settings       models.Settings
	active         *models.Session
	nextPhase      Phase
	completedToday int
	creditToday    int
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

func (e *Engine) SetDurationProvider(p DurationProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.durations = p
}

func (e *Engine) SetBlocksProvider(p BlocksProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blocks = p
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
		e.nextPhase = e.breakPhaseForLocked(e.completedToday)
	}
	return nil
}

func (e *Engine) breakPhaseForLocked(completed int) Phase {
	if e.durations != nil && completed > 0 {
		if _, br := e.durations(completed - 1); br != nil && *br > 0 && e.settings.LongBreakSeconds > 0 {
			if *br >= e.settings.LongBreakSeconds {
				return PhaseLongBreak
			}
			return PhaseShortBreak
		}
	}
	return breakPhaseAfter(e.dayBlocksLocked(), completed)
}

func (e *Engine) dayBlocksLocked() []int {
	if e.blocks != nil {
		if b := e.blocks(); len(b) > 0 {
			return b
		}
	}
	return e.settings.DayBlocks
}

func (e *Engine) refreshNextBreakLocked() {
	if e.active == nil && (e.nextPhase == PhaseShortBreak || e.nextPhase == PhaseLongBreak) && e.completedToday > 0 {
		e.nextPhase = e.breakPhaseForLocked(e.completedToday)
	}
}

func (e *Engine) plannedFocusLocked() int {
	if e.durations != nil {
		if f, _ := e.durations(e.completedToday); f != nil && *f > 0 {
			return *f
		}
	}
	return e.settings.FocusDurationSeconds
}

func (e *Engine) plannedBreakLocked(phase Phase) int {
	planned := e.settings.ShortBreakSeconds
	if phase == PhaseLongBreak {
		planned = e.settings.LongBreakSeconds
	}
	if e.durations != nil && e.completedToday > 0 {
		if _, br := e.durations(e.completedToday - 1); br != nil && *br > 0 {
			planned = *br
		}
	}
	return planned
}

func settleFocus(s *models.Session, endAt time.Time) {
	if s.PausedAt != nil {
		s.PausedTotalSeconds += int(endAt.Sub(*s.PausedAt) / time.Second)
		s.PausedAt = nil
	}
	if endAt.Before(s.StartedAt) {
		endAt = s.StartedAt
	}
	s.EndedAt = &endAt
	if s.Kind != models.KindFocus {
		return
	}
	focus := int(endAt.Sub(s.StartedAt)/time.Second) - s.PausedTotalSeconds
	if focus < 0 {
		focus = 0
	}
	if focus > s.PlannedDurationSeconds && s.PlannedDurationSeconds > 0 {
		focus = s.PlannedDurationSeconds
	}
	s.FocusSeconds = &focus
}

func withCredit(s *models.Session, twelfths int) {
	if s.Kind != models.KindFocus {
		return
	}
	s.CreditTwelfths = &twelfths
}

func (e *Engine) markHangingInterrupted(ctx context.Context) error {
	hanging, err := e.store.Sessions().ListHanging(ctx)
	if err != nil {
		return err
	}
	now := e.clock.Now()
	for i := range hanging {
		s := hanging[i]
		settleFocus(&s, now)
		withCredit(&s, 0)
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

func (e *Engine) Now() time.Time {
	return e.clock.Now()
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
	e.refreshNextBreakLocked()
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
		PlannedDurationSeconds: e.plannedFocusLocked(),
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
	e.refreshNextBreakLocked()
	phase := e.nextPhase
	if phase != PhaseShortBreak && phase != PhaseLongBreak {
		phase = PhaseShortBreak
		if e.completedToday > 0 {
			phase = e.breakPhaseForLocked(e.completedToday)
		}
	}
	planned := e.plannedBreakLocked(phase)
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
	if outcome != "" && outcome != models.OutcomeAbandoned && outcome != models.OutcomeCompleted {
		return e.snapshotLocked(), fmt.Errorf("%w: outcome must be abandoned or completed", ErrInvalidInput)
	}
	if outcome == models.OutcomeCompleted && !e.earnsNothingLocked(now) {
		if err := e.completeLocked(ctx, now); err != nil {
			return e.snapshotLocked(), err
		}
		return e.snapshotLocked(), nil
	}
	s := e.active
	settleFocus(s, now)
	withCredit(s, 0)
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

func (e *Engine) earnsNothingLocked(now time.Time) bool {
	s := e.active
	if s == nil || s.Kind != models.KindFocus {
		return false
	}
	probe := *s
	settleFocus(&probe, now)
	return models.CreditTwelfthsFor(probe.ElapsedFocusSeconds(), probe.PlannedDurationSeconds) == 0
}

func (e *Engine) completeLocked(ctx context.Context, endAt time.Time) error {
	s := e.active
	if s == nil {
		return ErrNoActiveSession
	}
	settleFocus(s, endAt)
	withCredit(s, models.CreditTwelfthsFor(s.ElapsedFocusSeconds(), s.PlannedDurationSeconds))
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
			e.creditToday += s.Credit()
		}
		e.nextPhase = e.breakPhaseForLocked(e.completedToday)
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

func (e *Engine) ReloadSettings(ctx context.Context) (bool, error) {
	stored, err := e.store.Settings().Get(ctx)
	if err != nil {
		return false, fmt.Errorf("load settings: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if stored.UpdatedAt.Equal(e.settings.UpdatedAt) {
		return false, nil
	}
	e.settings = stored
	e.refreshNextBreakLocked()
	e.notifyLocked("settings")
	return true, nil
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
	e.refreshNextBreakLocked()
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
	if len(s.DayBlocks) > 24 {
		return fmt.Errorf("%w: at most 24 day blocks", ErrInvalidInput)
	}
	if s.DayStartMin < 0 || s.DayEndMin > 24*60 || s.DayEndMin-s.DayStartMin < 30 {
		return fmt.Errorf("%w: active day must be within 00:00..24:00 and last at least 30 minutes", ErrInvalidInput)
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
	e.refreshNextBreakLocked()
	blocks := e.dayBlocksLocked()
	st := State{
		Phase:          PhaseIdle,
		NextPhase:      e.nextPhase,
		CompletedToday: e.completedToday,
		CreditToday:    float64(e.creditToday*100/models.FullCreditTwelfths) / 100,
		DayBlocks:      append([]int{}, blocks...),
		DayTotal:       sum(blocks),
		SoundEnabled:   e.settings.SoundEnabled,
		SettingsStamp:  e.settings.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	block, pos := blockPos(blocks, e.completedToday)
	st.BlockIndex = block
	st.PosInBlock = pos
	st.BlockSize = blockSize(blocks, block)
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
		st.NextPlannedSeconds = st.PlannedSeconds
		st.RemainingSeconds = e.active.RemainingSeconds(now)
		st.Label = e.active.Label
		st.Task = e.active.TaskRef()
	} else if e.nextPhase == PhaseFocus {
		st.NextPlannedSeconds = e.plannedFocusLocked()
	} else {
		st.NextPlannedSeconds = e.plannedBreakLocked(e.nextPhase)
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
	sessions, err := e.store.Sessions().ListRange(ctx, from, to)
	if err != nil {
		return err
	}
	credit := 0
	for _, s := range sessions {
		if s.Kind == models.KindFocus && s.EndedAt != nil {
			credit += s.Credit()
		}
	}
	e.completedToday = n
	e.creditToday = credit
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
