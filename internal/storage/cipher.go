//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	sqlite3 "github.com/0xCarbon/go-sqlite3"
)

// A connector keeps key material out of DSNs and the process-wide driver
// registry, and reapplies it before SQLCipher reads any page on reconnection.
type cipherConnector struct {
	driver *sqlite3.SQLiteDriver
	uri    string
}

func (c *cipherConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.driver.Open(c.uri)
}

func (c *cipherConnector) Driver() driver.Driver { return c.driver }

func init() {
	// Earlier gnotes releases wrote Go time.Time's String representation.
	sqlite3.SQLiteTimestampFormats = append(sqlite3.SQLiteTimestampFormats,
		"2006-01-02 15:04:05.999999999 -0700 MST")
}

func openEncrypted(uri string, key []byte) (*sql.DB, error) {
	driver := &sqlite3.SQLiteDriver{EncryptionKeyBytes: key, ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		// Temporary sorting/index data stays in memory; mmap is disabled.
		_, err := conn.Exec("PRAGMA temp_store=MEMORY; PRAGMA mmap_size=0; PRAGMA cipher_memory_security=ON;", nil)
		if err != nil {
			return errors.New("cannot configure encrypted database connection")
		}
		return nil
	}}
	return sql.OpenDB(&cipherConnector{driver: driver, uri: uri}), nil
}
