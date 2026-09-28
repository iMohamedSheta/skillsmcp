package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Simulate a DB created by the pre-scope version, then Open with new code.
func TestMigrateOldDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE skills(
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		t.Fatalf("old schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO skills(id,name,description,content,enabled,created_at,updated_at)
		VALUES('1','git-commit','d','c',1,'t','t')`)
	if err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open old db: %v", err)
	}
	defer s.Close()
	globals := s.ListGlobalSkills(true)
	if len(globals) != 1 || globals[0].Name != "git-commit" {
		t.Fatalf("globals after migrate = %+v", globals)
	}
}
