package store

import "database/sql"

type MessageStore struct {
	db *sql.DB
}

func NewMessageStore(db *sql.DB) *MessageStore {
	return &MessageStore{db: db}
}

// Create saves a new message. Pass iv="" and encrypted=false for plaintext messages.
func (s *MessageStore) Create(senderID, recipientID int64, body, iv string, encrypted bool) (*Message, error) {
	row := s.db.QueryRow(
		`INSERT INTO messages (sender_id, recipient_id, body, iv, encrypted, created_at)
         VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
         RETURNING id, sender_id, recipient_id, body, iv, encrypted, created_at`,
		senderID, recipientID, body, iv, encrypted,
	)

	var m Message
	var ivVal sql.NullString
	if err := row.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.IV = ivVal.String
	return &m, nil
}

// ListConversationSince returns up to limit messages between two users with
// id > sinceID, ordered oldest-first. It fetches limit+1 rows and trims the
// last one so the caller can tell whether more pages exist without a second
// COUNT query.
// NOTE: Does not JOIN users — sender username is resolved on the frontend from contacts.
func (s *MessageStore) ListConversationSince(userID, peerID, sinceID int64, limit int) (msgs []Message, hasMore bool, err error) {
	rows, err := s.db.Query(
		`SELECT id, sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
         FROM messages
         WHERE ((sender_id = $1 AND recipient_id = $2)
             OR (sender_id = $3 AND recipient_id = $4))
           AND id > $5
         ORDER BY id ASC
         LIMIT $6`,
		userID, peerID,
		peerID, userID,
		sinceID,
		limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	msgs = make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		var ivVal sql.NullString
		var clientMsgID sql.NullString
		if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &clientMsgID, &m.CreatedAt); err != nil {
			return nil, false, err
		}
		m.IV = ivVal.String
		m.ClientMessageID = clientMsgID.String
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	if len(msgs) > limit {
		return msgs[:limit], true, nil
	}
	return msgs, false, nil
}

func (s *MessageStore) GetByID(id int64) (*Message, error) {
	row := s.db.QueryRow(
		`SELECT id, sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
		 FROM messages
		 WHERE id = $1`,
		id,
	)

	var m Message
	var ivVal sql.NullString
	if err := row.Scan(
		&m.ID,
		&m.SenderID,
		&m.RecipientID,
		&m.Body,
		&ivVal,
		&m.Encrypted,
		&m.ClientMessageID,
		&m.CreatedAt,
	); err != nil {
		return nil, err
	}

	m.IV = ivVal.String
	return &m, nil
}
func (s *MessageStore) CreateOrGetExisting(
	senderID, recipientID int64,
	body, iv string,
	encrypted bool,
	clientMessageID string,
) (*Message, bool, error) {
	const q = `
		INSERT INTO messages (
			sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		ON CONFLICT (sender_id, client_message_id)
		DO NOTHING
		RETURNING id, sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
	`

	var m Message
	var ivVal sql.NullString

	err := s.db.QueryRow(
		q,
		senderID,
		recipientID,
		body,
		iv,
		encrypted,
		clientMessageID,
	).Scan(
		&m.ID,
		&m.SenderID,
		&m.RecipientID,
		&m.Body,
		&ivVal,
		&m.Encrypted,
		&m.ClientMessageID,
		&m.CreatedAt,
	)
	if err == nil {
		m.IV = ivVal.String
		return &m, true, nil
	}

	if err != sql.ErrNoRows {
		return nil, false, err
	}

	const existingQ = `
		SELECT id, sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
		FROM messages
		WHERE sender_id = $1 AND client_message_id = $2
	`

	ivVal = sql.NullString{}
	err = s.db.QueryRow(existingQ, senderID, clientMessageID).Scan(
		&m.ID,
		&m.SenderID,
		&m.RecipientID,
		&m.Body,
		&ivVal,
		&m.Encrypted,
		&m.ClientMessageID,
		&m.CreatedAt,
	)
	if err != nil {
		return nil, false, err
	}

	m.IV = ivVal.String
	return &m, false, nil
}

// DeleteMessage hard-deletes a message row. Only succeeds if senderID matches,
// so callers cannot delete other users' messages. Returns sql.ErrNoRows if the
// message doesn't exist or the caller is not the sender.
func (s *MessageStore) DeleteMessage(id, senderID int64) error {
	res, err := s.db.Exec(
		`DELETE FROM messages WHERE id = $1 AND sender_id = $2`,
		id, senderID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetUnreadForUser returns all messages sent to userID with id > sinceID.
func (s *MessageStore) GetUnreadForUser(userID, sinceID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, sender_id, recipient_id, body, iv, encrypted, client_message_id, created_at
         FROM messages
         WHERE recipient_id = $1 AND id > $2
         ORDER BY id ASC`,
		userID, sinceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]Message, 0)
	for rows.Next() {
		var m Message
		var ivVal sql.NullString
		var clientMsgID sql.NullString
		if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &clientMsgID, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IV = ivVal.String
		m.ClientMessageID = clientMsgID.String
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}
