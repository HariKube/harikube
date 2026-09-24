package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// createBloatedDB creates a temporary SQLite database in WAL mode with the kine
// schema, inserts rowCount rows, then deletes them all. The deleted pages remain
// on SQLite's internal freelist so the file stays large on disk despite holding
// no data — simulating the post-compaction bloat that VACUUM is meant to fix.
// It returns the open *sql.DB.
func createBloatedDB(t *testing.T, rowCount int) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	connector, err := newConnector("sqlite3", dbPath+"?"+DefaultParams)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}

	db := sql.OpenDB(connector)

	// Create the kine table (same schema as production).
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS kine
		(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name INTEGER,
			uid INTEGER,
			created INTEGER,
			deleted INTEGER,
			create_revision INTEGER,
			prev_revision INTEGER,
			lease INTEGER,
			value BLOB,
			old_value BLOB
		)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert rows with non-trivial payload to grow the file.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("failed to begin insert tx: %v", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO kine (name, created, deleted, create_revision, prev_revision, lease, value, old_value)
		VALUES (?, 1, 0, 1, 0, 0, randomblob(512), randomblob(512))`)
	if err != nil {
		t.Fatalf("failed to prepare insert: %v", err)
	}

	for i := range rowCount {
		if _, err := stmt.Exec(i); err != nil {
			t.Fatalf("failed to insert row %d: %v", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit insert tx: %v", err)
	}

	stmt.Close()

	// Checkpoint WAL into main DB so the file size reflects all data.
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("failed to checkpoint: %v", err)
	}

	// Delete all rows - pages go to the freelist, file does not shrink.
	if _, err := db.Exec(`DELETE FROM kine`); err != nil {
		t.Fatalf("failed to delete rows: %v", err)
	}

	// Checkpoint again so the deletes are in the main DB file.
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("failed to checkpoint after delete: %v", err)
	}

	return db
}

// freelistCount returns the number of pages on SQLite's internal freelist.
func freelistCount(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	var count int64
	if err := db.QueryRow(`SELECT freelist_count FROM pragma_freelist_count()`).Scan(&count); err != nil {
		t.Fatalf("failed to query freelist_count: %v", err)
	}

	return count
}

func historyRowCount(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()

	var count int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("failed to count %s rows: %v", table, err)
	}

	return count
}

func TestSetupStreamsDeletedRowsToSeparateTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	connector, err := newConnector("sqlite3", dbPath+"?"+DefaultParams)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}

	db := sql.OpenDB(connector)
	defer db.Close()

	if err := setup(db, false, false, true, false); err != nil {
		t.Fatalf("initial setup() failed: %v", err)
	}

	_, err = db.Exec(`INSERT INTO kine (id, name, created, deleted, create_revision, prev_revision, lease, value, old_value)
		VALUES
			(1, '/registry/a', 1, 0, 1, 0, 0, x'01', NULL),
			(2, '/registry/a', 1, 0, 2, 1, 0, x'02', x'01'),
			(3, '/registry/b', 1, 1, 3, 0, 0, x'03', NULL)`)
	if err != nil {
		t.Fatalf("failed to seed kine rows: %v", err)
	}

	if err := setup(db, false, false, true, true); err != nil {
		t.Fatalf("history setup() failed: %v", err)
	}

	if got := historyRowCount(t, db, "kine"); got != 3 {
		t.Fatalf("kine row count = %d, want 3", got)
	}
	if got := historyRowCount(t, db, "kine_history"); got != 0 {
		t.Fatalf("kine_history row count = %d, want 0", got)
	}

	var exists bool
	if _, err := db.Exec(`DELETE FROM kine WHERE id = 2`); err != nil {
		t.Fatalf("failed to delete live kine row: %v", err)
	}

	if got := historyRowCount(t, db, "kine_history"); got != 1 {
		t.Fatalf("kine_history row count after delete = %d, want 1", got)
	}
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM kine_history WHERE id = 2)`).Scan(&exists); err != nil {
		t.Fatalf("failed to check archived delete row: %v", err)
	}
	if !exists {
		t.Fatal("expected deleted kine row to be streamed into kine_history")
	}
}

func TestSetupVacuumReclaimsDiskSpace(t *testing.T) {
	const rowCount = 10000

	db := createBloatedDB(t, rowCount)
	defer db.Close()

	freelistBefore := freelistCount(t, db)
	if freelistBefore == 0 {
		t.Fatal("freelist is empty before setup - test setup is broken")
	}

	// Run setup with VACUUM enabled (noStartupVacuum=false).
	if err := setup(db, false, false, false, false); err != nil {
		t.Fatalf("setup() failed: %v", err)
	}

	freelistAfter := freelistCount(t, db)

	// After VACUUM the freelist should be empty — all unused pages are
	// reclaimed and the database file is rewritten without gaps.
	if freelistAfter != 0 {
		t.Errorf("VACUUM did not reclaim freelist pages: before=%d, after=%d (expected 0)", freelistBefore, freelistAfter)
	}

	t.Logf("VACUUM reclaimed freelist pages: %d -> %d", freelistBefore, freelistAfter)
}

func TestSetupVacuumDisabledPreservesFileSize(t *testing.T) {
	const rowCount = 10000

	db := createBloatedDB(t, rowCount)
	defer db.Close()

	freelistBefore := freelistCount(t, db)
	if freelistBefore == 0 {
		t.Fatal("freelist is empty before setup - test setup is broken")
	}

	// Run setup with VACUUM disabled (noStartupVacuum=true).
	if err := setup(db, false, false, true, false); err != nil {
		t.Fatalf("setup() failed: %v", err)
	}

	freelistAfter := freelistCount(t, db)

	// With VACUUM disabled, the freelist should remain substantially
	// unchanged. Schema/index creation is idempotent but may consume a
	// handful of freelist pages, so allow a small tolerance.
	lowerBound := freelistBefore * 9 / 10
	if freelistAfter < lowerBound {
		t.Errorf("freelist unexpectedly shrank without VACUUM: before=%d, after=%d (expected >= %d)", freelistBefore, freelistAfter, lowerBound)
	}

	t.Logf("No VACUUM: freelist pages before=%d, after=%d", freelistBefore, freelistAfter)
}
