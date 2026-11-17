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

	"github.com/google/go-cmp/cmp"
)

func sampleTopics() []Topic {
	return []Topic{
		{Name: "closing stale watchflakes", MaxID: 619610069},
		{Name: "proposal review for some procedural deprecations?", MaxID: 619610050},
		{Name: "compiler optimizations in Go 1.27", MaxID: 619610010},
	}
}

func sampleStreams() []Stream {
	return []Stream{
		{StreamID: 615773, Name: "general", Description: "General discussion"},
		{StreamID: 1002, Name: "announce", Description: "Official announcements"},
		{StreamID: 1003, Name: "tools", Description: "Go tooling and gopls"},
	}
}

func newTopicsTestServer(t *testing.T, streams []Stream, topics []Topic) *httptest.Server {
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
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestResolveChannel(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	ts := newTopicsTestServer(t, streams, nil)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "key", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name         string
		channelRef   string
		wantStreamID int64
		wantName     string
		wantErr      string
	}{
		{
			name:         "exact name match",
			channelRef:   "general",
			wantStreamID: 615773,
			wantName:     "general",
		},
		{
			name:         "case-insensitive name match",
			channelRef:   "GENERAL",
			wantStreamID: 615773,
			wantName:     "general",
		},
		{
			name:         "leading hash symbol",
			channelRef:   "#announce",
			wantStreamID: 1002,
			wantName:     "announce",
		},
		{
			name:         "stream ID integer string",
			channelRef:   "1003",
			wantStreamID: 1003,
			wantName:     "tools",
		},
		{
			name:         "stream slug with ID and name",
			channelRef:   "615773-general",
			wantStreamID: 615773,
			wantName:     "general",
		},
		{
			name:         "full Zulip URL with channel and topic",
			channelRef:   ts.URL + "/#narrow/channel/615773-general/topic/some.20topic",
			wantStreamID: 615773,
			wantName:     "general",
		},
		{
			name:         "legacy stream URL",
			channelRef:   ts.URL + "/#narrow/stream/1002-announce",
			wantStreamID: 1002,
			wantName:     "announce",
		},
		{
			name:         "URL with channel ID only",
			channelRef:   ts.URL + "/#narrow/channel/1003",
			wantStreamID: 1003,
			wantName:     "tools",
		},
		{
			name:         "URL with channel name only",
			channelRef:   ts.URL + "/#narrow/channel/tools",
			wantStreamID: 1003,
			wantName:     "tools",
		},
		{
			name:         "URL with channel ID not found on server but channel name present in URL",
			channelRef:   ts.URL + "/#narrow/channel/999-tools",
			wantStreamID: 999,
			wantName:     "tools",
		},
		{
			name:       "empty channel reference",
			channelRef: "   ",
			wantErr:    "empty channel reference",
		},
		{
			name:       "nonexistent channel",
			channelRef: "nonexistent",
			wantErr:    "channel \"nonexistent\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamID, streamName, err := ResolveChannel(ctx, client, tt.channelRef)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ResolveChannel(%q) expected error containing %q, got nil", tt.channelRef, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ResolveChannel(%q) error = %q, want error containing %q", tt.channelRef, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveChannel(%q) unexpected error: %v", tt.channelRef, err)
			}
			if streamID != tt.wantStreamID {
				t.Errorf("streamID = %d, want %d", streamID, tt.wantStreamID)
			}
			if streamName != tt.wantName {
				t.Errorf("streamName = %q, want %q", streamName, tt.wantName)
			}
		})
	}
}

func TestResolveChannelServerError(t *testing.T) {
	t.Parallel()

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"result": "error", "msg": "Database failure"})
	}))
	defer errServer.Close()

	ctx := context.Background()
	cfg := &Config{Email: "a@b.com", Key: "k", Site: errServer.URL}
	client, err := NewClientWithHTTP(cfg, errServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = ResolveChannel(ctx, client, "general")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching channels") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFilterTopics(t *testing.T) {
	t.Parallel()

	topics := sampleTopics()

	t.Run("empty filter returns all", func(t *testing.T) {
		got := FilterTopics(topics, "")
		if diff := cmp.Diff(topics, got); diff != "" {
			t.Errorf("FilterTopics mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("substring filter case-insensitive single match", func(t *testing.T) {
		got := FilterTopics(topics, "WATCHFLAKES")
		if len(got) != 1 || got[0].Name != "closing stale watchflakes" {
			t.Errorf("unexpected filter result: %+v", got)
		}
	})

	t.Run("substring filter matching multiple", func(t *testing.T) {
		got := FilterTopics(topics, "in")
		// "closing stale watchflakes" (in 'closing'), "compiler optimizations in Go 1.27" (in 'in', 'optimizations')
		if len(got) < 2 {
			t.Errorf("expected at least 2 matches for 'in', got %d: %+v", len(got), got)
		}
	})

	t.Run("substring filter matching none", func(t *testing.T) {
		got := FilterTopics(topics, "nonexistent-query-string")
		if len(got) != 0 {
			t.Errorf("expected 0 matches, got %d: %+v", len(got), got)
		}
	})
}

func TestResolveTopic(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	ts := newTopicsTestServer(t, streams, topics)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "key", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("exact match", func(t *testing.T) {
		name, err := ResolveTopic(ctx, client, 615773, "closing stale watchflakes")
		if err != nil {
			t.Fatalf("ResolveTopic error: %v", err)
		}
		if name != "closing stale watchflakes" {
			t.Errorf("got %q, want 'closing stale watchflakes'", name)
		}
	})

	t.Run("case-insensitive exact match", func(t *testing.T) {
		name, err := ResolveTopic(ctx, client, 615773, "CLOSING STALE WATCHFLAKES")
		if err != nil {
			t.Fatalf("ResolveTopic error: %v", err)
		}
		if name != "closing stale watchflakes" {
			t.Errorf("got %q, want 'closing stale watchflakes'", name)
		}
	})

	t.Run("substring match", func(t *testing.T) {
		name, err := ResolveTopic(ctx, client, 615773, "watchflakes")
		if err != nil {
			t.Fatalf("ResolveTopic error: %v", err)
		}
		if name != "closing stale watchflakes" {
			t.Errorf("got %q, want 'closing stale watchflakes'", name)
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		_, err := ResolveTopic(ctx, client, 615773, "   ")
		if err == nil || !strings.Contains(err.Error(), "empty topic query") {
			t.Errorf("expected empty query error, got: %v", err)
		}
	})

	t.Run("topic not found error", func(t *testing.T) {
		_, err := ResolveTopic(ctx, client, 615773, "unknown-topic-query")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("api error on invalid stream", func(t *testing.T) {
		_, err := ResolveTopic(ctx, client, 999999, "watchflakes")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRunTopics(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	ts := newTopicsTestServer(t, streams, topics)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "key", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("topics text output all", func(t *testing.T) {
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			Limit:   30,
		}
		err := runTopics(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "[1] closing stale watchflakes") {
			t.Errorf("missing topic 1 in output:\n%s", out)
		}
		if !strings.Contains(out, "[2] proposal review for some procedural deprecations?") {
			t.Errorf("missing topic 2 in output:\n%s", out)
		}
		if !strings.Contains(out, "[3] compiler optimizations in Go 1.27") {
			t.Errorf("missing topic 3 in output:\n%s", out)
		}
	})

	t.Run("topics text output filtered", func(t *testing.T) {
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			Filter:  "watchflakes",
			Limit:   30,
		}
		err := runTopics(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "[1] closing stale watchflakes") {
			t.Errorf("missing topic 1 in output:\n%s", out)
		}
		if strings.Contains(out, "proposal review") {
			t.Errorf("unexpected topic in filtered output:\n%s", out)
		}
	})

	t.Run("topics limit flag", func(t *testing.T) {
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			Limit:   1,
		}
		err := runTopics(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "[1] closing stale watchflakes") {
			t.Errorf("missing topic 1:\n%s", out)
		}
		if strings.Contains(out, "[2]") {
			t.Errorf("topic count exceeded limit 1:\n%s", out)
		}
	})

	t.Run("topics json output", func(t *testing.T) {
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			AsJSON:  true,
			Limit:   30,
		}
		err := runTopics(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		var res TopicResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshaling TopicResult: %v\nOutput: %s", err, buf.String())
		}
		if res.Channel != "general" {
			t.Errorf("res.Channel = %q, want general", res.Channel)
		}
		if res.ChannelID != 615773 {
			t.Errorf("res.ChannelID = %d, want 615773", res.ChannelID)
		}
		if len(res.Topics) != 3 {
			t.Fatalf("len(res.Topics) = %d, want 3", len(res.Topics))
		}
		if res.Topics[0].Name != "closing stale watchflakes" || res.Topics[0].MaxID != 619610069 {
			t.Errorf("unexpected topic 0: %+v", res.Topics[0])
		}
	})

	t.Run("topics filter matching none text notice", func(t *testing.T) {
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			Filter:  "nomatch",
			Limit:   30,
		}
		err := runTopics(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "(no topics matching \"nomatch\" in general out of 3 total topics)") {
			t.Errorf("unexpected output for no match:\n%s", out)
		}
	})

	t.Run("topics empty on server text notice", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1/streams" {
				json.NewEncoder(w).Encode(map[string]any{
					"result":  "success",
					"streams": []Stream{{StreamID: 1, Name: "empty-channel"}},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"topics": []Topic{},
			})
		}))
		defer emptyServer.Close()

		emptyClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "empty-channel",
			Limit:   30,
		}
		err := runTopics(ctx, emptyClient, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		if !strings.Contains(buf.String(), "(no topics found in empty-channel)") {
			t.Errorf("unexpected output:\n%s", buf.String())
		}
	})

	t.Run("topics empty in JSON renders empty array", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1/streams" {
				json.NewEncoder(w).Encode(map[string]any{
					"result":  "success",
					"streams": []Stream{{StreamID: 1, Name: "empty-channel"}},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"topics": nil,
			})
		}))
		defer emptyServer.Close()

		emptyClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "empty-channel",
			AsJSON:  true,
			Limit:   30,
		}
		err := runTopics(ctx, emptyClient, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics error: %v", err)
		}
		var res TopicResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal error: %v\nOutput: %s", err, buf.String())
		}
		if res.Topics == nil || len(res.Topics) != 0 {
			t.Errorf("res.Topics = %v, want non-nil empty slice", res.Topics)
		}
	})

	t.Run("validation errors", func(t *testing.T) {
		if err := runTopics(ctx, client, TopicOptions{Channel: "", Limit: 10}, &bytes.Buffer{}); err == nil {
			t.Error("expected error for empty channel, got nil")
		}
		if err := runTopics(ctx, client, TopicOptions{Channel: "general", Limit: 0}, &bytes.Buffer{}); err == nil {
			t.Error("expected error for limit 0, got nil")
		}
		if err := runTopics(ctx, client, TopicOptions{Channel: "general", Limit: -5}, &bytes.Buffer{}); err == nil {
			t.Error("expected error for negative limit, got nil")
		}
		if err := runTopics(ctx, client, TopicOptions{Channel: "nonexistent", Limit: 10}, &bytes.Buffer{}); err == nil {
			t.Error("expected error for nonexistent channel, got nil")
		}

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
				"msg":    "Invalid channel ID",
				"code":   "BAD_REQUEST",
			})
		}))
		defer errServer.Close()

		errClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: errServer.URL}, errServer.Client())
		err := runTopics(ctx, errClient, TopicOptions{Channel: "bad-stream", Limit: 10}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "fetching topics for channel") {
			t.Errorf("expected fetching topics error, got: %v", err)
		}
	})
}

func TestTopicsSynctestBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		streams := sampleStreams()
		topics := sampleTopics()

		client, err := NewClientWithHTTP(&Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "application/json")
				if req.URL.Path == "/api/v1/streams" {
					json.NewEncoder(rec).Encode(map[string]any{
						"result":  "success",
						"streams": streams,
					})
					return rec.Result(), nil
				}
				json.NewEncoder(rec).Encode(map[string]any{
					"result": "success",
					"topics": topics,
				})
				return rec.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		var buf bytes.Buffer
		opts := TopicOptions{
			Channel: "general",
			Limit:   30,
		}
		err = runTopics(context.Background(), client, opts, &buf)
		if err != nil {
			t.Fatalf("runTopics in synctest bubble: %v", err)
		}
		if !strings.Contains(buf.String(), "[1] closing stale watchflakes") {
			t.Errorf("synctest output missing topic:\n%s", buf.String())
		}
	})
}

func TestTopicsCLIExecution(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	ts := newTopicsTestServer(t, streams, topics)
	defer ts.Close()

	dir := t.TempDir()
	validCfg := dir + "/.zuliprc"
	cfgContent := fmt.Sprintf("[api]\nemail = cli@example.com\nkey = clikey\nsite = %s\n", ts.URL)
	if err := os.WriteFile(validCfg, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("topics general text cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "general")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "[1] closing stale watchflakes") {
			t.Errorf("stdout missing topic 1:\n%s", stdout)
		}
		if !strings.Contains(stdout, "[2] proposal review") {
			t.Errorf("stdout missing topic 2:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing all 3 topics in general.") {
			t.Errorf("stdout missing summary footer:\n%s", stdout)
		}
	})

	t.Run("topics general with filter cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "general", "watchflakes")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "[1] closing stale watchflakes") {
			t.Errorf("stdout missing topic 1:\n%s", stdout)
		}
		if strings.Contains(stdout, "proposal review") {
			t.Errorf("stdout has unmatching topic:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing all 1 matching topics in general (3 total topics in channel).") {
			t.Errorf("stdout missing filter summary footer:\n%s", stdout)
		}
	})

	t.Run("topics general json cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "-json", "general")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		var res TopicResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("json unmarshal: %v\nstdout:\n%s", err, stdout)
		}
		if res.Channel != "general" || len(res.Topics) != 3 || res.Total != 3 || res.Shown != 3 || res.HasMore {
			t.Errorf("unexpected TopicResult: %+v", res)
		}
	})

	t.Run("topics limit cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "-n", "1", "general")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "[1] closing stale watchflakes") {
			t.Errorf("stdout missing topic 1:\n%s", stdout)
		}
		if strings.Contains(stdout, "[2]") {
			t.Errorf("stdout contains more than 1 topic:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing 1 of 3 topics in general. Use -n 2 to display more.") {
			t.Errorf("stdout missing pagination footer:\n%s", stdout)
		}
	})

	t.Run("topics missing channel argument cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "missing channel argument") {
			t.Errorf("stderr = %q, want missing channel argument error", stderr)
		}
	})

	t.Run("topics extra arguments cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "general", "filter", "extra_arg")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected extra arguments") {
			t.Errorf("stderr = %q, want unexpected extra arguments error", stderr)
		}
	})

	t.Run("topics invalid limit cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "-n", "0", "general")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "-n limit must be greater than 0") {
			t.Errorf("stderr = %q, want limit error", stderr)
		}
	})

	t.Run("topics channel not found cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "topics", "nonexistent")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "channel \"nonexistent\" not found") {
			t.Errorf("stderr = %q, want channel not found error", stderr)
		}
	})

	t.Run("topics missing config error exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=/nonexistent/.zuliprc"}, "topics", "general")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: no configuration found at /nonexistent/.zuliprc") {
			t.Errorf("stderr = %q, want missing config instructions", stderr)
		}
	})
}

func TestTopicsJSONEncodeError(t *testing.T) {
	t.Parallel()

	streams := sampleStreams()
	topics := sampleTopics()
	ts := newTopicsTestServer(t, streams, topics)
	defer ts.Close()

	cfg := &Config{Email: "a@b.com", Key: "k", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	opts := TopicOptions{
		Channel: "general",
		AsJSON:  true,
		Limit:   30,
	}
	err = runTopics(context.Background(), client, opts, errWriter{})
	if err == nil || !strings.Contains(err.Error(), "encoding JSON") {
		t.Errorf("runTopics expected encoding JSON error, got: %v", err)
	}
}

func TestCmdTopicsDirect(t *testing.T) {
	if err := cmdTopics([]string{"-invalid"}); err == nil {
		t.Error("cmdTopics with invalid flag expected error, got nil")
	}

	if err := cmdTopics([]string{}); err == nil {
		t.Error("cmdTopics with missing channel expected error, got nil")
	}

	if err := cmdTopics([]string{"c", "f", "extra"}); err == nil {
		t.Error("cmdTopics with extra args expected error, got nil")
	}

	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdTopics([]string{"general"}); err == nil {
		t.Error("cmdTopics with nonexistent config expected error, got nil")
	}
}
