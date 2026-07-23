ALTER TABLE sessions ADD COLUMN updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE preset_schedule ADD COLUMN updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP;

CREATE TABLE sync_tombstones (
    id integer PRIMARY KEY AUTOINCREMENT,
    table_name text NOT NULL,
    row_id text NOT NULL,
    deleted_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX sync_tombstones_deleted_idx ON sync_tombstones (deleted_at);

CREATE TABLE sync_state (
    key text PRIMARY KEY,
    value text NOT NULL
);
