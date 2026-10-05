import { expect, type APIRequestContext, type APIResponse, type Locator, type Page } from "@playwright/test";

// ---------------------------------------------------------------------------
// Управление тестовым сервером (cmd/uitest-server): /__test/*
// ---------------------------------------------------------------------------

/** Время сегодняшнего дня в tz сервера: 05:50 — за десять минут до начала активного дня (06:00). */
export const DEFAULT_NOW = "05:50";

/** Фикстура без задач: помидоры не привязаны к плану, длительности берутся из настроек. */
export const NO_TASKS = { tasks: [] };

export interface PomodoroState {
  phase: "idle" | "focus" | "short_break" | "long_break";
  next_phase: "focus" | "short_break" | "long_break";
  paused: boolean;
  paused_at: string | null;
  paused_total_seconds: number;
  planned_seconds: number;
  remaining_seconds: number;
  completed_today: number;
  credit_today: number;
  day_total: number;
  session_id: string | null;
  [key: string]: unknown;
}

export interface ClockInfo {
  now: string;
  offset_ms: number;
  state?: PomodoroState;
}

export interface ResetOptions {
  /** RFC3339, «2026-10-05T05:50:00» (tz сервера) или «05:50» — это время сегодняшнего дня. */
  now?: string;
  /** Подмена фикстуры task-planner: {tasks?, topics?} или просто массив задач. */
  tasks?: unknown;
  /** Частичные настройки поверх models.DefaultSettings(). */
  settings?: Record<string, unknown>;
}

export interface TasksLogEntry {
  at: string;
  kind: "progress" | "complete";
  occurrence_id: string;
  task_id: string;
  date: string;
  minutes?: number;
}

async function json<T>(res: APIResponse): Promise<T> {
  if (!res.ok()) {
    throw new Error(`${res.url()} -> ${res.status()}: ${await res.text()}`);
  }
  return (await res.json()) as T;
}

/** Пересоздаёт весь бэкенд (чистая БД, новый движок/план) и ставит часы. */
export async function reset(request: APIRequestContext, opts: ResetOptions = {}): Promise<ClockInfo> {
  return json(await request.post("/__test/reset", { data: { now: DEFAULT_NOW, ...opts } }));
}

export async function setClock(request: APIRequestContext, now: string): Promise<ClockInfo> {
  return json(await request.post("/__test/clock", { data: { now } }));
}

/** Сдвигает часы вперёд; сервер сразу делает Tick, так что истёкшие сессии завершены к моменту ответа. */
export async function advance(request: APIRequestContext, seconds: number): Promise<ClockInfo> {
  return json(await request.post("/__test/clock", { data: { advance_seconds: seconds } }));
}

export async function getClock(request: APIRequestContext): Promise<ClockInfo> {
  return json(await request.get("/__test/clock"));
}

export async function tasksLog(request: APIRequestContext): Promise<TasksLogEntry[]> {
  return json(await request.get("/__test/tasks-log"));
}

export async function putTasks(request: APIRequestContext, fixture: unknown): Promise<void> {
  const res = await request.put("/__test/tasks", { data: fixture });
  if (!res.ok()) throw new Error(`PUT /__test/tasks -> ${res.status()}: ${await res.text()}`);
}

export async function getState(request: APIRequestContext): Promise<PomodoroState> {
  return json(await request.get("/api/v1/rpc/state"));
}

export async function getSettings(request: APIRequestContext): Promise<Record<string, unknown>> {
  return json(await request.get("/api/v1/settings"));
}

// ---------------------------------------------------------------------------
// Page object главного экрана. Основа — data-testid; запасные селекторы по роли
// и тексту (русские подписи) работают на сборке без testid.
// ---------------------------------------------------------------------------

export class PomodoroPage {
  readonly phaseTitle: Locator;
  readonly digits: Locator;
  readonly pausedLabel: Locator;
  readonly dayCounter: Locator;
  readonly creditHint: Locator;
  readonly nowTitle: Locator;
  readonly btnFocus: Locator;
  readonly btnBreak: Locator;
  readonly btnPause: Locator;
  readonly btnComplete: Locator;
  readonly btnStop: Locator;
  readonly navTimer: Locator;
  readonly navSettings: Locator;
  readonly sessionRows: Locator;

  constructor(readonly page: Page) {
    const byId = (id: string) => page.getByTestId(id);
    const button = (name: string) => page.getByRole("button", { name, exact: true });

    this.phaseTitle = byId("phase-title")
      .or(page.locator("p", { hasText: /^(Фокус ·|Короткий перерыв|Длинный перерыв|Готов к фокусу|Дальше )/ }))
      .first();
    this.digits = byId("timer-digits").or(page.locator("span", { hasText: /^\d\d:\d\d$/ })).first();
    this.pausedLabel = byId("paused-label").or(page.getByText("пауза", { exact: true }));
    this.dayCounter = byId("day-counter").or(page.getByText(/^Сегодня:/)).first();
    this.creditHint = byId("credit-hint").or(page.getByText(/^Завершить сейчас/)).first();
    this.nowTitle = byId("now-title").or(page.getByText(/^(Отдыхай|Свободный помидор)$/)).first();
    this.btnFocus = byId("btn-focus").or(button("Фокус"));
    this.btnBreak = byId("btn-break").or(button("Перерыв"));
    this.btnPause = byId("btn-pause").or(button("Пауза")).or(button("Продолжить"));
    this.btnComplete = byId("btn-complete").or(button("Завершить"));
    this.btnStop = byId("btn-stop").or(button("Стоп"));
    this.navTimer = byId("nav-timer").or(button("Таймер"));
    this.navSettings = byId("nav-settings").or(page.getByRole("button", { name: /Настройки/ }));
    this.sessionRows = byId("session-row").or(
      page.locator("li").filter({ has: page.getByTitle("Засчитано помидоров") }),
    );
  }

  async open(): Promise<void> {
    await this.page.goto("/");
    await expect(this.digits).toBeVisible();
  }

  /** Остаток на циферблате в секундах. */
  async seconds(): Promise<number> {
    const text = (await this.digits.textContent()) ?? "";
    const m = /^(\d+):(\d\d)$/.exec(text.trim());
    if (!m) throw new Error(`unexpected digits: ${JSON.stringify(text)}`);
    return Number(m[1]) * 60 + Number(m[2]);
  }

  /** Клик «Фокус» и ожидание, пока циферблат покажет идущую сессию (UI узнаёт о ней из опроса состояния). */
  async startFocus(): Promise<void> {
    await this.btnFocus.click();
    await expect(this.phaseTitle).toContainText("Фокус");
    await expect(this.btnStop).toBeVisible();
  }

  async startBreak(): Promise<void> {
    await this.btnBreak.click();
    await expect(this.btnStop).toBeVisible();
  }

  creditBadge(row: Locator): Locator {
    return row.getByTestId("session-credit").or(row.getByTitle("Засчитано помидоров"));
  }

  async gotoSettings(): Promise<void> {
    await this.navSettings.click();
    await expect(this.page.getByRole("heading", { name: "Длительности" })).toBeVisible();
  }

  async gotoTimer(): Promise<void> {
    await this.navTimer.click();
    await expect(this.digits).toBeVisible();
  }
}
