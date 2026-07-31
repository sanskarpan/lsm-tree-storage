package manifest

import (
	"os"
	"testing"
)

// TestManifestCRCDetectsCorruption verifies that a corrupted manifest record
// is caught during Recover.
func TestManifestCRCDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/MANIFEST"

	m, err := OpenManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	edit := VersionEdit{
		Type:     EditAddSSTable,
		Level:    0,
		FileID:   1,
		FileSize: 1024,
		FirstKey: []byte("a"),
		LastKey:  []byte("z"),
	}
	if err := m.Apply(edit); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	// Read the file and corrupt a byte in the middle (past the magic header).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 10 {
		t.Skip("manifest file too small to corrupt meaningfully")
	}
	// Flip bits at the midpoint — this falls inside a record body or header.
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Recovery should detect the corruption and return a non-nil error.
	_, _, err = Recover(path)
	if err == nil {
		t.Error("expected error from Recover on corrupted manifest, got nil")
	}
}

// TestManifestCRCPassesOnCleanFile verifies that a freshly-written manifest
// can be recovered without error (sanity check for the CRC path).
func TestManifestCRCPassesOnCleanFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/MANIFEST"

	m, err := OpenManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if err := m.Apply(VersionEdit{
			Type:     EditAddSSTable,
			Level:    0,
			FileID:   uint64(i),
			FileSize: 4096,
			FirstKey: []byte{byte(i * 10)},
			LastKey:  []byte{byte(i*10 + 9)},
		}); err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	version, _, err := Recover(path)
	if err != nil {
		t.Fatalf("Recover failed on clean file: %v", err)
	}
	if len(version.Levels[0]) != 5 {
		t.Errorf("expected 5 entries at L0, got %d", len(version.Levels[0]))
	}
}

// TestManifestWriteBeforeDisk verifies that a failed disk write does not leave
// ghost in-memory entries (write-before-disk atomicity).
// After closing the manifest's underlying file, Apply should fail and the
// in-memory Current() must NOT reflect the failed edit.
func TestManifestWriteBeforeDisk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/MANIFEST"

	m, err := OpenManifest(path)
	if err != nil {
		t.Fatal(err)
	}

	// Apply a valid edit — this succeeds and is visible in Current().
	if err := m.Apply(VersionEdit{
		Type:     EditAddSSTable,
		Level:    0,
		FileID:   1,
		FileSize: 1024,
		FirstKey: []byte("a"),
		LastKey:  []byte("b"),
	}); err != nil {
		t.Fatal(err)
	}

	// Close the manifest file to make subsequent disk writes fail.
	_ = m.Close()

	// Try to apply another edit — writeEdit will fail when Flush hits the closed file.
	_ = m.Apply(VersionEdit{
		Type:     EditAddSSTable,
		Level:    0,
		FileID:   2,
		FileSize: 1024,
		FirstKey: []byte("c"),
		LastKey:  []byte("d"),
	})

	// The in-memory state must NOT contain FileID=2 (disk write failed first).
	current := m.Current()
	for _, sst := range current.Levels[0] {
		if sst.FileID == 2 {
			t.Error("ghost entry FileID=2 found in Current() after failed Apply")
		}
	}
	// FileID=1 must still be present.
	found := false
	for _, sst := range current.Levels[0] {
		if sst.FileID == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("FileID=1 missing from Current() after successful Apply")
	}
}
