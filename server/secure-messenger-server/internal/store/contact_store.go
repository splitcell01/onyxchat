package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────
// Errors
// ─────────────────────────────────────────────────────────────

var (
	ErrContactNotFound = errors.New("contact not found")
	ErrContactExists   = errors.New("contact already added")
	ErrAlreadyDeleted  = errors.New("account already deleted")
)

// ─────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────

type Contact struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Online   bool   `json:"online"` // populated by WS hub, not DB
}

type GDPRDeletionRecord struct {
	UserID           int64
	MessagesPurged   int
	PublicKeyCleared bool
	InvitesExpired   int
}

// ─────────────────────────────────────────────────────────────
// GDPR-compliant account deletion
// ─────────────────────────────────────────────────────────────

// DeleteAccountGDPR performs a GDPR-compliant right-to-erasure deletion:
//   - Anonymizes the user row (username → "deleted_<id>", clears password hash)
//   - Clears the ECDH public key
//   - Hard-deletes message bodies (replaces with empty string — sender/recipient IDs remain for thread integrity)
//   - Expires any unused invite codes owned by the user
//   - Writes an audit record to gdpr_deletion_log
//
// Messages are not fully deleted because that would corrupt conversation
// history for the other party. Instead the body is cleared and the sender
// becomes "Deleted User" at query time via COALESCE on display_name.
func (s *UserStore) DeleteAccountGDPR(userID int64) (*GDPRDeletionRecord, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()

	// 1. Anonymize user — fail fast if already deleted
	res, err := tx.Exec(`
		UPDATE users
		SET
			username      = 'deleted_' || id::text,
			display_name  = 'Deleted User',
			password_hash = '',
			public_key    = NULL,
			deleted_at    = $1
		WHERE id = $2
		  AND deleted_at IS NULL
	`, now, userID)
	if err != nil {
		return nil, fmt.Errorf("anonymize user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrAlreadyDeleted
	}

	// 2. Clear message bodies (GDPR erasure of personal content)
	//    We keep the row so the other party's thread doesn't break,
	//    but the body becomes empty and encrypted flag is cleared.
	msgRes, err := tx.Exec(`
		UPDATE messages
		SET body = '', iv = NULL, encrypted = FALSE
		WHERE sender_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("purge message bodies: %w", err)
	}
	msgCount, _ := msgRes.RowsAffected()

	// 3. Expire unused invite codes
	invRes, err := tx.Exec(`
		UPDATE invite_codes
		SET expires_at = NOW()
		WHERE created_by = $1
		  AND used_by IS NULL
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("expire invite codes: %w", err)
	}
	invCount, _ := invRes.RowsAffected()

	// 4. Remove from all contacts lists
	_, err = tx.Exec(`DELETE FROM contacts WHERE user_id = $1 OR contact_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("remove contacts: %w", err)
	}

	// 5. Write GDPR audit record
	record := &GDPRDeletionRecord{
		UserID:           userID,
		MessagesPurged:   int(msgCount),
		PublicKeyCleared: true,
		InvitesExpired:   int(invCount),
	}
	_, err = tx.Exec(`
		INSERT INTO gdpr_deletion_log
			(user_id, anonymized_at, messages_purged, public_key_cleared, invites_expired, notes)
		VALUES ($1, $2, $3, $4, $5, 'user-initiated deletion')
	`, userID, now, record.MessagesPurged, record.PublicKeyCleared, record.InvitesExpired)
	if err != nil {
		return nil, fmt.Errorf("write gdpr log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return record, nil
}

// ─────────────────────────────────────────────────────────────
// Contacts
// ─────────────────────────────────────────────────────────────

// AddContact adds a contact by username. The contact must exist and not be deleted.
func (s *UserStore) AddContact(userID int64, targetUsername string) error {
	var targetID int64
	err := s.db.QueryRow(`
		SELECT id FROM users
		WHERE username = $1
		  AND deleted_at IS NULL
	`, targetUsername).Scan(&targetID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrUserNotFound
		}
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO contacts (user_id, contact_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, userID, targetID)
	return err
}

// RemoveContact removes a contact by username.
func (s *UserStore) RemoveContact(userID int64, targetUsername string) error {
	var targetID int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE username = $1`, targetUsername).Scan(&targetID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrContactNotFound
		}
		return err
	}

	res, err := s.db.Exec(`
		DELETE FROM contacts WHERE user_id = $1 AND contact_id = $2
	`, userID, targetID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrContactNotFound
	}
	return nil
}

// ListContacts returns contacts for a user, excluding deleted accounts.
// Online status is not set here — the WS hub should populate it.
func (s *UserStore) ListContacts(userID int64) ([]*Contact, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.username
		FROM contacts c
		JOIN users u ON u.id = c.contact_id
		WHERE c.user_id = $1
		  AND u.deleted_at IS NULL
		ORDER BY u.username ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []*Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Username); err != nil {
			return nil, err
		}
		contacts = append(contacts, &c)
	}
	return contacts, rows.Err()
}

// GetUserByID fetches a user by ID. Returns ErrUserNotFound if deleted or missing.
// Use this in AuthMiddleware to reject deleted users with valid JWTs.
func (s *UserStore) GetUserByID(userID int64) (*User, error) {
	var u User
	err := s.db.QueryRow(`
		SELECT id, username, password_hash, COALESCE(public_key, '')
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
	`, userID).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.PublicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}