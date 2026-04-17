package db

import "testing"

func TestMigrationFilesEmbedded(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 1 {
		t.Fatal("expected at least one migration .sql file")
	}
}
