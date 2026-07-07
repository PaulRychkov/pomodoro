import { useEffect, useState } from "react";
import { Music, Play, Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, playChime } from "../api";
import type { Settings } from "../types";

interface SettingsViewProps {
  settings: Settings;
  onSaved: (s: Settings) => void;
}

function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4 py-1">
      <span className="text-sm">{label}</span>
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className={
          "relative h-6 w-11 rounded-full transition " + (checked ? "bg-primary" : "bg-slate-300")
        }
      >
        <span
          className={
            "absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all " +
            (checked ? "left-[22px]" : "left-0.5")
          }
        />
      </button>
    </label>
  );
}

function MinutesInput({ value, onChange, label }: { value: number; onChange: (seconds: number) => void; label: string }) {
  return (
    <label className="flex items-center justify-between gap-4 py-1">
      <span className="text-sm">{label}</span>
      <div className="flex items-center gap-2">
        <input
          type="number"
          min={1}
          max={180}
          className="w-20 rounded-xl border border-slate-200 px-3 py-1.5 text-right text-sm outline-none focus:border-primary"
          value={Math.round(value / 60)}
          onChange={(e) => onChange(Math.min(180, Math.max(1, Number(e.target.value) || 1)) * 60)}
        />
        <span className="w-8 text-xs text-muted">мин</span>
      </div>
    </label>
  );
}

export default function SettingsView({ settings, onSaved }: SettingsViewProps) {
  const [form, setForm] = useState<Settings>(settings);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => setForm(settings), [settings]);

  const patch = (p: Partial<Settings>) => setForm((f) => ({ ...f, ...p }));

  const save = () => {
    api
      .saveSettings(form)
      .then((saved) => {
        onSaved(saved);
        setError(null);
        setStatus("Сохранено");
        window.setTimeout(() => setStatus(null), 1500);
      })
      .catch((e) => {
        setStatus(null);
        setError(String(e));
      });
  };

  const setBlock = (i: number, v: number) => {
    const blocks = [...form.day_blocks];
    blocks[i] = Math.min(16, Math.max(1, v || 1));
    patch({ day_blocks: blocks });
  };

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <section className="rounded-2xl bg-surface p-5 shadow-sm">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted">Длительности</h2>
        <MinutesInput label="Фокус" value={form.focus_duration_seconds} onChange={(v) => patch({ focus_duration_seconds: v })} />
        <MinutesInput label="Короткий перерыв" value={form.short_break_seconds} onChange={(v) => patch({ short_break_seconds: v })} />
        <MinutesInput label="Длинный перерыв" value={form.long_break_seconds} onChange={(v) => patch({ long_break_seconds: v })} />
        <div className="mt-3 border-t border-slate-100 pt-3">
          <Toggle label="Автостарт перерыва" checked={form.auto_start_break} onChange={(v) => patch({ auto_start_break: v })} />
          <Toggle label="Автостарт фокуса" checked={form.auto_start_focus} onChange={(v) => patch({ auto_start_focus: v })} />
        </div>
      </section>

      <section className="rounded-2xl bg-surface p-5 shadow-sm">
        <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-muted">Блоки дня</h2>
        <p className="mb-3 text-xs text-muted">
          Число помидоров в каждом блоке. Внутри блока — короткие перерывы, между блоками — длинный.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {form.day_blocks.map((b, i) => (
            <div key={i} className="flex items-center gap-1 rounded-xl border border-slate-200 px-2 py-1">
              <input
                type="number"
                min={1}
                max={16}
                className="w-12 text-center text-sm outline-none"
                value={b}
                onChange={(e) => setBlock(i, Number(e.target.value))}
              />
              <button
                className="text-muted hover:text-danger disabled:opacity-30"
                disabled={form.day_blocks.length <= 1}
                onClick={() => patch({ day_blocks: form.day_blocks.filter((_, j) => j !== i) })}
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
          <button
            className="flex items-center gap-1 rounded-xl border border-dashed border-slate-300 px-3 py-1.5 text-sm text-muted hover:border-primary hover:text-primary-dark disabled:opacity-30"
            disabled={form.day_blocks.length >= 8}
            onClick={() => patch({ day_blocks: [...form.day_blocks, 4] })}
          >
            <Plus size={14} /> блок
          </button>
        </div>
      </section>

      <section className="rounded-2xl bg-surface p-5 shadow-sm">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted">Звук</h2>
        <Toggle label="Звук окончания" checked={form.sound_enabled} onChange={(v) => patch({ sound_enabled: v })} />
        <div className="mt-2 flex items-center gap-2 text-sm">
          <Music size={15} className="text-muted" />
          <span className="flex-1 truncate text-muted">
            {form.sound_file ? form.sound_file : "встроенный сигнал"}
          </span>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          <button
            className="rounded-xl border border-slate-200 px-3 py-1.5 text-sm hover:border-primary"
            onClick={() => {
              api
                .chooseSoundFile()
                .then((path) => {
                  if (path) {
                    patch({ sound_file: path });
                  }
                })
                .catch(() => undefined);
            }}
          >
            Выбрать файл...
          </button>
          <button
            className="flex items-center gap-1 rounded-xl border border-slate-200 px-3 py-1.5 text-sm hover:border-primary disabled:opacity-30"
            disabled={!form.sound_file}
            onClick={() => patch({ sound_file: null })}
          >
            <RotateCcw size={14} /> Встроенный
          </button>
          <button
            className="flex items-center gap-1 rounded-xl border border-slate-200 px-3 py-1.5 text-sm hover:border-primary"
            onClick={() => playChime(settings.sound_file)}
          >
            <Play size={14} /> Проверить
          </button>
        </div>
        <p className="mt-2 text-xs text-muted">Свой файл начнёт играть после сохранения настроек.</p>
      </section>

      <section className="rounded-2xl bg-surface p-5 shadow-sm">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-muted">Оверлей</h2>
        <label className="block py-1">
          <div className="mb-1 flex justify-between text-sm">
            <span>Размер круга</span>
            <span className="text-muted">{form.overlay.size}px</span>
          </div>
          <input
            type="range"
            min={60}
            max={600}
            step={10}
            className="w-full accent-[#00ADD8]"
            value={form.overlay.size}
            onChange={(e) => patch({ overlay: { ...form.overlay, size: Number(e.target.value) } })}
          />
        </label>
        <label className="block py-1">
          <div className="mb-1 flex justify-between text-sm">
            <span>Размер цифр</span>
            <span className="text-muted">{form.overlay.digits_size}px</span>
          </div>
          <input
            type="range"
            min={12}
            max={160}
            step={2}
            className="w-full accent-[#00ADD8]"
            value={form.overlay.digits_size}
            onChange={(e) => patch({ overlay: { ...form.overlay, digits_size: Number(e.target.value) } })}
          />
        </label>
        <label className="block py-1">
          <div className="mb-1 flex justify-between text-sm">
            <span>Прозрачность круга</span>
            <span className="text-muted">{Math.round(form.overlay.circle_opacity * 100)}%</span>
          </div>
          <input
            type="range"
            min={0.05}
            max={1}
            step={0.05}
            className="w-full accent-[#00ADD8]"
            value={form.overlay.circle_opacity}
            onChange={(e) => patch({ overlay: { ...form.overlay, circle_opacity: Number(e.target.value) } })}
          />
        </label>
        <label className="block py-1">
          <div className="mb-1 flex justify-between text-sm">
            <span>Прозрачность цифр</span>
            <span className="text-muted">{Math.round(form.overlay.digits_opacity * 100)}%</span>
          </div>
          <input
            type="range"
            min={0.05}
            max={1}
            step={0.05}
            className="w-full accent-[#00ADD8]"
            value={form.overlay.digits_opacity}
            onChange={(e) => patch({ overlay: { ...form.overlay, digits_opacity: Number(e.target.value) } })}
          />
        </label>
        <label className="block py-1">
          <div className="mb-1 flex justify-between text-sm">
            <span>Прозрачность кнопок</span>
            <span className="text-muted">{Math.round(form.overlay.buttons_opacity * 100)}%</span>
          </div>
          <input
            type="range"
            min={0.05}
            max={1}
            step={0.05}
            className="w-full accent-[#00ADD8]"
            value={form.overlay.buttons_opacity}
            onChange={(e) => patch({ overlay: { ...form.overlay, buttons_opacity: Number(e.target.value) } })}
          />
        </label>
        <Toggle
          label="Показывать цифры остатка"
          checked={form.overlay.show_time}
          onChange={(v) => patch({ overlay: { ...form.overlay, show_time: v } })}
        />
      </section>

      <div className="flex items-center gap-3 lg:col-span-2">
        <button
          className="rounded-xl bg-primary px-6 py-2.5 text-sm font-medium text-white shadow hover:bg-primary-dark"
          onClick={save}
        >
          Сохранить настройки
        </button>
        {status && <span className="text-sm text-success">{status}</span>}
        {error && <span className="max-w-md truncate text-sm text-danger">{error}</span>}
      </div>
    </div>
  );
}
