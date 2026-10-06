package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"gnotes/internal/db"
)

const spellingListLimit = 2000

var spellingWordPattern = regexp.MustCompile(`^[\p{L}\p{N}]+(?:['-][\p{L}\p{N}]+)*$`)
var spellingNamePattern = regexp.MustCompile(`^[\p{L}\p{N}'’&.,() -]+$`)
var spellingLetterPattern = regexp.MustCompile(`\p{L}`)
var errSpellingLimit = errors.New("Each spelling list can hold up to 2,000 entries")

type spellingName struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type spellingLists struct {
	Words []string       `json:"words"`
	Names []spellingName `json:"names"`
}

type spellingEntry struct {
	Scope string `json:"scope"`
	Value string `json:"value"`
	Kind  string `json:"kind"`
}

func registerSpellingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/spelling", func(w http.ResponseWriter, r *http.Request) {
		protect(spellingHandler, r.Method != http.MethodGet)(w, r)
	})
	mux.HandleFunc("/api/spelling/migrate", protect(migrateSpellingHandler, true))
}

func normalizeSpellingEntry(input spellingEntry) (spellingEntry, string, error) {
	input.Value = strings.Join(strings.Fields(input.Value), " ")
	if len(utf16.Encode([]rune(input.Value))) > 96 || input.Value == "" {
		return input, "", errors.New("Enter a word or name using up to 96 characters")
	}
	switch input.Scope {
	case "word":
		input.Value = strings.ToLower(strings.ReplaceAll(input.Value, "’", "'"))
		if !spellingWordPattern.MatchString(input.Value) || (input.Kind != "" && input.Kind != "word") {
			return input, "", errors.New("Enter one word to ignore")
		}
		input.Kind = "word"
	case "name":
		if !spellingNamePattern.MatchString(input.Value) || !spellingLetterPattern.MatchString(input.Value) || len(strings.Fields(input.Value)) > 12 {
			return input, "", errors.New("Enter a name using up to 96 characters and 12 words")
		}
		if input.Kind != "person" && input.Kind != "company" && input.Kind != "place" && input.Kind != "other" {
			return input, "", errors.New("Choose Person, Company, Place, or Other")
		}
	default:
		return input, "", errors.New("Choose the ignored words or Names list")
	}
	return input, strings.ToLower(strings.ReplaceAll(input.Value, "’", "'")), nil
}

func addSpellingEntry(tx *sql.Tx, userID int, input spellingEntry, key string) error {
	// Existing entries keep their spelling/category during migration or retries.
	_, err := tx.Exec(`INSERT INTO spelling_entries(user_id,scope,entry_key,value,kind,created_at)
		SELECT ?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM spelling_entries WHERE user_id=? AND scope=?)<?
		ON CONFLICT(user_id,scope,entry_key) DO NOTHING`, userID, input.Scope, key, input.Value, input.Kind, time.Now().UTC().Format(time.RFC3339Nano), userID, input.Scope, spellingListLimit)
	if err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM spelling_entries WHERE user_id=? AND scope=? AND entry_key=?)", userID, input.Scope, key).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errSpellingLimit
	}
	return nil
}

func readSpellingLists(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT scope,value,kind FROM spelling_entries WHERE user_id=? ORDER BY scope,entry_key", userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load spelling lists", 500)
		return
	}
	defer rows.Close()
	lists := spellingLists{Words: []string{}, Names: []spellingName{}}
	for rows.Next() {
		var scope, value, kind string
		if err := rows.Scan(&scope, &value, &kind); err != nil {
			http.Error(w, "Could not load spelling lists", 500)
			return
		}
		if scope == "word" {
			lists.Words = append(lists.Words, value)
		} else {
			lists.Names = append(lists.Names, spellingName{value, kind})
		}
	}
	if rows.Err() != nil {
		http.Error(w, "Could not load spelling lists", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dictionaryJSON(w, lists)
}

func spellingWriteError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSpellingLimit) {
		http.Error(w, err.Error(), 409)
	} else {
		http.Error(w, "Could not save spelling lists", 500)
	}
}

func spellingHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		readSpellingLists(w, r)
	case http.MethodPost:
		var input spellingEntry
		if err := decodeJSONBody(w, r, &input); err != nil {
			http.Error(w, "Invalid spelling entry", 400)
			return
		}
		entry, key, err := normalizeSpellingEntry(input)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		tx, err := db.DB.Begin()
		if err != nil {
			spellingWriteError(w, err)
			return
		}
		defer tx.Rollback()
		if err := addSpellingEntry(tx, userIDFromRequest(r), entry, key); err != nil {
			spellingWriteError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			spellingWriteError(w, err)
			return
		}
		readSpellingLists(w, r)
	case http.MethodDelete:
		input := spellingEntry{Scope: r.URL.Query().Get("scope"), Value: r.URL.Query().Get("value")}
		if input.Scope == "name" {
			input.Kind = "other"
		}
		entry, key, err := normalizeSpellingEntry(input)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if _, err := db.DB.Exec("DELETE FROM spelling_entries WHERE user_id=? AND scope=? AND entry_key=?", userIDFromRequest(r), entry.Scope, key); err != nil {
			spellingWriteError(w, err)
			return
		}
		readSpellingLists(w, r)
	default:
		http.Error(w, "Use GET, POST, or DELETE", 405)
	}
}

func migrateSpellingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", 405)
		return
	}
	var input spellingLists
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "Invalid spelling lists", 400)
		return
	}
	if len(input.Words) > spellingListLimit || len(input.Names) > spellingListLimit {
		http.Error(w, errSpellingLimit.Error(), 409)
		return
	}
	entries := []spellingEntry{}
	keys := []string{}
	appendEntry := func(input spellingEntry) error {
		entry, key, err := normalizeSpellingEntry(input)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		keys = append(keys, key)
		return nil
	}
	for _, word := range input.Words {
		if err := appendEntry(spellingEntry{Scope: "word", Value: word}); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	for _, name := range input.Names {
		if err := appendEntry(spellingEntry{Scope: "name", Value: name.Name, Kind: name.Kind}); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		spellingWriteError(w, err)
		return
	}
	defer tx.Rollback()
	for i, entry := range entries {
		if err := addSpellingEntry(tx, userIDFromRequest(r), entry, keys[i]); err != nil {
			spellingWriteError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		spellingWriteError(w, err)
		return
	}
	readSpellingLists(w, r)
}
