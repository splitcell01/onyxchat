-- db/migrate_contacts_gdpr.sql
-- Run after existing migrations
-- Adds: soft delete, contacts table, GDPR deletion audit log

-- ─────────────────────────────────────────────────────────────
-- 1. Soft delete + anonymization columns on users
-- ─────────────────────────────────────────────────────────────
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS display_name TEXT        DEFAULT NULL;

-- Fast lookup for auth middleware to reject deleted users
CREATE INDEX IF NOT EXISTS idx_users_deleted_at
    ON users (deleted_at)
    WHERE deleted_at IS NOT NULL;

-- ─────────────────────────────────────────────────────────────
-- 2. GDPR deletion audit log
--    Proves compliance if ever asked — stores what was purged and when
-- ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS gdpr_deletion_log (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT       NOT NULL,   -- original user ID (kept for audit)
    requested_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    anonymized_at  TIMESTAMPTZ,
    messages_purged INT          NOT NULL DEFAULT 0,
    public_key_cleared BOOLEAN  NOT NULL DEFAULT FALSE,
    invites_expired    INT       NOT NULL DEFAULT 0,
    notes          TEXT
);

-- ─────────────────────────────────────────────────────────────
-- 3. Contacts (friends list)
-- ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS contacts (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, contact_id),
    CHECK (user_id <> contact_id)  -- prevent self-add
);

CREATE INDEX IF NOT EXISTS idx_contacts_user_id ON contacts (user_id);