import { expect, test } from "@playwright/test";
import { advance, getState } from "./helpers";
import { boot, committed, expectNoHorizontalScroll, expectPlanValid, readPlan, reloadPlan, slotAt } from "./plan-helpers";

test.describe("список «Помидоры сегодня»", () => {
  test("сессию можно перепривязать к своей метке прямо из списка", async ({ page, request }, info) => {
    const ui = await boot(request, page, { now: "08:00" });
    const slot0 = committed(await readPlan(page))[0];

    await ui.startFocus();
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect(ui.btnFocus).toBeVisible();

    await expect(ui.sessionRows).toHaveCount(1);
    const row = ui.sessionRows.first();
    await expect(row.getByTestId("session-title")).toHaveText(slot0.task);
    await expect(ui.creditBadge(row)).toHaveText("1");
    await expect(row.getByTestId("session-credit")).toHaveAttribute("data-twelfths", "12");
    await expect(row.getByTestId("session-time")).toHaveText("08:00");
    await expect(row.getByTestId("session-duration")).toHaveText(`${slot0.focus}:00`);

    await expect(page.getByTestId("session-relabel-panel")).toHaveCount(0);
    await row.getByTestId("session-relabel").click();
    const panel = row.getByTestId("session-relabel-panel");
    await expect(panel).toBeVisible();
    await expectNoHorizontalScroll(page, info);
    await panel.getByTestId("binding-label-input").fill("Разбор почты");
    await panel.getByTestId("binding-label-input").press("Enter");

    await expect(page.getByTestId("session-relabel-panel")).toHaveCount(0);
    await expect(row.getByTestId("session-title")).toHaveText("Разбор почты");
    await expect(page.getByTestId("sessions-error")).toHaveCount(0);
    await expect(row.getByTestId("session-credit")).toHaveAttribute("data-twelfths", "12");
    await expect(row.getByTestId("session-time")).toHaveText("08:00");

    const sessions = (await (await request.get("/api/v1/rpc/sessions-today")).json()) as Array<Record<string, unknown>>;
    const focus = sessions.filter((s) => s.kind === "focus");
    expect(focus).toHaveLength(1);
    expect(focus[0].label).toBe("Разбор почты");
    expect(focus[0].task_external_id ?? null).toBeNull();
    await reloadPlan(page);
    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.sessionRows.first().getByTestId("session-title")).toHaveText("Разбор почты");
    await expectNoHorizontalScroll(page, info);
  });

  test("сессию можно перепривязать к задаче из списка задач", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    await ui.startFocus();
    await advance(request, (await getState(request)).planned_seconds + 2);
    await expect(ui.sessionRows).toHaveCount(1);
    const row = ui.sessionRows.first();

    await row.getByTestId("session-relabel").click();
    const panel = row.getByTestId("session-relabel-panel");
    await panel.getByTestId("binding-mode-task").click();
    await panel.getByTestId("binding-search").fill("Англ");
    await panel.getByTestId("binding-option").filter({ hasText: "Английский" }).click();
    await expect(page.getByTestId("session-relabel-panel")).toHaveCount(0);
    await expect(row.getByTestId("session-title")).toHaveText("Английский");
    await expect(page.getByTestId("sessions-error")).toHaveCount(0);
  });

  test("«Завершить» на половине слота засчитывает ½ помидора", async ({ page, request }, info) => {
    const ui = await boot(request, page, { now: "08:00" });
    const slot0 = committed(await readPlan(page))[0];
    const slotSeconds = Number(slot0.focus) * 60;

    await ui.startFocus();
    await advance(request, slotSeconds / 2);
    await expect(ui.creditHint).toContainText("½");
    await expect(page.getByTestId("credit-hint")).toHaveAttribute("data-twelfths", "6");

    await ui.btnComplete.click();
    await expect(ui.btnFocus).toBeVisible();
    await expect(ui.sessionRows).toHaveCount(1);
    const row = ui.sessionRows.first();
    await expect(row.getByTestId("session-credit")).toHaveText("½");
    await expect(row.getByTestId("session-credit")).toHaveAttribute("data-twelfths", "6");
    await expect(row).toHaveAttribute("data-outcome", "completed");

    await expect(ui.dayCounter).toHaveAttribute("data-completed", "1");
    await expect(ui.dayCounter).toContainText("Сегодня: 1 из");
    await expect(ui.dayCounter).toContainText(/засчитано 0[,.]5/);
    await expect(page.locator('[data-testid="day-dot"][data-state="done"]')).toHaveCount(1);
    const st = await getState(request);
    expect(st.completed_today).toBe(1);
    expect(st.credit_today).toBe(0.5);

    await reloadPlan(page);
    await expect(slotAt(page, 0)).toHaveAttribute("data-done", "true");
    await expect(slotAt(page, 1)).toHaveAttribute("data-active", "true");
    expectPlanValid(await readPlan(page), { noAdjacentOthers: true });
    await expectNoHorizontalScroll(page, info);
  });

  test("«Завершить» на 13-й минуте слота даёт ближайшую долю по шкале", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    const slotMin = Number(committed(await readPlan(page))[0].focus);
    await ui.startFocus();
    await advance(request, 13 * 60);
    const expectedTwelfths = [0, 3, 4, 6, 8, 9, 12].reduce((best, step) =>
      Math.abs(780 * 12 - step * slotMin * 60) <= Math.abs(780 * 12 - best * slotMin * 60) ? step : best,
    );
    await expect(page.getByTestId("credit-hint")).toHaveAttribute("data-twelfths", String(expectedTwelfths));
    await ui.btnComplete.click();
    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.sessionRows.first().getByTestId("session-credit")).toHaveAttribute(
      "data-twelfths",
      String(expectedTwelfths),
    );
    await expect(ui.dayCounter).toHaveAttribute("data-completed", "1");
  });

  test("брошенный помидор попадает в список без зачёта и слот остаётся следующим", async ({ page, request }) => {
    const ui = await boot(request, page, { now: "08:00" });
    await ui.startFocus();
    await advance(request, 300);
    await ui.btnStop.click();
    await expect(ui.btnFocus).toBeVisible();

    await expect(ui.sessionRows).toHaveCount(1);
    await expect(ui.sessionRows.first().getByTestId("session-credit")).toHaveAttribute("data-twelfths", "0");
    await expect(ui.dayCounter).toHaveAttribute("data-completed", "0");
    await expect(slotAt(page, 0)).toHaveAttribute("data-done", "false");
    await expect(slotAt(page, 0)).toHaveAttribute("data-active", "true");
    const first = committed(await readPlan(page))[0];
    await expect(ui.digits).toHaveText(`${first.focus}:00`);
  });
});
