package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

func normalizeTags(input models.Tags) (models.Tags, error) {
	if len(input) > 10 {
		return nil, errors.New("Use at most 10 tags per note")
	}
	result := models.Tags{}
	seen := map[string]bool{}
	for _, value := range input {
		if !utf8.ValidString(value) || len(value) > 256 {
			return nil, errors.New("Invalid tag")
		}
		value = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "#"))
		value = strings.Join(strings.Fields(value), "-")
		if value == "" || utf8.RuneCountInString(value) > 48 {
			return nil, errors.New("Tags must contain 1–48 characters")
		}
		for _, char := range value {
			if !unicode.IsLetter(char) && !unicode.IsNumber(char) && char != '-' && char != '_' {
				return nil, errors.New("Tags may contain letters, numbers, hyphens and underscores")
			}
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func tagsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", http.StatusMethodNotAllowed)
		return
	}
	archiveCondition := "n.archived_at IS NULL"
	if r.URL.Query().Get("archive") == "1" {
		archiveCondition = "n.archived_at IS NOT NULL"
	}
	rows, err := db.DB.Query(`SELECT t.value, COUNT(*) FROM notes n, json_each(n.tags) t
		WHERE n.user_id = ? AND n.deleted_at IS NULL AND `+archiveCondition+`
		GROUP BY t.value ORDER BY t.value`, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load tags", 500)
		return
	}
	defer rows.Close()
	type tagCount struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	tags := []tagCount{}
	for rows.Next() {
		var tag tagCount
		if err := rows.Scan(&tag.Name, &tag.Count); err != nil {
			http.Error(w, "Could not load tags", 500)
			return
		}
		tags = append(tags, tag)
	}
	if rows.Err() != nil {
		http.Error(w, "Could not load tags", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

func updateNoteTagsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}
	var input struct {
		Tags models.Tags `json:"tags"`
	}
	if decodeJSONBody(w, r, &input) != nil || input.Tags == nil {
		http.Error(w, "Supply a tags array", 400)
		return
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	result, err := db.DB.Exec("UPDATE notes SET tags=? WHERE id=? AND user_id=? AND deleted_at IS NULL", tags, id, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not save tags", 500)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not save tags", 500)
		return
	}
	if count == 0 {
		http.Error(w, "Note not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]models.Tags{"tags": tags})
}

func tagSearchFilter(r *http.Request, column string) (string, []any, error) {
	value := r.URL.Query().Get("tag")
	if value == "" {
		return "", nil, nil
	}
	tags, err := normalizeTags(models.Tags{value})
	if err != nil {
		return "", nil, err
	}
	return "EXISTS (SELECT 1 FROM json_each(" + column + ") WHERE value = ?)", []any{tags[0]}, nil
}
