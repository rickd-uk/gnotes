package storage

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestVolatileKeyPolicy(t *testing.T) {
	for _, test := range []struct {
		name       string
		filesystem int64
		swaps      string
		valid      bool
	}{
		{"tmpfs without swap", unix.TMPFS_MAGIC, "Filename Type Size Used Priority\n", true},
		{"ramfs without swap", unix.RAMFS_MAGIC, "Filename Type Size Used Priority\n", true},
		{"persistent disk", unix.EXT4_SUPER_MAGIC, "Filename Type Size Used Priority\n", false},
		{"swappable memory", unix.TMPFS_MAGIC, "Filename Type Size Used Priority\n/swap.img file 1024 0 -1\n", false},
		{"unavailable swap status", unix.TMPFS_MAGIC, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateVolatilePolicy(test.filesystem, []byte(test.swaps)); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
