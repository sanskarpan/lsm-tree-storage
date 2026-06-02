//go:build !linux

package sstable

import "os"

func fadviseDontNeed(_ *os.File, _ int64) error { return nil }
