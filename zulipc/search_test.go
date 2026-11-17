// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/synctest"
)

func sampleSearchMessages() []Message {
	return []Message{
		{
			ID:               619610069,
			SenderID:         101,
			SenderFullName:   "Morgan Price",
			SenderEmail:      "morgan@example.com",
			Timestamp:        testTime1, // 2026-08-28 14:15:00 UTC
			Content:          "I have gone through the list of stale watchflakes and closed the resolved ones.",
			Type:             "stream",
			StreamID:         615773,
			DisplayRecipient: "general",
			Subject:          "closing stale watchflakes",
			Topic:            "closing stale watchflakes",
			Flags:            []string{"read"},
		},
		{
			ID:               619610075,
			SenderID:         102,
			SenderFullName:   "Alex Chen",
			SenderEmail:      "alex@example.com",
			Timestamp:        testTime2, // 2026-08-28 15:02:00 UTC
			Content:          "Thanks! Let's monitor the builder queue over the weekend.",
			Type:             "stream",
			StreamID:         615773,
			DisplayRecipient: "general",
			Subject:          "closing stale watchflakes",
			Topic:            "closing stale watchflakes",
			Flags:            []string{"read"},
		},
	}
}

func newSearchTestServer(t *testing.T, streams []Stream, topics []Topic, messages []Message) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/streams":
			json.NewEncoder(w).Encode(map[string]any{
				"result":  "success",
				"streams": streams,
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/users/me/") && strings.HasSuffix(r.URL.Path, "/topics"):
			if strings.Contains(r.URL.Path, "/999999/") {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"result": "error",
					"msg":    "Invalid channel ID",
					"code":   "BAD_REQUEST",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"topics": topics,
			})
		case r.URL.Path == "/api/v1/messages":
			q := r.URL.Query()
			if q.Get("anchor") == "invalid" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"result": "error",
					"msg":    "Invalid anchor value",
					"code":   "BAD_REQUEST",
				})
				return
			}
			json.NewEncoder(w).Encode(MessagesResponse{
				Result:      "success",
				FoundAnchor: true,
				FoundOldest: true,
				FoundNewest: true,
				Messages:    messages,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSearchGlobal(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var buf bytes.Buffer
	opts := SearchOptions{
		Query: "watchflakes",
		Limit: 20,
	}
	err = runSearch(ctx, client, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("runSearch error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `# Search: "watchflakes"`) {
		t.Errorf("out missing header, got:\n%s", out)
	}
	if !strings.Contains(out, "## [general > closing stale watchflakes]") {
		t.Errorf("out missing channel > topic group header, got:\n%s", out)
	}
	if !strings.Contains(out, "URL: "+ts.URL+"/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes") {
		t.Errorf("out missing topic URL, got:\n%s", out)
	}
	if !strings.Contains(out, "**Morgan Price**") {
		t.Errorf("out missing Morgan Price entry, got:\n%s", out)
	}
	if !strings.Contains(out, "**Alex Chen**") {
		t.Errorf("out missing Alex Chen entry, got:\n%s", out)
	}
	if !strings.Contains(out, "[ID: 619610069]") {
		t.Errorf("out missing message ID, got:\n%s", out)
	}
	if !strings.Contains(out, "closed the resolved ones.") {
		t.Errorf("out missing message content, got:\n%s", out)
	}
}

func TestSearchMultiChannelGrouping(t *testing.T) {
	t.Parallel()

	streams := []Stream{
		{StreamID: 101, Name: "security"},
		{StreamID: 102, Name: "tools"},
	}
	topics := []Topic{
		{Name: "go-vuln-db", MaxID: 1002},
		{Name: "gopls", MaxID: 2001},
	}
	multiMessages := []Message{
		{
			ID:               1001,
			SenderFullName:   "Alice Smith",
			Timestamp:        testTime1,
			Content:          "Discussing the latest vuln report.",
			Type:             "stream",
			StreamID:         101,
			DisplayRecipient: "security",
			Topic:            "go-vuln-db",
		},
		{
			ID:               2001,
			SenderFullName:   "Charlie",
			Timestamp:        testTime2,
			Content:          "Checking vulncheck integration.",
			Type:             "stream",
			StreamID:         102,
			DisplayRecipient: "tools",
			Topic:            "gopls",
		},
		{
			ID:               1002,
			SenderFullName:   "Bob Jones",
			Timestamp:        testTime2 + 10,
			Content:          "Fixed in the latest release.",
			Type:             "stream",
			StreamID:         101,
			DisplayRecipient: "security",
			Topic:            "go-vuln-db",
		},
	}

	ts := newSearchTestServer(t, streams, topics, multiMessages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var buf bytes.Buffer
	opts := SearchOptions{
		Query: "vuln",
		Limit: 20,
	}
	err = runSearch(ctx, client, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("runSearch error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `# Search: "vuln"`) {
		t.Errorf("missing top title:\n%s", out)
	}
	if !strings.Contains(out, "## [security > go-vuln-db]") {
		t.Errorf("missing security section header:\n%s", out)
	}
	if !strings.Contains(out, "## [tools > gopls]") {
		t.Errorf("missing tools section header:\n%s", out)
	}
	if !strings.Contains(out, "**Alice Smith**") || !strings.Contains(out, "**Bob Jones**") {
		t.Errorf("missing security messages:\n%s", out)
	}
	if !strings.Contains(out, "**Charlie**") {
		t.Errorf("missing tools message:\n%s", out)
	}
}

func TestSearchScopedChannelAndTopic(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("scoped channel only", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: "general",
			Query:   "watchflakes",
			Limit:   20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search: "watchflakes" in [general]`) {
			t.Errorf("out missing channel header, got:\n%s", out)
		}
		if !strings.Contains(out, "## [general > closing stale watchflakes]") {
			t.Errorf("out missing topic group header, got:\n%s", out)
		}
	})

	t.Run("scoped channel and topic single topic", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: "general",
			Topic:   "watchflakes", // fuzzy resolved
			Query:   "stale",
			Limit:   20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search: "stale" in [general > closing stale watchflakes]`) {
			t.Errorf("out missing channel+topic header, got:\n%s", out)
		}
		if !strings.Contains(out, "URL: "+ts.URL+"/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes") {
			t.Errorf("out missing single topic URL, got:\n%s", out)
		}
	})

	t.Run("scoped channel URL containing topic", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes",
			Query:   "stale",
			Limit:   20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search: "stale" in [general > closing stale watchflakes]`) {
			t.Errorf("out missing extracted channel+topic header, got:\n%s", out)
		}
	})

	t.Run("scoped topic only without channel", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Topic: "closing stale watchflakes",
			Query: "stale",
			Limit: 20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search: "stale" in [topic: closing stale watchflakes]`) {
			t.Errorf("out missing topic only header, got:\n%s", out)
		}
	})

	t.Run("scoped sender only", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Sender: "morgan@example.com",
			Limit:  20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search from morgan@example.com`) {
			t.Errorf("out missing sender header, got:\n%s", out)
		}
	})

	t.Run("scoped channel topic and sender without query", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Sender:  "morgan@example.com",
			Limit:   20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `# Search in [general > closing stale watchflakes] from morgan@example.com`) {
			t.Errorf("out missing combined header, got:\n%s", out)
		}
	})
}

func TestSearchSince(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("since filter matches second message only", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Query: "watchflakes",
			Since: "2026-08-28T14:30:00Z",
			Limit: 20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		out := buf.String()
		if strings.Contains(out, "**Morgan Price**") {
			t.Errorf("out should not contain older message, got:\n%s", out)
		}
		if !strings.Contains(out, "**Alex Chen**") {
			t.Errorf("out missing newer message, got:\n%s", out)
		}
	})

	t.Run("since relative duration", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Query: "watchflakes",
			Since: "24h",
			Limit: 20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected non-empty output")
		}
	})
}

func TestSearchJSON(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("valid JSON output", func(t *testing.T) {
		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Sender:  "morgan@example.com",
			Query:   "stale",
			AsJSON:  true,
			Limit:   20,
		}
		err := runSearch(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}

		var res SearchResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v\nOutput: %s", err, buf.String())
		}
		if res.Query != "stale" {
			t.Errorf("res.Query = %q, want stale", res.Query)
		}
		if res.Channel != "general" {
			t.Errorf("res.Channel = %q, want general", res.Channel)
		}
		if res.Topic != "closing stale watchflakes" {
			t.Errorf("res.Topic = %q, want closing stale watchflakes", res.Topic)
		}
		if res.Sender != "morgan@example.com" {
			t.Errorf("res.Sender = %q, want morgan@example.com", res.Sender)
		}
		if len(res.Messages) != 2 {
			t.Fatalf("len(res.Messages) = %d, want 2", len(res.Messages))
		}
	})

	t.Run("empty search results JSON", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(MessagesResponse{Result: "success", Messages: nil})
		}))
		defer emptyServer.Close()

		emptyCfg := &Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}
		emptyClient, _ := NewClientWithHTTP(emptyCfg, emptyServer.Client())
		var buf bytes.Buffer
		opts := SearchOptions{
			Query:  "nomatch",
			AsJSON: true,
			Limit:  20,
		}
		err := runSearch(ctx, emptyClient, emptyCfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch error: %v", err)
		}

		var res SearchResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v", err)
		}
		if res.Messages == nil || len(res.Messages) != 0 {
			t.Errorf("res.Messages = %v, want non-nil empty slice", res.Messages)
		}
	})

	t.Run("json writer error", func(t *testing.T) {
		opts := SearchOptions{
			Query:  "stale",
			AsJSON: true,
			Limit:  20,
		}
		err := runSearch(ctx, client, cfg, opts, errWriter{})
		if err == nil || !strings.Contains(err.Error(), "encoding JSON") {
			t.Errorf("expected encoding JSON error, got: %v", err)
		}
	})
}

func TestSearchUTC(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var buf bytes.Buffer
	opts := SearchOptions{
		Query: "watchflakes",
		UTC:   true,
		Limit: 20,
	}
	err = runSearch(ctx, client, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("runSearch error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "(2026-08-28 14:15 UTC)") {
		t.Errorf("out missing UTC timestamp, got:\n%s", out)
	}
}

func TestSearchFormattingEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("sender fallback and channel/topic combinations", func(t *testing.T) {
		msgs := []Message{
			{
				ID:          1,
				Timestamp:   testTime1,
				SenderEmail: "fallback@example.com",
				Content:     "message with email only",
			},
			{
				ID:               2,
				SenderID:         999,
				Timestamp:        testTime1,
				DisplayRecipient: "general",
				Content:          "message with channel only and user ID",
			},
			{
				ID:        3,
				Timestamp: testTime1,
				Topic:     "topic-only",
				Content:   "message with topic only",
			},
		}

		var buf bytes.Buffer
		err := formatSearchMarkdown(&buf, "https://example.com", "query", "", "", "sender@example.com", msgs, true, true, 20)
		if err != nil {
			t.Fatalf("formatSearchMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "**fallback@example.com**") {
			t.Errorf("out missing fallback email sender, got:\n%s", out)
		}
		if !strings.Contains(out, "**User 999**") {
			t.Errorf("out missing User 999, got:\n%s", out)
		}
		if !strings.Contains(out, "## [general]") {
			t.Errorf("out missing channel section header, got:\n%s", out)
		}
		if !strings.Contains(out, "## [topic-only]") {
			t.Errorf("out missing topic section header, got:\n%s", out)
		}
		if !strings.Contains(out, "## [messages]") {
			t.Errorf("out missing default messages section header, got:\n%s", out)
		}
		if !strings.Contains(out, "(Showing 3 messages. More results may exist; use -n 40 to fetch more, or refine with -c, -t, -sender, -since)") {
			t.Errorf("out missing search pagination footer:\n%s", out)
		}
	})

	t.Run("empty messages notice", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatSearchMarkdown(&buf, "https://example.com", "query", "", "", "", nil, true, false, 20)
		if err != nil {
			t.Fatalf("formatSearchMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "(no messages found)") {
			t.Errorf("out missing (no messages found), got:\n%s", out)
		}
	})
}

func TestSearchValidationErrors(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "testkey", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name    string
		opts    SearchOptions
		wantErr string
	}{
		{
			name:    "limit_zero",
			opts:    SearchOptions{Query: "watchflakes", Limit: 0},
			wantErr: "-n limit must be greater than 0",
		},
		{
			name:    "limit_negative",
			opts:    SearchOptions{Query: "watchflakes", Limit: -5},
			wantErr: "-n limit must be greater than 0",
		},
		{
			name:    "missing_query_and_filters",
			opts:    SearchOptions{Limit: 20},
			wantErr: "missing search query: specify a search query or filter (-c, -t, -sender)",
		},
		{
			name:    "nonexistent_channel",
			opts:    SearchOptions{Channel: "nonexistent", Query: "watchflakes", Limit: 20},
			wantErr: "channel \"nonexistent\" not found",
		},
		{
			name:    "nonexistent_topic",
			opts:    SearchOptions{Channel: "general", Topic: "nonexistent", Query: "watchflakes", Limit: 20},
			wantErr: "topic \"nonexistent\" not found",
		},
		{
			name:    "invalid_since_format",
			opts:    SearchOptions{Query: "watchflakes", Since: "invalid-date", Limit: 20},
			wantErr: "invalid time or duration",
		},
		{
			name:    "invalid_until_format",
			opts:    SearchOptions{Query: "watchflakes", Until: "invalid-date", Limit: 20},
			wantErr: "invalid time or duration",
		},
		{
			name:    "until_before_since",
			opts:    SearchOptions{Query: "watchflakes", Since: "2h", Until: "4h", Limit: 20},
			wantErr: "-until time must be after -since time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := runSearch(ctx, client, cfg, tt.opts, &buf)
			if err == nil {
				t.Fatalf("runSearch expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("runSearch error = %q, want error containing %q", err.Error(), tt.wantErr)
			}
		})
	}

	t.Run("api_error_on_messages", func(t *testing.T) {
		errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"result": "error",
				"msg":    "Invalid query syntax",
				"code":   "BAD_REQUEST",
			})
		}))
		defer errServer.Close()

		errCfg := &Config{Email: "a@b.com", Key: "k", Site: errServer.URL}
		errClient, _ := NewClientWithHTTP(errCfg, errServer.Client())
		var buf bytes.Buffer
		err := runSearch(ctx, errClient, errCfg, SearchOptions{Query: "bad", Limit: 20}, &buf)
		if err == nil || !strings.Contains(err.Error(), "searching messages") {
			t.Errorf("expected searching messages error, got: %v", err)
		}
	})

	t.Run("api_error_on_topics", func(t *testing.T) {
		errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1/streams" {
				json.NewEncoder(w).Encode(map[string]any{
					"result":  "success",
					"streams": []Stream{{StreamID: 999999, Name: "bad-stream"}},
				})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"result": "error",
				"msg":    "Stream topics unavailable",
				"code":   "BAD_REQUEST",
			})
		}))
		defer errServer.Close()

		errCfg := &Config{Email: "a@b.com", Key: "k", Site: errServer.URL}
		errClient, _ := NewClientWithHTTP(errCfg, errServer.Client())
		var buf bytes.Buffer
		err := runSearch(ctx, errClient, errCfg, SearchOptions{Channel: "bad-stream", Topic: "any", Limit: 20}, &buf)
		if err == nil || !strings.Contains(err.Error(), "fetching topics") {
			t.Errorf("expected fetching topics error, got: %v", err)
		}
	})
}

func TestSearchSynctestBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		streams := sampleStreams()
		topics := sampleTopics()
		messages := sampleSearchMessages()

		cfg := &Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}
		client, err := NewClientWithHTTP(cfg, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "application/json")
				switch {
				case req.URL.Path == "/api/v1/streams":
					json.NewEncoder(rec).Encode(map[string]any{
						"result":  "success",
						"streams": streams,
					})
				case strings.HasPrefix(req.URL.Path, "/api/v1/users/me/") && strings.HasSuffix(req.URL.Path, "/topics"):
					json.NewEncoder(rec).Encode(map[string]any{
						"result": "success",
						"topics": topics,
					})
				case req.URL.Path == "/api/v1/messages":
					json.NewEncoder(rec).Encode(MessagesResponse{
						Result:   "success",
						Messages: messages,
					})
				default:
					http.NotFound(rec, req)
				}
				return rec.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		var buf bytes.Buffer
		opts := SearchOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Query:   "stale",
			Limit:   20,
		}
		err = runSearch(context.Background(), client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runSearch in synctest bubble: %v", err)
		}
		if !strings.Contains(buf.String(), "**Morgan Price**") {
			t.Errorf("synctest output missing Morgan Price:\n%s", buf.String())
		}
	})
}

func TestSearchCLIExecution(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	messages := sampleSearchMessages()
	ts := newSearchTestServer(t, streams, topics, messages)
	defer ts.Close()

	dir := t.TempDir()
	validCfg := dir + "/.zuliprc"
	cfgContent := fmt.Sprintf("[api]\nemail = cli@example.com\nkey = clikey\nsite = %s\n", ts.URL)
	if err := os.WriteFile(validCfg, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("global search cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "watchflakes")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, `# Search: "watchflakes"`) {
			t.Errorf("stdout missing header:\n%s", stdout)
		}
		if !strings.Contains(stdout, "## [general > closing stale watchflakes]") {
			t.Errorf("stdout missing section header:\n%s", stdout)
		}
		if !strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout missing Morgan Price:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing all 2 matching messages") {
			t.Errorf("stdout missing search results count footer:\n%s", stdout)
		}
	})

	t.Run("scoped channel and topic json cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "-c", "general", "-t", "closing stale watchflakes", "-json", "stale")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		var res SearchResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v, stdout:\n%s", err, stdout)
		}
		if res.Channel != "general" || res.Topic != "closing stale watchflakes" || len(res.Messages) != 2 || res.Shown != 2 || res.HasMore {
			t.Errorf("unexpected SearchResult: %+v", res)
		}
	})

	t.Run("sender filter cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "-sender", "morgan@example.com")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "# Search from morgan@example.com") {
			t.Errorf("stdout missing sender header:\n%s", stdout)
		}
	})

	t.Run("missing query and filters cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "missing search query") {
			t.Errorf("stderr = %q, want missing search query error", stderr)
		}
	})

	t.Run("invalid limit cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "-n", "0", "query")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "-n limit must be greater than 0") {
			t.Errorf("stderr = %q, want limit error", stderr)
		}
	})

	t.Run("nonexistent channel cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "-c", "nonexistent", "query")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "channel \"nonexistent\" not found") {
			t.Errorf("stderr = %q, want channel not found error", stderr)
		}
	})

	t.Run("missing config error exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=/nonexistent/.zuliprc"}, "search", "query")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: no configuration found at /nonexistent/.zuliprc") {
			t.Errorf("stderr = %q, want missing config instructions", stderr)
		}
	})

	t.Run("until filter cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "search", "-c", "general", "-since", "2026-08-28T14:00:00Z", "-until", "2026-08-28T14:30:00Z", "watchflakes")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "## [general > closing stale watchflakes]") {
			t.Errorf("stdout missing section header:\n%s", stdout)
		}
		if !strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout missing Morgan Price:\n%s", stdout)
		}
		if strings.Contains(stdout, "**Alex Chen**") {
			t.Errorf("stdout should not have later message:\n%s", stdout)
		}
	})
}

func TestSearchTieBreakByID(t *testing.T) {
	t.Parallel()

	// Same timestamp, different IDs
	msgs := []Message{
		{ID: 200, Timestamp: 1724854500, StreamID: 1, DisplayRecipient: "general", Topic: "topic"},
		{ID: 100, Timestamp: 1724854500, StreamID: 1, DisplayRecipient: "general", Topic: "topic"},
	}

	groups := groupMessagesByTopic(msgs)
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if len(groups[0].messages) != 2 {
		t.Fatalf("len(groups[0].messages) = %d, want 2", len(groups[0].messages))
	}
	if groups[0].messages[0].ID != 100 || groups[0].messages[1].ID != 200 {
		t.Errorf("expected sorted by ID [100, 200], got [%d, %d]", groups[0].messages[0].ID, groups[0].messages[1].ID)
	}
}

func TestCmdSearchDirect(t *testing.T) {
	if err := cmdSearch([]string{"-invalid"}); err == nil {
		t.Error("cmdSearch with invalid flag expected error, got nil")
	}

	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdSearch([]string{"query"}); err == nil {
		t.Error("cmdSearch with nonexistent config expected error, got nil")
	}
}
