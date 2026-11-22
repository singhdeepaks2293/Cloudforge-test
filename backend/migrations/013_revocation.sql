-- Phase 7 (CloudForge v2) — JWT revocation denylist.
--
-- revoked_tokens is the durable denylist for HS256 session tokens
-- (risk R3). One row per revoked token id (jti); rows expire with
-- the token and are deleted opportunistically, so the table cannot
-- grow without bound. issued_tokens records every jti the API
-- issued, so an admin revoking a user's sessions can find them all.
CREATE TABLE IF NOT EXISTS revoked_tokens (
    jti        TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS revoked_tokens_expires_idx ON revoked_tokens (expires_at);

CREATE TABLE IF NOT EXISTS issued_tokens (
    jti        TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    issued_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS issued_tokens_user_idx ON issued_tokens (user_id, expires_at);
