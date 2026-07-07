package main

import (
	"embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"go.uber.org/zap"

	"github.com/PaulRychkov/pomodoro/internal/config"
	"github.com/PaulRychkov/pomodoro/internal/logger"
	"github.com/PaulRychkov/pomodoro/internal/sound"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := logger.New(cfg.LogLevel)
	defer func() { _ = log.Sync() }()

	app := NewApp(cfg, log)
	if err := app.initBackend(); err != nil {
		log.Error("backend init failed", zap.Error(err))
		app.initErr = err
	}

	err = wails.Run(&options.App{
		Title:     "Pomodoro",
		Width:     980,
		Height:    700,
		MinWidth:  60,
		MinHeight: 60,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: &soundHandler{app: app},
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		OnStartup:        app.startup,
		OnShutdown:       app.onShutdown,
		Bind:             []any{app},
		Windows: &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			BackdropType:                      windows.None,
			DisableFramelessWindowDecorations: true,
		},
	})
	if err != nil {
		log.Error("wails run failed", zap.Error(err))
	}
}

type soundHandler struct {
	app *App
}

func (s *soundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/sound/default":
		writeDefaultChime(w)
	case "/sound/custom":
		if s.app.engine == nil {
			writeDefaultChime(w)
			return
		}
		settings := s.app.engine.Settings()
		if settings.SoundFile == nil || *settings.SoundFile == "" {
			writeDefaultChime(w)
			return
		}
		data, err := os.ReadFile(*settings.SoundFile)
		if err != nil {
			writeDefaultChime(w)
			return
		}
		w.Header().Set("Content-Type", soundMime(*settings.SoundFile))
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func writeDefaultChime(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "audio/wav")
	_, _ = w.Write(sound.DefaultChime())
}

func soundMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	default:
		return "audio/wav"
	}
}
