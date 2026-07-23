package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func DBOf(s Store) *gorm.DB {
	if g, ok := s.(*gormStore); ok {
		return g.db
	}
	return nil
}

func (s *gormStore) Sessions() SessionRepo  { return &sessionRepo{db: s.db} }
func (s *gormStore) Settings() SettingsRepo { return &settingsRepo{db: s.db} }
func (s *gormStore) Outbox() OutboxRepo     { return &outboxRepo{db: s.db} }
func (s *gormStore) Plans() PlanRepo        { return &planRepo{db: s.db} }
func (s *gormStore) Presets() PresetRepo    { return &presetRepo{db: s.db} }

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

type planRepo struct {
	db *gorm.DB
}

func (r *planRepo) ListDay(ctx context.Context, date string) ([]models.PlanSlot, error) {
	var out []models.PlanSlot
	if err := r.db.WithContext(ctx).Where("date = ?", date).Order("idx ASC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list plan slots: %w", err)
	}
	return out, nil
}

func (r *planRepo) ReplaceDay(ctx context.Context, date string, slots []models.PlanSlot) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("date = ?", date).Delete(&models.PlanSlot{}).Error; err != nil {
			return fmt.Errorf("clear plan slots: %w", err)
		}
		if len(slots) == 0 {
			return nil
		}
		if err := tx.Create(&slots).Error; err != nil {
			return fmt.Errorf("insert plan slots: %w", err)
		}
		return nil
	})
}

func (r *planRepo) Upsert(ctx context.Context, slot *models.PlanSlot) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "date"}, {Name: "idx"}},
		UpdateAll: true,
	}).Create(slot).Error
	if err != nil {
		return fmt.Errorf("upsert plan slot: %w", err)
	}
	return nil
}

type presetRepo struct {
	db *gorm.DB
}

func (r *presetRepo) List(ctx context.Context) ([]models.Preset, error) {
	var out []models.Preset
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	return out, nil
}

func (r *presetRepo) GetByName(ctx context.Context, name string) (*models.Preset, error) {
	var p models.Preset
	err := r.db.WithContext(ctx).First(&p, "name = ?", name).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get preset: %w", err)
	}
	return &p, nil
}

func (r *presetRepo) Save(ctx context.Context, p *models.Preset) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"slots", "updated_at"}),
	}).Create(p).Error
	if err != nil {
		return fmt.Errorf("save preset: %w", err)
	}
	return nil
}

func (r *presetRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Delete(&models.Preset{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete preset: %w", err)
	}
	if err := r.db.WithContext(ctx).Create(&models.SyncTombstone{Table: "presets", RowID: id.String(), DeletedAt: time.Now().UTC()}).Error; err != nil {
		return fmt.Errorf("record preset tombstone: %w", err)
	}
	return nil
}

func (r *presetRepo) Schedule(ctx context.Context) ([]models.PresetAssignment, error) {
	var out []models.PresetAssignment
	if err := r.db.WithContext(ctx).Order("weekday ASC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list preset schedule: %w", err)
	}
	return out, nil
}

func (r *presetRepo) Assign(ctx context.Context, weekday int, presetID *uuid.UUID) error {
	if presetID == nil {
		if err := r.db.WithContext(ctx).Delete(&models.PresetAssignment{}, "weekday = ?", weekday).Error; err != nil {
			return fmt.Errorf("clear preset assignment: %w", err)
		}
		if err := r.db.WithContext(ctx).Create(&models.SyncTombstone{Table: "preset_schedule", RowID: fmt.Sprint(weekday), DeletedAt: time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("record schedule tombstone: %w", err)
		}
		return nil
	}
	a := models.PresetAssignment{Weekday: weekday, PresetID: *presetID}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "weekday"}},
		UpdateAll: true,
	}).Create(&a).Error
	if err != nil {
		return fmt.Errorf("assign preset: %w", err)
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
