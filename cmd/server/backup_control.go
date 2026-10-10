package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"gnotes/internal/db"
)

// The app receives a private Unix socket, never host systemd or cloud credentials.
func backupControlProxy(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	socket := os.Getenv("GNOTES_BACKUP_CONTROL_SOCKET")
	if socket == "" {
		http.Error(w, "Backup setup is not installed on this server", http.StatusServiceUnavailable)
		return
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(r.Context(), method, "http://backup-control"+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Could not prepare backup action", 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport, Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, "Backup control is unavailable. Ask the server administrator to check its service.", 503)
		return
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil || len(payload) > 128*1024 || !json.Valid(payload) {
		http.Error(w, "Invalid backup control response", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	w.Write(payload)
}

func backupControlStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	backupControlProxy(w, r, "/status", nil)
}

func backupControlActionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Password      string `json:"password"`
		Action        string `json:"action"`
		Region        string `json:"region"`
		Bucket        string `json:"bucket"`
		Prefix        string `json:"prefix"`
		AccessKey     string `json:"access_key"`
		SecretKey     string `json:"secret_key"`
		AllowCreate   bool   `json:"allow_create"`
		RecoverySaved bool   `json:"recovery_saved"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	switch input.Action {
	case "configure", "enable", "pause", "stop", "run", "history":
	default:
		http.Error(w, "Choose a backup action", 400)
		return
	}
	if len(input.Password) == 0 || len(input.Password) > 72 {
		http.Error(w, "Enter your current password", 400)
		return
	}
	session := sessionFromContext(r)
	allowed, wait, err := consumePersistentLimit("backup-control", strconv.Itoa(session.UserID), 12, 15*time.Minute)
	if err != nil {
		http.Error(w, "Could not check request limits", 503)
		return
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many attempts. Please try again later", 429)
		return
	}
	var hash []byte
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id = ? AND active = 1", session.UserID).Scan(&hash); err != nil {
		http.Error(w, "Could not verify account", 503)
		return
	}
	if !passwordMatches(hash, input.Password) {
		http.Error(w, "Current password is incorrect", 403)
		return
	}
	current, err := authenticateRequest(r)
	if err != nil || current.UserID != session.UserID {
		http.Error(w, "Authentication required", 401)
		return
	}
	if current.Role != "admin" {
		http.Error(w, "Administrator access required", 403)
		return
	}
	// Do not forward or store the login password.
	body, _ := json.Marshal(map[string]any{
		"action": input.Action, "region": input.Region, "bucket": input.Bucket, "prefix": input.Prefix,
		"access_key": input.AccessKey, "secret_key": input.SecretKey,
		"allow_create": input.AllowCreate, "recovery_saved": input.RecoverySaved,
	})
	input.Password, input.SecretKey, input.AccessKey = "", "", ""
	backupControlProxy(w, r, "/action", body)
}
