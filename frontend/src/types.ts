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
  remaining_seconds: number;
  label: string | null;
  task: TaskRef | null;
  completed_today: number;
  day_blocks: number[];
  block_index: number;
  pos_in_block: number;
  block_size: number;
  day_total: number;
  day_complete: boolean;
  sound_enabled: boolean;
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
  created_at: string;
}

export interface TaskOption {
  source: string;
  external_id: string;
  title: string;
}
