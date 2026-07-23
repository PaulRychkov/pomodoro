package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
)

func (h *Handler) rpcRoutes(api *gin.RouterGroup) {
	rpc := api.Group("/rpc")
	rpc.GET("/state", func(c *gin.Context) { c.JSON(http.StatusOK, h.engine.Snapshot()) })
	rpc.POST("/start-focus", h.rpcStartFocus)
	rpc.POST("/start-break", h.rpcStartBreak)
	rpc.POST("/start-next", h.rpcStartNext)
	rpc.POST("/pause", h.rpcState(func(c *gin.Context) (engine.State, error) { return h.engine.Pause(c.Request.Context()) }))
	rpc.POST("/resume", h.rpcState(func(c *gin.Context) (engine.State, error) { return h.engine.Resume(c.Request.Context()) }))
	rpc.POST("/stop", h.rpcStop)
	rpc.POST("/relabel", h.rpcRelabel)
	rpc.GET("/sessions-today", h.rpcSessionsToday)
	rpc.GET("/search-tasks", h.rpcSearchTasks)
	rpc.GET("/day-plan", h.rpcPlan(false))
	rpc.POST("/refresh-day-plan", h.rpcPlan(true))
	rpc.POST("/set-plan-slot", h.rpcSetPlanSlot)
	rpc.GET("/plan-candidates", h.rpcPlanCandidates)
	rpc.GET("/picker-tree", h.rpcPickerTree)
	rpc.GET("/presets", h.rpcListPresets)
	rpc.POST("/presets", h.rpcSavePreset)
	rpc.DELETE("/presets/:name", h.rpcDeletePreset)
	rpc.GET("/preset-schedule", h.rpcPresetSchedule)
	rpc.PUT("/preset-schedule", h.rpcAssignPreset)
}

func (h *Handler) rpcState(fn func(*gin.Context) (engine.State, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		st, err := fn(c)
		if err != nil {
			h.writeEngineError(c, err)
			return
		}
		c.JSON(http.StatusOK, st)
	}
}

func (h *Handler) rpcStartFocus(c *gin.Context) {
	var b bindingBody
	if err := c.ShouldBindJSON(&b); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	h.rpcState(func(c *gin.Context) (engine.State, error) {
		return h.engine.StartFocus(c.Request.Context(), engine.Binding{Label: b.Label, Task: b.Task})
	})(c)
}

func (h *Handler) rpcStartBreak(c *gin.Context) {
	h.rpcState(func(c *gin.Context) (engine.State, error) {
		return h.engine.StartBreak(c.Request.Context())
	})(c)
}

func (h *Handler) rpcStartNext(c *gin.Context) {
	var b bindingBody
	if err := c.ShouldBindJSON(&b); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	h.rpcState(func(c *gin.Context) (engine.State, error) {
		return h.engine.StartNext(c.Request.Context(), engine.Binding{Label: b.Label, Task: b.Task})
	})(c)
}

func (h *Handler) rpcStop(c *gin.Context) {
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	h.rpcState(func(c *gin.Context) (engine.State, error) {
		return h.engine.Stop(c.Request.Context(), body.Outcome)
	})(c)
}

func (h *Handler) rpcRelabel(c *gin.Context) {
	var body struct {
		ID    string          `json:"id"`
		Label *string         `json:"label"`
		Task  *models.TaskRef `json:"task"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sid, err := uuid.Parse(body.ID)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return
	}
	s, err := h.engine.Relabel(c.Request.Context(), sid, engine.Binding{Label: body.Label, Task: body.Task})
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *Handler) rpcSessionsToday(c *gin.Context) {
	now := time.Now().Local()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	list, err := h.store.Sessions().ListRange(c.Request.Context(), from, from.Add(24*time.Hour))
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) rpcSearchTasks(c *gin.Context) {
	out, err := h.plan.Tasks.Search(c.Request.Context(), c.Query("q"), 20)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcPlan(refresh bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		slots, err := h.plan.Day(c.Request.Context(), refresh)
		if err != nil {
			h.writeEngineError(c, err)
			return
		}
		c.JSON(http.StatusOK, slots)
	}
}

func (h *Handler) rpcSetPlanSlot(c *gin.Context) {
	var body struct {
		Idx          int             `json:"idx"`
		Task         *models.TaskRef `json:"task"`
		Label        *string         `json:"label"`
		ClearBinding bool            `json:"clear_binding"`
		FocusMinutes *int            `json:"focus_minutes"`
		BreakMinutes *int            `json:"break_minutes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	slots, err := h.plan.SetSlot(c.Request.Context(), body.Idx, plan.SlotUpdate{
		Task:         body.Task,
		Label:        body.Label,
		ClearBinding: body.ClearBinding,
		FocusMinutes: body.FocusMinutes,
		BreakMinutes: body.BreakMinutes,
	})
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, slots)
}

func (h *Handler) rpcPlanCandidates(c *gin.Context) {
	out, err := h.plan.Candidates(c.Request.Context())
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcPickerTree(c *gin.Context) {
	out, err := h.plan.PickerTree(c.Request.Context())
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcListPresets(c *gin.Context) {
	out, err := h.plan.Presets(c.Request.Context())
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcSavePreset(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := h.plan.SavePreset(c.Request.Context(), body.Name)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcDeletePreset(c *gin.Context) {
	out, err := h.plan.DeletePreset(c.Request.Context(), c.Param("name"))
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcPresetSchedule(c *gin.Context) {
	out, err := h.plan.Schedule(c.Request.Context())
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) rpcAssignPreset(c *gin.Context) {
	var body struct {
		Weekday    int    `json:"weekday"`
		PresetName string `json:"preset_name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := h.plan.AssignPreset(c.Request.Context(), body.Weekday, body.PresetName)
	if err != nil {
		h.writeEngineError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

