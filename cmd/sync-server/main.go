package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/pomodoro/internal/config"
	"github.com/PaulRychkov/pomodoro/internal/syncer"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config", zap.Error(err))
	}

	db, err := gorm.Open(postgres.Open(cfg.Database.DSN()), &gorm.Config{
		Logger:  gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		log.Fatal("open db", zap.Error(err))
	}

	token := cfg.SyncToken
	if token == "" {
		log.Warn("POMO_SYNC_TOKEN пуст — синхронизация открыта без авторизации")
	}

	svc := &syncer.Service{DB: db, Log: log}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	sg := r.Group("/api/v1/sync", auth(token))
	sg.GET("/changes", func(c *gin.Context) {
		since := time.Time{}
		if raw := c.Query("since"); raw != "" {
			parsed, perr := time.Parse(time.RFC3339Nano, raw)
			if perr != nil {
				c.JSON(http.StatusBadRequest, errBody("invalid_input", "since must be RFC3339"))
				return
			}
			since = parsed
		}
		changes, cerr := svc.Collect(c.Request.Context(), since)
		if cerr != nil {
			c.JSON(http.StatusInternalServerError, errBody("internal", cerr.Error()))
			return
		}
		c.JSON(http.StatusOK, changes)
	})
	sg.POST("/changes", func(c *gin.Context) {
		var in syncer.Changes
		if berr := c.ShouldBindJSON(&in); berr != nil {
			c.JSON(http.StatusBadRequest, errBody("invalid_input", berr.Error()))
			return
		}
		res, aerr := svc.Apply(c.Request.Context(), in, true)
		if aerr != nil {
			c.JSON(http.StatusInternalServerError, errBody("internal", aerr.Error()))
			return
		}
		c.JSON(http.StatusOK, res)
	})

	addr := ":" + strings.TrimPrefix(os.Getenv("POMO_SYNC_PORT"), ":")
	if addr == ":" {
		addr = ":8086"
	}
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Info("pomodoro sync-server запущен", zap.String("addr", addr))
		if serr := srv.ListenAndServe(); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			log.Fatal("listen", zap.Error(serr))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("остановлен")
}

func auth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.TrimPrefix(header, "Bearer ") != token {
			c.JSON(http.StatusUnauthorized, errBody("unauthorized", "invalid sync token"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func errBody(code, msg string) gin.H {
	return gin.H{"error": gin.H{"code": code, "message": msg}}
}
