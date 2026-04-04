-- Full production schema as of 2026-03-24.
-- All statements use IF NOT EXISTS so this is safe to run against an existing database.
-- On a fresh deployment this creates everything from scratch.

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL   PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    public_key    TEXT,
    deleted_at    TIMESTAMPTZ DEFAULT NULL,
    display_name  TEXT        DEFAULT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_deleted_at
    ON users (deleted_at)
    WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS messages (
    id                   BIGSERIAL   PRIMARY KEY,
    sender_id            INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id         INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body                 TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    iv                   TEXT                 DEFAULT '',
    ephemeral_public_key TEXT        NOT NULL DEFAULT '',
    mac                  TEXT        NOT NULL DEFAULT '',
    encrypted            BOOLEAN     NOT NULL DEFAULT TRUE,
    client_message_id    TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS messages_sender_client_msg_uniq
    ON messages (sender_id, client_message_id);

CREATE INDEX IF NOT EXISTS idx_messages_recipient_id ON messages (recipient_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender_id    ON messages (sender_id);
CREATE INDEX IF NOT EXISTS idx_messages_pair_time    ON messages (sender_id, recipient_id, created_at);

CREATE TABLE IF NOT EXISTS invite_codes (
    id         BIGSERIAL   PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,
    created_by TEXT        NOT NULL DEFAULT 'admin',
    used_by    TEXT,
    used_at    TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gdpr_deletion_log (
    id                 BIGSERIAL   PRIMARY KEY,
    user_id            BIGINT      NOT NULL,
    requested_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    anonymized_at      TIMESTAMPTZ,
    messages_purged    INT         NOT NULL DEFAULT 0,
    public_key_cleared BOOLEAN     NOT NULL DEFAULT FALSE,
    invites_expired    INT         NOT NULL DEFAULT 0,
    notes              TEXT
);

CREATE TABLE IF NOT EXISTS contacts (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, contact_id),
    CHECK (user_id <> contact_id)
);

CREATE INDEX IF NOT EXISTS idx_contacts_user_id ON contacts (user_id);
