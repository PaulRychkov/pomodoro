package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

type gormStore struct {
	db *gorm.DB
}

func Open(dsn string) (Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return &gormStore{db: db}, nil
}

func NewWithDB(db *gorm.DB) Store {
	return &gormStore{db: db}
}

func (s *gormStore) Sessions() SessionRepo  { return &sessionRepo{db: s.db} }
func (s *gormStore) Settings() SettingsRepo { return &settingsRepo{db: s.db} }
func (s *gormStore) Outbox() OutboxRepo     { return &outboxRepo{db: s.db} }

func (s *gormStore) InTx(ctx context.Context, fn func(r Repos) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&gormStore{db: tx})
	})
}

type sessionRepo struct {
	db *gorm.DB
}

func (r *sessionRepo) Create(ctx context.Context, s *models.Session) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *sessionRepo) Save(ctx context.Context, s *models.Session) error {
	if err := r.db.WithContext(ctx).Save(s).Error; err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (r *sessionRepo) Get(ctx context.Context, id uuid.UUID) (*models.Session, error) {
	var s models.Session
	err := r.db.WithContext(ctx).First(&s, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &s, nil
}

func (r *sessionRepo) FindActive(ctx context.Context) (*models.Session, error) {
	var s models.Session
	err := r.db.WithContext(ctx).Where("ended_at IS NULL").First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active session: %w", err)
	}
	return &s, nil
}

func (r *sessionRepo) ListHanging(ctx context.Context) ([]models.Session, error) {
	var out []models.Session
	if err := r.db.WithContext(ctx).Where("ended_at IS NULL").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list hanging sessions: %w", err)
	}
	return out, nil
}

func (r *sessionRepo) ListRange(ctx context.Context, from, to time.Time) ([]models.Session, error) {
	var out []models.Session
	err := r.db.WithContext(ctx).
		Where("started_at >= ? AND started_at < ?", from, to).
		Order("started_at DESC").
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return out, nil
}

func (r *sessionRepo) LastStarted(ctx context.Context) (*models.Session, error) {
	var s models.Session
	err := r.db.WithContext(ctx).Order("started_at DESC").First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last session: %w", err)
	}
	return &s, nil
}

func (r *sessionRepo) CountCompletedFocusBetween(ctx context.Context, from, to time.Time) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.Session{}).
		Where("kind = ? AND outcome = ? AND started_at >= ? AND started_at < ?",
			models.KindFocus, models.OutcomeCompleted, from, to).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("count completed focus: %w", err)
	}
	return int(n), nil
}

type settingsRepo struct {
	db *gorm.DB
}

func (r *settingsRepo) Get(ctx context.Context) (models.Settings, error) {
	var s models.Settings
	if err := r.db.WithContext(ctx).First(&s, "id = 1").Error; err != nil {
		return models.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	return s, nil
}

func (r *settingsRepo) Save(ctx context.Context, s *models.Settings) error {
	s.ID = 1
	if err := r.db.WithContext(ctx).Save(s).Error; err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

type outboxRepo struct {
	db *gorm.DB
}

func (r *outboxRepo) Insert(ctx context.Context, e *models.OutboxEvent) error {
	if err := r.db.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (r *outboxRepo) Unpublished(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	var out []models.OutboxEvent
	err := r.db.WithContext(ctx).
		Where("published_at IS NULL").
		Order("created_at ASC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list unpublished events: %w", err)
	}
	return out, nil
}

func (r *outboxRepo) MarkPublished(ctx context.Context, id uuid.UUID, at time.Time) error {
	err := r.db.WithContext(ctx).Model(&models.OutboxEvent{}).
		Where("id = ?", id).
		Update("published_at", at).Error
	if err != nil {
		return fmt.Errorf("mark published: %w", err)
	}
	return nil
}

func (r *outboxRepo) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	err := r.db.WithContext(ctx).Model(&models.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": message,
		}).Error
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}
