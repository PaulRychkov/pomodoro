package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

type SessionRepo interface {
	Create(ctx context.Context, s *models.Session) error
	Save(ctx context.Context, s *models.Session) error
	Get(ctx context.Context, id uuid.UUID) (*models.Session, error)
	FindActive(ctx context.Context) (*models.Session, error)
	ListHanging(ctx context.Context) ([]models.Session, error)
	ListRange(ctx context.Context, from, to time.Time) ([]models.Session, error)
	LastStarted(ctx context.Context) (*models.Session, error)
	CountCompletedFocusBetween(ctx context.Context, from, to time.Time) (int, error)
}

type SettingsRepo interface {
	Get(ctx context.Context) (models.Settings, error)
	Save(ctx context.Context, s *models.Settings) error
}

type OutboxRepo interface {
	Insert(ctx context.Context, e *models.OutboxEvent) error
	Unpublished(ctx context.Context, limit int) ([]models.OutboxEvent, error)
	MarkPublished(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, message string) error
}

type PlanRepo interface {
	ListDay(ctx context.Context, date string) ([]models.PlanSlot, error)
	ReplaceDay(ctx context.Context, date string, slots []models.PlanSlot) error
	Upsert(ctx context.Context, slot *models.PlanSlot) error
}

type PresetRepo interface {
	List(ctx context.Context) ([]models.Preset, error)
	GetByName(ctx context.Context, name string) (*models.Preset, error)
	Save(ctx context.Context, p *models.Preset) error
	Delete(ctx context.Context, id uuid.UUID) error
	Schedule(ctx context.Context) ([]models.PresetAssignment, error)
	Assign(ctx context.Context, weekday int, presetID *uuid.UUID) error
}

type Repos interface {
	Sessions() SessionRepo
	Settings() SettingsRepo
	Outbox() OutboxRepo
	Plans() PlanRepo
	Presets() PresetRepo
}

type Store interface {
	Repos
	InTx(ctx context.Context, fn func(r Repos) error) error
}
