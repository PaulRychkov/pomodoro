import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import {
  NO_TASKS,
  PomodoroPage,
  advance,
  getSettings,
  getState,
  reset,
  type ResetOptions,
} from "./helpers";

// Таймерные сценарии не зависят от плана дня: задач нет, длительности — из настроек
// (фокус 25 мин, короткий перерыв 5, длинный 15, блоки дня 4+4).
async function boot(request: APIRequestContext, page: Page, opts: ResetOptions = {}): Promise<PomodoroPage> {
  await reset(request, { tasks: NO_TASKS, ...opts });
  const ui = new PomodoroPage(page);
  await ui.open();
  return ui;
}

/** Фокус до конца по часам сервера: старт, прыжок за плановую длительность, ожидание idle в UI. */
async function completeFocus(request: APIRequestContext, ui: PomodoroPage): Promise<void> {
  await ui.startFocus();
  const { planned_seconds } = await getState(request);
  await advance(request, planned_seconds + 2);
  await expect(ui.btnFocus).toBeVisible();
}

test.describe("фокус", () => {
  test("старт: циферблат идёт вниз, заголовок показывает фокус", async ({ page, request }) => {
    const ui = await boot(request, page);

    await expect(ui.phaseTitle).toContainText("Готов к фокусу");
    await expect(ui.digits).toHaveText("25:00");

    await ui.startFocus();
    await expect(ui.phaseTitle).toContainText("Фокус");
    await expect(ui.phaseTitle).toContainText("помидор 1");

    const first = await ui.seconds();
    expect(first).toBeLessThanOrEqual(1500);
    expect(first).toBeGreaterThan(1490);
    await expect.poll(() => ui.seconds(), { timeout: 5_000 }).toBeLessThan(first);

    const st = await getState(request);
    expect(st.phase).toBe("focus");
    expect(st.paused).toBe(false);
    expect(st.planned_seconds).toBe(1500);
    await expect(ui.creditHint).toContainText("не засчитается");
  });

  test("пауза замораживает циферблат, «Продолжить» возобновляет, паузу сервер не считает", async ({
    page,
    request,
  }) => {
    const ui = await boot(request, page);
    await ui.startFocus();

    await ui.btnPause.click();
    await expect(ui.pausedLabel).toBeVisible();
    await expect(ui.btnPause).toContainText("Продолжить");
    expect((await getState(request)).paused).toBe(true);

    const frozen = await ui.seconds();
    await page.waitForTimeout(2_500);
    expect(await ui.seconds()).toBe(frozen);

    // Пока идёт пауза, часы сервера уезжают на две минуты — остаток не должен просесть.
    await advance(request, 120);
    await page.waitForTimeout(1_500);
    expect(await ui.seconds()).toBe(frozen);

    await ui.btnPause.click();
    await expect(ui.pausedLabel).toBeHidden();
    await expect(ui.btnPause).toContainText("Пауза");
    await expect.poll(() => ui.seconds(), { timeout: 5_000 }).toBeLessThan(frozen);
    expect(await ui.seconds()).toBeGreaterThan(frozen - 15);

    const st = await getState(request);
    expect(st.paused).toBe(false);
    expect(st.paused_total_seconds).toBeGreaterThanOrEqual(120);
    expect(st.remaining_seconds).toBeGreaterThan(frozen - 15);
  });

  test("«Завершить» раньше срока засчитывает долю помидора", async ({ page, request }) => {
    const ui = await boot(request, page);
    await ui.startFocus();

    // Ровно половина фокуса по часам сервера.
    await advance(request, 750);
    await expect(ui.creditHint).toContainText("½");

    await ui.btnComplete.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.phaseTitle).toContainText("Дальше короткий перерыв");
    await expect(ui.dayCounter).toContainText("Сегодня: 1 из");
    await expect(ui.dayCounter).toContainText(/засчитано 0[,.]5/);

    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.creditBadge(ui.sessionRows.first())).toHaveText("½");

    const st = await getState(request);
    expect(st.completed_today).toBe(1);
    expect(st.credit_today).toBe(0.5);
    expect(st.next_phase).toBe("short_break");
  });

  test("«Завершить» сразу после старта ничего не засчитывает", async ({ page, request }) => {
    const ui = await boot(request, page);
    await ui.startFocus();
    await expect(ui.creditHint).toContainText("не засчитается");

    await ui.btnComplete.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.phaseTitle).toContainText("Готов к фокусу");
    await expect(ui.dayCounter).toContainText("Сегодня: 0 из");

    const st = await getState(request);
    expect(st.completed_today).toBe(0);
    expect(st.credit_today).toBe(0);
    expect(st.next_phase).toBe("focus");
  });

  test("«Стоп» бросает помидор без зачёта", async ({ page, request }) => {
    const ui = await boot(request, page);
    await ui.startFocus();

    await advance(request, 300);
    await expect(ui.creditHint).toContainText("¼");

    await ui.btnStop.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.phaseTitle).toContainText("Готов к фокусу");
    await expect(ui.dayCounter).toContainText("Сегодня: 0 из");
    await expect(ui.dayCounter).not.toContainText("засчитано");

    const st = await getState(request);
    expect(st.completed_today).toBe(0);
    expect(st.credit_today).toBe(0);
    expect(st.next_phase).toBe("focus");

    // Брошенный помидор попадает в список «Помидоры сегодня», но с нулевым зачётом.
    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.creditBadge(ui.sessionRows.first())).toHaveText("0");
  });

  test("часы сервера за концом фокуса: помидор завершается сам, дальше — перерыв", async ({ page, request }) => {
    const ui = await boot(request, page);
    await completeFocus(request, ui);

    await expect(ui.phaseTitle).toContainText("Дальше короткий перерыв");
    await expect(ui.dayCounter).toContainText("Сегодня: 1 из");
    await expect(ui.dayCounter).not.toContainText("засчитано");

    const st = await getState(request);
    expect(st.phase).toBe("idle");
    expect(st.next_phase).toBe("short_break");
    expect(st.completed_today).toBe(1);
    expect(st.credit_today).toBe(1);

    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.creditBadge(ui.sessionRows.first())).toHaveText("1");
  });
});

test.describe("перерыв", () => {
  test("после фокуса: старт короткого перерыва и «Стоп»", async ({ page, request }) => {
    const ui = await boot(request, page);
    await completeFocus(request, ui);

    await ui.startBreak();
    await expect(ui.phaseTitle).toHaveText("Короткий перерыв");
    await expect(ui.nowTitle).toHaveText("Отдыхай");
    await expect(ui.btnComplete).toHaveCount(0);
    const left = await ui.seconds();
    expect(left).toBeLessThanOrEqual(300);
    expect(left).toBeGreaterThan(290);
    expect((await getState(request)).phase).toBe("short_break");

    await ui.btnStop.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.phaseTitle).toContainText("Готов к фокусу");
    expect((await getState(request)).phase).toBe("idle");
    // Остановка перерыва не трогает счётчик дня.
    await expect(ui.dayCounter).toContainText("Сегодня: 1 из");
  });

  test("перерыв из чистого состояния — короткий", async ({ page, request }) => {
    const ui = await boot(request, page);
    await ui.startBreak();
    await expect(ui.phaseTitle).toHaveText("Короткий перерыв");
    await ui.btnStop.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.dayCounter).toContainText("Сегодня: 0 из");
  });

  test("последний помидор блока открывает длинный перерыв", async ({ page, request }) => {
    const ui = await boot(request, page, { settings: { day_blocks: [1, 2] } });
    await completeFocus(request, ui);

    await expect(ui.phaseTitle).toContainText("Дальше длинный перерыв");
    await ui.startBreak();
    await expect(ui.phaseTitle).toHaveText("Длинный перерыв");
    const left = await ui.seconds();
    expect(left).toBeLessThanOrEqual(900);
    expect(left).toBeGreaterThan(890);
  });

  test("перерыв заканчивается по часам сервера, следующий шаг — фокус", async ({ page, request }) => {
    const ui = await boot(request, page);
    await completeFocus(request, ui);
    await ui.startBreak();

    await advance(request, 302);
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.phaseTitle).toContainText("Готов к фокусу");
    const st = await getState(request);
    expect(st.phase).toBe("idle");
    expect(st.next_phase).toBe("focus");
    expect(st.completed_today).toBe(1);
  });
});

test.describe("автостарт", () => {
  test("auto_start_break: перерыв начинается сразу после фокуса, фокус — после перерыва", async ({
    page,
    request,
  }) => {
    const ui = await boot(request, page, { settings: { auto_start_break: true, auto_start_focus: true } });
    await ui.startFocus();
    await advance(request, 1502);

    await expect(ui.phaseTitle).toHaveText("Короткий перерыв");
    expect((await getState(request)).phase).toBe("short_break");

    await advance(request, 302);
    await expect(ui.phaseTitle).toContainText("Фокус");
    await expect(ui.phaseTitle).toContainText("помидор 2");
    const st = await getState(request);
    expect(st.phase).toBe("focus");
    expect(st.completed_today).toBe(1);
  });
});

test.describe("список «Помидоры сегодня»", () => {
  test("показывает завершённые сессии и растёт после каждого помидора", async ({ page, request }) => {
    const ui = await boot(request, page);
    await expect(ui.sessionRows).toHaveCount(0);

    await completeFocus(request, ui);
    await expect(ui.sessionRows).toHaveCount(1);

    await ui.startBreak();
    await ui.btnStop.click();
    await expect(ui.btnFocus).toBeVisible();

    await completeFocus(request, ui);
    await expect(ui.sessionRows).toHaveCount(2);
    for (const row of await ui.sessionRows.all()) {
      await expect(ui.creditBadge(row)).toHaveText("1");
    }
    await expect(ui.dayCounter).toContainText("Сегодня: 2 из");
    await expect(ui.phaseTitle).toContainText("Дальше");
  });
});

test.describe("настройки", () => {
  test("навигация туда-обратно и сохранение длительности фокуса", async ({ page, request }) => {
    const ui = await boot(request, page);

    await ui.gotoSettings();
    const focus = page
      .getByTestId("set-focus")
      .or(page.locator("section").filter({ has: page.getByRole("heading", { name: "Длительности" }) }).locator("input[type=number]").first());
    await expect(focus).toHaveValue("25");
    await focus.fill("20");
    await page
      .getByTestId("settings-save")
      .or(page.getByRole("button", { name: "Сохранить настройки" }))
      .click();
    await expect(page.getByText("Сохранено")).toBeVisible();
    expect((await getSettings(request)).focus_duration_seconds).toBe(1200);

    await ui.gotoTimer();
    await expect(ui.digits).toHaveText("20:00");

    // Настройка пережила перезагрузку страницы и действует на новый помидор.
    await page.reload();
    await expect(ui.digits).toHaveText("20:00");
    await ui.startFocus();
    expect((await getState(request)).planned_seconds).toBe(1200);
  });
});
