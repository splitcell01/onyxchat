package store

import "time"

type Message struct {
	ID              int64     `db:"id"                json:"id"`
	SenderID        int64     `db:"sender_id"         json:"senderId"`
	RecipientID     int64     `db:"recipient_id"      json:"recipientId"`
	Body            string    `db:"body"              json:"body"`
	IV              string    `db:"iv"                json:"iv,omitempty"`
	Encrypted       bool      `db:"encrypted"         json:"encrypted"`
	ClientMessageID string    `db:"client_message_id" json:"clientMessageId,omitempty"`
	CreatedAt       time.Time `db:"created_at"        json:"createdAt"`
}
