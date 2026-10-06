package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3/gormlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/handler"
	migrationssqlite "github.com/PaulRychkov/pomodoro/internal/migrate/migrations_sqlite"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/sqlitemigrate"
	"github.com/PaulRychkov/pomodoro/internal/store"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type stack struct {
	engine  *engine.Engine
	handler http.Handler
	cancel  context.CancelFunc
	db      *sql.DB
	dir     string
}

type stackConfig struct {
	dataDir  string
	web      fs.FS
	tasksURL string
	clock    engine.Clock
	settings models.Settings
	log      *zap.Logger
}

func newStack(cfg stackConfig) (*stack, error) {
	dir, err := os.MkdirTemp(cfg.dataDir, "db-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	s, err := buildStack(dir, cfg)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

func buildStack(dir string, cfg stackConfig) (*stack, error) {
	dbPath := "file:" + strings.ReplaceAll(filepath.Join(dir, "pomodoro.db"), "\\", "/") +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(gormlite.Open(dbPath), &gorm.Config{
		Logger:  gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap db: %w", err)
	}
	fail := func(err error) (*stack, error) {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := sqlitemigrate.Up(sqlDB, migrationssqlite.FS); err != nil {
		return fail(fmt.Errorf("migrate: %w", err))
	}
	st := store.NewWithDB(db)

	settings := cfg.settings
	if err := st.Settings().Save(context.Background(), &settings); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng := engine.New(st, cfg.clock)
	if err := eng.Init(ctx); err != nil {
		cancel()
		return fail(fmt.Errorf("engine: %w", err))
	}
	go eng.Run(ctx)

	planSvc := &plan.Service{
		Store:     st,
		Tasks:     tasksclient.New(cfg.tasksURL, "tasks"),
		Settings:  eng.Settings,
		Completed: func() int { return eng.Snapshot().CompletedToday },
		Now:       cfg.clock.Now,
	}
	eng.SetDurationProvider(planSvc.SlotDurations)
	eng.SetBlocksProvider(planSvc.Blocks)
	refreshPlan := func() {
		if _, err := planSvc.Day(context.Background(), false); err != nil {
			cfg.log.Warn("обновление плана дня", zap.Error(err))
		}
	}
	eng.SetNotifier(func(state engine.State, reason string) {
		switch reason {
		case "completed":
			focusDone := state.NextPhase != engine.PhaseFocus
			go func() {
				if focusDone {
					if _, _, err := planSvc.HandleFocusCompleted(context.Background()); err != nil {
						cfg.log.Warn("автозакрытие вхождения", zap.Error(err))
					}
				}
				refreshPlan()
			}()
		case "day_rolled", "started", "stopped":
			go refreshPlan()
		}
	})
	go refreshPlan()

	h := handler.New(eng, st, planSvc, cfg.log).WithStatic(cfg.web)
	return &stack{engine: eng, handler: h.Router(), cancel: cancel, db: sqlDB, dir: dir}, nil
}

func (s *stack) close() {
	s.cancel()
	_ = s.db.Close()
	_ = os.RemoveAll(s.dir)
}

func mergeSettings(raw json.RawMessage) (models.Settings, error) {
	s := models.DefaultSettings()
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &s); err != nil {
			return models.Settings{}, fmt.Errorf("settings: %w", err)
		}
	}
	s.ID = 1
	if err := engine.ValidateSettings(s); err != nil {
		return models.Settings{}, fmt.Errorf("settings: %w", err)
	}
	return s, nil
}
