// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "build" {
		runWrappedBuild(os.Args[1:])
		return
	}
	runRealGo(os.Args[1:], os.Environ())
}

func findModuleRoot(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func hasVCSDir(startDir string) bool {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

func parseTimestamp(tStr string) string {
	if tStr == "" {
		return "1700000000"
	}
	if _, err := strconv.ParseInt(tStr, 10, 64); err == nil {
		return tStr
	}
	// Clean up potential typo like "00:00:00:Z" -> "00:00:00Z"
	cleaned := tStr
	if len(tStr) > 2 && tStr[len(tStr)-2:] == ":Z" {
		cleaned = tStr[:len(tStr)-2] + "Z"
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, cleaned); err == nil {
			return strconv.FormatInt(t.Unix(), 10)
		}
	}
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func runWrappedBuild(buildArgs []string) {
	rev := os.Getenv("GO_VCS_REVISION")
	timeStr := os.Getenv("GO_VCS_TIME")
	tag := os.Getenv("GO_VCS_TAG")

	if rev == "" && timeStr == "" && tag == "" {
		runRealGo(buildArgs, os.Environ())
		return
	}

	unixTime := parseTimestamp(timeStr)

	cwd, _ := os.Getwd()
	modRoot := findModuleRoot(cwd)
	if modRoot == "" {
		modRoot = cwd
	}

	var cleanupDummyGit func()
	if !hasVCSDir(cwd) && !hasVCSDir(modRoot) {
		dummyGit := filepath.Join(modRoot, ".git")
		if err := os.Mkdir(dummyGit, 0755); err == nil {
			cleanupDummyGit = func() { os.Remove(dummyGit) }
			defer cleanupDummyGit()
		}
	}

	tmpDir, err := os.MkdirTemp("", "govcs-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "govcs: failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	goModPath := filepath.Join(modRoot, "go.mod")
	gitScript := fmt.Sprintf(`#!/bin/sh
H="%s"
T="%s"
V="%s"
GOMOD="%s"

case "$*" in
  *"status"*)   exit 0 ;;
  *"%%H:%%ct"*)   echo "$H:$T" ;;
  *"%%D"*)
    if [ -n "$V" ]; then
      printf "%%s %%s HEAD -> main, tag: %%s" "$H" "$T" "$V"
    else
      printf "%%s %%s HEAD -> main" "$H" "$T"
    fi
    exit 0
    ;;
  *"tag -l"*)   [ -n "$V" ] && echo "$V" ; exit 0 ;;
  *"cat-file"*) [ -f "$GOMOD" ] && cat "$GOMOD" ; exit 0 ;;
  *"config"*)   exit 1 ;;
esac
`, rev, unixTime, tag, goModPath)

	fakeGitPath := filepath.Join(tmpDir, "git")
	if err := os.WriteFile(fakeGitPath, []byte(gitScript), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "govcs: failed to write fake git: %v\n", err)
		os.Exit(1)
	}

	newEnv := append([]string{}, os.Environ()...)
	newEnv = append(newEnv, "PATH="+tmpDir+":"+os.Getenv("PATH"))

	args := append([]string{}, buildArgs...)
	hasBuildVCS := false
	for _, arg := range args {
		if len(arg) >= 9 && arg[:9] == "-buildvcs" {
			hasBuildVCS = true
			break
		}
	}
	if !hasBuildVCS {
		args = append([]string{"build", "-buildvcs=true"}, args[1:]...)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if cleanupDummyGit != nil {
			cleanupDummyGit()
		}
		os.RemoveAll(tmpDir)
		os.Exit(130)
	}()

	runRealGo(args, newEnv)
}

func runRealGo(args []string, env []string) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "govcs: cannot find go in PATH: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command(realGo, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
