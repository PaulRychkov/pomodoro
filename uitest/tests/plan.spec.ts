import { expect, test } from "@playwright/test";
import { advance, getState, reset, setClock } from "./helpers";
import {
  WORK_ID,
  allSlots,
  boot,
  committed,
  expectNoHorizontalScroll,
  expectPlanValid,
  fmClock,
  isWork,
  overflowGroup,
  readPlan,
  reloadPlan,
  slotAt,
  toMinutes,
  waitForPlan,
  windowGroups,
} from "./plan-helpers";

// План дня 06:00–20:00 режется на периоды: свободное время между событиями и окна («Работа»).
// Фикстура по умолчанию: «Сборы в зал» 06:40–07:00, «Зал» 07:00–07:40, «Дорога» 07:40–08:00 и «Обед» 14:00–15:00
// вырезаются (без помидоров); окно «Работа» 10:00–19:00 с помидорами; гибкие задачи — «Поиск работы» (120 мин),
// «Работа с ИИ и другое» (75), «Английский» (60), «Слепая печать» (40).

const mm = (minutes: number) => `${String(minutes).padStart(2, "0")}:00`;

test.describe("раскладка дня", () => {
  test("в 05:50 день режется на периоды, помидоры не пересекают их границы", async ({ page, request }, info) => {
    await boot(request, page);
    const groups = await readPlan(page);

    // Периоды: 06:00–06:40, 08:00–10:00, окно 10:00–14:00, окно 15:00–19:00, 19:00–20:00.
    const periods = groups
      .filter((g) => g.kind === "period")
      .map((g) => [g.periodStart, g.periodEnd, g.windowId]);
    expect(periods).toEqual([
      [toMinutes("06:00"), toMinutes("06:40"), ""],
      [toMinutes("08:00"), toMinutes("10:00"), ""],
      [toMinutes("10:00"), toMinutes("14:00"), WORK_ID],
      [toMinutes("15:00"), toMinutes("19:00"), WORK_ID],
      [toMinutes("19:00"), toMinutes("20:00"), ""],
    ]);
    const titles = await page.getByTestId("plan-group-title").allTextContents();
    expect(titles[0]).toBe("06:00–06:40 · свободное время");
    expect(titles).toContain("10:00–14:00 · Работа");
    expect(titles).toContain("15:00–19:00 · Работа");

    // Каждый слот лежит целиком в своём периоде и в активном дне (≤ 20:00), слоты идут по времени.
    expectPlanValid(groups, { noAdjacentOthers: true });
    for (const g of groups.filter((x) => x.kind === "period")) {
      expect(g.slots.length, `в группе ${g.title} есть помидоры`).toBeGreaterThan(0);
    }
    for (const s of committed(groups)) {
      expect(s.end as number).toBeLessThanOrEqual(1200);
    }

    // 67% помидоров окна — задаче окна: 11 «Работа» из 16 (считается по всему окну «10:00–19:00», не по половинкам).
    const wins = windowGroups(groups);
    expect(wins).toHaveLength(2);
    const total = wins.reduce((n, g) => n + g.slots.length, 0);
    const work = wins.reduce((n, g) => n + (g.counter?.count ?? -1000), 0);
    expect(total).toBe(16);
    expect(work).toBe(11);
    expect(wins.reduce((n, g) => n + g.slots.filter(isWork).length, 0)).toBe(11);
    // Свободные периоды вне окон — только другие задачи.
    for (const g of groups.filter((x) => x.kind === "period" && !x.windowId)) {
      expect(g.slots.some(isWork), `«Работа» в свободном периоде ${g.title}`).toBe(false);
    }

    // Что не влезло в день — в отдельной группе без времени, а не в хвосте дня.
    const over = overflowGroup(groups);
    if (over) {
      expect(over.title).toContain("Не влезли в день");
      expect(over.slots.every((s) => s.start === null && s.end === null)).toBe(true);
    }

    await expectNoHorizontalScroll(page, info);
  });

  test("в свободное время помидоры занимают только другие задачи, окно — своё время", async ({ page, request }) => {
    await boot(request, page);
    const groups = await readPlan(page);
    // Задача окна не появляется вне окна даже в overflow.
    const outside = allSlots(groups).filter((s) => isWork(s) && s.windowId !== WORK_ID);
    expect(outside).toEqual([]);
    // Слот окна знает своё окно.
    for (const g of windowGroups(groups)) {
      for (const s of g.slots) expect(s.windowId).toBe(WORK_ID);
    }
    // Часы и точное время слотов: подпись slot-start совпадает с data-start.
    for (const s of committed(groups)) expect(s.startText).toBe(fmClock(s.start as number));
  });
});

test.describe("циферблат в простое", () => {
  test("показывает длину ближайшего слота из плана, а не 25:00 из настроек", async ({ page, request }) => {
    const ui = await boot(request, page);
    const first = committed(await readPlan(page))[0];
    const focusMin = Number(first.focus);
    expect(focusMin, "у слота плана есть своя длительность").toBeGreaterThan(0);
    expect(focusMin, "первый период 06:00–06:40 короче двух стандартных помидоров").toBeLessThan(25);

    await expect(ui.digits).toHaveText(mm(focusMin));
    await expect(page.getByText("25:00")).toHaveCount(0);
    await expect(page.getByTestId("phase-title")).toContainText("Готов к фокусу");
    await expect(page.getByTestId("next-start")).toContainText("по плану в 06:00");
    await expect(page.getByTestId("next-start")).toHaveAttribute("data-start", String(first.start));
    await expect(page.getByTestId("now-title")).toHaveAttribute("data-kind", "next");
    await expect(page.getByTestId("now-title")).toHaveText(first.task);

    const st = (await getState(request)) as { next_planned_seconds?: number };
    expect(st.next_planned_seconds).toBe(focusMin * 60);
    // Для сравнения: в настройках — стандартные 25 минут.
    const settings = await (await request.get("/api/v1/settings")).json();
    expect(settings.focus_duration_seconds).toBe(1500);
  });
});

test.describe("опоздание", () => {
  test("в 08:40 план пересчитывается от «сейчас», но «Работа» всё равно начинается в 10:00", async ({
    page,
    request,
  }) => {
    await boot(request, page);
    await setClock(request, "08:40");
    await reloadPlan(page);
    const groups = await readPlan(page);

    const slots = committed(groups);
    const firstOpen = slots.find((s) => !s.done);
    expect(firstOpen, "есть несделанный слот").toBeDefined();
    // Утро укладывается заново с 08:40 (допуск в минуту: часы сервера идут сами).
    expect(firstOpen!.start).toBeGreaterThanOrEqual(toMinutes("08:40"));
    expect(firstOpen!.start).toBeLessThanOrEqual(toMinutes("08:41"));
    expect(slots.every((s) => (s.start as number) >= toMinutes("08:40"))).toBe(true);
    await expect(page.getByTestId("next-start")).toContainText("по плану в 08:4");

    // Группа 08:00–10:00 никогда не заходит за 10:00.
    const morning = groups.find((g) => g.periodStart === toMinutes("08:00") && g.windowId === "");
    expect(morning, "группа 08:00–10:00 осталась").toBeDefined();
    expect(morning!.slots.length).toBeGreaterThan(0);
    for (const s of morning!.slots) expect(s.end as number).toBeLessThanOrEqual(toMinutes("10:00"));

    // Окно «Работа» стартует ровно в 10:00 и хранит свою долю.
    const firstWindow = windowGroups(groups)[0];
    expect(firstWindow.periodStart).toBe(toMinutes("10:00"));
    expect(firstWindow.slots[0].start).toBe(toMinutes("10:00"));
    expect(firstWindow.slots.filter(isWork).length).toBeGreaterThan(0);
    // Утренних слотов до 08:40 в плане нет вовсе: группа 06:00–06:40 исчезла.
    expect(groups.some((g) => g.periodEnd === toMinutes("06:40"))).toBe(false);

    expectPlanValid(groups, { noAdjacentOthers: true });
    const over = overflowGroup(groups);
    if (over) expect(over.slots.every((s) => s.start === null)).toBe(true);
  });
});

test.describe("идущий помидор", () => {
  test("фокус идёт по длине слота; пауза сдвигает следующие слоты вперёд, активный слот подсвечен", async ({
    page,
    request,
  }, info) => {
    const ui = await boot(request, page, { now: "08:00" });
    const before = await readPlan(page);
    const slot0 = committed(before)[0];
    const slot1 = committed(before)[1];
    const focusMin = Number(slot0.focus);
    expect(focusMin, "помидор окна 08:00–10:00 короче 25 минут").toBeLessThan(25);
    expect(slot0.start).toBe(toMinutes("08:00"));
    expect(slot0.active, "до старта первый слот — «следующий»").toBe(true);

    await ui.startFocus();
    const first = await ui.seconds();
    expect(first).toBeLessThanOrEqual(focusMin * 60);
    expect(first).toBeGreaterThan(focusMin * 60 - 20);
    expect((await getState(request)).planned_seconds).toBe(focusMin * 60);
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "focus");
    await expect(page.getByTestId("now-title")).toHaveText(slot0.task);
    await expect(slotAt(page, 0)).toHaveAttribute("data-active", "true");
    await expect(page.locator('[data-testid="plan-slot"][data-active="true"]')).toHaveCount(1);
    // Идущий помидор нельзя перетащить на другую длительность.
    await expect(slotAt(page, 0).getByTestId("slot-focus")).toBeDisabled();
    await expect(slotAt(page, 1).getByTestId("slot-focus")).toBeEnabled();

    // 5 минут фокуса, пауза, сервер уезжает на 10 минут.
    await advance(request, 300);
    await ui.btnPause.click();
    await expect(page.getByTestId("btn-pause")).toHaveAttribute("data-paused", "true");
    await advance(request, 600);
    await page.waitForTimeout(1_200);
    await ui.btnPause.click();
    await expect(page.getByTestId("btn-pause")).toHaveAttribute("data-paused", "false");
    const left = await ui.seconds();
    expect(left, "пауза не съела фокус").toBeGreaterThan(focusMin * 60 - 300 - 30);
    expect(left).toBeLessThanOrEqual(focusMin * 60 - 300);

    // Следующий слот уехал примерно на длину паузы (10 минут).
    await reloadPlan(page);
    const after = await readPlan(page);
    const moved = committed(after).find((s) => s.idx === 1);
    expect(moved, "слот №2 остался в плане").toBeDefined();
    expect(moved!.start! - slot1.start!, "следующий старт сдвинулся на паузу").toBeGreaterThanOrEqual(9);
    expect(moved!.start! - slot1.start!).toBeLessThanOrEqual(12);
    expect(committed(after)[0].active).toBe(true);
    expect(committed(after).filter((s) => s.active)).toHaveLength(1);
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "focus");
    expectPlanValid(after, { noAdjacentOthers: true });
    await expectNoHorizontalScroll(page, info);
  });

  test("конец фокуса по часам: перерыв берётся из слота, выполненный слот помнит реальный старт", async ({
    page,
    request,
  }) => {
    const ui = await boot(request, page, { now: "08:00" });
    const slot0 = committed(await readPlan(page))[0];
    const focusMin = Number(slot0.focus);
    const breakMin = Number(slot0.brk);
    expect(breakMin).toBeGreaterThan(0);
    await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", "0");

    await ui.startFocus();
    await advance(request, focusMin * 60 + 2);
    await expect(ui.btnFocus).toBeVisible();

    // Дальше — короткий перерыв длиной из слота, а не 5:00 из настроек.
    await expect(page.getByTestId("phase-title")).toContainText("Дальше короткий перерыв");
    await expect(ui.digits).toHaveText(mm(breakMin));
    await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", "1");
    await expect(page.getByTestId("day-counter")).toContainText("Сегодня: 1 из");
    await expect(page.locator('[data-testid="day-dot"][data-state="done"]')).toHaveCount(1);

    // Слот #0 выполнен, показывает фактический старт; слот #1 — следующий.
    await reloadPlan(page);
    const done = slotAt(page, 0);
    await expect(done).toHaveAttribute("data-done", "true");
    await expect(done).toHaveAttribute("data-active", "false");
    await expect(done).toHaveAttribute("data-start", String(toMinutes("08:00")));
    await expect(done.getByTestId("slot-start")).toHaveText("08:00");
    await expect(done.getByTestId("slot-task")).toBeDisabled();
    await expect(done.getByTestId("slot-focus")).toBeDisabled();
    await expect(slotAt(page, 1)).toHaveAttribute("data-active", "true");
    await expect(slotAt(page, 1)).toHaveAttribute("data-done", "false");
    await expect(ui.digits).toHaveText(mm(breakMin));

    // Перерыв стартует с длиной слота.
    await ui.startBreak();
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "short_break");
    const left = await ui.seconds();
    expect(left).toBeLessThanOrEqual(breakMin * 60);
    expect(left).toBeGreaterThan(breakMin * 60 - 20);
    expect((await getState(request)).planned_seconds).toBe(breakMin * 60);

    expectPlanValid(await readPlan(page), { noAdjacentOthers: true });
  });

  test("после третьего помидора блока перерыв длинный и берётся из слота", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    for (let i = 0; i < 3; i++) {
      await ui.startFocus();
      const planned = (await getState(request)).planned_seconds;
      await advance(request, planned + 2);
      await expect(ui.btnFocus).toBeVisible();
      await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", String(i + 1));
    }
    await expect(page.getByTestId("phase-title")).toContainText("Дальше длинный перерыв");
    await reloadPlan(page);
    const third = committed(await readPlan(page)).find((s) => s.idx === 2)!;
    expect(third.done).toBe(true);
    const longMin = Number(third.brk);
    expect(longMin, "длинный перерыв слота не короче настроенных 15 минут").toBeGreaterThanOrEqual(15);
    await expect(ui.digits).toHaveText(mm(longMin));
    await ui.startBreak();
    await expect(page.getByTestId("phase-title")).toHaveAttribute("data-phase", "long_break");
    expect((await getState(request)).planned_seconds).toBe(longMin * 60);
  });
});

test.describe("после конца активного дня", () => {
  test("в 20:30 у слотов нет времени, остаток задач — в «Не влезли в день»", async ({ page, request }, info) => {
    await boot(request, page, { now: "20:30" });
    const groups = await readPlan(page);

    await expect(page.locator('[data-testid="plan-slot"]:not([data-start=""])')).toHaveCount(0);
    expect(groups.every((g) => g.kind === "overflow")).toBe(true);
    const over = overflowGroup(groups)!;
    expect(over.title).toContain("Не влезли в день");
    expect(over.slots.length).toBeGreaterThan(0);
    expect(over.slots.every((s) => s.overflow && s.start === null && s.end === null)).toBe(true);
    expect(over.slots.every((s) => s.startText === "")).toBe(true);

    // Остаётся вся работа дня: гибкие задачи фикстуры; окно «Работа» уже закрыто.
    expect(new Set(over.slots.map((s) => s.task))).toEqual(
      new Set(["Поиск работы", "Работа с ИИ и другое", "Английский", "Слепая печать"]),
    );
    await expect(page.getByTestId("next-start")).toHaveCount(0);
    await expectNoHorizontalScroll(page, info);
  });
});

test.describe("правка слотов", () => {
  test("задача слота: выбор в дереве сохраняется после перезагрузки", async ({ page, request }) => {
    await boot(request, page);
    const before = await readPlan(page);
    // Первый слот утреннего периода 08:00–10:00 — ещё не начатый, свободный.
    const target = committed(before).find((s) => s.start === toMinutes("08:00"))!;
    expect(target, "слот 08:00").toBeDefined();
    expect(target.task).not.toBe("Английский");

    await slotAt(page, target.idx).getByTestId("slot-task").click();
    await expect(page.getByTestId("task-picker")).toBeVisible();
    await page.getByTestId("picker-node").filter({ hasText: "Английский" }).first().click();
    await expect(page.getByTestId("task-picker")).toHaveCount(0);
    await expect(slotAt(page, target.idx).getByTestId("slot-task")).toContainText("Английский");

    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-task")).toContainText("Английский");
    const groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
    // Закреплённый слот остался на своём месте.
    expect(committed(groups).find((s) => s.idx === target.idx)!.start).toBe(target.start);
  });

  test("метка слота: своя метка из пикера, «очистить слот» возвращает слот автоматике", async ({ page, request }) => {
    await boot(request, page);
    const target = committed(await readPlan(page)).find((s) => s.start === toMinutes("08:00"))!;
    const task = slotAt(page, target.idx).getByTestId("slot-task");

    await task.click();
    await page.getByTestId("picker-label-input").fill("Разбор почты");
    await page.getByTestId("picker-label-ok").click();
    await expect(task).toContainText("Разбор почты");
    await reloadPlan(page);
    await expect(task).toContainText("Разбор почты");

    // «Очистить слот» снимает ручную привязку: слот снова отдан автоматике и получает задачу плана.
    await task.click();
    await page.getByTestId("picker-clear").click();
    await expect(task).not.toContainText("Разбор почты");
    await expect(task).toHaveText(target.task);
    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-task")).toHaveText(target.task);
    expectPlanValid(await readPlan(page), { noAdjacentOthers: true });
  });

  test("длительность слота: своя минутность переживает перезагрузку, пустое поле возвращает значение плана", async ({
    page,
    request,
  }) => {
    await boot(request, page);
    const before = await readPlan(page);
    const target = committed(before).find((s) => s.start === toMinutes("08:00"))!;
    const auto = Number(target.focus);
    expect(auto).toBeGreaterThan(20);
    const field = slotAt(page, target.idx).getByTestId("slot-focus");

    await field.fill("20");
    await expect(field).toHaveValue("20");
    await expect(field).toBeEnabled();
    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-focus")).toHaveValue("20");
    let groups = await readPlan(page);
    const custom = committed(groups).find((s) => s.idx === target.idx)!;
    expect(custom.end! - custom.start!, "конец слота = старт + 20 минут").toBe(20);
    expect(committed(groups).length, "никто не пропал из плана").toBeGreaterThanOrEqual(committed(before).length - 1);
    expectPlanValid(groups, { noAdjacentOthers: true });
    // Циферблат тоже знает: если это следующий слот, он покажет новую длину — здесь слот не первый, так что
    // проверяем первый и убеждаемся, что он не затронут.
    await expect(page.getByTestId("timer-digits")).toHaveText(mm(Number(committed(groups)[0].focus)));

    // Пустое поле — снова значение плана.
    await slotAt(page, target.idx).getByTestId("slot-focus").fill("");
    await expect(slotAt(page, target.idx).getByTestId("slot-focus")).toHaveValue(String(auto));
    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-focus")).toHaveValue(String(auto));
    groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
  });

  test("слишком длинный помидор не вылезает за период: лишнее уходит из группы", async ({ page, request }) => {
    await boot(request, page);
    const groups0 = await readPlan(page);
    const target = committed(groups0).find((s) => s.start === toMinutes("08:00"))!;
    await slotAt(page, target.idx).getByTestId("slot-focus").fill("90");
    await expect(slotAt(page, target.idx).getByTestId("slot-focus")).toHaveValue("90");
    await reloadPlan(page);
    const groups = await readPlan(page);
    // Что бы ни решил планировщик, граница периода 10:00 нерушима.
    expectPlanValid(groups, { noAdjacentOthers: true });
    const morning = groups.find((g) => g.periodStart === toMinutes("08:00") && g.windowId === "")!;
    for (const s of morning.slots) expect(s.end as number).toBeLessThanOrEqual(toMinutes("10:00"));
  });

  test("кнопка обновления перестраивает план без ошибок и не ломает раскладку", async ({ page, request }) => {
    await boot(request, page);
    const before = await readPlan(page);
    await page.getByTestId("plan-refresh").click();
    await expect(page.getByTestId("plan-refresh")).toBeEnabled();
    await expect(page.getByTestId("app-error")).toHaveCount(0);
    await waitForPlan(page);
    const after = await readPlan(page);
    expectPlanValid(after, { noAdjacentOthers: true });
    // Состояние дня не менялось — раскладка та же.
    expect(after.map((g) => [g.kind, g.periodStart, g.periodEnd, g.slots.length])).toEqual(
      before.map((g) => [g.kind, g.periodStart, g.periodEnd, g.slots.length]),
    );
    expect(committed(after).map((s) => s.start)).toEqual(committed(before).map((s) => s.start));
  });

  test("обновление после опоздания перекладывает остаток дня, а не копит старое", async ({ page, request }) => {
    await boot(request, page);
    await setClock(request, "09:30");
    await page.getByTestId("plan-refresh").click();
    await expect(page.getByTestId("plan-refresh")).toBeEnabled();
    const groups = await readPlan(page);
    const firstOpen = committed(groups).find((s) => !s.done)!;
    expect(firstOpen.start).toBeGreaterThanOrEqual(toMinutes("09:30"));
    expect(firstOpen.start).toBeLessThanOrEqual(toMinutes("09:31"));
    expectPlanValid(groups, { noAdjacentOthers: true });
    // Окно по-прежнему начинается в 10:00.
    expect(windowGroups(groups)[0].slots[0].start).toBe(toMinutes("10:00"));
    // Сброс часов не нужен: reset следующего теста вернёт сегодняшнее время.
    await reset(request, { now: "05:50" });
  });
});
