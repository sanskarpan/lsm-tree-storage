package cluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type peerRegistryStore struct {
	mu   sync.Mutex
	path string
}

func newPeerRegistryStore(path string) *peerRegistryStore {
	return &peerRegistryStore{path: path}
}

func (s *peerRegistryStore) Load() (map[string]Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Peer{}, nil
		}
		return nil, err
	}
	var peers map[string]Peer
	if err := json.Unmarshal(data, &peers); err != nil {
		return nil, err
	}
	if peers == nil {
		peers = map[string]Peer{}
	}
	return peers, nil
}

func (s *peerRegistryStore) Save(peers map[string]Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func peersToSortedSlice(peers map[string]Peer) []Peer {
	out := make([]Peer, 0, len(peers))
	for _, peer := range peers {
		out = append(out, peer)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NodeID < out[j].NodeID
	})
	return out
}
