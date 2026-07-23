package main

import (
	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/plan"
	"github.com/PaulRychkov/pomodoro/internal/tasksclient"
)

type SlotPatch struct {
	Task         *models.TaskRef `json:"task"`
	Label        *string         `json:"label"`
	ClearBinding bool            `json:"clear_binding"`
	FocusMinutes *int            `json:"focus_minutes"`
	BreakMinutes *int            `json:"break_minutes"`
}

func (a *App) GetDayPlan() ([]plan.SlotView, error) {
	return a.plan.Day(a.ctx, false)
}

func (a *App) RefreshDayPlan() ([]plan.SlotView, error) {
	return a.plan.Day(a.ctx, true)
}

func (a *App) SetPlanSlot(idx int, p SlotPatch) ([]plan.SlotView, error) {
	return a.plan.SetSlot(a.ctx, idx, plan.SlotUpdate{
		Task:         p.Task,
		Label:        p.Label,
		ClearBinding: p.ClearBinding,
		FocusMinutes: p.FocusMinutes,
		BreakMinutes: p.BreakMinutes,
	})
}

func (a *App) ListPlanCandidates() ([]tasksclient.TaskOption, error) {
	return a.plan.Candidates(a.ctx)
}

func (a *App) GetPickerTree() ([]plan.PickerNode, error) {
	return a.plan.PickerTree(a.ctx)
}

func (a *App) ListPresets() ([]models.Preset, error) {
	return a.plan.Presets(a.ctx)
}

func (a *App) SavePreset(name string) ([]models.Preset, error) {
	return a.plan.SavePreset(a.ctx, name)
}

func (a *App) DeletePreset(name string) ([]models.Preset, error) {
	return a.plan.DeletePreset(a.ctx, name)
}

func (a *App) GetPresetSchedule() ([]plan.ScheduleEntry, error) {
	return a.plan.Schedule(a.ctx)
}

func (a *App) AssignPreset(weekday int, presetName string) ([]plan.ScheduleEntry, error) {
	return a.plan.AssignPreset(a.ctx, weekday, presetName)
}
