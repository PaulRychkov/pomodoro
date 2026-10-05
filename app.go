package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/gormlite"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/pomodoro/internal/config"
	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/handler"
	"github.com/PaulRychkov/pomodoro/internal/migrate"
	migrationssqlite "github.com/PaulRychkov/pomodoro/internal/migrate/migrations_sqlite"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/relay"
	"github.com/PaulRychkov/pomodoro/internal/sqlitemigrate"
	"github.com/PaulRychkov/pomodoro/internal/store"
	"github.com/PaulRychkov/pomodoro/internal/syncer"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type App struct {
	ctx      context.Context
	cfg      config.Config
	engine   *engine.Engine
	store    store.Store
	tasks    *tasksclient.Client
	plan     *plan.Service
	log      *zap.Logger
	initErr  error
	shutdown func()
	mu       sync.Mutex
	overlay  bool
	mainW    int
	mainH    int
}

type StatePush struct {
	State  engine.State `json:"state"`
	Reason string       `json:"reason"`
}

func NewApp(cfg config.Config, log *zap.Logger) *App {
	return &App{cfg: cfg, log: log}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.initErr != nil {
		_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "Pomodoro: ошибка запуска",
			Message: "Не удалось инициализировать ядро.\nПроверьте, что PostgreSQL на порту 5434 запущен (docker compose up -d).\n\n" + a.initErr.Error(),
		})
		runtime.Quit(ctx)
		return
	}
	a.engine.SetNotifier(func(st engine.State, reason string) {
		runtime.EventsEmit(a.ctx, "pomodoro:state", StatePush{State: st, Reason: reason})
		// уведомитель работает под мьютексом движка: план трогаем только в горутине
		switch reason {
		case "completed":
			focusDone := st.NextPhase != engine.PhaseFocus
			go func() {
				if focusDone {
					taskID, closed, err := a.plan.HandleFocusCompleted(context.Background())
					if err != nil {
						a.log.Warn("автозакрытие вхождения", zap.Error(err))
					} else if closed {
						a.log.Info("все помидоры задачи выполнены, вхождение закрыто", zap.String("task", taskID))
					}
				}
				a.refreshPlan(false)
			}()
		case "day_rolled", "started", "stopped":
			go a.refreshPlan(false)
		}
	})
}

func (a *App) refreshPlan(rebuild bool) {
	if _, err := a.plan.Day(context.Background(), rebuild); err != nil {
		a.log.Warn("обновление плана дня", zap.Error(err))
	}
}

func openStore(dbCfg config.Database) (store.Store, error) {
	if dbCfg.IsSQLite() {
		db, err := gorm.Open(gormlite.Open(dbCfg.SQLiteDSN()), &gorm.Config{
			Logger:  gormlogger.Default.LogMode(gormlogger.Silent),
			NowFunc: func() time.Time { return time.Now().UTC() },
		})
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("unwrap sqlite: %w", err)
		}
		if err := sqlitemigrate.Up(sqlDB, migrationssqlite.FS); err != nil {
			return nil, fmt.Errorf("sqlite migrations: %w", err)
		}
		return store.NewWithDB(db), nil
	}
	if err := migrate.Up(dbCfg.URL()); err != nil {
		return nil, err
	}
	return store.Open(dbCfg.DSN())
}

func (a *App) initBackend() error {
	st, err := openStore(a.cfg.Database)
	if err != nil {
		return err
	}
	backendCtx, cancel := context.WithCancel(context.Background())

	eng := engine.New(st, engine.SystemClock())
	if err := eng.Init(backendCtx); err != nil {
		cancel()
		return err
	}
	go eng.Run(backendCtx)

	if len(a.cfg.Kafka.Brokers) > 0 && a.cfg.Kafka.Brokers[0] != "" {
		rel := relay.New(st, a.cfg.Kafka.Topic, func() (relay.Publisher, error) {
			return relay.NewSaramaPublisher(a.cfg.Kafka.Brokers)
		}, a.log)
		go rel.Run(backendCtx)
	}

	a.engine = eng
	a.store = st
	a.tasks = tasksclient.New(a.cfg.TasksBaseURL, a.cfg.TasksSource)
	a.plan = &plan.Service{
		Store:     st,
		Tasks:     a.tasks,
		Settings:  eng.Settings,
		Completed: func() int { return eng.Snapshot().CompletedToday },
	}
	eng.SetDurationProvider(a.plan.SlotDurations)
	eng.SetBlocksProvider(a.plan.Blocks)
	go a.refreshPlan(false)

	h := handler.New(eng, st, a.plan, a.log)
	if db := store.DBOf(st); db != nil {
		syncSvc := &syncer.Service{DB: db, Log: a.log}
		h.WithSync(syncSvc, a.cfg.SyncToken)
		if a.cfg.SyncURL != "" {
			cl := syncer.NewClient(syncSvc, a.cfg.SyncURL, a.cfg.SyncToken, a.cfg.SyncInterval, a.log)
			cl.AfterPull = func(c context.Context) {
				changed, err := eng.ReloadSettings(c)
				if err != nil {
					a.log.Warn("перечитывание настроек после синхронизации", zap.Error(err))
					return
				}
				if changed {
					if _, err := a.plan.Day(c, true); err != nil {
						a.log.Warn("пересборка плана после синхронизации", zap.Error(err))
					}
				}
			}
			go cl.Run(backendCtx)
		}
	}
	srv := &http.Server{Addr: fmt.Sprintf(":%d", a.cfg.HTTPPort), Handler: h.Router()}
	go func() {
		a.log.Info("api listening", zap.Int("port", a.cfg.HTTPPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.log.Error("api server failed", zap.Error(err))
		}
	}()
	a.shutdown = func() {
		cancel()
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			a.log.Warn("api shutdown", zap.Error(err))
		}
	}
	return nil
}

func (a *App) onShutdown(ctx context.Context) {
	if a.shutdown != nil {
		a.shutdown()
	}
}

func (a *App) GetState() engine.State {
	return a.engine.Snapshot()
}

func (a *App) StartFocus(b engine.Binding) (engine.State, error) {
	return a.engine.StartFocus(a.ctx, b)
}

func (a *App) StartBreak() (engine.State, error) {
	return a.engine.StartBreak(a.ctx)
}

func (a *App) StartNext(b engine.Binding) (engine.State, error) {
	return a.engine.StartNext(a.ctx, b)
}

func (a *App) Pause() (engine.State, error) {
	return a.engine.Pause(a.ctx)
}

func (a *App) Resume() (engine.State, error) {
	return a.engine.Resume(a.ctx)
}

func (a *App) Stop(outcome string) (engine.State, error) {
	return a.engine.Stop(a.ctx, outcome)
}

func (a *App) Relabel(id string, b engine.Binding) (*models.Session, error) {
	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse session id: %w", err)
	}
	return a.engine.Relabel(a.ctx, sid, b)
}

func (a *App) ListTodaySessions() ([]models.Session, error) {
	now := a.engine.Now().Local()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return a.store.Sessions().ListRange(a.ctx, from, from.Add(24*time.Hour))
}

func (a *App) SearchTasks(query string) ([]tasksclient.TaskOption, error) {
	return a.tasks.Search(a.ctx, query, 20)
}

func (a *App) GetSettings() models.Settings {
	return a.engine.Settings()
}

func (a *App) SaveSettings(s models.Settings) (models.Settings, error) {
	saved, err := a.engine.UpdateSettings(a.ctx, s)
	if err != nil {
		return models.Settings{}, err
	}
	if _, err := a.plan.Day(a.ctx, true); err != nil {
		a.log.Warn("пересборка плана после смены настроек", zap.Error(err))
	}
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	if overlay {
		runtime.WindowSetSize(a.ctx, saved.Overlay.Size, saved.Overlay.Size)
	}
	return saved, nil
}

func (a *App) ChooseSoundFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите звук окончания",
		Filters: []runtime.FileFilter{
			{DisplayName: "Аудио (*.wav;*.mp3;*.ogg)", Pattern: "*.wav;*.mp3;*.ogg"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("open sound dialog: %w", err)
	}
	return path, nil
}

func (a *App) EnterOverlay() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.overlay {
		return nil
	}
	a.mainW, a.mainH = runtime.WindowGetSize(a.ctx)
	s := a.engine.Settings()
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetSize(a.ctx, s.Overlay.Size, s.Overlay.Size)
	a.overlay = true
	runtime.EventsEmit(a.ctx, "pomodoro:mode", "overlay")
	return nil
}

func (a *App) ExitOverlay() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.overlay {
		return nil
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	if a.mainW > 0 && a.mainH > 0 {
		runtime.WindowSetSize(a.ctx, a.mainW, a.mainH)
	}
	runtime.WindowCenter(a.ctx)
	a.overlay = false
	runtime.EventsEmit(a.ctx, "pomodoro:mode", "main")
	runtime.WindowUnminimise(a.ctx)
	return nil
}

func (a *App) Minimise() {
	runtime.WindowMinimise(a.ctx)
}

func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
