// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/google/go-cmp/cmp"
)

func TestDecodeHashComponent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "plain word",
			input: "general",
			want:  "general",
		},
		{
			name:  "spaces encoded as dot20",
			input: "closing.20stale.20watchflakes",
			want:  "closing stale watchflakes",
		},
		{
			name:  "dots encoded as dot2E",
			input: "release.20v1.2E26.2E0",
			want:  "release v1.26.0",
		},
		{
			name:  "parentheses and slashes",
			input: "proposal.20.28math.2Frand.2Fv2.29",
			want:  "proposal (math/rand/v2)",
		},
		{
			name:  "punctuation colons question marks hashes",
			input: "question.3A.20why.3F.20tag.23urgent",
			want:  "question: why? tag#urgent",
		},
		{
			name:  "plus signs",
			input: "c.2B.2B.20support",
			want:  "c++ support",
		},
		{
			name:  "emoji utf8 multi-byte",
			input: "rocket.20.F0.9F.9A.80.20launch",
			want:  "rocket 🚀 launch",
		},
		{
			name:  "accented unicode utf8",
			input: "caf.C3.A9.20au.20lait",
			want:  "café au lait",
		},
		{
			name:  "japanese characters",
			input: ".E3.83.86.E3.82.B9.E3.83.88",
			want:  "テスト",
		},
		{
			name:  "literal unescaped non-hex dot sequences",
			input: "file.go",
			want:  "file.go",
		},
		{
			name:  "short dot at end",
			input: "test.2",
			want:  "test.2",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "mixed percent and dot encoding",
			input: "foo%20bar.2Ebaz",
			want:  "foo bar.baz",
		},
		{
			name:    "invalid percent encoding",
			input:   "foo%zzbar",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeHashComponent(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DecodeHashComponent(%q) error = %v, want error: %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("DecodeHashComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEncodeHashComponent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain alphanumeric with hyphens and underscores",
			input: "general-123_abc",
			want:  "general-123_abc",
		},
		{
			name:  "spaces and dots",
			input: "closing stale watchflakes.go",
			want:  "closing.20stale.20watchflakes.2Ego",
		},
		{
			name:  "parentheses and slashes",
			input: "proposal (math/rand/v2)",
			want:  "proposal.20.28math.2Frand.2Fv2.29",
		},
		{
			name:  "plus signs",
			input: "c++",
			want:  "c.2B.2B",
		},
		{
			name:  "emoji utf8",
			input: "🚀",
			want:  ".F0.9F.9A.80",
		},
		{
			name:  "unicode café",
			input: "café",
			want:  "caf.C3.A9",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeHashComponent(tt.input)
			if got != tt.want {
				t.Errorf("EncodeHashComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}

			// Verify roundtrip
			decoded, err := DecodeHashComponent(got)
			if err != nil {
				t.Fatalf("roundtrip DecodeHashComponent(%q) error = %v, want nil", got, err)
			}
			if decoded != tt.input {
				t.Errorf("roundtrip DecodeHashComponent(%q) = %q, want %q", got, decoded, tt.input)
			}
		})
	}
}

func TestParseURLValid(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want NarrowTarget
	}{
		{
			name: "real world channel topic with anchor",
			url:  "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069",
			want: NarrowTarget{
				Site:       "https://zulip.example.com",
				ChannelID:  615773,
				Channel:    "general",
				Topic:      "closing stale watchflakes",
				Anchor:     619610069,
				AnchorType: AnchorWith,
				NumBefore:  20,
				NumAfter:   20,
				Raw:        "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "closing stale watchflakes"},
					{Op: "with", Operand: int64(619610069)},
				},
			},
		},
		{
			name: "legacy stream channel topic near anchor",
			url:  "http://localhost:9991/#narrow/stream/10-dev/topic/welcome.20to.20dev/near/42",
			want: NarrowTarget{
				Site:       "http://localhost:9991",
				ChannelID:  10,
				Channel:    "dev",
				Topic:      "welcome to dev",
				Anchor:     42,
				AnchorType: AnchorNear,
				NumBefore:  20,
				NumAfter:   20,
				Raw:        "http://localhost:9991/#narrow/stream/10-dev/topic/welcome.20to.20dev/near/42",
				Ops: []NarrowOp{
					{Op: "stream", Operand: int64(10)},
					{Op: "topic", Operand: "welcome to dev"},
					{Op: "near", Operand: int64(42)},
				},
			},
		},
		{
			name: "stream without numeric ID",
			url:  "https://zulip.example.com/#narrow/stream/general/topic/gopls.20release",
			want: NarrowTarget{
				Site:    "https://zulip.example.com",
				Channel: "general",
				Topic:   "gopls release",
				Raw:     "https://zulip.example.com/#narrow/stream/general/topic/gopls.20release",
				Ops: []NarrowOp{
					{Op: "stream", Operand: "general"},
					{Op: "topic", Operand: "gopls release"},
				},
			},
		},
		{
			name: "channel without numeric ID",
			url:  "https://zulip.example.com/#narrow/channel/general/topic/gopls.20release",
			want: NarrowTarget{
				Site:    "https://zulip.example.com",
				Channel: "general",
				Topic:   "gopls release",
				Raw:     "https://zulip.example.com/#narrow/channel/general/topic/gopls.20release",
				Ops: []NarrowOp{
					{Op: "channel", Operand: "general"},
					{Op: "topic", Operand: "gopls release"},
				},
			},
		},
		{
			name: "channel with numeric ID only",
			url:  "https://zulip.example.com/#narrow/channel/615773/topic/test",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Topic:     "test",
				Raw:       "https://zulip.example.com/#narrow/channel/615773/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "channel only without topic",
			url:  "https://zulip.example.com/#narrow/channel/615773-general",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Raw:       "https://zulip.example.com/#narrow/channel/615773-general",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
				},
			},
		},
		{
			name: "stream only without topic",
			url:  "https://zulip.example.com/#narrow/stream/general",
			want: NarrowTarget{
				Site:    "https://zulip.example.com",
				Channel: "general",
				Raw:     "https://zulip.example.com/#narrow/stream/general",
				Ops: []NarrowOp{
					{Op: "stream", Operand: "general"},
				},
			},
		},
		{
			name: "near anchor only",
			url:  "https://zulip.example.com/#narrow/near/619610069",
			want: NarrowTarget{
				Site:       "https://zulip.example.com",
				Anchor:     619610069,
				AnchorType: AnchorNear,
				NumBefore:  20,
				NumAfter:   20,
				Raw:        "https://zulip.example.com/#narrow/near/619610069",
				Ops: []NarrowOp{
					{Op: "near", Operand: int64(619610069)},
				},
			},
		},
		{
			name: "id single message anchor only",
			url:  "https://zulip.example.com/#narrow/id/619610069",
			want: NarrowTarget{
				Site:       "https://zulip.example.com",
				Anchor:     619610069,
				AnchorType: AnchorID,
				NumBefore:  0,
				NumAfter:   0,
				Raw:        "https://zulip.example.com/#narrow/id/619610069",
				Ops: []NarrowOp{
					{Op: "id", Operand: int64(619610069)},
				},
			},
		},
		{
			name: "with message anchor only",
			url:  "https://zulip.example.com/#narrow/with/619610069",
			want: NarrowTarget{
				Site:       "https://zulip.example.com",
				Anchor:     619610069,
				AnchorType: AnchorWith,
				NumBefore:  20,
				NumAfter:   20,
				Raw:        "https://zulip.example.com/#narrow/with/619610069",
				Ops: []NarrowOp{
					{Op: "with", Operand: int64(619610069)},
				},
			},
		},
		{
			name: "search narrow",
			url:  "https://zulip.example.com/#narrow/search/watchflakes.20stale",
			want: NarrowTarget{
				Site:   "https://zulip.example.com",
				Search: "watchflakes stale",
				Raw:    "https://zulip.example.com/#narrow/search/watchflakes.20stale",
				Ops: []NarrowOp{
					{Op: "search", Operand: "watchflakes stale"},
				},
			},
		},
		{
			name: "sender ID and name",
			url:  "https://zulip.example.com/#narrow/sender/12345-alice",
			want: NarrowTarget{
				Site:   "https://zulip.example.com",
				Sender: "12345-alice",
				Raw:    "https://zulip.example.com/#narrow/sender/12345-alice",
				Ops: []NarrowOp{
					{Op: "sender", Operand: "12345-alice"},
				},
			},
		},
		{
			name: "sender email",
			url:  "https://zulip.example.com/#narrow/sender/alice.40example.2Ecom",
			want: NarrowTarget{
				Site:   "https://zulip.example.com",
				Sender: "alice@example.com",
				Raw:    "https://zulip.example.com/#narrow/sender/alice.40example.2Ecom",
				Ops: []NarrowOp{
					{Op: "sender", Operand: "alice@example.com"},
				},
			},
		},
		{
			name: "negated topic",
			url:  "https://zulip.example.com/#narrow/-topic/muted.20topic",
			want: NarrowTarget{
				Site:  "https://zulip.example.com",
				Topic: "muted topic",
				Raw:   "https://zulip.example.com/#narrow/-topic/muted.20topic",
				Ops: []NarrowOp{
					{Op: "topic", Operand: "muted topic", Negated: true},
				},
			},
		},
		{
			name: "dm conversation",
			url:  "https://zulip.example.com/#narrow/dm/12345,67890-group",
			want: NarrowTarget{
				Site: "https://zulip.example.com",
				Raw:  "https://zulip.example.com/#narrow/dm/12345,67890-group",
				Ops: []NarrowOp{
					{Op: "dm", Operand: "12345,67890-group"},
				},
			},
		},
		{
			name: "pm-with legacy conversation",
			url:  "https://zulip.example.com/#narrow/pm-with/12345,67890-group",
			want: NarrowTarget{
				Site: "https://zulip.example.com",
				Raw:  "https://zulip.example.com/#narrow/pm-with/12345,67890-group",
				Ops: []NarrowOp{
					{Op: "pm-with", Operand: "12345,67890-group"},
				},
			},
		},
		{
			name: "is and has operators",
			url:  "https://zulip.example.com/#narrow/is/starred/has/link",
			want: NarrowTarget{
				Site: "https://zulip.example.com",
				Raw:  "https://zulip.example.com/#narrow/is/starred/has/link",
				Ops: []NarrowOp{
					{Op: "is", Operand: "starred"},
					{Op: "has", Operand: "link"},
				},
			},
		},
		{
			name: "custom operator",
			url:  "https://zulip.example.com/#narrow/customop/customval",
			want: NarrowTarget{
				Site: "https://zulip.example.com",
				Raw:  "https://zulip.example.com/#narrow/customop/customval",
				Ops: []NarrowOp{
					{Op: "customop", Operand: "customval"},
				},
			},
		},
		{
			name: "fragment only with hash",
			url:  "#narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "#narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "fragment only with slash-narrow",
			url:  "/narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "/narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "fragment only starting with narrow",
			url:  "narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "URL without scheme",
			url:  "zulip.example.com/#narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "zulip.example.com/#narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "URL with query parameter",
			url:  "https://zulip.example.com/#narrow/channel/615773-general/topic/test?unread=true",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "https://zulip.example.com/#narrow/channel/615773-general/topic/test?unread=true",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "URL with percent-encoded hash",
			url:  "https://zulip.example.com/%23narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "https://zulip.example.com/%23narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
		{
			name: "subject alias for topic",
			url:  "https://zulip.example.com/#narrow/channel/615773-general/subject/old.20subject.20name",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "old subject name",
				Raw:       "https://zulip.example.com/#narrow/channel/615773-general/subject/old.20subject.20name",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "old subject name"},
				},
			},
		},
		{
			name: "path-based narrow URL without fragment",
			url:  "https://zulip.example.com/narrow/channel/615773-general/topic/test",
			want: NarrowTarget{
				Site:      "https://zulip.example.com",
				ChannelID: 615773,
				Channel:   "general",
				Topic:     "test",
				Raw:       "https://zulip.example.com/narrow/channel/615773-general/topic/test",
				Ops: []NarrowOp{
					{Op: "channel", Operand: int64(615773)},
					{Op: "topic", Operand: "test"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.url)
			if err != nil {
				t.Fatalf("ParseURL(%q) unexpected error: %v", tt.url, err)
			}
			if diff := cmp.Diff(tt.want, *got); diff != "" {
				t.Errorf("ParseURL(%q) mismatch (-want +got):\n%s", tt.url, diff)
			}
		})
	}
}

func TestParseURLErrors(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "empty string",
			url:     "",
			wantErr: "empty URL",
		},
		{
			name:    "whitespace only",
			url:     "   ",
			wantErr: "empty URL",
		},
		{
			name:    "no narrow fragment",
			url:     "https://zulip.example.com/help",
			wantErr: "no narrow fragment found in URL",
		},
		{
			name:    "empty narrow fragment hash only",
			url:     "https://zulip.example.com/#narrow/",
			wantErr: "empty narrow fragment",
		},
		{
			name:    "odd number of components",
			url:     "#narrow/channel/615773-general/topic",
			wantErr: "missing operand for operator",
		},
		{
			name:    "invalid message ID in with operator",
			url:     "#narrow/with/abc",
			wantErr: "invalid message ID in with operator",
		},
		{
			name:    "zero message ID in with operator",
			url:     "#narrow/with/0",
			wantErr: "invalid message ID in with operator",
		},
		{
			name:    "invalid message ID in near operator",
			url:     "#narrow/near/notanumber",
			wantErr: "invalid message ID in near operator",
		},
		{
			name:    "negative message ID in near operator",
			url:     "#narrow/near/-5",
			wantErr: "invalid message ID in near operator",
		},
		{
			name:    "invalid message ID in id operator",
			url:     "#narrow/id/xyz",
			wantErr: "invalid message ID in id operator",
		},
		{
			name:    "zero message ID in id operator",
			url:     "#narrow/id/0",
			wantErr: "invalid message ID in id operator",
		},
		{
			name:    "invalid hex encoding in topic",
			url:     "#narrow/topic/bad%zztopic",
			wantErr: "decoding topic operand",
		},
		{
			name:    "invalid hex encoding in channel name with ID",
			url:     "#narrow/channel/123-bad%zzchannel",
			wantErr: "decoding channel name",
		},
		{
			name:    "invalid hex encoding in channel name without ID",
			url:     "#narrow/channel/bad%zzchannel",
			wantErr: "decoding channel name",
		},
		{
			name:    "invalid hex encoding in sender",
			url:     "#narrow/sender/bad%zzsender",
			wantErr: "decoding sender operand",
		},
		{
			name:    "invalid hex encoding in search",
			url:     "#narrow/search/bad%zzsearch",
			wantErr: "decoding search operand",
		},
		{
			name:    "invalid hex encoding in is operator",
			url:     "#narrow/is/bad%zzis",
			wantErr: "decoding is operand",
		},
		{
			name:    "invalid hex encoding in has operator",
			url:     "#narrow/has/bad%zzhas",
			wantErr: "decoding has operand",
		},
		{
			name:    "invalid hex encoding in dm operator",
			url:     "#narrow/dm/bad%zzdm",
			wantErr: "decoding dm operand",
		},
		{
			name:    "invalid hex encoding in custom operator",
			url:     "#narrow/custom/bad%zzcustom",
			wantErr: "decoding custom operand",
		},
		{
			name:    "narrow word only fragment",
			url:     "#narrow",
			wantErr: "empty narrow fragment",
		},
		{
			name:    "malformed URL with control characters",
			url:     "https://example.com/foo\x7f#narrow/channel/1",
			wantErr: "parsing URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.url)
			if err == nil {
				t.Fatalf("ParseURL(%q) = %+v, want error containing %q", tt.url, got, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseURL(%q) error = %q, want error containing %q", tt.url, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNarrowTargetString(t *testing.T) {
	tests := []struct {
		name   string
		target NarrowTarget
		want   string
	}{
		{
			name: "channel topic anchor",
			target: NarrowTarget{
				Channel: "general",
				Topic:   "watchflakes",
				Anchor:  619610069,
			},
			want: "[general > watchflakes #619610069]",
		},
		{
			name: "channel topic without anchor",
			target: NarrowTarget{
				Channel: "general",
				Topic:   "watchflakes",
			},
			want: "[general > watchflakes]",
		},
		{
			name: "channel only",
			target: NarrowTarget{
				Channel: "general",
			},
			want: "[general]",
		},
		{
			name: "topic only",
			target: NarrowTarget{
				Topic: "watchflakes",
			},
			want: "[topic: watchflakes]",
		},
		{
			name: "search only",
			target: NarrowTarget{
				Search: "watchflakes",
			},
			want: "[search: watchflakes]",
		},
		{
			name: "anchor only",
			target: NarrowTarget{
				Anchor: 12345,
			},
			want: "[msg: 12345]",
		},
		{
			name:   "empty",
			target: NarrowTarget{},
			want:   "[narrow]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.target.String()
			if got != tt.want {
				t.Errorf("target.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNarrowTargetNarrowOpsAndJSON(t *testing.T) {
	// 1. Existing explicit Ops
	t1 := &NarrowTarget{
		Ops: []NarrowOp{
			{Op: "channel", Operand: int64(615773)},
			{Op: "topic", Operand: "closing stale watchflakes"},
		},
	}
	ops1 := t1.NarrowOps()
	if len(ops1) != 2 {
		t.Fatalf("len(ops1) = %d, want 2", len(ops1))
	}
	json1, err := t1.NarrowJSON()
	if err != nil {
		t.Fatalf("t1.NarrowJSON() unexpected error: %v", err)
	}
	var unmarshaled []NarrowOp
	if err := json.Unmarshal([]byte(json1), &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal unexpected error: %v", err)
	}
	wantUnmarshaled := []NarrowOp{
		{Op: "channel", Operand: float64(615773)},
		{Op: "topic", Operand: "closing stale watchflakes"},
	}
	if diff := cmp.Diff(wantUnmarshaled, unmarshaled); diff != "" {
		t.Errorf("unmarshaled Ops mismatch (-want +got):\n%s", diff)
	}

	// 2. Synthesized from fields
	t2 := &NarrowTarget{
		ChannelID: 100,
		Topic:     "my topic",
		Sender:    "alice@example.com",
		Search:    "query",
	}
	ops2 := t2.NarrowOps()
	wantOps2 := []NarrowOp{
		{Op: "channel", Operand: int64(100)},
		{Op: "topic", Operand: "my topic"},
		{Op: "sender", Operand: "alice@example.com"},
		{Op: "search", Operand: "query"},
	}
	if diff := cmp.Diff(wantOps2, ops2); diff != "" {
		t.Errorf("ops2 mismatch (-want +got):\n%s", diff)
	}

	// 3. Channel string without ChannelID
	t3 := &NarrowTarget{
		Channel: "dev",
	}
	ops3 := t3.NarrowOps()
	wantOps3 := []NarrowOp{
		{Op: "channel", Operand: "dev"},
	}
	if diff := cmp.Diff(wantOps3, ops3); diff != "" {
		t.Errorf("ops3 mismatch (-want +got):\n%s", diff)
	}

	// 4. Empty target JSON
	t4 := &NarrowTarget{}
	json4, err := t4.NarrowJSON()
	if err != nil {
		t.Fatalf("t4.NarrowJSON() unexpected error: %v", err)
	}
	if json4 != "[]" {
		t.Errorf("t4.NarrowJSON() = %q, want '[]'", json4)
	}
}

func TestNarrowJSONError(t *testing.T) {
	// Function value cannot be marshaled to JSON.
	target := &NarrowTarget{
		Ops: []NarrowOp{
			{Op: "invalid", Operand: func() {}},
		},
	}
	got, err := target.NarrowJSON()
	if err == nil {
		t.Fatalf("target.NarrowJSON() = %q, want error for unmarshalable type", got)
	}
}

func TestNarrowTargetWebURL(t *testing.T) {
	tests := []struct {
		name   string
		target NarrowTarget
		site   string
		want   string
	}{
		{
			name: "full target with site",
			target: NarrowTarget{
				Site:       "https://zulip.example.com",
				ChannelID:  615773,
				Channel:    "general",
				Topic:      "closing stale watchflakes",
				Anchor:     619610069,
				AnchorType: AnchorWith,
			},
			site: "",
			want: "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069",
		},
		{
			name: "override site argument",
			target: NarrowTarget{
				Site:       "https://wrong.example.com",
				ChannelID:  615773,
				Channel:    "general",
				Topic:      "watchflakes",
				Anchor:     123,
				AnchorType: AnchorNear,
			},
			site: "https://zulip.example.com",
			want: "https://zulip.example.com/#narrow/channel/615773-general/topic/watchflakes/near/123",
		},
		{
			name: "channel ID only without channel name",
			target: NarrowTarget{
				ChannelID: 100,
				Topic:     "test",
			},
			site: "https://example.com",
			want: "https://example.com/#narrow/channel/100/topic/test",
		},
		{
			name: "channel name only without channel ID",
			target: NarrowTarget{
				Channel: "general",
				Topic:   "test",
			},
			site: "https://example.com",
			want: "https://example.com/#narrow/channel/general/topic/test",
		},
		{
			name: "sender and search with empty anchor type defaulting to near",
			target: NarrowTarget{
				Sender: "alice@example.com",
				Search: "test query",
				Anchor: 456,
			},
			site: "https://example.com",
			want: "https://example.com/#narrow/sender/alice.40example.2Ecom/search/test.20query/near/456",
		},
		{
			name: "no site returns fragment only",
			target: NarrowTarget{
				Channel: "general",
				Topic:   "test",
			},
			site: "",
			want: "#narrow/channel/general/topic/test",
		},
		{
			name:   "site with no narrow parts returns site url",
			target: NarrowTarget{},
			site:   "https://zulip.example.com",
			want:   "https://zulip.example.com",
		},
		{
			name:   "empty target and empty site returns empty fragment",
			target: NarrowTarget{},
			site:   "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.target.WebURL(tt.site)
			if got != tt.want {
				t.Errorf("target.WebURL(%q) = %q, want %q", tt.site, got, tt.want)
			}
		})
	}
}

func TestFormatTopicURL(t *testing.T) {
	// With message ID
	got1 := FormatTopicURL("https://zulip.example.com", 615773, "general", "closing stale watchflakes", 619610069)
	want1 := "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069"
	if got1 != want1 {
		t.Errorf("FormatTopicURL(with msgID) = %q, want %q", got1, want1)
	}

	// Without message ID
	got2 := FormatTopicURL("https://zulip.example.com", 615773, "general", "closing stale watchflakes", 0)
	want2 := "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes"
	if got2 != want2 {
		t.Errorf("FormatTopicURL(without msgID) = %q, want %q", got2, want2)
	}
}

func TestIsZulipURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://zulip.example.com/#narrow/channel/615773-general/topic/test", true},
		{"http://localhost:9991/#narrow/stream/1-test", true},
		{"#narrow/channel/615773-general", true},
		{"/#narrow/channel/615773-general", true},
		{"narrow/channel/615773-general", true},
		{"/narrow/channel/615773-general", true},
		{"general", false},
		{"closing stale watchflakes", false},
		{"https://github.com/golang/go/issues/12345", false},
		{"", false},
		{"   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsZulipURL(tt.input)
			if got != tt.want {
				t.Errorf("IsZulipURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseNarrowFragmentEdgeCases(t *testing.T) {
	edgeCases := []string{
		"narrow",
		"#narrow",
		"narrow/",
		"#narrow/",
		"///",
		"narrow////",
		"   /   ",
		"narrow/   /   ",
		"narrow/   ",
	}
	for _, ec := range edgeCases {
		got, err := ParseNarrowFragment(ec)
		if err == nil {
			t.Errorf("ParseNarrowFragment(%q) = %+v, want error", ec, got)
		}
	}
}

func TestSynctestURLConcurrentParsing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const raw = "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069"
		var wg sync.WaitGroup
		results := make([]*NarrowTarget, 20)
		errs := make([]error, 20)

		for i := range 20 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				target, err := ParseURL(raw)
				results[idx] = target
				errs[idx] = err
			}(i)
		}

		synctest.Wait()
		wg.Wait()

		for i := range 20 {
			if errs[i] != nil {
				t.Fatalf("worker %d unexpected error: %v", i, errs[i])
			}
			if results[i].Channel != "general" {
				t.Errorf("worker %d Channel = %q, want %q", i, results[i].Channel, "general")
			}
			if results[i].Topic != "closing stale watchflakes" {
				t.Errorf("worker %d Topic = %q, want %q", i, results[i].Topic, "closing stale watchflakes")
			}
			if results[i].Anchor != 619610069 {
				t.Errorf("worker %d Anchor = %d, want %d", i, results[i].Anchor, 619610069)
			}
		}
	})
}
