//go:build linux

package sstable

import (
	"os"

	"golang.org/x/sys/unix"
)

func fadviseDontNeed(f *os.File, size int64) error {
	if size <= 0 || f == nil {
		return nil
	}
	return unix.Fadvise(int(f.Fd()), 0, size, unix.POSIX_FADV_DONTNEED)
}
