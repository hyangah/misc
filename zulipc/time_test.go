// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestFormatTime(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// 2026-08-28 14:15:00 UTC = 2026-08-28 10:15:00 EDT
	fixedTime := time.Date(2026, 8, 28, 14, 15, 0, 0, time.UTC)

	t.Run("utcOnly_true", func(t *testing.T) {
		got := FormatTimeInLocation(fixedTime, ny, true)
		want := "2026-08-28 14:15 UTC"
		if got != want {
			t.Errorf("FormatTimeInLocation(..., true) = %q, want %q", got, want)
		}
	})

	t.Run("utc_location", func(t *testing.T) {
		got := FormatTimeInLocation(fixedTime, time.UTC, false)
		want := "2026-08-28 14:15 UTC"
		if got != want {
			t.Errorf("FormatTimeInLocation(UTC, false) = %q, want %q", got, want)
		}
	})

	t.Run("same_day_local_and_utc", func(t *testing.T) {
		got := FormatTimeInLocation(fixedTime, ny, false)
		want := "2026-08-28 10:15 EDT / 14:15 UTC"
		if got != want {
			t.Errorf("FormatTimeInLocation(ny, false) = %q, want %q", got, want)
		}
	})

	t.Run("different_day_local_and_utc", func(t *testing.T) {
		// 2026-08-28 23:30 EDT = 2026-08-29 03:30 UTC
		lateTime := time.Date(2026, 8, 28, 23, 30, 0, 0, ny)
		got := FormatTimeInLocation(lateTime, ny, false)
		want := "2026-08-28 23:30 EDT / 2026-08-29 03:30 UTC"
		if got != want {
			t.Errorf("FormatTimeInLocation(lateTime, ny, false) = %q, want %q", got, want)
		}
	})

	t.Run("nil_location_defaults_to_local", func(t *testing.T) {
		got := FormatTimeInLocation(fixedTime, nil, false)
		if !strings.Contains(got, "UTC") {
			t.Errorf("FormatTimeInLocation(nil) = %q, want UTC component", got)
		}
	})

	t.Run("default_format_time", func(t *testing.T) {
		got := FormatTime(fixedTime, false)
		if !strings.Contains(got, "UTC") {
			t.Errorf("FormatTime() = %q, want UTC component", got)
		}
	})
}

func TestFormatTimestamp(t *testing.T) {
	t.Parallel()

	// 1724854500 = 2024-08-28 14:15:00 UTC
	got := FormatTimestamp(1724854500, true)
	want := "2024-08-28 14:15 UTC"
	if got != want {
		t.Errorf("FormatTimestamp(1724854500, true) = %q, want %q", got, want)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{
			name: "future_time",
			t:    now.Add(5 * time.Minute),
			want: "just now",
		},
		{
			name: "under_one_minute",
			t:    now.Add(-30 * time.Second),
			want: "just now",
		},
		{
			name: "minutes_ago",
			t:    now.Add(-15 * time.Minute),
			want: "15m ago",
		},
		{
			name: "hours_ago",
			t:    now.Add(-4 * time.Hour),
			want: "4h ago",
		},
		{
			name: "days_ago",
			t:    now.Add(-6 * 24 * time.Hour),
			want: "6d ago",
		},
		{
			name: "months_ago",
			t:    now.Add(-45 * 24 * time.Hour),
			want: "2026-07-16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRelativeTime(tt.t, now)
			if got != tt.want {
				t.Errorf("FormatRelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{input: "30s", want: 30 * time.Second},
		{input: "15m", want: 15 * time.Minute},
		{input: "2h", want: 2 * time.Hour},
		{input: "500ms", want: 500 * time.Millisecond},
		{input: "100us", want: 100 * time.Microsecond},
		{input: "100µs", want: 100 * time.Microsecond},
		{input: "100ns", want: 100 * time.Nanosecond},
		{input: "1d", want: 24 * time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "30d", want: 30 * 24 * time.Hour},
		{input: "1.5d", want: 36 * time.Hour},
		{input: "1w", want: 7 * 24 * time.Hour},
		{input: "2w", want: 14 * 24 * time.Hour},
		{input: "4weeks", want: 28 * 24 * time.Hour},
		{input: "2days", want: 48 * time.Hour},
		{input: "3hours", want: 3 * time.Hour},
		{input: "10minutes", want: 10 * time.Minute},
		{input: "45seconds", want: 45 * time.Second},
		{input: "2d12h", want: 60 * time.Hour},
		{input: "1w2d", want: 9 * 24 * time.Hour},
		{input: "1d 2h 30m", want: 26*time.Hour + 30*time.Minute},
		{input: "1d 2h ", want: 26 * time.Hour},
		{input: " 2h ", want: 2 * time.Hour},
		{input: "1 hr", want: time.Hour},
		{input: "2 min", want: 2 * time.Minute},
		{input: "5 sec", want: 5 * time.Second},
		{input: "50 millisecond", want: 50 * time.Millisecond},
		{input: "50 microsecond", want: 50 * time.Microsecond},
		{input: "50 nanosecond", want: 50 * time.Nanosecond},
		// Errors
		{input: "", wantErr: true},
		{input: "   ", wantErr: true},
		{input: "invalid", wantErr: true},
		{input: "123", wantErr: true},
		{input: "10xyz", wantErr: true},
		{input: "..d", wantErr: true},
		{input: "1.2.3d", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDuration(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSince(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	t.Run("relative_duration", func(t *testing.T) {
		got, err := ParseSince("24h", now)
		if err != nil {
			t.Fatalf("ParseSince(24h): %v", err)
		}
		want := now.Add(-24 * time.Hour)
		if !got.Equal(want) {
			t.Errorf("ParseSince(24h) = %v, want %v", got, want)
		}

		got7d, err := ParseSince("7d", now)
		if err != nil {
			t.Fatalf("ParseSince(7d): %v", err)
		}
		want7d := now.Add(-7 * 24 * time.Hour)
		if !got7d.Equal(want7d) {
			t.Errorf("ParseSince(7d) = %v, want %v", got7d, want7d)
		}
	})

	t.Run("unix_timestamp", func(t *testing.T) {
		got, err := ParseSince("1724854500", now)
		if err != nil {
			t.Fatalf("ParseSince(1724854500): %v", err)
		}
		want := time.Unix(1724854500, 0)
		if !got.Equal(want) {
			t.Errorf("ParseSince(1724854500) = %v, want %v", got, want)
		}
	})

	t.Run("rfc3339_utc", func(t *testing.T) {
		got, err := ParseSince("2026-08-28T14:00:00Z", now)
		if err != nil {
			t.Fatalf("ParseSince: %v", err)
		}
		want := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("rfc3339_offset", func(t *testing.T) {
		got, err := ParseSince("2026-08-28T10:00:00-04:00", now)
		if err != nil {
			t.Fatalf("ParseSince: %v", err)
		}
		want := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("explicit_zone_formats", func(t *testing.T) {
		layouts := []string{
			"2026-08-28 14:00:00 UTC",
			"2026-08-28 14:00 UTC",
			"2026-08-28 10:00:00 -0400",
			"2026-08-28 10:00 -0400",
		}
		want := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
		for _, layout := range layouts {
			got, err := ParseSince(layout, now)
			if err != nil {
				t.Fatalf("ParseSince(%q): %v", layout, err)
			}
			if !got.Equal(want) {
				t.Errorf("ParseSince(%q) = %v, want %v", layout, got, want)
			}
		}
	})

	t.Run("local_datetime_formats", func(t *testing.T) {
		layouts := []string{
			"2026-08-28 14:00:00",
			"2026-08-28 14:00",
			"2026-08-28T14:00:00",
			"2026-08-28T14:00",
			"2026/08/28 14:00:00",
			"2026/08/28 14:00",
		}
		want := time.Date(2026, 8, 28, 14, 0, 0, 0, time.Local)
		for _, layout := range layouts {
			got, err := ParseSince(layout, now)
			if err != nil {
				t.Fatalf("ParseSince(%q): %v", layout, err)
			}
			if !got.Equal(want) {
				t.Errorf("ParseSince(%q) = %v, want %v", layout, got, want)
			}
		}
	})

	t.Run("local_date_only", func(t *testing.T) {
		got, err := ParseSince("2026-08-28", now)
		if err != nil {
			t.Fatalf("ParseSince(2026-08-28): %v", err)
		}
		want := time.Date(2026, 8, 28, 0, 0, 0, 0, time.Local)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}

		gotSlash, err := ParseSince("2026/08/28", now)
		if err != nil {
			t.Fatalf("ParseSince(2026/08/28): %v", err)
		}
		if !gotSlash.Equal(want) {
			t.Errorf("got %v, want %v", gotSlash, want)
		}
	})

	t.Run("error_cases", func(t *testing.T) {
		invalidInputs := []string{
			"",
			"   ",
			"invalid-date",
			"2026-13-45",
			"2026-08-28 99:99",
			"not a time",
		}
		for _, input := range invalidInputs {
			_, err := ParseSince(input, now)
			if err == nil {
				t.Errorf("ParseSince(%q) expected error, got nil", input)
			}
		}
	})
}

func TestParseTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	got, err := ParseTime("2h", now)
	if err != nil {
		t.Fatalf("ParseTime(2h): %v", err)
	}
	want := now.Add(-2 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("ParseTime(2h) = %v, want %v", got, want)
	}
}

func TestSynctestTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
		var wg sync.WaitGroup
		errs := make([]error, 20)

		for i := range 20 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				input := fmt.Sprintf("%dh", idx+1)
				parsed, err := ParseSince(input, now)
				if err != nil {
					errs[idx] = err
					return
				}
				expected := now.Add(-time.Duration(idx+1) * time.Hour)
				if !parsed.Equal(expected) {
					errs[idx] = fmt.Errorf("idx %d: got %v, want %v", idx, parsed, expected)
					return
				}
				formatted := FormatTime(parsed, true)
				if !strings.Contains(formatted, "UTC") {
					errs[idx] = fmt.Errorf("idx %d: formatted %q missing UTC", idx, formatted)
				}
			}(i)
		}

		synctest.Wait()
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("worker %d failed: %v", i, err)
			}
		}
	})
}
