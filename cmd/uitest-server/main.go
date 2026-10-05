// Command uitest-server — тестовый бэкенд для браузерных e2e-тестов (Playwright).
// Собирает тот же стек, что и mobile.Start (SQLite + engine + plan + handler +
// статика фронтенда), но с управляемыми часами и подменой task-planner на том же
// листенере. Управление — под /__test/, фейковый planner — под /__tasks/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18090", "listen address")
	web := flag.String("web", "frontend/dist", "directory with built frontend")
	tz := flag.String("tz", "Asia/Qyzylorda", "time zone of the day plan")
	data := flag.String("data", "", "directory for temporary SQLite files (default: $TMPDIR/uitest-server-<port>, wiped on start)")
	flag.Parse()

	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tz:", err)
		os.Exit(1)
	}
	time.Local = loc

	if _, err := os.Stat(filepath.Join(*web, "index.html")); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no built frontend in %s (npm --prefix frontend run build); only the API is served\n", *web)
	}

	logCfg := zap.NewProductionConfig()
	logCfg.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	log, err := logCfg.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}

	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "addr:", err)
		os.Exit(1)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}

	// Порт занимаем до чистки каталога: второй экземпляр на том же порту не должен
	// стереть БД работающего.
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}

	dataDir := *data
	if dataDir == "" {
		// Каталог чистится при старте: после SIGKILL (так Playwright гасит webServer) БД не копятся.
		dataDir = filepath.Join(os.TempDir(), "uitest-server-"+port)
		if err := os.RemoveAll(dataDir); err != nil {
			fmt.Fprintln(os.Stderr, "data dir:", err)
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "data dir:", err)
		os.Exit(1)
	}

	clock := &fakeClock{}
	srv := &server{
		dataDir:   dataDir,
		web:       os.DirFS(*web),
		tasksBase: "http://" + net.JoinHostPort(host, port) + "/__tasks",
		clock:     clock,
		tasks:     newFakeTasks(clock, loc),
		loc:       loc,
		log:       log,
	}
	if err := srv.reset(resetRequest{}); err != nil {
		fmt.Fprintln(os.Stderr, "initial stack:", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("/__tasks/", srv.tasks.Handler())
	srv.registerControl(mux)
	mux.Handle("/", srv)

	httpSrv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	fmt.Printf("uitest-server: http://%s (tz %s, web %s)\n", *addr, *tz, *web)
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
	srv.mu.Lock()
	srv.cur.close()
	srv.mu.Unlock()
	if *data == "" {
		_ = os.RemoveAll(dataDir)
	}
}

// server держит активный стек за RWMutex: reset ждёт завершения запросов
// в полёте, подменяет стек и только потом сносит старый.
type server struct {
	dataDir   string
	web       fs.FS
	tasksBase string
	clock     *fakeClock
	tasks     *fakeTasks
	loc       *time.Location
	log       *zap.Logger

	resetMu sync.Mutex
	mu      sync.RWMutex
	cur     *stack
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.cur.handler.ServeHTTP(w, r)
}
