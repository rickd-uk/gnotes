package main

import (
	"database/sql"
	"encoding/json"
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

	http.HandleFunc("/api/notes/create", createNoteHandler)
	http.HandleFunc("/api/notes/list", listNotesHandler)
	// define routes
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/api/health", healthCheck)

	// start server
	fmt.Println("gnotes started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createNoteHandler(w http.ResponseWriter, r *http.Request) {
	// only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// decode incoming json
	var n Note
	err := json.NewDecoder(r.Body).Decode(&n)
	if err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	n.CreatedAt = time.Now()
	// insert into sqlite
	query := `INSERT INTO notes (title, content, created_at) VALUES(?,?,?)`
	result, err := db.Exec(query, n.Title, n.Content, n.CreatedAt)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// get ID of note we just created
	id, _ := result.LastInsertId()
	n.ID = int(id)

	// respond with creted note (including its new ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(n)
}

func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	// quey db
	rows, err := db.Query("SELECT id, title, content, created_at FROM notes ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close() // always close to free up db con

	var allNotes []Note

	for rows.Next() {
		var n Note
		// scan cols. into struct fields
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt)
		if err != nil {
			log.Println("Scan error:", err)
			continue
		}
		allNotes = append(allNotes, n)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allNotes)
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "<h1>gnotes</h1><p>System online!</p>")
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Backend is healthy and SQLite is connected.")
}
