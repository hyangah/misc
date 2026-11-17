// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// TopicOptions holds options for the topics subcommand.
type TopicOptions struct {
	Channel string
	Filter  string
	AsJSON  bool
	Limit   int
}

// TopicResult represents the structured JSON output of the topics command.
type TopicResult struct {
	Channel   string  `json:"channel"`
	ChannelID int64   `json:"channel_id"`
	Total     int     `json:"total"`
	Shown     int     `json:"shown"`
	HasMore   bool    `json:"has_more"`
	Topics    []Topic `json:"topics"`
}

// ChannelOptions holds options for the channels subcommand.
type ChannelOptions struct {
	All         bool
	Description bool
	AsJSON      bool
	Limit       int
}

// ChannelItem represents a channel in channels command output.
type ChannelItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	Subscribed  bool   `json:"subscribed"`
	UnreadCount int    `json:"unread_count"`
}

// ChannelResult represents the structured JSON output of the channels command.
type ChannelResult struct {
	Total    int           `json:"total"`
	Shown    int           `json:"shown"`
	HasMore  bool          `json:"has_more"`
	Channels []ChannelItem `json:"channels"`
}

// ResolveChannel resolves a channel identifier (name, ID, URL, or #channel) to its stream ID and canonical name.
func ResolveChannel(ctx context.Context, client *Client, channelRef string) (int64, string, error) {
	channelRef = strings.TrimSpace(channelRef)
	if channelRef == "" {
		return 0, "", errors.New("empty channel reference")
	}

	// 1. Handle URL or hash fragment
	if strings.HasPrefix(channelRef, "http://") || strings.HasPrefix(channelRef, "https://") || strings.HasPrefix(channelRef, "#narrow/") {
		target, err := ParseURL(channelRef)
		if err == nil {
			if target.ChannelID > 0 {
				name, err := client.GetStreamName(ctx, target.ChannelID)
				if err == nil {
					return target.ChannelID, name, nil
				}
				if target.Channel != "" {
					return target.ChannelID, target.Channel, nil
				}
			}
			if target.Channel != "" {
				channelRef = target.Channel
			}
		}
	}

	// 2. Strip leading '#'
	channelRef = strings.TrimPrefix(channelRef, "#")

	// 3. Fetch all streams from server
	streams, err := client.GetStreams(ctx, true)
	if err != nil {
		return 0, "", fmt.Errorf("fetching channels: %w", err)
	}

	// Exact match by name
	for _, s := range streams {
		if s.Name == channelRef {
			return s.StreamID, s.Name, nil
		}
	}

	// Match by stream ID if channelRef is an integer
	if id, err := strconv.ParseInt(channelRef, 10, 64); err == nil && id > 0 {
		for _, s := range streams {
			if s.StreamID == id {
				return s.StreamID, s.Name, nil
			}
		}
	}

	// Case-insensitive match by name
	lower := strings.ToLower(channelRef)
	for _, s := range streams {
		if strings.ToLower(s.Name) == lower {
			return s.StreamID, s.Name, nil
		}
	}

	// Handle <id>-<name> slug format if present (e.g. 615773-general)
	if idx := strings.Index(channelRef, "-"); idx > 0 {
		if id, err := strconv.ParseInt(channelRef[:idx], 10, 64); err == nil && id > 0 {
			for _, s := range streams {
				if s.StreamID == id {
					return s.StreamID, s.Name, nil
				}
			}
		}
	}

	return 0, "", fmt.Errorf("channel %q not found", channelRef)
}

// FilterTopics filters topics matching a case-insensitive substring query.
func FilterTopics(topics []Topic, filter string) []Topic {
	if filter == "" {
		return topics
	}
	lowerFilter := strings.ToLower(filter)
	var matched []Topic
	for _, t := range topics {
		if strings.Contains(strings.ToLower(t.Name), lowerFilter) {
			matched = append(matched, t)
		}
	}
	return matched
}

// ResolveTopic finds a topic in a stream by exact match or substring match.
func ResolveTopic(ctx context.Context, client *Client, streamID int64, topicQuery string) (string, error) {
	topicQuery = strings.TrimSpace(topicQuery)
	if topicQuery == "" {
		return "", errors.New("empty topic query")
	}
	topics, err := client.GetStreamTopics(ctx, streamID)
	if err != nil {
		return "", fmt.Errorf("fetching topics: %w", err)
	}

	// 1. Exact match
	for _, t := range topics {
		if t.Name == topicQuery {
			return t.Name, nil
		}
	}

	// 2. Case-insensitive exact match
	lower := strings.ToLower(topicQuery)
	for _, t := range topics {
		if strings.ToLower(t.Name) == lower {
			return t.Name, nil
		}
	}

	// 3. Substring match
	matched := FilterTopics(topics, topicQuery)
	if len(matched) > 0 {
		return matched[0].Name, nil
	}

	return "", fmt.Errorf("topic %q not found", topicQuery)
}

// runTopics executes the topics command logic with the given options and writes output to w.
func runTopics(ctx context.Context, client *Client, opts TopicOptions, w io.Writer) error {
	if strings.TrimSpace(opts.Channel) == "" {
		return errors.New("missing channel name or ID")
	}
	if opts.Limit <= 0 {
		return errors.New("-n limit must be greater than 0")
	}

	streamID, streamName, err := ResolveChannel(ctx, client, opts.Channel)
	if err != nil {
		return err
	}

	allTopics, err := client.GetStreamTopics(ctx, streamID)
	if err != nil {
		return fmt.Errorf("fetching topics for channel %q: %w", streamName, err)
	}

	filtered := allTopics
	if opts.Filter != "" {
		filtered = FilterTopics(allTopics, opts.Filter)
	}

	total := len(filtered)
	topics := filtered
	hasMore := false
	if len(topics) > opts.Limit {
		topics = topics[:opts.Limit]
		hasMore = true
	}

	if opts.AsJSON {
		if topics == nil {
			topics = []Topic{}
		}
		res := TopicResult{
			Channel:   streamName,
			ChannelID: streamID,
			Total:     total,
			Shown:     len(topics),
			HasMore:   hasMore,
			Topics:    topics,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	if len(topics) == 0 {
		if opts.Filter != "" {
			fmt.Fprintf(w, "(no topics matching %q in %s out of %d total topics)\n", opts.Filter, streamName, len(allTopics))
		} else {
			fmt.Fprintf(w, "(no topics found in %s)\n", streamName)
		}
		return nil
	}

	for i, t := range topics {
		fmt.Fprintf(w, "[%d] %s\n", i+1, t.Name)
	}

	fmt.Fprintln(w)
	if hasMore {
		if opts.Filter != "" {
			fmt.Fprintf(w, "Showing %d of %d matching topics in %s (%d total topics in channel). Use -n %d to display more.\n", len(topics), total, streamName, len(allTopics), opts.Limit*2)
		} else {
			fmt.Fprintf(w, "Showing %d of %d topics in %s. Use -n %d to display more.\n", len(topics), total, streamName, opts.Limit*2)
		}
	} else {
		if opts.Filter != "" {
			fmt.Fprintf(w, "Showing all %d matching topics in %s (%d total topics in channel).\n", len(topics), streamName, len(allTopics))
		} else {
			fmt.Fprintf(w, "Showing all %d topics in %s.\n", len(topics), streamName)
		}
	}
	return nil
}

// runChannels executes the channels / streams command logic with the given options and writes output to w.
func runChannels(ctx context.Context, client *Client, opts ChannelOptions, w io.Writer) error {
	// Fetch unread messages to compute unread counts per stream.
	unreadCounts := make(map[int64]int)
	if unreadResp, err := client.GetMessages(ctx, GetMessagesOptions{
		Anchor:    string(AnchorNewest),
		NumBefore: 1000,
		Narrow:    []NarrowOp{{Op: "is", Operand: "unread"}},
	}); err == nil && unreadResp != nil {
		for _, msg := range unreadResp.Messages {
			if msg.StreamID > 0 {
				unreadCounts[msg.StreamID]++
			}
		}
	}

	var items []ChannelItem

	if opts.All {
		subMap := make(map[int64]bool)
		if subs, err := client.GetSubscriptions(ctx); err == nil {
			for _, sub := range subs {
				subMap[sub.StreamID] = true
			}
		}

		streams, err := client.GetStreams(ctx, true)
		if err != nil {
			return fmt.Errorf("fetching channels: %w", err)
		}
		items = make([]ChannelItem, len(streams))
		for i, s := range streams {
			isSub := subMap[s.StreamID] || s.IsSubscribed
			items[i] = ChannelItem{
				ID:          s.StreamID,
				Name:        s.Name,
				Description: s.Description,
				Private:     s.InviteOnly,
				Subscribed:  isSub,
				UnreadCount: unreadCounts[s.StreamID],
			}
		}
	} else {
		subs, err := client.GetSubscriptions(ctx)
		if err != nil {
			return fmt.Errorf("fetching subscribed channels: %w", err)
		}
		items = make([]ChannelItem, len(subs))
		for i, s := range subs {
			items[i] = ChannelItem{
				ID:          s.StreamID,
				Name:        s.Name,
				Description: s.Description,
				Private:     s.InviteOnly,
				Subscribed:  true,
				UnreadCount: unreadCounts[s.StreamID],
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	total := len(items)
	shownItems := items
	hasMore := false
	if opts.Limit > 0 && len(shownItems) > opts.Limit {
		shownItems = shownItems[:opts.Limit]
		hasMore = true
	}

	if opts.AsJSON {
		if shownItems == nil {
			shownItems = []ChannelItem{}
		}
		res := ChannelResult{
			Total:    total,
			Shown:    len(shownItems),
			HasMore:  hasMore,
			Channels: shownItems,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	if len(shownItems) == 0 {
		fmt.Fprintln(w, "(no channels found)")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if opts.Description {
		fmt.Fprintln(tw, "NAME\tACCESS\tSTATUS\tUNREAD\tDESCRIPTION")
		for _, ch := range shownItems {
			access := "public"
			if ch.Private {
				access = "private"
			}
			status := "-"
			if ch.Subscribed {
				status = "subscribed"
			}
			unread := "-"
			if ch.UnreadCount > 0 {
				unread = fmt.Sprintf("%d unread", ch.UnreadCount)
			}
			desc := ch.Description
			if desc == "" {
				desc = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", ch.Name, access, status, unread, desc)
		}
	} else {
		fmt.Fprintln(tw, "NAME\tACCESS\tSTATUS\tUNREAD")
		for _, ch := range shownItems {
			access := "public"
			if ch.Private {
				access = "private"
			}
			status := "-"
			if ch.Subscribed {
				status = "subscribed"
			}
			unread := "-"
			if ch.UnreadCount > 0 {
				unread = fmt.Sprintf("%d unread", ch.UnreadCount)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ch.Name, access, status, unread)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	if hasMore {
		fmt.Fprintf(w, "Showing %d of %d channels. Use -n %d to display more.\n", len(shownItems), total, opts.Limit*2)
	} else if opts.All {
		fmt.Fprintf(w, "Showing all %d public channels.\n", total)
	} else {
		fmt.Fprintf(w, "Showing all %d subscribed channels. (Use -all to see all public channels)\n", total)
	}
	return nil
}
