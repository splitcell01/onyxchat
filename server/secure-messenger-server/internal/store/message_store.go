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
		senderID, recipientID, body, nullableString(iv), encrypted,
	)

	var m Message
	var ivVal sql.NullString
	if err := row.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.IV = ivVal.String
	return &m, nil
}

// ListConversationSince returns messages between two users with id > sinceID.
func (s *MessageStore) ListConversationSince(userID, peerID, sinceID int64) ([]Message, error) {
    rows, err := s.db.Query(
        `SELECT m.id, m.sender_id, m.recipient_id, m.body, m.iv, m.encrypted, m.created_at,
                COALESCE(u.display_name, u.username) AS sender_username
         FROM messages m
         LEFT JOIN users u ON u.id = m.sender_id
         WHERE ((sender_id = $1 AND recipient_id = $2)
             OR (sender_id = $3 AND recipient_id = $4))
           AND id > $5
         ORDER BY id ASC`,
        userID, peerID,
        peerID, userID,
        sinceID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    msgs := make([]Message, 0)
    for rows.Next() {
        var m Message
        var ivVal sql.NullString
        var senderUsername string
        if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &m.CreatedAt, &senderUsername); err != nil {
            return nil, err
        }
        m.IV = ivVal.String
        msgs = append(msgs, m)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return msgs, nil
}

// nullableString converts an empty string to SQL NULL.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
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
		nullableString(iv),
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

// GetUnreadForUser returns all messages sent to userID with id > sinceID.
func (s *MessageStore) GetUnreadForUser(userID, sinceID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, sender_id, recipient_id, body, iv, encrypted, created_at
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
		if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &ivVal, &m.Encrypted, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IV = ivVal.String
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}
