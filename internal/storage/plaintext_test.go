package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyVerificationCannotCreateOrModifyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created.db")
	if database, err := Open(path, nil, "ro"); err == nil {
		database.Close()
		t.Fatal("read-only open created a missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only open left a database behind")
	}
	database, err := Open(path, nil, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE preserved(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	readOnly, err := Open(path, nil, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err := readOnly.Exec("INSERT INTO preserved VALUES('unexpected')"); err == nil {
		t.Fatal("read-only database accepted a write")
	}
}
