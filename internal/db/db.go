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
	created_at DATETIME
  );
`

	_, err = DB.Exec(setupSQL)
	if err != nil {
		log.Fatal(err)
	}
}
