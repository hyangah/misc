// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultStateFile returns the default path for the zulipc state file:
// 1. ZULIP_STATE environment variable if set.
// 2. $XDG_STATE_HOME/zulipc/state.json if set.
// 3. os.UserCacheDir()/zulipc/state.json or ~/.cache/zulipc/state.json.
func DefaultStateFile() (string, error) {
	if env := os.Getenv("ZULIP_STATE"); env != "" {
		return env, nil
	}
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "zulipc", "state.json"), nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		home, hErr := os.UserHomeDir()
		if hErr != nil {
			return "", errors.New("cannot determine state file directory: unable to resolve cache or home dir")
		}
		return filepath.Join(home, ".cache", "zulipc", "state.json"), nil
	}
	return filepath.Join(cacheDir, "zulipc", "state.json"), nil
}

// ChannelState tracks read state for a specific channel.
type ChannelState struct {
	LastMessageID int64            `json:"last_message_id,omitempty"`
	LastCheckTime time.Time        `json:"last_check_time,omitempty"`
	Topics        map[string]int64 `json:"topics,omitempty"` // topic name -> max message ID
}

// State tracks the local reading cursor for daily summaries and digests.
type State struct {
	Site          string                   `json:"site,omitempty"`
	LastCheckTime time.Time                `json:"last_check_time"`
	LastMessageID int64                    `json:"last_message_id,omitempty"`
	Channels      map[string]*ChannelState `json:"channels,omitempty"`
}

// LoadState reads the state file from path. If the file does not exist,
// it returns an initialized empty State without error.
func LoadState(path string) (*State, error) {
	if path == "" {
		var err error
		path, err = DefaultStateFile()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Channels: make(map[string]*ChannelState),
			}, nil
		}
		return nil, fmt.Errorf("reading state file %s: %w", path, err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}
	if state.Channels == nil {
		state.Channels = make(map[string]*ChannelState)
	}
	return &state, nil
}

// Save writes the state to path atomically.
func (s *State) Save(path string) error {
	if path == "" {
		var err error
		path, err = DefaultStateFile()
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating state directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("saving state file to %s: %w", path, err)
	}
	return nil
}

// UpdateFromMessages updates the state with the latest message timestamp and ID from msgs.
func (s *State) UpdateFromMessages(msgs []Message, checkTime time.Time) {
	if !checkTime.IsZero() {
		if s.LastCheckTime.IsZero() || checkTime.After(s.LastCheckTime) {
			s.LastCheckTime = checkTime
		}
	}
	for _, m := range msgs {
		if m.ID > s.LastMessageID {
			s.LastMessageID = m.ID
		}
		msgTime := time.Unix(m.Timestamp, 0)
		if s.LastCheckTime.IsZero() || msgTime.After(s.LastCheckTime) {
			s.LastCheckTime = msgTime
		}

		chName := m.ChannelName()
		if chName != "" {
			if s.Channels == nil {
				s.Channels = make(map[string]*ChannelState)
			}
			chState := s.Channels[chName]
			if chState == nil {
				chState = &ChannelState{
					Topics: make(map[string]int64),
				}
				s.Channels[chName] = chState
			}
			if m.ID > chState.LastMessageID {
				chState.LastMessageID = m.ID
			}
			if chState.LastCheckTime.IsZero() || msgTime.After(chState.LastCheckTime) {
				chState.LastCheckTime = msgTime
			}

			tName := m.TopicName()
			if tName != "" {
				if chState.Topics == nil {
					chState.Topics = make(map[string]int64)
				}
				if m.ID > chState.Topics[tName] {
					chState.Topics[tName] = m.ID
				}
			}
		}
	}
}
