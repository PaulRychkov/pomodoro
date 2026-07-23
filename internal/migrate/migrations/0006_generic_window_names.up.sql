ALTER TABLE settings RENAME COLUMN study_before_work_minutes TO plan_before_window_minutes;
ALTER TABLE settings RENAME COLUMN study_after_work_minutes TO plan_after_window_minutes;
ALTER TABLE settings RENAME COLUMN work_share_percent TO window_share_percent;
