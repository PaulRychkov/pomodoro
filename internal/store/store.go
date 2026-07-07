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

type Repos interface {
	Sessions() SessionRepo
	Settings() SettingsRepo
	Outbox() OutboxRepo
}

type Store interface {
	Repos
	InTx(ctx context.Context, fn func(r Repos) error) error
}
