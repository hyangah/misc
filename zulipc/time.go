// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FormatTimeInLocation formats a time.Time in a specified location with UTC reference.
// If utcOnly is true or location is UTC, it outputs "YYYY-MM-DD HH:MM UTC".
// Otherwise, it formats the local time and appends the UTC time:
// "YYYY-MM-DD HH:MM MST / HH:MM UTC" (or full UTC date if different from local date).
func FormatTimeInLocation(t time.Time, loc *time.Location, utcOnly bool) string {
	utc := t.UTC()
	if utcOnly {
		return utc.Format("2006-01-02 15:04 UTC")
	}
	if loc == nil {
		loc = time.Local
	}

	local := t.In(loc)
	_, locOffset := local.Zone()
	_, utcOffset := utc.Zone()
	if locOffset == utcOffset && (loc == time.UTC || local.Format("MST") == "UTC") {
		return utc.Format("2006-01-02 15:04 UTC")
	}

	locStr := local.Format("2006-01-02 15:04 MST")
	if local.Format("2006-01-02") == utc.Format("2006-01-02") {
		return fmt.Sprintf("%s / %s", locStr, utc.Format("15:04 UTC"))
	}
	return fmt.Sprintf("%s / %s", locStr, utc.Format("2006-01-02 15:04 UTC"))
}

// FormatTime formats a time.Time for display in thread messages using time.Local.
func FormatTime(t time.Time, utcOnly bool) string {
	return FormatTimeInLocation(t, time.Local, utcOnly)
}

// FormatTimestamp converts a Unix timestamp in seconds to a formatted string.
func FormatTimestamp(unixSec int64, utcOnly bool) string {
	return FormatTime(time.Unix(unixSec, 0), utcOnly)
}

// FormatRelativeTime formats a duration from a timestamp until now into a compact relative string
// (e.g. "just now", "5m ago", "2h ago", "3d ago", or "YYYY-MM-DD").
func FormatRelativeTime(t, now time.Time) string {
	if t.After(now) || now.Sub(t) < time.Minute {
		return "just now"
	}
	d := now.Sub(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	if d < 30*24*time.Hour {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("2006-01-02")
}

// ParseDuration parses a duration string, supporting standard Go durations (e.g. "30m", "2h")
// as well as days ("d", "day", "days") and weeks ("w", "week", "weeks").
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration string")
	}

	// Try standard time.ParseDuration first (handles single units like "2h", "30m", "100ms")
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	var total time.Duration
	runes := []rune(s)
	i := 0

	for i < len(runes) {
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}

		var numStr strings.Builder
		for i < len(runes) && ((runes[i] >= '0' && runes[i] <= '9') || runes[i] == '.') {
			numStr.WriteRune(runes[i])
			i++
		}
		if numStr.Len() == 0 {
			return 0, fmt.Errorf("invalid duration format %q", s)
		}
		val, err := strconv.ParseFloat(numStr.String(), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration number %q in %q: %w", numStr.String(), s, err)
		}

		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}

		var unitStr strings.Builder
		for i < len(runes) && ((runes[i] >= 'a' && runes[i] <= 'z') || (runes[i] >= 'A' && runes[i] <= 'Z') || runes[i] == 'µ') {
			unitStr.WriteRune(runes[i])
			i++
		}
		unit := strings.ToLower(unitStr.String())
		if unit == "" {
			return 0, fmt.Errorf("missing unit in duration %q", s)
		}

		switch unit {
		case "w", "week", "weeks":
			total += time.Duration(val * float64(7*24*time.Hour))
		case "d", "day", "days":
			total += time.Duration(val * float64(24*time.Hour))
		case "h", "hr", "hour", "hours":
			total += time.Duration(val * float64(time.Hour))
		case "m", "min", "minute", "minutes":
			total += time.Duration(val * float64(time.Minute))
		case "s", "sec", "second", "seconds":
			total += time.Duration(val * float64(time.Second))
		case "ms", "millisecond", "milliseconds":
			total += time.Duration(val * float64(time.Millisecond))
		case "us", "µs", "microsecond", "microseconds":
			total += time.Duration(val * float64(time.Microsecond))
		case "ns", "nanosecond", "nanoseconds":
			total += time.Duration(val * float64(time.Nanosecond))
		default:
			return 0, fmt.Errorf("unknown duration unit %q in %q", unit, s)
		}
	}

	return total, nil
}

// ParseTime parses a time or duration argument (used by -since and -until), which can be:
// - Relative duration: "24h", "7d", "30m", "2w" (computed as now - duration)
// - Unix timestamp (seconds): "1724854500"
// - RFC3339 / ISO8601: "2026-08-28T14:00:00Z", "2026-08-28T10:00:00-04:00"
// - Explicit timezone format: "2026-08-28 14:00:00 UTC", "2026-08-28 10:00:00 EDT"
// - Local datetime: "2026-08-28 14:00:00", "2026-08-28 14:00", "2026-08-28"
func ParseTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty time string")
	}

	// 1. Relative duration
	if d, err := ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d), nil
	}

	// 2. Unix timestamp in seconds
	if sec, err := strconv.ParseInt(s, 10, 64); err == nil && sec > 1000000000 {
		return time.Unix(sec, 0), nil
	}

	// 3. Formats with explicit timezone
	explicitLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z07:00",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04 -0700",
	}
	for _, layout := range explicitLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	// 4. Local datetime formats (interpreted in time.Local)
	localLayouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid time or duration %q: expected relative duration (e.g. 24h, 7d), date (2006-01-02), or timestamp (2006-01-02T15:04:05Z)", s)
}

// ParseSince parses a -since time argument. It is an alias for ParseTime.
func ParseSince(s string, now time.Time) (time.Time, error) {
	return ParseTime(s, now)
}
