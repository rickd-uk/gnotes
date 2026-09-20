package db

import (
	"database/sql"
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
	deleted_at DATETIME
  );
`

	_, err = DB.Exec(setupSQL)
	if err != nil {
		log.Fatal(err)
	}

	ensureDeletedAtColumn()
}

func ensureDeletedAtColumn() {
	rows, err := DB.Query("PRAGMA table_info(notes)")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	hasDeletedAt := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			log.Fatal(err)
		}
		if name == "deleted_at" {
			hasDeletedAt = true
		}
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	if !hasDeletedAt {
		if _, err := DB.Exec("ALTER TABLE notes ADD COLUMN deleted_at DATETIME"); err != nil {
			log.Fatal(err)
		}
	}
}
