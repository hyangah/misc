// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WhoamiResult represents the structured output of the whoami command.
type WhoamiResult struct {
	Site       string          `json:"site"`
	User       *User           `json:"user"`
	Role       string          `json:"role"`
	Server     *ServerSettings `json:"server,omitempty"`
	ConfigPath string          `json:"config_path"`
}

// runWhoami executes the whoami command logic.
func runWhoami(ctx context.Context, client *Client, cfg *Config, asJSON bool, w io.Writer) error {
	user, err := client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("fetching user profile: %w", err)
	}

	settings, _ := client.GetServerSettings(ctx)

	configPath := cfg.Path
	if configPath == "" {
		configPath = "~/.zuliprc"
	}

	roleName := user.RoleName()

	if asJSON {
		res := WhoamiResult{
			Site:       cfg.Site,
			User:       user,
			Role:       roleName,
			Server:     settings,
			ConfigPath: configPath,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	serverVer := "Unknown"
	if settings != nil && settings.ZulipVersion != "" {
		serverVer = settings.ZulipVersion
	} else if user.ZulipVersion != "" {
		serverVer = user.ZulipVersion
	}

	if serverVer != "Unknown" && !strings.HasPrefix(serverVer, "Zulip Server") {
		serverVer = "Zulip Server " + serverVer
	}

	fmt.Fprintf(w, "%-12s %s\n", "Site:", cfg.Site)
	fmt.Fprintf(w, "%-12s %s (%s)\n", "User:", user.FullName, user.Email)
	fmt.Fprintf(w, "%-12s %s\n", "Role:", roleName)
	fmt.Fprintf(w, "%-12s %s\n", "Server:", serverVer)
	fmt.Fprintf(w, "%-12s %s [api]\n", "Config:", configPath)

	return nil
}
