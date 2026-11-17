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
	"strings"
	"time"
)

// SearchOptions holds parsed options for the search subcommand.
type SearchOptions struct {
	Query   string
	Channel string
	Topic   string
	Sender  string
	Since   string
	Until   string
	AsJSON  bool
	UTC     bool
	Limit   int
}

// SearchResult represents the structured JSON output of the search command.
type SearchResult struct {
	Query    string    `json:"query,omitempty"`
	Channel  string    `json:"channel,omitempty"`
	Topic    string    `json:"topic,omitempty"`
	Sender   string    `json:"sender,omitempty"`
	Since    string    `json:"since,omitempty"`
	Until    string    `json:"until,omitempty"`
	Shown    int       `json:"shown"`
	HasMore  bool      `json:"has_more"`
	Messages []Message `json:"messages"`
}

// messageGroup represents a group of messages belonging to the same channel and topic.
type messageGroup struct {
	channelID int64
	channel   string
	topic     string
	messages  []Message
}

func groupMessagesByTopic(messages []Message) []messageGroup {
	type key struct {
		channelID int64
		channel   string
		topic     string
	}
	var groupOrder []key
	groupMap := make(map[key][]Message)

	for _, msg := range messages {
		ch := msg.ChannelName()
		top := msg.TopicName()
		k := key{channelID: msg.StreamID, channel: ch, topic: top}
		if _, exists := groupMap[k]; !exists {
			groupOrder = append(groupOrder, k)
		}
		groupMap[k] = append(groupMap[k], msg)
	}

	var groups []messageGroup
	for _, k := range groupOrder {
		msgs := groupMap[k]
		sort.SliceStable(msgs, func(i, j int) bool {
			if msgs[i].Timestamp != msgs[j].Timestamp {
				return msgs[i].Timestamp < msgs[j].Timestamp
			}
			return msgs[i].ID < msgs[j].ID
		})
		groups = append(groups, messageGroup{
			channelID: k.channelID,
			channel:   k.channel,
			topic:     k.topic,
			messages:  msgs,
		})
	}
	return groups
}

// runSearch executes the search command logic with the given options and writes output to w.
func runSearch(ctx context.Context, client *Client, cfg *Config, opts SearchOptions, w io.Writer) error {
	if opts.Limit <= 0 {
		return errors.New("-n limit must be greater than 0")
	}

	query := strings.TrimSpace(opts.Query)
	channelRef := strings.TrimSpace(opts.Channel)
	topic := strings.TrimSpace(opts.Topic)
	sender := strings.TrimSpace(opts.Sender)
	since := strings.TrimSpace(opts.Since)
	until := strings.TrimSpace(opts.Until)

	if query == "" && channelRef == "" && topic == "" && sender == "" && since == "" && until == "" {
		return errors.New("missing search query: specify a search query or filter (-c, -t, -sender)")
	}

	if topic == "" && (strings.HasPrefix(channelRef, "http://") || strings.HasPrefix(channelRef, "https://") || strings.HasPrefix(channelRef, "#narrow/")) {
		if parsed, err := ParseURL(channelRef); err == nil && parsed.Topic != "" {
			topic = parsed.Topic
		}
	}

	var (
		channelName string
		streamID    int64
		ops         []NarrowOp
	)

	if channelRef != "" {
		var err error
		streamID, channelName, err = ResolveChannel(ctx, client, channelRef)
		if err != nil {
			return err
		}
		ops = append(ops, NarrowOp{Op: "channel", Operand: streamID})

		if topic != "" {
			resolvedTopic, err := ResolveTopic(ctx, client, streamID, topic)
			if err != nil {
				return err
			}
			topic = resolvedTopic
			ops = append(ops, NarrowOp{Op: "topic", Operand: topic})
		}
	} else if topic != "" {
		ops = append(ops, NarrowOp{Op: "topic", Operand: topic})
	}

	if sender != "" {
		ops = append(ops, NarrowOp{Op: "sender", Operand: sender})
	}

	if query != "" {
		ops = append(ops, NarrowOp{Op: "search", Operand: query})
	}

	var sinceTime time.Time
	if since != "" {
		var err error
		sinceTime, err = ParseTime(since, time.Now())
		if err != nil {
			return err
		}
	}

	var untilTime time.Time
	if until != "" {
		var err error
		untilTime, err = ParseTime(until, time.Now())
		if err != nil {
			return err
		}
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && untilTime.Before(sinceTime) {
		return errors.New("-until time must be after -since time")
	}

	msgOpts := GetMessagesOptions{
		Anchor:    string(AnchorNewest),
		NumBefore: opts.Limit,
		NumAfter:  0,
		Narrow:    ops,
	}

	resp, err := client.GetMessagesInRange(ctx, msgOpts, sinceTime, untilTime, opts.Limit)
	if err != nil {
		return fmt.Errorf("searching messages: %w", err)
	}

	messages := resp.Messages
	hasMore := !resp.FoundOldest || len(messages) >= opts.Limit

	if opts.AsJSON {
		if messages == nil {
			messages = []Message{}
		}
		res := SearchResult{
			Query:    query,
			Channel:  channelName,
			Topic:    topic,
			Sender:   sender,
			Since:    since,
			Until:    until,
			Shown:    len(messages),
			HasMore:  hasMore,
			Messages: messages,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	var site string
	if cfg != nil {
		site = cfg.Site
	}

	return formatSearchMarkdown(w, site, query, channelName, topic, sender, messages, opts.UTC, hasMore, opts.Limit)
}

func formatSearchMarkdown(w io.Writer, site, query, channel, topic, sender string, messages []Message, utcOnly bool, hasMore bool, limit int) error {
	var header strings.Builder
	header.WriteString("# ")
	if query != "" {
		fmt.Fprintf(&header, "Search: %q", query)
	} else {
		header.WriteString("Search")
	}
	if channel != "" && topic != "" {
		fmt.Fprintf(&header, " in [%s > %s]", channel, topic)
	} else if channel != "" {
		fmt.Fprintf(&header, " in [%s]", channel)
	} else if topic != "" {
		fmt.Fprintf(&header, " in [topic: %s]", topic)
	}
	if sender != "" {
		fmt.Fprintf(&header, " from %s", sender)
	}
	fmt.Fprintln(w, header.String())

	if len(messages) == 0 {
		fmt.Fprintln(w, "\n(no messages found)")
		return nil
	}

	groups := groupMessagesByTopic(messages)
	isSingleTopicScope := channel != "" && topic != "" && len(groups) == 1

	for _, g := range groups {
		if isSingleTopicScope {
			if site != "" && (g.channel != "" || g.channelID > 0) && g.topic != "" {
				topicURL := FormatTopicURL(site, g.channelID, g.channel, g.topic, 0)
				if topicURL != "" {
					fmt.Fprintf(w, "URL: %s\n", topicURL)
				}
			}
		} else {
			fmt.Fprintln(w)
			switch {
			case g.channel != "" && g.topic != "":
				fmt.Fprintf(w, "## [%s > %s]\n", g.channel, g.topic)
			case g.channel != "":
				fmt.Fprintf(w, "## [%s]\n", g.channel)
			case g.topic != "":
				fmt.Fprintf(w, "## [%s]\n", g.topic)
			default:
				fmt.Fprintf(w, "## [messages]\n")
			}

			if site != "" && (g.channel != "" || g.channelID > 0) && g.topic != "" {
				topicURL := FormatTopicURL(site, g.channelID, g.channel, g.topic, 0)
				if topicURL != "" {
					fmt.Fprintf(w, "URL: %s\n", topicURL)
				}
			}
		}

		for _, msg := range g.messages {
			senderName := msg.SenderFullName
			if senderName == "" {
				senderName = msg.SenderEmail
			}
			if senderName == "" {
				senderName = fmt.Sprintf("User %d", msg.SenderID)
			}

			timeStr := FormatTimestamp(msg.Timestamp, utcOnly)

			fmt.Fprintln(w)
			fmt.Fprintln(w, "---")
			fmt.Fprintf(w, "**%s** (%s) [ID: %d]:\n", senderName, timeStr, msg.ID)
			fmt.Fprintln(w, strings.TrimRight(msg.Content, "\r\n"))
		}
	}

	fmt.Fprintln(w)
	if hasMore {
		fmt.Fprintf(w, "(Showing %d messages. More results may exist; use -n %d to fetch more, or refine with -c, -t, -sender, -since)\n", len(messages), limit*2)
	} else {
		fmt.Fprintf(w, "(Showing all %d matching messages)\n", len(messages))
	}

	return nil
}
