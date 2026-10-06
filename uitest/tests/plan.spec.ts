import { expect, test } from "@playwright/test";
import { advance, getState, reset, setClock } from "./helpers";
import {
  DAY_END,
  DAY_START,
  WORK_ID,
  allSlots,
  boot,
  committed,
  expectNoHorizontalScroll,
  expectPlanValid,
  fmClock,
  isMobile,
  isWork,
  overflowGroup,
  readPlan,
  reloadPlan,
  slotAt,
  toMinutes,
  waitForPlan,
  windowGroups,
} from "./plan-helpers";

const mm = (minutes: number) => `${String(minutes).padStart(2, "0")}:00`;

test.describe("раскладка дня", () => {
  test("в 05:50 день режется на периоды, помидоры не пересекают их границы", async ({ page, request }, info) => {
    await boot(request, page);
    const groups = await readPlan(page);

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

    expectPlanValid(groups, { noAdjacentOthers: true });
    for (const g of groups.filter((x) => x.kind === "period")) {
      expect(g.slots.length, `в группе ${g.title} есть помидоры`).toBeGreaterThan(0);
    }
    for (const s of committed(groups)) {
      expect(s.end as number).toBeLessThanOrEqual(1200);
    }

    const wins = windowGroups(groups);
    expect(wins).toHaveLength(2);
    const total = wins.reduce((n, g) => n + g.slots.length, 0);
    const work = wins.reduce((n, g) => n + (g.counter?.count ?? -1000), 0);
    expect(total).toBe(16);
    expect(work).toBe(11);
    expect(wins.reduce((n, g) => n + g.slots.filter(isWork).length, 0)).toBe(11);
    for (const g of groups.filter((x) => x.kind === "period" && !x.windowId)) {
      expect(g.slots.some(isWork), `«Работа» в свободном периоде ${g.title}`).toBe(false);
    }

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
    const outside = allSlots(groups).filter((s) => isWork(s) && s.windowId !== WORK_ID);
    expect(outside).toEqual([]);
    for (const g of windowGroups(groups)) {
      for (const s of g.slots) expect(s.windowId).toBe(WORK_ID);
    }
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
    expect(firstOpen!.start).toBeGreaterThanOrEqual(toMinutes("08:40"));
    expect(firstOpen!.start).toBeLessThanOrEqual(toMinutes("08:41"));
    expect(slots.every((s) => (s.start as number) >= toMinutes("08:40"))).toBe(true);
    await expect(page.getByTestId("next-start")).toContainText("по плану в 08:4");

    const morning = groups.find((g) => g.periodStart === toMinutes("08:00") && g.windowId === "");
    expect(morning, "группа 08:00–10:00 осталась").toBeDefined();
    expect(morning!.slots.length).toBeGreaterThan(0);
    for (const s of morning!.slots) expect(s.end as number).toBeLessThanOrEqual(toMinutes("10:00"));

    const firstWindow = windowGroups(groups)[0];
    expect(firstWindow.periodStart).toBe(toMinutes("10:00"));
    expect(firstWindow.slots[0].start).toBe(toMinutes("10:00"));
    expect(firstWindow.slots.filter(isWork).length).toBeGreaterThan(0);
    expect(groups.some((g) => g.periodEnd === toMinutes("06:40"))).toBe(false);

    expectPlanValid(groups, { noAdjacentOthers: true });
    const over = overflowGroup(groups);
    if (over) expect(over.slots.every((s) => s.start === null)).toBe(true);
  });
});

test.describe("опоздание: границы периодов", () => {
  const cases: Array<{ now: string; first: string; group: [string, string]; window: boolean }> = [
    { now: "09:50", first: "10:00", group: ["10:00", "14:00"], window: true },
    { now: "13:50", first: "15:00", group: ["15:00", "19:00"], window: true },
    { now: "14:30", first: "15:00", group: ["15:00", "19:00"], window: true },
    { now: "19:40", first: "19:40", group: ["19:00", "20:00"], window: false },
  ];
  for (const c of cases) {
    test(`в ${c.now} ближайший помидор — в ${c.first}, всё остальное не пересекает границы`, async ({ page, request }) => {
      const ui = await boot(request, page, { now: c.now });
      const groups = await readPlan(page);
      const slots = committed(groups);
      expect(slots.length).toBeGreaterThan(0);
      const first = slots[0];
      expect(first.start).toBeGreaterThanOrEqual(toMinutes(c.first));
      expect(first.start).toBeLessThanOrEqual(toMinutes(c.first) + 1);
      const g = groups.find((x) => x.slots.includes(first) || x.slots.some((s) => s.idx === first.idx))!;
      expect([g.periodStart, g.periodEnd]).toEqual([toMinutes(c.group[0]), toMinutes(c.group[1])]);
      expect(g.windowId !== "").toBe(c.window);
      expect(slots.every((s) => (s.start as number) >= toMinutes(c.now))).toBe(true);
      expectPlanValid(groups, { noAdjacentOthers: true });
      await expect(ui.digits).toHaveText(mm(Number(first.focus)));
      await expect(page.getByTestId("next-start")).toContainText(`по плану в ${c.first.slice(0, 4)}`);
    });
  }

  test("в 19:50 до конца дня не остаётся места: все помидоры — в «Не влезли в день»", async ({ page, request }) => {
    await boot(request, page, { now: "19:50" });
    const groups = await readPlan(page);
    expect(groups.every((g) => g.kind === "overflow")).toBe(true);
    expect(committed(groups)).toHaveLength(0);
    await expect(page.locator('[data-testid="plan-slot"]:not([data-start=""])')).toHaveCount(0);
    await expect(page.getByTestId("next-start")).toHaveCount(0);
  });

  test("два помидора сделаны, потом опоздание до 09:00: сделанные остаются, остаток — от «сейчас»", async ({
    page,
    request,
  }) => {
    const ui = await boot(request, page);
    for (let i = 0; i < 2; i++) {
      await ui.startFocus();
      await advance(request, (await getState(request)).planned_seconds + 2);
      await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", String(i + 1));
    }
    await setClock(request, "09:00");
    await reloadPlan(page);
    const groups = await readPlan(page);
    const slots = committed(groups);
    expect(slots.filter((s) => s.done).map((s) => s.idx)).toEqual([0, 1]);
    const open = slots.filter((s) => !s.done);
    expect(open[0].idx).toBe(2);
    expect(open[0].start).toBeGreaterThanOrEqual(toMinutes("09:00"));
    expect(open[0].start).toBeLessThanOrEqual(toMinutes("09:01"));
    for (const s of slots.filter((x) => x.done)) expect(s.start as number).toBeLessThan(toMinutes("07:00"));
    expect(windowGroups(groups)[0].slots[0].start).toBe(toMinutes("10:00"));
    expectPlanValid(groups, { noAdjacentOthers: true });
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
    await expect(slotAt(page, 0).getByTestId("slot-focus")).toBeDisabled();
    await expect(slotAt(page, 1).getByTestId("slot-focus")).toBeEnabled();

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

    await expect(page.getByTestId("phase-title")).toContainText("Дальше короткий перерыв");
    await expect(ui.digits).toHaveText(mm(breakMin));
    await expect(page.getByTestId("day-counter")).toHaveAttribute("data-completed", "1");
    await expect(page.getByTestId("day-counter")).toContainText("Сегодня: 1 из");
    await expect(page.locator('[data-testid="day-dot"][data-state="done"]')).toHaveCount(1);

    await reloadPlan(page);
    const done = slotAt(page, 0);
    await expect(done).toHaveAttribute("data-done", "true");
    await expect(done).toHaveAttribute("data-active", "false");
    await expect(done).toHaveAttribute("data-start", /^48[01]$/);
    await expect(done.getByTestId("slot-start")).toHaveText(/^08:0[01]$/);
    await expect(done.getByTestId("slot-task")).toBeDisabled();
    await expect(done.getByTestId("slot-focus")).toBeDisabled();
    await expect(slotAt(page, 1)).toHaveAttribute("data-active", "true");
    await expect(slotAt(page, 1)).toHaveAttribute("data-done", "false");
    await expect(ui.digits).toHaveText(mm(breakMin));

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
    expectPlanValid(await readPlan(page), { noAdjacentOthers: true });
  });

  test("час паузы выталкивает помидоры из утра, но не за 10:00 и не в окно раньше времени", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    const focusMin = Number((committed(await readPlan(page))[0]).focus);
    await ui.startFocus();
    await advance(request, 300);
    await ui.btnPause.click();
    await expect(page.getByTestId("btn-pause")).toHaveAttribute("data-paused", "true");
    await advance(request, 3600);
    await ui.btnPause.click();
    await expect(page.getByTestId("btn-pause")).toHaveAttribute("data-paused", "false");
    await reloadPlan(page);

    const groups = await readPlan(page);
    const slots = committed(groups);
    expect(slots[0].active).toBe(true);
    await expect(ui.digits).toHaveText(/^\d\d:\d\d$/);
    expect(await ui.seconds()).toBeLessThanOrEqual(focusMin * 60 - 300);
    expect(windowGroups(groups)[0].slots[0].start).toBe(toMinutes("10:00"));
    const morning = groups.find((g) => g.periodStart === toMinutes("08:00") && g.windowId === "")!;
    for (const s of morning.slots) expect(s.end as number).toBeLessThanOrEqual(toMinutes("10:00"));
    expect(morning.slots.length).toBeLessThan(4);
    expectPlanValid(groups, { noAdjacentOthers: true });
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

    expect(new Set(over.slots.map((s) => s.task))).toEqual(
      new Set(["Поиск работы", "Работа с ИИ и другое", "Английский", "Слепая печать"]),
    );
    await expect(page.getByTestId("next-start")).toHaveCount(0);
    await expectNoHorizontalScroll(page, info);
  });
});

test.describe("правка слотов", () => {
  test("задача слота: выбор в дереве сохраняется после перезагрузки", async ({ page, request }, info) => {
    await boot(request, page);
    const before = await readPlan(page);
    const target = committed(before).find((s) => s.start === toMinutes("08:00"))!;
    expect(target, "слот 08:00").toBeDefined();
    expect(target.task).not.toBe("Английский");

    await slotAt(page, target.idx).getByTestId("slot-task").click();
    await expect(page.getByTestId("task-picker")).toBeVisible();
    await expectNoHorizontalScroll(page, info);
    await page.getByTestId("picker-node").filter({ hasText: "Английский" }).first().click();
    await expect(page.getByTestId("task-picker")).toHaveCount(0);
    await expect(slotAt(page, target.idx).getByTestId("slot-task")).toContainText("Английский");

    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-task")).toContainText("Английский");
    const groups = await readPlan(page);
    expectPlanValid(groups, { noAdjacentOthers: true });
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
    await expect(page.getByTestId("timer-digits")).toHaveText(mm(Number(committed(groups)[0].focus)));

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
    expect(windowGroups(groups)[0].slots[0].start).toBe(toMinutes("10:00"));
    await reset(request, { now: "05:50" });
  });
});

test.describe("правка перерыва и «не влезших»", () => {
  test("перерыв после слота: своя длина сохраняется и двигает следующий старт", async ({ page, request }) => {
    await boot(request, page);
    const before = committed(await readPlan(page));
    const target = before.find((s) => s.start === toMinutes("08:00"))!;
    const next = before.find((s) => s.idx === target.idx + 1)!;
    const oldBreak = Number(target.brk);
    const field = slotAt(page, target.idx).getByTestId("slot-break");

    await field.fill("12");
    await expect(field).toHaveValue("12");
    await expect(field).toBeEnabled();
    await reloadPlan(page);
    await expect(slotAt(page, target.idx).getByTestId("slot-break")).toHaveValue("12");
    const groups = await readPlan(page);
    const moved = committed(groups).find((s) => s.idx === next.idx)!;
    expect(moved.start! - next.start!, "следующий помидор сдвинулся на разницу в перерыве").toBe(12 - oldBreak);
    expectPlanValid(groups, { noAdjacentOthers: true });

    await slotAt(page, target.idx).getByTestId("slot-break").fill("");
    await expect(slotAt(page, target.idx).getByTestId("slot-break")).toHaveValue(String(oldBreak));
  });

  test("перерыв выполненного помидора можно поменять до старта перерыва — циферблат его покажет", async ({
    page,
    request,
  }) => {
    const ui = await boot(request, page, { now: "08:00" });
    await ui.startFocus();
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect(ui.btnFocus).toBeVisible();
    await reloadPlan(page);

    const field = slotAt(page, 0).getByTestId("slot-break");
    await expect(field).toBeEnabled();
    await field.fill("9");
    await expect(field).toHaveValue("9");
    await expect(ui.digits).toHaveText("09:00");
    await ui.startBreak();
    expect((await getState(request)).planned_seconds).toBe(9 * 60);
    await expect(field).toBeDisabled();
  });

  test("пикер задач слота из «Не влезли в день» не перекрыт соседними строками", async ({ page, request }, info) => {
    await boot(request, page, { now: "20:30" });
    const first = (await readPlan(page))[0].slots[0];
    await slotAt(page, first.idx).getByTestId("slot-task").click();
    await expect(page.getByTestId("task-picker")).toBeVisible();
    await page.screenshot({ path: info.outputPath("overflow-picker.png") });

    const node = page.getByTestId("picker-node").filter({ hasText: "Слепая печать" }).first();
    await node.scrollIntoViewIfNeeded();
    const topmost = await node.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return { inside: hit ? el.contains(hit) : false, hit: hit ? `${hit.tagName}[data-testid=${hit.getAttribute("data-testid")}]` : null };
    });
    expect(topmost, `поверх пункта «Слепая печать» лежит ${topmost.hit}`).toEqual({ inside: true, hit: expect.any(String) });
  });

  test("слоты «Не влезли в день» остаются без времени после обновления плана", async ({ page, request }) => {
    await boot(request, page, { now: "20:30" });
    await page.getByTestId("plan-refresh").click();
    await expect(page.getByTestId("plan-refresh")).toBeEnabled();
    await expect(page.getByTestId("app-error")).toHaveCount(0);
    const groups = await readPlan(page);
    expect(groups.every((g) => g.kind === "overflow")).toBe(true);
    expectPlanValid(groups);
    await expect(page.locator('[data-testid="plan-slot"]:not([data-start=""])')).toHaveCount(0);
  });
});

interface ApiSlot {
  idx: number;
  start_minutes: number | null;
  end_minutes: number | null;
  period_start_minutes: number | null;
  period_end_minutes: number | null;
  window_id: string | null;
  task: { title_snapshot: string } | null;
  overflow: boolean;
}

function planProblems(label: string, plan: ApiSlot[], dayStart: number, dayEnd: number, share: number | null): string[] {
  const out: string[] = [];
  const com = plan.filter((s) => !s.overflow);
  let prev = -1;
  for (const s of com) {
    const at = `${label}: слот ${s.idx}`;
    if (s.start_minutes == null || s.end_minutes == null) {
      out.push(`${at} без времени`);
      continue;
    }
    if (s.start_minutes <= prev) out.push(`${at} не по возрастанию`);
    prev = s.start_minutes;
    if (s.start_minutes < (s.period_start_minutes ?? 0) || s.end_minutes > (s.period_end_minutes ?? 0))
      out.push(`${at} ${fmClock(s.start_minutes)}–${fmClock(s.end_minutes)} вне периода`);
    if (s.start_minutes < dayStart || s.end_minutes > dayEnd) out.push(`${at} вне активного дня`);
    if (s.window_id == null && s.task?.title_snapshot === "Работа") out.push(`${at} «Работа» вне окна`);
  }
  for (const s of plan.filter((x) => x.overflow)) {
    if (s.start_minutes != null || s.end_minutes != null) out.push(`${label}: overflow-слот ${s.idx} со временем`);
  }
  if (share != null && share >= 50) {
    for (let i = 1; i < com.length; i++) {
      const a = com[i - 1];
      const b = com[i];
      if (a.window_id && b.window_id && a.period_end_minutes === b.period_end_minutes) {
        if (a.task?.title_snapshot !== "Работа" && b.task?.title_snapshot !== "Работа")
          out.push(`${label}: два «чужих» слота подряд ${a.idx},${b.idx}`);
      }
    }
  }
  if (share != null) {
    const win = com.filter((s) => s.window_id);
    const work = win.filter((s) => s.task?.title_snapshot === "Работа").length;
    const want = Math.floor((win.length * share + 50) / 100);
    if (work !== want) out.push(`${label}: «Работа» ${work} из ${win.length}, ожидалось ${want}`);
  }
  return out;
}

test.describe("инварианты плана на сетке настроек (API)", () => {
  test("время × фокус × блоки × доля окна: границы периодов, порядок, доля «Работы», чередование", async ({
    request,
  }, info) => {
    test.skip(isMobile(info), "не зависит от экрана — достаточно desktop");
    test.setTimeout(120_000);
    const problems: string[] = [];
    let plans = 0;
    for (const now of ["05:50", "08:40", "12:00", "13:50", "17:00"]) {
      for (const focus of [15, 25, 40]) {
        for (const blocks of [[2], [3], [4, 4]]) {
          for (const share of [50, 60, 67, 80, 100]) {
            await reset(request, {
              now,
              settings: { day_blocks: blocks, focus_duration_seconds: focus * 60, window_share_percent: share },
            });
            const plan = (await (await request.get("/api/v1/rpc/day-plan")).json()) as ApiSlot[];
            plans++;
            problems.push(
              ...planProblems(`now=${now} focus=${focus} blocks=${blocks} share=${share}`, plan, DAY_START, DAY_END, share),
            );
          }
        }
      }
    }
    expect(plans).toBe(225);
    expect(problems.slice(0, 20)).toEqual([]);
  });

  test("границы дня × время: помидоры только внутри активного дня", async ({ request }, info) => {
    test.skip(isMobile(info), "не зависит от экрана — достаточно desktop");
    test.setTimeout(120_000);
    const problems: string[] = [];
    const starts = [360, 450, 555, 620, 780, 905];
    const ends = [1200, 1130, 1020, 905, 700, 640, 1440];
    for (const now of ["05:50", "10:20", "14:20"]) {
      for (const ds of starts) {
        for (const de of ends) {
          if (de - ds < 30) continue;
          await reset(request, {
            now,
            settings: { day_blocks: [3], day_start_minutes: ds, day_end_minutes: de },
          });
          const plan = (await (await request.get("/api/v1/rpc/day-plan")).json()) as ApiSlot[];
          problems.push(...planProblems(`now=${now} day=${fmClock(ds)}-${fmClock(de)}`, plan, ds, de, null));
        }
      }
    }
    expect(problems.slice(0, 20)).toEqual([]);
  });
});
