ALTER TABLE settings RENAME COLUMN plan_before_window_minutes TO study_before_work_minutes;
ALTER TABLE settings RENAME COLUMN plan_after_window_minutes TO study_after_work_minutes;
ALTER TABLE settings RENAME COLUMN window_share_percent TO work_share_percent;
