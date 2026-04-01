-- init.sql — canonical schema, matches prod as of 2026-03-24
-- Replace internal/store/schema/init.sql with this file.

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT      NOT NULL UNIQUE,
    password_hash TEXT      NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    public_key    TEXT                          -- ECDH P-256 SPKI, base64. NULL until first key upload.
);

CREATE TABLE IF NOT EXISTS messages (
    id                   BIGSERIAL PRIMARY KEY,
    sender_id            INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id         INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body                 TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    iv                   TEXT                 DEFAULT '',   -- AES-GCM nonce, base64
    ephemeral_public_key TEXT        NOT NULL DEFAULT '',   -- ECDH ephemeral pub key, base64
    mac                  TEXT        NOT NULL DEFAULT '',   -- message auth code, base64
    encrypted            BOOLEAN     NOT NULL DEFAULT TRUE,
    client_message_id    TEXT                               -- idempotency key set by sender
);

CREATE UNIQUE INDEX IF NOT EXISTS messages_sender_client_msg_uniq
    ON messages (sender_id, client_message_id);

CREATE INDEX IF NOT EXISTS idx_messages_recipient_id ON messages (recipient_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender_id    ON messages (sender_id);
CREATE INDEX IF NOT EXISTS idx_messages_pair_time    ON messages (sender_id, recipient_id, created_at);

CREATE TABLE IF NOT EXISTS invite_codes (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,
    created_by TEXT        NOT NULL DEFAULT 'admin',
    used_by    TEXT,
    used_at    TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);