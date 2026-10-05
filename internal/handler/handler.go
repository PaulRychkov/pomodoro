package handler

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/store"
	"github.com/PaulRychkov/pomodoro/internal/syncer"
)

type Handler struct {
	engine *engine.Engine
	store  store.Store
	plan   *plan.Service
	log    *zap.Logger
	mcp    http.Handler

	sync      *syncer.Service
	syncToken string
	static    fs.FS
}

func New(e *engine.Engine, st store.Store, pl *plan.Service, log *zap.Logger) *Handler {
	h := &Handler{engine: e, store: st, plan: pl, log: log}
	h.mcp = h.newMCPHandler()
	return h
}

func (h *Handler) WithSync(svc *syncer.Service, token string) *Handler {
	h.sync = svc
	h.syncToken = token
	return h
}

func (h *Handler) WithStatic(static fs.FS) *Handler {
	h.static = static
	return h
}

func (h *Handler) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), cors())

	r.GET("/healthz", h.healthz)
	r.Any("/mcp", gin.WrapH(h.mcp))

	api := r.Group("/api/v1")
	api.GET("/state", h.getState)
	api.GET("/sessions", h.listSessions)
	api.GET("/sessions/active", h.activeSession)
	api.POST("/sessions", h.startSession)
	api.POST("/sessions/:id/stop", h.stopSession)
	api.POST("/sessions/:id/pause", h.pauseSession)
	api.POST("/sessions/:id/resume", h.resumeSession)
	api.PATCH("/sessions/:id", h.patchSession)
	api.GET("/settings", h.getSettings)
	api.PUT("/settings", h.putSettings)
	api.GET("/plan", h.getDayPlan)
	h.rpcRoutes(api)

	if h.sync != nil {
		sg := api.Group("/sync", h.syncAuth())
		sg.GET("/changes", h.syncChanges)
		sg.POST("/changes", h.syncApply)
	}

	if h.static != nil {
		fileServer := http.FileServer(http.FS(h.static))
		r.NoRoute(func(c *gin.Context) {
			p := strings.TrimPrefix(c.Request.URL.Path, "/")
			if p == "" {
				p = "index.html"
			}
			if _, err := fs.Stat(h.static, p); err != nil {
				c.Request.URL.Path = "/"
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	return r
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (h *Handler) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) getState(c *gin.Context) {
	c.JSON(http.StatusOK, h.engine.Snapshot())
}

func (h *Handler) getDayPlan(c *gin.Context) {
	slots, err := h.plan.Day(c.Request.Context(), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"slots": slots})
}

type bindingBody struct {
	Label *string         `json:"label"`
	Task  *models.TaskRef `json:"task"`
}

type startBody struct {
	Kind  string          `json:"kind"`
	Label *string         `json:"label"`
	Task  *models.TaskRef `json:"task"`
}

func (h *Handler) startSession(c *gin.Context) {
	var body startBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	b := engine.Binding{Label: body.Label, Task: body.Task}
	var st engine.State
	var err error
	switch body.Kind {
	case "", "focus":
		st, err = h.engine.StartFocus(c.Request.Context(), b)
	case "break":
		st, err = h.engine.StartBreak(c.Request.Context())
	case "next":
		st, err = h.engine.StartNext(c.Request.Context(), b)
	default:
		writeError(c, http.StatusBadRequest, "invalid_input", "kind must be focus, break or next")
		return
	}
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	s, err := h.store.Sessions().Get(c.Request.Context(), *st.SessionID)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	if s == nil {
		h.log.Error("started session missing in store", zap.String("session_id", st.SessionID.String()))
		writeError(c, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"session": s, "state": st})
}

func (h *Handler) activeSession(c *gin.Context) {
	s, err := h.store.Sessions().FindActive(c.Request.Context())
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	st := h.engine.Snapshot()
	c.JSON(http.StatusOK, gin.H{"session": s, "state": st})
}

func (h *Handler) listSessions(c *gin.Context) {
	now := h.engine.Now()
	from, to := engine.LocalDayBounds(now)
	if v := c.Query("from"); v != "" {
		t, err := parseMoment(v, false)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_input", "from: "+err.Error())
			return
		}
		from = t
	}
	if v := c.Query("to"); v != "" {
		t, err := parseMoment(v, true)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_input", "to: "+err.Error())
			return
		}
		to = t
	}
	sessions, err := h.store.Sessions().ListRange(c.Request.Context(), from, to)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *Handler) stopSession(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	st, err := h.engine.StopSession(c.Request.Context(), id, body.Outcome)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": st})
}

func (h *Handler) pauseSession(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	st, err := h.engine.PauseSession(c.Request.Context(), id)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": st})
}

func (h *Handler) resumeSession(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	st, err := h.engine.ResumeSession(c.Request.Context(), id)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": st})
}

func (h *Handler) patchSession(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body bindingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s, err := h.engine.Relabel(c.Request.Context(), id, engine.Binding{Label: body.Label, Task: body.Task})
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s})
}

func (h *Handler) getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.engine.Settings())
}

func (h *Handler) putSettings(c *gin.Context) {
	var s models.Settings
	if err := c.ShouldBindJSON(&s); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	saved, err := h.engine.UpdateSettings(c.Request.Context(), s)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	h.rebuildPlan(c.Request.Context())
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) rebuildPlan(ctx context.Context) {
	if h.plan == nil {
		return
	}
	if _, err := h.plan.Day(ctx, true); err != nil {
		h.log.Warn("пересборка плана после смены настроек", zap.Error(err))
	}
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) writeEngineError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, engine.ErrInvalidInput):
		writeError(c, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, engine.ErrSessionActive):
		writeError(c, http.StatusConflict, "session_active", err.Error())
	case errors.Is(err, engine.ErrNoActiveSession):
		writeError(c, http.StatusConflict, "no_active_session", err.Error())
	case errors.Is(err, engine.ErrAlreadyPaused):
		writeError(c, http.StatusConflict, "already_paused", err.Error())
	case errors.Is(err, engine.ErrNotPaused):
		writeError(c, http.StatusConflict, "not_paused", err.Error())
	case errors.Is(err, engine.ErrNotActive):
		writeError(c, http.StatusConflict, "not_active", err.Error())
	case errors.Is(err, engine.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, engine.ErrNotFocus):
		writeError(c, http.StatusUnprocessableEntity, "not_focus", err.Error())
	default:
		h.log.Error("internal error", zap.Error(err))
		writeError(c, http.StatusInternalServerError, "internal", "internal error")
	}
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func parseMoment(v string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	d, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, errors.New("expected RFC3339 or YYYY-MM-DD")
	}
	if endOfDay {
		return d.Add(24 * time.Hour), nil
	}
	return d, nil
}
