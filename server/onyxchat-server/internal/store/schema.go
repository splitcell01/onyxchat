package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies all pending migrations. Safe to call on startup —
// it is a no-op when the database is already at the latest version.
func RunMigrations(db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	v, _, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("check migration version: %w", err)
	}
	log.Printf("[DB] migrations OK (version %d)", v)
	return nil
}
