import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, RefreshCw } from "lucide-react";
import { api } from "../api";
import { fmtClock } from "../time";
import type { PickerNode, Phase, PlanSlot, SlotPatch } from "../types";
import TaskTreePicker from "./TaskTreePicker";

interface PlanTodayProps {
  /** Растёт при событиях, после которых план/сессии нужно перечитать (старт, стоп, настройки...). */
  version: number;
  completedToday: number;
  phase: Phase;
  paused: boolean;
  defaultFocusMin: number;
  defaultBreakMin: number;
  onPlanChanged: (plan: PlanSlot[]) => void;
}

// Проекция времён зависит от часов, поэтому план перечитывается раз в минуту.
const REFETCH_MS = 60_000;

interface Group {
  key: string;
  kind: "period" | "none" | "overflow";
  start: number | null;
  end: number | null;
  windowId: string | null;
  windowTitle: string | null;
  slots: PlanSlot[];
}

function groupTitle(g: Group): string {
  if (g.kind === "overflow") return `Не влезли в день · ${g.slots.length}`;
  if (g.kind === "none") return "Без времени";
  const range = g.start != null && g.end != null ? `${fmtClock(g.start)}–${fmtClock(g.end)}` : "";
  const name = g.windowTitle || (g.windowId ? "окно" : "свободное время");
  return range ? `${range} · ${name}` : name;
}

function buildGroups(plan: PlanSlot[]): Group[] {
  const groups: Group[] = [];
  const committed = plan.filter((s) => !s.overflow);
  const overflow = plan.filter((s) => s.overflow);

  let current: Group | null = null;
  let currentSig = "";
  for (const s of committed) {
    const hasPeriod = s.period_end_minutes != null;
    const sig = hasPeriod ? `p:${s.period_end_minutes}:${s.window_id ?? ""}` : "none";
    if (current && sig === currentSig) {
      current.slots.push(s);
      if (hasPeriod && s.period_start_minutes != null) {
        current.start = current.start == null ? s.period_start_minutes : Math.min(current.start, s.period_start_minutes);
      }
      continue;
    }
    current = {
      key: `g${groups.length}`,
      kind: hasPeriod ? "period" : "none",
      start: hasPeriod ? (s.period_start_minutes ?? null) : null,
      end: hasPeriod ? (s.period_end_minutes ?? null) : null,
      windowId: s.window_id ?? null,
      windowTitle: s.window_title ?? null,
      slots: [s],
    };
    currentSig = sig;
    groups.push(current);
  }
  if (overflow.length > 0) {
    groups.push({ key: "overflow", kind: "overflow", start: null, end: null, windowId: null, windowTitle: null, slots: overflow });
  }
  return groups;
}

/** Слот принадлежит задаче окна, если его задача — само окно (по id) либо носит название окна. */
function isWindowSlot(s: PlanSlot, g: Group): boolean {
  if (!s.task) return false;
  if (g.windowId && s.task.external_id === g.windowId) return true;
  return Boolean(g.windowTitle) && s.task.title_snapshot === g.windowTitle;
}

export default function PlanToday({
  version,
  completedToday,
  phase,
  paused,
  defaultFocusMin,
  defaultBreakMin,
  onPlanChanged,
}: PlanTodayProps) {
  const [plan, setPlan] = useState<PlanSlot[]>([]);
  const [tree, setTree] = useState<PickerNode[]>([]);
  const [busy, setBusy] = useState(false);
  const [pickerFor, setPickerFor] = useState<number | null>(null);

  // Ответы приходят асинхронно и могут обгонять друг друга. Чтение применяется, только если оно самое
  // свежее и после его старта не завершилась запись (ответ записи всегда авторитетен).
  const readSeq = useRef(0);
  const writeEpoch = useRef(0);
  const onPlanChangedRef = useRef(onPlanChanged);
  onPlanChangedRef.current = onPlanChanged;

  const applyList = useCallback((slots: PlanSlot[] | null) => {
    const list = slots ?? [];
    setPlan(list);
    onPlanChangedRef.current(list);
  }, []);

  const applyWrite = useCallback(
    (slots: PlanSlot[] | null) => {
      writeEpoch.current++;
      applyList(slots);
    },
    [applyList],
  );

  const fetchPlan = useCallback(() => {
    const token = ++readSeq.current;
    const epoch = writeEpoch.current;
    api
      .getDayPlan()
      .then((slots) => {
        if (token !== readSeq.current || epoch !== writeEpoch.current) return;
        applyList(slots);
      })
      .catch(() => undefined);
  }, [applyList]);

  const loadTree = () => api.getPickerTree().then(setTree).catch(() => undefined);

  useEffect(() => {
    void loadTree();
  }, [version]);

  // Перечитываем план при событиях движка и при смене числа выполненных / фазы / паузы.
  useEffect(() => {
    fetchPlan();
  }, [fetchPlan, version, completedToday, phase, paused]);

  useEffect(() => {
    const id = window.setInterval(fetchPlan, REFETCH_MS);
    return () => window.clearInterval(id);
  }, [fetchPlan]);

  const refresh = () => {
    setBusy(true);
    Promise.all([api.refreshDayPlan(), loadTree()])
      .then(([slots]) => applyWrite(slots))
      .catch(() => undefined)
      .finally(() => setBusy(false));
  };

  const patchSlot = (idx: number, patch: SlotPatch) => {
    setPickerFor(null);
    setBusy(true);
    api
      .setPlanSlot(idx, patch)
      .then(applyWrite)
      .catch(() => undefined)
      .finally(() => setBusy(false));
  };

  const changeDuration = (idx: number, field: "focus_minutes" | "break_minutes", raw: string) => {
    const v = raw === "" ? -1 : Number(raw);
    if (Number.isNaN(v)) return;
    patchSlot(idx, { [field]: v });
  };

  if (plan.length === 0) {
    return null;
  }

  const focusing = phase === "focus";
  const idle = phase === "idle";
  const groups = buildGroups(plan);

  const slotText = (s: PlanSlot) => s.task?.title_snapshot || s.label || "—";

  const focusLock = (s: PlanSlot): string | null => {
    if (s.done) return "Помидор уже выполнен — длительность не изменить";
    if (focusing && s.idx === completedToday) return "Помидор идёт — длительность не изменить";
    return null;
  };

  // Перерыв, который движок возьмёт при следующем старте, — после последнего выполненного помидора.
  const breakLock = (s: PlanSlot): string | null => {
    if (!s.done) return null;
    if (idle && s.idx === completedToday - 1) return null;
    return "Помидор уже выполнен — перерыв после него не изменить";
  };

  return (
    <div data-testid="plan" className="rounded-2xl bg-surface p-3 shadow-sm sm:p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">План помидоров</h2>
        <button
          data-testid="plan-refresh"
          className="rounded-lg p-1.5 text-muted hover:bg-canvas hover:text-ink disabled:opacity-40"
          title="Перераспределить свободные слоты по задачам"
          onClick={refresh}
          disabled={busy}
        >
          <RefreshCw size={14} className={busy ? "animate-spin" : ""} />
        </button>
      </div>
      <div className="flex flex-col gap-3">
        {groups.map((g) => {
          const muted = g.kind === "overflow";
          const windowTotal = g.slots.length;
          const windowCount = g.windowTitle || g.windowId ? g.slots.filter((s) => isWindowSlot(s, g)).length : 0;
          const showCounter = g.kind === "period" && Boolean(g.windowTitle || g.windowId);
          return (
            <div
              key={g.key}
              data-testid="plan-group"
              data-kind={g.kind}
              data-window-id={g.windowId ?? ""}
              data-period-start={g.start ?? ""}
              data-period-end={g.end ?? ""}
              data-count={windowTotal}
            >
              <div className="mb-1 flex flex-wrap items-baseline justify-between gap-x-2">
                <p
                  data-testid="plan-group-title"
                  className={"text-xs " + (muted ? "font-medium text-amber-600" : "text-muted")}
                >
                  {groupTitle(g)}
                </p>
                {showCounter && (
                  <p
                    data-testid="plan-window-counter"
                    data-window-count={windowCount}
                    data-other-count={windowTotal - windowCount}
                    className="text-[11px] tabular-nums text-muted"
                  >
                    {g.windowTitle || "Окно"} {windowCount} из {windowTotal} · другие {windowTotal - windowCount}
                  </p>
                )}
              </div>
              <div className="flex flex-col gap-1">
                {g.slots.map((s) => {
                  const isActive = s.idx === completedToday;
                  const fLock = focusLock(s);
                  const bLock = breakLock(s);
                  return (
                    <div
                      key={s.idx}
                      data-testid="plan-slot"
                      data-idx={s.idx}
                      data-done={s.done}
                      data-overflow={s.overflow}
                      data-active={isActive}
                      data-start={s.start_minutes ?? ""}
                      data-end={s.end_minutes ?? ""}
                      data-window-id={s.window_id ?? ""}
                      className={
                        "flex items-center gap-1.5 rounded-lg px-1 py-0.5 " +
                        (s.done ? "opacity-45 " : "") +
                        (muted ? "opacity-70 " : "") +
                        // opacity создаёт свой контекст наложения: строку с открытым
                        // пикером поднимаем над соседними, иначе они перекроют список
                        (pickerFor === s.idx ? "relative z-30 " : "") +
                        (isActive ? "bg-primary/10 ring-1 ring-primary/40" : "")
                      }
                    >
                      <span
                        className={
                          "w-5 shrink-0 text-right text-xs tabular-nums " +
                          (isActive ? "font-semibold text-primary-dark" : "text-muted")
                        }
                      >
                        {isActive ? (focusing ? "▶" : "•") : s.idx + 1}
                      </span>
                      <span
                        data-testid="slot-start"
                        className={"w-10 shrink-0 text-xs tabular-nums " + (s.done ? "text-slate-400" : "text-muted")}
                      >
                        {s.start_minutes == null ? "" : fmtClock(s.start_minutes)}
                      </span>
                      <div className="relative min-w-0 flex-1">
                        <button
                          data-testid="slot-task"
                          className={
                            "flex w-full items-center gap-1 truncate rounded-lg border bg-transparent px-1.5 py-1 text-left text-xs outline-none disabled:opacity-60 " +
                            (s.pinned ? "border-primary/60" : "border-slate-200")
                          }
                          disabled={s.done || busy}
                          onClick={() => setPickerFor(pickerFor === s.idx ? null : s.idx)}
                        >
                          <span className={"min-w-0 flex-1 truncate " + (slotText(s) === "—" ? "text-muted" : "")}>
                            {slotText(s)}
                          </span>
                          <ChevronDown size={12} className="shrink-0 text-muted" />
                        </button>
                        {pickerFor === s.idx && (
                          <TaskTreePicker
                            tree={tree}
                            currentLabel={s.label}
                            onPick={(patch) => patchSlot(s.idx, patch)}
                            onClose={() => setPickerFor(null)}
                          />
                        )}
                      </div>
                      <input
                        data-testid="slot-focus"
                        className="w-10 shrink-0 rounded-lg border border-slate-200 bg-transparent px-1 py-1 text-center text-xs tabular-nums outline-none focus:border-primary"
                        type="number"
                        min="1"
                        title={fLock ?? "Длительность помидора, мин"}
                        placeholder={String(defaultFocusMin)}
                        value={s.focus_minutes ?? ""}
                        disabled={fLock != null || busy}
                        onChange={(e) => changeDuration(s.idx, "focus_minutes", e.target.value)}
                      />
                      <input
                        data-testid="slot-break"
                        className="w-10 shrink-0 rounded-lg border border-slate-200 bg-transparent px-1 py-1 text-center text-xs tabular-nums outline-none focus:border-primary"
                        type="number"
                        min="1"
                        title={
                          bLock ??
                          (s.done
                            ? "Перерыв после этого помидора, мин — его возьмёт ближайший старт перерыва"
                            : "Перерыв после, мин")
                        }
                        placeholder={String(defaultBreakMin)}
                        value={s.break_minutes ?? ""}
                        disabled={bLock != null || busy}
                        onChange={(e) => changeDuration(s.idx, "break_minutes", e.target.value)}
                      />
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
