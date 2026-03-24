package store

import (
    "errors"
    "strings"
    "time"
)

// ─────────────────────────────────────────────────────────────
// APPEND to internal/store/user_store.go
// Add "time" to the existing import block.
// ─────────────────────────────────────────────────────────────

// InviteCodeFull is the admin-facing view of an invite code row.
type InviteCodeFull struct {
	ID        int64
	Code      string
	CreatedBy string
	UsedBy    *string    // nil if unused
	UsedAt    *time.Time // nil if unused
	ExpiresAt *time.Time // nil if no expiry
	CreatedAt time.Time
}

// AdminListInvites returns every invite code, newest first.
func (s *UserStore) AdminListInvites() ([]InviteCodeFull, error) {
	rows, err := s.db.Query(`
		SELECT id, code, created_by,
		       used_by, used_at, expires_at, created_at
		FROM invite_codes
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InviteCodeFull
	for rows.Next() {
		var c InviteCodeFull
		if err := rows.Scan(
			&c.ID, &c.Code, &c.CreatedBy,
			&c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AdminCreateInvite inserts a new invite code. expiresAt may be nil for no expiry.
func (s *UserStore) AdminCreateInvite(code, createdBy string, expiresAt *time.Time) (*InviteCodeFull, error) {
	var c InviteCodeFull
	err := s.db.QueryRow(`
		INSERT INTO invite_codes (code, created_by, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, code, created_by, used_by, used_at, expires_at, created_at
	`, code, createdBy, expiresAt).Scan(
		&c.ID, &c.Code, &c.CreatedBy,
		&c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "unique") {
			return nil, errors.New("invite code already exists")
		}
		return nil, err
	}
	return &c, nil
}

// AdminResetInvite clears used_by/used_at so the code can be reused.
func (s *UserStore) AdminResetInvite(code string) error {
	res, err := s.db.Exec(`
		UPDATE invite_codes
		SET used_by = NULL, used_at = NULL
		WHERE code = $1
	`, code)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("invite code not found")
	}
	return nil
}