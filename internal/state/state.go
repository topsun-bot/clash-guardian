package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type State struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Outage              bool      `json:"outage"`
	CurrentNode         string    `json:"current_node,omitempty"`
	LastCheck           time.Time `json:"last_check,omitempty"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
	LastSwitch          time.Time `json:"last_switch,omitempty"`
	NextRetry           time.Time `json:"next_retry,omitempty"`
	LastMessage         string    `json:"last_message,omitempty"`
}

type Store struct {
	Path string
}

func (s Store) Load() (State, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var value State
	if err := json.Unmarshal(data, &value); err != nil {
		return State{}, err
	}
	return value, nil
}

func (s Store) Save(value State) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return err
	}
	return os.Chmod(s.Path, 0o600)
}
