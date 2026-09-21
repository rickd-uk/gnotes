package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"gnotes/internal/db"
)

type archiveBucket struct {
	Key   string    `json:"key"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}

type archiveResponse struct {
	Total  int             `json:"total"`
	Months []archiveBucket `json:"months"`
	Weeks  []archiveBucket `json:"weeks"`
}

func noteArchiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	timezone := r.URL.Query().Get("timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	if len(timezone) > 64 {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`SELECT created_at FROM notes
    WHERE user_id = ? AND deleted_at IS NULL ORDER BY created_at DESC`, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load archive", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	months := make(map[string]*archiveBucket)
	weeks := make(map[string]*archiveBucket)
	total := 0
	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			http.Error(w, "Could not load archive", http.StatusInternalServerError)
			return
		}
		local := createdAt.In(location)
		monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		monthKey := monthStart.Format("2006-01")
		incrementArchiveBucket(months, monthKey, monthStart, monthStart.AddDate(0, 1, 0))

		weekdayOffset := (int(local.Weekday()) + 6) % 7
		weekStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -weekdayOffset)
		isoYear, isoWeek := local.ISOWeek()
		weekKey := fmt.Sprintf("%04d-W%02d", isoYear, isoWeek)
		incrementArchiveBucket(weeks, weekKey, weekStart, weekStart.AddDate(0, 0, 7))
		total++
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not load archive", http.StatusInternalServerError)
		return
	}

	response := archiveResponse{
		Total:  total,
		Months: sortedArchiveBuckets(months),
		Weeks:  sortedArchiveBuckets(weeks),
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func incrementArchiveBucket(buckets map[string]*archiveBucket, key string, start, end time.Time) {
	if bucket := buckets[key]; bucket != nil {
		bucket.Count++
		return
	}
	buckets[key] = &archiveBucket{Key: key, Start: start, End: end, Count: 1}
}

func sortedArchiveBuckets(source map[string]*archiveBucket) []archiveBucket {
	buckets := make([]archiveBucket, 0, len(source))
	for _, bucket := range source {
		buckets = append(buckets, *bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Start.After(buckets[j].Start) })
	return buckets
}
