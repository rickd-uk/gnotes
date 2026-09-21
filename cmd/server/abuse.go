package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gnotes/internal/db"
)

const (
	loginFailureWindow = 15 * time.Minute
	maxLoginCooldown   = 15 * time.Minute
	maxLimiterRows     = 50_000
)

var (
	errSignupsClosed      = errors.New("signups are closed")
	errSignupLimitReached = errors.New("daily signup limit reached")
	errInvitationRequired = errors.New("valid invitation required")
)

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type registrationPolicy struct {
	Enabled          bool `json:"enabled"`
	DailyLimit       int  `json:"daily_limit"`
	IPDailyLimit     int  `json:"ip_daily_limit"`
	InvitationNeeded bool `json:"invite_required"`
}

type securityStats struct {
	FailedLogins   int `json:"failed_logins"`
	BlockedLogins  int `json:"blocked_logins"`
	BlockedSignups int `json:"blocked_signups"`
	Registrations  int `json:"registrations"`
}

func loadRegistrationPolicy(query rowQuerier) (registrationPolicy, error) {
	values := make(map[string]string, 4)
	for _, key := range []string{"signups_enabled", "signup_daily_limit", "signup_ip_daily_limit", "invite_required"} {
		var value string
		if err := query.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value); err != nil {
			return registrationPolicy{}, err
		}
		values[key] = value
	}
	dailyLimit, err := strconv.Atoi(values["signup_daily_limit"])
	if err != nil {
		return registrationPolicy{}, err
	}
	ipDailyLimit, err := strconv.Atoi(values["signup_ip_daily_limit"])
	if err != nil {
		return registrationPolicy{}, err
	}
	return registrationPolicy{
		Enabled:          values["signups_enabled"] == "true",
		DailyLimit:       dailyLimit,
		IPDailyLimit:     ipDailyLimit,
		InvitationNeeded: values["invite_required"] == "true",
	}, nil
}

func consumePersistentLimit(scope, subject string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit < 1 {
		return true, 0, nil
	}
	now := time.Now().UTC()
	keyHash := hashToken(scope + ":" + subject)
	tx, err := db.DB.Begin()
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()

	var attempts int
	var windowStarted time.Time
	err = tx.QueryRow(
		"SELECT attempts, window_started_at FROM rate_limits WHERE scope = ? AND key_hash = ?",
		scope, keyHash,
	).Scan(&attempts, &windowStarted)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec("DELETE FROM rate_limits WHERE updated_at < ?", now.Add(-48*time.Hour)); err != nil {
			return false, 0, err
		}
		var rowCount int
		if err := tx.QueryRow("SELECT COUNT(*) FROM rate_limits").Scan(&rowCount); err != nil {
			return false, 0, err
		}
		if rowCount >= maxLimiterRows {
			if err := tx.Commit(); err != nil {
				return false, 0, err
			}
			return false, window, nil
		}
		_, err = tx.Exec(
			"INSERT INTO rate_limits (scope, key_hash, attempts, window_started_at, updated_at) VALUES (?, ?, 1, ?, ?)",
			scope, keyHash, now, now,
		)
		if err != nil {
			return false, 0, err
		}
		return true, 0, tx.Commit()
	}
	if err != nil {
		return false, 0, err
	}

	windowEnd := windowStarted.Add(window)
	if !windowEnd.After(now) {
		_, err = tx.Exec(
			"UPDATE rate_limits SET attempts = 1, window_started_at = ?, updated_at = ? WHERE scope = ? AND key_hash = ?",
			now, now, scope, keyHash,
		)
		if err != nil {
			return false, 0, err
		}
		return true, 0, tx.Commit()
	}
	if attempts >= limit {
		return false, windowEnd.Sub(now), nil
	}
	if _, err := tx.Exec(
		"UPDATE rate_limits SET attempts = attempts + 1, updated_at = ? WHERE scope = ? AND key_hash = ?",
		now, scope, keyHash,
	); err != nil {
		return false, 0, err
	}
	return true, 0, tx.Commit()
}

func loginCooldown(username string) (time.Duration, error) {
	var blockedUntil time.Time
	err := db.DB.QueryRow(
		"SELECT blocked_until FROM login_cooldowns WHERE username_hash = ?",
		hashUsername(username),
	).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	remaining := time.Until(blockedUntil)
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

func recordLoginFailure(username string) (time.Duration, error) {
	now := time.Now().UTC()
	usernameHash := hashUsername(username)
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	failures := 0
	var lastFailed time.Time
	err = tx.QueryRow(
		"SELECT failures, last_failed_at FROM login_cooldowns WHERE username_hash = ?",
		usernameHash,
	).Scan(&failures, &lastFailed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec("DELETE FROM login_cooldowns WHERE last_failed_at < ?", now.Add(-30*24*time.Hour)); err != nil {
			return 0, err
		}
		var rowCount int
		if err := tx.QueryRow("SELECT COUNT(*) FROM login_cooldowns").Scan(&rowCount); err != nil {
			return 0, err
		}
		if rowCount >= maxLimiterRows {
			if err := tx.Commit(); err != nil {
				return 0, err
			}
			return maxLoginCooldown, nil
		}
	}
	if errors.Is(err, sql.ErrNoRows) || now.Sub(lastFailed) >= loginFailureWindow {
		failures = 0
	}
	failures++

	cooldown := time.Duration(0)
	if failures >= 5 {
		shift := failures - 5
		if shift > 5 {
			shift = 5
		}
		cooldown = 30 * time.Second * time.Duration(1<<shift)
		if cooldown > maxLoginCooldown {
			cooldown = maxLoginCooldown
		}
	}
	blockedUntil := now.Add(cooldown)
	if _, err := tx.Exec(`
		INSERT INTO login_cooldowns (username_hash, failures, last_failed_at, blocked_until)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(username_hash) DO UPDATE SET
			failures = excluded.failures,
			last_failed_at = excluded.last_failed_at,
			blocked_until = excluded.blocked_until`,
		usernameHash, failures, now, blockedUntil,
	); err != nil {
		return 0, err
	}
	if err := incrementSecurityEvent(tx, "failed_login", now); err != nil {
		return 0, err
	}
	return cooldown, tx.Commit()
}

func clearLoginCooldown(username string) error {
	_, err := db.DB.Exec("DELETE FROM login_cooldowns WHERE username_hash = ?", hashUsername(username))
	return err
}

func hashUsername(username string) string {
	return hashToken("username:" + strings.ToLower(strings.TrimSpace(username)))
}

func hashedClientIP(r *http.Request) string {
	return hashToken("ip:" + clientIP(r))
}

func setRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
}

func cleanupAbuseData(now time.Time) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM rate_limits WHERE updated_at < ?", []any{now.UTC().Add(-48 * time.Hour)}},
		{"DELETE FROM login_cooldowns WHERE last_failed_at < ?", []any{now.UTC().Add(-30 * 24 * time.Hour)}},
		{"DELETE FROM signup_events WHERE created_at < ?", []any{now.UTC().Add(-31 * 24 * time.Hour)}},
		{"DELETE FROM invitations WHERE used_at < ? OR (used_at IS NULL AND expires_at < ?)", []any{now.UTC().Add(-180 * 24 * time.Hour), now.UTC().Add(-180 * 24 * time.Hour)}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM security_daily WHERE day < ?", now.UTC().AddDate(0, 0, -180).Format("2006-01-02")); err != nil {
		return err
	}
	return tx.Commit()
}

func incrementSecurityEvent(executor sqlExecutor, event string, when time.Time) error {
	if _, err := executor.Exec("DELETE FROM security_daily WHERE day < ?", when.UTC().AddDate(0, 0, -180).Format("2006-01-02")); err != nil {
		return err
	}
	_, err := executor.Exec(`
		INSERT INTO security_daily (day, event, count) VALUES (?, ?, 1)
		ON CONFLICT(day, event) DO UPDATE SET count = count + 1`,
		when.UTC().Format("2006-01-02"), event,
	)
	return err
}

func recordBlockedEvent(event string) {
	_ = incrementSecurityEvent(db.DB, event, time.Now().UTC())
}

func loadSecurityStats(now time.Time) (securityStats, error) {
	stats := securityStats{}
	rows, err := db.DB.Query("SELECT event, count FROM security_daily WHERE day = ?", now.UTC().Format("2006-01-02"))
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var event string
		var count int
		if err := rows.Scan(&event, &count); err != nil {
			return stats, err
		}
		switch event {
		case "failed_login":
			stats.FailedLogins = count
		case "blocked_login":
			stats.BlockedLogins = count
		case "blocked_signup":
			stats.BlockedSignups = count
		case "registration":
			stats.Registrations = count
		}
	}
	return stats, rows.Err()
}

func signupCapacity(query rowQuerier, policy registrationPolicy, ipHash string, now time.Time) error {
	start := now.UTC().Truncate(24 * time.Hour)
	if policy.DailyLimit > 0 {
		var count int
		if err := query.QueryRow("SELECT COUNT(*) FROM signup_events WHERE created_at >= ?", start).Scan(&count); err != nil {
			return err
		}
		if count >= policy.DailyLimit {
			return errSignupLimitReached
		}
	}
	if policy.IPDailyLimit > 0 {
		var count int
		if err := query.QueryRow(
			"SELECT COUNT(*) FROM signup_events WHERE ip_hash = ? AND created_at >= ?", ipHash, start,
		).Scan(&count); err != nil {
			return err
		}
		if count >= policy.IPDailyLimit {
			return errSignupLimitReached
		}
	}
	return nil
}

func checkRegistrationAccess(query rowQuerier, inviteCode, ipHash string, now time.Time) (registrationPolicy, error) {
	policy, err := loadRegistrationPolicy(query)
	if err != nil {
		return policy, err
	}
	if !policy.Enabled {
		return policy, errSignupsClosed
	}
	if err := signupCapacity(query, policy, ipHash, now); err != nil {
		return policy, err
	}
	if policy.InvitationNeeded {
		valid, err := validInvitation(query, inviteCode, now)
		if err != nil {
			return policy, err
		}
		if !valid {
			return policy, errInvitationRequired
		}
	}
	return policy, nil
}

func validInvitation(query rowQuerier, code string, now time.Time) (bool, error) {
	if strings.TrimSpace(code) == "" {
		return false, nil
	}
	var exists int
	err := query.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM invitations
			 WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?
		)`, hashToken(strings.TrimSpace(code)), now.UTC(),
	).Scan(&exists)
	return exists == 1, err
}

func consumeInvitation(executor sqlExecutor, code string, userID int64, now time.Time) error {
	result, err := executor.Exec(`
		UPDATE invitations SET used_by = ?, used_at = ?
		 WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		userID, now.UTC(), hashToken(strings.TrimSpace(code)), now.UTC(),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("invitation is no longer available")
	}
	return nil
}
