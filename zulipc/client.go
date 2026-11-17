// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// APIError represents an error response returned by the Zulip REST API.
type APIError struct {
	StatusCode int    `json:"-"`
	Result     string `json:"result"`
	Msg        string `json:"msg"`
	Code       string `json:"code,omitempty"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("zulip api error (%d %s): %s", e.StatusCode, e.Code, e.Msg)
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("zulip api error (%d): %s", e.StatusCode, e.Msg)
	}
	return fmt.Sprintf("zulip api error: %s", e.Msg)
}

// User represents a Zulip user profile from /api/v1/users/me.
type User struct {
	UserID            int64  `json:"user_id"`
	Email             string `json:"email"`
	FullName          string `json:"full_name"`
	IsAdmin           bool   `json:"is_admin"`
	IsOwner           bool   `json:"is_owner"`
	IsGuest           bool   `json:"is_guest"`
	IsBot             bool   `json:"is_bot"`
	Role              int    `json:"role"`
	Timezone          string `json:"timezone"`
	AvatarURL         string `json:"avatar_url"`
	ZulipVersion      string `json:"zulip_version,omitempty"`
	ZulipFeatureLevel int    `json:"zulip_feature_level,omitempty"`
}

// RoleName returns a human-readable role name.
func (u *User) RoleName() string {
	switch u.Role {
	case 100:
		return "Owner"
	case 200:
		return "Administrator"
	case 300:
		return "Moderator"
	case 400:
		return "Member"
	case 600:
		return "Guest"
	default:
		if u.IsOwner {
			return "Owner"
		}
		if u.IsAdmin {
			return "Administrator"
		}
		if u.IsGuest {
			return "Guest"
		}
		return "Member"
	}
}

// ServerSettings represents Zulip server settings from /api/v1/server_settings.
type ServerSettings struct {
	ZulipVersion      string `json:"zulip_version"`
	ZulipFeatureLevel int    `json:"zulip_feature_level"`
	RealmName         string `json:"realm_name,omitempty"`
	RealmURI          string `json:"realm_uri,omitempty"`
	RealmDescription  string `json:"realm_description,omitempty"`
}

// Stream represents a Zulip stream (channel) from /api/v1/streams.
type Stream struct {
	StreamID             int64  `json:"stream_id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	RenderedDescription  string `json:"rendered_description,omitempty"`
	InviteOnly           bool   `json:"invite_only"`
	IsWebPublic          bool   `json:"is_web_public"`
	IsSubscribed         bool   `json:"is_subscribed,omitempty"`
	FirstMessageID       int64  `json:"first_message_id,omitempty"`
	StreamPostPolicy     int    `json:"stream_post_policy,omitempty"`
	MessageRetentionDays *int   `json:"message_retention_days,omitempty"`
}

// Subscription represents a subscribed stream from /api/v1/users/me/subscriptions.
type Subscription struct {
	StreamID    int64  `json:"stream_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color,omitempty"`
	IsMuted     bool   `json:"is_muted,omitempty"`
	PinToTop    bool   `json:"pin_to_top,omitempty"`
	InviteOnly  bool   `json:"invite_only"`
	IsWebPublic bool   `json:"is_web_public"`
}

// Topic represents a topic from /api/v1/users/me/{stream_id}/topics.
type Topic struct {
	Name  string `json:"name"`
	MaxID int64  `json:"max_id"`
}

// Reaction represents a reaction on a message.
type Reaction struct {
	EmojiName    string `json:"emoji_name"`
	EmojiCode    string `json:"emoji_code"`
	ReactionType string `json:"reaction_type"`
	UserID       int64  `json:"user_id"`
}

// Message represents a Zulip message from /api/v1/messages.
type Message struct {
	ID                int64      `json:"id"`
	SenderID          int64      `json:"sender_id"`
	SenderFullName    string     `json:"sender_full_name"`
	SenderEmail       string     `json:"sender_email"`
	RecipientID       int64      `json:"recipient_id,omitempty"`
	Timestamp         int64      `json:"timestamp"` // Unix timestamp in seconds
	Client            string     `json:"client,omitempty"`
	Content           string     `json:"content"`
	ContentType       string     `json:"content_type,omitempty"`
	Type              string     `json:"type"` // "stream", "channel", "private"
	StreamID          int64      `json:"stream_id,omitempty"`
	DisplayRecipient  any        `json:"display_recipient,omitempty"`
	Subject           string     `json:"subject,omitempty"`
	Topic             string     `json:"topic,omitempty"`
	Flags             []string   `json:"flags,omitempty"`
	Reactions         []Reaction `json:"reactions,omitempty"`
	LastEditTimestamp int64      `json:"last_edit_timestamp,omitempty"`
}

// TopicName returns Topic if non-empty, otherwise Subject.
func (m *Message) TopicName() string {
	if m.Topic != "" {
		return m.Topic
	}
	return m.Subject
}

// ChannelName returns the channel/stream name from DisplayRecipient if it is a string.
func (m *Message) ChannelName() string {
	if s, ok := m.DisplayRecipient.(string); ok {
		return s
	}
	return ""
}

// IsUnread reports whether the message has not been marked as read.
func (m *Message) IsUnread() bool {
	return !slices.Contains(m.Flags, "read")
}

// IsStarred reports whether the message has been starred.
func (m *Message) IsStarred() bool {
	return slices.Contains(m.Flags, "starred")
}

// GetMessagesOptions defines parameters for fetching messages via /api/v1/messages.
type GetMessagesOptions struct {
	Anchor         string     `json:"anchor,omitempty"`
	NumBefore      int        `json:"num_before"`
	NumAfter       int        `json:"num_after"`
	Narrow         []NarrowOp `json:"narrow,omitempty"`
	NarrowJSON     string     `json:"-"`
	ApplyMarkdown  *bool      `json:"apply_markdown,omitempty"`
	ClientGravatar bool       `json:"client_gravatar,omitempty"`
}

// MessagesResponse represents the API response from /api/v1/messages.
type MessagesResponse struct {
	Result         string    `json:"result"`
	Msg            string    `json:"msg"`
	Messages       []Message `json:"messages"`
	FoundAnchor    bool      `json:"found_anchor"`
	FoundOldest    bool      `json:"found_oldest"`
	FoundNewest    bool      `json:"found_newest"`
	HistoryLimited bool      `json:"history_limited"`
	Anchor         int64     `json:"anchor,omitempty"`
}

// Client represents a Zulip REST API client.
type Client struct {
	baseURL    string
	email      string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Client configured from the given Config.
func NewClient(cfg *Config) (*Client, error) {
	return NewClientWithHTTP(cfg, nil)
}

// NewClientWithHTTP creates a new Client configured with a custom http.Client.
func NewClientWithHTTP(cfg *Config, httpClient *http.Client) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("nil config provided")
	}
	if cfg.Email == "" {
		return nil, errors.New("missing email in config")
	}
	if cfg.Key == "" {
		return nil, errors.New("missing key in config")
	}
	if cfg.Site == "" {
		return nil, errors.New("missing site in config")
	}

	baseURL := strings.TrimRight(cfg.Site, "/")

	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.Insecure {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	}

	return &Client{
		baseURL:    baseURL,
		email:      cfg.Email,
		apiKey:     cfg.Key,
		httpClient: httpClient,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, query url.Values, body io.Reader) (*http.Response, error) {
	fullURL := c.baseURL
	if strings.HasPrefix(endpoint, "/") {
		fullURL += endpoint
	} else {
		fullURL += "/" + endpoint
	}

	if len(query) > 0 {
		u, err := url.Parse(fullURL)
		if err != nil {
			return nil, fmt.Errorf("parsing request URL %q: %w", fullURL, err)
		}
		u.RawQuery = query.Encode()
		fullURL = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	req.SetBasicAuth(c.email, c.apiKey)
	req.Header.Set("User-Agent", "zulipc/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, query url.Values, target any) error {
	resp, err := c.doRequest(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr APIError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil && apiErr.Result == "error" {
			apiErr.StatusCode = resp.StatusCode
			return &apiErr
		}
		return fmt.Errorf("zulip server error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// Check if Zulip returned a 200 OK with {"result": "error", "msg": "..."}
	var checkErr struct {
		Result string `json:"result"`
		Msg    string `json:"msg"`
		Code   string `json:"code,omitempty"`
	}
	if err := json.Unmarshal(respBody, &checkErr); err == nil && checkErr.Result == "error" {
		return &APIError{
			StatusCode: resp.StatusCode,
			Result:     checkErr.Result,
			Msg:        checkErr.Msg,
			Code:       checkErr.Code,
		}
	}

	if err := json.Unmarshal(respBody, target); err != nil {
		return fmt.Errorf("decoding JSON response: %w", err)
	}
	return nil
}

// GetMe retrieves the current user's profile and organization details from /api/v1/users/me.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var user User
	if err := c.getJSON(ctx, "/api/v1/users/me", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetServerSettings retrieves the Zulip server settings and version from /api/v1/server_settings.
func (c *Client) GetServerSettings(ctx context.Context) (*ServerSettings, error) {
	var settings ServerSettings
	if err := c.getJSON(ctx, "/api/v1/server_settings", nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// GetMessages retrieves messages matching the given options from /api/v1/messages.
func (c *Client) GetMessages(ctx context.Context, opts GetMessagesOptions) (*MessagesResponse, error) {
	q := make(url.Values)

	if opts.Anchor != "" {
		q.Set("anchor", opts.Anchor)
	}
	q.Set("num_before", strconv.Itoa(opts.NumBefore))
	q.Set("num_after", strconv.Itoa(opts.NumAfter))

	if opts.NarrowJSON != "" {
		q.Set("narrow", opts.NarrowJSON)
	} else if len(opts.Narrow) > 0 {
		b, err := json.Marshal(opts.Narrow)
		if err != nil {
			return nil, fmt.Errorf("marshaling narrow filter: %w", err)
		}
		q.Set("narrow", string(b))
	}

	if opts.ApplyMarkdown != nil {
		q.Set("apply_markdown", strconv.FormatBool(*opts.ApplyMarkdown))
	} else {
		q.Set("apply_markdown", "false")
	}

	if opts.ClientGravatar {
		q.Set("client_gravatar", "true")
	}

	var resp MessagesResponse
	if err := c.getJSON(ctx, "/api/v1/messages", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetMessagesInRange retrieves messages in a time range [sinceTime, untilTime] with backward pagination.
// If both sinceTime and untilTime are zero, it falls back to a single GetMessages call with baseOpts.
func (c *Client) GetMessagesInRange(ctx context.Context, baseOpts GetMessagesOptions, sinceTime, untilTime time.Time, limit int) (*MessagesResponse, error) {
	if sinceTime.IsZero() && untilTime.IsZero() {
		return c.GetMessages(ctx, baseOpts)
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && untilTime.Before(sinceTime) {
		return nil, errors.New("-until time must be after -since time")
	}

	if limit <= 0 {
		limit = 100
	}

	// Handle first_unread anchor directly (forward reading)
	if baseOpts.Anchor == string(AnchorFirstUnread) {
		resp, err := c.GetMessages(ctx, baseOpts)
		if err != nil {
			return nil, err
		}
		var filtered []Message
		for _, m := range resp.Messages {
			if !sinceTime.IsZero() && m.Timestamp < sinceTime.Unix() {
				continue
			}
			if !untilTime.IsZero() && m.Timestamp > untilTime.Unix() {
				continue
			}
			filtered = append(filtered, m)
		}
		resp.Messages = filtered
		return resp, nil
	}

	var (
		allMessages []Message
		seenIDs     = make(map[int64]bool)
		anchor      = baseOpts.Anchor
		foundOldest bool
		foundNewest bool
	)
	if anchor == "" {
		anchor = string(AnchorNewest)
	}

	maxPages := 20
	for page := 0; page < maxPages; page++ {
		batchSize := 100
		if limit < batchSize && sinceTime.IsZero() {
			batchSize = limit
		}

		reqOpts := baseOpts
		reqOpts.Anchor = anchor
		reqOpts.NumBefore = batchSize
		reqOpts.NumAfter = 0

		resp, err := c.GetMessages(ctx, reqOpts)
		if err != nil {
			return nil, err
		}
		if resp.FoundNewest {
			foundNewest = true
		}
		if resp.FoundOldest {
			foundOldest = true
		}

		if len(resp.Messages) == 0 {
			foundOldest = true
			break
		}

		var newInBatch []Message
		for _, m := range resp.Messages {
			if seenIDs[m.ID] {
				continue
			}
			seenIDs[m.ID] = true
			newInBatch = append(newInBatch, m)
		}

		if len(newInBatch) == 0 {
			foundOldest = true
			break
		}

		oldestInBatch := newInBatch[0]
		reachedBeforeSince := false
		if !sinceTime.IsZero() && oldestInBatch.Timestamp < sinceTime.Unix() {
			reachedBeforeSince = true
		}

		for _, m := range newInBatch {
			if !sinceTime.IsZero() && m.Timestamp < sinceTime.Unix() {
				continue
			}
			if !untilTime.IsZero() && m.Timestamp > untilTime.Unix() {
				continue
			}
			allMessages = append(allMessages, m)
		}

		if resp.FoundOldest || reachedBeforeSince || len(allMessages) >= limit {
			if resp.FoundOldest || reachedBeforeSince {
				foundOldest = true
			}
			break
		}

		anchor = strconv.FormatInt(oldestInBatch.ID, 10)
	}

	slices.SortFunc(allMessages, func(a, b Message) int {
		if a.Timestamp != b.Timestamp {
			if a.Timestamp < b.Timestamp {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		} else if a.ID > b.ID {
			return 1
		}
		return 0
	})

	if len(allMessages) > limit {
		allMessages = allMessages[len(allMessages)-limit:]
	}

	return &MessagesResponse{
		Result:      "success",
		Messages:    allMessages,
		FoundAnchor: true,
		FoundOldest: foundOldest,
		FoundNewest: foundNewest,
	}, nil
}

// GetStreams retrieves the list of accessible streams from /api/v1/streams.
func (c *Client) GetStreams(ctx context.Context, includeAllPublic bool) ([]Stream, error) {
	q := make(url.Values)
	q.Set("include_public", "true")
	q.Set("include_subscribed", "true")
	q.Set("include_all_active", strconv.FormatBool(includeAllPublic))
	q.Set("include_default", "true")

	var result struct {
		Result  string   `json:"result"`
		Msg     string   `json:"msg"`
		Streams []Stream `json:"streams"`
	}
	if err := c.getJSON(ctx, "/api/v1/streams", q, &result); err != nil {
		return nil, err
	}
	return result.Streams, nil
}

// GetSubscriptions retrieves the user's subscribed streams from /api/v1/users/me/subscriptions.
func (c *Client) GetSubscriptions(ctx context.Context) ([]Subscription, error) {
	var result struct {
		Result        string         `json:"result"`
		Msg           string         `json:"msg"`
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := c.getJSON(ctx, "/api/v1/users/me/subscriptions", nil, &result); err != nil {
		return nil, err
	}
	return result.Subscriptions, nil
}

// GetStreamTopics retrieves topics for a stream from /api/v1/users/me/{stream_id}/topics.
func (c *Client) GetStreamTopics(ctx context.Context, streamID int64) ([]Topic, error) {
	endpoint := fmt.Sprintf("/api/v1/users/me/%d/topics", streamID)
	var result struct {
		Result string  `json:"result"`
		Msg    string  `json:"msg"`
		Topics []Topic `json:"topics"`
	}
	if err := c.getJSON(ctx, endpoint, nil, &result); err != nil {
		return nil, err
	}
	return result.Topics, nil
}

// GetStreamID resolves a stream name to its stream ID.
func (c *Client) GetStreamID(ctx context.Context, streamName string) (int64, error) {
	streams, err := c.GetStreams(ctx, true)
	if err != nil {
		return 0, err
	}

	// Exact match first
	for _, s := range streams {
		if s.Name == streamName {
			return s.StreamID, nil
		}
	}

	// Case-insensitive match
	lowerName := strings.ToLower(streamName)
	for _, s := range streams {
		if strings.ToLower(s.Name) == lowerName {
			return s.StreamID, nil
		}
	}

	return 0, fmt.Errorf("channel %q not found", streamName)
}

// GetStreamName resolves a stream ID to its stream name.
func (c *Client) GetStreamName(ctx context.Context, streamID int64) (string, error) {
	streams, err := c.GetStreams(ctx, true)
	if err != nil {
		return "", err
	}

	for _, s := range streams {
		if s.StreamID == streamID {
			return s.Name, nil
		}
	}

	return "", fmt.Errorf("channel ID %d not found", streamID)
}

// TargetToMessagesOptions converts a NarrowTarget into GetMessagesOptions.
func TargetToMessagesOptions(target *NarrowTarget) (GetMessagesOptions, error) {
	if target == nil {
		return GetMessagesOptions{}, errors.New("nil target provided")
	}

	ops := FilterAPINarrowOps(target.NarrowOps())
	var narrowJSON string
	if len(ops) > 0 {
		b, err := json.Marshal(ops)
		if err != nil {
			return GetMessagesOptions{}, err
		}
		narrowJSON = string(b)
	} else {
		narrowJSON = "[]"
	}

	anchor := ""
	if target.Anchor > 0 {
		anchor = strconv.FormatInt(target.Anchor, 10)
	} else if target.AnchorType != "" {
		anchor = string(target.AnchorType)
	}

	numBefore := target.NumBefore
	numAfter := target.NumAfter
	if numBefore == 0 && numAfter == 0 && target.Anchor <= 0 {
		numBefore = 50
	}

	return GetMessagesOptions{
		Anchor:     anchor,
		NumBefore:  numBefore,
		NumAfter:   numAfter,
		NarrowJSON: narrowJSON,
	}, nil
}
