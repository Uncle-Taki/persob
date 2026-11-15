-- Preserve legacy identities and history. No unknown role receives elevated access.
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN active BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN preferences JSONB NOT NULL DEFAULT '{"language":"fa","preferred_city":"","preferred_area":"","default_sort":"best_offer","number_format":"locale"}'::jsonb;
UPDATE users SET role='buyer' WHERE role NOT IN ('admin','super_admin','buyer');
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'buyer';
ALTER TABLE users ADD CONSTRAINT users_valid_role CHECK(role IN ('super_admin','admin','buyer'));
-- Case-folding collisions must be resolved explicitly; never merge two people's data.
CREATE UNIQUE INDEX users_username_folded ON users(lower(username));
CREATE TABLE user_sessions (
 token_hash TEXT PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX user_sessions_user ON user_sessions(user_id);
CREATE INDEX user_sessions_expiry ON user_sessions(expires_at);
-- Bounded, shared across API replicas. Keys are hashes, not credentials or message text.
CREATE TABLE auth_rate_limits (
 bucket TEXT PRIMARY KEY,
 window_start TIMESTAMPTZ NOT NULL,
 attempts INT NOT NULL
);
