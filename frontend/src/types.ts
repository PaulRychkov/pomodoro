export type Phase = "idle" | "focus" | "short_break" | "long_break";

export interface TaskRef {
  source: string;
  external_id: string;
  title_snapshot: string;
}

export interface Binding {
  label: string | null;
  task: TaskRef | null;
}

export interface State {
  phase: Phase;
  next_phase: Phase;
  paused: boolean;
  session_id: string | null;
  started_at: string | null;
  paused_at: string | null;
  paused_total_seconds: number;
  planned_seconds: number;
  /** В idle — длительность следующей сессии по плану дня; в активной фазе равна planned_seconds. */
  next_planned_seconds: number;
  remaining_seconds: number;
  label: string | null;
  task: TaskRef | null;
  completed_today: number;
  credit_today: number;
  day_blocks: number[];
  block_index: number;
  pos_in_block: number;
  block_size: number;
  day_total: number;
  day_complete: boolean;
  sound_enabled: boolean;
  settings_stamp: string;
}

export interface StatePush {
  state: State;
  reason: string;
}

export interface OverlayConfig {
  size: number;
  digits_size: number;
  circle_opacity: number;
  digits_opacity: number;
  buttons_opacity: number;
  show_time: boolean;
  pos_x?: number | null;
  pos_y?: number | null;
}

export interface Settings {
  focus_duration_seconds: number;
  short_break_seconds: number;
  long_break_seconds: number;
  day_blocks: number[];
  /** Начало активного дня, минуты от полуночи (по умолчанию 360 = 06:00). */
  day_start_minutes: number;
  /** Конец активного дня, минуты от полуночи (по умолчанию 1200 = 20:00). */
  day_end_minutes: number;
  window_share_percent: number;
  auto_start_break: boolean;
  auto_start_focus: boolean;
  sound_enabled: boolean;
  sound_file: string | null;
  overlay: OverlayConfig;
}

export interface Session {
  id: string;
  kind: "focus" | "break";
  started_at: string;
  ended_at: string | null;
  planned_duration_seconds: number;
  outcome: "completed" | "abandoned" | "interrupted" | null;
  paused_at: string | null;
  paused_total_seconds: number;
  label: string | null;
  task_source: string | null;
  task_external_id: string | null;
  task_title_snapshot: string | null;
  relabeled_at: string | null;
  focus_seconds: number | null;
  credit_twelfths: number | null;
  created_at: string;
}

export interface TaskOption {
  source: string;
  external_id: string;
  title: string;
  topic_path: string;
}

export interface PickerNode {
  kind: "topic" | "task";
  id: string;
  name: string;
  source: string;
  bind_task_id: string;
  today: boolean;
  children: PickerNode[] | null;
}

export interface PlanSlot {
  idx: number;
  task: TaskRef | null;
  label: string | null;
  focus_minutes: number | null;
  /** Проекция начала помидора, минуты от полуночи; null — у слота нет места в дне. */
  start_minutes: number | null;
  /** Проекция конца фокуса, минуты от полуночи. */
  end_minutes: number | null;
  break_minutes: number | null;
  /** Период дня, которому принадлежит слот: свободное время между событиями либо окно. */
  period_start_minutes: number | null;
  period_end_minutes: number | null;
  /** Не null, если слот лежит внутри окна — события с фиксированным временем (например «Работа»). */
  window_id: string | null;
  window_title: string | null;
  pinned: boolean;
  done: boolean;
  overflow: boolean;
}

export interface SlotPatch {
  task?: TaskRef | null;
  label?: string | null;
  clear_binding?: boolean;
  focus_minutes?: number | null;
  break_minutes?: number | null;
}

export interface PresetSlot {
  focus_minutes: number;
  break_minutes: number;
}

export interface Preset {
  id: string;
  name: string;
  slots: PresetSlot[];
}

export interface ScheduleEntry {
  weekday: number;
  preset_name: string;
}
