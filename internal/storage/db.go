package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Open connects to the PostgreSQL database and runs schema migrations.
// It returns a ready-to-use *sql.DB.
func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// migrate ensures the schema is up-to-date. Safe to run on every startup.
func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS installations (
			id                 BIGSERIAL    PRIMARY KEY,
			installation_id    BIGINT       UNIQUE NOT NULL,
			account_login      TEXT         NOT NULL DEFAULT '',
			provider           TEXT         NOT NULL DEFAULT '',
			api_key_encrypted  TEXT         NOT NULL DEFAULT '',
			model              TEXT         NOT NULL DEFAULT '',
			free_reviews_used  INTEGER      NOT NULL DEFAULT 0,
			free_reviews_limit INTEGER      NOT NULL DEFAULT 100,
			created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		)
	`)
	return err
}
