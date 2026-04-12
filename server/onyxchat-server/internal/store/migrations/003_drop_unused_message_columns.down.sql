-- Restore ephemeral_public_key and mac with the same constraints as the original schema.
ALTER TABLE messages ADD COLUMN ephemeral_public_key TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN mac                  TEXT NOT NULL DEFAULT '';
