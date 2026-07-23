DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS sync_tombstones;
ALTER TABLE preset_schedule DROP COLUMN updated_at;
ALTER TABLE sessions DROP COLUMN updated_at;
