package models

import (
	"html/template"
	"time"
)

// Note "Blueprint"
type Note struct {
	ID              int           `json:"id"`
	Title           string        `json:"title"`
	Content         string        `json:"content"` // Markdown here
	HTMLContent     template.HTML `json:"html_content"`
	CreatedAt       time.Time     `json:"created_at"`
	DeletedAt       *time.Time    `json:"deleted_at,omitempty"`
	ArchivedAt      *time.Time    `json:"archived_at,omitempty"`
	Pinned          bool          `json:"pinned"`
	BackgroundColor string        `json:"background_color,omitempty"`
	Tags            Tags          `json:"tags"`
}
