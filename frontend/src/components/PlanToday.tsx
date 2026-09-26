import { useEffect, useState } from "react";
import { ChevronDown, RefreshCw } from "lucide-react";
import { api } from "../api";
import type { PickerNode, PlanSlot, SlotPatch } from "../types";
import TaskTreePicker from "./TaskTreePicker";

interface PlanTodayProps {
  version: number;
  blocks: number[];
  activeIdx: number;
  focusing: boolean;
  defaultFocusMin: number;
  defaultBreakMin: number;
  onPlanChanged: (plan: PlanSlot[]) => void;
}

export default function PlanToday({ version, blocks, activeIdx, focusing, defaultFocusMin, defaultBreakMin, onPlanChanged }: PlanTodayProps) {
  const [plan, setPlan] = useState<PlanSlot[]>([]);
  const [tree, setTree] = useState<PickerNode[]>([]);
  const [busy, setBusy] = useState(false);
  const [pickerFor, setPickerFor] = useState<number | null>(null);

  const apply = (slots: PlanSlot[]) => {
    setPlan(slots);
    onPlanChanged(slots);
  };

  const loadTree = () => api.getPickerTree().then(setTree).catch(() => undefined);

  useEffect(() => {
    api.getDayPlan().then(apply).catch(() => undefined);
    void loadTree();
  }, [version]);

  const refresh = () => {
    setBusy(true);
    Promise.all([api.refreshDayPlan(), loadTree()])
      .then(([slots]) => apply(slots))
      .catch(() => undefined)
      .finally(() => setBusy(false));
  };

  const patchSlot = (idx: number, patch: SlotPatch) => {
    setPickerFor(null);
    setBusy(true);
    api
      .setPlanSlot(idx, patch)
      .then(apply)
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

  const committed = plan.filter((s) => !s.overflow);
  const overflow = plan.filter((s) => s.overflow);

  const sections: { key: string; title: string; slots: PlanSlot[]; muted: boolean }[] = [];
  let offset = 0;
  for (let bi = 0; offset < committed.length; bi++) {
    const size = bi < blocks.length ? blocks[bi] : committed.length - offset;
    sections.push({
      key: `block-${bi}`,
      title: `Пакет ${bi + 1}`,
      slots: committed.slice(offset, offset + size),
      muted: false,
    });
    offset += size;
  }
  if (overflow.length > 0) {
    sections.push({
      key: "overflow",
      title: `Не влезшие помидоры · ${overflow.length}`,
      slots: overflow,
      muted: true,
    });
  }

  const slotText = (s: PlanSlot) => s.task?.title_snapshot || s.label || "—";

  return (
    <div className="rounded-2xl bg-surface p-3 shadow-sm sm:p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">План помидоров</h2>
        <button
          className="rounded-lg p-1.5 text-muted hover:bg-canvas hover:text-ink disabled:opacity-40"
          title="Перераспределить свободные слоты по задачам"
          onClick={refresh}
          disabled={busy}
        >
          <RefreshCw size={14} className={busy ? "animate-spin" : ""} />
        </button>
      </div>
      <div className="flex flex-col gap-3">
        {sections.map(({ key, title, slots, muted }) => (
          <div key={key}>
            <p className={"mb-1 text-xs " + (muted ? "font-medium text-amber-600" : "text-muted")}>{title}</p>
            <div className="flex flex-col gap-1">
              {slots.map((s) => {
                const isActive = s.idx === activeIdx;
                return (
                  <div
                    key={s.idx}
                    className={
                      "flex items-center gap-1.5 rounded-lg px-1 py-0.5 " +
                      (s.done ? "opacity-45 " : "") +
                      (muted ? "opacity-70 " : "") +
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
                    <span className="w-10 shrink-0 text-xs tabular-nums text-muted">
                      {s.start_minutes == null
                        ? ""
                        : `${String(Math.floor(s.start_minutes / 60) % 24).padStart(2, "0")}:${String(s.start_minutes % 60).padStart(2, "0")}`}
                    </span>
                    <div className="relative min-w-0 flex-1">
                      <button
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
                      className="w-10 shrink-0 rounded-lg border border-slate-200 bg-transparent px-1 py-1 text-center text-xs tabular-nums outline-none focus:border-primary"
                      type="number"
                      min="1"
                      title="Длительность помидора, мин"
                      placeholder={String(defaultFocusMin)}
                      value={s.focus_minutes ?? ""}
                      disabled={s.done || busy}
                      onChange={(e) => changeDuration(s.idx, "focus_minutes", e.target.value)}
                    />
                    <input
                      className="w-10 shrink-0 rounded-lg border border-slate-200 bg-transparent px-1 py-1 text-center text-xs tabular-nums outline-none focus:border-primary"
                      type="number"
                      min="1"
                      title="Перерыв после, мин"
                      placeholder={String(defaultBreakMin)}
                      value={s.break_minutes ?? ""}
                      disabled={s.done || busy}
                      onChange={(e) => changeDuration(s.idx, "break_minutes", e.target.value)}
                    />
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
