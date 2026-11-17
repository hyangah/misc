// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"embed"
	"io"
	"strings"

	"github.com/hyangah/misc/internal/skills"
)

//go:embed SKILL.md
var skillFS embed.FS

var skillManager = func() *skills.Manager {
	m := skills.NewManager("zulip", skillFS)
	m.ToolCommand = "zulipc"
	return m
}()

// skillText returns the full skill Markdown document.
func skillText() string {
	var buf strings.Builder
	_ = skillManager.Print(&buf)
	return buf.String()
}

func runSkill(wOut, wErr io.Writer, doPrint, doInstall, project bool, outDir string) error {
	return runSkillWithOptions(wOut, wErr, skills.RunOptions{
		Print:   doPrint,
		Install: doInstall,
		Project: project,
		OutDir:  outDir,
	})
}

func runSkillWithOptions(wOut, wErr io.Writer, opts skills.RunOptions) error {
	return skillManager.RunWithOptions(wOut, wErr, opts)
}

func updateSkills() (wrote, kept []string) {
	return skillManager.Update("")
}
