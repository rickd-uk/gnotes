package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/glebarez/go-sqlite"
)

var DB *sql.DB

func InitDB(filepath string) {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		log.Fatal(err)
	}
	// A single connection avoids SQLite writer contention inside one process and
	// ensures the connection-scoped safety pragmas below apply to every query.
	DB.SetMaxOpenConns(1)
	DB.SetMaxIdleConns(1)
	if err := DB.Ping(); err != nil {
		log.Fatal(err)
	}
	if _, err := DB.Exec(`
  PRAGMA busy_timeout = 5000;
  PRAGMA foreign_keys = ON;
  PRAGMA synchronous = FULL;
`); err != nil {
		log.Fatal(err)
	}
	if filepath != ":memory:" {
		var journalMode string
		if err := DB.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil || journalMode != "wal" {
			log.Fatalf("could not enable SQLite WAL mode: mode=%q error=%v", journalMode, err)
		}
	}
	var integrity string
	if err := DB.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		log.Fatalf("SQLite integrity check failed: result=%q error=%v", integrity, err)
	}

	setupSQL := `
  CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash BLOB NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL
  );

  CREATE TABLE IF NOT EXISTS notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
	title TEXT,
    content TEXT,
	created_at DATETIME,
	deleted_at DATETIME,
	pinned INTEGER NOT NULL DEFAULT 0
  );

  CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
  );

  CREATE TABLE IF NOT EXISTS drafts (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL
  );

  CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
  );

  CREATE TABLE IF NOT EXISTS rate_limits (
    scope TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    window_started_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (scope, key_hash)
  );

  CREATE TABLE IF NOT EXISTS login_cooldowns (
    username_hash TEXT PRIMARY KEY,
    failures INTEGER NOT NULL,
    last_failed_at DATETIME NOT NULL,
    blocked_until DATETIME NOT NULL
  );

  CREATE TABLE IF NOT EXISTS signup_events (
    user_id INTEGER PRIMARY KEY,
    ip_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL
  );

  CREATE TABLE IF NOT EXISTS invitations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash TEXT NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    used_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    used_at DATETIME
  );

  CREATE TABLE IF NOT EXISTS security_daily (
    day TEXT NOT NULL,
    event TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, event)
  );

  INSERT OR IGNORE INTO settings (key, value) VALUES ('signups_enabled', 'false');
  INSERT OR IGNORE INTO settings (key, value) VALUES ('signup_daily_limit', '20');
  INSERT OR IGNORE INTO settings (key, value) VALUES ('signup_ip_daily_limit', '3');
  INSERT OR IGNORE INTO settings (key, value) VALUES ('invite_required', 'true');
`

	_, err = DB.Exec(setupSQL)
	if err != nil {
		log.Fatal(err)
	}
	migrateSignupEvents()

	ensureColumn("deleted_at", "DATETIME")
	ensureColumn("pinned", "INTEGER NOT NULL DEFAULT 0")
	ensureColumn("user_id", "INTEGER")
	ensureTableColumn("users", "role", "TEXT NOT NULL DEFAULT 'user'")
	ensureTableColumn("users", "active", "INTEGER NOT NULL DEFAULT 1")

	indexSQL := `
  CREATE INDEX IF NOT EXISTS idx_notes_user_active
    ON notes (user_id, deleted_at, pinned, created_at);
  CREATE INDEX IF NOT EXISTS idx_sessions_expiry
    ON sessions (expires_at);
  CREATE INDEX IF NOT EXISTS idx_sessions_user
    ON sessions (user_id);
  CREATE INDEX IF NOT EXISTS idx_rate_limits_updated
    ON rate_limits (updated_at);
  CREATE INDEX IF NOT EXISTS idx_signup_events_created
    ON signup_events (created_at, ip_hash);
  CREATE INDEX IF NOT EXISTS idx_invitations_expiry
    ON invitations (expires_at, used_at);
`
	if _, err := DB.Exec(indexSQL); err != nil {
		log.Fatal(err)
	}
	if filepath != ":memory:" {
		if err := os.Chmod(filepath, 0o600); err != nil {
			log.Printf("warning: could not restrict database file permissions: %v", err)
		}
	}
}

func migrateSignupEvents() {
	rows, err := DB.Query("PRAGMA foreign_key_list(signup_events)")
	if err != nil {
		log.Fatal(err)
	}
	hasForeignKey := rows.Next()
	if err := rows.Close(); err != nil {
		log.Fatal(err)
	}
	if !hasForeignKey {
		return
	}

	tx, err := DB.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
  CREATE TABLE signup_events_new (
    user_id INTEGER PRIMARY KEY,
    ip_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL
  );
  INSERT INTO signup_events_new (user_id, ip_hash, created_at)
    SELECT user_id, ip_hash, created_at FROM signup_events;
  DROP TABLE signup_events;
  ALTER TABLE signup_events_new RENAME TO signup_events;
`); err != nil {
		log.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

func ensureColumn(columnName, declaration string) {
	ensureTableColumn("notes", columnName, declaration)
}

func ensureTableColumn(tableName, columnName, declaration string) {
	rows, err := DB.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	hasColumn := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			log.Fatal(err)
		}
		if name == columnName {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		log.Fatal(err)
	}

	if !hasColumn {
		query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, declaration)
		if _, err := DB.Exec(query); err != nil {
			log.Fatal(err)
		}
	}
}
