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
	"strconv"
	"strings"
	"time"
)

// ReadOptions holds parsed options for the read subcommand.
type ReadOptions struct {
	URL     string
	Channel string
	Topic   string
	Since   string
	Until   string
	Unread  bool
	MsgID   int64
	AsJSON  bool
	UTC     bool
	Limit   int
}

// ReadResult represents the structured JSON output of the read command.
type ReadResult struct {
	URL         string    `json:"url,omitempty"`
	Channel     string    `json:"channel,omitempty"`
	Topic       string    `json:"topic,omitempty"`
	Since       string    `json:"since,omitempty"`
	Until       string    `json:"until,omitempty"`
	Shown       int       `json:"shown"`
	FoundOldest bool      `json:"found_oldest"`
	FoundNewest bool      `json:"found_newest"`
	HasOlder    bool      `json:"has_older"`
	HasNewer    bool      `json:"has_newer"`
	Messages    []Message `json:"messages"`
}

// runRead executes the read command logic with the given options and writes output to w.
func runRead(ctx context.Context, client *Client, cfg *Config, opts ReadOptions, w io.Writer) error {
	if opts.Limit <= 0 {
		return errors.New("-n limit must be greater than 0")
	}

	var target *NarrowTarget
	if opts.URL != "" {
		parsed, err := ParseURL(opts.URL)
		if err != nil {
			return err
		}
		target = parsed
	} else {
		target = &NarrowTarget{}
	}

	if opts.Channel != "" {
		target.Channel = opts.Channel
	}
	if opts.Topic != "" {
		target.Topic = opts.Topic
	}
	if opts.MsgID > 0 {
		target.Anchor = opts.MsgID
		if target.AnchorType == "" || target.AnchorType == AnchorDefault {
			target.AnchorType = AnchorWith
		}
	}
	if opts.Unread {
		target.AnchorType = AnchorFirstUnread
		target.Anchor = 0
	}

	if target.Channel == "" && target.ChannelID <= 0 && target.Topic == "" && target.Anchor <= 0 && target.AnchorType == "" && target.Search == "" && target.Sender == "" && len(target.Ops) == 0 {
		return errors.New("must specify a Zulip URL, channel (-c), topic (-t), or message ID (-id)")
	}

	var sinceTime time.Time
	if opts.Since != "" {
		var err error
		sinceTime, err = ParseTime(opts.Since, time.Now())
		if err != nil {
			return err
		}
	}

	var untilTime time.Time
	if opts.Until != "" {
		var err error
		untilTime, err = ParseTime(opts.Until, time.Now())
		if err != nil {
			return err
		}
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && untilTime.Before(sinceTime) {
		return errors.New("-until time must be after -since time")
	}

	var (
		anchor    string
		numBefore int
		numAfter  int
	)

	switch {
	case target.AnchorType == AnchorFirstUnread:
		anchor = string(AnchorFirstUnread)
		numBefore = 0
		numAfter = opts.Limit

	case target.AnchorType == AnchorID:
		anchor = strconv.FormatInt(target.Anchor, 10)
		numBefore = 0
		numAfter = 0

	case target.Anchor > 0:
		anchor = strconv.FormatInt(target.Anchor, 10)
		numBefore = opts.Limit / 2
		numAfter = opts.Limit - numBefore

	default:
		anchor = string(AnchorNewest)
		numBefore = opts.Limit
		numAfter = 0
	}

	apiOps := FilterAPINarrowOps(target.NarrowOps())
	var narrowJSON string
	if len(apiOps) > 0 {
		b, err := json.Marshal(apiOps)
		if err != nil {
			return fmt.Errorf("marshaling narrow filter: %w", err)
		}
		narrowJSON = string(b)
	}

	msgOpts := GetMessagesOptions{
		Anchor:     anchor,
		NumBefore:  numBefore,
		NumAfter:   numAfter,
		NarrowJSON: narrowJSON,
	}

	resp, err := client.GetMessagesInRange(ctx, msgOpts, sinceTime, untilTime, opts.Limit)
	if err != nil {
		return fmt.Errorf("fetching messages: %w", err)
	}

	messages := resp.Messages

	if opts.Unread {
		var filtered []Message
		for _, m := range messages {
			if m.IsUnread() {
				filtered = append(filtered, m)
			}
		}
		messages = filtered
	}

	if len(messages) > 0 {
		if target.Channel == "" {
			target.Channel = messages[0].ChannelName()
		}
		if target.ChannelID <= 0 {
			target.ChannelID = messages[0].StreamID
		}
		if target.Topic == "" {
			target.Topic = messages[0].TopicName()
		}
	}

	site := cfg.Site
	if target.Site != "" {
		site = target.Site
	}
	webURL := target.WebURL(site)

	hasOlder := !resp.FoundOldest
	hasNewer := !resp.FoundNewest

	if opts.AsJSON {
		if messages == nil {
			messages = []Message{}
		}
		res := ReadResult{
			URL:         webURL,
			Channel:     target.Channel,
			Topic:       target.Topic,
			Since:       opts.Since,
			Until:       opts.Until,
			Shown:       len(messages),
			FoundOldest: resp.FoundOldest,
			FoundNewest: resp.FoundNewest,
			HasOlder:    hasOlder,
			HasNewer:    hasNewer,
			Messages:    messages,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	return formatThreadMarkdown(w, target, messages, webURL, opts.UTC, resp.FoundOldest, resp.FoundNewest, opts.Limit)
}

func formatThreadMarkdown(w io.Writer, target *NarrowTarget, messages []Message, webURL string, utcOnly bool, foundOldest, foundNewest bool, limit int) error {
	switch {
	case target.Channel != "" && target.Topic != "":
		fmt.Fprintf(w, "# [%s] %s\n", target.Channel, target.Topic)
	case target.Channel != "":
		fmt.Fprintf(w, "# [%s]\n", target.Channel)
	case target.Topic != "":
		fmt.Fprintf(w, "# %s\n", target.Topic)
	default:
		fmt.Fprintf(w, "# Zulip Messages\n")
	}

	if webURL != "" {
		fmt.Fprintf(w, "URL: %s\n", webURL)
	}

	if len(messages) == 0 {
		fmt.Fprintln(w, "\n(no messages found)")
		return nil
	}

	if !foundOldest {
		fmt.Fprintf(w, "\n[... earlier messages exist in this topic. Use -n %d or -id %d to read earlier ...]\n", limit*2, messages[0].ID)
	}

	for _, msg := range messages {
		sender := msg.SenderFullName
		if sender == "" {
			sender = msg.SenderEmail
		}
		if sender == "" {
			sender = fmt.Sprintf("User %d", msg.SenderID)
		}

		timeStr := FormatTimestamp(msg.Timestamp, utcOnly)

		fmt.Fprintln(w)
		fmt.Fprintln(w, "---")
		fmt.Fprintf(w, "**%s** (%s) [ID: %d]:\n", sender, timeStr, msg.ID)
		fmt.Fprintln(w, strings.TrimRight(msg.Content, "\r\n"))
	}

	if !foundNewest {
		fmt.Fprintf(w, "\n[... newer messages exist in this topic. Use -n %d or -unread to read newer ...]\n", limit*2)
	}

	fmt.Fprintln(w)
	hasOlder := !foundOldest
	hasNewer := !foundNewest
	if hasOlder || hasNewer {
		olderStr := "no"
		if hasOlder {
			olderStr = "yes"
		}
		newerStr := "no"
		if hasNewer {
			newerStr = "yes"
		}
		fmt.Fprintf(w, "(Showing %d messages. Earlier messages: %s, Newer messages: %s. Use -n to adjust limit)\n", len(messages), olderStr, newerStr)
	} else {
		fmt.Fprintf(w, "(Showing all %d messages in topic)\n", len(messages))
	}

	return nil
}
