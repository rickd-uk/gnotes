package models

import (
	"encoding/json"
	"html/template"
	"time"
)

// Note "Blueprint"
type Note struct {
	PrivacyLocked    bool          `json:"privacy_locked"`
	PrivacyHideTitle bool          `json:"privacy_hide_title,omitempty"`
	Locked           bool          `json:"locked"`
	ID               int           `json:"id"`
	Revision         int64         `json:"revision"`
	Title            string        `json:"title"`
	Content          string        `json:"content"` // Markdown here
	HTMLContent      template.HTML `json:"html_content"`
	CreatedAt        time.Time     `json:"created_at"`
	DeletedAt        *time.Time    `json:"deleted_at,omitempty"`
	ArchivedAt       *time.Time    `json:"archived_at,omitempty"`
	Favorited        bool          `json:"favorited"`
	Pinned           bool          `json:"pinned"`
	BackgroundColor  string        `json:"background_color,omitempty"`
	Tags             Tags          `json:"tags"`
}

// Privacy-locked text is never included in ordinary note API responses.
func (n Note) MarshalJSON() ([]byte, error) {
	type plain Note
	if n.PrivacyLocked {
		n.Content, n.HTMLContent, n.Tags = "", "", nil
		if n.PrivacyHideTitle {
			n.Title = ""
		}
	}
	return json.Marshal(plain(n))
}
