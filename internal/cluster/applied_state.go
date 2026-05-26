package cluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type appliedState struct {
	LastAppliedIndex uint64    `json:"last_applied_index"`
	LastAppliedTerm  uint64    `json:"last_applied_term,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type appliedStateStore struct {
	mu   sync.Mutex
	path string
}

func newAppliedStateStore(path string) *appliedStateStore {
	return &appliedStateStore{path: path}
}

func (s *appliedStateStore) Load() (appliedState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return appliedState{}, nil
		}
		return appliedState{}, err
	}

	var state appliedState
	if err := json.Unmarshal(data, &state); err != nil {
		return appliedState{}, err
	}
	return state, nil
}

func (s *appliedStateStore) Save(state appliedState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
