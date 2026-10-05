package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/PaulRychkov/pomodoro/internal/engine"
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
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
	Outcome string `json:"outcome,omitempty" jsonschema:"completed — завершить досрочно с частичным зачётом; abandoned — бросить без зачёта; по умолчанию abandoned"`
}

type stopSessionOutput struct {
	Stopped        bool    `json:"stopped"`
	NextPhase      string  `json:"next_phase"`
	CompletedToday int     `json:"completed_today"`
	CreditToday    float64 `json:"credit_today"`
}

type todayStatsOutput struct {
	CompletedToday    int     `json:"completed_today"`
	CreditToday       float64 `json:"credit_today"`
	DayTotal          int     `json:"day_total"`
	DayBlocks         []int   `json:"day_blocks"`
	BlockIndex        int     `json:"block_index"`
	PosInBlock        int     `json:"pos_in_block"`
	DayComplete       bool    `json:"day_complete"`
	FocusSecondsToday int     `json:"focus_seconds_today"`
	AbandonedToday    int     `json:"abandoned_today"`
	ActivePhase       string  `json:"active_phase"`
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
		Description: "Остановить активную сессию. completed до конца таймера — досрочное завершение: засчитывается доля помидора по фактическому фокусу, округлённая до 0, 1/4, 1/3, 1/2, 2/3, 3/4 или 1; доля 0 записывается как брошенный помидор. abandoned — бросить без зачёта",
	}, h.mcpStopSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_today_stats",
		Description: "Статистика помидоров за сегодня: completed_today — сколько слотов плана пройдено, credit_today — сумма засчитанных долей помидоров, фокус-время в секундах",
	}, h.mcpGetTodayStats)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_day_plan",
		Description: "План помидоров на сегодня: все слоты дня с привязанными задачами; done-слоты уже выполнены",
	}, h.mcpGetDayPlan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_plan_slot",
		Description: "Назначить слоту помидора задачу (task_external_id из tasks) или метку; пустой вызов возвращает слот в автораспределение",
	}, h.mcpSetPlanSlot)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "refresh_day_plan",
		Description: "Перераспределить незакреплённые слоты по актуальным задачам и их трудозатратам (effort_minutes)",
	}, h.mcpRefreshDayPlan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_plan_settings",
		Description: "Настройки раскладки дня: учёба до/после рабочих окон (минуты), доля помидоров задаче окна (%), размеры блоков. Пустой вызов просто возвращает текущие настройки",
	}, h.mcpUpdatePlanSettings)

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
}

type planOutput struct {
	Slots []plan.SlotView `json:"slots"`
}

type setPlanSlotInput struct {
	Idx            int    `json:"idx" jsonschema:"индекс слота 0..N-1"`
	TaskSource     string `json:"task_source,omitempty" jsonschema:"источник задачи, по умолчанию tasks"`
	TaskExternalID string `json:"task_external_id,omitempty" jsonschema:"UUID задачи из tasks"`
	TaskTitle      string `json:"task_title,omitempty" jsonschema:"название задачи для отображения"`
	Label          string `json:"label,omitempty" jsonschema:"свободная метка вместо задачи"`
	ClearBinding   bool   `json:"clear_binding,omitempty" jsonschema:"true — убрать задачу/метку со слота"`
	FocusMinutes   int    `json:"focus_minutes,omitempty" jsonschema:"длительность этого помидора в минутах; -1 — вернуть дефолт; 0/не задано — не менять"`
	BreakMinutes   int    `json:"break_minutes,omitempty" jsonschema:"длительность перерыва после этого помидора в минутах; -1 — вернуть дефолт; 0/не задано — не менять"`
}

type planSettingsInput struct {
	DayStartMinutes    *int  `json:"day_start_minutes,omitempty" jsonschema:"начало активного дня в минутах от полуночи, например 360 = 06:00"`
	DayEndMinutes      *int  `json:"day_end_minutes,omitempty" jsonschema:"конец активного дня в минутах от полуночи, например 1200 = 20:00"`
	WindowSharePercent *int  `json:"window_share_percent,omitempty" jsonschema:"процент помидоров окна с фиксированным временем (например, работы), отдаваемый задаче окна; остальное — другим задачам (0-100)"`
	DayBlocks          []int `json:"day_blocks,omitempty" jsonschema:"размеры блоков дня, например [3,3,3]: длинный перерыв после каждого блока"`
}

type planSettingsOutput struct {
	DayStartMinutes    int   `json:"day_start_minutes"`
	DayEndMinutes      int   `json:"day_end_minutes"`
	WindowSharePercent int   `json:"window_share_percent"`
	DayBlocks          []int `json:"day_blocks"`
}

func (h *Handler) mcpUpdatePlanSettings(ctx context.Context, _ *mcp.CallToolRequest, in planSettingsInput) (*mcp.CallToolResult, planSettingsOutput, error) {
	s := h.engine.Settings()
	changed := false
	if in.DayStartMinutes != nil {
		s.DayStartMin = clampInt(*in.DayStartMinutes, 0, 24*60-1)
		changed = true
	}
	if in.DayEndMinutes != nil {
		s.DayEndMin = clampInt(*in.DayEndMinutes, 1, 24*60)
		changed = true
	}
	if in.WindowSharePercent != nil {
		s.WindowSharePercent = clampInt(*in.WindowSharePercent, 0, 100)
		changed = true
	}
	if len(in.DayBlocks) > 0 {
		blocks := make(models.IntList, 0, len(in.DayBlocks))
		for _, b := range in.DayBlocks {
			blocks = append(blocks, clampInt(b, 1, 16))
		}
		s.DayBlocks = blocks
		changed = true
	}
	if changed {
		saved, err := h.engine.UpdateSettings(ctx, s)
		if err != nil {
			return nil, planSettingsOutput{}, err
		}
		s = saved
		h.rebuildPlan(ctx)
	}
	return nil, planSettingsOutput{
		DayStartMinutes:    s.DayStartMin,
		DayEndMinutes:      s.DayEndMin,
		WindowSharePercent: s.WindowSharePercent,
		DayBlocks:          s.DayBlocks,
	}, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (h *Handler) mcpGetDayPlan(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, planOutput, error) {
	slots, err := h.plan.Day(ctx, false)
	if err != nil {
		return nil, planOutput{}, err
	}
	return nil, planOutput{Slots: slots}, nil
}

func (h *Handler) mcpSetPlanSlot(ctx context.Context, _ *mcp.CallToolRequest, in setPlanSlotInput) (*mcp.CallToolResult, planOutput, error) {
	upd := plan.SlotUpdate{ClearBinding: in.ClearBinding}
	if in.TaskExternalID != "" {
		src := in.TaskSource
		if src == "" {
			src = "tasks"
		}
		upd.Task = &models.TaskRef{Source: src, ExternalID: in.TaskExternalID, TitleSnapshot: in.TaskTitle}
	} else if in.Label != "" {
		l := in.Label
		upd.Label = &l
	}
	if in.FocusMinutes != 0 {
		v := in.FocusMinutes
		upd.FocusMinutes = &v
	}
	if in.BreakMinutes != 0 {
		v := in.BreakMinutes
		upd.BreakMinutes = &v
	}
	slots, err := h.plan.SetSlot(ctx, in.Idx, upd)
	if err != nil {
		return nil, planOutput{}, err
	}
	return nil, planOutput{Slots: slots}, nil
}

func (h *Handler) mcpRefreshDayPlan(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, planOutput, error) {
	slots, err := h.plan.Day(ctx, true)
	if err != nil {
		return nil, planOutput{}, err
	}
	return nil, planOutput{Slots: slots}, nil
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
		CreditToday:    st.CreditToday,
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
		focusSeconds += s.ElapsedFocusSeconds()
		if s.Outcome != nil && *s.Outcome == models.OutcomeAbandoned {
			abandoned++
		}
	}
	return nil, todayStatsOutput{
		CompletedToday:    st.CompletedToday,
		CreditToday:       st.CreditToday,
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
