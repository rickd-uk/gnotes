package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gnotes/internal/db"
)

const (
	defaultSeedCount = 500
	maxSeedCount     = 100000
)

type seedConfig struct {
	database        string
	username        string
	count           int
	clean           bool
	confirmTestData bool
	allowProduction bool
	now             time.Time
}

type seedStats struct {
	Created int
	Pinned  int
	Trashed int
	Deleted int
	Batch   string
}

type generatedNote struct {
	Title     string
	Content   string
	CreatedAt time.Time
	DeletedAt *time.Time
	Pinned    bool
}

func main() {
	config := seedConfig{}
	flag.StringVar(&config.database, "database", "", "SQLite database path (required)")
	flag.StringVar(&config.username, "user", "", "existing username to receive test notes (required)")
	flag.IntVar(&config.count, "count", defaultSeedCount, "number of test notes")
	flag.BoolVar(&config.clean, "clean", false, "remove generated notes for this user")
	flag.BoolVar(&config.confirmTestData, "confirm-test-data", false, "confirm that generated test data may be written or removed")
	flag.BoolVar(&config.allowProduction, "allow-production", false, "allow a database under /var/lib/gnotes")
	flag.Parse()
	config.now = time.Now()

	stats, err := runSeed(config)
	if err != nil {
		log.Fatal(err)
	}
	if config.clean {
		fmt.Printf("Removed %d generated notes for %s.\n", stats.Deleted, config.username)
		return
	}
	fmt.Printf(
		"Created %d test notes for %s (%d pinned, %d in recycle bin). Batch: %s\n",
		stats.Created, config.username, stats.Pinned, stats.Trashed, stats.Batch,
	)
}

func runSeed(config seedConfig) (seedStats, error) {
	if !config.confirmTestData {
		return seedStats{}, errors.New("refusing to change data without -confirm-test-data")
	}
	if strings.TrimSpace(config.database) == "" {
		return seedStats{}, errors.New("-database is required")
	}
	if strings.TrimSpace(config.username) == "" {
		return seedStats{}, errors.New("-user is required")
	}
	if config.count < 1 || config.count > maxSeedCount {
		return seedStats{}, fmt.Errorf("-count must be between 1 and %d", maxSeedCount)
	}
	if productionDatabasePath(config.database) && !config.allowProduction {
		return seedStats{}, errors.New("refusing a database under /var/lib/gnotes; use -allow-production only if you deliberately want test data on production")
	}
	if config.now.IsZero() {
		config.now = time.Now()
	}

	db.InitDB(config.database)
	defer db.DB.Close()

	var userID int
	if err := db.DB.QueryRow("SELECT id FROM users WHERE username = ? COLLATE NOCASE", config.username).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return seedStats{}, fmt.Errorf("user %q does not exist", config.username)
		}
		return seedStats{}, fmt.Errorf("find user: %w", err)
	}
	if err := ensureSeedRegistry(); err != nil {
		return seedStats{}, err
	}
	if config.clean {
		return cleanSeededNotes(userID)
	}
	return createSeededNotes(config, userID)
}

func ensureSeedRegistry() error {
	_, err := db.DB.Exec(`
CREATE TABLE IF NOT EXISTS development_seed_notes (
  note_id INTEGER PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL,
  batch TEXT NOT NULL,
  seeded_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_development_seed_notes_user
  ON development_seed_notes (user_id, note_id);`)
	if err != nil {
		return fmt.Errorf("create test-data registry: %w", err)
	}
	return nil
}

func createSeededNotes(config seedConfig, userID int) (seedStats, error) {
	batch, err := newBatchID(config.now)
	if err != nil {
		return seedStats{}, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return seedStats{}, err
	}
	defer tx.Rollback()

	insertNote, err := tx.Prepare(`INSERT INTO notes
  (user_id, title, content, created_at, deleted_at, pinned)
  VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return seedStats{}, err
	}
	defer insertNote.Close()
	registerNote, err := tx.Prepare(`INSERT INTO development_seed_notes
  (note_id, user_id, batch, seeded_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return seedStats{}, err
	}
	defer registerNote.Close()

	stats := seedStats{Batch: batch}
	for i := 0; i < config.count; i++ {
		note := generateNote(i, config.now)
		result, err := insertNote.Exec(
			userID, note.Title, note.Content, note.CreatedAt, note.DeletedAt, note.Pinned,
		)
		if err != nil {
			return seedStats{}, fmt.Errorf("insert note %d: %w", i+1, err)
		}
		noteID, err := result.LastInsertId()
		if err != nil {
			return seedStats{}, err
		}
		if _, err := registerNote.Exec(noteID, userID, batch, config.now); err != nil {
			return seedStats{}, fmt.Errorf("register note %d: %w", i+1, err)
		}
		stats.Created++
		if note.Pinned {
			stats.Pinned++
		}
		if note.DeletedAt != nil {
			stats.Trashed++
		}
	}
	if err := tx.Commit(); err != nil {
		return seedStats{}, err
	}
	return stats, nil
}

func cleanSeededNotes(userID int) (seedStats, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return seedStats{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM notes
WHERE user_id = ? AND id IN (
  SELECT note_id FROM development_seed_notes WHERE user_id = ?
)`, userID, userID)
	if err != nil {
		return seedStats{}, fmt.Errorf("delete generated notes: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return seedStats{}, err
	}
	if _, err := tx.Exec("DELETE FROM development_seed_notes WHERE user_id = ?", userID); err != nil {
		return seedStats{}, err
	}
	if err := tx.Commit(); err != nil {
		return seedStats{}, err
	}
	return seedStats{Deleted: int(deleted)}, nil
}

func generateNote(index int, now time.Time) generatedNote {
	dayOffsets := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 14, 30, 90, 180, 365, 550, 730}
	daysAgo := 0
	if index < len(dayOffsets) {
		daysAgo = dayOffsets[index]
	} else {
		daysAgo = (index * 37) % 731
	}
	createdAt := time.Date(now.Year(), now.Month(), now.Day(), 8+(index%12), (index*7)%60, index%60, 0, now.Location()).AddDate(0, 0, -daysAgo)
	if index == 15 {
		createdAt = time.Date(2024, time.June, 15, 12, 0, 0, 0, now.Location())
		daysAgo = int(now.Sub(createdAt).Hours() / 24)
	}
	if createdAt.After(now) {
		createdAt = now.Add(-time.Duration(index+1) * time.Minute)
	}
	deleted := index%11 == 0
	pinned := !deleted && index%23 == 0
	var deletedAt *time.Time
	if deleted {
		value := createdAt.Add(time.Duration(2+index%72) * time.Hour)
		if value.After(now) {
			value = now.Add(-time.Duration(index+1) * time.Minute)
		}
		deletedAt = &value
	}

	styles := []struct {
		title   string
		content string
	}{
		{"Daily log", "## Progress\n\n- Finished the first task\n- Reviewed the next step\n\n**Status:** going well."},
		{"Shopping list", "- Oats\n- Coffee\n- Apples\n- Dark chocolate"},
		{"Project checklist", "- [x] Sketch the idea\n- [x] Build a prototype\n- [ ] Test on mobile\n- [ ] Ship it"},
		{"Code fragment", "```go\nfunc greet(name string) string {\n    return \"Hello, \" + name\n}\n```\n\nRemember to add a test."},
		{"Useful reference", "Read [Go documentation](https://go.dev/doc/) and capture the important points here."},
		{"Meeting notes", "> Keep the interface calm and make the common action obvious.\n\nDecision: review again next week."},
		{"A small idea", "> [!TIP]\n> Search for **OrchidSignal** to find generated examples quickly."},
		{"Unicode and symbols", "日本語のメモ · café · naïve · £42 · ✓ complete\n\nMixed-case marker: ORCHIDSIGNAL."},
		{"Long-form note", strings.Repeat("This paragraph exercises longer note rendering and compact display. ", 12)},
		{"Case-search sample", "Try searching for orchidsignal, OrchidSignal, and ORCHIDSIGNAL with case matching on and off."},
	}
	style := styles[index%len(styles)]
	title := fmt.Sprintf("%s · %04d", style.title, index+1)
	if index%7 == 0 {
		title = ""
	}
	content := style.content
	if title == "" {
		content = fmt.Sprintf("Untitled test note %04d\n\n%s", index+1, content)
	}
	content += fmt.Sprintf("\n\nSeed sequence: %04d · %d days ago", index+1, daysAgo)
	return generatedNote{Title: title, Content: content, CreatedAt: createdAt, DeletedAt: deletedAt, Pinned: pinned}
}

func newBatchID(now time.Time) (string, error) {
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("create batch ID: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(randomBytes), nil
}

func productionDatabasePath(path string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	productionRoot := filepath.Clean("/var/lib/gnotes")
	return absolute == productionRoot || strings.HasPrefix(absolute, productionRoot+string(os.PathSeparator))
}
