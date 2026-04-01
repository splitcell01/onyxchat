-- ─────────────────────────────────────────────────────────────
-- E2E Encryption Migration
-- Run this against your database before deploying the new code.
-- ─────────────────────────────────────────────────────────────

-- Store each user's ECDH P-256 public key (SPKI, base64-encoded).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS public_key TEXT;

-- Add E2E fields to messages.
-- iv:        12-byte AES-GCM nonce, base64-encoded. NULL for plaintext legacy messages.
-- encrypted: flag so clients know whether to attempt decryption.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS iv        TEXT    DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS encrypted BOOLEAN NOT NULL DEFAULT FALSE;