import { useEffect, useState } from "react";
import { CheckCircle2, XCircle, Zap, Pencil } from "lucide-react";
import { api } from "../api";
import type { Binding, Session } from "../types";
import BindingPicker from "./BindingPicker";

interface SessionsTodayProps {
  version: number;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

function durationMinutes(s: Session): number {
  if (!s.ended_at) {
    return 0;
  }
  const ms = new Date(s.ended_at).getTime() - new Date(s.started_at).getTime();
  return Math.max(0, Math.round(ms / 60000 - s.paused_total_seconds / 60));
}

function bindingText(s: Session): string {
  if (s.task_title_snapshot) {
    return s.task_title_snapshot;
  }
  if (s.task_external_id) {
    return s.task_external_id;
  }
  return s.label ?? "без привязки";
}

function OutcomeIcon({ outcome }: { outcome: Session["outcome"] }) {
  if (outcome === "completed") {
    return <CheckCircle2 size={16} className="text-success" />;
  }
  if (outcome === "interrupted") {
    return <Zap size={16} className="text-muted" />;
  }
  return <XCircle size={16} className="text-danger" />;
}

export default function SessionsToday({ version }: SessionsTodayProps) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [editing, setEditing] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    api
      .listTodaySessions()
      .then((list) => setSessions((list ?? []).filter((s) => s.kind === "focus" && s.ended_at)))
      .catch(() => setError("Не удалось загрузить сессии"));
  };

  useEffect(load, [version]);

  const save = (id: string, b: Binding) => {
    api
      .relabel(id, b)
      .then(() => {
        setEditing(null);
        setError(null);
        load();
      })
      .catch(() => setError("Не удалось сохранить привязку"));
  };

  return (
    <div className="rounded-2xl bg-surface p-5 shadow-sm">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted">Помидоры сегодня</h2>
      {error && <p className="mb-2 text-xs text-danger">{error}</p>}
      {sessions.length === 0 ? (
        <p className="text-sm text-muted">Пока пусто — запустите первый помидор.</p>
      ) : (
        <ul className="space-y-2">
          {sessions.map((s) => (
            <li key={s.id} className="rounded-xl border border-slate-100 px-3 py-2">
              <div className="flex items-center gap-3">
                <OutcomeIcon outcome={s.outcome} />
                <span className="w-12 text-sm tabular-nums text-muted">{fmtTime(s.started_at)}</span>
                <span className="flex-1 truncate text-sm">{bindingText(s)}</span>
                <span className="text-xs text-muted">{durationMinutes(s)} мин</span>
                <button
                  className="text-muted hover:text-primary-dark"
                  title="Изменить привязку"
                  onClick={() => setEditing(editing === s.id ? null : s.id)}
                >
                  <Pencil size={15} />
                </button>
              </div>
              {editing === s.id && (
                <div className="mt-2 border-t border-slate-100 pt-2">
                  <BindingPicker compact value={{ label: null, task: null }} onChange={(b) => save(s.id, b)} />
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
