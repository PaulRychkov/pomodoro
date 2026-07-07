CREATE TYPE session_kind AS ENUM ('focus', 'break');
CREATE TYPE session_outcome AS ENUM ('completed', 'abandoned', 'interrupted');

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind session_kind NOT NULL,
    started_at timestamptz NOT NULL,
    ended_at timestamptz NULL,
    planned_duration_seconds integer NOT NULL CHECK (planned_duration_seconds > 0),
    outcome session_outcome NULL,
    paused_at timestamptz NULL,
    paused_total_seconds integer NOT NULL DEFAULT 0,
    label text NULL,
    task_source text NULL,
    task_external_id text NULL,
    task_title_snapshot text NULL,
    relabeled_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((ended_at IS NULL) = (outcome IS NULL)),
    CHECK (ended_at IS NULL OR ended_at >= started_at),
    CHECK (paused_at IS NULL OR ended_at IS NULL),
    CHECK ((task_source IS NULL) = (task_external_id IS NULL)),
    CHECK (label IS NULL OR task_source IS NULL),
    CHECK (kind = 'focus' OR (label IS NULL AND task_source IS NULL))
);

CREATE UNIQUE INDEX sessions_single_active_idx ON sessions ((true)) WHERE ended_at IS NULL;
CREATE INDEX sessions_started_at_idx ON sessions (started_at DESC);
CREATE INDEX sessions_task_ref_idx ON sessions (task_source, task_external_id) WHERE task_source IS NOT NULL;

CREATE TABLE settings (
    id integer PRIMARY KEY CHECK (id = 1),
    focus_duration_seconds integer NOT NULL DEFAULT 1500 CHECK (focus_duration_seconds > 0),
    short_break_seconds integer NOT NULL DEFAULT 300 CHECK (short_break_seconds > 0),
    long_break_seconds integer NOT NULL DEFAULT 900 CHECK (long_break_seconds > 0),
    day_blocks jsonb NOT NULL DEFAULT '[4,4]',
    auto_start_break boolean NOT NULL DEFAULT true,
    auto_start_focus boolean NOT NULL DEFAULT false,
    sound_enabled boolean NOT NULL DEFAULT true,
    sound_file text NULL,
    overlay jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO settings (id) VALUES (1);

CREATE TABLE events_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz NULL,
    attempts integer NOT NULL DEFAULT 0,
    last_error text NULL
);

CREATE INDEX events_outbox_unpublished_idx ON events_outbox (created_at) WHERE published_at IS NULL;
