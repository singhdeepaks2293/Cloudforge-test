-- CloudForge migration 007: sub-hour lab-duration cap (lightweight mode).
--
-- quota.MaxLabMinutes (minutes) takes precedence over max_lab_hours
-- when > 0, so the lightweight mode's 30-minute labs can be expressed
-- (the hour column cannot hold 30 minutes). Existing rows keep
-- max_lab_minutes = 0, i.e. the previous hour-based behaviour, so
-- this migration changes nothing for full-mode deployments.
ALTER TABLE quotas ADD COLUMN IF NOT EXISTS max_lab_minutes INT NOT NULL DEFAULT 0 CHECK (max_lab_minutes >= 0);
