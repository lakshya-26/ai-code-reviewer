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
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS installations (
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
		)`,
		`CREATE TABLE IF NOT EXISTS pr_reviews (
			id                 BIGSERIAL    PRIMARY KEY,
			installation_id    BIGINT       NOT NULL,
			repo_full_name     TEXT         NOT NULL,
			pr_number          INTEGER      NOT NULL,
			last_reviewed_sha  TEXT         NOT NULL DEFAULT '',
			check_run_id       BIGINT       NOT NULL DEFAULT 0,
			created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			UNIQUE (installation_id, repo_full_name, pr_number)
		)`,
		`CREATE TABLE IF NOT EXISTS posted_comments (
			id                 BIGSERIAL    PRIMARY KEY,
			repo_full_name     TEXT         NOT NULL,
			pr_number          INTEGER      NOT NULL,
			path               TEXT         NOT NULL,
			line               INTEGER      NOT NULL,
			category           TEXT         NOT NULL,
			body_hash          TEXT         NOT NULL DEFAULT '',
			commit_sha         TEXT         NOT NULL DEFAULT '',
			created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			UNIQUE (repo_full_name, pr_number, path, line, category)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
