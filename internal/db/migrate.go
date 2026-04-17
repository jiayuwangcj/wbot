package db

import (
	"database/sql"
	"embed"
	"path/filepath"
	"sort"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	name text PRIMARY KEY,
	applied_at timestamptz NOT NULL DEFAULT now()
)`)
	return err
}

func migrationApplied(db *sql.DB, name string) (bool, error) {
	var n string
	err := db.QueryRow(`SELECT name FROM schema_migrations WHERE name = $1`, name).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MigrateUp applies embedded migrations in lexical filename order. Each file runs
// in a transaction and is recorded in schema_migrations after success.
func MigrateUp(db *sql.DB) error {
	if err := ensureMigrationTable(db); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		ok, err := migrationApplied(db, name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		body, err := migrationFS.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			return err
		}
		if err := applyMigration(db, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(db *sql.DB, name string, sqlText string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(sqlText); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
		return err
	}
	return tx.Commit()
}
