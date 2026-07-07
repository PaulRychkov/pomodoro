import { Pause, Play, Square } from "lucide-react";
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

  return (
    <div className="draggable group flex h-screen w-screen select-none items-center justify-center">
      <div
        className="no-drag relative"
        style={{ width: o.size, height: o.size }}
        onDoubleClick={() => void api.exitOverlay()}
        title="Двойной клик или F9 — развернуть"
      >
        <div style={{ opacity: o.circle_opacity }}>
          <TimerPie size={o.size} fraction={fraction} color={color} />
        </div>
        {o.show_time && active && (
          <div
            className="pointer-events-none absolute inset-0 flex items-center justify-center"
            style={{ opacity: o.digits_opacity }}
          >
            <span
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
          <div
            className="absolute inset-x-0 -bottom-2 flex justify-center gap-2 opacity-0 transition group-hover:opacity-100"
          >
            <div className="flex gap-2" style={{ opacity: o.buttons_opacity }}>
              <button
                className="rounded-full bg-slate-800/80 p-1.5 text-white hover:bg-primary"
                onClick={() => void (state.paused ? api.resume() : api.pause())}
              >
                {state.paused ? <Play size={14} /> : <Pause size={14} />}
              </button>
              <button
                className="rounded-full bg-slate-800/80 p-1.5 text-white hover:bg-danger"
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
