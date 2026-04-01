CREATE TABLE IF NOT EXISTS invite_codes (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL DEFAULT 'admin',
    used_by    TEXT,
    used_at    TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
