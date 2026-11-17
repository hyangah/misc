// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
)

func TestParseConfigValid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCfg Config
	}{
		{
			name: "standard api section",
			input: `[api]
email = alice@example.com
key = 32charhexapikeyabcdef0123456789
site = https://zulip.example.com
insecure = false
`,
			wantCfg: Config{
				Email:    "alice@example.com",
				Key:      "32charhexapikeyabcdef0123456789",
				Site:     "https://zulip.example.com",
				Insecure: false,
			},
		},
		{
			name: "trailing slash and no scheme in site",
			input: `[api]
email = bob@example.com
key = secretkey123
site = zulip.example.com/
insecure = true
`,
			wantCfg: Config{
				Email:    "bob@example.com",
				Key:      "secretkey123",
				Site:     "https://zulip.example.com",
				Insecure: true,
			},
		},
		{
			name: "http site and boolean variants",
			input: `[api]
email = charlie@example.com
key = key456
site = http://zulip.local:9991/
insecure = 1
`,
			wantCfg: Config{
				Email:    "charlie@example.com",
				Key:      "key456",
				Site:     "http://zulip.local:9991",
				Insecure: true,
			},
		},
		{
			name: "quoted values and inline comments",
			input: `# Configuration file
[api]
email = "dave@example.com" # primary email
key = 'key789#withhash' ; inline semicolon comment
site = "https://zulip.example.com" # site url
insecure = "false"
`,
			wantCfg: Config{
				Email:    "dave@example.com",
				Key:      "key789#withhash",
				Site:     "https://zulip.example.com",
				Insecure: false,
			},
		},
		{
			name:  "colon separator and utf8 bom",
			input: "\ufeff[api]\nemail: eve@example.com\nkey: evekey\nsite: https://eve.zulipchat.com\n",
			wantCfg: Config{
				Email:    "eve@example.com",
				Key:      "evekey",
				Site:     "https://eve.zulipchat.com",
				Insecure: false,
			},
		},
		{
			name: "field aliases username and api_key and domain",
			input: `[default]
username = frank@example.com
api_key = frankkey
domain = https://frank.zulipchat.com
insecure = yes
`,
			wantCfg: Config{
				Email:    "frank@example.com",
				Key:      "frankkey",
				Site:     "https://frank.zulipchat.com",
				Insecure: true,
			},
		},
		{
			name: "field aliases user and apikey and url without section",
			input: `user = grace@example.com
apikey = gracekey
url = https://grace.zulipchat.com
insecure = y
`,
			wantCfg: Config{
				Email:    "grace@example.com",
				Key:      "gracekey",
				Site:     "https://grace.zulipchat.com",
				Insecure: true,
			},
		},
		{
			name: "single arbitrary section",
			input: `[custom_section]
email = heidi@example.com
api-key = heidikey
site = https://heidi.zulipchat.com
insecure = t
`,
			wantCfg: Config{
				Email:    "heidi@example.com",
				Key:      "heidikey",
				Site:     "https://heidi.zulipchat.com",
				Insecure: true,
			},
		},
		{
			name: "zulip section name",
			input: `[zulip]
email = ivan@example.com
key = ivankey
site = https://ivan.zulipchat.com
insecure = 0
`,
			wantCfg: Config{
				Email:    "ivan@example.com",
				Key:      "ivankey",
				Site:     "https://ivan.zulipchat.com",
				Insecure: false,
			},
		},
		{
			name: "insecure false variants",
			input: `[api]
email = judy@example.com
key = judykey
site = https://judy.zulipchat.com
insecure = no
`,
			wantCfg: Config{
				Email:    "judy@example.com",
				Key:      "judykey",
				Site:     "https://judy.zulipchat.com",
				Insecure: false,
			},
		},
		{
			name: "insecure f variant",
			input: `[api]
email = mallory@example.com
key = mallorykey
site = https://mallory.zulipchat.com
insecure = f
`,
			wantCfg: Config{
				Email:    "mallory@example.com",
				Key:      "mallorykey",
				Site:     "https://mallory.zulipchat.com",
				Insecure: false,
			},
		},
		{
			name: "insecure n variant",
			input: `[api]
email = oscar@example.com
key = oscarkey
site = https://oscar.zulipchat.com
insecure = n
`,
			wantCfg: Config{
				Email:    "oscar@example.com",
				Key:      "oscarkey",
				Site:     "https://oscar.zulipchat.com",
				Insecure: false,
			},
		},
		{
			name: "unicode characters in values and comments",
			input: `# 注释: Zulip 設定ファイル
[api]
email = "ユーザー@example.com" # ユーザー名
key = 'ключ-🔑-✨-çàé#hash' ; コメント
site = "https://zulip.example.com" # サイトURL 🌐
insecure = false
`,
			wantCfg: Config{
				Email:    "ユーザー@example.com",
				Key:      "ключ-🔑-✨-çàé#hash",
				Site:     "https://zulip.example.com",
				Insecure: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("ParseConfig() unexpected error: %v", err)
			}
			if cfg.Email != tt.wantCfg.Email {
				t.Errorf("Email = %q, want %q", cfg.Email, tt.wantCfg.Email)
			}
			if cfg.Key != tt.wantCfg.Key {
				t.Errorf("Key = %q, want %q", cfg.Key, tt.wantCfg.Key)
			}
			if cfg.Site != tt.wantCfg.Site {
				t.Errorf("Site = %q, want %q", cfg.Site, tt.wantCfg.Site)
			}
			if cfg.Insecure != tt.wantCfg.Insecure {
				t.Errorf("Insecure = %v, want %v", cfg.Insecure, tt.wantCfg.Insecure)
			}
		})
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "empty config",
			input:   "",
			wantErr: "empty configuration file",
		},
		{
			name:    "comments only",
			input:   "# just a comment\n; another comment\n",
			wantErr: "empty configuration file",
		},
		{
			name: "empty section name",
			input: `[]
email = a@b.com
key = 123
site = https://example.com
`,
			wantErr: "empty section name",
		},
		{
			name: "invalid line format without separator",
			input: `[api]
this is not valid
`,
			wantErr: "invalid format",
		},
		{
			name: "empty key",
			input: `[api]
= value
`,
			wantErr: "empty key",
		},
		{
			name: "missing email",
			input: `[api]
key = 123
site = https://example.com
`,
			wantErr: `missing required field "email"`,
		},
		{
			name: "missing key",
			input: `[api]
email = a@b.com
site = https://example.com
`,
			wantErr: `missing required field "key"`,
		},
		{
			name: "missing site",
			input: `[api]
email = a@b.com
key = 123
`,
			wantErr: `missing required field "site"`,
		},
		{
			name: "invalid insecure boolean",
			input: `[api]
email = a@b.com
key = 123
site = https://example.com
insecure = notabool
`,
			wantErr: "invalid boolean for insecure",
		},
		{
			name: "multiple sections none matching api/default/zulip",
			input: `[section1]
email = a@b.com
key = 123
site = https://example.com

[section2]
email = b@c.com
key = 456
site = https://example2.com
`,
			wantErr: "no [api] section found in configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig(strings.NewReader(tt.input))
			if err == nil {
				t.Fatalf("ParseConfig() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseConfig() error = %q, want error containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadConfigFile(t *testing.T) {
	t.Setenv("ZULIP_SITE", "")
	dir := t.TempDir()
	validPath := filepath.Join(dir, ".zuliprc")
	content := `[api]
email = testuser@example.com
key = 9876543210abcdef9876543210abcdef
site = https://chat.example.com
insecure = false
`
	if err := os.WriteFile(validPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// 1. Explicit valid path
	cfg, err := LoadConfig(validPath)
	if err != nil {
		t.Fatalf("LoadConfig(%q) unexpected error: %v", validPath, err)
	}
	if cfg.Email != "testuser@example.com" || cfg.Site != "https://chat.example.com" {
		t.Errorf("LoadConfig(%q) = %+v, unexpected fields", validPath, cfg)
	}
	if cfg.Path != validPath {
		t.Errorf("cfg.Path = %q, want %q", cfg.Path, validPath)
	}

	// 2. Explicit non-existent path
	missingPath := filepath.Join(dir, "nonexistent.zuliprc")
	_, err = LoadConfig(missingPath)
	if err == nil {
		t.Fatal("LoadConfig(missingPath) expected error, got nil")
	}
	notFound, ok := errors.AsType[*ConfigNotFoundError](err)
	if !ok {
		t.Fatalf("expected ConfigNotFoundError, got %T: %v", err, err)
	}
	if notFound.Path != missingPath {
		t.Errorf("notFound.Path = %q, want %q", notFound.Path, missingPath)
	}

	// 3. Syntax error in explicit file
	badPath := filepath.Join(dir, "bad.zuliprc")
	if err := os.WriteFile(badPath, []byte("invalid content here"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadConfig(badPath)
	if err == nil || !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("LoadConfig(badPath) error = %v, want syntax error", err)
	}

	// 4. Default candidates in working directory
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(dir)
	cfgDefault, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") from working dir unexpected error: %v", err)
	}
	if cfgDefault.Email != "testuser@example.com" {
		t.Errorf("cfgDefault.Email = %q, want testuser@example.com", cfgDefault.Email)
	}

	// 5. Default candidates in ~/.zuliprc
	userZuliprc := filepath.Join(homeDir, ".zuliprc")
	homeContent := `[api]
email = homedir@example.com
key = homekey123
site = https://home.zulipchat.com
`
	if err := os.WriteFile(userZuliprc, []byte(homeContent), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // change to directory with no .zuliprc
	cfgHome, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") from home dir unexpected error: %v", err)
	}
	if cfgHome.Email != "homedir@example.com" {
		t.Errorf("cfgHome.Email = %q, want homedir@example.com", cfgHome.Email)
	}

	// 6. Default candidates in XDG_CONFIG_HOME
	xdgDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdgZulipDir := filepath.Join(xdgDir, "zulip")
	if err := os.MkdirAll(xdgZulipDir, 0700); err != nil {
		t.Fatal(err)
	}
	xdgContent := `[api]
email = xdg@example.com
key = xdgkey123
site = https://xdg.zulipchat.com
`
	if err := os.WriteFile(filepath.Join(xdgZulipDir, "zuliprc"), []byte(xdgContent), 0600); err != nil {
		t.Fatal(err)
	}
	cfgXDG, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") from XDG dir unexpected error: %v", err)
	}
	if cfgXDG.Email != "xdg@example.com" {
		t.Errorf("cfgXDG.Email = %q, want xdg@example.com", cfgXDG.Email)
	}

	// 7. ZULIP_CONFIG environment variable
	envCfgPath := filepath.Join(dir, "env.zuliprc")
	envContent := `[api]
email = envuser@example.com
key = envkey123
site = https://env.zulipchat.com
`
	if err := os.WriteFile(envCfgPath, []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZULIP_CONFIG", envCfgPath)
	cfgEnv, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") with ZULIP_CONFIG unexpected error: %v", err)
	}
	if cfgEnv.Email != "envuser@example.com" {
		t.Errorf("cfgEnv.Email = %q, want envuser@example.com", cfgEnv.Email)
	}

	// 8. ZULIP_CONFIG pointing to missing file
	missingEnvPath := filepath.Join(dir, "nonexistent-env.zuliprc")
	t.Setenv("ZULIP_CONFIG", missingEnvPath)
	_, err = LoadConfig("")
	if err == nil {
		t.Fatal("LoadConfig(\"\") with missing ZULIP_CONFIG expected error, got nil")
	}
	notFound, ok = errors.AsType[*ConfigNotFoundError](err)
	if !ok {
		t.Fatalf("expected ConfigNotFoundError, got %T: %v", err, err)
	}
	if notFound.Path != missingEnvPath {
		t.Errorf("notFound.Path = %q, want %q", notFound.Path, missingEnvPath)
	}
	t.Setenv("ZULIP_CONFIG", "")

	// 9. Default candidates missing
	emptyDir := t.TempDir()
	t.Chdir(emptyDir)
	t.Setenv("HOME", emptyDir)
	t.Setenv("XDG_CONFIG_HOME", emptyDir)
	_, err = LoadConfig("")
	if err == nil {
		t.Fatal("LoadConfig(\"\") in empty dir expected error, got nil")
	}
	notFound, ok = errors.AsType[*ConfigNotFoundError](err)
	if !ok {
		t.Fatalf("expected ConfigNotFoundError, got %T: %v", err, err)
	}
	if notFound.Path != "~/.zuliprc" {
		t.Errorf("notFound.Path = %q, want ~/.zuliprc", notFound.Path)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	got, err := expandHome("~")
	if err != nil || got != home {
		t.Errorf("expandHome(\"~\") = (%q, %v), want (%q, nil)", got, err, home)
	}

	got, err = expandHome("~/.zuliprc")
	want := filepath.Join(home, ".zuliprc")
	if err != nil || got != want {
		t.Errorf("expandHome(\"~/.zuliprc\") = (%q, %v), want (%q, nil)", got, err, want)
	}

	got, err = expandHome("/tmp/config")
	if err != nil || got != "/tmp/config" {
		t.Errorf("expandHome(\"/tmp/config\") = (%q, %v), want (\"/tmp/config\", nil)", got, err)
	}
}

func TestConfigNotFoundErrorOutput(t *testing.T) {
	err := &ConfigNotFoundError{Path: "~/.zuliprc"}
	msg := err.Error()
	if !strings.Contains(msg, "no configuration found at ~/.zuliprc") {
		t.Errorf("error message missing header:\n%s", msg)
	}
	if !strings.Contains(msg, "Save the file to ~/.zuliprc (permissions 0600):") {
		t.Errorf("error message missing save instructions:\n%s", msg)
	}
	if !strings.Contains(msg, "email = your-email@example.com") {
		t.Errorf("error message missing sample config:\n%s", msg)
	}

	// Empty path defaults to ~/.zuliprc
	errEmpty := &ConfigNotFoundError{}
	if !strings.Contains(errEmpty.Error(), "no configuration found at ~/.zuliprc") {
		t.Errorf("empty path error message missing default path:\n%s", errEmpty.Error())
	}

	// Custom path reflected in message
	errCustom := &ConfigNotFoundError{Path: "/custom/path/.zuliprc"}
	if !strings.Contains(errCustom.Error(), "no configuration found at /custom/path/.zuliprc") {
		t.Errorf("custom path not reflected in error:\n%s", errCustom.Error())
	}
}

func TestSynctestConfigConcurrentLoading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const configData = `[api]
email = synctest@example.com
key = synckey123456
site = https://sync.zulipchat.com
insecure = false
`
		var wg sync.WaitGroup
		results := make([]*Config, 10)
		errs := make([]error, 10)

		for i := range 10 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				cfg, err := ParseConfig(strings.NewReader(configData))
				results[idx] = cfg
				errs[idx] = err
			}(i)
		}

		synctest.Wait()
		wg.Wait()

		for i := range 10 {
			if errs[i] != nil {
				t.Fatalf("worker %d failed: %v", i, errs[i])
			}
			if results[i].Email != "synctest@example.com" {
				t.Errorf("worker %d got email %q, want synctest@example.com", i, results[i].Email)
			}
		}
	})
}

type errReader struct{}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func TestParseConfigReaderError(t *testing.T) {
	_, err := ParseConfig(errReader{})
	if err == nil || !strings.Contains(err.Error(), "reading config") {
		t.Errorf("ParseConfig(errReader) error = %v, want reading config error", err)
	}
}

func TestLoadConfigCandidateParseError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
	badContent := "invalid line without separator\n"
	if err := os.WriteFile(filepath.Join(dir, ".zuliprc"), []byte(badContent), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig("")
	if err == nil || !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("LoadConfig(\"\") error = %v, want invalid format error", err)
	}
}

func TestLoadConfigUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable.zuliprc")
	if err := os.WriteFile(unreadable, []byte("[api]\n"), 0000); err != nil {
		t.Fatal(err)
	}
	// In some test runners (running as root), chmod 0000 can still be read.
	// So we also test with a directory as config path.
	_, err := LoadConfig(unreadable)
	if err == nil {
		// Try directory instead
		_, err = LoadConfig(dir)
	}
	if err == nil {
		t.Errorf("LoadConfig on unreadable/directory path expected error, got nil")
	}
}

func TestLoadConfigSiteEnv(t *testing.T) {
	dir := t.TempDir()

	t.Run("overrides site in file", func(t *testing.T) {
		path := filepath.Join(dir, "with-site")
		content := "[api]\nemail = a@example.com\nkey = k\nsite = https://file.example.com\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ZULIP_SITE", "env.example.com/")
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig(%q) unexpected error: %v", path, err)
		}
		if got, want := cfg.Site, "https://env.example.com"; got != want {
			t.Errorf("cfg.Site = %q, want %q", got, want)
		}
	})

	t.Run("file without site", func(t *testing.T) {
		path := filepath.Join(dir, "no-site")
		content := "[api]\nemail = a@example.com\nkey = k\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ZULIP_SITE", "https://env.example.com")
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig(%q) unexpected error: %v", path, err)
		}
		if got, want := cfg.Site, "https://env.example.com"; got != want {
			t.Errorf("cfg.Site = %q, want %q", got, want)
		}

		t.Setenv("ZULIP_SITE", "")
		if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "ZULIP_SITE") {
			t.Errorf("LoadConfig(%q) with empty ZULIP_SITE error = %v, want error mentioning ZULIP_SITE", path, err)
		}
	})
}
