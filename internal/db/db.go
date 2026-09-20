package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/glebarez/go-sqlite"
)

var DB *sql.DB

func InitDB(filepath string) {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		log.Fatal(err)
	}

	// create table logic moves here
	setupSQL := `
  CREATE TABLE IF NOT EXISTS notes (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
	 title TEXT,
    content TEXT,
	created_at DATETIME,
	deleted_at DATETIME,
	pinned INTEGER NOT NULL DEFAULT 0
  );
`

	_, err = DB.Exec(setupSQL)
	if err != nil {
		log.Fatal(err)
	}

	ensureColumn("deleted_at", "DATETIME")
	ensureColumn("pinned", "INTEGER NOT NULL DEFAULT 0")
}

func ensureColumn(columnName, declaration string) {
	rows, err := DB.Query("PRAGMA table_info(notes)")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	hasColumn := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			log.Fatal(err)
		}
		if name == columnName {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	if !hasColumn {
		query := fmt.Sprintf("ALTER TABLE notes ADD COLUMN %s %s", columnName, declaration)
		if _, err := DB.Exec(query); err != nil {
			log.Fatal(err)
		}
	}
}
