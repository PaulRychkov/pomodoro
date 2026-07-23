ALTER TABLE plan_slots ADD COLUMN focus_seconds integer CHECK (focus_seconds > 0);
ALTER TABLE plan_slots ADD COLUMN break_seconds integer CHECK (break_seconds > 0);

CREATE TABLE presets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    slots jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE preset_schedule (
    weekday integer PRIMARY KEY CHECK (weekday BETWEEN 1 AND 7),
    preset_id uuid NOT NULL REFERENCES presets(id) ON DELETE CASCADE
);
