package engine

import (
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

func startedEvent(s *models.Session) *models.OutboxEvent {
	payload := basePayload(s)
	return newEvent("pomodoro.started", s.ID, payload)
}

func finishedEvent(eventType string, s *models.Session) *models.OutboxEvent {
	payload := basePayload(s)
	if s.EndedAt != nil {
		payload["ended_at"] = s.EndedAt.UTC().Format(time.RFC3339)
		payload["actual_seconds"] = int(s.EndedAt.Sub(s.StartedAt) / time.Second)
	}
	payload["paused_total_seconds"] = s.PausedTotalSeconds
	if s.Kind == models.KindFocus && s.EndedAt != nil {
		credit := s.Credit()
		payload["focus_seconds"] = s.ElapsedFocusSeconds()
		payload["credit"] = models.CreditLabel(credit)
		payload["credit_value"] = float64(credit) / float64(models.FullCreditTwelfths)
	}
	return newEvent(eventType, s.ID, payload)
}

func relabeledEvent(s *models.Session, old models.JSONMap) *models.OutboxEvent {
	payload := models.JSONMap{
		"session_id": s.ID.String(),
		"old":        old,
		"new":        bindingPayload(s),
	}
	if s.RelabeledAt != nil {
		payload["relabeled_at"] = s.RelabeledAt.UTC().Format(time.RFC3339)
	}
	return newEvent("pomodoro.relabeled", s.ID, payload)
}

func basePayload(s *models.Session) models.JSONMap {
	payload := models.JSONMap{
		"session_id":               s.ID.String(),
		"kind":                     s.Kind,
		"started_at":               s.StartedAt.UTC().Format(time.RFC3339),
		"planned_duration_seconds": s.PlannedDurationSeconds,
	}
	for k, v := range bindingPayload(s) {
		payload[k] = v
	}
	return payload
}

func bindingPayload(s *models.Session) models.JSONMap {
	payload := models.JSONMap{"label": nil, "task": nil}
	if s.Label != nil {
		payload["label"] = *s.Label
	}
	if ref := s.TaskRef(); ref != nil {
		payload["task"] = models.JSONMap{
			"source":         ref.Source,
			"external_id":    ref.ExternalID,
			"title_snapshot": ref.TitleSnapshot,
		}
	}
	return payload
}

func newEvent(eventType string, sessionID uuid.UUID, payload models.JSONMap) *models.OutboxEvent {
	return &models.OutboxEvent{
		ID:            uuid.New(),
		EventType:     eventType,
		AggregateType: "session",
		AggregateID:   sessionID,
		Payload:       payload,
	}
}
