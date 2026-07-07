import { useEffect, useState } from "react";
import { Tag, ListTodo, X } from "lucide-react";
import { api } from "../api";
import type { Binding, TaskOption } from "../types";

interface BindingPickerProps {
  value: Binding;
  onChange: (b: Binding) => void;
  compact?: boolean;
}

export default function BindingPicker({ value, onChange, compact = false }: BindingPickerProps) {
  const [mode, setMode] = useState<"label" | "task">(value.task ? "task" : "label");
  const [query, setQuery] = useState("");
  const [options, setOptions] = useState<TaskOption[]>([]);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [searching, setSearching] = useState(false);

  useEffect(() => {
    if (mode !== "task") {
      return;
    }
    const timer = window.setTimeout(() => {
      setSearching(true);
      api
        .searchTasks(query)
        .then((found) => {
          setOptions(found ?? []);
          setSearchError(null);
        })
        .catch(() => {
          setOptions([]);
          setSearchError("Источник задач недоступен");
        })
        .finally(() => setSearching(false));
    }, 300);
    return () => window.clearTimeout(timer);
  }, [mode, query]);

  const selectedText = value.task ? value.task.title_snapshot || value.task.external_id : value.label;

  if (selectedText) {
    return (
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-3 py-1.5 text-sm text-primary-dark">
          {value.task ? <ListTodo size={14} /> : <Tag size={14} />}
          <span className="max-w-56 truncate">{selectedText}</span>
          <button
            className="text-muted hover:text-danger"
            onClick={() => onChange({ label: null, task: null })}
            title="Убрать привязку"
          >
            <X size={14} />
          </button>
        </span>
      </div>
    );
  }

  return (
    <div className={compact ? "space-y-2" : "space-y-3"}>
      <div className="flex gap-1 rounded-xl bg-canvas p-1">
        <button
          className={
            "flex-1 rounded-lg px-3 py-1.5 text-sm transition " +
            (mode === "label" ? "bg-surface shadow text-ink" : "text-muted hover:text-ink")
          }
          onClick={() => setMode("label")}
        >
          Метка
        </button>
        <button
          className={
            "flex-1 rounded-lg px-3 py-1.5 text-sm transition " +
            (mode === "task" ? "bg-surface shadow text-ink" : "text-muted hover:text-ink")
          }
          onClick={() => setMode("task")}
        >
          Задача
        </button>
      </div>
      {mode === "label" ? (
        <input
          className="w-full rounded-xl border border-slate-200 bg-surface px-3 py-2 text-sm outline-none focus:border-primary"
          placeholder="Чем займётесь? (Enter — сохранить)"
          onKeyDown={(e) => {
            const text = (e.target as HTMLInputElement).value.trim();
            if (e.key === "Enter" && text) {
              onChange({ label: text, task: null });
            }
          }}
        />
      ) : (
        <div className="space-y-2">
          <input
            className="w-full rounded-xl border border-slate-200 bg-surface px-3 py-2 text-sm outline-none focus:border-primary"
            placeholder="Поиск задачи..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {searchError ? (
            <p className="text-xs text-danger">{searchError}</p>
          ) : searching ? (
            <p className="text-xs text-muted">Ищу...</p>
          ) : (
            <ul className="max-h-40 space-y-1 overflow-y-auto">
              {options.map((o) => (
                <li key={o.source + o.external_id}>
                  <button
                    className="w-full rounded-lg px-3 py-1.5 text-left text-sm hover:bg-primary/10"
                    onClick={() =>
                      onChange({
                        label: null,
                        task: { source: o.source, external_id: o.external_id, title_snapshot: o.title },
                      })
                    }
                  >
                    {o.title}
                  </button>
                </li>
              ))}
              {options.length === 0 && <li className="px-3 py-1.5 text-xs text-muted">Ничего не найдено</li>}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
