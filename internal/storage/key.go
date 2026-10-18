package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// ReadKey accepts only a private regular file, not a symlink, device or FIFO.
// The text format is exactly a random 256-bit key in hexadecimal plus newline.
func ReadKey(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot open database key file")
	}
	file := os.NewFile(uintptr(fd), "database-key")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("database key must be a regular file accessible only to its owner (mode 0600 or 0400)")
	}
	text, err := io.ReadAll(io.LimitReader(file, 130))
	if err != nil {
		return nil, errors.New("cannot read database key file")
	}
	defer clear(text)
	key := make([]byte, 32)
	encoded := bytes.TrimSpace(text)
	if len(encoded) != 64 {
		return nil, errors.New("database key file must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.Decode(key, encoded); err != nil {
		clear(key)
		return nil, errors.New("invalid database key file")
	}
	return key, nil
}

func GenerateKey(path string) error {
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return errors.New("cannot generate database key")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create key file; existing keys are never overwritten")
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	encoded := make([]byte, 65)
	defer clear(encoded)
	hex.Encode(encoded, key)
	encoded[64] = '\n'
	if _, err := file.Write(encoded); err != nil {
		return errors.New("cannot write database key")
	}
	if err := file.Sync(); err != nil {
		return errors.New("cannot sync database key")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot close database key")
	}
	ok = true
	return nil
}
