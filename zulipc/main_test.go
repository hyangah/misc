// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMainProcess is the subprocess entrypoint used by tests to execute zulipc CLI commands.
func TestMainProcess(t *testing.T) {
	if os.Getenv("TEST_ZULIPC_MAIN") == "1" {
		var args []string
		if jsonArgs := os.Getenv("TEST_ZULIPC_ARGS_JSON"); jsonArgs != "" {
			if err := json.Unmarshal([]byte(jsonArgs), &args); err != nil {
				fmt.Fprintf(os.Stderr, "unmarshaling args: %v\n", err)
				os.Exit(2)
			}
		}
		os.Args = append([]string{"zulipc"}, args...)
		main()
		// Explicitly exit with code 0. If TestMainProcess returns normally,
		// the go test framework continues and prints "PASS" to stdout, which
		// pollutes stdout in CLI subprocess tests.
		os.Exit(0)
	}
}

// runCLI executes the zulipc CLI in a subprocess with the given arguments and environment variables.
func runCLI(t *testing.T, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(exe, "-test.run=^TestMainProcess$")
	cmd.Env = append(os.Environ(), "TEST_ZULIPC_MAIN=1", "TEST_ZULIPC_ARGS_JSON="+string(argsJSON), "ZULIP_SITE=")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("running subprocess: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestRunUsageAndHelp(t *testing.T) {
	t.Parallel()

	t.Run("no arguments", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil)
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "usage:") {
			t.Errorf("stderr = %q, want usage output", stderr)
		}
		if !strings.Contains(stderr, "no command specified") {
			t.Errorf("stderr = %q, want 'no command specified'", stderr)
		}
	})

	helpFlags := []string{"help", "-h", "-help", "--help"}
	for _, h := range helpFlags {
		t.Run("help flag "+h, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runCLI(t, nil, h)
			if code != 0 {
				t.Errorf("runCLI(%q) exit code = %d, want 0", h, code)
			}
			if !strings.Contains(stderr, "usage:") {
				t.Errorf("runCLI(%q) stderr = %q, want usage output", h, stderr)
			}
			for _, cmd := range []string{"read", "search", "catchup", "topics", "channels", "whoami", "skill"} {
				if !strings.Contains(stderr, cmd) {
					t.Errorf("runCLI(%q) stderr missing command %q", h, cmd)
				}
			}
		})
	}

	t.Run("unknown command", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "nonexistent")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unknown command \"nonexistent\"") {
			t.Errorf("stderr = %q, want unknown command error", stderr)
		}
	})

	t.Run("unexpected top level flag", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "-config", "foo")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected top-level flag \"-config\"") {
			t.Errorf("stderr = %q, want unexpected top-level flag error", stderr)
		}
	})
}

func TestSubcommandFlagErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "read invalid flag",
			args: []string{"read", "-invalidflag"},
		},
		{
			name: "search invalid flag",
			args: []string{"search", "-invalidflag"},
		},
		{
			name: "topics invalid flag",
			args: []string{"topics", "-invalidflag"},
		},
		{
			name: "channels invalid flag",
			args: []string{"channels", "-invalidflag"},
		},
		{
			name: "catchup invalid flag",
			args: []string{"catchup", "-invalidflag"},
		},
		{
			name: "digest invalid flag",
			args: []string{"digest", "-invalidflag"},
		},
		{
			name: "whoami invalid flag",
			args: []string{"whoami", "-invalidflag"},
		},
		{
			name: "skill invalid flag",
			args: []string{"skill", "-invalidflag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, code := runCLI(t, nil, tt.args...)
			if code == 0 {
				t.Errorf("runCLI(%v) exit code = %d, want non-zero", tt.args, code)
			}
		})
	}
}

func TestSubcommandHelpFlags(t *testing.T) {
	t.Parallel()

	commands := []struct {
		cmd  string
		desc string
	}{
		{"read", "Read messages from a thread URL or channel and topic."},
		{"search", "Search messages across channels and topics."},
		{"catchup", "Fetch and group messages with local read tracking."},
		{"digest", "Fetch and group messages with local read tracking."},
		{"topics", "List topics in a channel."},
		{"channels", "List subscribed or public channels."},
		{"whoami", "Show current user identity and server details."},
		{"skill", "Output or install the agent skill definition."},
	}
	for _, tc := range commands {
		t.Run(tc.cmd+" -help", func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runCLI(t, nil, tc.cmd, "-help")
			if code != 0 {
				t.Errorf("runCLI(%q, \"-help\") exit code = %d, want 0", tc.cmd, code)
			}
			if !strings.Contains(stderr, "usage: zulipc "+tc.cmd) {
				t.Errorf("usage output missing expected format:\n%s", stderr)
			}
			if !strings.Contains(stderr, tc.desc) {
				t.Errorf("usage output missing expected description %q:\n%s", tc.desc, stderr)
			}
		})
	}
}

func TestMainProcessSubtests(t *testing.T) {
	t.Parallel()

	t.Run("help flag exit 0", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "help")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if !strings.Contains(stderr, "usage:") {
			t.Errorf("stderr = %q, want usage", stderr)
		}
	})

	t.Run("generic unknown command exit 1", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "unknown_command")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: unknown command \"unknown_command\"") {
			t.Errorf("stderr = %q, want unknown command error", stderr)
		}
	})

	t.Run("unexpected top level flag exit 1", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "-invalid")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: unexpected top-level flag \"-invalid\"") {
			t.Errorf("stderr = %q, want unexpected top-level flag error", stderr)
		}
	})

	t.Run("missing config error output exit 1", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, []string{"ZULIP_CONFIG=/nonexistent/path/.zuliprc"}, "whoami")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "zulipc: no configuration found at /nonexistent/path/.zuliprc") {
			t.Errorf("stderr = %q, want formatted missing config error", stderr)
		}
	})
}

func TestRunFunctionDirect(t *testing.T) {
	// Directly test run function for help exit path
	if err := run([]string{"help"}); err != nil {
		t.Errorf("run([\"help\"]) error = %v, want nil", err)
	}
}
