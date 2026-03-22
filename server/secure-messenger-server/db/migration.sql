ALTER TABLE messages ADD COLUMN IF NOT EXISTS iv TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS encrypted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS client_message_id TEXT;
ALTER TABLE messages ADD CONSTRAINT messages_sender_client_msg_uniq 
    UNIQUE (sender_id, client_message_id);
