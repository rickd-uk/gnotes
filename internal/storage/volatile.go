package storage

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// CheckVolatileKey refuses disk-backed keys and active swap. Merely keeping the
// key file on tmpfs is insufficient: copies in Go/SQLCipher/transport memory can
// otherwise be paged out. Call this before reading any deployment key bytes.
func CheckVolatileKey(path string) error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return errors.New("cannot disable core dumps for volatile-key operation")
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(path, &filesystem); err != nil {
		return errors.New("volatile database key is unavailable; supply it again after reboot")
	}
	swaps, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return errors.New("cannot verify swap safety for volatile-key operation")
	}
	return validateVolatilePolicy(filesystem.Type, swaps)
}

func validateVolatilePolicy(filesystem int64, swaps []byte) error {
	if filesystem != unix.TMPFS_MAGIC && filesystem != unix.RAMFS_MAGIC {
		return errors.New("volatile database key must be stored on tmpfs or ramfs, not a persistent drive")
	}
	lines := bytes.Split(bytes.TrimSpace(swaps), []byte{'\n'})
	if len(lines) != 1 || !bytes.HasPrefix(lines[0], []byte("Filename")) {
		return errors.New("volatile-key operation requires swap disabled; key and plaintext memory must not be paged to disk")
	}
	return nil
}
