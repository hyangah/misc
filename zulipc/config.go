// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Config represents Zulip API credentials and server settings loaded from .zuliprc.
type Config struct {
	Email    string `json:"email"`
	Key      string `json:"key"`
	Site     string `json:"site"`
	Insecure bool   `json:"insecure,omitempty"`
	Path     string `json:"path,omitempty"`
}

// ConfigNotFoundError is returned when no .zuliprc file could be found.
type ConfigNotFoundError struct {
	Path string
}

func (e *ConfigNotFoundError) Error() string {
	displayPath := e.Path
	if displayPath == "" {
		displayPath = "~/.zuliprc"
	}
	return fmt.Sprintf(`no configuration found at %s

To configure zulipc:
  1. Open your Zulip organization in a browser
  2. Navigate to: Settings (Gear icon) -> Personal settings -> Account & privacy -> API key
  3. Click "Download .zuliprc" (or "Show/change your API key")
  4. Save the file to %s (permissions 0600):
     [api]
     email = your-email@example.com
     key   = your-api-key
     site  = https://your-org.zulipchat.com

  Alternatively, set ZULIP_SITE to your Zulip server URL instead of
  putting site in the file.`, displayPath, displayPath)
}

// stripComment removes inline comments starting with '#' or ';' unless quoted.
func stripComment(s string) string {
	var inSingle, inDouble bool
	for i, r := range s {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#', ';':
			if !inSingle && !inDouble {
				return s[:i]
			}
		}
	}
	return s
}

// unquoteValue strips surrounding quotes from an INI value.
func unquoteValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
			(strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func lookupFirstMatch(dict map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := dict[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// ParseConfig parses a Zulip .zuliprc configuration from an io.Reader.
func ParseConfig(r io.Reader) (*Config, error) {
	return parseConfig(r, "")
}

// parseConfig parses a .zuliprc configuration from r.
// If siteOverride is non-empty, it replaces the site value from the file.
func parseConfig(r io.Reader, siteOverride string) (*Config, error) {
	scanner := bufio.NewScanner(r)
	sections := make(map[string]map[string]string)
	currentSection := ""
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		rawLine := scanner.Text()
		// Strip UTF-8 BOM on first line if present.
		if lineNum == 1 {
			rawLine = strings.TrimPrefix(rawLine, "\ufeff")
		}

		line := strings.TrimSpace(stripComment(rawLine))
		if line == "" {
			continue
		}

		// Section header: [section]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			secName := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if secName == "" {
				return nil, fmt.Errorf("line %d: empty section name", lineNum)
			}
			currentSection = secName
			if sections[currentSection] == nil {
				sections[currentSection] = make(map[string]string)
			}
			continue
		}

		// Key-Value pair separated by '=' or ':'
		sepIdx := strings.IndexAny(line, "=:")
		if sepIdx < 0 {
			return nil, fmt.Errorf("line %d: invalid format (missing '=' or ':'): %q", lineNum, rawLine)
		}

		key := strings.ToLower(strings.TrimSpace(line[:sepIdx]))
		val := unquoteValue(line[sepIdx+1:])

		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNum)
		}

		if sections[currentSection] == nil {
			sections[currentSection] = make(map[string]string)
		}
		sections[currentSection][key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Select active section: prefer [api], then [default], then [zulip], then root, then single section.
	var sec map[string]string
	if s, ok := sections["api"]; ok && len(s) > 0 {
		sec = s
	} else if s, ok := sections["default"]; ok && len(s) > 0 {
		sec = s
	} else if s, ok := sections["zulip"]; ok && len(s) > 0 {
		sec = s
	} else if s, ok := sections[""]; ok && len(s) > 0 {
		sec = s
	} else if len(sections) == 1 {
		for _, s := range sections {
			sec = s
		}
	} else if len(sections) == 0 {
		return nil, errors.New("empty configuration file")
	} else {
		return nil, errors.New("no [api] section found in configuration")
	}

	email := lookupFirstMatch(sec, "email", "username", "user")
	key := lookupFirstMatch(sec, "key", "api_key", "apikey", "api-key")
	site := lookupFirstMatch(sec, "site", "url", "domain")
	if siteOverride != "" {
		site = siteOverride
	}

	// Validate required fields
	if email == "" {
		return nil, errors.New(`missing required field "email" in config`)
	}
	if key == "" {
		return nil, errors.New(`missing required field "key" in config`)
	}
	if site == "" {
		return nil, errors.New(`missing required field "site" in config (or set ZULIP_SITE)`)
	}

	// Normalize site
	site = strings.TrimRight(site, "/")
	if !strings.HasPrefix(site, "http://") && !strings.HasPrefix(site, "https://") {
		site = "https://" + site
	}

	// Parse insecure
	var insecure bool
	if insecStr, ok := sec["insecure"]; ok && insecStr != "" {
		insecLower := strings.ToLower(insecStr)
		switch insecLower {
		case "true", "1", "yes", "y", "t":
			insecure = true
		case "false", "0", "no", "n", "f":
			insecure = false
		default:
			return nil, fmt.Errorf("invalid boolean for insecure: %q", insecStr)
		}
	}

	return &Config{
		Email:    email,
		Key:      key,
		Site:     site,
		Insecure: insecure,
	}, nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func configCandidates() []string {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".zuliprc"))
	}
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "zulip", "zuliprc"))
	}
	candidates = append(candidates, ".zuliprc")
	return candidates
}

// LoadConfig loads the Zulip configuration.
// If customPath is non-empty, it expands ~ and loads from that path.
// If customPath is empty, it checks the ZULIP_CONFIG environment variable.
// If ZULIP_CONFIG is not set, it checks candidate locations (~/.zuliprc, UserConfigDir path, .zuliprc).
// If the ZULIP_SITE environment variable is set, it overrides the site in the file.
func LoadConfig(customPath string) (*Config, error) {
	siteOverride := os.Getenv("ZULIP_SITE")
	if customPath == "" {
		if env := os.Getenv("ZULIP_CONFIG"); env != "" {
			customPath = env
		}
	}

	if customPath != "" {
		path, err := expandHome(customPath)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, &ConfigNotFoundError{Path: customPath}
			}
			return nil, err
		}
		defer f.Close()
		cfg, err := parseConfig(f, siteOverride)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		cfg.Path = path
		return cfg, nil
	}

	candidates := configCandidates()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			cfg, err := parseConfig(f, siteOverride)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			cfg.Path = p
			return cfg, nil
		}
	}

	return nil, &ConfigNotFoundError{Path: "~/.zuliprc"}
}
