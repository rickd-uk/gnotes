package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"gnotes/internal/db"
)

type personalRecoveryKey struct {
	Format       string `json:"format"`
	Version      int    `json:"version"`
	Username     string `json:"username"`
	KeyID        string `json:"key_id"`
	Key          []byte `json:"key"`
	Instructions string `json:"instructions"`
}

type encryptedPersonalBackup struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func personalBackupKey(userID int) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// INSERT OR IGNORE makes simultaneous first downloads return the same key.
	if _, err := db.DB.Exec("INSERT OR IGNORE INTO backup_keys(user_id,key) VALUES(?,?)", userID, key); err != nil {
		return nil, err
	}
	if err := db.DB.QueryRow("SELECT key FROM backup_keys WHERE user_id = ?", userID).Scan(&key); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid backup key")
	}
	return key, nil
}

func personalKeyID(key []byte) string {
	hash := sha256.Sum256(key)
	return hex.EncodeToString(hash[:16])
}

func backupDownloadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	var input struct {
		Password string `json:"password"`
		Kind     string `json:"kind"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	session := sessionFromContext(r)
	if input.Kind != "personal-key" && input.Kind != "personal-backup" && input.Kind != "site-key" {
		http.Error(w, "Choose a backup or recovery key", http.StatusBadRequest)
		return
	}
	if input.Kind == "site-key" && session.Role != "admin" {
		http.Error(w, "Administrator access required", http.StatusForbidden)
		return
	}
	if len(input.Password) == 0 || len(input.Password) > 72 {
		http.Error(w, "Enter your current password", http.StatusBadRequest)
		return
	}
	allowed, wait, err := consumePersistentLimit("backup-download", strconv.Itoa(session.UserID), 12, 15*time.Minute)
	if err != nil {
		http.Error(w, "Could not check request limits", http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many attempts. Please try again later", http.StatusTooManyRequests)
		return
	}
	var hash []byte
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id = ? AND active = 1", session.UserID).Scan(&hash); err != nil {
		http.Error(w, "Could not verify account", http.StatusServiceUnavailable)
		return
	}
	if !passwordMatches(hash, input.Password) {
		// A mistyped reauthentication password must not sign out a valid session.
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}
	input.Password = ""
	// Password checks are deliberately expensive. Recheck the session afterward
	// so a concurrent disable, reset, revocation or demotion cannot release a key.
	current, err := authenticateRequest(r)
	if err != nil || current.UserID != session.UserID {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if input.Kind == "site-key" && current.Role != "admin" {
		http.Error(w, "Administrator access required", http.StatusForbidden)
		return
	}
	var body []byte
	var filename string
	if input.Kind == "site-key" {
		path := os.Getenv("GNOTES_SITE_BACKUP_RECOVERY_FILE")
		if path == "" {
			http.Error(w, "Site recovery key is not configured", http.StatusServiceUnavailable)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.Error(w, "Site recovery key is unavailable", http.StatusServiceUnavailable)
			return
		}
		defer file.Close()
		body, err = io.ReadAll(io.LimitReader(file, 16385))
		if err != nil || len(body) == 0 || len(body) > 16384 {
			http.Error(w, "Site recovery key is unavailable", http.StatusServiceUnavailable)
			return
		}
		filename = "gnotes-site-backup-recovery.env"
	} else {
		key, err := personalBackupKey(session.UserID)
		if err != nil {
			http.Error(w, "Could not load recovery key", http.StatusInternalServerError)
			return
		}
		if input.Kind == "personal-key" {
			body, err = json.MarshalIndent(personalRecoveryKey{
				Format: "gnotes-recovery-key", Version: 1, Username: session.Username, KeyID: personalKeyID(key), Key: key,
				Instructions: "Keep this file private and separate from your encrypted backups. To restore your notes, open Account > Export & import, choose your .gnotes-backup file and this recovery key, then Import file. This key unlocks only personal note backups made for this account. It does not reset your login password or unlock the whole-site Wasabi backup.",
			}, "", "  ")
			filename = "gnotes-" + safeTransferName(session.Username) + "-recovery-key.json"
		} else {
			notes, loadErr := loadTransferNotes(session.UserID, nil)
			if loadErr != nil {
				code := http.StatusInternalServerError
				if errors.Is(loadErr, errTransferTooLarge) {
					code = http.StatusRequestEntityTooLarge
				}
				http.Error(w, "Could not export personal notes", code)
				return
			}
			plain, marshalErr := json.Marshal(transferFile{Format: "gnotes", Version: transferVersion, ExportedAt: time.Now().UTC(), Notes: notes})
			if marshalErr != nil || len(plain) > maxTransferBytes {
				http.Error(w, "Backup exceeds the size limit", http.StatusRequestEntityTooLarge)
				return
			}
			block, _ := aes.NewCipher(key)
			gcm, _ := cipher.NewGCM(block)
			nonce := make([]byte, gcm.NonceSize())
			if _, err = rand.Read(nonce); err != nil {
				http.Error(w, "Could not encrypt backup", http.StatusInternalServerError)
				return
			}
			body, err = json.Marshal(encryptedPersonalBackup{Format: "gnotes-encrypted-backup", Version: 1, KeyID: personalKeyID(key), Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, plain, []byte("gnotes-personal-backup-v1"))})
			filename = "gnotes-" + safeTransferName(session.Username) + "-" + time.Now().UTC().Format("20060102T150405Z") + ".gnotes-backup"
		}
		if err != nil {
			http.Error(w, "Could not prepare download", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write(body)
}
