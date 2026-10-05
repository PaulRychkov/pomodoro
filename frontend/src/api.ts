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

export const isDesktop = Boolean(window.go);

async function rpc<T>(method: "GET" | "POST" | "PUT" | "DELETE", path: string, body?: unknown): Promise<T> {
  const res = await fetch("/api/v1/rpc" + path, {
    method,
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    throw new Error(data?.error?.message ?? data?.error ?? res.statusText);
  }
  return data as T;
}

const wailsApi = {
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

const restApi: typeof wailsApi = {
  getState: () => rpc<State>("GET", "/state"),
  startFocus: (b: Binding) => rpc<State>("POST", "/start-focus", b),
  startBreak: () => rpc<State>("POST", "/start-break", {}),
  startNext: (b: Binding) => rpc<State>("POST", "/start-next", b),
  pause: () => rpc<State>("POST", "/pause", {}),
  resume: () => rpc<State>("POST", "/resume", {}),
  stop: (outcome: string) => rpc<State>("POST", "/stop", { outcome }),
  relabel: (id: string, b: Binding) => rpc<Session>("POST", "/relabel", { id, label: b.label, task: b.task }),
  listTodaySessions: () => rpc<Session[]>("GET", "/sessions-today"),
  searchTasks: (query: string) => rpc<TaskOption[]>("GET", "/search-tasks?q=" + encodeURIComponent(query)),
  getDayPlan: () => rpc<PlanSlot[]>("GET", "/day-plan"),
  refreshDayPlan: () => rpc<PlanSlot[]>("POST", "/refresh-day-plan", {}),
  setPlanSlot: (idx: number, p: SlotPatch) => rpc<PlanSlot[]>("POST", "/set-plan-slot", { idx, ...p }),
  listPlanCandidates: () => rpc<TaskOption[]>("GET", "/plan-candidates"),
  getPickerTree: () => rpc<PickerNode[]>("GET", "/picker-tree"),
  listPresets: () => rpc<Preset[]>("GET", "/presets"),
  savePreset: (name: string) => rpc<Preset[]>("POST", "/presets", { name }),
  deletePreset: (name: string) => rpc<Preset[]>("DELETE", "/presets/" + encodeURIComponent(name)),
  getPresetSchedule: () => rpc<ScheduleEntry[]>("GET", "/preset-schedule"),
  assignPreset: (weekday: number, presetName: string) =>
    rpc<ScheduleEntry[]>("PUT", "/preset-schedule", { weekday, preset_name: presetName }),
  getSettings: async () => {
    const res = await fetch("/api/v1/settings");
    return (await res.json()) as Settings;
  },
  saveSettings: async (s: Settings) => {
    const res = await fetch("/api/v1/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(s),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message ?? res.statusText);
    return data as Settings;
  },
  chooseSoundFile: async () => "",
  enterOverlay: async () => undefined,
  exitOverlay: async () => undefined,
  minimise: async () => undefined,
  quit: async () => undefined,
};

export const api = isDesktop ? wailsApi : restApi;

/**
 * В REST/polling-режиме нет push-событий движка, поэтому причину перехода
 * восстанавливаем сравнением предыдущего состояния с новым. За один опрос
 * может случиться несколько переходов (например, помидор завершился и
 * автостартовал перерыв), поэтому причин может быть несколько.
 */
function deriveReasons(prev: State, next: State, prevAt: number): string[] {
  const reasons: string[] = [];
  const sessionChanged = next.session_id !== prev.session_id;
  const hadSession = prev.session_id != null;
  if (next.completed_today < prev.completed_today) {
    reasons.push("day_rolled");
  }
  if (hadSession && (next.phase === "idle" || sessionChanged)) {
    // Перерыв счётчик не двигает: закончился сам, если к этому опросу его время вышло.
    const breakRanOut =
      prev.phase !== "focus" && !prev.paused && Date.now() >= prevAt + prev.remaining_seconds * 1000 - 1500;
    reasons.push(next.completed_today > prev.completed_today || breakRanOut ? "completed" : "stopped");
  }
  if (sessionChanged && next.session_id != null) {
    reasons.push("started");
  }
  if (!sessionChanged && next.session_id != null && prev.paused !== next.paused) {
    reasons.push(next.paused ? "paused" : "resumed");
  }
  if (prev.settings_stamp !== next.settings_stamp) {
    reasons.push("settings");
  }
  return reasons;
}

export function onStatePush(cb: (push: StatePush) => void): () => void {
  if (window.runtime) {
    return window.runtime.EventsOn("pomodoro:state", (data) => cb(data as StatePush));
  }
  let prev: State | null = null;
  let prevAt = 0;
  let prevKey = "";
  const timer = window.setInterval(async () => {
    try {
      const st = await restApi.getState();
      const key = JSON.stringify(st);
      if (key !== prevKey) {
        const reasons = prev ? deriveReasons(prev, st, prevAt) : [];
        prev = st;
        prevAt = Date.now();
        prevKey = key;
        if (reasons.length === 0) {
          reasons.push("poll");
        }
        for (const reason of reasons) {
          cb({ state: st, reason });
        }
      }
    } catch {
      /* сервер ещё поднимается */
    }
  }, 1000);
  return () => window.clearInterval(timer);
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
