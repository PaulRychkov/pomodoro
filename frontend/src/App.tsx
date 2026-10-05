import { useCallback, useEffect, useRef, useState } from "react";
import { CheckCheck, Coffee, Minus, Pause, PictureInPicture2, Play, RefreshCw, Settings as SettingsIcon, Square, Timer, X } from "lucide-react";
import { api, isDesktop, onModeChange, onStatePush, playChime } from "./api";
import { creditLabel, creditTwelfths, formatCreditSum } from "./credit";
import TimerRing from "./components/TimerRing";
import DayProgress from "./components/DayProgress";
import SessionsToday from "./components/SessionsToday";
import SettingsView from "./components/SettingsView";
import PlanToday from "./components/PlanToday";
import Overlay from "./components/Overlay";
import { fmtClock } from "./time";
import type { Binding, PlanSlot, Settings, State } from "./types";

function slotTitle(slot: PlanSlot | undefined): string | null {
  if (!slot) return null;
  if (slot.task) return slot.task.title_snapshot || slot.task.external_id;
  return slot.label ?? null;
}

// Причины push-события, после которых нужно перечитать список помидоров и план дня.
const SESSION_REASONS = new Set(["completed", "stopped", "relabeled", "started", "paused", "resumed", "day_rolled"]);

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

const androidHost = (window as unknown as { AndroidHost?: { openSync: () => void } }).AndroidHost

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
  const stampRef = useRef<string>("");

  const remaining = useCountdown(state);

  useEffect(() => {
    let cancelled = false;
    let offState = () => {};
    let offMode = () => {};
    const subscribe = () => {
      offState = onStatePush((push) => {
        setState(push.state);
        let settingsChanged = push.reason === "settings";
        if (push.state.settings_stamp && push.state.settings_stamp !== stampRef.current) {
          const first = stampRef.current === "";
          stampRef.current = push.state.settings_stamp;
          if (!first) {
            settingsChanged = true;
          }
        }
        if (settingsChanged) {
          api.getSettings().then(setSettings).catch(() => undefined);
        }
        if (settingsChanged || SESSION_REASONS.has(push.reason)) {
          setSessionsVersion((v) => v + 1);
        }
        if (push.reason === "completed" && push.state.sound_enabled) {
          playChime(settingsRef.current?.sound_file ?? null);
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
          stampRef.current = st.settings_stamp ?? "";
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
    if (mode === "overlay" && state && state.phase === "idle") {
      void api.exitOverlay();
    }
  }, [mode, state]);

  const [plan, setPlan] = useState<PlanSlot[]>([]);

  const activeSlot = state ? plan.find((s) => s.idx === state.completed_today) : undefined;

  useEffect(() => {
    if (!state || state.phase !== "idle") {
      return;
    }
    setBinding({ label: activeSlot?.label ?? null, task: activeSlot?.task ?? null });
  }, [state, activeSlot]);

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
    return (
      <div data-testid="app-loading" className="flex h-screen items-center justify-center text-muted">
        {error ?? "Загрузка..."}
      </div>
    );
  }

  if (mode === "overlay") {
    return <Overlay state={state} remaining={remaining} settings={settings} />;
  }

  const active = state.phase !== "idle";
  const isBreak = state.phase === "short_break" || state.phase === "long_break";
  const fraction = active && state.planned_seconds > 0 ? remaining / state.planned_seconds : 1;
  const focusing = state.phase === "focus";
  const earnedNow = focusing ? creditTwelfths(state.planned_seconds - remaining, state.planned_seconds) : 0;
  // В idle длительность следующей сессии отдаёт движок по плану дня; настройки — только запасной вариант.
  const idleSeconds =
    state.next_planned_seconds > 0
      ? state.next_planned_seconds
      : state.next_phase === "focus"
        ? settings.focus_duration_seconds
        : state.next_phase === "long_break"
          ? settings.long_break_seconds
          : settings.short_break_seconds;
  const nextStart = !active && activeSlot && activeSlot.start_minutes != null ? fmtClock(activeSlot.start_minutes) : null;

  return (
    <div className="flex h-screen flex-col">
      <header className="draggable flex select-none items-center gap-2 border-b border-slate-200 bg-surface px-3 py-2 pt-[max(0.5rem,env(safe-area-inset-top))] sm:gap-3 sm:px-4">
        <Timer size={18} className="shrink-0 text-primary" />
        <span className="hidden text-sm font-semibold sm:inline">Pomodoro</span>
        <nav className="no-drag flex gap-1 sm:ml-6">
          <button
            data-testid="nav-timer"
            className={
              "rounded-lg px-3 py-1 text-sm " +
              (view === "timer" ? "bg-primary/10 text-primary-dark" : "text-muted hover:text-ink")
            }
            onClick={() => setView("timer")}
          >
            Таймер
          </button>
          <button
            data-testid="nav-settings"
            className={
              "flex items-center gap-1 rounded-lg px-3 py-1 text-sm " +
              (view === "settings" ? "bg-primary/10 text-primary-dark" : "text-muted hover:text-ink")
            }
            onClick={() => setView("settings")}
          >
            <SettingsIcon size={14} /> Настройки
          </button>
        </nav>
        {androidHost && (
          <button
            data-testid="btn-sync"
            className="no-drag ml-auto rounded-lg p-1.5 text-muted hover:bg-canvas"
            title="Синхронизация"
            onClick={() => androidHost.openSync()}
          >
            <RefreshCw size={16} />
          </button>
        )}
        {isDesktop && (
          <div className="no-drag ml-auto flex items-center gap-1">
            <button data-testid="btn-minimise" className="rounded-lg p-1.5 text-muted hover:bg-canvas" onClick={() => void api.minimise()}>
              <Minus size={16} />
            </button>
            <button data-testid="btn-quit" className="rounded-lg p-1.5 text-muted hover:bg-danger hover:text-white" onClick={() => void api.quit()}>
              <X size={16} />
            </button>
          </div>
        )}
      </header>

      <main className="flex-1 overflow-y-auto p-3 sm:p-6">
        {error && (
          <div data-testid="app-error" className="mx-auto mb-4 max-w-3xl rounded-xl bg-danger/10 px-4 py-2 text-sm text-danger">
            {error}
          </div>
        )}
        {view === "settings" ? (
          <div className="mx-auto max-w-3xl">
            <SettingsView
              settings={settings}
              onSaved={(s) => {
                setSettings(s);
                setSessionsVersion((v) => v + 1);
              }}
            />
          </div>
        ) : (
          <div className="mx-auto grid max-w-4xl grid-cols-1 gap-4 sm:gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
            <div className="flex flex-col items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm sm:gap-5 sm:p-8">
              <p data-testid="phase-title" data-phase={state.phase} className="text-sm font-medium text-muted">
                {phaseTitle(state)}
              </p>
              {(() => {
                const focusing = state.phase === "focus";
                const nowTitle = focusing
                  ? state.task
                    ? state.task.title_snapshot || state.task.external_id
                    : state.label
                  : slotTitle(activeSlot);
                if (isBreak) {
                  return (
                    <p data-testid="now-title" data-kind="break" className="text-lg font-semibold text-slate-400">
                      Отдыхай
                    </p>
                  );
                }
                if (!nowTitle) {
                  return (
                    <p data-testid="now-title" data-kind="free" className="text-lg font-semibold text-slate-400">
                      Свободный помидор
                    </p>
                  );
                }
                return (
                  <div className="w-full min-w-0 text-center">
                    <p data-testid="now-label" className="text-[11px] uppercase tracking-wide text-muted">
                      {focusing ? "Сейчас" : "Следующее"}
                    </p>
                    <p
                      data-testid="now-title"
                      data-kind={focusing ? "now" : "next"}
                      className="mx-auto max-w-full truncate px-2 text-xl font-semibold text-ink"
                    >
                      {nowTitle}
                    </p>
                  </div>
                );
              })()}
              <TimerRing responsive size={260} stroke={14} fraction={fraction} color={isBreak ? "#5DC9E2" : "#00ADD8"}>
                <span
                  data-testid="timer-digits"
                  className="text-[clamp(2.25rem,16vw,3.75rem)] font-semibold tabular-nums tracking-tight"
                >
                  {active ? fmt(remaining) : fmt(idleSeconds)}
                </span>
                {state.paused && (
                  <span data-testid="paused-label" className="mt-1 text-sm font-medium text-muted">
                    пауза
                  </span>
                )}
              </TimerRing>
              {nextStart && (
                <p data-testid="next-start" data-start={activeSlot?.start_minutes ?? ""} className="-mt-2 text-xs text-muted">
                  {state.next_phase === "focus" ? `по плану в ${nextStart}` : `следующий помидор по плану в ${nextStart}`}
                </p>
              )}

              <div className="flex w-full flex-wrap items-center justify-center gap-2 sm:gap-3">
                {!active ? (
                  <>
                    <button
                      data-testid="btn-focus"
                      className="flex items-center gap-2 rounded-xl bg-primary px-6 py-3 font-medium text-white shadow hover:bg-primary-dark"
                      onClick={() => run(api.startFocus(binding))}
                    >
                      <Play size={18} /> Фокус
                    </button>
                    <button
                      data-testid="btn-break"
                      className="flex items-center gap-2 rounded-xl border border-slate-200 px-5 py-3 text-sm text-muted hover:border-primary hover:text-primary-dark"
                      onClick={() => run(api.startBreak())}
                    >
                      <Coffee size={16} /> Перерыв
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      data-testid="btn-pause"
                      data-paused={state.paused}
                      className="flex items-center gap-2 rounded-xl bg-primary px-6 py-3 font-medium text-white shadow hover:bg-primary-dark"
                      onClick={() => run(state.paused ? api.resume() : api.pause())}
                    >
                      {state.paused ? <Play size={18} /> : <Pause size={18} />}
                      {state.paused ? "Продолжить" : "Пауза"}
                    </button>
                    {focusing && (
                      <button
                        data-testid="btn-complete"
                        className="flex items-center gap-2 rounded-xl border border-primary/50 px-5 py-3 text-sm text-primary-dark hover:bg-primary hover:text-white"
                        title="Закончить помидор сейчас: засчитается доля по фактическому фокусу"
                        onClick={() => run(api.stop("completed"))}
                      >
                        <CheckCheck size={16} /> Завершить
                      </button>
                    )}
                    <button
                      data-testid="btn-stop"
                      className="flex items-center gap-2 rounded-xl border border-danger/40 px-5 py-3 text-sm text-danger hover:bg-danger hover:text-white"
                      title={focusing ? "Бросить помидор без зачёта" : "Закончить перерыв"}
                      onClick={() => run(api.stop(""))}
                    >
                      <Square size={16} /> Стоп
                    </button>
                    {isDesktop && (
                      <button
                        data-testid="btn-overlay"
                        className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-3 text-sm text-muted hover:border-primary hover:text-primary-dark"
                        title="Свернуть в оверлей поверх всех окон"
                        onClick={() => run(api.enterOverlay())}
                      >
                        <PictureInPicture2 size={16} /> Оверлей
                      </button>
                    )}
                  </>
                )}
              </div>
              {focusing && !state.paused && (
                <p data-testid="credit-hint" data-twelfths={earnedNow} className="-mt-2 text-xs text-muted">
                  Завершить сейчас — засчитается {creditLabel(earnedNow)}
                  {earnedNow === 0 ? " (помидор не засчитается)" : " помидора"}
                </p>
              )}

              <div className="w-full border-t border-slate-100 pt-5">
                <p
                  data-testid="day-counter"
                  data-completed={state.completed_today}
                  data-total={state.day_total}
                  className="mb-3 text-center text-xs text-muted"
                >
                  Сегодня: {state.completed_today} из {state.day_total}
                  {state.credit_today !== state.completed_today && ` · засчитано ${formatCreditSum(state.credit_today)}`}
                  {state.day_complete && " — план дня выполнен!"}
                </p>
                <DayProgress blocks={state.day_blocks} completed={state.completed_today} focusActive={state.phase === "focus"} />
              </div>
            </div>

            <div className="flex flex-col gap-4">
              <PlanToday
                version={sessionsVersion}
                completedToday={state.completed_today}
                phase={state.phase}
                paused={state.paused}
                defaultFocusMin={Math.round(settings.focus_duration_seconds / 60)}
                defaultBreakMin={Math.round(settings.short_break_seconds / 60)}
                onPlanChanged={setPlan}
              />
              <SessionsToday version={sessionsVersion} />
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
