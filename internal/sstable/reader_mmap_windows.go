//go:build windows

package sstable

import "os"

func mmapFile(_ *os.File) ([]byte, error)        { return nil, nil }
func munmapFile(_ []byte) error                  { return nil }
func madviseSequential(_ []byte) error           { return nil }
func fadviseDontNeed(_ *os.File, _ int64) error  { return nil }
