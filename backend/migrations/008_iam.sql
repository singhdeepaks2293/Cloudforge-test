-- CloudForge migration 008: AWS-style IAM identities (Phase 2).
--
-- CloudForge platform users (users table, auth) are NOT AWS IAM.
-- These tables hold the teaching IAM identities the IAM console
-- manages: users, groups (+ memberships), roles, managed policies
-- (+ attachments), access keys and assumed-role sessions.
-- Policy documents are JSONB and are always validated by the backend
-- IAM evaluator before they are stored (see internal/providers/iam.go
-- ValidatePolicyDocument); the database is the persistence, not the
-- validator.
-- Access-key secrets and session tokens are NEVER stored: only their
-- SHA-256 hashes are persisted, and the secret/token is shown to the
-- caller exactly once at creation.
CREATE TABLE IF NOT EXISTS iam_users (
  id         UUID PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  path       TEXT NOT NULL DEFAULT '/',
  arn        TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS iam_groups (
  id         UUID PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  path       TEXT NOT NULL DEFAULT '/',
  arn        TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS iam_group_members (
  group_id UUID NOT NULL REFERENCES iam_groups(id) ON DELETE CASCADE,
  user_id  UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
);
CREATE TABLE IF NOT EXISTS iam_roles (
  id                UUID PRIMARY KEY,
  name              TEXT NOT NULL UNIQUE,
  path              TEXT NOT NULL DEFAULT '/',
  arn               TEXT NOT NULL,
  trust_policy      JSONB NOT NULL,
  max_session_secs  INT NOT NULL DEFAULT 3600 CHECK (max_session_secs > 0),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS iam_policies (
  id          UUID PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  document    JSONB NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- attachable: user | group | role; target_id is the entity UUID.
CREATE TABLE IF NOT EXISTS iam_attachments (
  attachable TEXT NOT NULL CHECK (attachable IN ('user','group','role')),
  target_id  UUID NOT NULL,
  policy_id  UUID NOT NULL REFERENCES iam_policies(id) ON DELETE CASCADE,
  PRIMARY KEY (attachable, target_id, policy_id)
);
CREATE TABLE IF NOT EXISTS iam_access_keys (
  id          TEXT PRIMARY KEY,
  user_id     UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  secret_hash TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS iam_sessions (
  id         UUID PRIMARY KEY,
  role_id    UUID NOT NULL REFERENCES iam_roles(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_iam_attachments_policy ON iam_attachments(policy_id);
CREATE INDEX IF NOT EXISTS idx_iam_access_keys_user ON iam_access_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_iam_sessions_role ON iam_sessions(role_id);
