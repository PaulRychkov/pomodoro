import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight, FolderTree, ListChecks, X } from "lucide-react";
import type { PickerNode, SlotPatch } from "../types";

interface Props {
  tree: PickerNode[];
  currentLabel: string | null;
  onPick: (patch: SlotPatch) => void;
  onClose: () => void;
}

function bindingFor(node: PickerNode): SlotPatch {
  if (node.bind_task_id) {
    return { task: { source: node.source || "tasks", external_id: node.bind_task_id, title_snapshot: node.name } };
  }
  return { label: node.name };
}

function TreeRow({ node, depth, onPick }: { node: PickerNode; depth: number; onPick: (p: SlotPatch) => void }) {
  const [open, setOpen] = useState(true);
  const kids = node.children ?? [];
  const hasKids = kids.length > 0;
  const isTask = node.kind === "task";
  return (
    <div>
      <div className="flex items-center gap-1" style={{ paddingLeft: `${depth * 14}px` }}>
        <button
          className="flex h-5 w-5 shrink-0 items-center justify-center text-muted hover:text-ink disabled:opacity-0"
          onClick={() => setOpen((o) => !o)}
          disabled={!hasKids}
        >
          {hasKids ? (open ? <ChevronDown size={13} /> : <ChevronRight size={13} />) : null}
        </button>
        <button
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-xs hover:bg-primary/10"
          onClick={() => onPick(bindingFor(node))}
          title={isTask ? "Задача — время идёт в неё" : node.bind_task_id ? "Тема — время идёт в общую задачу" : "Тема — как метка"}
        >
          {isTask ? (
            <ListChecks size={13} className="shrink-0 text-primary" />
          ) : (
            <FolderTree size={13} className="shrink-0 text-muted" />
          )}
          <span className={"min-w-0 truncate " + (isTask ? "" : "font-medium")}>{node.name}</span>
          {node.today && (
            <span className="ml-auto shrink-0 rounded bg-primary/15 px-1 text-[9px] font-semibold uppercase text-primary-dark">
              сег
            </span>
          )}
        </button>
      </div>
      {open && hasKids && (
        <div>
          {kids.map((c) => (
            <TreeRow key={`${c.kind}:${c.id}`} node={c} depth={depth + 1} onPick={onPick} />
          ))}
        </div>
      )}
    </div>
  );
}

export default function TaskTreePicker({ tree, currentLabel, onPick, onClose }: Props) {
  const [label, setLabel] = useState(currentLabel ?? "");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const submitLabel = () => {
    const t = label.trim();
    if (t) onPick({ label: t });
  };

  return (
    <div
      ref={ref}
      className="absolute left-0 top-full z-30 mt-1 max-h-80 w-72 overflow-y-auto rounded-xl border border-slate-200 bg-white p-2 shadow-xl"
    >
      <div className="mb-1 flex items-center justify-between px-1">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-muted">Что делаю</span>
        <button className="text-muted hover:text-ink" onClick={onClose}>
          <X size={14} />
        </button>
      </div>
      <button
        className="mb-1 w-full rounded-md px-2 py-1 text-left text-xs text-muted hover:bg-slate-100"
        onClick={() => onPick({ clear_binding: true })}
      >
        — очистить слот
      </button>
      <div className="border-t border-slate-100 pt-1">
        {tree.length === 0 && <div className="px-2 py-3 text-center text-xs text-muted">Нет задач на сегодня</div>}
        {tree.map((n) => (
          <TreeRow key={`${n.kind}:${n.id}`} node={n} depth={0} onPick={onPick} />
        ))}
      </div>
      <div className="mt-1 flex gap-1 border-t border-slate-100 pt-2">
        <input
          className="min-w-0 flex-1 rounded-md border border-slate-200 px-2 py-1 text-xs outline-none focus:border-primary"
          placeholder="Своя метка…"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submitLabel();
          }}
        />
        <button
          className="rounded-md bg-primary px-2 py-1 text-xs font-medium text-white hover:bg-primary-dark disabled:opacity-40"
          onClick={submitLabel}
          disabled={!label.trim()}
        >
          OK
        </button>
      </div>
    </div>
  );
}
