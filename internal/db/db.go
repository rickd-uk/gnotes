package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/glebarez/go-sqlite"
)

var DB *sql.DB

const renderedContentCacheVersion = "2"

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
    last_login_at DATETIME,
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
	ensureColumn("rendered_content", "TEXT")
	ensureTableColumn("users", "role", "TEXT NOT NULL DEFAULT 'user'")
	ensureTableColumn("users", "active", "INTEGER NOT NULL DEFAULT 1")
	ensureTableColumn("users", "last_login_at", "DATETIME")
	ensureRenderedContentCacheVersion()
	ensureNotesFTS()

	indexSQL := `
  CREATE INDEX IF NOT EXISTS idx_notes_user_active
    ON notes (user_id, deleted_at, pinned, created_at);
  CREATE INDEX IF NOT EXISTS idx_notes_user_active_page
    ON notes (user_id, deleted_at, pinned DESC, created_at DESC, id DESC);
  CREATE INDEX IF NOT EXISTS idx_notes_user_trash_page
    ON notes (user_id, deleted_at DESC, id DESC);
  CREATE INDEX IF NOT EXISTS idx_notes_user_active_absolute_page
    ON notes (user_id, deleted_at, pinned DESC, unixepoch(created_at) DESC, id DESC);
  CREATE INDEX IF NOT EXISTS idx_notes_user_trash_absolute_page
    ON notes (user_id, unixepoch(deleted_at) DESC, id DESC);
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

// ensureRenderedContentCacheVersion invalidates cached Markdown after renderer
// changes. Notes are rendered lazily when they are next requested, avoiding a
// potentially expensive startup-time rewrite for large databases.
func ensureRenderedContentCacheVersion() {
	tx, err := DB.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	var version string
	err = tx.QueryRow("SELECT value FROM settings WHERE key = 'rendered_content_cache_version'").Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		log.Fatal(err)
	}
	if version == renderedContentCacheVersion {
		return
	}
	if _, err := tx.Exec("UPDATE notes SET rendered_content = NULL WHERE rendered_content IS NOT NULL"); err != nil {
		log.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES ('rendered_content_cache_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, renderedContentCacheVersion); err != nil {
		log.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

func ensureNotesFTS() {
	var tableExists int
	if err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'notes_fts'").Scan(&tableExists); err != nil {
		log.Fatal(err)
	}

	tx, err := DB.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
  CREATE TRIGGER IF NOT EXISTS notes_rendered_content_invalidate
    AFTER UPDATE OF content ON notes
    WHEN new.rendered_content IS old.rendered_content
  BEGIN
    UPDATE notes SET rendered_content = NULL WHERE id = new.id;
  END;
  CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
    title,
    content,
    content = 'notes',
    content_rowid = 'id',
    tokenize = 'trigram'
  );
  CREATE TRIGGER IF NOT EXISTS notes_fts_insert AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
  END;
  CREATE TRIGGER IF NOT EXISTS notes_fts_delete AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content)
      VALUES ('delete', old.id, old.title, old.content);
  END;
  CREATE TRIGGER IF NOT EXISTS notes_fts_update AFTER UPDATE OF title, content ON notes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, content)
      VALUES ('delete', old.id, old.title, old.content);
    INSERT INTO notes_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
  END;
`); err != nil {
		log.Fatal(err)
	}
	if tableExists == 0 {
		if _, err := tx.Exec("INSERT INTO notes_fts(notes_fts) VALUES ('rebuild')"); err != nil {
			log.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
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
