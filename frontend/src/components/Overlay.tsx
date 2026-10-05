import { CheckCheck, Pause, Play, Square } from "lucide-react";
import { api } from "../api";
import TimerPie from "./TimerPie";
import type { Settings, State } from "../types";

interface OverlayProps {
  state: State;
  remaining: number;
  settings: Settings;
}

function fmt(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export default function Overlay({ state, remaining, settings }: OverlayProps) {
  const o = settings.overlay;
  const active = state.phase !== "idle";
  const fraction = active && state.planned_seconds > 0 ? remaining / state.planned_seconds : 0;
  const isBreak = state.phase === "short_break" || state.phase === "long_break";
  const color = isBreak ? "#5DC9E2" : "#00ADD8";
  const taskTitle = state.task ? state.task.title_snapshot || state.task.external_id : state.label;
  const hint = isBreak ? "Перерыв" : taskTitle ?? "Фокус";

  return (
    <div data-testid="overlay" className="draggable group flex h-screen w-screen select-none items-center justify-center">
      <div
        className="draggable relative"
        style={{ width: o.size, height: o.size }}
        onDoubleClick={() => void api.exitOverlay()}
        title={`${hint} · тяни мышью, чтобы двигать · двойной клик или F9 — развернуть`}
      >
        <div className="draggable" style={{ opacity: o.circle_opacity }}>
          <TimerPie size={o.size} fraction={fraction} color={color} />
        </div>
        {active && taskTitle && (
          <div className="pointer-events-none absolute inset-x-1 top-1 flex justify-center opacity-0 transition group-hover:opacity-100">
            <span data-testid="overlay-task" className="max-w-full truncate rounded bg-slate-800/85 px-1.5 py-0.5 text-[10px] font-medium text-white">
              {taskTitle}
            </span>
          </div>
        )}
        {o.show_time && active && (
          <div
            className="pointer-events-none absolute inset-0 flex items-center justify-center"
            style={{ opacity: o.digits_opacity }}
          >
            <span
              data-testid="overlay-digits"
              className="font-semibold tabular-nums"
              style={{
                fontSize: o.digits_size,
                color: "#0F172A",
                textShadow: "0 0 8px rgba(255,255,255,0.95), 0 1px 3px rgba(255,255,255,0.95)",
              }}
            >
              {state.paused ? "⏸ " : ""}
              {fmt(remaining)}
            </span>
          </div>
        )}
        {active && (
          <div className="no-drag absolute inset-x-0 bottom-1 flex justify-center gap-2 opacity-0 transition group-hover:opacity-100">
            <div className="flex gap-2" style={{ opacity: o.buttons_opacity }}>
              <button
                data-testid="overlay-btn-pause"
                className="rounded-full bg-slate-800/80 p-1.5 text-white hover:bg-primary"
                onClick={() => void (state.paused ? api.resume() : api.pause())}
              >
                {state.paused ? <Play size={14} /> : <Pause size={14} />}
              </button>
              {state.phase === "focus" && (
                <button
                  data-testid="overlay-btn-complete"
                  className="rounded-full bg-slate-800/80 p-1.5 text-white hover:bg-primary"
                  title="Завершить с частичным зачётом"
                  onClick={() => void api.stop("completed")}
                >
                  <CheckCheck size={14} />
                </button>
              )}
              <button
                data-testid="overlay-btn-stop"
                className="rounded-full bg-slate-800/80 p-1.5 text-white hover:bg-danger"
                title="Стоп без зачёта"
                onClick={() => void api.stop("")}
              >
                <Square size={14} />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
