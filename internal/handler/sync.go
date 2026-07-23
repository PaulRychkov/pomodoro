package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PaulRychkov/pomodoro/internal/syncer"
)

func (h *Handler) syncAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.syncToken == "" {
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.TrimPrefix(header, "Bearer ") != h.syncToken {
			writeError(c, http.StatusUnauthorized, "unauthorized", "invalid sync token")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *Handler) syncChanges(c *gin.Context) {
	since := time.Time{}
	if raw := c.Query("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_input", "since must be RFC3339")
			return
		}
		since = parsed
	}
	changes, err := h.sync.Collect(c.Request.Context(), since)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, changes)
}

func (h *Handler) syncApply(c *gin.Context) {
	var in syncer.Changes
	if err := c.ShouldBindJSON(&in); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := h.sync.Apply(c.Request.Context(), in, true)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}
