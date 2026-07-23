CREATE TABLE sessions (
    id text PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('focus', 'break')),
    started_at datetime NOT NULL,
    ended_at datetime NULL,
    planned_duration_seconds integer NOT NULL CHECK (planned_duration_seconds > 0),
    outcome text NULL CHECK (outcome IS NULL OR outcome IN ('completed', 'abandoned', 'interrupted')),
    paused_at datetime NULL,
    paused_total_seconds integer NOT NULL DEFAULT 0,
    label text NULL,
    task_source text NULL,
    task_external_id text NULL,
    task_title_snapshot text NULL,
    relabeled_at datetime NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((ended_at IS NULL) = (outcome IS NULL)),
    CHECK (ended_at IS NULL OR ended_at >= started_at),
    CHECK (paused_at IS NULL OR ended_at IS NULL),
    CHECK ((task_source IS NULL) = (task_external_id IS NULL)),
    CHECK (label IS NULL OR task_source IS NULL),
    CHECK (kind = 'focus' OR (label IS NULL AND task_source IS NULL))
);

CREATE UNIQUE INDEX sessions_single_active_idx ON sessions ((id IS NOT NULL)) WHERE ended_at IS NULL;
CREATE INDEX sessions_started_at_idx ON sessions (started_at DESC);
CREATE INDEX sessions_task_ref_idx ON sessions (task_source, task_external_id) WHERE task_source IS NOT NULL;

CREATE TABLE settings (
    id integer PRIMARY KEY CHECK (id = 1),
    focus_duration_seconds integer NOT NULL DEFAULT 1500 CHECK (focus_duration_seconds > 0),
    short_break_seconds integer NOT NULL DEFAULT 300 CHECK (short_break_seconds > 0),
    long_break_seconds integer NOT NULL DEFAULT 900 CHECK (long_break_seconds > 0),
    day_blocks text NOT NULL DEFAULT '[4,4]',
    plan_before_window_minutes integer NOT NULL DEFAULT 80,
    plan_after_window_minutes integer NOT NULL DEFAULT 90,
    window_share_percent integer NOT NULL DEFAULT 67,
    auto_start_break boolean NOT NULL DEFAULT 1,
    auto_start_focus boolean NOT NULL DEFAULT 0,
    sound_enabled boolean NOT NULL DEFAULT 1,
    sound_file text NULL,
    overlay text NOT NULL DEFAULT '{}',
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO settings (id) VALUES (1);

CREATE TABLE events_outbox (
    id text PRIMARY KEY,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload text NOT NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at datetime NULL,
    attempts integer NOT NULL DEFAULT 0,
    last_error text NULL
);

CREATE INDEX events_outbox_unpublished_idx ON events_outbox (created_at) WHERE published_at IS NULL;

CREATE TABLE plan_slots (
    date text NOT NULL,
    idx integer NOT NULL CHECK (idx >= 0),
    task_source text,
    task_external_id text,
    task_title text,
    label text,
    focus_seconds integer CHECK (focus_seconds IS NULL OR focus_seconds > 0),
    break_seconds integer CHECK (break_seconds IS NULL OR break_seconds > 0),
    pinned boolean NOT NULL DEFAULT 0,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (date, idx)
);

CREATE TABLE presets (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    slots text NOT NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE preset_schedule (
    weekday integer PRIMARY KEY CHECK (weekday BETWEEN 1 AND 7),
    preset_id text NOT NULL REFERENCES presets(id) ON DELETE CASCADE
);
