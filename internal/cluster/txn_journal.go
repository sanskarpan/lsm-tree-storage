package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type txPreImage struct {
	Shard   int    `json:"shard"`
	Key     string `json:"key"`
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
}

type txJournalEntry struct {
	ID         string       `json:"id"`
	CreatedAt  time.Time    `json:"created_at"`
	Committed  bool         `json:"committed"`
	PreImages  []txPreImage `json:"pre_images"`
	EntryCount int          `json:"entry_count"`
}

type txJournalStore struct {
	mu  sync.Mutex
	dir string
}

func newTxJournalStore(dir string) *txJournalStore {
	return &txJournalStore{dir: dir}
}

func (s *txJournalStore) path(id string) string {
	return filepath.Join(s.dir, fmt.Sprintf("pending-%s.json", id))
}

func (s *txJournalStore) committedPath(id string) string {
	return filepath.Join(s.dir, fmt.Sprintf("committed-%s.json", id))
}

func (s *txJournalStore) Save(entry txJournalEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	entry.CreatedAt = entry.CreatedAt.UTC()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	dst := s.path(entry.ID)
	if entry.Committed {
		dst = s.committedPath(entry.ID)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func (s *txJournalStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(s.committedPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *txJournalStore) LoadAll() ([]txJournalEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]txJournalEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if strings.HasPrefix(entry.Name(), "committed-") {
			pendingName := strings.TrimPrefix(entry.Name(), "committed-")
			if err := os.Remove(filepath.Join(s.dir, "pending-"+pendingName)); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record txJournalEntry
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		if record.ID == "" {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}
