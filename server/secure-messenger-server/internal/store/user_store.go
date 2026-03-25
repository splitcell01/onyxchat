package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidInviteCode = errors.New("invalid or already used invite code")
	ErrUsernameTaken    = errors.New("username already taken")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	PublicKey    string // ECDH P-256 SPKI, base64-encoded. Empty if not yet set.
}

// InviteCode represents a single-use registration invite.
type InviteCode struct {
	ID        int64
	Code      string
	UsedBy    sql.NullString
	ExpiresAt sql.NullTime
}

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

// ─────────────────────────────────────────────────────────────
// Registration
// ─────────────────────────────────────────────────────────────

// RegisterWithInvite atomically consumes an invite code and creates the user
// in a single transaction. If user creation fails (e.g. duplicate username)
// the invite code is rolled back and remains available.
func (s *UserStore) RegisterWithInvite(code, username, passwordHash string) (*User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // no-op if Commit() succeeds

	// Consume the invite code inside the transaction.
	res, err := tx.Exec(
		`UPDATE invite_codes SET used_by = $1, used_at = NOW()
         WHERE code = $2 AND used_by IS NULL
         AND (expires_at IS NULL OR expires_at > NOW())`,
		username, code,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrInvalidInviteCode
	}

	// Create the user inside the same transaction.
	var id int64
	err = tx.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, passwordHash,
	).Scan(&id)
	if err != nil {
		// Postgres unique violation code 23505
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "unique") {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
	}, nil
}

// ─────────────────────────────────────────────────────────────
// Basic user operations
// ─────────────────────────────────────────────────────────────

func (s *UserStore) CreateUser(username string, passwordHash string) (*User, error) {
	row := s.db.QueryRow(
		`INSERT INTO users (username, password_hash)
         VALUES ($1, $2)
         RETURNING id`,
		username, passwordHash,
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return nil, err
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
	}, nil
}

func (s *UserStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *UserStore) GetUserByUsername(username string) (*User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, password_hash, COALESCE(public_key, '') FROM users WHERE username = $1 AND deleted_at IS NULL`,
		username,
	)

	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.PublicKey); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return &u, nil
}

func (s *UserStore) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(
		`SELECT id, username FROM users WHERE deleted_at IS NULL ORDER BY username ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// Compatibility alias.
func (s *UserStore) GetByUsername(username string) (*User, error) {
	return s.GetUserByUsername(username)
}

// ─────────────────────────────────────────────────────────────
// E2E key management
// ─────────────────────────────────────────────────────────────

func (s *UserStore) SetPublicKey(userID int64, pubKey string) error {
	_, err := s.db.Exec(
		`UPDATE users SET public_key = $1 WHERE id = $2`,
		pubKey, userID,
	)
	return err
}

func (s *UserStore) GetPublicKeyByUsername(username string) (string, error) {
	var key sql.NullString
	err := s.db.QueryRow(
		`SELECT public_key FROM users WHERE username = $1`,
		username,
	).Scan(&key)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrUserNotFound
		}
		return "", err
	}
	return key.String, nil
}

// ─────────────────────────────────────────────────────────────
// Invite codes
// ─────────────────────────────────────────────────────────────

func (s *UserStore) GetInviteCode(code string) (*InviteCode, error) {
	var ic InviteCode
	err := s.db.QueryRow(
		`SELECT id, code, used_by, expires_at FROM invite_codes
         WHERE code = $1 AND used_by IS NULL
         AND (expires_at IS NULL OR expires_at > NOW())`, code,
	).Scan(&ic.ID, &ic.Code, &ic.UsedBy, &ic.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &ic, err
}

func (s *UserStore) ConsumeInviteCode(code string, username string) error {
	res, err := s.db.Exec(
		`UPDATE invite_codes SET used_by = $1, used_at = NOW()
         WHERE code = $2 AND used_by IS NULL
         AND (expires_at IS NULL OR expires_at > NOW())`,
		username, code,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("invalid or already used invite code")
	}
	return nil
}

// UpdatePassword sets a new bcrypt password hash for the given user.
func (s *UserStore) UpdatePassword(userID int64, newHash string) error {
	_, err := s.db.Exec(
		`UPDATE users SET password_hash = $1 WHERE id = $2`,
		newHash, userID,
	)
	return err
}
