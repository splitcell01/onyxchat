package com.cole.securechat.model;

import java.time.LocalDateTime;

public class Message {
    private final String sender;
    private final String content;
    private final LocalDateTime timestamp;

    public Message(String sender, String content, LocalDateTime timestamp) {
        this.sender = sender;
        this.content = content;
        this.timestamp = timestamp;
    }

    public String getSender() {
        return sender;
    }

    public String getContent() {
        return content;
    }

    public LocalDateTime getTimestamp() {
        return timestamp;
    }

    @Override
    public String toString() {
        // How it appears in the ListView
        return "[" + timestamp.toLocalTime().withNano(0) + "] " + sender + ": " + content;
    }
}
