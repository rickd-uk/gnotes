package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gnotes/internal/db"
	"gnotes/internal/models"
)

// helper for markdown
func mdToHTML(raw string) string {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	var buf bytes.Buffer
	if err := md.Convert([]byte(raw), &buf); err != nil {
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
	http.HandleFunc("/api/notes/update", updateNoteHandler)
	http.HandleFunc("/api/notes/delete", deleteNoteHandler)
	http.HandleFunc("/api/notes/trash", trashNotesHandler)
	http.HandleFunc("/api/notes/restore", restoreNotesHandler)
	http.HandleFunc("/api/notes/empty-trash", emptyTrashHandler)
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
	if strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Content) == "" {
		http.Error(w, "A title or content is required", http.StatusBadRequest)
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
	rows, err := db.DB.Query("SELECT id, title, content, created_at FROM notes WHERE deleted_at IS NULL ORDER BY created_at DESC")
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

func updateNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Use PUT or POST", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	var n models.Note
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Content) == "" {
		http.Error(w, "A title or content is required", http.StatusBadRequest)
		return
	}

	result, err := db.DB.Exec(
		"UPDATE notes SET title = ?, content = ? WHERE id = ? AND deleted_at IS NULL",
		n.Title,
		n.Content,
		id,
	)
	if err != nil {
		http.Error(w, "Update failed", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Update failed", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func deleteNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Use DELETE or POST", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec(
		"UPDATE notes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL",
		time.Now(),
		id,
	)
	if err != nil {
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func trashNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(
		"SELECT id, title, content, created_at, deleted_at FROM notes WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC",
	)
	if err != nil {
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	trash := make([]models.Note, 0)
	for rows.Next() {
		var n models.Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt, &n.DeletedAt); err != nil {
			log.Println("Trash scan error:", err)
			continue
		}
		trash = append(trash, n)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trash)
}

func restoreNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	query := "UPDATE notes SET deleted_at = NULL WHERE deleted_at IS NOT NULL"
	args := []any{}
	if id != "all" {
		query += " AND id = ?"
		args = append(args, id)
	}

	result, err := db.DB.Exec(query, args...)
	if err != nil {
		http.Error(w, "Restore failed", http.StatusInternalServerError)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Restore failed", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 && id != "all" {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func emptyTrashHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Use DELETE or POST", http.StatusMethodNotAllowed)
		return
	}

	if _, err := db.DB.Exec("DELETE FROM notes WHERE deleted_at IS NOT NULL"); err != nil {
		http.Error(w, "Could not empty trash", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Backend is healthy and SQLite is connected.")
}
