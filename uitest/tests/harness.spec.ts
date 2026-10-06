import { expect, test, type APIRequestContext } from "@playwright/test";
import {
  advance,
  getClock,
  getSettings,
  getState,
  putTasks,
  reset,
  setClock,
  tasksLog,
  type TasksLogEntry,
} from "./helpers";

test.skip(({ isMobile }) => isMobile, "API-тесты стенда не зависят от вьюпорта");

const TASKS = "/__tasks/api/v1";

test.describe("управление часами", () => {
  test("reset ставит время суток сегодняшнего дня и отдаёт состояние", async ({ request }) => {
    const info = await reset(request, { now: "05:50" });
    expect(info.now).toMatch(/T05:5[01]:/);
    expect(info.now).toMatch(/\+05:00$/);
    expect(info.state?.phase).toBe("idle");
    expect(info.state?.completed_today).toBe(0);
  });

  test("полная дата в tz сервера и RFC3339 с зоной", async ({ request }) => {
    expect((await setClock(request, "2026-10-05T09:15:00")).now).toMatch(/^2026-10-05T09:15:0\d/);
    expect((await setClock(request, "2026-10-05T12:00:00Z")).now).toMatch(/^2026-10-05T17:00:0\d.*\+05:00$/);
  });

  test("advance сдвигает часы, время при этом идёт само", async ({ request }) => {
    await reset(request, { now: "10:00" });
    await advance(request, 600);
    const clock = await getClock(request);
    expect(clock.now).toMatch(/T10:10:0\d/);
    await new Promise((r) => setTimeout(r, 1_200));
    const later = Date.parse((await getClock(request)).now) - Date.parse(clock.now);
    expect(later).toBeGreaterThanOrEqual(1_000);
  });

  test("advance сразу завершает истёкшую сессию (Tick в ответе)", async ({ request }) => {
    await reset(request, { settings: { focus_duration_seconds: 600 } });
    const started = await request.post("/api/v1/rpc/start-focus", { data: {} });
    expect(started.ok()).toBe(true);
    expect((await getState(request)).phase).toBe("focus");

    const info = await advance(request, 601);
    expect(info.state?.phase).toBe("idle");
    expect(info.state?.completed_today).toBe(1);
    expect(info.state?.next_phase).toBe("short_break");
  });
});

test.describe("reset", () => {
  test("сносит БД: сессии и состояние обнуляются", async ({ request }) => {
    await reset(request);
    await request.post("/api/v1/rpc/start-focus", { data: {} });
    await advance(request, 1501);
    expect((await getState(request)).completed_today).toBe(1);
    expect(await (await request.get("/api/v1/rpc/sessions-today")).json()).toHaveLength(1);

    await reset(request);
    expect((await getState(request)).completed_today).toBe(0);
    expect(await (await request.get("/api/v1/rpc/sessions-today")).json()).toHaveLength(0);
  });

  test("настройки мёржатся поверх умолчаний, overlay — по полям", async ({ request }) => {
    await reset(request, { settings: { focus_duration_seconds: 600, overlay: { size: 200 } } });
    const s = await getSettings(request);
    expect(s.focus_duration_seconds).toBe(600);
    expect(s.short_break_seconds).toBe(300);
    expect(s.overlay).toMatchObject({ size: 200, digits_size: 32 });
    expect((await getState(request)).next_planned_seconds).toBe(600);
  });

  test("неверный ввод даёт 400 и не ломает работающий стек", async ({ request }) => {
    await reset(request);
    const badTime = await request.post("/__test/reset", { data: { now: "вчера" } });
    expect(badTime.status()).toBe(400);
    const badSettings = await request.post("/__test/reset", { data: { settings: { focus_duration_seconds: 0 } } });
    expect(badSettings.status()).toBe(400);
    expect((await badSettings.json()).error).toContain("durations must be positive");
    expect((await request.get("/healthz")).ok()).toBe(true);
  });

  test("фиксируется время сервера и фикстура сбрасывается к умолчанию", async ({ request }) => {
    await reset(request, { tasks: [{ id: "only", title: "Одна" }] });
    expect(await tasksJSON(request, "tasks")).toHaveLength(1);
    await reset(request);
    expect((await tasksJSON(request, "tasks")).length).toBeGreaterThan(5);
  });
});

test.describe("фейковый task-planner", () => {
  test("occurrences: вхождения на день со встроенной задачей, фильтр по статусу", async ({ request }) => {
    await reset(request, { now: "2026-10-05T09:00:00" });
    const res = await request.get(`${TASKS}/occurrences?from=2026-10-05&to=2026-10-05&status=pending`);
    const occs = (await res.json()) as { id: string; task_id: string; date: string; status: string; task: Record<string, unknown> }[];
    const titles = occs.map((o) => o.task.title);
    expect(titles).toEqual(
      expect.arrayContaining(["Сборы в зал", "Зал", "Дорога из зала", "Обед", "Работа", "Поиск работы", "Английский"]),
    );
    const work = occs.find((o) => o.task_id === "win-work");
    expect(work).toMatchObject({
      date: "2026-10-05",
      status: "pending",
      task: { start_time_minutes: 600, estimated_duration_minutes: 540, requires_pomodoro: true, priority: 3 },
    });
    const gym = occs.find((o) => o.task_id === "ev-gym");
    expect(gym?.task).toMatchObject({ start_time_minutes: 420, requires_pomodoro: false });
    const search = occs.find((o) => o.task_id === "job-search");
    expect(search?.task).toMatchObject({ start_time_minutes: null, effort_minutes: 120, priority: 3 });
  });

  test("complete и progress пишутся в журнал, выполненное уходит из pending", async ({ request }) => {
    await reset(request, { now: "2026-10-05T09:00:00" });
    const id = "occ_2026-10-05_job-search";
    expect((await request.post(`${TASKS}/occurrences/${id}/progress`, { data: { minutes: 25 } })).ok()).toBe(true);
    expect((await request.post(`${TASKS}/occurrences/${id}/complete`, { data: {} })).ok()).toBe(true);
    expect((await request.post(`${TASKS}/occurrences/occ_2026-10-05_nope/complete`, { data: {} })).status()).toBe(404);

    const log = await tasksLog(request);
    expect(log.map((e) => [e.kind, e.task_id, e.minutes ?? 0])).toEqual([
      ["progress", "job-search", 25],
      ["complete", "job-search", 0],
    ]);

    const pending = await (await request.get(`${TASKS}/occurrences?from=2026-10-05&to=2026-10-05&status=pending`)).json();
    expect(pending.map((o: { task_id: string }) => o.task_id)).not.toContain("job-search");
    const done = await (await request.get(`${TASKS}/occurrences?from=2026-10-05&to=2026-10-05&status=completed`)).json();
    expect(done.map((o: { task_id: string }) => o.task_id)).toEqual(["job-search"]);
  });

  test("PUT /__test/tasks подменяет фикстуру, не трогая журнал", async ({ request }) => {
    await reset(request);
    await request.post(`${TASKS}/occurrences/occ_2026-10-05_typing/complete`, { data: {} });
    await putTasks(request, { tasks: [{ id: "x", title: "Икс", effort_minutes: 30, priority: 1 }] });
    expect(await tasksJSON(request, "tasks")).toHaveLength(1);
    expect(await tasksLog(request)).toHaveLength(1);
    expect((await request.delete("/__test/tasks-log")).ok()).toBe(true);
    expect(await tasksLog(request)).toHaveLength(0);
  });

  test("план дня строится из задач фейкового planner через tasksclient", async ({ request }) => {
    await reset(request);
    const slots = (await (await request.get("/api/v1/rpc/day-plan")).json()) as {
      task: { title_snapshot: string } | null;
      window_title: string | null;
    }[];
    const titles = new Set(slots.map((s) => s.task?.title_snapshot));
    expect(titles).toContain("Работа");
    expect(titles).toContain("Поиск работы");
    expect(slots.some((s) => s.window_title === "Работа")).toBe(true);
    expect(titles).not.toContain("Зал");
    expect(titles).not.toContain("Обед");

    await reset(request, { tasks: { tasks: [] } });
    const empty = (await (await request.get("/api/v1/rpc/day-plan")).json()) as { task: unknown }[];
    expect(empty.every((s) => s.task === null)).toBe(true);
  });

  test("клиент прошлого стека после reset получает 410", async ({ request }) => {
    await reset(request);
    const res = await request.post(`/__tasks/g0/api/v1/occurrences/occ_2026-10-05_typing/complete`, { data: {} });
    expect(res.status()).toBe(410);
    expect(await tasksLog(request)).toHaveLength(0);
  });

  test("помидор по задаче плана: по завершении в planner уходит прогресс", async ({ request }) => {
    await reset(request, {
      tasks: [
        {
          id: "solo",
          title: "Единственная",
          progress: "pending",
          is_active: true,
          effort_minutes: 25,
          priority: 2,
          requires_pomodoro: true,
        },
      ],
    });
    const started = await request.post("/api/v1/rpc/start-focus", {
      data: { task: { source: "tasks", external_id: "solo", title_snapshot: "Единственная" } },
    });
    expect(started.ok()).toBe(true);
    await advance(request, 1501);

    await expect
      .poll(async () => (await tasksLog(request)).map((e: TasksLogEntry) => e.kind), { timeout: 10_000 })
      .toContain("progress");
    const progress = (await tasksLog(request)).find((e) => e.kind === "progress");
    expect(progress).toMatchObject({ task_id: "solo", minutes: 25 });
  });
});

async function tasksJSON(request: APIRequestContext, what: string): Promise<unknown[]> {
  return (await request.get(`${TASKS}/${what}`)).json();
}
