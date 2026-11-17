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
	"os"
	"slices"
	"strings"
	"time"
)

// CatchupOptions holds parsed options for the catchup subcommand.
type CatchupOptions struct {
	Channel   string
	Since     string
	Until     string
	StateFile string
	NoUpdate  bool
	MarkOnly  bool
	Status    bool
	Reset     bool
	AsJSON    bool
	UTC       bool
	Limit     int
}

// DigestOptions is an alias for CatchupOptions for backward compatibility.
type DigestOptions = CatchupOptions

// CatchupTopicSummary represents a high-level summary of an active topic in catchup output.
type CatchupTopicSummary struct {
	Channel      string   `json:"channel"`
	ChannelID    int64    `json:"channel_id"`
	Topic        string   `json:"topic"`
	URL          string   `json:"url"`
	MessageCount int      `json:"message_count"`
	Senders      []string `json:"senders"`
}

// DigestTopicSummary is an alias for CatchupTopicSummary for backward compatibility.
type DigestTopicSummary = CatchupTopicSummary

// CatchupResult represents the structured JSON output of the catchup command.
type CatchupResult struct {
	Since         string                `json:"since"`
	Until         string                `json:"until"`
	LastCheckTime string                `json:"last_check_time,omitempty"`
	Channel       string                `json:"channel,omitempty"`
	TotalTopics   int                   `json:"total_topics"`
	Shown         int                   `json:"shown_messages"`
	Topics        []CatchupTopicSummary `json:"topics"`
	Messages      []Message             `json:"messages"`
}

// DigestResult is an alias for CatchupResult for backward compatibility.
type DigestResult = CatchupResult

// CatchupStatusResult represents the structured JSON output of catchup -status.
type CatchupStatusResult struct {
	StateFile         string `json:"state_file"`
	LastCheckTime     string `json:"last_check_time,omitempty"`
	LastCheckTimeUTC  string `json:"last_check_time_utc,omitempty"`
	LastCheckRelative string `json:"last_check_relative,omitempty"`
	LastMessageID     int64  `json:"last_message_id,omitempty"`
	TrackedChannels   int    `json:"tracked_channels"`
	Site              string `json:"site,omitempty"`
}

func runCatchupStatus(state *State, filePath string, asJSON, utc bool, w io.Writer) error {
	now := time.Now()
	res := CatchupStatusResult{
		StateFile:       filePath,
		TrackedChannels: len(state.Channels),
		Site:            state.Site,
	}
	if !state.LastCheckTime.IsZero() {
		res.LastCheckTime = FormatTime(state.LastCheckTime, utc)
		res.LastCheckTimeUTC = state.LastCheckTime.UTC().Format(time.RFC3339)
		res.LastCheckRelative = FormatRelativeTime(state.LastCheckTime, now)
		res.LastMessageID = state.LastMessageID
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	fmt.Fprintln(w, "Zulip Catchup Status:")
	fmt.Fprintf(w, "  State file:       %s\n", filePath)
	if !state.LastCheckTime.IsZero() {
		fmt.Fprintf(w, "  Last check:       %s (%s)\n", res.LastCheckTime, res.LastCheckRelative)
		fmt.Fprintf(w, "  Last message ID:  %d\n", res.LastMessageID)
	} else {
		fmt.Fprintln(w, "  Last check:       never (will default to past 24 hours on first catchup)")
		fmt.Fprintln(w, "  Last message ID:  none")
	}
	fmt.Fprintf(w, "  Tracked channels: %d\n", res.TrackedChannels)
	if res.Site != "" {
		fmt.Fprintf(w, "  Site:             %s\n", res.Site)
	}
	return nil
}

// runCatchup executes the catchup command logic and writes Markdown or JSON to w.
func runCatchup(ctx context.Context, client *Client, cfg *Config, opts CatchupOptions, w io.Writer) error {
	if opts.Status && opts.Reset {
		return errors.New("cannot specify both -status and -reset")
	}

	filePath := opts.StateFile
	if filePath == "" {
		var err error
		filePath, err = DefaultStateFile()
		if err != nil {
			return err
		}
	}

	if opts.Reset {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("resetting state file: %w", err)
		}
		fmt.Fprintf(w, "Reset Zulip catchup state (%s).\n", filePath)
		return nil
	}

	state, err := LoadState(opts.StateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if opts.Status {
		return runCatchupStatus(state, filePath, opts.AsJSON, opts.UTC, w)
	}

	now := time.Now()

	if opts.MarkOnly {
		state.LastCheckTime = now
		if cfg != nil && state.Site == "" {
			state.Site = cfg.Site
		}
		if err := state.Save(opts.StateFile); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		fmt.Fprintf(w, "Marked Zulip catchup cursor up to %s.\n", FormatTime(now, opts.UTC))
		return nil
	}

	if opts.Limit <= 0 {
		return errors.New("-n limit must be greater than 0")
	}

	var sinceTime time.Time
	if opts.Since != "" {
		var err error
		sinceTime, err = ParseTime(opts.Since, now)
		if err != nil {
			return fmt.Errorf("parsing -since: %w", err)
		}
	} else if !state.LastCheckTime.IsZero() {
		sinceTime = state.LastCheckTime
	} else {
		// Default to past 24 hours on initial run
		sinceTime = now.Add(-24 * time.Hour)
	}

	var untilTime time.Time
	if opts.Until != "" {
		var err error
		untilTime, err = ParseTime(opts.Until, now)
		if err != nil {
			return fmt.Errorf("parsing -until: %w", err)
		}
	} else {
		untilTime = now
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && untilTime.Before(sinceTime) {
		return errors.New("-until time must be after -since time")
	}

	var (
		channelName string
		streamID    int64
		ops         []NarrowOp
	)

	channelRef := strings.TrimSpace(opts.Channel)
	if channelRef != "" {
		var err error
		streamID, channelName, err = ResolveChannel(ctx, client, channelRef)
		if err != nil {
			return err
		}
		ops = append(ops, NarrowOp{Op: "channel", Operand: streamID})
	}

	msgOpts := GetMessagesOptions{
		Anchor:    string(AnchorNewest),
		NumBefore: opts.Limit,
		NumAfter:  0,
		Narrow:    ops,
	}

	resp, err := client.GetMessagesInRange(ctx, msgOpts, sinceTime, untilTime, opts.Limit)
	if err != nil {
		return fmt.Errorf("fetching catchup messages: %w", err)
	}

	messages := resp.Messages
	groups := groupMessagesByTopic(messages)

	site := ""
	if cfg != nil {
		site = cfg.Site
	}

	var topicSummaries []CatchupTopicSummary
	for _, g := range groups {
		var senders []string
		for _, m := range g.messages {
			name := m.SenderFullName
			if name == "" {
				name = m.SenderEmail
			}
			if name != "" && !slices.Contains(senders, name) {
				senders = append(senders, name)
			}
		}
		topicURL := FormatTopicURL(site, g.channelID, g.channel, g.topic, 0)
		topicSummaries = append(topicSummaries, CatchupTopicSummary{
			Channel:      g.channel,
			ChannelID:    g.channelID,
			Topic:        g.topic,
			URL:          topicURL,
			MessageCount: len(g.messages),
			Senders:      senders,
		})
	}

	if !opts.NoUpdate {
		state.UpdateFromMessages(messages, untilTime)
		if site != "" && state.Site == "" {
			state.Site = site
		}
		if err := state.Save(opts.StateFile); err != nil {
			return fmt.Errorf("updating state: %w", err)
		}
	}

	sinceStr := FormatTime(sinceTime, opts.UTC)
	untilStr := FormatTime(untilTime, opts.UTC)

	if opts.AsJSON {
		if messages == nil {
			messages = []Message{}
		}
		if topicSummaries == nil {
			topicSummaries = []CatchupTopicSummary{}
		}
		res := CatchupResult{
			Since:       sinceStr,
			Until:       untilStr,
			Channel:     channelName,
			TotalTopics: len(topicSummaries),
			Shown:       len(messages),
			Topics:      topicSummaries,
			Messages:    messages,
		}
		if !state.LastCheckTime.IsZero() {
			res.LastCheckTime = FormatTime(state.LastCheckTime, opts.UTC)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	// Markdown output
	if channelName != "" {
		fmt.Fprintf(w, "# Zulip Catchup for [%s] (%s to %s)\n\n", channelName, sinceStr, untilStr)
	} else {
		fmt.Fprintf(w, "# Zulip Catchup (%s to %s)\n\n", sinceStr, untilStr)
	}

	if len(messages) == 0 {
		fmt.Fprintln(w, "(no new messages found during this period)")
		return nil
	}

	fmt.Fprintf(w, "Total messages: %d across %d active topic(s)\n\n", len(messages), len(topicSummaries))
	fmt.Fprintln(w, "## Active Topics:")
	for _, ts := range topicSummaries {
		senderList := strings.Join(ts.Senders, ", ")
		if ts.URL != "" {
			fmt.Fprintf(w, "- [%s > %s](%s) (%d msg(s) by %s)\n", ts.Channel, ts.Topic, ts.URL, ts.MessageCount, senderList)
		} else {
			fmt.Fprintf(w, "- [%s > %s] (%d msg(s) by %s)\n", ts.Channel, ts.Topic, ts.MessageCount, senderList)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")

	for _, g := range groups {
		topicURL := FormatTopicURL(site, g.channelID, g.channel, g.topic, 0)
		fmt.Fprintf(w, "\n## [%s > %s]\n", g.channel, g.topic)
		if topicURL != "" {
			fmt.Fprintf(w, "URL: %s\n", topicURL)
		}
		for _, m := range g.messages {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "---")
			sender := m.SenderFullName
			if sender == "" {
				sender = m.SenderEmail
			}
			tStr := FormatTimestamp(m.Timestamp, opts.UTC)
			fmt.Fprintf(w, "**%s** (%s) [ID: %d]:\n", sender, tStr, m.ID)
			fmt.Fprintln(w, m.Content)
		}
	}

	return nil
}

// runDigest is an alias for runCatchup for backward compatibility.
func runDigest(ctx context.Context, client *Client, cfg *Config, opts DigestOptions, w io.Writer) error {
	return runCatchup(ctx, client, cfg, opts, w)
}
