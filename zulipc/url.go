// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// NarrowOp represents a single narrow filter operator and operand for the Zulip API.
type NarrowOp struct {
	Op      string `json:"operator"`
	Operand any    `json:"operand"`
	Negated bool   `json:"negated,omitempty"`
}

// FilterAPINarrowOps removes anchor-only operators (with, near, id) from a narrow operator list.
func FilterAPINarrowOps(ops []NarrowOp) []NarrowOp {
	var result []NarrowOp
	for _, op := range ops {
		switch strings.ToLower(op.Op) {
		case "with", "near", "id":
			continue
		default:
			result = append(result, op)
		}
	}
	return result
}

// AnchorType represents the context mode for message fetching.
type AnchorType string

const (
	AnchorDefault     AnchorType = ""
	AnchorWith        AnchorType = "with"
	AnchorNear        AnchorType = "near"
	AnchorID          AnchorType = "id"
	AnchorOldest      AnchorType = "oldest"
	AnchorNewest      AnchorType = "newest"
	AnchorFirstUnread AnchorType = "first_unread"
)

// NarrowTarget holds the extracted narrow parameters from a URL or CLI arguments.
type NarrowTarget struct {
	Site       string     `json:"site,omitempty"`
	ChannelID  int64      `json:"channel_id,omitempty"`
	Channel    string     `json:"channel,omitempty"`
	Topic      string     `json:"topic,omitempty"`
	Anchor     int64      `json:"anchor,omitempty"`
	AnchorType AnchorType `json:"anchor_type,omitempty"`
	NumBefore  int        `json:"num_before,omitempty"`
	NumAfter   int        `json:"num_after,omitempty"`
	Sender     string     `json:"sender,omitempty"`
	Search     string     `json:"search,omitempty"`
	Raw        string     `json:"raw,omitempty"`
	Ops        []NarrowOp `json:"ops,omitempty"`
}

// String returns a human-readable summary of the narrow target.
func (t *NarrowTarget) String() string {
	if t.Channel != "" && t.Topic != "" {
		if t.Anchor > 0 {
			return fmt.Sprintf("[%s > %s #%d]", t.Channel, t.Topic, t.Anchor)
		}
		return fmt.Sprintf("[%s > %s]", t.Channel, t.Topic)
	}
	if t.Channel != "" {
		return fmt.Sprintf("[%s]", t.Channel)
	}
	if t.Topic != "" {
		return fmt.Sprintf("[topic: %s]", t.Topic)
	}
	if t.Search != "" {
		return fmt.Sprintf("[search: %s]", t.Search)
	}
	if t.Anchor > 0 {
		return fmt.Sprintf("[msg: %d]", t.Anchor)
	}
	return "[narrow]"
}

// NarrowOps returns the list of NarrowOp objects for the target.
func (t *NarrowTarget) NarrowOps() []NarrowOp {
	if len(t.Ops) > 0 {
		return t.Ops
	}
	var ops []NarrowOp
	if t.ChannelID > 0 {
		ops = append(ops, NarrowOp{Op: "channel", Operand: t.ChannelID})
	} else if t.Channel != "" {
		ops = append(ops, NarrowOp{Op: "channel", Operand: t.Channel})
	}
	if t.Topic != "" {
		ops = append(ops, NarrowOp{Op: "topic", Operand: t.Topic})
	}
	if t.Sender != "" {
		ops = append(ops, NarrowOp{Op: "sender", Operand: t.Sender})
	}
	if t.Search != "" {
		ops = append(ops, NarrowOp{Op: "search", Operand: t.Search})
	}
	return ops
}

// NarrowJSON returns the JSON encoding of the narrow filter for the Zulip REST API.
func (t *NarrowTarget) NarrowJSON() (string, error) {
	ops := t.NarrowOps()
	if len(ops) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(ops)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WebURL returns the canonical Zulip web URL for the narrow target.
// If site is empty, it falls back to target.Site.
func (t *NarrowTarget) WebURL(site string) string {
	if site == "" {
		site = t.Site
	}
	site = strings.TrimRight(site, "/")

	var parts []string

	if t.ChannelID > 0 && t.Channel != "" {
		parts = append(parts, "channel", fmt.Sprintf("%d-%s", t.ChannelID, EncodeHashComponent(t.Channel)))
	} else if t.ChannelID > 0 {
		parts = append(parts, "channel", fmt.Sprintf("%d", t.ChannelID))
	} else if t.Channel != "" {
		parts = append(parts, "channel", EncodeHashComponent(t.Channel))
	}

	if t.Topic != "" {
		parts = append(parts, "topic", EncodeHashComponent(t.Topic))
	}

	if t.Sender != "" {
		parts = append(parts, "sender", EncodeHashComponent(t.Sender))
	}

	if t.Search != "" {
		parts = append(parts, "search", EncodeHashComponent(t.Search))
	}

	if t.Anchor > 0 {
		anchorType := string(t.AnchorType)
		if anchorType == "" {
			anchorType = "near"
		}
		parts = append(parts, anchorType, fmt.Sprintf("%d", t.Anchor))
	}

	var fragment string
	if len(parts) > 0 {
		fragment = "#narrow/" + strings.Join(parts, "/")
	}

	if site != "" {
		if fragment != "" {
			return site + "/" + fragment
		}
		return site
	}
	return fragment
}

// FormatTopicURL constructs a canonical Zulip topic web URL.
func FormatTopicURL(site string, channelID int64, channelName, topic string, msgID int64) string {
	target := &NarrowTarget{
		Site:       site,
		ChannelID:  channelID,
		Channel:    channelName,
		Topic:      topic,
		Anchor:     msgID,
		AnchorType: AnchorWith,
	}
	if msgID <= 0 {
		target.Anchor = 0
		target.AnchorType = AnchorDefault
	}
	return target.WebURL(site)
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// DecodeHashComponent unescapes a Zulip URL hash component where '.' followed
// by 2 hex digits represents an escaped byte (e.g. .20 -> space, .2E -> dot).
func DecodeHashComponent(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '.' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			b.WriteByte('%')
			b.WriteByte(s[i+1])
			b.WriteByte(s[i+2])
			i += 3
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return url.PathUnescape(b.String())
}

// EncodeHashComponent encodes a string into Zulip's hash-safe dot-escaped format.
// Alphanumeric characters, '-' and '_' are preserved without modification.
// All other bytes are encoded as .HH (uppercase hex).
func EncodeHashComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, ".%02X", c)
		}
	}
	return b.String()
}

func parseChannelOperand(raw string) (int64, string, error) {
	hyphenIdx := strings.IndexByte(raw, '-')
	if hyphenIdx > 0 {
		prefix := raw[:hyphenIdx]
		if id, err := strconv.ParseInt(prefix, 10, 64); err == nil && id > 0 {
			name, err := DecodeHashComponent(raw[hyphenIdx+1:])
			if err != nil {
				return 0, "", fmt.Errorf("decoding channel name %q: %w", raw[hyphenIdx+1:], err)
			}
			return id, name, nil
		}
	}

	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return id, "", nil
	}

	name, err := DecodeHashComponent(raw)
	if err != nil {
		return 0, "", fmt.Errorf("decoding channel name %q: %w", raw, err)
	}
	return 0, name, nil
}

// IsZulipURL reports whether a string looks like a Zulip narrow URL or fragment.
func IsZulipURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "#narrow/") ||
		strings.Contains(s, "/#narrow/") ||
		strings.HasPrefix(s, "#narrow/") ||
		strings.HasPrefix(s, "narrow/") ||
		strings.HasPrefix(s, "/narrow/")
}

// ParseNarrowFragment parses the fragment portion of a Zulip URL (e.g. narrow/channel/...).
func ParseNarrowFragment(fragment string) (*NarrowTarget, error) {
	s := strings.TrimSpace(fragment)
	if idx := strings.IndexByte(s, '?'); idx >= 0 {
		s = s[:idx]
	}

	s = strings.TrimLeft(s, "/#")
	if after, ok := strings.CutPrefix(s, "narrow/"); ok {
		s = after
	} else if s == "narrow" {
		s = ""
	}

	s = strings.Trim(s, "/")
	if s == "" {
		return nil, errors.New("empty narrow fragment")
	}

	rawTokens := strings.Split(s, "/")
	var tokens []string
	for _, tok := range rawTokens {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			tokens = append(tokens, tok)
		}
	}

	if len(tokens) == 0 {
		return nil, errors.New("empty narrow fragment")
	}

	if len(tokens)%2 != 0 {
		return nil, fmt.Errorf("invalid narrow fragment: missing operand for operator %q", tokens[len(tokens)-1])
	}

	target := &NarrowTarget{}

	for i := 0; i < len(tokens); i += 2 {
		op := tokens[i]
		rawVal := tokens[i+1]

		negated := false
		if strings.HasPrefix(op, "-") {
			negated = true
			op = op[1:]
		}
		op = strings.ToLower(op)

		switch op {
		case "channel", "stream":
			channelID, channelName, err := parseChannelOperand(rawVal)
			if err != nil {
				return nil, err
			}
			target.ChannelID = channelID
			target.Channel = channelName
			var operand any = channelName
			if channelID > 0 {
				operand = channelID
			}
			target.Ops = append(target.Ops, NarrowOp{
				Op:      op,
				Operand: operand,
				Negated: negated,
			})

		case "topic", "subject":
			topic, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding topic operand %q: %w", rawVal, err)
			}
			target.Topic = topic
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "topic",
				Operand: topic,
				Negated: negated,
			})

		case "with":
			msgID, err := strconv.ParseInt(rawVal, 10, 64)
			if err != nil || msgID <= 0 {
				return nil, fmt.Errorf("invalid message ID in with operator: %q", rawVal)
			}
			target.Anchor = msgID
			target.AnchorType = AnchorWith
			target.NumBefore = 20
			target.NumAfter = 20
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "with",
				Operand: msgID,
				Negated: negated,
			})

		case "near":
			msgID, err := strconv.ParseInt(rawVal, 10, 64)
			if err != nil || msgID <= 0 {
				return nil, fmt.Errorf("invalid message ID in near operator: %q", rawVal)
			}
			target.Anchor = msgID
			target.AnchorType = AnchorNear
			target.NumBefore = 20
			target.NumAfter = 20
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "near",
				Operand: msgID,
				Negated: negated,
			})

		case "id":
			msgID, err := strconv.ParseInt(rawVal, 10, 64)
			if err != nil || msgID <= 0 {
				return nil, fmt.Errorf("invalid message ID in id operator: %q", rawVal)
			}
			target.Anchor = msgID
			target.AnchorType = AnchorID
			target.NumBefore = 0
			target.NumAfter = 0
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "id",
				Operand: msgID,
				Negated: negated,
			})

		case "sender":
			sender, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding sender operand %q: %w", rawVal, err)
			}
			target.Sender = sender
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "sender",
				Operand: sender,
				Negated: negated,
			})

		case "search":
			searchQuery, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding search operand %q: %w", rawVal, err)
			}
			target.Search = searchQuery
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "search",
				Operand: searchQuery,
				Negated: negated,
			})

		case "is":
			isVal, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding is operand %q: %w", rawVal, err)
			}
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "is",
				Operand: isVal,
				Negated: negated,
			})

		case "has":
			hasVal, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding has operand %q: %w", rawVal, err)
			}
			target.Ops = append(target.Ops, NarrowOp{
				Op:      "has",
				Operand: hasVal,
				Negated: negated,
			})

		case "pm-with", "dm":
			dmVal, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding %s operand %q: %w", op, rawVal, err)
			}
			target.Ops = append(target.Ops, NarrowOp{
				Op:      op,
				Operand: dmVal,
				Negated: negated,
			})

		default:
			decodedVal, err := DecodeHashComponent(rawVal)
			if err != nil {
				return nil, fmt.Errorf("decoding %s operand %q: %w", op, rawVal, err)
			}
			target.Ops = append(target.Ops, NarrowOp{
				Op:      op,
				Operand: decodedVal,
				Negated: negated,
			})
		}
	}

	return target, nil
}

// ParseURL parses a Zulip URL into structured narrow parameters.
func ParseURL(rawURL string) (*NarrowTarget, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return nil, errors.New("empty URL")
	}

	raw = strings.ReplaceAll(raw, "%23narrow", "#narrow")
	raw = strings.ReplaceAll(raw, "%23/narrow", "#narrow")

	var site string
	var fragment string

	if strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "/#") || strings.HasPrefix(raw, "/narrow/") || strings.HasPrefix(raw, "narrow/") {
		fragment = raw
	} else {
		toParse := raw
		if !strings.Contains(toParse, "://") {
			toParse = "https://" + toParse
		}
		u, err := url.Parse(toParse)
		if err != nil {
			return nil, fmt.Errorf("parsing URL %q: %w", rawURL, err)
		}
		if u.Scheme != "" && u.Host != "" {
			site = u.Scheme + "://" + u.Host
		}
		fragment = u.Fragment
		if fragment == "" && (strings.HasPrefix(u.Path, "/narrow/") || strings.HasPrefix(u.Path, "narrow/")) {
			fragment = u.Path
		}
	}

	if fragment == "" {
		return nil, fmt.Errorf("no narrow fragment found in URL %q", rawURL)
	}

	target, err := ParseNarrowFragment(fragment)
	if err != nil {
		return nil, fmt.Errorf("parsing narrow fragment %q: %w", fragment, err)
	}
	target.Site = site
	target.Raw = rawURL
	return target, nil
}
