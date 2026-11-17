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

func sampleSubscriptions() []Subscription {
	return []Subscription{
		{StreamID: 615773, Name: "general", Description: "General discussion", InviteOnly: false, IsWebPublic: true},
		{StreamID: 1002, Name: "announce", Description: "Official announcements", InviteOnly: false, IsWebPublic: true},
		{StreamID: 1004, Name: "security", Description: "Security discussions", InviteOnly: true, IsWebPublic: false},
		{StreamID: 1005, Name: "random", Description: "", InviteOnly: false, IsWebPublic: true},
	}
}

func sampleAllStreams() []Stream {
	return []Stream{
		{StreamID: 615773, Name: "general", Description: "General discussion", InviteOnly: false, IsWebPublic: true},
		{StreamID: 1002, Name: "announce", Description: "Official announcements", InviteOnly: false, IsWebPublic: true},
		{StreamID: 1003, Name: "tools", Description: "Go tooling and gopls", InviteOnly: false, IsWebPublic: true},
		{StreamID: 1004, Name: "security", Description: "Security discussions", InviteOnly: true, IsWebPublic: false},
		{StreamID: 1005, Name: "random", Description: "", InviteOnly: false, IsWebPublic: true},
	}
}

func sampleUnreadMessages() []Message {
	return []Message{
		{ID: 1, StreamID: 615773, Flags: []string{"starred"}}, // unread in general
		{ID: 2, StreamID: 615773, Flags: []string{"starred"}}, // unread in general
		{ID: 3, StreamID: 615773, Flags: []string{"starred"}}, // unread in general
		{ID: 4, StreamID: 1003, Flags: []string{"starred"}},   // unread in tools
	}
}

func newChannelsTestServer(t *testing.T, subs []Subscription, streams []Stream, unreads []Message) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/users/me/subscriptions":
			json.NewEncoder(w).Encode(map[string]any{
				"result":        "success",
				"subscriptions": subs,
			})
		case "/api/v1/streams":
			json.NewEncoder(w).Encode(map[string]any{
				"result":  "success",
				"streams": streams,
			})
		case "/api/v1/messages":
			json.NewEncoder(w).Encode(MessagesResponse{
				Result:   "success",
				Messages: unreads,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunChannels(t *testing.T) {
	t.Parallel()

	subs := sampleSubscriptions()
	streams := sampleAllStreams()
	unreads := sampleUnreadMessages()
	ts := newChannelsTestServer(t, subs, streams, unreads)
	defer ts.Close()

	ctx := context.Background()
	cfg := &Config{Email: "test@example.com", Key: "key", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("subscribed channels default text with access and unreads", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ChannelOptions{}
		err := runChannels(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runChannels error: %v", err)
		}
		out := buf.String()
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) < 4 {
			t.Fatalf("expected at least 4 lines (header + 3 channels), got %d:\n%s", len(lines), out)
		}
		if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "ACCESS") || !strings.Contains(lines[0], "STATUS") || !strings.Contains(lines[0], "UNREAD") {
			t.Errorf("header missing expected columns: %q", lines[0])
		}
		// announce: public, subscribed, -
		if !strings.Contains(out, "announce") || !strings.Contains(out, "public") {
			t.Errorf("missing announce row in output:\n%s", out)
		}
		// general: public, subscribed, 3 unread
		if !strings.Contains(out, "general") || !strings.Contains(out, "3 unread") {
			t.Errorf("missing general 3 unread in output:\n%s", out)
		}
		// security: private, subscribed, -
		if !strings.Contains(out, "security") || !strings.Contains(out, "private") {
			t.Errorf("missing security private in output:\n%s", out)
		}
	})

	t.Run("all channels text showing unsubscribed", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ChannelOptions{All: true}
		err := runChannels(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runChannels error: %v", err)
		}
		out := buf.String()
		// tools is unsubscribed and has 1 unread
		if !strings.Contains(out, "tools") || !strings.Contains(out, "1 unread") {
			t.Errorf("missing tools row in all channels:\n%s", out)
		}
	})

	t.Run("channels with descriptions", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ChannelOptions{Description: true}
		err := runChannels(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runChannels error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "DESCRIPTION") {
			t.Errorf("missing DESCRIPTION header in output:\n%s", out)
		}
		if !strings.Contains(out, "Official announcements") {
			t.Errorf("missing announce description:\n%s", out)
		}
		if !strings.Contains(out, "General discussion") {
			t.Errorf("missing general description:\n%s", out)
		}
	})

	t.Run("channels json output with private subscribed and unread_count", func(t *testing.T) {
		var buf bytes.Buffer
		opts := ChannelOptions{All: true, AsJSON: true}
		err := runChannels(ctx, client, opts, &buf)
		if err != nil {
			t.Fatalf("runChannels error: %v", err)
		}
		var res ChannelResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal ChannelResult: %v\nOutput:\n%s", err, buf.String())
		}
		if len(res.Channels) != 5 {
			t.Fatalf("len(res.Channels) = %d, want 5", len(res.Channels))
		}

		// Find general, security, tools
		var general, security, tools ChannelItem
		for _, ch := range res.Channels {
			switch ch.Name {
			case "general":
				general = ch
			case "security":
				security = ch
			case "tools":
				tools = ch
			}
		}

		if general.Private || !general.Subscribed || general.UnreadCount != 3 {
			t.Errorf("unexpected general: %+v", general)
		}
		if !security.Private || !security.Subscribed || security.UnreadCount != 0 {
			t.Errorf("unexpected security: %+v", security)
		}
		if tools.Private || tools.Subscribed || tools.UnreadCount != 1 {
			t.Errorf("unexpected tools: %+v", tools)
		}
	})

	t.Run("empty channels text output", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"result":        "success",
				"subscriptions": []Subscription{},
			})
		}))
		defer emptyServer.Close()

		emptyClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		var buf bytes.Buffer
		err := runChannels(ctx, emptyClient, ChannelOptions{}, &buf)
		if err != nil {
			t.Fatalf("runChannels: %v", err)
		}
		if strings.TrimSpace(buf.String()) != "(no channels found)" {
			t.Errorf("got %q, want '(no channels found)'", buf.String())
		}
	})

	t.Run("empty channels json output", func(t *testing.T) {
		emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"result":        "success",
				"subscriptions": nil,
			})
		}))
		defer emptyServer.Close()

		emptyClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: emptyServer.URL}, emptyServer.Client())
		var buf bytes.Buffer
		err := runChannels(ctx, emptyClient, ChannelOptions{AsJSON: true}, &buf)
		if err != nil {
			t.Fatalf("runChannels: %v", err)
		}
		var res ChannelResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if res.Channels == nil || len(res.Channels) != 0 {
			t.Errorf("res.Channels = %v, want non-nil empty slice", res.Channels)
		}
	})
}

func TestChannelsErrorHandling(t *testing.T) {
	t.Parallel()

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"result": "error", "msg": "Invalid API key"})
	}))
	defer errServer.Close()

	ctx := context.Background()
	cfg := &Config{Email: "a@b.com", Key: "badkey", Site: errServer.URL}
	client, err := NewClientWithHTTP(cfg, errServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("subscriptions error", func(t *testing.T) {
		err := runChannels(ctx, client, ChannelOptions{All: false}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "fetching subscribed channels") {
			t.Errorf("expected fetching subscribed channels error, got: %v", err)
		}
	})

	t.Run("all channels error", func(t *testing.T) {
		err := runChannels(ctx, client, ChannelOptions{All: true}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "fetching channels") {
			t.Errorf("expected fetching channels error, got: %v", err)
		}
	})
}

func TestChannelsSynctestBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		subs := sampleSubscriptions()
		streams := sampleAllStreams()
		unreads := sampleUnreadMessages()

		client, err := NewClientWithHTTP(&Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "application/json")
				switch req.URL.Path {
				case "/api/v1/streams":
					json.NewEncoder(rec).Encode(map[string]any{
						"result":  "success",
						"streams": streams,
					})
					return rec.Result(), nil
				case "/api/v1/messages":
					json.NewEncoder(rec).Encode(MessagesResponse{
						Result:   "success",
						Messages: unreads,
					})
					return rec.Result(), nil
				default:
					json.NewEncoder(rec).Encode(map[string]any{
						"result":        "success",
						"subscriptions": subs,
					})
					return rec.Result(), nil
				}
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		var buf bytes.Buffer
		err = runChannels(context.Background(), client, ChannelOptions{All: true, Description: true}, &buf)
		if err != nil {
			t.Fatalf("runChannels synctest: %v", err)
		}
		if !strings.Contains(buf.String(), "announce") || !strings.Contains(buf.String(), "Official announcements") {
			t.Errorf("synctest channels output missing expected content:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "3 unread") {
			t.Errorf("synctest channels missing 3 unread:\n%s", buf.String())
		}
	})
}

func TestChannelsCLIExecution(t *testing.T) {
	t.Parallel()

	subs := sampleSubscriptions()
	streams := sampleAllStreams()
	unreads := sampleUnreadMessages()
	ts := newChannelsTestServer(t, subs, streams, unreads)
	defer ts.Close()

	dir := t.TempDir()
	validCfg := dir + "/.zuliprc"
	cfgContent := fmt.Sprintf("[api]\nemail = cli@example.com\nkey = clikey\nsite = %s\n", ts.URL)
	if err := os.WriteFile(validCfg, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("channels default text cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "announce") || !strings.Contains(stdout, "general") {
			t.Errorf("stdout missing channels:\n%s", stdout)
		}
		if strings.Contains(stdout, "tools") {
			t.Errorf("stdout should not have unsubscribed stream 'tools':\n%s", stdout)
		}
		if !strings.Contains(stdout, "3 unread") {
			t.Errorf("stdout missing unread count:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing all 4 subscribed channels. (Use -all to see all public channels)") {
			t.Errorf("stdout missing summary footer:\n%s", stdout)
		}
	})

	t.Run("streams alias cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "streams")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "announce") || !strings.Contains(stdout, "general") {
			t.Errorf("stdout missing channels:\n%s", stdout)
		}
	})

	t.Run("channels -all cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels", "-all")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "tools") || !strings.Contains(stdout, "security") {
			t.Errorf("stdout missing all public streams:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Showing all 5 public channels.") {
			t.Errorf("stdout missing all channels summary footer:\n%s", stdout)
		}
	})

	t.Run("channels -desc cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels", "-desc")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "Official announcements") {
			t.Errorf("stdout missing channel description:\n%s", stdout)
		}
	})

	t.Run("channels -json cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels", "-json")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		var res ChannelResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("json unmarshal: %v\nstdout:\n%s", err, stdout)
		}
		if len(res.Channels) != 4 || res.Total != 4 || res.Shown != 4 || res.HasMore {
			t.Errorf("unexpected ChannelResult: %+v", res)
		}
	})

	t.Run("channels -n limit cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels", "-n", "2")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "Showing 2 of 4 channels. Use -n 4 to display more.") {
			t.Errorf("stdout missing pagination footer:\n%s", stdout)
		}
	})

	t.Run("channels unexpected args cli", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + validCfg}, "channels", "unexpected_arg")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected arguments") {
			t.Errorf("stderr = %q, want unexpected arguments error", stderr)
		}
	})

	t.Run("channels missing config error exit code 1", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=/nonexistent/.zuliprc"}, "channels")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: no configuration found at /nonexistent/.zuliprc") {
			t.Errorf("stderr = %q, want missing config instructions", stderr)
		}
	})
}

func TestChannelsJSONEncodeError(t *testing.T) {
	t.Parallel()

	subs := sampleSubscriptions()
	streams := sampleAllStreams()
	unreads := sampleUnreadMessages()
	ts := newChannelsTestServer(t, subs, streams, unreads)
	defer ts.Close()

	cfg := &Config{Email: "a@b.com", Key: "k", Site: ts.URL}
	client, err := NewClientWithHTTP(cfg, ts.Client())
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	opts := ChannelOptions{
		AsJSON: true,
	}
	err = runChannels(context.Background(), client, opts, errWriter{})
	if err == nil || !strings.Contains(err.Error(), "encoding JSON") {
		t.Errorf("runChannels expected encoding JSON error, got: %v", err)
	}
}

func TestCmdChannelsDirect(t *testing.T) {
	if err := cmdChannels([]string{"-invalid"}); err == nil {
		t.Error("cmdChannels with invalid flag expected error, got nil")
	}

	if err := cmdChannels([]string{"unexpected"}); err == nil {
		t.Error("cmdChannels with unexpected args expected error, got nil")
	}

	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdChannels([]string{}); err == nil {
		t.Error("cmdChannels with nonexistent config expected error, got nil")
	}
}
