package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gnotes/internal/db"
	"gnotes/internal/models"
)

const (
	maxJSONBodyBytes  = 2 * 1024 * 1024
	maxNoteTitleBytes = 4 * 1024
	maxNoteBodyBytes  = 1024 * 1024
	maxSearchBytes    = 4 * 1024
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// initialize SQLite
	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "gnotes.db"
	}
	db.InitDB(databasePath)
	defer db.DB.Close()

	// Authentication is public; all note and administration routes are protected.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/register", registerHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
	mux.HandleFunc("/api/draft/finalize", protect(finalizeDraftHandler, true))
	mux.HandleFunc("/api/notes/create", protect(createNoteHandler, true))
	mux.HandleFunc("/api/notes/list", protect(listNotesHandler, false))
	mux.HandleFunc("/api/notes/search", protect(searchNotesHandler, false))
	mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
	mux.HandleFunc("/api/notes/pin", protect(pinNoteHandler, true))
	mux.HandleFunc("/api/notes/delete", protect(deleteNoteHandler, true))
	mux.HandleFunc("/api/notes/delete-all", protect(deleteAllNotesHandler, true))
	mux.HandleFunc("/api/notes/trash", protect(trashNotesHandler, false))
	mux.HandleFunc("/api/notes/restore", protect(restoreNotesHandler, true))
	mux.HandleFunc("/api/notes/empty-trash", protect(emptyTrashHandler, true))
	mux.HandleFunc("/api/admin/overview", protect(requireAdmin(adminOverviewHandler), false))
	mux.HandleFunc("/api/admin/signups", protect(requireAdmin(adminSignupsHandler), true))
	mux.HandleFunc("/api/admin/users/status", protect(requireAdmin(adminUserStatusHandler), true))
	mux.HandleFunc("/api/admin/users/revoke", protect(requireAdmin(adminRevokeSessionsHandler), true))
	mux.HandleFunc("/api/admin/users/delete", protect(requireAdmin(adminDeleteUserHandler), true))
	mux.HandleFunc("/api/health", healthCheck)

	// serve frontend
	fileServer := http.FileServer(http.Dir("./public"))
	mux.Handle("/", fileServer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	address := net.JoinHostPort(host, port)
	server := &http.Server{
		Addr:              address,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("could not listen on %s: %w", address, err)
	}
	fmt.Printf("gnotes started at http://%s\n", address)

	serverErrors := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		return err
	case <-shutdownSignal.Done():
		log.Println("gnotes shutting down")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	return nil
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func createNoteHandler(w http.ResponseWriter, r *http.Request) {
	// only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// decode incoming json
	var n models.Note
	err := decodeJSONBody(w, r, &n)
	if err != nil {
		http.Error(w, "Invalid input", 400)
		return
	}
	if err := validateNoteText(n.Title, n.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	n.CreatedAt = time.Now()
	// insert into sqlite
	query := `INSERT INTO notes (user_id, title, content, created_at) VALUES(?,?,?,?)`
	result, err := db.DB.Exec(query, userIDFromRequest(r), n.Title, n.Content, n.CreatedAt)
	if err != nil {
		http.Error(w, "Database error", 500)
		return
	}

	// get ID of note we just created
	id, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	n.ID = int(id)

	// respond with creted note (including its new ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(n)
}

func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// quey db
	rows, err := db.DB.Query("SELECT id, title, content, created_at, pinned FROM notes WHERE user_id = ? AND deleted_at IS NULL ORDER BY pinned DESC, created_at DESC", userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Query error", 500)
		return
	}
	defer rows.Close() // always close to free up db con

	allNotes := make([]models.Note, 0)

	for rows.Next() {
		var n models.Note
		// scan cols. into struct fields
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt, &n.Pinned)
		if err != nil {
			log.Println("Scan error:", err)
			http.Error(w, "Could not load notes", http.StatusInternalServerError)
			return
		}
		n.HTMLContent = template.HTML(mdToHTML(n.Content)) // convert it to HTML
		allNotes = append(allNotes, n)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not load notes", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allNotes)
}

func searchNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queryText := r.URL.Query().Get("q")
	if len(queryText) > maxSearchBytes {
		http.Error(w, "Search text is too long", http.StatusBadRequest)
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "title" && scope != "content" {
		http.Error(w, "Invalid search scope", http.StatusBadRequest)
		return
	}

	conditions := []string{"user_id = ?", "deleted_at IS NULL"}
	args := []any{userIDFromRequest(r)}

	fromDate, err := parseSearchDate(r.URL.Query().Get("from"))
	if err != nil {
		http.Error(w, "Invalid start date", http.StatusBadRequest)
		return
	}
	toDate, err := parseSearchDate(r.URL.Query().Get("to"))
	if err != nil {
		http.Error(w, "Invalid end date", http.StatusBadRequest)
		return
	}
	if !fromDate.IsZero() && !toDate.IsZero() && fromDate.After(toDate) {
		http.Error(w, "Start date must not be after end date", http.StatusBadRequest)
		return
	}
	if !fromDate.IsZero() {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, fromDate)
	}
	if !toDate.IsZero() {
		conditions = append(conditions, "created_at < ?")
		args = append(args, toDate.AddDate(0, 0, 1))
	}

	if queryText != "" {
		matchCase := r.URL.Query().Get("match_case") == "true"
		matchExpression := func(column string) string {
			if matchCase {
				return "instr(" + column + ", ?) > 0"
			}
			return "instr(lower(" + column + "), lower(?)) > 0"
		}

		switch scope {
		case "title":
			conditions = append(conditions, matchExpression("title"))
			args = append(args, queryText)
		case "content":
			conditions = append(conditions, matchExpression("content"))
			args = append(args, queryText)
		default:
			conditions = append(conditions, "("+matchExpression("title")+" OR "+matchExpression("content")+")")
			args = append(args, queryText, queryText)
		}
	}

	query := "SELECT id, title, content, created_at, pinned FROM notes WHERE " +
		strings.Join(conditions, " AND ") +
		" ORDER BY pinned DESC, created_at DESC"
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		http.Error(w, "Search failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	matches := make([]models.Note, 0)
	for rows.Next() {
		var n models.Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt, &n.Pinned); err != nil {
			log.Println("Search scan error:", err)
			http.Error(w, "Search failed", http.StatusInternalServerError)
			return
		}
		n.HTMLContent = template.HTML(mdToHTML(n.Content))
		matches = append(matches, n)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Search failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(matches)
}

func parseSearchDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation("2006-01-02", value, time.Local)
}

func pinNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Use POST or PUT", http.StatusMethodNotAllowed)
		return
	}

	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := db.DB.Exec(
		"UPDATE notes SET pinned = CASE pinned WHEN 1 THEN 0 ELSE 1 END WHERE id = ? AND user_id = ? AND deleted_at IS NULL",
		id, userIDFromRequest(r),
	)
	if err != nil {
		http.Error(w, "Could not change pin", http.StatusInternalServerError)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not change pin", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func updateNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Use PUT or POST", http.StatusMethodNotAllowed)
		return
	}

	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}

	var n models.Note
	if err := decodeJSONBody(w, r, &n); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	if err := validateNoteText(n.Title, n.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.DB.Exec(
		"UPDATE notes SET title = ?, content = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL",
		n.Title,
		n.Content,
		id,
		userIDFromRequest(r),
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
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}
	result, err := db.DB.Exec(
		"UPDATE notes SET deleted_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL",
		time.Now(),
		id,
		userIDFromRequest(r),
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

func deleteAllNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	if _, err := db.DB.Exec(
		"UPDATE notes SET deleted_at = ? WHERE user_id = ? AND deleted_at IS NULL",
		time.Now(), userIDFromRequest(r),
	); err != nil {
		http.Error(w, "Delete failed", http.StatusInternalServerError)
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
		"SELECT id, title, content, created_at, deleted_at FROM notes WHERE user_id = ? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC",
		userIDFromRequest(r),
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
			http.Error(w, "Could not load trash", http.StatusInternalServerError)
			return
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

	idValue := r.URL.Query().Get("id")
	if idValue == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	query := "UPDATE notes SET deleted_at = NULL WHERE user_id = ? AND deleted_at IS NOT NULL"
	args := []any{userIDFromRequest(r)}
	if idValue != "all" {
		id, err := strconv.Atoi(idValue)
		if err != nil || id < 1 {
			http.Error(w, "Valid note ID required", http.StatusBadRequest)
			return
		}
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
	if rowsAffected == 0 && idValue != "all" {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateNoteText(title, content string) error {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(content) == "" {
		return errors.New("a title or content is required")
	}
	return validateNoteSize(title, content)
}

func validateNoteSize(title, content string) error {
	if len(title) > maxNoteTitleBytes {
		return errors.New("note title is too long")
	}
	if len(content) > maxNoteBodyBytes {
		return errors.New("note content is too long")
	}
	return nil
}

func noteIDFromRequest(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id < 1 {
		http.Error(w, "Valid note ID required", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func emptyTrashHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Use DELETE or POST", http.StatusMethodNotAllowed)
		return
	}

	if _, err := db.DB.Exec("DELETE FROM notes WHERE user_id = ? AND deleted_at IS NOT NULL", userIDFromRequest(r)); err != nil {
		http.Error(w, "Could not empty trash", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Use GET or HEAD", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := db.DB.PingContext(ctx); err != nil {
		http.Error(w, "Database is unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprint(w, "Backend is healthy and SQLite is connected.")
}
