import { useCallback, useEffect, useRef, useState } from "react";
import { Coffee, Minus, Pause, PictureInPicture2, Play, Settings as SettingsIcon, Square, Timer, X } from "lucide-react";
import { api, onModeChange, onStatePush, playChime } from "./api";
import TimerRing from "./components/TimerRing";
import DayProgress from "./components/DayProgress";
import BindingPicker from "./components/BindingPicker";
import SessionsToday from "./components/SessionsToday";
import SettingsView from "./components/SettingsView";
import Overlay from "./components/Overlay";
import type { Binding, Settings, State } from "./types";

function fmt(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

function phaseTitle(state: State): string {
  switch (state.phase) {
    case "focus":
      return `Фокус · помидор ${state.pos_in_block + 1} из ${state.block_size} (блок ${state.block_index + 1})`;
    case "short_break":
      return "Короткий перерыв";
    case "long_break":
      return "Длинный перерыв";
    default:
      return state.next_phase === "focus"
        ? "Готов к фокусу"
        : state.next_phase === "long_break"
          ? "Дальше длинный перерыв"
          : "Дальше короткий перерыв";
  }
}

function useCountdown(state: State | null): number {
  const [remaining, setRemaining] = useState(0);
  const anchor = useRef<{ at: number; value: number }>({ at: 0, value: 0 });

  useEffect(() => {
    if (!state) {
      return;
    }
    anchor.current = { at: Date.now(), value: state.remaining_seconds };
    setRemaining(state.remaining_seconds);
    if (state.phase === "idle" || state.paused) {
      return;
    }
    const id = window.setInterval(() => {
      const elapsed = (Date.now() - anchor.current.at) / 1000;
      setRemaining(Math.max(0, Math.round(anchor.current.value - elapsed)));
    }, 250);
    return () => window.clearInterval(id);
  }, [state]);

  return remaining;
}

export default function App() {
  const [state, setState] = useState<State | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [mode, setMode] = useState<"main" | "overlay">("main");
  const [view, setView] = useState<"timer" | "settings">("timer");
  const [binding, setBinding] = useState<Binding>({ label: null, task: null });
  const [error, setError] = useState<string | null>(null);
  const [sessionsVersion, setSessionsVersion] = useState(0);
  const settingsRef = useRef<Settings | null>(null);
  settingsRef.current = settings;

  const remaining = useCountdown(state);

  useEffect(() => {
    let cancelled = false;
    let offState = () => {};
    let offMode = () => {};
    const subscribe = () => {
      offState = onStatePush((push) => {
        setState(push.state);
        if (push.reason === "completed" || push.reason === "stopped" || push.reason === "relabeled" || push.reason === "started") {
          setSessionsVersion((v) => v + 1);
        }
        if (push.reason === "completed" && push.state.sound_enabled) {
          playChime(settingsRef.current?.sound_file ?? null);
        }
        if (push.reason === "settings") {
          api.getSettings().then(setSettings).catch(() => undefined);
        }
      });
      offMode = onModeChange((m) => {
        setMode(m);
        document.documentElement.classList.toggle("overlay", m === "overlay");
      });
    };
    const load = () => {
      Promise.resolve()
        .then(() => Promise.all([api.getState(), api.getSettings()]))
        .then(([st, se]) => {
          if (cancelled) return;
          subscribe();
          setState(st);
          setSettings(se);
          setError(null);
        })
        .catch(() => {
          if (cancelled) return;
          setError("Ядро приложения запускается...");
          window.setTimeout(() => {
            if (!cancelled) load();
          }, 1000);
        });
    };
    load();
    return () => {
      cancelled = true;
      offState();
      offMode();
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "F9") {
        e.preventDefault();
        void (mode === "overlay" ? api.exitOverlay() : api.enterOverlay());
      }
      if (e.key === "Escape" && mode === "overlay") {
        void api.exitOverlay();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [mode]);

  const run = useCallback((p: Promise<unknown>) => {
    p.then(() => setError(null)).catch((e) => setError(String(e)));
  }, []);

  if (!state || !settings) {
    return <div className="flex h-screen items-center justify-center text-muted">{error ?? "Загрузка..."}</div>;
  }

  if (mode === "overlay") {
    return <Overlay state={state} remaining={remaining} settings={settings} />;
  }

  const active = state.phase !== "idle";
  const isBreak = state.phase === "short_break" || state.phase === "long_break";
  const fraction = active && state.planned_seconds > 0 ? remaining / state.planned_seconds : 1;

  return (
    <div className="flex h-screen flex-col">
      <header className="draggable flex select-none items-center gap-3 border-b border-slate-200 bg-surface px-4 py-2">
        <Timer size={18} className="text-primary" />
        <span className="text-sm font-semibold">Pomodoro</span>
        <nav className="no-drag ml-6 flex gap-1">
          <button
            className={
              "rounded-lg px-3 py-1 text-sm " +
              (view === "timer" ? "bg-primary/10 text-primary-dark" : "text-muted hover:text-ink")
            }
            onClick={() => setView("timer")}
          >
            Таймер
          </button>
          <button
            className={
              "flex items-center gap-1 rounded-lg px-3 py-1 text-sm " +
              (view === "settings" ? "bg-primary/10 text-primary-dark" : "text-muted hover:text-ink")
            }
            onClick={() => setView("settings")}
          >
            <SettingsIcon size={14} /> Настройки
          </button>
        </nav>
        <div className="no-drag ml-auto flex items-center gap-1">
          <button className="rounded-lg p-1.5 text-muted hover:bg-canvas" onClick={() => void api.minimise()}>
            <Minus size={16} />
          </button>
          <button className="rounded-lg p-1.5 text-muted hover:bg-danger hover:text-white" onClick={() => void api.quit()}>
            <X size={16} />
          </button>
        </div>
      </header>

      <main className="flex-1 overflow-y-auto p-6">
        {error && (
          <div className="mx-auto mb-4 max-w-3xl rounded-xl bg-danger/10 px-4 py-2 text-sm text-danger">{error}</div>
        )}
        {view === "settings" ? (
          <div className="mx-auto max-w-3xl">
            <SettingsView settings={settings} onSaved={setSettings} />
          </div>
        ) : (
          <div className="mx-auto grid max-w-4xl grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
            <div className="flex flex-col items-center gap-6 rounded-2xl bg-surface p-8 shadow-sm">
              <p className="text-sm font-medium text-muted">{phaseTitle(state)}</p>
              <TimerRing size={280} stroke={14} fraction={fraction} color={isBreak ? "#5DC9E2" : "#00ADD8"}>
                <span className="text-6xl font-semibold tabular-nums tracking-tight">
                  {active ? fmt(remaining) : fmt(state.next_phase === "focus" ? settings.focus_duration_seconds : state.next_phase === "long_break" ? settings.long_break_seconds : settings.short_break_seconds)}
                </span>
                {state.paused && <span className="mt-1 text-sm font-medium text-muted">пауза</span>}
                {active && (state.label || state.task) && (
                  <span className="mt-1 max-w-44 truncate text-xs text-muted">
                    {state.task ? state.task.title_snapshot || state.task.external_id : state.label}
                  </span>
                )}
              </TimerRing>

              <div className="flex items-center gap-3">
                {!active ? (
                  <>
                    <button
                      className="flex items-center gap-2 rounded-xl bg-primary px-6 py-3 font-medium text-white shadow hover:bg-primary-dark"
                      onClick={() => run(api.startFocus(binding))}
                    >
                      <Play size={18} /> Фокус
                    </button>
                    <button
                      className="flex items-center gap-2 rounded-xl border border-slate-200 px-5 py-3 text-sm text-muted hover:border-primary hover:text-primary-dark"
                      onClick={() => run(api.startBreak())}
                    >
                      <Coffee size={16} /> Перерыв
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      className="flex items-center gap-2 rounded-xl bg-primary px-6 py-3 font-medium text-white shadow hover:bg-primary-dark"
                      onClick={() => run(state.paused ? api.resume() : api.pause())}
                    >
                      {state.paused ? <Play size={18} /> : <Pause size={18} />}
                      {state.paused ? "Продолжить" : "Пауза"}
                    </button>
                    <button
                      className="flex items-center gap-2 rounded-xl border border-danger/40 px-5 py-3 text-sm text-danger hover:bg-danger hover:text-white"
                      onClick={() => run(api.stop(""))}
                    >
                      <Square size={16} /> Стоп
                    </button>
                    <button
                      className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-3 text-sm text-muted hover:border-primary hover:text-primary-dark"
                      title="Свернуть в оверлей поверх всех окон"
                      onClick={() => run(api.enterOverlay())}
                    >
                      <PictureInPicture2 size={16} /> Оверлей
                    </button>
                  </>
                )}
              </div>

              <div className="w-full border-t border-slate-100 pt-5">
                <p className="mb-3 text-center text-xs text-muted">
                  Сегодня: {state.completed_today} из {state.day_total}
                  {state.day_complete && " — план дня выполнен!"}
                </p>
                <DayProgress blocks={state.day_blocks} completed={state.completed_today} focusActive={state.phase === "focus"} />
              </div>
            </div>

            <div className="flex flex-col gap-4">
              <div className="rounded-2xl bg-surface p-5 shadow-sm">
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted">Что делаю</h2>
                {active ? (
                  <BindingPicker
                    value={{ label: state.label, task: state.task }}
                    onChange={(b) => {
                      if (state.session_id) {
                        run(api.relabel(state.session_id, b));
                      }
                    }}
                  />
                ) : (
                  <BindingPicker value={binding} onChange={setBinding} />
                )}
              </div>
              <SessionsToday version={sessionsVersion} />
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
