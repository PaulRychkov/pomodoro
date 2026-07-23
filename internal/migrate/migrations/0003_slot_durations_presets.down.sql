DROP TABLE preset_schedule;
DROP TABLE presets;
ALTER TABLE plan_slots DROP COLUMN break_seconds;
ALTER TABLE plan_slots DROP COLUMN focus_seconds;
