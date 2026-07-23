ALTER TABLE sessions ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE preset_schedule ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE sync_tombstones (
    id bigserial PRIMARY KEY,
    table_name text NOT NULL,
    row_id text NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sync_tombstones_deleted_idx ON sync_tombstones (deleted_at);

CREATE TABLE sync_state (
    key text PRIMARY KEY,
    value text NOT NULL
);
