package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/models"
)

type emptyInput struct{}

type sessionInfo struct {
	SessionID        string  `json:"session_id"`
	Kind             string  `json:"kind"`
	Phase            string  `json:"phase"`
	Paused           bool    `json:"paused"`
	RemainingSeconds int     `json:"remaining_seconds"`
	PlannedSeconds   int     `json:"planned_seconds"`
	Label            *string `json:"label"`
	TaskSource       *string `json:"task_source"`
	TaskExternalID   *string `json:"task_external_id"`
	TaskTitle        *string `json:"task_title"`
}

type activeSessionOutput struct {
	Active  bool         `json:"active"`
	Session *sessionInfo `json:"session,omitempty"`
}

type startFocusInput struct {
	Label          string `json:"label,omitempty" jsonschema:"свободная текстовая метка того, чем занят пользователь"`
	TaskSource     string `json:"task_source,omitempty" jsonschema:"источник задачи, например tasks"`
	TaskExternalID string `json:"task_external_id,omitempty" jsonschema:"идентификатор задачи во внешнем источнике"`
	TaskTitle      string `json:"task_title,omitempty" jsonschema:"снапшот названия задачи"`
}

type startFocusOutput struct {
	SessionID        string `json:"session_id"`
	PlannedSeconds   int    `json:"planned_seconds"`
	RemainingSeconds int    `json:"remaining_seconds"`
}

type stopSessionInput struct {
	Outcome string `json:"outcome,omitempty" jsonschema:"completed или abandoned; по умолчанию abandoned"`
}

type stopSessionOutput struct {
	Stopped        bool   `json:"stopped"`
	NextPhase      string `json:"next_phase"`
	CompletedToday int    `json:"completed_today"`
}

type todayStatsOutput struct {
	CompletedToday    int    `json:"completed_today"`
	DayTotal          int    `json:"day_total"`
	DayBlocks         []int  `json:"day_blocks"`
	BlockIndex        int    `json:"block_index"`
	PosInBlock        int    `json:"pos_in_block"`
	DayComplete       bool   `json:"day_complete"`
	FocusSecondsToday int    `json:"focus_seconds_today"`
	AbandonedToday    int    `json:"abandoned_today"`
	ActivePhase       string `json:"active_phase"`
}

func (h *Handler) newMCPHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "pomodoro", Version: "1.0.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_active_session",
		Description: "Текущая активная сессия помидор-таймера (фокус или перерыв) с остатком времени",
	}, h.mcpGetActiveSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_focus",
		Description: "Запустить фокус-помидор; можно привязать метку или задачу из внешнего источника",
	}, h.mcpStartFocus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_session",
		Description: "Остановить активную сессию с исходом completed или abandoned",
	}, h.mcpStopSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_today_stats",
		Description: "Статистика помидоров за сегодня: завершённые, прогресс по блокам дня, фокус-время",
	}, h.mcpGetTodayStats)

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
}

func (h *Handler) mcpGetActiveSession(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, activeSessionOutput, error) {
	st := h.engine.Snapshot()
	out := activeSessionOutput{}
	if st.SessionID != nil {
		out.Active = true
		info := &sessionInfo{
			SessionID:        st.SessionID.String(),
			Phase:            string(st.Phase),
			Paused:           st.Paused,
			RemainingSeconds: st.RemainingSeconds,
			PlannedSeconds:   st.PlannedSeconds,
			Label:            st.Label,
		}
		if st.Phase == engine.PhaseFocus {
			info.Kind = models.KindFocus
		} else {
			info.Kind = models.KindBreak
		}
		if st.Task != nil {
			info.TaskSource = &st.Task.Source
			info.TaskExternalID = &st.Task.ExternalID
			if st.Task.TitleSnapshot != "" {
				title := st.Task.TitleSnapshot
				info.TaskTitle = &title
			}
		}
		out.Session = info
	}
	return nil, out, nil
}

func (h *Handler) mcpStartFocus(ctx context.Context, req *mcp.CallToolRequest, in startFocusInput) (*mcp.CallToolResult, startFocusOutput, error) {
	b := engine.Binding{}
	if in.Label != "" {
		label := in.Label
		b.Label = &label
	}
	if in.TaskSource != "" || in.TaskExternalID != "" {
		b.Task = &models.TaskRef{
			Source:        in.TaskSource,
			ExternalID:    in.TaskExternalID,
			TitleSnapshot: in.TaskTitle,
		}
	}
	st, err := h.engine.StartFocus(ctx, b)
	if err != nil {
		return nil, startFocusOutput{}, err
	}
	return nil, startFocusOutput{
		SessionID:        st.SessionID.String(),
		PlannedSeconds:   st.PlannedSeconds,
		RemainingSeconds: st.RemainingSeconds,
	}, nil
}

func (h *Handler) mcpStopSession(ctx context.Context, req *mcp.CallToolRequest, in stopSessionInput) (*mcp.CallToolResult, stopSessionOutput, error) {
	st, err := h.engine.Stop(ctx, in.Outcome)
	if err != nil {
		return nil, stopSessionOutput{}, err
	}
	return nil, stopSessionOutput{
		Stopped:        true,
		NextPhase:      string(st.NextPhase),
		CompletedToday: st.CompletedToday,
	}, nil
}

func (h *Handler) mcpGetTodayStats(ctx context.Context, req *mcp.CallToolRequest, in emptyInput) (*mcp.CallToolResult, todayStatsOutput, error) {
	st := h.engine.Snapshot()
	from, to := engine.LocalDayBounds(time.Now())
	sessions, err := h.store.Sessions().ListRange(ctx, from, to)
	if err != nil {
		return nil, todayStatsOutput{}, err
	}
	focusSeconds := 0
	abandoned := 0
	for _, s := range sessions {
		if s.Kind != models.KindFocus || s.EndedAt == nil {
			continue
		}
		focusSeconds += int(s.EndedAt.Sub(s.StartedAt)/time.Second) - s.PausedTotalSeconds
		if s.Outcome != nil && *s.Outcome == models.OutcomeAbandoned {
			abandoned++
		}
	}
	return nil, todayStatsOutput{
		CompletedToday:    st.CompletedToday,
		DayTotal:          st.DayTotal,
		DayBlocks:         st.DayBlocks,
		BlockIndex:        st.BlockIndex,
		PosInBlock:        st.PosInBlock,
		DayComplete:       st.DayComplete,
		FocusSecondsToday: focusSeconds,
		AbandonedToday:    abandoned,
		ActivePhase:       string(st.Phase),
	}, nil
}
