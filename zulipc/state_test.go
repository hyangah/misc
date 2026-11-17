// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultStateFile(t *testing.T) {
	t.Run("env_override", func(t *testing.T) {
		t.Setenv("ZULIP_STATE", "/custom/state.json")
		got, err := DefaultStateFile()
		if err != nil {
			t.Fatalf("DefaultStateFile: %v", err)
		}
		if got != "/custom/state.json" {
			t.Errorf("got %q, want /custom/state.json", got)
		}
	})

	t.Run("xdg_state_home", func(t *testing.T) {
		t.Setenv("ZULIP_STATE", "")
		t.Setenv("XDG_STATE_HOME", "/custom/xdg")
		got, err := DefaultStateFile()
		if err != nil {
			t.Fatalf("DefaultStateFile: %v", err)
		}
		want := filepath.Join("/custom/xdg", "zulipc", "state.json")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("default_cache_dir", func(t *testing.T) {
		t.Setenv("ZULIP_STATE", "")
		t.Setenv("XDG_STATE_HOME", "")
		got, err := DefaultStateFile()
		if err != nil {
			t.Fatalf("DefaultStateFile: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("got non-abs path: %q", got)
		}
		if filepath.Base(got) != "state.json" {
			t.Errorf("base = %q, want state.json", filepath.Base(got))
		}
	})
}

func TestStateLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "state.json")

	// 1. Loading non-existent file returns empty state
	st, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState non-existent: %v", err)
	}
	if st == nil || len(st.Channels) != 0 {
		t.Errorf("unexpected empty state: %+v", st)
	}

	// 2. Update and save
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	st.Site = "https://zulip.example.com"
	st.UpdateFromMessages([]Message{
		{
			ID:               100,
			Timestamp:        now.Unix(),
			DisplayRecipient: "general",
			Topic:            "topic1",
		},
		{
			ID:               105,
			Timestamp:        now.Add(5 * time.Minute).Unix(),
			DisplayRecipient: "general",
			Topic:            "topic1",
		},
	}, now.Add(10*time.Minute))

	if err := st.Save(path); err != nil {
		t.Fatalf("st.Save: %v", err)
	}

	// 3. Reload and verify
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState after save: %v", err)
	}
	if loaded.Site != "https://zulip.example.com" {
		t.Errorf("loaded.Site = %q, want https://zulip.example.com", loaded.Site)
	}
	if loaded.LastMessageID != 105 {
		t.Errorf("loaded.LastMessageID = %d, want 105", loaded.LastMessageID)
	}
	wantCheck := now.Add(10 * time.Minute)
	if !loaded.LastCheckTime.Equal(wantCheck) {
		t.Errorf("loaded.LastCheckTime = %v, want %v", loaded.LastCheckTime, wantCheck)
	}
	ch := loaded.Channels["general"]
	if ch == nil || ch.LastMessageID != 105 || ch.Topics["topic1"] != 105 {
		t.Errorf("unexpected channel state: %+v", ch)
	}
}

func TestStateErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")

	if err := os.WriteFile(path, []byte("invalid json data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("LoadState expected invalid json error, got nil")
	}

	// Test saving to unwritable path
	unwritablePath := "/dev/null/unwritable/state.json"
	st := &State{}
	if err := st.Save(unwritablePath); err == nil {
		t.Fatal("st.Save expected error for unwritable path, got nil")
	}
}

func TestStateDefaultPathSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default_state.json")
	t.Setenv("ZULIP_STATE", path)

	st := &State{Site: "https://example.com"}
	if err := st.Save(""); err != nil {
		t.Fatalf("Save(\"\"): %v", err)
	}

	loaded, err := LoadState("")
	if err != nil {
		t.Fatalf("LoadState(\"\"): %v", err)
	}
	if loaded.Site != "https://example.com" {
		t.Errorf("loaded.Site = %q, want https://example.com", loaded.Site)
	}
}
