import type { Binding, PickerNode, PlanSlot, Preset, ScheduleEntry, Session, Settings, SlotPatch, State, StatePush, TaskOption } from "./types";

interface GoApp {
  GetState(): Promise<State>;
  StartFocus(b: Binding): Promise<State>;
  StartBreak(): Promise<State>;
  StartNext(b: Binding): Promise<State>;
  Pause(): Promise<State>;
  Resume(): Promise<State>;
  Stop(outcome: string): Promise<State>;
  Relabel(id: string, b: Binding): Promise<Session>;
  ListTodaySessions(): Promise<Session[]>;
  SearchTasks(query: string): Promise<TaskOption[]>;
  GetDayPlan(): Promise<PlanSlot[]>;
  RefreshDayPlan(): Promise<PlanSlot[]>;
  SetPlanSlot(idx: number, p: SlotPatch): Promise<PlanSlot[]>;
  ListPlanCandidates(): Promise<TaskOption[]>;
  GetPickerTree(): Promise<PickerNode[]>;
  ListPresets(): Promise<Preset[]>;
  SavePreset(name: string): Promise<Preset[]>;
  DeletePreset(name: string): Promise<Preset[]>;
  GetPresetSchedule(): Promise<ScheduleEntry[]>;
  AssignPreset(weekday: number, presetName: string): Promise<ScheduleEntry[]>;
  GetSettings(): Promise<Settings>;
  SaveSettings(s: Settings): Promise<Settings>;
  ChooseSoundFile(): Promise<string>;
  EnterOverlay(): Promise<void>;
  ExitOverlay(): Promise<void>;
  Minimise(): Promise<void>;
  Quit(): Promise<void>;
}

interface WailsRuntime {
  EventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void;
}

declare global {
  interface Window {
    go?: { main: { App: GoApp } };
    runtime?: WailsRuntime;
  }
}

function app(): GoApp {
  if (!window.go) {
    throw new Error("wails bindings are not available");
  }
  return window.go.main.App;
}

export const api = {
  getState: () => app().GetState(),
  startFocus: (b: Binding) => app().StartFocus(b),
  startBreak: () => app().StartBreak(),
  startNext: (b: Binding) => app().StartNext(b),
  pause: () => app().Pause(),
  resume: () => app().Resume(),
  stop: (outcome: string) => app().Stop(outcome),
  relabel: (id: string, b: Binding) => app().Relabel(id, b),
  listTodaySessions: () => app().ListTodaySessions(),
  searchTasks: (query: string) => app().SearchTasks(query),
  getDayPlan: () => app().GetDayPlan(),
  refreshDayPlan: () => app().RefreshDayPlan(),
  setPlanSlot: (idx: number, p: SlotPatch) => app().SetPlanSlot(idx, p),
  listPlanCandidates: () => app().ListPlanCandidates(),
  getPickerTree: () => app().GetPickerTree(),
  listPresets: () => app().ListPresets(),
  savePreset: (name: string) => app().SavePreset(name),
  deletePreset: (name: string) => app().DeletePreset(name),
  getPresetSchedule: () => app().GetPresetSchedule(),
  assignPreset: (weekday: number, presetName: string) => app().AssignPreset(weekday, presetName),
  getSettings: () => app().GetSettings(),
  saveSettings: (s: Settings) => app().SaveSettings(s),
  chooseSoundFile: () => app().ChooseSoundFile(),
  enterOverlay: () => app().EnterOverlay(),
  exitOverlay: () => app().ExitOverlay(),
  minimise: () => app().Minimise(),
  quit: () => app().Quit(),
};

export function onStatePush(cb: (push: StatePush) => void): () => void {
  if (!window.runtime) {
    return () => undefined;
  }
  return window.runtime.EventsOn("pomodoro:state", (data) => cb(data as StatePush));
}

export function onModeChange(cb: (mode: "main" | "overlay") => void): () => void {
  if (!window.runtime) {
    return () => undefined;
  }
  return window.runtime.EventsOn("pomodoro:mode", (data) => cb(data as "main" | "overlay"));
}

export function playChime(soundFile: string | null): void {
  const src = soundFile ? "/sound/custom" : "/sound/default";
  const audio = new Audio(src + "?t=" + Date.now());
  void audio.play().catch(() => undefined);
}
