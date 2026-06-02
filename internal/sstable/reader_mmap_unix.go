//go:build !windows

package sstable

import (
	"os"

	"golang.org/x/sys/unix"
)

func mmapFile(f *os.File) ([]byte, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size <= 0 {
		return nil, nil
	}
	data, err := unix.Mmap(int(f.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func munmapFile(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Munmap(data)
}

func madviseSequential(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Madvise(data, unix.MADV_SEQUENTIAL)
}
