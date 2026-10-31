package main

import (
	"net/http"
	"strconv"
)

// Missing preconditions fail closed so a page from an older release cannot
// silently overwrite a revision-aware client's edits.
func validRevision(w http.ResponseWriter, revision *int64) bool {
	if revision == nil {
		http.Error(w, "Reload gnotes before saving. Your text is still in this page.", http.StatusPreconditionRequired)
		return false
	}
	if *revision < 0 {
		http.Error(w, "Invalid revision", http.StatusBadRequest)
		return false
	}
	return true
}

func writeRevision(w http.ResponseWriter, revision int64) {
	w.Header().Set("X-Gnotes-Revision", strconv.FormatInt(revision, 10))
	w.Header().Set("Cache-Control", "no-store")
}
