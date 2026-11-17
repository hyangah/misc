// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCatchup(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	msgs := []Message{
		{
			ID:               101,
			SenderFullName:   "Morgan Price",
			Timestamp:        now.Add(-2 * time.Hour).Unix(),
			DisplayRecipient: "general",
			Topic:            "watchflakes",
			Content:          "fixing flakes",
		},
		{
			ID:               102,
			SenderFullName:   "Alex Chen",
			Timestamp:        now.Add(-1 * time.Hour).Unix(),
			DisplayRecipient: "general",
			Topic:            "watchflakes",
			Content:          "looks good",
		},
		{
			ID:               103,
			SenderEmail:      "rob@example.com",
			Timestamp:        now.Add(-30 * time.Minute).Unix(),
			DisplayRecipient: "compiler",
			Topic:            "escape analysis",
			Content:          "new benchmark",
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/streams":
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"streams": []Stream{
					{StreamID: 1, Name: "general"},
					{StreamID: 2, Name: "compiler"},
				},
			})
		case "/api/v1/messages":
			json.NewEncoder(w).Encode(MessagesResponse{
				Result:      "success",
				Messages:    msgs,
				FoundOldest: true,
				FoundNewest: true,
			})
		}
	}))
	defer ts.Close()

	cfg := &Config{Email: "a@b.com", Key: "k", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	t.Run("basic_markdown_catchup", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			Since:     "2026-08-30 09:00:00 UTC",
			Until:     "2026-08-30 13:00:00 UTC",
			Limit:     100,
		}
		if err := runCatchup(context.Background(), client, cfg, opts, &buf); err != nil {
			t.Fatalf("runCatchup: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "# Zulip Catchup") {
			t.Errorf("missing catchup title:\n%s", out)
		}
		if !strings.Contains(out, "Total messages: 3 across 2 active topic(s)") {
			t.Errorf("missing summary count:\n%s", out)
		}
		if !strings.Contains(out, "## Active Topics:") {
			t.Errorf("missing active topics header:\n%s", out)
		}
		if !strings.Contains(out, "Morgan Price, Alex Chen") {
			t.Errorf("missing sender summary:\n%s", out)
		}
		if !strings.Contains(out, "## [general > watchflakes]") || !strings.Contains(out, "## [compiler > escape analysis]") {
			t.Errorf("missing topic sections:\n%s", out)
		}

		// Verify state file was created and updated
		st, err := LoadState(stateFile)
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if st.LastMessageID != 103 {
			t.Errorf("st.LastMessageID = %d, want 103", st.LastMessageID)
		}
	})

	t.Run("json_catchup", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			Since:     "2026-08-30 09:00:00 UTC",
			Until:     "2026-08-30 13:00:00 UTC",
			AsJSON:    true,
			Limit:     100,
		}
		if err := runCatchup(context.Background(), client, cfg, opts, &buf); err != nil {
			t.Fatalf("runCatchup -json: %v", err)
		}

		var res CatchupResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal CatchupResult: %v\nstdout:\n%s", err, buf.String())
		}
		if res.Shown != 3 || res.TotalTopics != 2 || len(res.Topics) != 2 {
			t.Errorf("unexpected CatchupResult: %+v", res)
		}
		if res.Topics[0].Topic != "watchflakes" || res.Topics[0].MessageCount != 2 {
			t.Errorf("unexpected topic summary: %+v", res.Topics[0])
		}
	})

	t.Run("mark_only", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			MarkOnly:  true,
		}
		if err := runCatchup(context.Background(), client, cfg, opts, &buf); err != nil {
			t.Fatalf("runCatchup -mark: %v", err)
		}

		if !strings.Contains(buf.String(), "Marked Zulip catchup cursor up to") {
			t.Errorf("unexpected mark output: %s", buf.String())
		}

		st, err := LoadState(stateFile)
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if st.LastCheckTime.IsZero() {
			t.Error("expected non-zero LastCheckTime")
		}
	})

	t.Run("status_never_checked", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "empty_state.json")

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			Status:    true,
		}
		if err := runCatchup(context.Background(), nil, nil, opts, &buf); err != nil {
			t.Fatalf("runCatchup -status: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Zulip Catchup Status:") || !strings.Contains(out, "Last check:       never") {
			t.Errorf("unexpected status output:\n%s", out)
		}
	})

	t.Run("status_with_check_time_and_json", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")
		st := &State{
			Site:          "https://example.com",
			LastCheckTime: time.Now().Add(-2 * time.Hour),
			LastMessageID: 999,
			Channels: map[string]*ChannelState{
				"general": {LastMessageID: 999},
			},
		}
		if err := st.Save(stateFile); err != nil {
			t.Fatalf("Save: %v", err)
		}

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			Status:    true,
			AsJSON:    true,
		}
		if err := runCatchup(context.Background(), nil, nil, opts, &buf); err != nil {
			t.Fatalf("runCatchup -status -json: %v", err)
		}
		var res CatchupStatusResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal CatchupStatusResult: %v\nstdout:\n%s", err, buf.String())
		}
		if res.LastMessageID != 999 || res.TrackedChannels != 1 || res.Site != "https://example.com" {
			t.Errorf("unexpected status json result: %+v", res)
		}
	})

	t.Run("reset_state", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")
		st := &State{LastCheckTime: time.Now()}
		if err := st.Save(stateFile); err != nil {
			t.Fatalf("Save: %v", err)
		}

		var buf bytes.Buffer
		opts := CatchupOptions{
			StateFile: stateFile,
			Reset:     true,
		}
		if err := runCatchup(context.Background(), nil, nil, opts, &buf); err != nil {
			t.Fatalf("runCatchup -reset: %v", err)
		}
		if !strings.Contains(buf.String(), "Reset Zulip catchup state") {
			t.Errorf("unexpected reset output:\n%s", buf.String())
		}
		if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
			t.Error("expected state file to be removed after -reset")
		}
	})

	t.Run("channel_scoped_catchup", func(t *testing.T) {
		stateDir := t.TempDir()
		stateFile := filepath.Join(stateDir, "state.json")

		var buf bytes.Buffer
		opts := CatchupOptions{
			Channel:   "general",
			StateFile: stateFile,
			Since:     "2026-08-30 09:00:00 UTC",
			Until:     "2026-08-30 13:00:00 UTC",
			NoUpdate:  true,
			Limit:     100,
		}
		if err := runCatchup(context.Background(), client, cfg, opts, &buf); err != nil {
			t.Fatalf("runCatchup: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "# Zulip Catchup for [general]") {
			t.Errorf("missing channel catchup header:\n%s", out)
		}

		// Verify state file was NOT updated due to NoUpdate
		if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
			t.Error("expected state file not to exist with -no-update")
		}
	})

	t.Run("empty_messages_catchup", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(MessagesResponse{
				Result:      "success",
				Messages:    []Message{},
				FoundOldest: true,
			})
		}))
		defer emptyServer.Close()

		emptyClient, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		var buf bytes.Buffer
		opts := CatchupOptions{
			Since:    "2026-08-30 09:00:00 UTC",
			Until:    "2026-08-30 13:00:00 UTC",
			NoUpdate: true,
			Limit:    10,
		}
		if err := runCatchup(context.Background(), emptyClient, cfg, opts, &buf); err != nil {
			t.Fatalf("runCatchup: %v", err)
		}

		if !strings.Contains(buf.String(), "(no new messages found during this period)") {
			t.Errorf("expected no messages notice, got:\n%s", buf.String())
		}
	})

	t.Run("runDigest_alias", func(t *testing.T) {
		var buf bytes.Buffer
		opts := DigestOptions{
			Since:    "2026-08-30 09:00:00 UTC",
			Until:    "2026-08-30 13:00:00 UTC",
			NoUpdate: true,
			Limit:    10,
		}
		if err := runDigest(context.Background(), client, cfg, opts, &buf); err != nil {
			t.Fatalf("runDigest alias: %v", err)
		}
	})
}

func TestCatchupValidationErrors(t *testing.T) {
	t.Parallel()

	cfg := &Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name    string
		opts    CatchupOptions
		wantErr string
	}{
		{
			name:    "status_and_reset_conflict",
			opts:    CatchupOptions{Status: true, Reset: true},
			wantErr: "cannot specify both -status and -reset",
		},
		{
			name:    "limit_negative",
			opts:    CatchupOptions{Limit: -1},
			wantErr: "-n limit must be greater than 0",
		},
		{
			name:    "invalid_since",
			opts:    CatchupOptions{Since: "invalid", Limit: 10},
			wantErr: "parsing -since",
		},
		{
			name:    "invalid_until",
			opts:    CatchupOptions{Until: "invalid", Limit: 10},
			wantErr: "parsing -until",
		},
		{
			name:    "until_before_since",
			opts:    CatchupOptions{Since: "2h", Until: "4h", Limit: 10},
			wantErr: "-until time must be after -since time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := runCatchup(context.Background(), client, cfg, tt.opts, &buf)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestCatchupCLIExecution(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/streams":
			json.NewEncoder(w).Encode(map[string]any{
				"result":  "success",
				"streams": []Stream{{StreamID: 1, Name: "general"}},
			})
		case "/api/v1/messages":
			json.NewEncoder(w).Encode(MessagesResponse{
				Result: "success",
				Messages: []Message{
					{
						ID:               500,
						Timestamp:        time.Now().Add(-10 * time.Minute).Unix(),
						DisplayRecipient: "general",
						Topic:            "general-topic",
						Content:          "catchup content",
					},
				},
				FoundOldest: true,
			})
		}
	}))
	defer ts.Close()

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, ".zuliprc")
	if err := os.WriteFile(cfgPath, []byte("[api]\nemail = a@b.com\nkey = k\nsite = "+ts.URL+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stateFile := filepath.Join(cfgDir, "state.json")

	t.Run("cli_catchup_markdown", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "catchup", "-state", stateFile, "-no-update")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "# Zulip Catchup") || !strings.Contains(stdout, "catchup content") {
			t.Errorf("unexpected stdout:\n%s", stdout)
		}
	})

	t.Run("cli_digest_alias_markdown", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "digest", "-state", stateFile, "-no-update")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "# Zulip Catchup") || !strings.Contains(stdout, "catchup content") {
			t.Errorf("unexpected stdout:\n%s", stdout)
		}
	})

	t.Run("cli_catchup_mark", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "catchup", "-state", stateFile, "-mark")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "Marked Zulip catchup cursor up to") {
			t.Errorf("unexpected stdout:\n%s", stdout)
		}
	})

	t.Run("cli_catchup_status", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, nil, "catchup", "-state", stateFile, "-status")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "Zulip Catchup Status:") || !strings.Contains(stdout, "Last check:") {
			t.Errorf("unexpected stdout:\n%s", stdout)
		}
	})

	t.Run("cli_catchup_reset", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, nil, "catchup", "-state", stateFile, "-reset")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "Reset Zulip catchup state") {
			t.Errorf("unexpected stdout:\n%s", stdout)
		}
	})

	t.Run("cli_catchup_json", func(t *testing.T) {
		jsonStateFile := filepath.Join(cfgDir, "state_json.json")
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "catchup", "-state", jsonStateFile, "-json")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr:\n%s", code, stderr)
		}
		var res CatchupResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("unmarshal error: %v\nstdout:\n%s", err, stdout)
		}
		if res.Shown != 1 {
			t.Errorf("res.Shown = %d, want 1", res.Shown)
		}
	})
}

func TestCmdCatchupDirect(t *testing.T) {
	if err := cmdCatchup([]string{"-invalid"}); err == nil {
		t.Error("cmdCatchup with invalid flag expected error, got nil")
	}

	if err := cmdCatchup([]string{"extra"}); err == nil {
		t.Error("cmdCatchup with extra arg expected error, got nil")
	}

	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdCatchup([]string{}); err == nil {
		t.Error("cmdCatchup with nonexistent config expected error, got nil")
	}
}

func TestCmdDigestDirect(t *testing.T) {
	if err := cmdDigest([]string{"-invalid"}); err == nil {
		t.Error("cmdDigest with invalid flag expected error, got nil")
	}
}
