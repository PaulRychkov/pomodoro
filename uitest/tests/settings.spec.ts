import { expect, test, type Page } from "@playwright/test";
import { PomodoroPage, advance, getClock, getSettings, getState, setClock } from "./helpers";
import {
  WORK_ID,
  allSlots,
  boot,
  committed,
  expectNoHorizontalScroll,
  expectPlanValid,
  isWork,
  readPlan,
  reloadPlan,
  saveSettings,
  slotAt,
  toMinutes,
  waitForPlan,
  windowGroups,
} from "./plan-helpers";

const mm = (minutes: number) => `${String(minutes).padStart(2, "0")}:00`;

/** Возврат на таймер после настроек: PlanToday монтируется заново и читает свежий план. */
async function backToTimer(ui: PomodoroPage): Promise<void> {
  await ui.gotoTimer();
  await waitForPlan(ui.page);
}

async function setSwitch(page: Page, testid: string, on: boolean): Promise<void> {
  const toggle = page.getByTestId(testid);
  if ((await toggle.getAttribute("data-checked")) !== String(on)) await toggle.click();
  await expect(toggle).toHaveAttribute("data-checked", String(on));
}

test.describe("границы дня", () => {
  test("07:00–18:00: ни один помидор не выходит за активный день, вечерней группы нет", async ({ page, request }, info) => {
    const ui = await boot(request, page);
    // До правки в плане есть вечерняя группа 19:00–20:00 и утренняя 06:00–06:40.
    const before = await readPlan(page);
    expect(before.some((g) => g.periodStart === toMinutes("19:00"))).toBe(true);

    await ui.gotoSettings();
    await expect(page.getByTestId("set-day-start")).toHaveValue("06:00");
    await expect(page.getByTestId("set-day-end")).toHaveValue("20:00");
    await page.getByTestId("set-day-start").fill("07:00");
    await page.getByTestId("set-day-end").fill("18:00");
    await expect(page.getByTestId("settings-day-error")).toHaveCount(0);
    await expectNoHorizontalScroll(page, info);
    await saveSettings(page);
    const saved = await getSettings(request);
    expect(saved.day_start_minutes).toBe(7 * 60);
    expect(saved.day_end_minutes).toBe(18 * 60);

    await backToTimer(ui);
    const groups = await readPlan(page);
    expectPlanValid(groups, { dayStart: 7 * 60, dayEnd: 18 * 60, noAdjacentOthers: true });
    for (const s of committed(groups)) {
      expect(s.start as number).toBeGreaterThanOrEqual(7 * 60);
      expect(s.end as number).toBeLessThanOrEqual(18 * 60);
    }
    expect(groups.some((g) => g.periodStart === toMinutes("19:00")), "вечерняя группа 19:00–20:00 исчезла").toBe(false);
    expect(groups.some((g) => g.periodEnd === toMinutes("06:40")), "утренняя группа 06:00–06:40 исчезла").toBe(false);
    // «Зал» и «Дорога» вырезают 07:00–08:00: утро начинается в 08:00, второе окно закрывается в 18:00.
    expect(groups.filter((g) => g.kind === "period").map((g) => [g.periodStart, g.periodEnd, g.windowId])).toEqual([
      [toMinutes("08:00"), toMinutes("10:00"), ""],
      [toMinutes("10:00"), toMinutes("14:00"), WORK_ID],
      [toMinutes("15:00"), toMinutes("18:00"), WORK_ID],
    ]);
    await expect(page.getByTestId("next-start")).toContainText("по плану в 08:00");

    // Границы дня переживают перезагрузку.
    await reloadPlan(page);
    expectPlanValid(await readPlan(page), { dayStart: 7 * 60, dayEnd: 18 * 60, noAdjacentOthers: true });
  });

  test("конец дня раньше начала + 30 минут: ошибка и заблокированное сохранение", async ({ page, request }, info) => {
    const ui = await boot(request, page);
    await ui.gotoSettings();
    const save = page.getByTestId("settings-save");
    const error = page.getByTestId("settings-day-error");

    await page.getByTestId("set-day-start").fill("07:00");
    await page.getByTestId("set-day-end").fill("07:20");
    await expect(error).toBeVisible();
    await expect(error).toContainText("минимум на 30 минут");
    await expect(save).toBeDisabled();
    await expect(page.getByTestId("set-day-start")).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByTestId("set-day-end")).toHaveAttribute("aria-invalid", "true");
    await expectNoHorizontalScroll(page, info);
    // Ничего не ушло на сервер.
    const stored = await getSettings(request);
    expect(stored.day_start_minutes).toBe(6 * 60);
    expect(stored.day_end_minutes).toBe(20 * 60);

    // Ровно 30 минут — допустимо.
    await page.getByTestId("set-day-end").fill("07:30");
    await expect(error).toHaveCount(0);
    await expect(save).toBeEnabled();
    // Конец раньше начала — тоже ошибка.
    await page.getByTestId("set-day-end").fill("06:00");
    await expect(error).toBeVisible();
    await expect(save).toBeDisabled();

    // Исправили (07:00–19:00) — сохраняется.
    await page.getByTestId("set-day-end").fill("19:00");
    await expect(error).toHaveCount(0);
    await saveSettings(page);
    expect((await getSettings(request)).day_end_minutes).toBe(19 * 60);
  });
});

test.describe("доля помидоров окна", () => {
  const shares: Array<[number, number]> = [
    [100, 16],
    [80, 13],
    [50, 8],
    [0, 0],
  ];
  for (const [share, expectedWork] of shares) {
    test(`window_share_percent=${share}: «Работа» получает ${expectedWork} из 16 помидоров окна`, async ({
      page,
      request,
    }) => {
      const ui = await boot(request, page);
      await ui.gotoSettings();
      await page.getByTestId("set-share").fill(String(share));
      await saveSettings(page);
      expect((await getSettings(request)).window_share_percent).toBe(share);
      await backToTimer(ui);

      const groups = await readPlan(page);
      expectPlanValid(groups, { noAdjacentOthers: share >= 50 });
      const wins = windowGroups(groups);
      const total = wins.reduce((n, g) => n + g.slots.length, 0);
      const work = allSlots(groups).filter(isWork).length;
      expect(total).toBe(16);
      expect(work).toBe(expectedWork);
      if (share === 100) {
        for (const g of wins) for (const s of g.slots) expect(s.task, `в окне ${g.title}`).toBe("Работа");
      }
      if (share === 0) {
        expect(allSlots(groups).some(isWork), "«Работа» нигде в плане").toBe(false);
        for (const g of wins) expect(g.counter?.count).toBe(0);
        // Окно остаётся окном: помидоры в нём есть и заняты другими задачами.
        for (const g of wins) expect(g.slots.length).toBeGreaterThan(0);
      }
      // Свободное время не затронуто настройкой окна: «Работа» вне окна не появляется.
      for (const g of groups.filter((x) => x.kind === "period" && !x.windowId)) {
        expect(g.slots.some(isWork)).toBe(false);
      }
    });
  }
});

test.describe("длительности и блоки", () => {
  test("фокус 30 минут: циферблат и план перестраиваются, помидоры не длиннее 30", async ({ page, request }) => {
    const ui = await boot(request, page);
    const before = committed(await readPlan(page));
    expect(Math.max(...before.map((s) => Number(s.focus)))).toBeLessThanOrEqual(25);

    await ui.gotoSettings();
    await expect(page.getByTestId("set-focus")).toHaveValue("25");
    await page.getByTestId("set-focus").fill("30");
    await saveSettings(page);
    expect((await getSettings(request)).focus_duration_seconds).toBe(1800);
    await backToTimer(ui);

    const groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
    const slots = committed(groups);
    const focuses = slots.map((s) => Number(s.focus));
    expect(Math.max(...focuses), "ни один помидор не длиннее настроенных 30 минут").toBeLessThanOrEqual(30);
    expect(Math.max(...focuses), "длинные периоды используют новые 30 минут").toBeGreaterThan(25);
    expect(Math.min(...focuses), "помидор ужимается не ниже 2/3 от 30").toBeGreaterThanOrEqual(20);
    for (const s of slots) expect(s.end! - s.start!).toBe(Number(s.focus));

    // Циферблат в простое показывает первый слот нового плана.
    await expect(ui.digits).toHaveText(mm(focuses[0]));
    await reloadPlan(page);
    await expect(ui.digits).toHaveText(mm(focuses[0]));
    // Фокус стартует с этой длиной.
    await ui.startFocus();
    expect((await getState(request)).planned_seconds).toBe(focuses[0] * 60);
  });

  test("перерывы 8 и 20 минут попадают в план", async ({ page, request }) => {
    const ui = await boot(request, page);
    await ui.gotoSettings();
    await page.getByTestId("set-short").fill("8");
    await page.getByTestId("set-long").fill("20");
    await saveSettings(page);
    const saved = await getSettings(request);
    expect(saved.short_break_seconds).toBe(480);
    expect(saved.long_break_seconds).toBe(1200);
    await backToTimer(ui);

    const groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
    const breaks = committed(groups).map((s) => Number(s.brk));
    expect(Math.min(...breaks), "перерывы не короче настроенного короткого").toBeGreaterThanOrEqual(8);
    expect(breaks.some((b) => b >= 20), "длинный перерыв в плане").toBe(true);
  });

  test("блоки: добавить и удалить блок, сохранить без ошибок", async ({ page, request }, info) => {
    const ui = await boot(request, page);
    await ui.gotoSettings();
    const blocks = page.getByTestId("set-block");
    await expect(blocks).toHaveCount(1);
    await expect(blocks.first()).toHaveValue("3");
    // Единственный блок удалить нельзя.
    await expect(page.getByTestId("set-block-remove")).toBeDisabled();

    await page.getByTestId("set-block-add").click();
    await expect(blocks).toHaveCount(2);
    await expect(blocks.nth(1)).toHaveValue("4");
    await blocks.nth(1).fill("5");
    await saveSettings(page);
    expect((await getSettings(request)).day_blocks).toEqual([3, 5]);
    await expectNoHorizontalScroll(page, info);

    // Удаляем первый блок: остаётся [5], длинный перерыв после каждого пятого помидора.
    await page.locator('[data-testid="set-block-remove"][data-index="0"]').click();
    await expect(blocks).toHaveCount(1);
    await expect(blocks.first()).toHaveValue("5");
    await saveSettings(page);
    expect((await getSettings(request)).day_blocks).toEqual([5]);

    await backToTimer(ui);
    await expect(page.getByTestId("app-error")).toHaveCount(0);
    expectPlanValid(await readPlan(page), { noAdjacentOthers: true });
    // Настройка переживает перезагрузку.
    await page.reload();
    await ui.gotoSettings();
    await expect(page.getByTestId("set-block")).toHaveCount(1);
    await expect(page.getByTestId("set-block").first()).toHaveValue("5");
  });
});

test.describe("автостарт", () => {
  test("перерыв и следующий фокус стартуют сами, длительности — из слотов плана", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    const plan0 = committed(await readPlan(page));
    const breakMin = Number(plan0[0].brk);

    await ui.gotoSettings();
    await setSwitch(page, "set-autostart-break", true);
    await setSwitch(page, "set-autostart-focus", true);
    await saveSettings(page);
    const saved = await getSettings(request);
    expect(saved.auto_start_break).toBe(true);
    expect(saved.auto_start_focus).toBe(true);
    await backToTimer(ui);

    await ui.startFocus();
    const focusSeconds = (await getState(request)).planned_seconds;
    await advance(request, focusSeconds + 2);

    // Перерыв пошёл сам, длиной из слота.
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", /^(short|long)_break$/);
    await expect(page.getByTestId("now-title")).toHaveAttribute("data-kind", "break");
    const st = await getState(request);
    expect(st.planned_seconds).toBe(breakMin * 60);
    expect(st.completed_today).toBe(1);

    // Перерыв закончился — стартовал помидор №2 со своей длиной из плана.
    await advance(request, breakMin * 60 + 2);
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "focus");
    await expect(page.getByTestId("phase-title")).toContainText("помидор 2");
    const st2 = await getState(request);
    expect(st2.phase).toBe("focus");
    const slot1Focus = Number(await slotAt(page, 1).getByTestId("slot-focus").inputValue());
    expect(slot1Focus).toBeGreaterThan(0);
    expect(st2.planned_seconds).toBe(slot1Focus * 60);
    await expect(slotAt(page, 1)).toHaveAttribute("data-active", "true");
  });

  test("без автостарта после фокуса ждём ручного запуска перерыва", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    await ui.startFocus();
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect(ui.btnFocus).toBeVisible();
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "idle");
    // Прошло много времени — всё равно ничего само не стартует.
    await advance(request, 600);
    await page.waitForTimeout(1_500);
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "idle");
    expect((await getState(request)).phase).toBe("idle");
  });
});

test.describe("пресеты помидорного дня", () => {
  test("сохранить план как пресет, назначить на сегодняшний день недели, план строится по его длительностям, удалить", async ({
    page,
    request,
  }, info) => {
    const ui = await boot(request, page);
    const plan0 = committed(await readPlan(page));
    const presetFocus = plan0.map((s) => Number(s.focus));
    const presetBreak = plan0.map((s) => Number(s.brk));
    expect(plan0.length).toBeGreaterThan(10);
    const today = (await getClock(request)).now.slice(0, 10);
    const weekday = ((new Date(`${today}T00:00:00Z`).getUTCDay() + 6) % 7) + 1;
    const todaySelect = page.locator(`[data-testid="preset-weekday"][data-weekday="${weekday}"]`);

    await ui.gotoSettings();
    await page.getByTestId("preset-name").fill("Тест");
    await page.getByTestId("preset-save").click();
    const row = page.locator('[data-testid="preset-row"][data-name="Тест"]');
    await expect(row).toBeVisible();
    await expect(row).toContainText(`${plan0.length} 🍅`);
    await expect(page.getByTestId("preset-name")).toHaveValue("");
    await expect(page.getByTestId("preset-error")).toHaveCount(0);

    await expect(todaySelect).toHaveValue("");
    await todaySelect.selectOption({ label: "Тест" });
    await expect(todaySelect).toHaveValue("Тест");
    const schedule = await (await request.get("/api/v1/rpc/preset-schedule")).json();
    expect(schedule).toEqual([{ weekday, preset_name: "Тест" }]);
    await expectNoHorizontalScroll(page, info);

    // Опаздываем до 08:40 и перестраиваем план: раскладка идёт по длительностям пресета, но в границах периодов.
    await setClock(request, "08:40");
    await page.reload();
    await waitForPlan(page);
    await page.getByTestId("plan-refresh").click();
    await expect(page.getByTestId("plan-refresh")).toBeEnabled();
    await waitForPlan(page);

    const groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
    const slots = committed(groups);
    expect(slots.length).toBeGreaterThan(5);
    expect(slots[0].start).toBeGreaterThanOrEqual(toMinutes("08:40"));
    slots.forEach((s, i) => {
      expect(Number(s.focus), `длительность фокуса слота #${i}`).toBe(presetFocus[i]);
      expect(Number(s.brk), `длительность перерыва слота #${i}`).toBe(presetBreak[i]);
    });
    // Для сравнения: без пресета в 08:40 первый помидор был бы другой длины.
    expect(Number(slots[0].focus)).toBe(17);
    await expect(ui.digits).toHaveText(mm(17));
    // Окно по-прежнему начинается в 10:00.
    expect(windowGroups(groups)[0].slots[0].start).toBe(toMinutes("10:00"));

    // Удаление пресета: строка и назначение пропадают.
    await ui.gotoSettings();
    await expect(todaySelect).toHaveValue("Тест");
    await page.locator('[data-testid="preset-delete"][data-name="Тест"]').click();
    await expect(row).toHaveCount(0);
    await expect(todaySelect).toHaveValue("");
    await expect(page.getByTestId("preset-error")).toHaveCount(0);
    expect(await (await request.get("/api/v1/rpc/preset-schedule")).json()).toEqual([]);

    // После удаления обновление плана возвращает автораскладку.
    await backToTimer(ui);
    await page.getByTestId("plan-refresh").click();
    await expect(page.getByTestId("plan-refresh")).toBeEnabled();
    const after = await readPlan(page);
    expectPlanValid(after, { noAdjacentOthers: true });
    expect(Number(committed(after)[0].focus), "без пресета первый помидор в 08:40 длиннее").toBeGreaterThan(17);
  });
});

test.describe("звук", () => {
  async function installPlayCounter(page: Page): Promise<void> {
    await page.addInitScript(() => {
      const w = window as unknown as { __plays: string[] };
      w.__plays = [];
      HTMLMediaElement.prototype.play = function (this: HTMLMediaElement) {
        w.__plays.push(this.src);
        return Promise.resolve();
      };
    });
  }
  const plays = (page: Page) => page.evaluate(() => (window as unknown as { __plays: string[] }).__plays.slice());

  test("звук включён — конец фокуса играет сигнал, выключен — тишина", async ({ page, request }) => {
    await installPlayCounter(page);
    const ui = await boot(request, page, { now: "08:00" });
    expect(await plays(page)).toEqual([]);

    // Включаем звук явно (по умолчанию включён) и сохраняем.
    await ui.gotoSettings();
    await setSwitch(page, "set-sound", true);
    await saveSettings(page);
    expect((await getSettings(request)).sound_enabled).toBe(true);
    await ui.gotoTimer();

    await ui.startFocus();
    expect(await plays(page), "старт фокуса не звучит").toEqual([]);
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect.poll(async () => (await plays(page)).length, { timeout: 7_000 }).toBe(1);
    expect((await plays(page))[0]).toContain("/sound/default");
    await expect(ui.btnFocus).toBeVisible();

    // Выключаем звук: следующий конец фокуса — без вызова play.
    await ui.gotoSettings();
    await setSwitch(page, "set-sound", false);
    await saveSettings(page);
    expect((await getSettings(request)).sound_enabled).toBe(false);
    await ui.gotoTimer();
    const played = (await plays(page)).length;

    await ui.startFocus();
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", "2");
    await expect(ui.btnFocus).toBeVisible();
    // Опрос состояния — раз в секунду; ждём заведомо больше.
    await page.waitForTimeout(2_500);
    expect((await plays(page)).length, "звук выключен — play не вызывался").toBe(played);
  });
});
