package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

// Note "Blueprint"
type Note struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"` // Markdown here
	CreatedAt time.Time `json:"created_at"`
}

var db *sql.DB

func main() {
	// initialize SQLite
	var err error
	db, err = sql.Open("sqlite", "gnotes.db")
	if err != nil {
		log.Fatal("DB Open error:", err)
	}

	// Crate table if it doen't exist
	setupSQL := `
  CREATE TABLE IF NOT EXISTS notes (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
	 title TEXT,
    content TEXT,
	created_at DATETIME
  );
`
	_, err = db.Exec(setupSQL)
	if err != nil {
		log.Fatal("Table Setup Error:", err)
	}
	// define routes
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/api/health", healthCheck)

	// start server
	fmt.Println("gnotes started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "<h1>gnotes</h1><p>System online!</p>")
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Backend is healthy and SQLite is connected.")
}
