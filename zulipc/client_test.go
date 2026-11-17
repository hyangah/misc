// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "nil_config",
			cfg:     nil,
			wantErr: "nil config provided",
		},
		{
			name: "missing_email",
			cfg: &Config{
				Key:  "secretkey",
				Site: "https://zulip.example.com",
			},
			wantErr: "missing email in config",
		},
		{
			name: "missing_key",
			cfg: &Config{
				Email: "alice@example.com",
				Site:  "https://zulip.example.com",
			},
			wantErr: "missing key in config",
		},
		{
			name: "missing_site",
			cfg: &Config{
				Email: "alice@example.com",
				Key:   "secretkey",
			},
			wantErr: "missing site in config",
		},
		{
			name: "valid_config",
			cfg: &Config{
				Email:    "alice@example.com",
				Key:      "secretkey",
				Site:     "https://zulip.example.com/",
				Insecure: true,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewClient() error = %v, wantErr %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}
			if client.baseURL != "https://zulip.example.com" {
				t.Errorf("client.baseURL = %q, want %q", client.baseURL, "https://zulip.example.com")
			}
		})
	}
}

func TestGetMe(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me" {
			http.NotFound(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alex@example.com" || pass != "validkey" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"result": "error",
				"msg":    "Invalid API key",
				"code":   "UNAUTHORIZED",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(User{
			UserID:            101,
			Email:             "alex@example.com",
			FullName:          "Alex Chen",
			IsAdmin:           true,
			IsOwner:           false,
			Role:              200,
			Timezone:          "America/New_York",
			AvatarURL:         "https://avatar.example.com/alex.png",
			ZulipVersion:      "9.0",
			ZulipFeatureLevel: 250,
		})
	}))
	defer ts.Close()

	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{
			Email: "alex@example.com",
			Key:   "validkey",
			Site:  ts.URL,
		}, ts.Client())
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		user, err := client.GetMe(ctx)
		if err != nil {
			t.Fatalf("GetMe: %v", err)
		}

		want := &User{
			UserID:            101,
			Email:             "alex@example.com",
			FullName:          "Alex Chen",
			IsAdmin:           true,
			IsOwner:           false,
			Role:              200,
			Timezone:          "America/New_York",
			AvatarURL:         "https://avatar.example.com/alex.png",
			ZulipVersion:      "9.0",
			ZulipFeatureLevel: 250,
		}

		if diff := cmp.Diff(want, user); diff != "" {
			t.Errorf("GetMe() mismatch (-want +got):\n%s", diff)
		}
		if got := user.RoleName(); got != "Administrator" {
			t.Errorf("RoleName() = %q, want Administrator", got)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{
			Email: "alex@example.com",
			Key:   "badkey",
			Site:  ts.URL,
		}, ts.Client())
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		_, err = client.GetMe(ctx)
		if err == nil {
			t.Fatal("expected unauthorized error, got nil")
		}
		if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid API key") {
			t.Errorf("error = %q, want 401 and Invalid API key", err.Error())
		}
	})
}

func TestUserRoleNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		user User
		want string
	}{
		{user: User{Role: 100}, want: "Owner"},
		{user: User{Role: 200}, want: "Administrator"},
		{user: User{Role: 300}, want: "Moderator"},
		{user: User{Role: 400}, want: "Member"},
		{user: User{Role: 600}, want: "Guest"},
		{user: User{IsOwner: true}, want: "Owner"},
		{user: User{IsAdmin: true}, want: "Administrator"},
		{user: User{IsGuest: true}, want: "Guest"},
		{user: User{}, want: "Member"},
	}

	for _, tt := range tests {
		got := tt.user.RoleName()
		if got != tt.want {
			t.Errorf("RoleName(%+v) = %q, want %q", tt.user, got, tt.want)
		}
	}
}

func TestGetServerSettings(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server_settings" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(ServerSettings{
			ZulipVersion:      "9.0",
			ZulipFeatureLevel: 250,
			RealmName:         "Go Community",
			RealmURI:          "https://zulip.example.com",
		})
	}))
	defer ts.Close()

	client, err := NewClientWithHTTP(&Config{
		Email: "a@b.com",
		Key:   "k",
		Site:  ts.URL,
	}, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	settings, err := client.GetServerSettings(context.Background())
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if settings.ZulipVersion != "9.0" || settings.RealmName != "Go Community" {
		t.Errorf("unexpected settings: %+v", settings)
	}
}

func TestGetMessages(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		resp := MessagesResponse{
			Result:      "success",
			FoundAnchor: true,
			FoundOldest: true,
			FoundNewest: true,
			Anchor:      619610069,
			Messages: []Message{
				{
					ID:               619610069,
					SenderID:         101,
					SenderFullName:   "Morgan Price",
					SenderEmail:      "morgan@example.com",
					Timestamp:        1724854500,
					Content:          "I have gone through the list of stale watchflakes.",
					Type:             "stream",
					StreamID:         615773,
					DisplayRecipient: "general",
					Subject:          "closing stale watchflakes",
					Flags:            []string{"read"},
				},
				{
					ID:               619610075,
					SenderID:         102,
					SenderFullName:   "Alex Chen",
					SenderEmail:      "alex@example.com",
					Timestamp:        1724857320,
					Content:          "Thanks! Let's monitor over the weekend.",
					Type:             "stream",
					StreamID:         615773,
					DisplayRecipient: "general",
					Topic:            "closing stale watchflakes",
					Flags:            []string{"starred"},
					Reactions: []Reaction{
						{
							EmojiName:    "+1",
							EmojiCode:    "1f44d",
							ReactionType: "unicode_emoji",
							UserID:       101,
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClientWithHTTP(&Config{
		Email: "a@b.com",
		Key:   "k",
		Site:  ts.URL,
	}, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("fetch_success", func(t *testing.T) {
		applyMd := true
		opts := GetMessagesOptions{
			Anchor:        "619610069",
			NumBefore:     10,
			NumAfter:      10,
			Narrow:        []NarrowOp{{Op: "channel", Operand: "general"}},
			ApplyMarkdown: &applyMd,
		}

		resp, err := client.GetMessages(ctx, opts)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}

		if len(resp.Messages) != 2 {
			t.Fatalf("got %d messages, want 2", len(resp.Messages))
		}

		m1 := resp.Messages[0]
		if m1.TopicName() != "closing stale watchflakes" {
			t.Errorf("m1.TopicName() = %q, want %q", m1.TopicName(), "closing stale watchflakes")
		}
		if m1.ChannelName() != "general" {
			t.Errorf("m1.ChannelName() = %q, want %q", m1.ChannelName(), "general")
		}
		if m1.IsUnread() {
			t.Errorf("m1.IsUnread() = true, want false")
		}
		if m1.IsStarred() {
			t.Errorf("m1.IsStarred() = true, want false")
		}

		m2 := resp.Messages[1]
		if m2.TopicName() != "closing stale watchflakes" {
			t.Errorf("m2.TopicName() = %q, want %q", m2.TopicName(), "closing stale watchflakes")
		}
		if !m2.IsUnread() {
			t.Errorf("m2.IsUnread() = false, want true")
		}
		if !m2.IsStarred() {
			t.Errorf("m2.IsStarred() = false, want true")
		}
		if len(m2.Reactions) != 1 || m2.Reactions[0].EmojiName != "+1" {
			t.Errorf("m2 reactions mismatch: %+v", m2.Reactions)
		}
	})

	t.Run("with_narrow_json", func(t *testing.T) {
		opts := GetMessagesOptions{
			NarrowJSON:     `[{"operator":"channel","operand":"general"}]`,
			ClientGravatar: true,
		}
		resp, err := client.GetMessages(ctx, opts)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(resp.Messages) != 2 {
			t.Errorf("got %d messages, want 2", len(resp.Messages))
		}
	})

	t.Run("bad_request", func(t *testing.T) {
		opts := GetMessagesOptions{
			Anchor: "invalid",
		}
		_, err := client.GetMessages(ctx, opts)
		if err == nil {
			t.Fatal("expected bad request error, got nil")
		}
		if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "Invalid anchor value") {
			t.Errorf("error = %q, want 400 with Invalid anchor value", err.Error())
		}
	})

	t.Run("get_messages_narrow_marshal_error", func(t *testing.T) {
		opts := GetMessagesOptions{
			Narrow: []NarrowOp{{Op: "custom", Operand: make(chan int)}},
		}
		_, err := client.GetMessages(ctx, opts)
		if err == nil {
			t.Fatal("expected error for unmarshalable narrow operand, got nil")
		}
	})
}

func TestGetStreamsAndSubscriptions(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/streams":
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"streams": []Stream{
					{
						StreamID:     615773,
						Name:         "general",
						Description:  "General discussions",
						IsWebPublic:  true,
						IsSubscribed: true,
					},
					{
						StreamID:    615774,
						Name:        "compiler",
						Description: "Go compiler development",
						IsWebPublic: true,
					},
				},
			})
		case "/api/v1/users/me/subscriptions":
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"subscriptions": []Subscription{
					{
						StreamID:    615773,
						Name:        "general",
						Description: "General discussions",
						Color:       "#e79ab5",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClientWithHTTP(&Config{
		Email: "a@b.com",
		Key:   "k",
		Site:  ts.URL,
	}, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("get_streams", func(t *testing.T) {
		streams, err := client.GetStreams(ctx, true)
		if err != nil {
			t.Fatalf("GetStreams: %v", err)
		}
		if len(streams) != 2 {
			t.Fatalf("got %d streams, want 2", len(streams))
		}
		if streams[0].Name != "general" || streams[1].Name != "compiler" {
			t.Errorf("unexpected streams: %+v", streams)
		}
	})

	t.Run("get_subscriptions", func(t *testing.T) {
		subs, err := client.GetSubscriptions(ctx)
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		if len(subs) != 1 || subs[0].Name != "general" {
			t.Errorf("unexpected subscriptions: %+v", subs)
		}
	})

	t.Run("get_stream_id_exact_and_case_insensitive", func(t *testing.T) {
		id, err := client.GetStreamID(ctx, "general")
		if err != nil {
			t.Fatalf("GetStreamID(general): %v", err)
		}
		if id != 615773 {
			t.Errorf("GetStreamID(general) = %d, want 615773", id)
		}

		idUpper, err := client.GetStreamID(ctx, "COMPILER")
		if err != nil {
			t.Fatalf("GetStreamID(COMPILER): %v", err)
		}
		if idUpper != 615774 {
			t.Errorf("GetStreamID(COMPILER) = %d, want 615774", idUpper)
		}

		_, err = client.GetStreamID(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent channel, got nil")
		}
		if !strings.Contains(err.Error(), "channel \"nonexistent\" not found") {
			t.Errorf("error = %q, want not found", err.Error())
		}
	})

	t.Run("get_stream_name", func(t *testing.T) {
		name, err := client.GetStreamName(ctx, 615773)
		if err != nil {
			t.Fatalf("GetStreamName(615773): %v", err)
		}
		if name != "general" {
			t.Errorf("GetStreamName(615773) = %q, want %q", name, "general")
		}

		_, err = client.GetStreamName(ctx, 999999)
		if err == nil {
			t.Fatal("expected error for nonexistent ID, got nil")
		}
	})
}

func TestGetStreamTopics(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/me/615773/topics" {
			json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"topics": []Topic{
					{Name: "closing stale watchflakes", MaxID: 619610075},
					{Name: "proposal review", MaxID: 619600120},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"result": "error",
			"msg":    "Stream does not exist",
			"code":   "STREAM_DOES_NOT_EXIST",
		})
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClientWithHTTP(&Config{
		Email: "a@b.com",
		Key:   "k",
		Site:  ts.URL,
	}, ts.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("topics_success", func(t *testing.T) {
		topics, err := client.GetStreamTopics(ctx, 615773)
		if err != nil {
			t.Fatalf("GetStreamTopics: %v", err)
		}
		if len(topics) != 2 {
			t.Fatalf("got %d topics, want 2", len(topics))
		}
		if topics[0].Name != "closing stale watchflakes" || topics[0].MaxID != 619610075 {
			t.Errorf("unexpected topic: %+v", topics[0])
		}
	})

	t.Run("topics_stream_not_found", func(t *testing.T) {
		_, err := client.GetStreamTopics(ctx, 999)
		if err == nil {
			t.Fatal("expected 404 error, got nil")
		}
		if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "Stream does not exist") {
			t.Errorf("error = %q, want 404 with Stream does not exist", err.Error())
		}
	})
}

func TestTargetToMessagesOptions(t *testing.T) {
	t.Parallel()

	t.Run("nil_target", func(t *testing.T) {
		_, err := TargetToMessagesOptions(nil)
		if err == nil {
			t.Fatal("expected error for nil target, got nil")
		}
	})

	t.Run("topic_target_with_anchor", func(t *testing.T) {
		target := &NarrowTarget{
			Channel:    "general",
			Topic:      "watchflakes",
			Anchor:     619610069,
			AnchorType: AnchorWith,
			NumBefore:  20,
			NumAfter:   20,
		}
		opts, err := TargetToMessagesOptions(target)
		if err != nil {
			t.Fatalf("TargetToMessagesOptions: %v", err)
		}
		if opts.Anchor != "619610069" {
			t.Errorf("opts.Anchor = %q, want 619610069", opts.Anchor)
		}
		if opts.NumBefore != 20 || opts.NumAfter != 20 {
			t.Errorf("opts before/after = %d/%d, want 20/20", opts.NumBefore, opts.NumAfter)
		}
		if !strings.Contains(opts.NarrowJSON, "watchflakes") {
			t.Errorf("opts.NarrowJSON = %q, missing watchflakes", opts.NarrowJSON)
		}
	})

	t.Run("default_num_before", func(t *testing.T) {
		target := &NarrowTarget{
			Channel: "general",
			Topic:   "watchflakes",
		}
		opts, err := TargetToMessagesOptions(target)
		if err != nil {
			t.Fatalf("TargetToMessagesOptions: %v", err)
		}
		if opts.NumBefore != 50 {
			t.Errorf("opts.NumBefore = %d, want 50", opts.NumBefore)
		}
	})

	t.Run("target_anchor_type", func(t *testing.T) {
		target := &NarrowTarget{
			AnchorType: AnchorFirstUnread,
		}
		opts, err := TargetToMessagesOptions(target)
		if err != nil {
			t.Fatalf("TargetToMessagesOptions: %v", err)
		}
		if opts.Anchor != "first_unread" {
			t.Errorf("opts.Anchor = %q, want first_unread", opts.Anchor)
		}
	})

	t.Run("target_narrow_json_error", func(t *testing.T) {
		target := &NarrowTarget{
			Ops: []NarrowOp{{Op: "custom", Operand: make(chan int)}},
		}
		_, err := TargetToMessagesOptions(target)
		if err == nil {
			t.Fatal("expected error for unmarshalable operand, got nil")
		}
	})
}

func TestClientErrorResponses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("non_json_502_bad_gateway", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("502 Bad Gateway: nginx"))
		}))
		defer ts.Close()

		client, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		_, err := client.GetMe(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "Bad Gateway") {
			t.Errorf("error = %q, want 502 Bad Gateway", err.Error())
		}
	})

	t.Run("200_ok_with_error_result", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"result": "error",
				"msg":    "Malformed request parameter",
				"code":   "BAD_REQUEST",
			})
		}))
		defer ts.Close()

		client, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		_, err := client.GetMe(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "Malformed request parameter") {
			t.Errorf("error = %q, want Malformed request parameter", err.Error())
		}
	})

	t.Run("malformed_json_in_200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("{invalid-json"))
		}))
		defer ts.Close()

		client, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		_, err := client.GetMe(ctx)
		if err == nil {
			t.Fatal("expected JSON decode error, got nil")
		}
	})

	t.Run("context_cancelled", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer ts.Close()

		client, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.GetMe(cancelCtx)
		if err == nil {
			t.Fatal("expected context cancelled error, got nil")
		}
	})

	t.Run("api_error_formatting", func(t *testing.T) {
		errWithCode := &APIError{StatusCode: 401, Result: "error", Msg: "Unauthorized", Code: "UNAUTHORIZED"}
		if errWithCode.Error() != "zulip api error (401 UNAUTHORIZED): Unauthorized" {
			t.Errorf("Error() = %q", errWithCode.Error())
		}

		errNoCode := &APIError{StatusCode: 404, Result: "error", Msg: "Not Found"}
		if errNoCode.Error() != "zulip api error (404): Not Found" {
			t.Errorf("Error() = %q", errNoCode.Error())
		}

		errNoStatus := &APIError{Result: "error", Msg: "Generic error"}
		if errNoStatus.Error() != "zulip api error: Generic error" {
			t.Errorf("Error() = %q", errNoStatus.Error())
		}
	})

	t.Run("channel_name_non_string", func(t *testing.T) {
		m := Message{DisplayRecipient: []any{"user1", "user2"}}
		if got := m.ChannelName(); got != "" {
			t.Errorf("ChannelName() = %q, want empty string", got)
		}
	})

	t.Run("server_errors_in_endpoints", func(t *testing.T) {
		errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 internal server error"))
		}))
		defer errServer.Close()

		errClient, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: errServer.URL}, errServer.Client())

		if _, err := errClient.GetServerSettings(ctx); err == nil {
			t.Error("GetServerSettings expected error, got nil")
		}
		if _, err := errClient.GetSubscriptions(ctx); err == nil {
			t.Error("GetSubscriptions expected error, got nil")
		}
		if _, err := errClient.GetStreamID(ctx, "general"); err == nil {
			t.Error("GetStreamID expected error, got nil")
		}
		if _, err := errClient.GetStreamName(ctx, 123); err == nil {
			t.Error("GetStreamName expected error, got nil")
		}
	})

	t.Run("endpoint_without_leading_slash", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(User{UserID: 1, Email: "a@b.com"})
		}))
		defer ts.Close()

		c, _ := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		var user User
		err := c.getJSON(ctx, "api/v1/users/me", nil, &user)
		if err != nil {
			t.Fatalf("getJSON without leading slash: %v", err)
		}
	})
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSynctestClientConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
		}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				recorder := httptest.NewRecorder()
				switch req.URL.Path {
				case "/api/v1/users/me":
					json.NewEncoder(recorder).Encode(User{UserID: 1, Email: "sync@example.com", FullName: "Sync User"})
				case "/api/v1/server_settings":
					json.NewEncoder(recorder).Encode(ServerSettings{ZulipVersion: "9.0"})
				}
				return recorder.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		var wg sync.WaitGroup
		users := make([]*User, 10)
		errs := make([]error, 10)

		for i := range 10 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				u, err := client.GetMe(context.Background())
				users[idx] = u
				errs[idx] = err
			}(i)
		}

		synctest.Wait()
		wg.Wait()

		for i := range 10 {
			if errs[i] != nil {
				t.Fatalf("worker %d failed: %v", i, errs[i])
			}
			if users[i].Email != "sync@example.com" {
				t.Errorf("worker %d got email %q, want sync@example.com", i, users[i].Email)
			}
		}
	})
}

func TestGetMessagesInRange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	t.Run("until_before_since_error", func(t *testing.T) {
		cfg := &Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}
		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		since := now
		until := now.Add(-time.Hour)
		_, err = client.GetMessagesInRange(context.Background(), GetMessagesOptions{}, since, until, 10)
		if err == nil || !strings.Contains(err.Error(), "-until time must be after -since time") {
			t.Fatalf("expected until before since error, got: %v", err)
		}
	})

	t.Run("first_unread_anchor", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(MessagesResponse{
				Result: "success",
				Messages: []Message{
					{ID: 1, Timestamp: now.Add(-2 * time.Hour).Unix(), Content: "old"},
					{ID: 2, Timestamp: now.Add(-30 * time.Minute).Unix(), Content: "in range"},
					{ID: 3, Timestamp: now.Add(time.Hour).Unix(), Content: "future"},
				},
			})
		}))
		defer ts.Close()

		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: ts.URL}, ts.Client())
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		resp, err := client.GetMessagesInRange(context.Background(), GetMessagesOptions{
			Anchor: string(AnchorFirstUnread),
		}, now.Add(-time.Hour), now, 10)
		if err != nil {
			t.Fatalf("GetMessagesInRange: %v", err)
		}
		if len(resp.Messages) != 1 || resp.Messages[0].ID != 2 {
			t.Errorf("unexpected messages: %+v", resp.Messages)
		}
	})

	t.Run("first_unread_error", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network failure")
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}
		_, err = client.GetMessagesInRange(context.Background(), GetMessagesOptions{
			Anchor: string(AnchorFirstUnread),
		}, now.Add(-time.Hour), now, 10)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("multi_page_backward_pagination", func(t *testing.T) {
		callCount := 0
		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				callCount++
				anchor := req.URL.Query().Get("anchor")
				recorder := httptest.NewRecorder()
				recorder.Header().Set("Content-Type", "application/json")

				var resp MessagesResponse
				resp.Result = "success"

				switch anchor {
				case "newest":
					// Page 1: messages 10 to 12 (latest)
					resp.Messages = []Message{
						{ID: 10, Timestamp: now.Add(-10 * time.Minute).Unix(), Content: "m10"},
						{ID: 11, Timestamp: now.Add(-5 * time.Minute).Unix(), Content: "m11"},
						{ID: 12, Timestamp: now.Add(-2 * time.Minute).Unix(), Content: "m12"},
					}
					resp.FoundNewest = true
				case "10":
					// Page 2: messages 7 to 10 (anchor 10 included)
					resp.Messages = []Message{
						{ID: 7, Timestamp: now.Add(-30 * time.Minute).Unix(), Content: "m7"},
						{ID: 8, Timestamp: now.Add(-20 * time.Minute).Unix(), Content: "m8"},
						{ID: 9, Timestamp: now.Add(-15 * time.Minute).Unix(), Content: "m9"},
						{ID: 10, Timestamp: now.Add(-10 * time.Minute).Unix(), Content: "m10"},
					}
				case "7":
					// Page 3: messages 4 to 7 (anchor 7 included, 4 is older than sinceTime)
					resp.Messages = []Message{
						{ID: 4, Timestamp: now.Add(-2 * time.Hour).Unix(), Content: "m4"},
						{ID: 5, Timestamp: now.Add(-50 * time.Minute).Unix(), Content: "m5"},
						{ID: 6, Timestamp: now.Add(-40 * time.Minute).Unix(), Content: "m6"},
						{ID: 7, Timestamp: now.Add(-30 * time.Minute).Unix(), Content: "m7"},
					}
					resp.FoundOldest = true
				}

				json.NewEncoder(recorder).Encode(resp)
				return recorder.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		// Since 1h ago, until 3m ago (m12 should be filtered out by until, m4 filtered out by since)
		since := now.Add(-time.Hour)
		until := now.Add(-3 * time.Minute)
		resp, err := client.GetMessagesInRange(context.Background(), GetMessagesOptions{}, since, until, 10)
		if err != nil {
			t.Fatalf("GetMessagesInRange: %v", err)
		}

		if callCount != 3 {
			t.Errorf("callCount = %d, want 3", callCount)
		}

		// Expect m5, m6, m7, m8, m9, m10, m11 (7 messages)
		if len(resp.Messages) != 7 {
			t.Fatalf("got %d messages, want 7: %+v", len(resp.Messages), resp.Messages)
		}
		if resp.Messages[0].ID != 5 || resp.Messages[len(resp.Messages)-1].ID != 11 {
			t.Errorf("unexpected message IDs: %d to %d", resp.Messages[0].ID, resp.Messages[len(resp.Messages)-1].ID)
		}
		if !resp.FoundOldest {
			t.Error("expected FoundOldest = true")
		}
	})

	t.Run("empty_messages_page", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				recorder := httptest.NewRecorder()
				recorder.Header().Set("Content-Type", "application/json")
				json.NewEncoder(recorder).Encode(MessagesResponse{
					Result:   "success",
					Messages: []Message{},
				})
				return recorder.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		resp, err := client.GetMessagesInRange(context.Background(), GetMessagesOptions{}, now.Add(-time.Hour), time.Time{}, 0)
		if err != nil {
			t.Fatalf("GetMessagesInRange: %v", err)
		}
		if len(resp.Messages) != 0 || !resp.FoundOldest {
			t.Errorf("unexpected resp: %+v", resp)
		}
	})

	t.Run("duplicate_messages_page_break", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				recorder := httptest.NewRecorder()
				recorder.Header().Set("Content-Type", "application/json")
				// Always return message 10
				json.NewEncoder(recorder).Encode(MessagesResponse{
					Result: "success",
					Messages: []Message{
						{ID: 10, Timestamp: now.Unix()},
					},
				})
				return recorder.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		resp, err := client.GetMessagesInRange(context.Background(), GetMessagesOptions{}, now.Add(-time.Hour), time.Time{}, 10)
		if err != nil {
			t.Fatalf("GetMessagesInRange: %v", err)
		}
		if len(resp.Messages) != 1 {
			t.Errorf("got %d messages, want 1", len(resp.Messages))
		}
	})

	t.Run("api_error_in_pagination", func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{Email: "a@b.com", Key: "k", Site: "https://example.com"}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("server down")
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		_, err = client.GetMessagesInRange(context.Background(), GetMessagesOptions{}, now.Add(-time.Hour), time.Time{}, 10)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
