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
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

var (
	testTime1 = time.Date(2026, 8, 28, 14, 15, 0, 0, time.UTC).Unix()
	testTime2 = time.Date(2026, 8, 28, 15, 2, 0, 0, time.UTC).Unix()
)

func sampleTestMessages() []Message {
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
			Flags:            []string{"starred"}, // unread
			Reactions: []Reaction{
				{
					EmojiName:    "+1",
					EmojiCode:    "1f44d",
					ReactionType: "unicode_emoji",
					UserID:       101,
				},
			},
		},
	}
}

func newTestServer(t *testing.T, messages []Message) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			http.NotFound(w, r)
			return
		}
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MessagesResponse{
			Result:      "success",
			FoundAnchor: true,
			FoundOldest: true,
			FoundNewest: true,
			Messages:    messages,
		})
	}))
}

func TestReadFromURL(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name       string
		url        string
		wantHeader string
		wantURL    string
	}{
		{
			name:       "channel topic with message ID anchor",
			url:        ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069",
			wantHeader: "# [general] closing stale watchflakes",
			wantURL:    ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069",
		},
		{
			name:       "legacy stream topic near anchor",
			url:        ts.URL + "/#narrow/stream/615773-general/topic/closing.20stale.20watchflakes/near/619610069",
			wantHeader: "# [general] closing stale watchflakes",
			wantURL:    ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/near/619610069",
		},
		{
			name:       "single message id anchor",
			url:        ts.URL + "/#narrow/id/619610069",
			wantHeader: "# [general] closing stale watchflakes",
			wantURL:    ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/id/619610069",
		},
		{
			name:       "relative fragment with channel topic only",
			url:        "#narrow/channel/615773-general/topic/closing.20stale.20watchflakes",
			wantHeader: "# [general] closing stale watchflakes",
			wantURL:    ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ReadOptions{
				URL:   tt.url,
				Limit: 50,
			}
			err := runRead(ctx, client, cfg, opts, &buf)
			if err != nil {
				t.Fatalf("runRead(%q) error = %v", tt.url, err)
			}
			out := buf.String()
			if !strings.Contains(out, tt.wantHeader) {
				t.Errorf("output missing wantHeader %q, got:\n%s", tt.wantHeader, out)
			}
			if !strings.Contains(out, "URL: "+tt.wantURL) {
				t.Errorf("output missing URL %q, got:\n%s", tt.wantURL, out)
			}
			if !strings.Contains(out, "**Morgan Price**") || !strings.Contains(out, "**Alex Chen**") {
				t.Errorf("output missing senders, got:\n%s", out)
			}
		})
	}
}

func TestReadFromFlags(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("channel and topic flags", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "# [general] closing stale watchflakes") {
			t.Errorf("out missing header, got:\n%s", out)
		}
	})

	t.Run("channel only flag", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "# [general]") {
			t.Errorf("out missing channel header, got:\n%s", out)
		}
	})

	t.Run("topic only flag", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Topic: "closing stale watchflakes",
			Limit: 50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "closing stale watchflakes") {
			t.Errorf("out missing topic header, got:\n%s", out)
		}
	})

	t.Run("message ID only flag", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			MsgID: 619610069,
			Limit: 50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "[ID: 619610069]") {
			t.Errorf("out missing message ID, got:\n%s", out)
		}
	})
}

func TestReadUnread(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("filter unread messages", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Unread:  true,
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		// Only Alex Chen's message is unread
		if strings.Contains(out, "**Morgan Price**") {
			t.Errorf("out should not contain read message from Morgan Price, got:\n%s", out)
		}
		if !strings.Contains(out, "**Alex Chen**") {
			t.Errorf("out missing unread message from Alex Chen, got:\n%s", out)
		}
	})

	t.Run("all messages read gives empty notice", func(t *testing.T) {
		readOnlyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(MessagesResponse{
				Result:   "success",
				Messages: []Message{messages[0]}, // flags: ["read"]
			})
		}))
		defer readOnlyServer.Close()

		readClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: readOnlyServer.URL}, readOnlyServer.Client())
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Unread:  true,
			Limit:   50,
		}
		err := runRead(ctx, readClient, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "(no messages found)") {
			t.Errorf("out missing '(no messages found)', got:\n%s", out)
		}
	})
}

func TestReadSince(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	// messages[0].Timestamp = 1724854500 (2026-08-28 14:15:00 UTC)
	// messages[1].Timestamp = 1724857320 (2026-08-28 15:02:00 UTC)
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("since filter matching second message only", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Since:   "2026-08-28T14:30:00Z",
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if strings.Contains(out, "**Morgan Price**") {
			t.Errorf("out should not contain older message, got:\n%s", out)
		}
		if !strings.Contains(out, "**Alex Chen**") {
			t.Errorf("out missing newer message, got:\n%s", out)
		}
	})

	t.Run("since filter matching both messages", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Since:   "2026-08-28T14:00:00Z",
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "**Morgan Price**") || !strings.Contains(out, "**Alex Chen**") {
			t.Errorf("out missing messages, got:\n%s", out)
		}
	})
}

func TestReadJSON(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("valid JSON output", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			AsJSON:  true,
			Limit:   50,
		}
		err := runRead(ctx, client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}

		var res ReadResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v\nOutput was: %s", err, buf.String())
		}
		if res.Channel != "general" {
			t.Errorf("res.Channel = %q, want general", res.Channel)
		}
		if res.Topic != "closing stale watchflakes" {
			t.Errorf("res.Topic = %q, want closing stale watchflakes", res.Topic)
		}
		if len(res.Messages) != 2 {
			t.Fatalf("got %d messages, want 2", len(res.Messages))
		}
		if res.Messages[0].ID != 619610069 || res.Messages[1].ID != 619610075 {
			t.Errorf("unexpected messages: %+v", res.Messages)
		}
	})

	t.Run("empty messages JSON array", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(MessagesResponse{Result: "success", Messages: nil})
		}))
		defer emptyServer.Close()

		emptyClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "empty",
			AsJSON:  true,
			Limit:   50,
		}
		err := runRead(ctx, emptyClient, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead error: %v", err)
		}

		var res ReadResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v", err)
		}
		if res.Messages == nil || len(res.Messages) != 0 {
			t.Errorf("res.Messages = %v, want non-nil empty slice", res.Messages)
		}
	})
}

func TestReadUTC(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  ts.URL,
	}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var buf bytes.Buffer
	opts := ReadOptions{
		Channel: "general",
		Topic:   "closing stale watchflakes",
		UTC:     true,
		Limit:   50,
	}
	err = runRead(ctx, client, cfg, opts, &buf)
	if err != nil {
		t.Fatalf("runRead error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "(2026-08-28 14:15 UTC)") {
		t.Errorf("out missing UTC timestamp, got:\n%s", out)
	}
}

func TestReadMarkdownFormattingEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("sender fallback hierarchy", func(t *testing.T) {
		msgs := []Message{
			{
				ID:          1,
				Timestamp:   1724854500,
				SenderEmail: "fallback@example.com",
				Content:     "message with email only",
			},
			{
				ID:        2,
				SenderID:  999,
				Timestamp: 1724854500,
				Content:   "message with user ID only",
			},
		}
		target := &NarrowTarget{Channel: "dev", Topic: "edge-cases"}
		var buf bytes.Buffer
		err := formatThreadMarkdown(&buf, target, msgs, "https://example.com", true, true, true, 50)
		if err != nil {
			t.Fatalf("formatThreadMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "**fallback@example.com**") {
			t.Errorf("out missing sender email fallback, got:\n%s", out)
		}
		if !strings.Contains(out, "**User 999**") {
			t.Errorf("out missing user ID fallback, got:\n%s", out)
		}
	})

	t.Run("headers without channel or topic", func(t *testing.T) {
		var buf bytes.Buffer
		target := &NarrowTarget{}
		err := formatThreadMarkdown(&buf, target, nil, "", true, true, true, 50)
		if err != nil {
			t.Fatalf("formatThreadMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "# Zulip Messages") {
			t.Errorf("out missing default header, got:\n%s", out)
		}
		if !strings.Contains(out, "(no messages found)") {
			t.Errorf("out missing (no messages found), got:\n%s", out)
		}
	})

	t.Run("channel only header", func(t *testing.T) {
		var buf bytes.Buffer
		target := &NarrowTarget{Channel: "general"}
		err := formatThreadMarkdown(&buf, target, nil, "", true, true, true, 50)
		if err != nil {
			t.Fatalf("formatThreadMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "# [general]") {
			t.Errorf("out missing channel only header, got:\n%s", out)
		}
	})

	t.Run("topic only header", func(t *testing.T) {
		var buf bytes.Buffer
		target := &NarrowTarget{Topic: "general-topic"}
		err := formatThreadMarkdown(&buf, target, nil, "", true, true, true, 50)
		if err != nil {
			t.Fatalf("formatThreadMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "# general-topic") {
			t.Errorf("out missing topic only header, got:\n%s", out)
		}
	})

	t.Run("pagination banners when has earlier or newer", func(t *testing.T) {
		msgs := []Message{
			{ID: 100, SenderFullName: "Alice", Content: "hello", Timestamp: 1724854500},
		}
		target := &NarrowTarget{Channel: "general", Topic: "pagination"}
		var buf bytes.Buffer
		err := formatThreadMarkdown(&buf, target, msgs, "https://example.com", true, false, false, 20)
		if err != nil {
			t.Fatalf("formatThreadMarkdown error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "[... earlier messages exist in this topic. Use -n 40 or -id 100 to read earlier ...]") {
			t.Errorf("out missing earlier messages banner:\n%s", out)
		}
		if !strings.Contains(out, "[... newer messages exist in this topic. Use -n 40 or -unread to read newer ...]") {
			t.Errorf("out missing newer messages banner:\n%s", out)
		}
		if !strings.Contains(out, "(Showing 1 messages. Earlier messages: yes, Newer messages: yes. Use -n to adjust limit)") {
			t.Errorf("out missing summary footer:\n%s", out)
		}
	})
}

func TestReadValidationErrors(t *testing.T) {
	t.Parallel()

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"result": "error",
			"msg":    "Stream does not exist",
			"code":   "STREAM_DOES_NOT_EXIST",
		})
	}))
	defer errServer.Close()

	ctx := context.Background()
	cfg := &Config{
		Email: "test@example.com",
		Key:   "testkey",
		Site:  errServer.URL,
	}
	client, err := NewClientWithHTTP(cfg, errServer.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name    string
		opts    ReadOptions
		wantErr string
	}{
		{
			name: "limit_zero",
			opts: ReadOptions{
				Channel: "general",
				Limit:   0,
			},
			wantErr: "-n limit must be greater than 0",
		},
		{
			name: "limit_negative",
			opts: ReadOptions{
				Channel: "general",
				Limit:   -5,
			},
			wantErr: "-n limit must be greater than 0",
		},
		{
			name: "missing_all_selectors",
			opts: ReadOptions{
				Limit: 50,
			},
			wantErr: "must specify a Zulip URL, channel (-c), topic (-t), or message ID (-id)",
		},
		{
			name: "invalid_url",
			opts: ReadOptions{
				URL:   "https://example.com/not_narrow",
				Limit: 50,
			},
			wantErr: "no narrow fragment found in URL",
		},
		{
			name: "invalid_since_format",
			opts: ReadOptions{
				Channel: "general",
				Since:   "not_a_valid_date_or_duration",
				Limit:   50,
			},
			wantErr: "invalid time or duration",
		},
		{
			name: "invalid_until_format",
			opts: ReadOptions{
				Channel: "general",
				Until:   "not_a_valid_date_or_duration",
				Limit:   50,
			},
			wantErr: "invalid time or duration",
		},
		{
			name: "until_before_since",
			opts: ReadOptions{
				Channel: "general",
				Since:   "2h",
				Until:   "4h",
				Limit:   50,
			},
			wantErr: "-until time must be after -since time",
		},
		{
			name: "api_error_stream_not_found",
			opts: ReadOptions{
				Channel: "nonexistent",
				Limit:   50,
			},
			wantErr: "Stream does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := runRead(ctx, client, cfg, tt.opts, &buf)
			if err == nil {
				t.Fatalf("runRead() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("runRead() error = %q, want error containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReadSynctestBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		messages := sampleTestMessages()
		client, err := NewClientWithHTTP(&Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "application/json")
				json.NewEncoder(rec).Encode(MessagesResponse{
					Result:   "success",
					Messages: messages,
				})
				return rec.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		cfg := &Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}

		var buf bytes.Buffer
		opts := ReadOptions{
			Channel: "general",
			Topic:   "closing stale watchflakes",
			Limit:   50,
		}
		err = runRead(context.Background(), client, cfg, opts, &buf)
		if err != nil {
			t.Fatalf("runRead: %v", err)
		}
		if !strings.Contains(buf.String(), "**Morgan Price**") {
			t.Errorf("output missing Morgan Price: %s", buf.String())
		}
	})
}

func TestFilterAPINarrowOps(t *testing.T) {
	t.Parallel()

	input := []NarrowOp{
		{Op: "channel", Operand: int64(100)},
		{Op: "topic", Operand: "test"},
		{Op: "with", Operand: int64(619610069)},
		{Op: "near", Operand: int64(619610070)},
		{Op: "id", Operand: int64(619610071)},
		{Op: "sender", Operand: "alice@example.com"},
		{Op: "is", Operand: "starred"},
	}

	got := FilterAPINarrowOps(input)
	want := []NarrowOp{
		{Op: "channel", Operand: int64(100)},
		{Op: "topic", Operand: "test"},
		{Op: "sender", Operand: "alice@example.com"},
		{Op: "is", Operand: "starred"},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FilterAPINarrowOps mismatch (-want +got):\n%s", diff)
	}
}

func TestReadCLIExecution(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MessagesResponse{
			Result:      "success",
			FoundAnchor: true,
			FoundOldest: true,
			FoundNewest: true,
			Messages:    messages,
		})
	}))
	defer ts.Close()

	dir := t.TempDir()
	validCfg := dir + "/.zuliprc"
	cfgContent := "[api]\nemail = cli@example.com\nkey = clikey\nsite = " + ts.URL + "\n"
	if err := os.WriteFile(validCfg, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("read URL via CLI", func(t *testing.T) {
		url := ts.URL + "/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069"
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", url)
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "# [general] closing stale watchflakes") {
			t.Errorf("stdout missing title, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout missing Morgan Price, got:\n%s", stdout)
		}
	})

	t.Run("read channel and topic with json flag", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-t", "closing stale watchflakes", "-json")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		var res ReadResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("json.Unmarshal error: %v, stdout:\n%s", err, stdout)
		}
		if res.Channel != "general" || len(res.Messages) != 2 || res.Shown != 2 || !res.FoundOldest || !res.FoundNewest {
			t.Errorf("unexpected ReadResult: %+v", res)
		}
	})

	t.Run("read unread messages via CLI", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-t", "closing stale watchflakes", "-unread")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout should not have read messages, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "**Alex Chen**") {
			t.Errorf("stdout missing unread message, got:\n%s", stdout)
		}
	})

	t.Run("read with since flag via CLI", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-t", "closing stale watchflakes", "-since", "2026-08-28T14:30:00Z")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout should not have filtered message, got:\n%s", stdout)
		}
		if !strings.Contains(stdout, "**Alex Chen**") {
			t.Errorf("stdout missing newer message, got:\n%s", stdout)
		}
	})

	t.Run("read with until flag via CLI", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-t", "closing stale watchflakes", "-until", "2026-08-28T14:30:00Z")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "**Morgan Price**") {
			t.Errorf("stdout missing Morgan Price, got:\n%s", stdout)
		}
		if strings.Contains(stdout, "**Alex Chen**") {
			t.Errorf("stdout should not have later message, got:\n%s", stdout)
		}
	})

	t.Run("read with utc flag via CLI", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-t", "closing stale watchflakes", "-utc")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "(2026-08-28 14:15 UTC)") {
			t.Errorf("stdout missing UTC timestamp, got:\n%s", stdout)
		}
	})

	t.Run("read with message ID flag via CLI", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-id", "619610069")
		if code != 0 {
			t.Fatalf("runCLI exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "[ID: 619610069]") {
			t.Errorf("stdout missing message ID, got:\n%s", stdout)
		}
	})

	t.Run("read missing config error exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=/nonexistent/.zuliprc"}, "read", "-c", "general")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: no configuration found at /nonexistent/.zuliprc") {
			t.Errorf("stderr = %q, want missing config instructions", stderr)
		}
	})

	t.Run("read missing selector error exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "must specify a Zulip URL, channel (-c), topic (-t), or message ID (-id)") {
			t.Errorf("stderr = %q, want selector error", stderr)
		}
	})

	t.Run("read extra arguments error exit code 1", func(t *testing.T) {
		url := ts.URL + "/#narrow/channel/615773-general"
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", url, "extra_arg")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected extra arguments: extra_arg") {
			t.Errorf("stderr = %q, want unexpected extra arguments", stderr)
		}
	})

	t.Run("read invalid limit exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "read", "-c", "general", "-n", "0")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "-n limit must be greater than 0") {
			t.Errorf("stderr = %q, want invalid limit error", stderr)
		}
	})
}

func TestReadJSONEncodeError(t *testing.T) {
	t.Parallel()

	messages := sampleTestMessages()
	ts := newTestServer(t, messages)
	defer ts.Close()

	cfg := &Config{Email: "a@b.com", Key: "k", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	opts := ReadOptions{
		Channel: "general",
		Topic:   "closing stale watchflakes",
		AsJSON:  true,
		Limit:   50,
	}
	err = runRead(context.Background(), client, cfg, opts, errWriter{})
	if err == nil || !strings.Contains(err.Error(), "encoding JSON") {
		t.Errorf("runRead expected encoding JSON error, got: %v", err)
	}
}

func TestCmdReadDirect(t *testing.T) {
	if err := cmdRead([]string{"-invalid"}); err == nil {
		t.Error("cmdRead with invalid flag expected error, got nil")
	}

	if err := cmdRead([]string{"url1", "url2"}); err == nil {
		t.Error("cmdRead with extra args expected error, got nil")
	}

	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdRead([]string{"url"}); err == nil {
		t.Error("cmdRead with nonexistent config expected error, got nil")
	}
}
