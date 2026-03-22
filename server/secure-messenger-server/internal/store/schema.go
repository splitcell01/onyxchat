package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"time"
)

//go:embed schema/init.sql
var initSQL string

func EnsureSchema(db *sql.DB) error {
	const lockKey int64 = 8675309
	start := time.Now()

	log.Println("[DB] ensuring schema (acquiring advisory lock)")

	if _, err := db.Exec(`SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("advisory lock failed: %w", err)
	}
	defer func() {
		if _, err := db.Exec(`SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
			log.Printf("[DB] advisory unlock failed (non-fatal): %v", err)
		}
	}()

	if initSQL == "" {
		return fmt.Errorf("embedded schema/init.sql is empty")
	}
	if _, err := db.Exec(initSQL); err != nil {
		return fmt.Errorf("init schema failed: %w", err)
	}

	log.Printf("[DB] schema OK (took %s)", time.Since(start))
	return nil
}
