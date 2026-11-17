// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/google/go-cmp/cmp"
)

func TestWhoamiTextOutput(t *testing.T) {
	t.Parallel()

	client, err := NewClientWithHTTP(&Config{
		Email: "alex@example.com",
		Key:   "apikey123",
		Site:  "https://zulip.example.com",
		Path:  "/path/to/.zuliprc",
	}, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			switch req.URL.Path {
			case "/api/v1/users/me":
				rec.Header().Set("Content-Type", "application/json")
				json.NewEncoder(rec).Encode(User{
					UserID:   1001,
					Email:    "alex@example.com",
					FullName: "Alex Chen",
					Role:     200,
				})
			case "/api/v1/server_settings":
				rec.Header().Set("Content-Type", "application/json")
				json.NewEncoder(rec).Encode(ServerSettings{
					ZulipVersion:      "9.0",
					ZulipFeatureLevel: 250,
				})
			default:
				rec.WriteHeader(http.StatusNotFound)
			}
			return rec.Result(), nil
		}),
	})
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	var buf bytes.Buffer
	cfg := &Config{
		Email: "alex@example.com",
		Key:   "apikey123",
		Site:  "https://zulip.example.com",
		Path:  "/path/to/.zuliprc",
	}

	err = runWhoami(context.Background(), client, cfg, false, &buf)
	if err != nil {
		t.Fatalf("runWhoami: %v", err)
	}

	want := "Site:        https://zulip.example.com\n" +
		"User:        Alex Chen (alex@example.com)\n" +
		"Role:        Administrator\n" +
		"Server:      Zulip Server 9.0\n" +
		"Config:      /path/to/.zuliprc [api]\n"

	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("runWhoami text output mismatch (-want +got):\n%s", diff)
	}
}

func TestWhoamiJSONOutput(t *testing.T) {
	t.Parallel()

	client, err := NewClientWithHTTP(&Config{
		Email: "alice@example.com",
		Key:   "key456",
		Site:  "https://example.zulipchat.com",
		Path:  "/home/alice/.zuliprc",
	}, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			switch req.URL.Path {
			case "/api/v1/users/me":
				rec.Header().Set("Content-Type", "application/json")
				json.NewEncoder(rec).Encode(User{
					UserID:   2002,
					Email:    "alice@example.com",
					FullName: "Alice Smith",
					Role:     100,
					Timezone: "UTC",
				})
			case "/api/v1/server_settings":
				rec.Header().Set("Content-Type", "application/json")
				json.NewEncoder(rec).Encode(ServerSettings{
					ZulipVersion: "Zulip Server 10.0",
					RealmName:    "Alice Community",
				})
			default:
				rec.WriteHeader(http.StatusNotFound)
			}
			return rec.Result(), nil
		}),
	})
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	var buf bytes.Buffer
	cfg := &Config{
		Email: "alice@example.com",
		Key:   "key456",
		Site:  "https://example.zulipchat.com",
		Path:  "/home/alice/.zuliprc",
	}

	err = runWhoami(context.Background(), client, cfg, true, &buf)
	if err != nil {
		t.Fatalf("runWhoami: %v", err)
	}

	var result WhoamiResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshaling JSON output: %v\nOutput was:\n%s", err, buf.String())
	}

	if result.Site != "https://example.zulipchat.com" {
		t.Errorf("result.Site = %q, want https://example.zulipchat.com", result.Site)
	}
	if result.Role != "Owner" {
		t.Errorf("result.Role = %q, want Owner", result.Role)
	}
	if result.User == nil || result.User.FullName != "Alice Smith" {
		t.Errorf("result.User = %+v, want Alice Smith", result.User)
	}
	if result.Server == nil || result.Server.ZulipVersion != "Zulip Server 10.0" {
		t.Errorf("result.Server = %+v, want Zulip Server 10.0", result.Server)
	}
	if result.ConfigPath != "/home/alice/.zuliprc" {
		t.Errorf("result.ConfigPath = %q, want /home/alice/.zuliprc", result.ConfigPath)
	}
}

func TestWhoamiRolesAndVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		user       User
		settings   *ServerSettings
		cfgPath    string
		wantRole   string
		wantServer string
		wantConfig string
	}{
		{
			name: "owner role",
			user: User{
				UserID:   1,
				Email:    "owner@example.com",
				FullName: "Owner User",
				Role:     100,
			},
			settings:   &ServerSettings{ZulipVersion: "9.0"},
			wantRole:   "Owner",
			wantServer: "Zulip Server 9.0",
			wantConfig: "~/.zuliprc",
		},
		{
			name: "moderator role",
			user: User{
				UserID:   2,
				Email:    "mod@example.com",
				FullName: "Mod User",
				Role:     300,
			},
			settings:   &ServerSettings{ZulipVersion: "Zulip Server 8.0"},
			wantRole:   "Moderator",
			wantServer: "Zulip Server 8.0",
			wantConfig: "~/.zuliprc",
		},
		{
			name: "guest role",
			user: User{
				UserID:   3,
				Email:    "guest@example.com",
				FullName: "Guest User",
				Role:     600,
			},
			settings:   nil,
			wantRole:   "Guest",
			wantServer: "Unknown",
			wantConfig: "~/.zuliprc",
		},
		{
			name: "fallback role flags",
			user: User{
				UserID:       4,
				Email:        "fallback@example.com",
				FullName:     "Fallback User",
				Role:         0,
				IsAdmin:      true,
				ZulipVersion: "8.5",
			},
			settings:   nil,
			wantRole:   "Administrator",
			wantServer: "Zulip Server 8.5",
			wantConfig: "~/.zuliprc",
		},
		{
			name: "fallback guest flag",
			user: User{
				UserID:   5,
				Email:    "guestflag@example.com",
				FullName: "Guest Flag User",
				Role:     0,
				IsGuest:  true,
			},
			settings:   nil,
			wantRole:   "Guest",
			wantServer: "Unknown",
			wantConfig: "~/.zuliprc",
		},
		{
			name: "fallback default member",
			user: User{
				UserID:   6,
				Email:    "default@example.com",
				FullName: "Default Member",
				Role:     0,
			},
			settings:   nil,
			wantRole:   "Member",
			wantServer: "Unknown",
			wantConfig: "~/.zuliprc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClientWithHTTP(&Config{
				Email: tt.user.Email,
				Key:   "k",
				Site:  "https://example.com",
			}, &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					rec := httptest.NewRecorder()
					switch req.URL.Path {
					case "/api/v1/users/me":
						json.NewEncoder(rec).Encode(tt.user)
					case "/api/v1/server_settings":
						if tt.settings != nil {
							json.NewEncoder(rec).Encode(tt.settings)
						} else {
							rec.WriteHeader(http.StatusNotFound)
						}
					}
					return rec.Result(), nil
				}),
			})
			if err != nil {
				t.Fatalf("NewClientWithHTTP: %v", err)
			}

			var buf bytes.Buffer
			cfg := &Config{
				Email: tt.user.Email,
				Key:   "k",
				Site:  "https://example.com",
				Path:  tt.cfgPath,
			}

			err = runWhoami(context.Background(), client, cfg, false, &buf)
			if err != nil {
				t.Fatalf("runWhoami: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, "Role:        "+tt.wantRole) {
				t.Errorf("output missing role %q:\n%s", tt.wantRole, out)
			}
			if !strings.Contains(out, "Server:      "+tt.wantServer) {
				t.Errorf("output missing server %q:\n%s", tt.wantServer, out)
			}
			if !strings.Contains(out, "Config:      "+tt.wantConfig) {
				t.Errorf("output missing config %q:\n%s", tt.wantConfig, out)
			}
		})
	}
}

type errWriter struct{}

func (errWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write error")
}

func TestWhoamiJSONEncodeError(t *testing.T) {
	t.Parallel()

	client, err := NewClientWithHTTP(&Config{
		Email: "jsonerr@example.com",
		Key:   "k",
		Site:  "https://example.com",
	}, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			json.NewEncoder(rec).Encode(User{UserID: 1, Email: "jsonerr@example.com"})
			return rec.Result(), nil
		}),
	})
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	cfg := &Config{
		Email: "jsonerr@example.com",
		Key:   "k",
		Site:  "https://example.com",
	}

	err = runWhoami(context.Background(), client, cfg, true, errWriter{})
	if err == nil {
		t.Fatal("expected JSON encoding error, got nil")
	}
	if !strings.Contains(err.Error(), "encoding JSON") {
		t.Errorf("error = %q, want 'encoding JSON'", err.Error())
	}
}

func TestWhoamiErrorHandling(t *testing.T) {
	t.Parallel()

	client, err := NewClientWithHTTP(&Config{
		Email: "err@example.com",
		Key:   "badkey",
		Site:  "https://err.example.com",
	}, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(rec).Encode(APIError{
				StatusCode: 401,
				Result:     "error",
				Msg:        "Invalid API key",
				Code:       "UNAUTHORIZED",
			})
			return rec.Result(), nil
		}),
	})
	if err != nil {
		t.Fatalf("NewClientWithHTTP: %v", err)
	}

	var buf bytes.Buffer
	cfg := &Config{
		Email: "err@example.com",
		Key:   "badkey",
		Site:  "https://err.example.com",
	}

	err = runWhoami(context.Background(), client, cfg, false, &buf)
	if err == nil {
		t.Fatal("runWhoami expected error for 401 Unauthorized, got nil")
	}
	if !strings.Contains(err.Error(), "fetching user profile") {
		t.Errorf("error = %q, want 'fetching user profile'", err.Error())
	}
}

func TestWhoamiCLIExecution(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/me":
			json.NewEncoder(w).Encode(User{
				UserID:   999,
				Email:    "cli@example.com",
				FullName: "CLI Test User",
				Role:     400,
			})
		case "/api/v1/server_settings":
			json.NewEncoder(w).Encode(ServerSettings{
				ZulipVersion: "9.1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".zuliprc")
	content := "[api]\nemail = cli@example.com\nkey = secretkey\nsite = " + ts.URL + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("whoami text cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "whoami")
		if code != 0 {
			t.Fatalf("runCLI whoami failed (code %d), stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stdout, "Site:        "+ts.URL) {
			t.Errorf("stdout = %q, missing site", stdout)
		}
		if !strings.Contains(stdout, "User:        CLI Test User (cli@example.com)") {
			t.Errorf("stdout = %q, missing user", stdout)
		}
		if !strings.Contains(stdout, "Role:        Member") {
			t.Errorf("stdout = %q, missing role", stdout)
		}
		if !strings.Contains(stdout, "Server:      Zulip Server 9.1") {
			t.Errorf("stdout = %q, missing server", stdout)
		}
	})

	t.Run("whoami json cli", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "whoami", "-json")
		if code != 0 {
			t.Fatalf("runCLI whoami -json failed (code %d), stderr:\n%s", code, stderr)
		}
		var res WhoamiResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("invalid json output: %v\nstdout:\n%s", err, stdout)
		}
		if res.User == nil || res.User.Email != "cli@example.com" {
			t.Errorf("res.User = %+v, want cli@example.com", res.User)
		}
		if res.Role != "Member" {
			t.Errorf("res.Role = %q, want Member", res.Role)
		}
	})

	t.Run("whoami unexpected args", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "whoami", "unexpected")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected arguments: unexpected") {
			t.Errorf("stderr = %q, want unexpected arguments error", stderr)
		}
	})

	t.Run("whoami invalid flag", func(t *testing.T) {
		_, _, code := runCLI(t, []string{"ZULIP_CONFIG=" + cfgPath}, "whoami", "-invalid")
		if code == 0 {
			t.Errorf("exit code = %d, want non-zero", code)
		}
	})

	t.Run("whoami missing config", func(t *testing.T) {
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=" + filepath.Join(dir, "nonexistent")}, "whoami")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "no configuration found") {
			t.Errorf("stderr = %q, want no configuration found", stderr)
		}
	})
}

func TestSynctestWhoami(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := NewClientWithHTTP(&Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
			Path:  "/path/to/.zuliprc",
		}, &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				switch req.URL.Path {
				case "/api/v1/users/me":
					json.NewEncoder(rec).Encode(User{
						UserID:   1,
						Email:    "sync@example.com",
						FullName: "Sync User",
						Role:     200,
					})
				case "/api/v1/server_settings":
					json.NewEncoder(rec).Encode(ServerSettings{
						ZulipVersion: "9.0",
					})
				}
				return rec.Result(), nil
			}),
		})
		if err != nil {
			t.Fatalf("NewClientWithHTTP: %v", err)
		}

		var buf bytes.Buffer
		cfg := &Config{
			Email: "sync@example.com",
			Key:   "synckey",
			Site:  "https://sync.zulipchat.com",
			Path:  "/path/to/.zuliprc",
		}

		err = runWhoami(context.Background(), client, cfg, false, &buf)
		if err != nil {
			t.Fatalf("runWhoami: %v", err)
		}

		synctest.Wait()

		if !strings.Contains(buf.String(), "Sync User (sync@example.com)") {
			t.Errorf("output missing Sync User: %s", buf.String())
		}
	})
}

func TestCmdWhoamiErrors(t *testing.T) {
	// Test cmdWhoami flag parse error
	if err := cmdWhoami([]string{"-invalid"}); err == nil {
		t.Error("cmdWhoami with invalid flag expected error, got nil")
	}

	// Test cmdWhoami unexpected arguments
	if err := cmdWhoami([]string{"unexpected"}); err == nil {
		t.Error("cmdWhoami with unexpected arg expected error, got nil")
	}

	// Test cmdWhoami config load error
	t.Setenv("ZULIP_CONFIG", "/nonexistent/path")
	if err := cmdWhoami([]string{}); err == nil {
		t.Error("cmdWhoami with nonexistent config expected error, got nil")
	}
}
