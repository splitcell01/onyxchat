-- Drop ephemeral_public_key and mac from messages.
-- These columns were placeholders for a per-message ephemeral ECDH design that
-- was never implemented. The current E2E model uses static ECDH (one key pair
-- per user) with AES-256-GCM; only body, iv, and encrypted are used.
ALTER TABLE messages DROP COLUMN ephemeral_public_key;
ALTER TABLE messages DROP COLUMN mac;
