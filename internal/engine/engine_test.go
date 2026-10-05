package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store/memstore"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 7, 5, 10, 0, 0, 0, time.Local)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestEngine(t *testing.T, mutate func(*models.Settings)) (*Engine, *memstore.Mem, *fakeClock) {
	t.Helper()
	mem := memstore.New()
	if mutate != nil {
		s, _ := mem.Settings().Get(context.Background())
		mutate(&s)
		if err := mem.Settings().Save(context.Background(), &s); err != nil {
			t.Fatalf("save settings: %v", err)
		}
	}
	clock := newFakeClock()
	e := New(mem, clock)
	if err := e.Init(context.Background()); err != nil {
		t.Fatalf("init engine: %v", err)
	}
	return e, mem, clock
}

func strPtr(s string) *string { return &s }

func TestStartFocusCreatesSessionAndEvent(t *testing.T) {
	e, mem, _ := newTestEngine(t, nil)
	st, err := e.StartFocus(context.Background(), Binding{Label: strPtr("читаю RFC")})
	if err != nil {
		t.Fatalf("start focus: %v", err)
	}
	if st.Phase != PhaseFocus {
		t.Fatalf("phase = %s, want focus", st.Phase)
	}
	if st.RemainingSeconds != 1500 {
		t.Fatalf("remaining = %d, want 1500", st.RemainingSeconds)
	}
	types := mem.OutboxTypes()
	if len(types) != 1 || types[0] != "pomodoro.started" {
		t.Fatalf("outbox = %v, want [pomodoro.started]", types)
	}
	if _, err := e.StartFocus(context.Background(), Binding{}); !errors.Is(err, ErrSessionActive) {
		t.Fatalf("second start err = %v, want ErrSessionActive", err)
	}
}

func TestBindingValidation(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		wantErr bool
	}{
		{"empty", Binding{}, false},
		{"label only", Binding{Label: strPtr("x")}, false},
		{"task only", Binding{Task: &models.TaskRef{Source: "tasks", ExternalID: "42"}}, false},
		{"both", Binding{Label: strPtr("x"), Task: &models.TaskRef{Source: "tasks", ExternalID: "42"}}, true},
		{"empty label", Binding{Label: strPtr("")}, true},
		{"task without id", Binding{Task: &models.TaskRef{Source: "tasks"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := newTestEngine(t, nil)
			_, err := e.StartFocus(context.Background(), tt.binding)
			if tt.wantErr && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestPauseResumeAccumulation(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) { s.AutoStartBreak = false })
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(10 * time.Minute)
	st, err := e.Pause(context.Background())
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !st.Paused || st.RemainingSeconds != 900 {
		t.Fatalf("after pause: paused=%v remaining=%d, want true 900", st.Paused, st.RemainingSeconds)
	}
	clock.Advance(5 * time.Minute)
	if got := e.Snapshot().RemainingSeconds; got != 900 {
		t.Fatalf("remaining while paused = %d, want 900", got)
	}
	if _, err := e.Pause(context.Background()); !errors.Is(err, ErrAlreadyPaused) {
		t.Fatalf("double pause err = %v, want ErrAlreadyPaused", err)
	}
	st, err = e.Resume(context.Background())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if st.Paused || st.PausedTotal != 300 {
		t.Fatalf("after resume: paused=%v total=%d, want false 300", st.Paused, st.PausedTotal)
	}
	if _, err := e.Resume(context.Background()); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("double resume err = %v, want ErrNotPaused", err)
	}
	clock.Advance(4 * time.Minute)
	clock.Advance(30 * time.Second)
	st, err = e.Pause(context.Background())
	if err != nil {
		t.Fatalf("second pause: %v", err)
	}
	clock.Advance(2 * time.Minute)
	st, err = e.Resume(context.Background())
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	if st.PausedTotal != 420 {
		t.Fatalf("paused total = %d, want 420", st.PausedTotal)
	}
	if st.RemainingSeconds != 1500-870 {
		t.Fatalf("remaining = %d, want %d", st.RemainingSeconds, 1500-870)
	}
}

func TestTickCompletesFocusExactlyAtDeadlineWithPauses(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) { s.AutoStartBreak = false })
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	started := clock.Now()
	clock.Advance(5 * time.Minute)
	if _, err := e.Pause(context.Background()); err != nil {
		t.Fatalf("pause: %v", err)
	}
	clock.Advance(7 * time.Minute)
	if _, err := e.Resume(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	clock.Advance(20*time.Minute - 1*time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if st := e.Snapshot(); st.Phase != PhaseFocus {
		t.Fatalf("phase before deadline = %s, want focus", st.Phase)
	}
	clock.Advance(2 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st := e.Snapshot()
	if st.Phase != PhaseIdle || st.CompletedToday != 1 || st.NextPhase != PhaseShortBreak {
		t.Fatalf("after complete: phase=%s completed=%d next=%s", st.Phase, st.CompletedToday, st.NextPhase)
	}
	sessions := mem.AllSessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	wantEnd := started.Add(1500*time.Second + 420*time.Second)
	if !sessions[0].EndedAt.Equal(wantEnd) {
		t.Fatalf("ended_at = %v, want %v", sessions[0].EndedAt, wantEnd)
	}
	types := mem.OutboxTypes()
	if len(types) != 2 || types[1] != "pomodoro.completed" {
		t.Fatalf("outbox = %v, want started+completed", types)
	}
}

func TestAutoStartBreakAndFocusChain(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = true
		s.AutoStartFocus = true
		s.FocusDurationSeconds = 60
		s.ShortBreakSeconds = 30
		s.DayBlocks = models.IntList{2, 2}
	})
	if _, err := e.StartFocus(context.Background(), Binding{Label: strPtr("метка")}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st := e.Snapshot()
	if st.Phase != PhaseShortBreak || st.PlannedSeconds != 30 {
		t.Fatalf("after focus: phase=%s planned=%d, want short_break 30", st.Phase, st.PlannedSeconds)
	}
	clock.Advance(31 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st = e.Snapshot()
	if st.Phase != PhaseFocus {
		t.Fatalf("after break: phase=%s, want focus (auto started)", st.Phase)
	}
	if st.Label == nil || *st.Label != "метка" {
		t.Fatalf("auto-started focus label = %v, want carried over", st.Label)
	}
	sessions := mem.AllSessions()
	if len(sessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(sessions))
	}
	types := mem.OutboxTypes()
	want := []string{"pomodoro.started", "pomodoro.completed", "pomodoro.started"}
	if len(types) != len(want) {
		t.Fatalf("outbox = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("outbox[%d] = %s, want %s", i, types[i], want[i])
		}
	}
}

func TestDayBlocksLongBreakBetweenBlocks(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = false
		s.AutoStartFocus = false
		s.FocusDurationSeconds = 60
		s.ShortBreakSeconds = 30
		s.LongBreakSeconds = 90
		s.DayBlocks = models.IntList{2, 2}
	})
	completeFocus := func() {
		t.Helper()
		if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
			t.Fatalf("start focus: %v", err)
		}
		clock.Advance(61 * time.Second)
		if err := e.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	completeBreak := func(wantPlanned int) {
		t.Helper()
		st, err := e.StartBreak(context.Background())
		if err != nil {
			t.Fatalf("start break: %v", err)
		}
		if st.PlannedSeconds != wantPlanned {
			t.Fatalf("break planned = %d, want %d", st.PlannedSeconds, wantPlanned)
		}
		clock.Advance(time.Duration(wantPlanned+1) * time.Second)
		if err := e.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}

	completeFocus()
	if st := e.Snapshot(); st.NextPhase != PhaseShortBreak {
		t.Fatalf("after 1st: next=%s, want short_break", st.NextPhase)
	}
	completeBreak(30)

	completeFocus()
	if st := e.Snapshot(); st.NextPhase != PhaseLongBreak {
		t.Fatalf("after 2nd: next=%s, want long_break (block boundary)", st.NextPhase)
	}
	completeBreak(90)

	completeFocus()
	st := e.Snapshot()
	if st.NextPhase != PhaseShortBreak || st.BlockIndex != 1 || st.PosInBlock != 1 {
		t.Fatalf("after 3rd: next=%s block=%d pos=%d, want short_break 1 1", st.NextPhase, st.BlockIndex, st.PosInBlock)
	}
	completeBreak(30)

	completeFocus()
	st = e.Snapshot()
	if st.NextPhase != PhaseLongBreak || !st.DayComplete || st.CompletedToday != 4 {
		t.Fatalf("after 4th: next=%s complete=%v count=%d", st.NextPhase, st.DayComplete, st.CompletedToday)
	}
}

func TestBlockPosBeyondPlanRepeatsLastBlock(t *testing.T) {
	tests := []struct {
		blocks    []int
		idx       int
		wantBlock int
		wantPos   int
	}{
		{[]int{4, 4}, 0, 0, 0},
		{[]int{4, 4}, 3, 0, 3},
		{[]int{4, 4}, 4, 1, 0},
		{[]int{4, 4}, 7, 1, 3},
		{[]int{4, 4}, 8, 2, 0},
		{[]int{4, 4}, 13, 3, 1},
		{[]int{2, 3, 1}, 5, 2, 0},
		{[]int{2, 3, 1}, 6, 3, 0},
		{nil, 5, 1, 1},
	}
	for _, tt := range tests {
		block, pos := blockPos(tt.blocks, tt.idx)
		if block != tt.wantBlock || pos != tt.wantPos {
			t.Errorf("blockPos(%v, %d) = (%d,%d), want (%d,%d)",
				tt.blocks, tt.idx, block, pos, tt.wantBlock, tt.wantPos)
		}
	}
}

func TestBreakPhaseAfter(t *testing.T) {
	tests := []struct {
		blocks    []int
		completed int
		want      Phase
	}{
		{[]int{4, 4}, 1, PhaseShortBreak},
		{[]int{4, 4}, 3, PhaseShortBreak},
		{[]int{4, 4}, 4, PhaseLongBreak},
		{[]int{4, 4}, 5, PhaseShortBreak},
		{[]int{4, 4}, 8, PhaseLongBreak},
		{[]int{4, 4}, 12, PhaseLongBreak},
		{[]int{1}, 1, PhaseLongBreak},
		{[]int{2, 3, 1}, 2, PhaseLongBreak},
		{[]int{2, 3, 1}, 4, PhaseShortBreak},
		{[]int{2, 3, 1}, 5, PhaseLongBreak},
		{[]int{2, 3, 1}, 6, PhaseLongBreak},
		{[]int{4, 4}, 0, PhaseShortBreak},
	}
	for _, tt := range tests {
		if got := breakPhaseAfter(tt.blocks, tt.completed); got != tt.want {
			t.Errorf("breakPhaseAfter(%v, %d) = %s, want %s", tt.blocks, tt.completed, got, tt.want)
		}
	}
}

func TestStopAbandonsFocus(t *testing.T) {
	e, mem, clock := newTestEngine(t, nil)
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := e.Pause(context.Background()); err != nil {
		t.Fatalf("pause: %v", err)
	}
	clock.Advance(1 * time.Minute)
	st, err := e.Stop(context.Background(), "")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if st.Phase != PhaseIdle || st.CompletedToday != 0 || st.NextPhase != PhaseFocus {
		t.Fatalf("after stop: phase=%s completed=%d next=%s", st.Phase, st.CompletedToday, st.NextPhase)
	}
	sessions := mem.AllSessions()
	if *sessions[0].Outcome != models.OutcomeAbandoned {
		t.Fatalf("outcome = %s, want abandoned", *sessions[0].Outcome)
	}
	if sessions[0].PausedAt != nil || sessions[0].PausedTotalSeconds != 60 {
		t.Fatalf("paused_at=%v total=%d, want nil 60", sessions[0].PausedAt, sessions[0].PausedTotalSeconds)
	}
	types := mem.OutboxTypes()
	if len(types) != 2 || types[1] != "pomodoro.abandoned" {
		t.Fatalf("outbox = %v, want started+abandoned", types)
	}
	if _, err := e.Stop(context.Background(), ""); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("stop idle err = %v, want ErrNoActiveSession", err)
	}
}

func TestStopWithCompletedOutcomeCountsPomodoro(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) { s.AutoStartBreak = false })
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(10 * time.Minute)
	st, err := e.Stop(context.Background(), models.OutcomeCompleted)
	if err != nil {
		t.Fatalf("stop completed: %v", err)
	}
	if st.CompletedToday != 1 || st.NextPhase != PhaseShortBreak {
		t.Fatalf("completed=%d next=%s, want 1 short_break", st.CompletedToday, st.NextPhase)
	}
	types := mem.OutboxTypes()
	if len(types) != 2 || types[1] != "pomodoro.completed" {
		t.Fatalf("outbox = %v", types)
	}
}

func TestEarlyFinishCreditsNearestFraction(t *testing.T) {
	cases := []struct {
		name        string
		focus       time.Duration
		pause       time.Duration
		wantCredit  int
		wantOutcome string
		wantDone    int
	}{
		{"почти до конца", 24 * time.Minute, 0, 12, models.OutcomeCompleted, 1},
		{"три четверти", 20 * time.Minute, 0, 9, models.OutcomeCompleted, 1},
		{"две трети", 17 * time.Minute, 0, 8, models.OutcomeCompleted, 1},
		{"половина", 13 * time.Minute, 0, 6, models.OutcomeCompleted, 1},
		{"треть", 9 * time.Minute, 0, 4, models.OutcomeCompleted, 1},
		{"четверть", 7 * time.Minute, 0, 3, models.OutcomeCompleted, 1},
		{"пауза не засчитывается", 15 * time.Minute, 5 * time.Minute, 8, models.OutcomeCompleted, 1},
		{"ноль", 2 * time.Minute, 0, 0, models.OutcomeAbandoned, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, mem, clock := newTestEngine(t, func(s *models.Settings) { s.AutoStartBreak = false })
			if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
				t.Fatalf("start: %v", err)
			}
			if tc.pause > 0 {
				clock.Advance(tc.focus / 2)
				if _, err := e.Pause(context.Background()); err != nil {
					t.Fatalf("pause: %v", err)
				}
				clock.Advance(tc.pause)
				if _, err := e.Resume(context.Background()); err != nil {
					t.Fatalf("resume: %v", err)
				}
				clock.Advance(tc.focus - tc.focus/2)
			} else {
				clock.Advance(tc.focus)
			}
			st, err := e.Stop(context.Background(), models.OutcomeCompleted)
			if err != nil {
				t.Fatalf("stop: %v", err)
			}
			s := mem.AllSessions()[0]
			if *s.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %s, ожидался %s", *s.Outcome, tc.wantOutcome)
			}
			if s.CreditTwelfths == nil || *s.CreditTwelfths != tc.wantCredit {
				t.Fatalf("зачёт = %v двенадцатых, ожидалось %d", s.CreditTwelfths, tc.wantCredit)
			}
			if s.FocusSeconds == nil || *s.FocusSeconds != int(tc.focus/time.Second) {
				t.Fatalf("фокус = %v с, ожидалось %d", s.FocusSeconds, int(tc.focus/time.Second))
			}
			if st.CompletedToday != tc.wantDone {
				t.Fatalf("completed_today = %d, ожидалось %d", st.CompletedToday, tc.wantDone)
			}
			wantDay := float64(tc.wantCredit*100/12) / 100
			if st.CreditToday != wantDay {
				t.Fatalf("credit_today = %v, ожидалось %v", st.CreditToday, wantDay)
			}
		})
	}
}

func TestNaturalCompletionGivesFullCreditAndDaySum(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) { s.AutoStartBreak = false })
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(25*time.Minute + time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start second: %v", err)
	}
	clock.Advance(19 * time.Minute)
	st, err := e.Stop(context.Background(), models.OutcomeCompleted)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if st.CompletedToday != 2 || st.CreditToday != 1.75 {
		t.Fatalf("completed=%d credit=%v, ожидалось 2 и 1.75", st.CompletedToday, st.CreditToday)
	}
	for _, s := range mem.AllSessions() {
		if s.StartedAt.Equal(newFakeClock().now) && (s.CreditTwelfths == nil || *s.CreditTwelfths != 12 || *s.FocusSeconds != 1500) {
			t.Fatalf("полный помидор: зачёт %v, фокус %v", s.CreditTwelfths, s.FocusSeconds)
		}
	}

	e2 := New(mem, clock)
	if err := e2.Init(context.Background()); err != nil {
		t.Fatalf("reinit: %v", err)
	}
	if got := e2.Snapshot().CreditToday; got != 1.75 {
		t.Fatalf("после перезапуска сумма дня %v, ожидалось 1.75", got)
	}
}

func TestStopRejectsUnknownOutcome(t *testing.T) {
	e, _, _ := newTestEngine(t, nil)
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := e.Stop(context.Background(), "skipped"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestBreakSessionsEmitNoEvents(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = false
		s.FocusDurationSeconds = 60
	})
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if _, err := e.StartBreak(context.Background()); err != nil {
		t.Fatalf("start break: %v", err)
	}
	clock.Advance(10 * time.Second)
	if _, err := e.Stop(context.Background(), ""); err != nil {
		t.Fatalf("stop break: %v", err)
	}
	types := mem.OutboxTypes()
	if len(types) != 2 {
		t.Fatalf("outbox = %v, break must not emit events", types)
	}
}

func TestInitMarksHangingSessionsInterrupted(t *testing.T) {
	mem := memstore.New()
	clock := newFakeClock()
	pausedAt := clock.Now().Add(-30 * time.Minute)
	hanging := models.Session{
		ID:                     uuid.New(),
		Kind:                   models.KindFocus,
		StartedAt:              clock.Now().Add(-2 * time.Hour),
		PlannedDurationSeconds: 1500,
		PausedAt:               &pausedAt,
		PausedTotalSeconds:     100,
	}
	if err := mem.Sessions().Create(context.Background(), &hanging); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e := New(mem, clock)
	if err := e.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	got, _ := mem.SessionByID(hanging.ID)
	if got.Outcome == nil || *got.Outcome != models.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want interrupted", got.Outcome)
	}
	if got.EndedAt == nil || got.PausedAt != nil {
		t.Fatalf("ended_at=%v paused_at=%v, want set and nil", got.EndedAt, got.PausedAt)
	}
	if got.PausedTotalSeconds != 100+1800 {
		t.Fatalf("paused_total = %d, want 1900", got.PausedTotalSeconds)
	}
	types := mem.OutboxTypes()
	if len(types) != 1 || types[0] != "pomodoro.interrupted" {
		t.Fatalf("outbox = %v, want [pomodoro.interrupted]", types)
	}
	if st := e.Snapshot(); st.Phase != PhaseIdle {
		t.Fatalf("phase = %s, want idle", st.Phase)
	}
}

func TestInitRestoresDayProgressAndNextBreak(t *testing.T) {
	mem := memstore.New()
	clock := newFakeClock()
	outcome := models.OutcomeCompleted
	for i := 0; i < 2; i++ {
		started := clock.Now().Add(time.Duration(-3+i) * time.Hour)
		ended := started.Add(25 * time.Minute)
		s := models.Session{
			ID:                     uuid.New(),
			Kind:                   models.KindFocus,
			StartedAt:              started,
			EndedAt:                &ended,
			PlannedDurationSeconds: 1500,
			Outcome:                &outcome,
		}
		if err := mem.Sessions().Create(context.Background(), &s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	settings, _ := mem.Settings().Get(context.Background())
	settings.DayBlocks = models.IntList{2, 2}
	if err := mem.Settings().Save(context.Background(), &settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	e := New(mem, clock)
	if err := e.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	st := e.Snapshot()
	if st.CompletedToday != 2 {
		t.Fatalf("completed today = %d, want 2", st.CompletedToday)
	}
	if st.NextPhase != PhaseLongBreak {
		t.Fatalf("next = %s, want long_break", st.NextPhase)
	}
}

func TestDayRolloverResetsCounter(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = false
		s.FocusDurationSeconds = 60
	})
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if st := e.Snapshot(); st.CompletedToday != 1 {
		t.Fatalf("completed = %d, want 1", st.CompletedToday)
	}
	clock.Advance(24 * time.Hour)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st := e.Snapshot()
	if st.CompletedToday != 0 || st.NextPhase != PhaseFocus {
		t.Fatalf("after rollover: completed=%d next=%s, want 0 focus", st.CompletedToday, st.NextPhase)
	}
}

func TestRelabelEndedSessionEmitsEvent(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = false
		s.FocusDurationSeconds = 60
	})
	st, err := e.StartFocus(context.Background(), Binding{Label: strPtr("старая метка")})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := *st.SessionID
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	task := &models.TaskRef{Source: "tasks", ExternalID: "abc", TitleSnapshot: "Выучить Go"}
	got, err := e.Relabel(context.Background(), id, Binding{Task: task})
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if got.Label != nil || got.TaskSource == nil || *got.TaskSource != "tasks" {
		t.Fatalf("binding after relabel: label=%v source=%v", got.Label, got.TaskSource)
	}
	if got.RelabeledAt == nil {
		t.Fatal("relabeled_at not set")
	}
	types := mem.OutboxTypes()
	if len(types) != 3 || types[2] != "pomodoro.relabeled" {
		t.Fatalf("outbox = %v, want relabeled last", types)
	}
	events := mem.OutboxEvents()
	payload := events[2].Payload
	oldPart, ok := payload["old"].(models.JSONMap)
	if !ok || oldPart["label"] != "старая метка" {
		t.Fatalf("old payload = %v", payload["old"])
	}
}

func TestRelabelActiveSessionNoEvent(t *testing.T) {
	e, mem, _ := newTestEngine(t, nil)
	st, err := e.StartFocus(context.Background(), Binding{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	got, err := e.Relabel(context.Background(), *st.SessionID, Binding{Label: strPtr("новая")})
	if err != nil {
		t.Fatalf("relabel: %v", err)
	}
	if got.RelabeledAt != nil {
		t.Fatal("active session must not get relabeled_at")
	}
	if snap := e.Snapshot(); snap.Label == nil || *snap.Label != "новая" {
		t.Fatalf("engine state label = %v, want новая", snap.Label)
	}
	if types := mem.OutboxTypes(); len(types) != 1 {
		t.Fatalf("outbox = %v, want only started", types)
	}
}

func TestRelabelRejectsBreakAndMissing(t *testing.T) {
	e, mem, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = true
		s.FocusDurationSeconds = 60
	})
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	var breakID uuid.UUID
	for _, s := range mem.AllSessions() {
		if s.Kind == models.KindBreak {
			breakID = s.ID
		}
	}
	if _, err := e.Relabel(context.Background(), breakID, Binding{Label: strPtr("x")}); !errors.Is(err, ErrNotFocus) {
		t.Fatalf("relabel break err = %v, want ErrNotFocus", err)
	}
	if _, err := e.Relabel(context.Background(), uuid.New(), Binding{Label: strPtr("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("relabel missing err = %v, want ErrNotFound", err)
	}
}

func TestValidateSettings(t *testing.T) {
	valid := models.DefaultSettings()
	tests := []struct {
		name    string
		mutate  func(*models.Settings)
		wantErr bool
	}{
		{"default ok", func(s *models.Settings) {}, false},
		{"zero focus", func(s *models.Settings) { s.FocusDurationSeconds = 0 }, true},
		{"negative break", func(s *models.Settings) { s.ShortBreakSeconds = -1 }, true},
		{"zero long break", func(s *models.Settings) { s.LongBreakSeconds = 0 }, true},
		{"empty blocks", func(s *models.Settings) { s.DayBlocks = models.IntList{} }, true},
		{"zero block", func(s *models.Settings) { s.DayBlocks = models.IntList{4, 0} }, true},
		{"too big block", func(s *models.Settings) { s.DayBlocks = models.IntList{17} }, true},
		{"too many blocks", func(s *models.Settings) {
			blocks := make(models.IntList, 25)
			for i := range blocks {
				blocks[i] = 1
			}
			s.DayBlocks = blocks
		}, true},
		{"nine blocks ok", func(s *models.Settings) {
			s.DayBlocks = models.IntList{3, 3, 3, 3, 3, 3, 3, 3, 1}
		}, false},
		{"single block ok", func(s *models.Settings) { s.DayBlocks = models.IntList{6} }, false},
		{"overlay too small", func(s *models.Settings) { s.Overlay.Size = 40 }, true},
		{"overlay circle opacity high", func(s *models.Settings) { s.Overlay.CircleOpacity = 1.5 }, true},
		{"overlay digits opacity low", func(s *models.Settings) { s.Overlay.DigitsOpacity = 0.01 }, true},
		{"overlay digits size out of range", func(s *models.Settings) { s.Overlay.DigitsSize = 4 }, true},
		{"overlay zero opacities normalized", func(s *models.Settings) {
			s.Overlay = models.OverlayConfig{Size: 200}
			s.Overlay.Normalize()
		}, false},
		{"overlay ok", func(s *models.Settings) { s.Overlay = models.DefaultOverlay() }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := valid
			s.DayBlocks = append(models.IntList{}, valid.DayBlocks...)
			tt.mutate(&s)
			err := ValidateSettings(s)
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func TestUpdateSettingsRecalculatesNextBreak(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.AutoStartBreak = false
		s.FocusDurationSeconds = 60
		s.DayBlocks = models.IntList{4, 4}
	})
	if _, err := e.StartFocus(context.Background(), Binding{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	clock.Advance(61 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if st := e.Snapshot(); st.NextPhase != PhaseShortBreak {
		t.Fatalf("next = %s, want short_break", st.NextPhase)
	}
	s := e.Settings()
	s.DayBlocks = models.IntList{1, 7}
	if _, err := e.UpdateSettings(context.Background(), s); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if st := e.Snapshot(); st.NextPhase != PhaseLongBreak {
		t.Fatalf("next after reconfigure = %s, want long_break", st.NextPhase)
	}
}

func intPtr(v int) *int { return &v }

func completeFocusNow(t *testing.T, e *Engine, clock *fakeClock) {
	t.Helper()
	st, err := e.StartFocus(context.Background(), Binding{})
	if err != nil {
		t.Fatalf("start focus: %v", err)
	}
	clock.Advance(time.Duration(st.PlannedSeconds+1) * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func completeBreakNow(t *testing.T, e *Engine, clock *fakeClock) State {
	t.Helper()
	st, err := e.StartBreak(context.Background())
	if err != nil {
		t.Fatalf("start break: %v", err)
	}
	clock.Advance(time.Duration(st.PlannedSeconds+1) * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	return st
}

func TestIdleSnapshotNextPlannedSecondsFollowsPlan(t *testing.T) {
	e, _, clock := newTestEngine(t, nil)
	e.SetDurationProvider(func(idx int) (*int, *int) {
		switch idx {
		case 0:
			return intPtr(600), intPtr(120)
		case 1:
			return intPtr(900), nil
		}
		return nil, nil
	})
	if st := e.Snapshot(); st.NextPhase != PhaseFocus || st.NextPlannedSeconds != 600 {
		t.Fatalf("idle: next=%s planned=%d, want focus 600", st.NextPhase, st.NextPlannedSeconds)
	}
	st, err := e.StartFocus(context.Background(), Binding{})
	if err != nil {
		t.Fatalf("start focus: %v", err)
	}
	if st.PlannedSeconds != 600 || st.NextPlannedSeconds != 600 {
		t.Fatalf("active: planned=%d next_planned=%d, want 600 600", st.PlannedSeconds, st.NextPlannedSeconds)
	}
	clock.Advance(601 * time.Second)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st = e.Snapshot()
	if st.NextPhase != PhaseShortBreak || st.NextPlannedSeconds != 120 {
		t.Fatalf("after focus: next=%s planned=%d, want short_break 120 (break of slot 0)", st.NextPhase, st.NextPlannedSeconds)
	}
	if got := completeBreakNow(t, e, clock); got.PlannedSeconds != 120 {
		t.Fatalf("break planned = %d, want 120", got.PlannedSeconds)
	}
	st = e.Snapshot()
	if st.NextPhase != PhaseFocus || st.NextPlannedSeconds != 900 {
		t.Fatalf("after break: next=%s planned=%d, want focus 900 (slot 1)", st.NextPhase, st.NextPlannedSeconds)
	}
}

func TestSnapshotFallsBackToSettingsWithoutProviders(t *testing.T) {
	provs := map[string]func(*Engine){
		"nil providers": func(*Engine) {},
		"empty providers": func(e *Engine) {
			e.SetDurationProvider(func(int) (*int, *int) { return nil, nil })
			e.SetBlocksProvider(func() []int { return nil })
		},
	}
	for name, setup := range provs {
		t.Run(name, func(t *testing.T) {
			e, _, clock := newTestEngine(t, func(s *models.Settings) {
				s.FocusDurationSeconds = 60
				s.ShortBreakSeconds = 30
				s.LongBreakSeconds = 90
				s.DayBlocks = models.IntList{2, 3}
			})
			setup(e)
			st := e.Snapshot()
			if st.NextPlannedSeconds != 60 || st.DayTotal != 5 || st.BlockSize != 2 ||
				len(st.DayBlocks) != 2 || st.DayBlocks[1] != 3 {
				t.Fatalf("idle: planned=%d total=%d block_size=%d blocks=%v, want 60 5 2 [2 3]",
					st.NextPlannedSeconds, st.DayTotal, st.BlockSize, st.DayBlocks)
			}
			completeFocusNow(t, e, clock)
			if st := e.Snapshot(); st.NextPhase != PhaseShortBreak || st.NextPlannedSeconds != 30 {
				t.Fatalf("after 1st: next=%s planned=%d, want short_break 30", st.NextPhase, st.NextPlannedSeconds)
			}
			completeBreakNow(t, e, clock)
			completeFocusNow(t, e, clock)
			if st := e.Snapshot(); st.NextPhase != PhaseLongBreak || st.NextPlannedSeconds != 90 {
				t.Fatalf("after 2nd: next=%s planned=%d, want long_break 90", st.NextPhase, st.NextPlannedSeconds)
			}
		})
	}
}

func TestBlocksProviderDrivesDayStructure(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.FocusDurationSeconds = 60
		s.ShortBreakSeconds = 30
		s.LongBreakSeconds = 90
		s.DayBlocks = models.IntList{4, 4}
	})
	e.SetBlocksProvider(func() []int { return []int{2, 1, 3} })

	st := e.Snapshot()
	if st.DayTotal != 6 || st.BlockSize != 2 || st.DayComplete {
		t.Fatalf("idle: total=%d block_size=%d complete=%v, want 6 2 false", st.DayTotal, st.BlockSize, st.DayComplete)
	}
	if len(st.DayBlocks) != 3 || st.DayBlocks[0] != 2 || st.DayBlocks[1] != 1 || st.DayBlocks[2] != 3 {
		t.Fatalf("day_blocks = %v, want [2 1 3]", st.DayBlocks)
	}

	completeFocusNow(t, e, clock)
	if st := e.Snapshot(); st.NextPhase != PhaseShortBreak {
		t.Fatalf("after 1st: next=%s, want short_break", st.NextPhase)
	}
	completeBreakNow(t, e, clock)

	completeFocusNow(t, e, clock)
	st = e.Snapshot()
	if st.NextPhase != PhaseLongBreak || st.BlockIndex != 1 || st.PosInBlock != 0 || st.BlockSize != 1 {
		t.Fatalf("after 2nd: next=%s block=%d pos=%d size=%d, want long_break 1 0 1",
			st.NextPhase, st.BlockIndex, st.PosInBlock, st.BlockSize)
	}
	completeBreakNow(t, e, clock)

	completeFocusNow(t, e, clock)
	if st := e.Snapshot(); st.NextPhase != PhaseLongBreak {
		t.Fatalf("after 3rd: next=%s, want long_break (block of one)", st.NextPhase)
	}
	completeBreakNow(t, e, clock)

	for i := 0; i < 3; i++ {
		completeFocusNow(t, e, clock)
		completeBreakNow(t, e, clock)
	}
	if st := e.Snapshot(); !st.DayComplete || st.CompletedToday != 6 {
		t.Fatalf("complete=%v count=%d, want true 6", st.DayComplete, st.CompletedToday)
	}
}

func TestPlanRebuildChangesPendingBreakKind(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.FocusDurationSeconds = 60
		s.DayBlocks = models.IntList{4, 4}
	})
	blocks := []int{4}
	e.SetBlocksProvider(func() []int { return blocks })
	completeFocusNow(t, e, clock)
	if st := e.Snapshot(); st.NextPhase != PhaseShortBreak {
		t.Fatalf("next = %s, want short_break", st.NextPhase)
	}
	blocks = []int{1, 3}
	if st := e.Snapshot(); st.NextPhase != PhaseLongBreak {
		t.Fatalf("next after plan rebuild = %s, want long_break", st.NextPhase)
	}
}

func TestManualBreakWithoutFinishedFocusIsLabelledBySlot(t *testing.T) {
	e, _, clock := newTestEngine(t, func(s *models.Settings) {
		s.FocusDurationSeconds = 60
		s.ShortBreakSeconds = 30
		s.LongBreakSeconds = 90
		s.DayBlocks = models.IntList{4, 4}
	})
	// нет завершённых помидоров: перерыв короткий
	st, err := e.StartBreak(context.Background())
	if err != nil {
		t.Fatalf("start break: %v", err)
	}
	if st.Phase != PhaseShortBreak || st.PlannedSeconds != 30 {
		t.Fatalf("no focus yet: phase=%s planned=%d, want short_break 30", st.Phase, st.PlannedSeconds)
	}
	if _, err := e.Stop(context.Background(), ""); err != nil {
		t.Fatalf("stop: %v", err)
	}

	e.SetDurationProvider(func(idx int) (*int, *int) {
		if idx == 0 {
			return nil, intPtr(90)
		}
		return nil, nil
	})
	completeFocusNow(t, e, clock)
	completeBreakNow(t, e, clock)
	if st := e.Snapshot(); st.NextPhase != PhaseFocus {
		t.Fatalf("after break: next=%s, want focus", st.NextPhase)
	}
	// ещё один перерыв по желанию: слот 0 длинный, вид и длительность — long
	st, err = e.StartBreak(context.Background())
	if err != nil {
		t.Fatalf("start extra break: %v", err)
	}
	if st.Phase != PhaseLongBreak || st.PlannedSeconds != 90 {
		t.Fatalf("extra break: phase=%s planned=%d, want long_break 90", st.Phase, st.PlannedSeconds)
	}
}
