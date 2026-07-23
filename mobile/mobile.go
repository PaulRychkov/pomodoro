package mobile

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/gormlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/handler"
	migrationssqlite "github.com/PaulRychkov/pomodoro/internal/migrate/migrations_sqlite"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/sqlitemigrate"
	"github.com/PaulRychkov/pomodoro/internal/store"
	"github.com/PaulRychkov/pomodoro/internal/syncer"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

//go:embed all:webdist
var webFS embed.FS

const Addr = "127.0.0.1:18082"

var (
	mu     sync.Mutex
	srv    *http.Server
	cancel context.CancelFunc
)

func Start(dataDir, tasksURL, syncURL, syncToken string) string {
	mu.Lock()
	defer mu.Unlock()
	if srv != nil {
		return ""
	}
	log, err := zap.NewProduction()
	if err != nil {
		return "logger: " + err.Error()
	}
	if tasksURL == "" {
		tasksURL = "http://127.0.0.1:18081"
	}
	dbPath := "file:" + strings.ReplaceAll(filepath.Join(dataDir, "pomodoro.db"), "\\", "/") +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(gormlite.Open(dbPath), &gorm.Config{
		Logger:  gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return "open db: " + err.Error()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return "unwrap db: " + err.Error()
	}
	if err := sqlitemigrate.Up(sqlDB, migrationssqlite.FS); err != nil {
		return "migrate: " + err.Error()
	}
	st := store.NewWithDB(db)

	ctx, stop := context.WithCancel(context.Background())

	eng := engine.New(st, engine.SystemClock())
	if err := eng.Init(ctx); err != nil {
		stop()
		return "engine: " + err.Error()
	}
	go eng.Run(ctx)

	tasks := tasksclient.New(tasksURL, "tasks")
	planSvc := &plan.Service{
		Store:     st,
		Tasks:     tasks,
		Settings:  eng.Settings,
		Completed: func() int { return eng.Snapshot().CompletedToday },
	}
	eng.SetDurationProvider(planSvc.SlotDurations)
	eng.SetNotifier(func(_ engine.State, reason string) {
		if reason == "completed" {
			go func() {
				if _, _, err := planSvc.HandleFocusCompleted(context.Background()); err != nil {
					log.Warn("автозакрытие вхождения", zap.Error(err))
				}
			}()
		}
	})

	static, err := fs.Sub(webFS, "webdist")
	if err != nil {
		stop()
		return "webdist: " + err.Error()
	}
	h := handler.New(eng, st, planSvc, log).WithStatic(static)
	syncSvc := &syncer.Service{DB: db, Log: log}
	h.WithSync(syncSvc, syncToken)
	if syncURL != "" {
		go syncer.NewClient(syncSvc, syncURL, syncToken, 60*time.Second, log).Run(ctx)
	}

	cancel = stop
	srv = &http.Server{Addr: Addr, Handler: h.Router()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("mobile http server", zap.Error(err))
		}
	}()
	return ""
}

func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if srv == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	_ = srv.Shutdown(shutdownCtx)
	srv = nil
}

func BaseURL() string {
	return fmt.Sprintf("http://%s/", Addr)
}
