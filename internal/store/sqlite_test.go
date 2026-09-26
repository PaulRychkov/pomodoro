package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/gorm"

	"github.com/PaulRychkov/pomodoro/internal/migrate/migrations_sqlite"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/sqlitemigrate"
)

func newSQLiteStore(t *testing.T) Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pomodoro.db")
	db, err := gorm.Open(gormlite.Open("file:"+path), &gorm.Config{
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("открыть sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("достать sql.DB: %v", err)
	}
	if err := sqlitemigrate.Up(sqlDB, migrationssqlite.FS); err != nil {
		t.Fatalf("миграции: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewWithDB(db)
}

func samplePreset(name string) *models.Preset {
	return &models.Preset{Name: name, Slots: models.PresetSlots{
		{FocusMinutes: 25, BreakMinutes: 5},
		{FocusMinutes: 25, BreakMinutes: 5},
	}}
}

func TestPresetGetsIdentifierOnSQLite(t *testing.T) {
	store := newSQLiteStore(t)
	ctx := context.Background()

	p := samplePreset("утро")
	if err := store.Presets().Save(ctx, p); err != nil {
		t.Fatalf("сохранить пресет: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Fatalf("пресету не выдан идентификатор")
	}
	list, err := store.Presets().List(ctx)
	if err != nil {
		t.Fatalf("список пресетов: %v", err)
	}
	if len(list) != 1 || list[0].ID == uuid.Nil {
		t.Fatalf("в базе пресет с нулевым идентификатором: %+v", list)
	}
}

func TestTwoPresetsDoNotCollide(t *testing.T) {
	store := newSQLiteStore(t)
	ctx := context.Background()

	first := samplePreset("утро")
	second := samplePreset("вечер")
	if err := store.Presets().Save(ctx, first); err != nil {
		t.Fatalf("первый пресет: %v", err)
	}
	if err := store.Presets().Save(ctx, second); err != nil {
		t.Fatalf("второй пресет: %v", err)
	}
	list, _ := store.Presets().List(ctx)
	if len(list) != 2 {
		t.Fatalf("ожидались два пресета, получено %d", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Fatalf("идентификаторы пресетов совпали: %s", list[0].ID)
	}
}

func TestPresetAssignmentAndDeletionOnSQLite(t *testing.T) {
	store := newSQLiteStore(t)
	ctx := context.Background()

	p := samplePreset("суббота")
	if err := store.Presets().Save(ctx, p); err != nil {
		t.Fatalf("сохранить пресет: %v", err)
	}
	if err := store.Presets().Assign(ctx, 6, &p.ID); err != nil {
		t.Fatalf("назначить пресет на субботу: %v", err)
	}
	sched, err := store.Presets().Schedule(ctx)
	if err != nil {
		t.Fatalf("расписание пресетов: %v", err)
	}
	if len(sched) != 1 || sched[0].Weekday != 6 || sched[0].PresetID != p.ID {
		t.Fatalf("назначение не сохранилось: %+v", sched)
	}
	if err := store.Presets().Assign(ctx, 6, nil); err != nil {
		t.Fatalf("снять назначение: %v", err)
	}
	sched, _ = store.Presets().Schedule(ctx)
	if len(sched) != 0 {
		t.Fatalf("назначение осталось после снятия: %+v", sched)
	}
	if err := store.Presets().Delete(ctx, p.ID); err != nil {
		t.Fatalf("удалить пресет: %v", err)
	}
	list, _ := store.Presets().List(ctx)
	if len(list) != 0 {
		t.Fatalf("пресет не удалён: %+v", list)
	}
}
