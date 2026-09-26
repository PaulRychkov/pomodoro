package syncer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/PaulRychkov/pomodoro/internal/models"
)

type Changes struct {
	ServerTime time.Time                 `json:"server_time"`
	Sessions   []models.Session          `json:"sessions"`
	Settings   []SettingsPayload         `json:"settings"`
	Presets    []models.Preset           `json:"presets"`
	Schedule   []models.PresetAssignment `json:"schedule"`
	Tombstones []models.SyncTombstone    `json:"tombstones"`
}

type SettingsPayload struct {
	models.Settings
	UpdatedAt time.Time `json:"updated_at"`
}

func (p SettingsPayload) settings() models.Settings {
	s := p.Settings
	s.ID = 1
	s.UpdatedAt = p.UpdatedAt
	return s
}

type ApplyResult struct {
	Upserted int `json:"upserted"`
	Deleted  int `json:"deleted"`
	Skipped  int `json:"skipped"`
}

type Service struct {
	DB  *gorm.DB
	Log *zap.Logger
}

func (s *Service) Collect(ctx context.Context, since time.Time) (Changes, error) {
	out := Changes{ServerTime: time.Now().UTC()}
	db := s.DB.WithContext(ctx)
	if err := db.Where("updated_at > ?", since).Order("updated_at").Find(&out.Sessions).Error; err != nil {
		return out, fmt.Errorf("collect sessions: %w", err)
	}
	var settings []models.Settings
	if err := db.Where("updated_at > ?", since).Find(&settings).Error; err != nil {
		return out, fmt.Errorf("collect settings: %w", err)
	}
	for _, s := range settings {
		out.Settings = append(out.Settings, SettingsPayload{Settings: s, UpdatedAt: s.UpdatedAt})
	}
	if err := db.Where("updated_at > ?", since).Order("updated_at").Find(&out.Presets).Error; err != nil {
		return out, fmt.Errorf("collect presets: %w", err)
	}
	if err := db.Where("updated_at > ?", since).Find(&out.Schedule).Error; err != nil {
		return out, fmt.Errorf("collect schedule: %w", err)
	}
	if err := db.Where("deleted_at > ?", since).Order("deleted_at").Find(&out.Tombstones).Error; err != nil {
		return out, fmt.Errorf("collect tombstones: %w", err)
	}
	return out, nil
}

func (s *Service) Apply(ctx context.Context, in Changes, recordTombstones bool) (ApplyResult, error) {
	var res ApplyResult
	db := s.DB.WithContext(ctx)
	for i := range in.Presets {
		s.upsertByID(db, &models.Preset{}, in.Presets[i].ID.String(), in.Presets[i].UpdatedAt, &in.Presets[i], &res)
	}
	for i := range in.Schedule {
		s.upsertByKey(db, &models.PresetAssignment{}, "weekday = ?", in.Schedule[i].Weekday, in.Schedule[i].UpdatedAt, &in.Schedule[i], &res)
	}
	for i := range in.Sessions {
		s.upsertByID(db, &models.Session{}, in.Sessions[i].ID.String(), in.Sessions[i].UpdatedAt, &in.Sessions[i], &res)
	}
	for i := range in.Settings {
		s.applySettings(db, in.Settings[i].settings(), &res)
	}
	for i := range in.Tombstones {
		s.applyTombstone(db, in.Tombstones[i], recordTombstones, &res)
	}
	return res, nil
}

func (s *Service) upsertByID(db *gorm.DB, model any, id string, incomingAt time.Time, in any, res *ApplyResult) {
	s.upsertByKey(db, model, "id = ?", id, incomingAt, in, res)
}

func (s *Service) upsertByKey(db *gorm.DB, model any, cond string, key any, incomingAt time.Time, in any, res *ApplyResult) {
	tx := db.Session(&gorm.Session{SkipHooks: true})
	var existingAt time.Time
	row := map[string]any{}
	err := db.Model(model).Where(cond, key).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if createErr := tx.Create(in).Error; createErr != nil {
			s.Log.Warn("sync: строка не применена", zap.Any("key", key), zap.Error(createErr))
			res.Skipped++
			return
		}
		res.Upserted++
		return
	case err != nil:
		res.Skipped++
		return
	}
	if raw, ok := row["updated_at"]; ok {
		switch v := raw.(type) {
		case time.Time:
			existingAt = v
		case string:
			existingAt, _ = time.Parse(time.RFC3339Nano, v)
			if existingAt.IsZero() {
				existingAt, _ = time.Parse("2006-01-02 15:04:05.999999999-07:00", v)
			}
		}
	}
	if !incomingAt.After(existingAt) {
		res.Skipped++
		return
	}
	err = tx.Model(model).Where(cond, key).Select("*").Omit("created_at").UpdateColumns(in).Error
	if err != nil {
		s.Log.Warn("sync: обновление не применено", zap.Any("key", key), zap.Error(err))
		res.Skipped++
		return
	}
	res.Upserted++
}

func (s *Service) applySettings(db *gorm.DB, in models.Settings, res *ApplyResult) {
	s.upsertByKey(db, &models.Settings{}, "id = ?", 1, in.UpdatedAt, &in, res)
}

func (s *Service) applyTombstone(db *gorm.DB, ts models.SyncTombstone, record bool, res *ApplyResult) {
	var q *gorm.DB
	switch ts.Table {
	case "presets":
		q = db.Where("id = ? AND updated_at <= ?", ts.RowID, ts.DeletedAt).Delete(&models.Preset{})
	case "preset_schedule":
		weekday, err := strconv.Atoi(ts.RowID)
		if err != nil {
			res.Skipped++
			return
		}
		q = db.Where("weekday = ? AND updated_at <= ?", weekday, ts.DeletedAt).Delete(&models.PresetAssignment{})
	case "sessions":
		q = db.Where("id = ? AND updated_at <= ?", ts.RowID, ts.DeletedAt).Delete(&models.Session{})
	default:
		res.Skipped++
		return
	}
	if q.Error != nil {
		s.Log.Warn("sync: удаление не применено", zap.String("table", ts.Table), zap.Error(q.Error))
		res.Skipped++
		return
	}
	if q.RowsAffected > 0 {
		res.Deleted++
	}
	if record {
		if err := db.Create(&models.SyncTombstone{Table: ts.Table, RowID: ts.RowID, DeletedAt: ts.DeletedAt}).Error; err != nil {
			s.Log.Warn("sync: томбстоун не записан", zap.Error(err))
		}
	}
}

func (s *Service) GetState(ctx context.Context, key string) time.Time {
	var st models.SyncState
	if err := s.DB.WithContext(ctx).Where("key = ?", key).First(&st).Error; err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, st.Value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *Service) SetState(ctx context.Context, key string, t time.Time) {
	st := models.SyncState{Key: key, Value: t.UTC().Format(time.RFC3339Nano)}
	if err := s.DB.WithContext(ctx).Save(&st).Error; err != nil {
		s.Log.Warn("sync: курсор не сохранён", zap.String("key", key), zap.Error(err))
	}
}
