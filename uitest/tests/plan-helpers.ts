import { expect, type APIRequestContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import { PomodoroPage, reset, type ResetOptions } from "./helpers";

// ---------------------------------------------------------------------------
// Разбор «Плана помидоров» из DOM и проверка его инвариантов.
// Один page.evaluate даёт согласованный снимок: между чтениями отдельных
// локаторов план не успевает перестроиться (опрос раз в секунду).
// ---------------------------------------------------------------------------

export const DAY_START = 6 * 60;
export const DAY_END = 20 * 60;
/** Окно «Работа» в фикстуре по умолчанию (cmd/uitest-server/tasks.go). */
export const WORK_ID = "win-work";
export const WORK_TITLE = "Работа";

export interface SlotInfo {
  idx: number;
  done: boolean;
  overflow: boolean;
  active: boolean;
  start: number | null;
  end: number | null;
  windowId: string;
  /** Текст кнопки slot-task: название задачи/метка либо «—». */
  task: string;
  startText: string;
  /** Значение полей длительности: минуты или "" (слот без своей длительности). */
  focus: string;
  brk: string;
}

export interface GroupInfo {
  kind: "period" | "none" | "overflow";
  windowId: string;
  periodStart: number | null;
  periodEnd: number | null;
  title: string;
  /** Счётчик окна («Работа 5 из 8 · другие 3») — только у групп-окон. */
  counter: { count: number; other: number } | null;
  slots: SlotInfo[];
}

export function fmClock(minutes: number): string {
  return `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}

/** «HH:MM» → минуты от полуночи. */
export function toMinutes(clock: string): number {
  const [h, m] = clock.split(":").map(Number);
  return h * 60 + m;
}

export async function readPlan(page: Page): Promise<GroupInfo[]> {
  return page.evaluate(() => {
    const num = (v: string | null): number | null => (v === null || v === "" ? null : Number(v));
    const q = (root: Element, id: string) => root.querySelector(`[data-testid="${id}"]`);
    return Array.from(document.querySelectorAll('[data-testid="plan-group"]')).map((g) => {
      const counter = q(g, "plan-window-counter");
      return {
        kind: g.getAttribute("data-kind") as "period" | "none" | "overflow",
        windowId: g.getAttribute("data-window-id") ?? "",
        periodStart: num(g.getAttribute("data-period-start")),
        periodEnd: num(g.getAttribute("data-period-end")),
        title: q(g, "plan-group-title")?.textContent?.trim() ?? "",
        counter: counter
          ? {
              count: Number(counter.getAttribute("data-window-count")),
              other: Number(counter.getAttribute("data-other-count")),
            }
          : null,
        slots: Array.from(g.querySelectorAll('[data-testid="plan-slot"]')).map((s) => ({
          idx: Number(s.getAttribute("data-idx")),
          done: s.getAttribute("data-done") === "true",
          overflow: s.getAttribute("data-overflow") === "true",
          active: s.getAttribute("data-active") === "true",
          start: num(s.getAttribute("data-start")),
          end: num(s.getAttribute("data-end")),
          windowId: s.getAttribute("data-window-id") ?? "",
          task: q(s, "slot-task")?.textContent?.trim() ?? "",
          startText: q(s, "slot-start")?.textContent?.trim() ?? "",
          focus: (q(s, "slot-focus") as HTMLInputElement | null)?.value ?? "",
          brk: (q(s, "slot-break") as HTMLInputElement | null)?.value ?? "",
        })),
      };
    });
  });
}

export const allSlots = (groups: GroupInfo[]): SlotInfo[] => groups.flatMap((g) => g.slots);
/** Слоты с местом в дне (не «Не влезли в день»), в порядке плана. */
export const committed = (groups: GroupInfo[]): SlotInfo[] => allSlots(groups).filter((s) => !s.overflow);
export const overflowGroup = (groups: GroupInfo[]): GroupInfo | undefined => groups.find((g) => g.kind === "overflow");
export const windowGroups = (groups: GroupInfo[]): GroupInfo[] => groups.filter((g) => g.kind === "period" && g.windowId);

/** Слот задачи окна «Работа»: точное совпадение — «Работа с ИИ и другое» к ней не относится. */
export const isWork = (s: SlotInfo): boolean => s.task === WORK_TITLE;

export interface PlanRules {
  dayStart?: number;
  dayEnd?: number;
  /** Доля окна ≥50%: две «чужие» задачи подряд внутри окна недопустимы. */
  noAdjacentOthers?: boolean;
}

/**
 * Инварианты плана дня: каждый слот с местом целиком лежит в группе-периоде и в активном дне,
 * слоты идут по времени; «Работа» — только в своём окне; «Не влезли» — без времени.
 */
export function expectPlanValid(groups: GroupInfo[], rules: PlanRules = {}): void {
  const dayStart = rules.dayStart ?? DAY_START;
  const dayEnd = rules.dayEnd ?? DAY_END;
  let prevStart = -1;
  let prevIdx = -1;

  for (const g of groups) {
    for (const s of g.slots) {
      const where = `слот #${s.idx} «${s.task}» в группе «${g.title}»`;
      expect(s.idx, `${where}: порядок idx`).toBeGreaterThan(prevIdx);
      prevIdx = s.idx;

      if (g.kind === "overflow") {
        expect(s.overflow, `${where}: overflow-флаг`).toBe(true);
        expect(s.start, `${where}: у «не влезшего» нет начала`).toBeNull();
        expect(s.end, `${where}: у «не влезшего» нет конца`).toBeNull();
        continue;
      }
      expect(s.overflow, `${where}: не overflow`).toBe(false);
      expect(s.start, `${where}: есть начало`).not.toBeNull();
      const start = s.start as number;
      expect(start, `${where}: идёт после предыдущего`).toBeGreaterThan(prevStart);
      prevStart = start;
      // Выполненный слот хранит фактический старт — человек мог начать до 06:00 или чуть раньше периода.
      if (!s.done) expect(start, `${where}: не раньше начала дня`).toBeGreaterThanOrEqual(dayStart);
      if (g.kind === "period") {
        // Группа-период: границы известны, слот целиком внутри.
        expect(g.periodStart, `${where}: у группы есть начало периода`).not.toBeNull();
        expect(g.periodEnd, `${where}: у группы есть конец периода`).not.toBeNull();
        if (!s.done) expect(start, `${where}: не раньше начала периода`).toBeGreaterThanOrEqual(g.periodStart as number);
        // У выполненного слота конца нет (в плане остаётся только фактический старт).
        if (!s.done) {
          expect(s.end, `${where}: есть конец`).not.toBeNull();
          expect(s.end as number, `${where}: конец позже начала`).toBeGreaterThan(start);
          expect(s.end as number, `${where}: не позже конца периода`).toBeLessThanOrEqual(g.periodEnd as number);
          expect(s.end as number, `${where}: не позже конца дня`).toBeLessThanOrEqual(dayEnd);
        }
        if (s.startText) expect(s.startText, `${where}: подпись начала`).toBe(fmClock(start));
      }
      // Окно «Работа» живёт только в своих группах-окнах.
      if (isWork(s)) {
        expect(g.windowId, `${where}: задача окна вне окна`).toBe(WORK_ID);
        expect(s.windowId, `${where}: window-id слота`).toBe(WORK_ID);
      }
    }

    if (g.kind === "overflow") continue;
    if (g.windowId && rules.noAdjacentOthers) {
      for (let i = 1; i < g.slots.length; i++) {
        const twoOthers = !isWork(g.slots[i - 1]) && !isWork(g.slots[i]);
        expect(twoOthers, `в окне «${g.title}» два «чужих» слота подряд: #${g.slots[i - 1].idx}, #${g.slots[i].idx}`).toBe(
          false,
        );
      }
    }
    if (g.counter) {
      const works = g.slots.filter(isWork).length;
      expect(g.counter.count, `счётчик окна «${g.title}»`).toBe(works);
      expect(g.counter.count + g.counter.other, `размер окна «${g.title}»`).toBe(g.slots.length);
    }
  }
}

// ---------------------------------------------------------------------------
// Запуск, ожидания, мобильный режим.
// ---------------------------------------------------------------------------

export interface BootOptions extends ResetOptions {
  /** Блоки дня; по умолчанию [3] — длинный перерыв после каждого третьего помидора. */
  blocks?: number[];
}

/** Сбрасывает бэкенд (фикстура задач по умолчанию), открывает страницу и ждёт, пока появится план. */
export async function boot(
  request: APIRequestContext,
  page: Page,
  opts: BootOptions = {},
): Promise<PomodoroPage> {
  const { blocks, ...rest } = opts;
  const settings = { day_blocks: blocks ?? [3], ...(rest.settings ?? {}) };
  await reset(request, { ...rest, settings });
  const ui = new PomodoroPage(page);
  await ui.open();
  await waitForPlan(page);
  return ui;
}

export async function waitForPlan(page: Page): Promise<void> {
  await expect(page.getByTestId("plan")).toBeVisible();
  await expect(page.getByTestId("plan-slot").first()).toBeVisible();
}

/** Перезагружает страницу и ждёт план: после сдвига часов сервера это самый надёжный способ увидеть новые времена. */
export async function reloadPlan(page: Page): Promise<void> {
  await page.reload();
  await expect(page.getByTestId("timer-digits")).toBeVisible();
  await waitForPlan(page);
}

export const slotAt = (page: Page, idx: number): Locator =>
  page.locator(`[data-testid="plan-slot"][data-idx="${idx}"]`);

export function planSlots(page: Page): Locator {
  return page.getByTestId("plan-slot");
}

/** Ждёт, пока кнопка настроек и форма вернутся из «Сохранено» — запись прошла. */
export async function saveSettings(page: Page): Promise<void> {
  await page.getByTestId("settings-save").click();
  await expect(page.getByTestId("settings-status")).toHaveText("Сохранено");
  await expect(page.getByTestId("settings-error")).toHaveCount(0);
}

export const isMobile = (info: TestInfo): boolean => info.project.name === "mobile";

/**
 * На мобильном экране страница не должна ехать вбок: ни документ, ни прокручиваемая область <main>
 * (у неё overflow-y: auto, поэтому лишняя ширина прячется там, а не в document).
 */
export async function expectNoHorizontalScroll(page: Page, info: TestInfo): Promise<void> {
  if (!isMobile(info)) return;
  const m = await page.evaluate(() => {
    const el = document.scrollingElement ?? document.documentElement;
    const main = document.querySelector("main");
    return {
      docScroll: el.scrollWidth,
      inner: window.innerWidth,
      mainScroll: main?.scrollWidth ?? 0,
      mainClient: main?.clientWidth ?? 0,
    };
  });
  expect(m.docScroll, `document.scrollWidth ${m.docScroll} > innerWidth ${m.inner}`).toBeLessThanOrEqual(m.inner + 1);
  expect(m.mainScroll, `main.scrollWidth ${m.mainScroll} > clientWidth ${m.mainClient}`).toBeLessThanOrEqual(
    m.mainClient + 1,
  );
}

export async function getDayPlanApi(request: APIRequestContext): Promise<unknown[]> {
  const res = await request.get("/api/v1/rpc/day-plan");
  return (await res.json()) as unknown[];
}
