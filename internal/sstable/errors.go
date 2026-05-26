// Package sstable — see format.go for the package doc.
package sstable

import "errors"

var (
	// ErrCorruptSSTable is returned when an SSTable file contains invalid or unreadable data.
	ErrCorruptSSTable = errors.New("sstable: corrupt data")
	// ErrNotFound is returned when a requested key does not exist in an SSTable.
	ErrNotFound = errors.New("sstable: key not found")
)
