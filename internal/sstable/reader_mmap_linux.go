//go:build linux

package sstable

import (
	"os"

	"golang.org/x/sys/unix"
)

// POSIX_FADV_DONTNEED is not exported by golang.org/x/sys/unix on Linux;
// it is 4 per <linux/fadvise.h>.
const posixFadvDontNeed = 4

func fadviseDontNeed(f *os.File, size int64) error {
	if size <= 0 || f == nil {
		return nil
	}
	return unix.Fadvise(int(f.Fd()), 0, size, posixFadvDontNeed)
}
