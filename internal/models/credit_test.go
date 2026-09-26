package models

import (
	"testing"
	"time"
)

func TestCreditTwelfthsFor(t *testing.T) {
	cases := []struct {
		focus, planned int
		want           int
		label          string
	}{
		{0, 1500, 0, "0"},
		{187, 1500, 0, "0"},
		{188, 1500, 3, "1/4"},
		{437, 1500, 3, "1/4"},
		{438, 1500, 4, "1/3"},
		{625, 1500, 6, "1/2"},
		{750, 1500, 6, "1/2"},
		{875, 1500, 8, "2/3"},
		{1000, 1500, 8, "2/3"},
		{1063, 1500, 9, "3/4"},
		{1125, 1500, 9, "3/4"},
		{1312, 1500, 9, "3/4"},
		{1313, 1500, 12, "1"},
		{1500, 1500, 12, "1"},
		{1800, 1500, 12, "1"},
		{600, 0, 0, "0"},
	}
	for _, tc := range cases {
		got := CreditTwelfthsFor(tc.focus, tc.planned)
		if got != tc.want {
			t.Errorf("CreditTwelfthsFor(%d, %d) = %d, ожидалось %d", tc.focus, tc.planned, got, tc.want)
		}
		if CreditLabel(got) != tc.label {
			t.Errorf("CreditLabel(%d) = %q, ожидалось %q", got, CreditLabel(got), tc.label)
		}
	}
}

func TestLegacySessionCreditFallback(t *testing.T) {
	started := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	ended := started.Add(26 * time.Minute)
	completed := OutcomeCompleted
	abandoned := OutcomeAbandoned
	full := Session{Kind: KindFocus, StartedAt: started, EndedAt: &ended, PausedTotalSeconds: 60, Outcome: &completed}
	if full.Credit() != FullCreditTwelfths || full.ElapsedFocusSeconds() != 1500 {
		t.Fatalf("старая завершённая сессия: зачёт %d, фокус %d", full.Credit(), full.ElapsedFocusSeconds())
	}
	dropped := Session{Kind: KindFocus, StartedAt: started, EndedAt: &ended, Outcome: &abandoned}
	if dropped.Credit() != 0 {
		t.Fatalf("брошенная сессия без поля зачёта должна давать 0, получено %d", dropped.Credit())
	}
}
