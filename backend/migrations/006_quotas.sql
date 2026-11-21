-- CloudForge migration 006: per-role quotas (Milestone 9, Part 3).
--
-- Admin-configurable limits (GET/PUT /api/v1/admin/quotas) for labs,
-- VM size and lab duration, enforced by the quota package at the API
-- layer. A zero value means "no limit" except max_labs, where 0 means
-- the built-in default (3) — see backend/internal/quota.
CREATE TABLE IF NOT EXISTS quotas (
  role TEXT PRIMARY KEY,
  max_labs INT NOT NULL DEFAULT 3 CHECK (max_labs >= 0),
  max_cpu INT NOT NULL DEFAULT 0 CHECK (max_cpu >= 0),
  max_memory_mb INT NOT NULL DEFAULT 0 CHECK (max_memory_mb >= 0),
  max_disk_gb INT NOT NULL DEFAULT 0 CHECK (max_disk_gb >= 0),
  max_lab_hours INT NOT NULL DEFAULT 0 CHECK (max_lab_hours >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the built-in defaults (quota.DefaultLimits); re-running the
-- migration never overwrites an admin's changes.
INSERT INTO quotas (role, max_labs, max_cpu, max_memory_mb, max_disk_gb, max_lab_hours) VALUES
  ('learner', 3, 4, 8192, 50, 8),
  ('instructor', 5, 8, 16384, 100, 12),
  ('admin', 10, 16, 32768, 200, 24)
ON CONFLICT (role) DO NOTHING;
