CREATE TABLE plan_slots (
    date text NOT NULL,
    idx integer NOT NULL CHECK (idx >= 0),
    task_source text,
    task_external_id text,
    task_title text,
    label text,
    pinned boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (date, idx)
);
