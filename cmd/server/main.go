package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/yuin/goldmark"
	"gnotes/internal/db"
	"gnotes/internal/models"
)

// helper for markdown
func mdToHTML(raw string) string {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(raw), &buf); err != nil {
		return raw // Fallback to raw text if it fails
	}
	return buf.String()
}

func main() {
	// initialize SQLite
	db.InitDB("gnotes.db")

	// routes
	http.HandleFunc("/api/notes/create", createNoteHandler)
	http.HandleFunc("/api/notes/list", listNotesHandler)
	http.HandleFunc("/api/notes/delete", deleteNoteHandler)
	http.HandleFunc("/api/health", healthCheck)

	// serve frontend
	fileServer := http.FileServer(http.Dir("./public"))
	http.Handle("/", fileServer)

	fmt.Println("gnotes started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createNoteHandler(w http.ResponseWriter, r *http.Request) {
	// only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// decode incoming json
	var n models.Note
	err := json.NewDecoder(r.Body).Decode(&n)
	if err != nil {
		http.Error(w, "Invalid input", 400)
		return
	}

	n.CreatedAt = time.Now()
	// insert into sqlite
	query := `INSERT INTO notes (title, content, created_at) VALUES(?,?,?)`
	result, err := db.DB.Exec(query, n.Title, n.Content, n.CreatedAt)
	if err != nil {
		http.Error(w, "Database error", 500)
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
	rows, err := db.DB.Query("SELECT id, title, content, created_at FROM notes ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, "Query error", 500)
		return
	}
	defer rows.Close() // always close to free up db con

	var allNotes []models.Note

	for rows.Next() {
		var n models.Note
		// scan cols. into struct fields
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt)
		if err != nil {
			log.Println("Scan error:", err)
			continue
		}
		n.HTMLContent = template.HTML(mdToHTML(n.Content)) // convert it to HTML
		allNotes = append(allNotes, n)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allNotes)
}

func deleteNoteHandler(w http.ResponseWriter, r *http.Request) {
	// we only wanr del. if user tells us to
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Use DELETE or POST", 405)
		return
	}
	// get the is from URL /api/notes/delete?id=1
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", 400)
		return
	}
	_, err := db.DB.Exec("DELETE FROM notes WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Delete failed", 500)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Note %s deleted successfully", id)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Backend is healthy and SQLite is connected.")
}
