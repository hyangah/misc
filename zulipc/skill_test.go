// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillPrint(t *testing.T) {
	t.Parallel()

	t.Run("default print", func(t *testing.T) {
		t.Parallel()
		var outBuf, errBuf bytes.Buffer
		err := runSkill(&outBuf, &errBuf, false, false, false, "")
		if err != nil {
			t.Fatalf("runSkill: %v", err)
		}

		out := outBuf.String()
		if !strings.HasPrefix(out, "---\nname: zulip\n") {
			t.Errorf("skill output missing frontmatter:\n%s", out[:min(len(out), 100)])
		}
		if !strings.Contains(out, "description: Read Zulip threads") {
			t.Errorf("skill output missing description")
		}
		if !strings.Contains(out, "# Reading Zulip threads and messages") {
			t.Errorf("skill output missing heading")
		}
		if !strings.Contains(out, "zulipc read") {
			t.Errorf("skill output missing zulipc read documentation")
		}
		if !strings.Contains(out, "zulipc search") {
			t.Errorf("skill output missing zulipc search documentation")
		}
		if !strings.Contains(out, "zulipc topics") {
			t.Errorf("skill output missing zulipc topics documentation")
		}
		if !strings.Contains(out, "zulipc channels") {
			t.Errorf("skill output missing zulipc channels documentation")
		}
		if !strings.Contains(out, "zulipc whoami") {
			t.Errorf("skill output missing zulipc whoami documentation")
		}
		if !strings.Contains(out, "ZULIP_CONFIG") {
			t.Errorf("skill output missing ZULIP_CONFIG documentation")
		}
	})

	t.Run("explicit print flag", func(t *testing.T) {
		t.Parallel()
		var outBuf, errBuf bytes.Buffer
		err := runSkill(&outBuf, &errBuf, true, false, false, "")
		if err != nil {
			t.Fatalf("runSkill: %v", err)
		}
		if outBuf.String() != skillText() {
			t.Errorf("runSkill with doPrint=true did not match skillText()")
		}
	})
}

func TestSkillInstallExplicitDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outDir := filepath.Join(dir, "skills", "zulip")

	var outBuf, errBuf bytes.Buffer
	err := runSkill(&outBuf, &errBuf, false, true, false, outDir)
	if err != nil {
		t.Fatalf("runSkill: %v", err)
	}

	skillFile := filepath.Join(outDir, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatalf("reading installed skill file: %v", err)
	}
	if string(data) != skillText() {
		t.Errorf("installed skill text does not match skillText()")
	}
	if !strings.Contains(outBuf.String(), "wrote "+skillFile) {
		t.Errorf("stdout = %q, want 'wrote %s'", outBuf.String(), skillFile)
	}

	// Overwrite existing ourSkill file should succeed
	outBuf.Reset()
	errBuf.Reset()
	err = runSkill(&outBuf, &errBuf, false, true, false, outDir)
	if err != nil {
		t.Fatalf("runSkill second time: %v", err)
	}
	if !strings.Contains(outBuf.String(), "wrote "+skillFile) {
		t.Errorf("stdout = %q, want 'wrote %s'", outBuf.String(), skillFile)
	}

	// If file was modified externally, do not overwrite
	if err := os.WriteFile(skillFile, []byte("custom user modified skill"), 0644); err != nil {
		t.Fatal(err)
	}
	outBuf.Reset()
	errBuf.Reset()
	err = runSkill(&outBuf, &errBuf, false, true, false, outDir)
	if err != nil {
		t.Fatalf("runSkill with modified file: %v", err)
	}
	if !strings.Contains(errBuf.String(), "not overwriting existing "+skillFile) {
		t.Errorf("stderr = %q, want 'not overwriting existing %s'", errBuf.String(), skillFile)
	}
}

func TestSkillInstallAgentDiscovery(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create markers for agents
	if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fakeHome, ".gemini"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fakeHome, ".codex"), 0755); err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	err := runSkill(&outBuf, &errBuf, false, true, false, "")
	if err != nil {
		t.Fatalf("runSkill -install: %v", err)
	}

	out := outBuf.String()
	claudePath := filepath.Join(fakeHome, ".claude", "skills", "zulip", "SKILL.md")
	geminiPath := filepath.Join(fakeHome, ".gemini", "config", "skills", "zulip", "SKILL.md")
	codexPath := filepath.Join(fakeHome, ".agents", "skills", "zulip", "SKILL.md")

	if !strings.Contains(out, "Claude Code: wrote "+claudePath) {
		t.Errorf("stdout missing Claude Code install:\n%s", out)
	}
	if !strings.Contains(out, "Antigravity: wrote "+geminiPath) {
		t.Errorf("stdout missing Antigravity install:\n%s", out)
	}
	if !strings.Contains(out, "Codex: wrote "+codexPath) {
		t.Errorf("stdout missing Codex install:\n%s", out)
	}

	// Verify files exist and match
	for _, p := range []string{claudePath, geminiPath, codexPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading %s: %v", p, err)
			continue
		}
		if string(data) != skillText() {
			t.Errorf("file %s content mismatch", p)
		}
	}

	// Test when file is modified, install reports not overwriting
	if err := os.WriteFile(claudePath, []byte("custom claude skill"), 0644); err != nil {
		t.Fatal(err)
	}
	outBuf.Reset()
	errBuf.Reset()
	err = runSkill(&outBuf, &errBuf, false, true, false, "")
	if err != nil {
		t.Fatalf("runSkill -install with custom file: %v", err)
	}
	if !strings.Contains(errBuf.String(), "not overwriting existing "+claudePath) {
		t.Errorf("stderr missing not overwriting message:\n%s", errBuf.String())
	}
}

func TestSkillInstallNoAgentsFound(t *testing.T) {
	emptyHome := t.TempDir()
	t.Setenv("HOME", emptyHome)

	var outBuf, errBuf bytes.Buffer
	err := runSkill(&outBuf, &errBuf, false, true, false, "")
	if err != nil {
		t.Fatalf("runSkill -install: %v", err)
	}

	if !strings.Contains(outBuf.String(), "no coding agents found in "+emptyHome) {
		t.Errorf("stdout = %q, want 'no coding agents found'", outBuf.String())
	}
}

func TestSkillInstallProject(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	if err := os.MkdirAll(filepath.Join(fakeHome, ".gemini"), 0755); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	var outBuf, errBuf bytes.Buffer
	err = runSkill(&outBuf, &errBuf, false, true, true, "")
	if err != nil {
		t.Fatalf("runSkill -install -project: %v", err)
	}

	projectSkillPath := filepath.Join(projectDir, ".agents", "skills", "zulip", "SKILL.md")
	data, err := os.ReadFile(projectSkillPath)
	if err != nil {
		t.Fatalf("reading project skill %s: %v", projectSkillPath, err)
	}
	if string(data) != skillText() {
		t.Errorf("project skill content mismatch")
	}
}

func TestSkillFlagValidation(t *testing.T) {
	t.Parallel()

	var outBuf, errBuf bytes.Buffer
	err := runSkill(&outBuf, &errBuf, true, true, false, "")
	if err == nil {
		t.Fatal("runSkill expected error for both -print and -install, got nil")
	}
	if !strings.Contains(err.Error(), "cannot use both -print and -install") {
		t.Errorf("error = %q, want 'cannot use both -print and -install'", err.Error())
	}
}

func TestUpdateSkills(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// No skills installed yet
	wrote, kept := updateSkills()
	if len(wrote) != 0 || len(kept) != 0 {
		t.Errorf("updateSkills on empty home: wrote=%v, kept=%v, want empty", wrote, kept)
	}

	// Install a skill and test updating
	skillDir := filepath.Join(fakeHome, ".claude", "skills", "zulip")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")

	// Same content -> nothing to update
	if err := os.WriteFile(skillPath, []byte(skillText()), 0644); err != nil {
		t.Fatal(err)
	}
	wrote, kept = updateSkills()
	if len(wrote) != 0 || len(kept) != 0 {
		t.Errorf("updateSkills on matching skill: wrote=%v, kept=%v, want empty", wrote, kept)
	}

	// Foreign content -> kept
	if err := os.WriteFile(skillPath, []byte("foreign modified content"), 0644); err != nil {
		t.Fatal(err)
	}
	wrote, kept = updateSkills()
	if len(wrote) != 0 || len(kept) != 1 {
		t.Errorf("updateSkills on foreign skill: wrote=%v, kept=%v, want 1 kept", wrote, kept)
	}
}

func TestUpdateSkillsWithKnownHash(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	skillDir := filepath.Join(fakeHome, ".claude", "skills", "zulip")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")

	oldText := "old skill version content"
	sum := sha256.Sum256([]byte(oldText))
	oldHash := hex.EncodeToString(sum[:])

	origKnown := skillManager.KnownSkills
	skillManager.KnownSkills = append(skillManager.KnownSkills, oldHash)
	defer func() { skillManager.KnownSkills = origKnown }()

	if err := os.WriteFile(skillPath, []byte(oldText), 0644); err != nil {
		t.Fatal(err)
	}

	wrote, kept := updateSkills()
	if len(wrote) != 1 || len(kept) != 0 {
		t.Errorf("updateSkills with known old skill: wrote=%v, kept=%v, want 1 wrote, 0 kept", wrote, kept)
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("reading updated skill: %v", err)
	}
	if string(data) != skillText() {
		t.Errorf("updated skill text mismatch")
	}
}

func TestWriteSkillError(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "a_file")
	if err := os.WriteFile(tempFile, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}

	invalidPath := filepath.Join(tempFile, "subdir")
	ok, err := skillManager.WriteToDir(invalidPath)
	if err == nil {
		t.Errorf("WriteToDir(%q) expected error, got nil", invalidPath)
	}
	if ok {
		t.Errorf("WriteToDir(%q) ok = true, want false", invalidPath)
	}

	var outBuf, errBuf bytes.Buffer
	err = runSkill(&outBuf, &errBuf, false, true, false, filepath.Join(tempFile, "subdir"))
	if err == nil {
		t.Errorf("runSkill with invalid outDir expected error, got nil")
	}

	// Test runSkill with invalid home path causing agent install error
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".gemini"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a file where .gemini/config should be
	if err := os.WriteFile(filepath.Join(fakeHome, ".gemini", "config"), []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	outBuf.Reset()
	errBuf.Reset()
	err = runSkill(&outBuf, &errBuf, false, true, false, "")
	if err == nil {
		t.Errorf("runSkill with unwriteable agent dir expected error, got nil")
	}
}

func TestSkillCLIExecution(t *testing.T) {
	t.Parallel()

	t.Run("skill default print cli", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runCLI(t, nil, "skill")
		if code != 0 {
			t.Fatalf("runCLI skill failed (code %d), stderr:\n%s", code, stderr)
		}
		if !strings.HasPrefix(stdout, "---\nname: zulip\n") {
			t.Errorf("stdout missing frontmatter:\n%s", stdout[:min(len(stdout), 100)])
		}
	})

	t.Run("skill -print cli", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runCLI(t, nil, "skill", "-print")
		if code != 0 {
			t.Fatalf("runCLI skill -print failed (code %d), stderr:\n%s", code, stderr)
		}
		if !strings.HasPrefix(stdout, "---\nname: zulip\n") {
			t.Errorf("stdout missing frontmatter:\n%s", stdout[:min(len(stdout), 100)])
		}
	})

	t.Run("skills alias cli", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runCLI(t, nil, "skills")
		if code != 0 {
			t.Fatalf("runCLI skills failed (code %d), stderr:\n%s", code, stderr)
		}
		if !strings.HasPrefix(stdout, "---\nname: zulip\n") {
			t.Errorf("stdout missing frontmatter:\n%s", stdout[:min(len(stdout), 100)])
		}
	})

	t.Run("skill -install -o dir cli", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		stdout, stderr, code := runCLI(t, nil, "skill", "-install", "-o", dir)
		if code != 0 {
			t.Fatalf("runCLI skill -install -o dir failed (code %d), stderr:\n%s", code, stderr)
		}
		skillFile := filepath.Join(dir, "SKILL.md")
		if !strings.Contains(stdout, "wrote "+skillFile) {
			t.Errorf("stdout = %q, want 'wrote %s'", stdout, skillFile)
		}
		data, err := os.ReadFile(skillFile)
		if err != nil {
			t.Fatalf("reading installed skill file: %v", err)
		}
		if string(data) != skillText() {
			t.Errorf("installed skill text does not match skillText()")
		}
	})

	t.Run("skill both -print and -install cli", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "skill", "-print", "-install")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "cannot use both -print and -install") {
			t.Errorf("stderr = %q, want 'cannot use both -print and -install'", stderr)
		}
	})

	t.Run("skill unexpected args cli", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runCLI(t, nil, "skill", "unexpected")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "unexpected arguments: unexpected") {
			t.Errorf("stderr = %q, want unexpected arguments error", stderr)
		}
	})

	t.Run("skill invalid flag cli", func(t *testing.T) {
		t.Parallel()
		_, _, code := runCLI(t, nil, "skill", "-invalid")
		if code == 0 {
			t.Errorf("exit code = %d, want non-zero", code)
		}
	})
}

func TestCmdSkillDirect(t *testing.T) {
	// Test cmdSkill flag parse error
	if err := cmdSkill([]string{"-invalid"}); err == nil {
		t.Error("cmdSkill with invalid flag expected error, got nil")
	}

	// Test cmdSkill unexpected arguments
	if err := cmdSkill([]string{"unexpected"}); err == nil {
		t.Error("cmdSkill with unexpected arguments expected error, got nil")
	}
}

func TestSkillSecurityNeverDeleteUserFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	userFile := filepath.Join(dir, "my_custom_config.json")
	if err := os.WriteFile(userFile, []byte("{\"user\": true}"), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, nil, "skill", "-install", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI failed (code %d), stderr:\n%s", code, stderr)
	}
	_ = stdout

	// Verify skill file was created
	skillFile := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Errorf("SKILL.md missing: %v", err)
	}

	// Verify user file is completely intact
	userData, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user file deleted: %v", err)
	}
	if string(userData) != "{\"user\": true}" {
		t.Errorf("user file modified: %s", string(userData))
	}
}

func TestSkillDiffAndForceCLI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Initial diff on uninstalled dir
	stdout, stderr, code := runCLI(t, nil, "skill", "-diff", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -diff failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "+---") && !strings.Contains(stdout, "+++") {
		t.Errorf("expected diff showing new skill file, got:\n%s", stdout)
	}

	// Install skill
	stdout, stderr, code = runCLI(t, nil, "skill", "-install", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -install failed (code %d), stderr:\n%s", code, stderr)
	}

	// Diff should now be up to date
	stdout, stderr, code = runCLI(t, nil, "skill", "-diff", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -diff failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "up to date (no differences)") {
		t.Errorf("expected up to date diff, got:\n%s", stdout)
	}

	// Modify skill file on disk
	skillFile := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("user modified content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Diff should show changes
	stdout, stderr, code = runCLI(t, nil, "skill", "-diff", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -diff failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "-user modified content") {
		t.Errorf("expected diff showing removed user content, got:\n%s", stdout)
	}

	// Install without force should refuse to overwrite
	stdout, stderr, code = runCLI(t, nil, "skill", "-install", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -install failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "not overwriting existing") || !strings.Contains(stderr, "-force") {
		t.Errorf("expected refusal mentioning -force, got stderr:\n%s", stderr)
	}

	// Install with -force should overwrite
	stdout, stderr, code = runCLI(t, nil, "skill", "-install", "-force", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -install -force failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "wrote "+skillFile) {
		t.Errorf("expected wrote output, got stdout:\n%s", stdout)
	}
	data, err := os.ReadFile(skillFile)
	if err != nil || string(data) != skillText() {
		t.Errorf("file after force install does not match skillText()")
	}

	// Modify again and install with -f (short alias)
	if err := os.WriteFile(skillFile, []byte("user modified again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, nil, "skill", "-install", "-f", "-o", dir)
	if code != 0 {
		t.Fatalf("runCLI skill -install -f failed (code %d), stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "wrote "+skillFile) {
		t.Errorf("expected wrote output for -f, got stdout:\n%s", stdout)
	}

	// Test conflict between -install and -diff
	stdout, stderr, code = runCLI(t, nil, "skill", "-install", "-diff", "-o", dir)
	if code == 0 {
		t.Error("expected error combining -install and -diff, got 0")
	}
}
