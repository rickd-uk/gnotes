//go:build !cgo

package storage

import (
	"database/sql"
	"errors"
)

func openEncrypted(string, []byte) (*sql.DB, error) {
	return nil, errors.New("this build cannot use encrypted databases; install the SQLCipher-enabled release")
}
