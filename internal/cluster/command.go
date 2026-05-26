package cluster

import (
	"encoding/json"
	"fmt"
)

// CommandType identifies one replicated logical operation.
type CommandType string

const (
	CommandPut        CommandType = "put"
	CommandDelete     CommandType = "delete"
	CommandWriteBatch CommandType = "write_batch"
)

// BatchEntry is one logical mutation in a replicated write batch.
type BatchEntry struct {
	Key    []byte `json:"key"`
	Value  []byte `json:"value,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

// Command is the wire-level logical mutation that will eventually be replicated
// through the consensus log.
//
// Phase 0 note:
// This is intentionally logical rather than file-oriented. We replicate writes,
// not SSTables or WAL fragments.
type Command struct {
	Type  CommandType `json:"type"`
	Key   []byte      `json:"key,omitempty"`
	Value []byte      `json:"value,omitempty"`

	Entries []BatchEntry `json:"entries,omitempty"`
}

func (c Command) Validate() error {
	switch c.Type {
	case CommandPut:
		if len(c.Key) == 0 {
			return fmt.Errorf("cluster command: put key required")
		}
	case CommandDelete:
		if len(c.Key) == 0 {
			return fmt.Errorf("cluster command: delete key required")
		}
	case CommandWriteBatch:
		if len(c.Entries) == 0 {
			return fmt.Errorf("cluster command: batch entries required")
		}
		for i, entry := range c.Entries {
			if len(entry.Key) == 0 {
				return fmt.Errorf("cluster command: entries[%d].key required", i)
			}
		}
	default:
		return fmt.Errorf("cluster command: unsupported type %q", c.Type)
	}
	return nil
}

func (c Command) Marshal() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func UnmarshalCommand(data []byte) (Command, error) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return Command{}, err
	}
	if err := cmd.Validate(); err != nil {
		return Command{}, err
	}
	return cmd, nil
}
