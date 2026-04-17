package db

import (
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx driver as "pgx"
)

// Open opens a pool using the pgx stdlib driver (postgres://… DSN).
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetMaxOpenConns(8)
	return db, nil
}
