// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSkillsPrint(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{
			Data: []byte("# My Skill\n\nInstructions here.\n"),
		},
	}
	mgr := NewManager("testtool", fsys)

	var buf bytes.Buffer
	if err := mgr.Print(&buf); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if buf.String() != "# My Skill\n\nInstructions here.\n" {
		t.Errorf("Print output = %q", buf.String())
	}

	// Test nil FS
	nilMgr := NewManager("testtool", nil)
	if err := nilMgr.Print(&buf); err == nil {
		t.Error("Print with nil FS expected error, got nil")
	}

	// Test multi-file FS without SKILL.md
	multiFS := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("file A\n")},
		"b.txt": &fstest.MapFile{Data: []byte("file B\n")},
	}
	multiMgr := NewManager("multi", multiFS)
	buf.Reset()
	if err := multiMgr.Print(&buf); err != nil {
		t.Fatalf("Print multi-file: %v", err)
	}
	if !strings.Contains(buf.String(), "file A\n") || !strings.Contains(buf.String(), "file B\n") {
		t.Errorf("Print multi-file output = %q", buf.String())
	}
}

func TestSkillsWriteToDir(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{
			Data: []byte("skill data v1"),
		},
		"sub/helper.txt": &fstest.MapFile{
			Data: []byte("helper data"),
		},
	}
	mgr := NewManager("testtool", fsys)

	destDir := t.TempDir()
	ok, err := mgr.WriteToDir(destDir)
	if err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	if !ok {
		t.Error("WriteToDir ok = false, want true")
	}

	// Verify written files
	skillData, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	if err != nil || string(skillData) != "skill data v1" {
		t.Errorf("SKILL.md = %q, %v", string(skillData), err)
	}
	helperData, err := os.ReadFile(filepath.Join(destDir, "sub", "helper.txt"))
	if err != nil || string(helperData) != "helper data" {
		t.Errorf("sub/helper.txt = %q, %v", string(helperData), err)
	}

	// Overwrite with identical content succeeds
	ok, err = mgr.WriteToDir(destDir)
	if err != nil || !ok {
		t.Errorf("WriteToDir identical: ok=%v, err=%v", ok, err)
	}

	// Modify file externally -> refusal to overwrite
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("user modified"), 0644); err != nil {
		t.Fatal(err)
	}
	ok, err = mgr.WriteToDir(destDir)
	if err != nil {
		t.Fatalf("WriteToDir with modified file: %v", err)
	}
	if ok {
		t.Error("WriteToDir on modified file ok = true, want false")
	}

	// Nil FS test
	nilMgr := NewManager("nil", nil)
	if _, err := nilMgr.WriteToDir(destDir); err == nil {
		t.Error("WriteToDir nil FS expected error, got nil")
	}

	// Unwriteable path test
	filePathAsDir := filepath.Join(destDir, "SKILL.md", "impossible")
	if _, err := mgr.WriteToDir(filePathAsDir); err == nil {
		t.Error("WriteToDir unwriteable path expected error, got nil")
	}
}

func TestSkillsMultiFileLifecycle(t *testing.T) {
	t.Parallel()

	// 1. Initial version with multiple files and nested directories
	v1FS := fstest.MapFS{
		"SKILL.md":          &fstest.MapFile{Data: []byte("skill v1")},
		"references/doc.md": &fstest.MapFile{Data: []byte("doc v1")},
		"scripts/helper.sh": &fstest.MapFile{Data: []byte("helper v1")},
	}

	docV1Hash := hex.EncodeToString(sha256Sum([]byte("doc v1")))
	helperV1Hash := hex.EncodeToString(sha256Sum([]byte("helper v1")))
	skillV1Hash := hex.EncodeToString(sha256Sum([]byte("skill v1")))

	mgrV1 := NewManager("tool", v1FS)
	destDir := t.TempDir()

	// Initial install
	ok, err := mgrV1.WriteToDir(destDir)
	if err != nil || !ok {
		t.Fatalf("WriteToDir v1: ok=%v, err=%v", ok, err)
	}

	// Verify all files exist
	checkFile(t, filepath.Join(destDir, "SKILL.md"), "skill v1")
	checkFile(t, filepath.Join(destDir, "references", "doc.md"), "doc v1")
	checkFile(t, filepath.Join(destDir, "scripts", "helper.sh"), "helper v1")

	// Add a user's custom file that is NOT part of the skill
	userCustomPath := filepath.Join(destDir, "user_notes.txt")
	if err := os.WriteFile(userCustomPath, []byte("my notes"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Version 2:
	// - updates SKILL.md and references/doc.md
	// - adds examples/demo.json
	// - deletes scripts/helper.sh (obsolete known file)
	v2FS := fstest.MapFS{
		"SKILL.md":           &fstest.MapFile{Data: []byte("skill v2")},
		"references/doc.md":  &fstest.MapFile{Data: []byte("doc v2")},
		"examples/demo.json": &fstest.MapFile{Data: []byte("{\"demo\": true}")},
	}
	mgrV2 := NewManager("tool", v2FS)
	mgrV2.KnownSkills = []string{skillV1Hash, docV1Hash, helperV1Hash}

	ok, err = mgrV2.WriteToDir(destDir)
	if err != nil || !ok {
		t.Fatalf("WriteToDir v2: ok=%v, err=%v", ok, err)
	}

	// Verify updated files
	checkFile(t, filepath.Join(destDir, "SKILL.md"), "skill v2")
	checkFile(t, filepath.Join(destDir, "references", "doc.md"), "doc v2")
	checkFile(t, filepath.Join(destDir, "examples", "demo.json"), "{\"demo\": true}")

	// Verify deleted file is removed
	if _, err := os.Stat(filepath.Join(destDir, "scripts", "helper.sh")); !os.IsNotExist(err) {
		t.Errorf("scripts/helper.sh should have been deleted, err = %v", err)
	}
	// Verify empty scripts/ dir was cleaned up
	if _, err := os.Stat(filepath.Join(destDir, "scripts")); !os.IsNotExist(err) {
		t.Errorf("empty scripts/ dir should have been deleted, err = %v", err)
	}

	// Verify user's custom file was preserved
	checkFile(t, userCustomPath, "my notes")

	// 3. User modifies references/doc.md -> v3 update must refuse to overwrite
	if err := os.WriteFile(filepath.Join(destDir, "references", "doc.md"), []byte("user modified doc"), 0644); err != nil {
		t.Fatal(err)
	}
	v3FS := fstest.MapFS{
		"SKILL.md":          &fstest.MapFile{Data: []byte("skill v3")},
		"references/doc.md": &fstest.MapFile{Data: []byte("doc v3")},
	}
	mgrV3 := NewManager("tool", v3FS)
	mgrV3.KnownSkills = []string{skillV1Hash, docV1Hash, helperV1Hash}

	ok, err = mgrV3.WriteToDir(destDir)
	if err != nil {
		t.Fatalf("WriteToDir v3: err = %v", err)
	}
	if ok {
		t.Error("WriteToDir v3 should have returned ok = false for user-modified file")
	}
	// Verify references/doc.md kept user's content
	checkFile(t, filepath.Join(destDir, "references", "doc.md"), "user modified doc")
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func checkFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(data) != want {
		t.Errorf("file %s = %q, want %q", path, string(data), want)
	}
}

func TestSkillsUpdate(t *testing.T) {
	fakeHome := t.TempDir()

	oldSkillData := []byte("old skill content")
	sum := sha256.Sum256(oldSkillData)
	oldHash := hex.EncodeToString(sum[:])

	fsys := fstest.MapFS{
		"SKILL.md":          &fstest.MapFile{Data: []byte("new skill content")},
		"references/ref.md": &fstest.MapFile{Data: []byte("new ref content")},
	}
	mgr := NewManager("testtool", fsys)
	mgr.KnownSkills = []string{oldHash}

	// Set up installed old skill in claude
	claudeDir := filepath.Join(fakeHome, ".claude", "skills", "testtool")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	claudeSkill := filepath.Join(claudeDir, "SKILL.md")
	if err := os.WriteFile(claudeSkill, oldSkillData, 0644); err != nil {
		t.Fatal(err)
	}

	// Set up user-modified skill in gemini
	geminiDir := filepath.Join(fakeHome, ".gemini", "config", "skills", "testtool")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatal(err)
	}
	geminiSkill := filepath.Join(geminiDir, "SKILL.md")
	if err := os.WriteFile(geminiSkill, []byte("user modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	wrote, kept := mgr.Update(fakeHome)
	if len(wrote) != 1 || wrote[0] != claudeSkill {
		t.Errorf("Update wrote = %v, want [%s]", wrote, claudeSkill)
	}
	if len(kept) != 1 || kept[0] != geminiSkill {
		t.Errorf("Update kept = %v, want [%s]", kept, geminiSkill)
	}

	// Verify claude skill was updated and ref.md added
	checkFile(t, claudeSkill, "new skill content")
	checkFile(t, filepath.Join(claudeDir, "references", "ref.md"), "new ref content")

	// Running Update again touches nothing
	wrote, kept = mgr.Update(fakeHome)
	if len(wrote) != 0 || len(kept) != 1 {
		t.Errorf("second Update: wrote=%v, kept=%v", wrote, kept)
	}
}

func TestSkillsRun(t *testing.T) {
	fsys := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: []byte("# My Tool Skill\n")},
	}
	mgr := NewManager("testtool", fsys)

	t.Run("run print default", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, false, false, false, ""); err != nil {
			t.Fatalf("Run print default: %v", err)
		}
		if outBuf.String() != "# My Tool Skill\n" {
			t.Errorf("out = %q", outBuf.String())
		}
	})

	t.Run("run print explicit", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, true, false, false, ""); err != nil {
			t.Fatalf("Run print explicit: %v", err)
		}
		if outBuf.String() != "# My Tool Skill\n" {
			t.Errorf("out = %q", outBuf.String())
		}
	})

	t.Run("run flag error", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, true, true, false, ""); err == nil {
			t.Fatal("Run both print and install expected error, got nil")
		}
	})

	t.Run("run install outDir", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "target")
		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, false, true, false, outDir); err != nil {
			t.Fatalf("Run install outDir: %v", err)
		}
		if !strings.Contains(outBuf.String(), "wrote "+filepath.Join(outDir, "SKILL.md")) {
			t.Errorf("stdout = %q", outBuf.String())
		}
	})

	t.Run("run install agent discovery", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)

		if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(fakeHome, ".codex"), 0755); err != nil {
			t.Fatal(err)
		}

		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, false, true, false, ""); err != nil {
			t.Fatalf("Run install discovery: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Claude Code: wrote") {
			t.Errorf("stdout missing Claude Code: %s", outBuf.String())
		}
		if !strings.Contains(outBuf.String(), "Codex: wrote") {
			t.Errorf("stdout missing Codex: %s", outBuf.String())
		}
	})

	t.Run("run install no agents", func(t *testing.T) {
		emptyHome := t.TempDir()
		t.Setenv("HOME", emptyHome)

		var outBuf, errBuf bytes.Buffer
		if err := mgr.Run(&outBuf, &errBuf, false, true, false, ""); err != nil {
			t.Fatalf("Run install no agents: %v", err)
		}
		if !strings.Contains(outBuf.String(), "no coding agents found in "+emptyHome) {
			t.Errorf("stdout = %q", outBuf.String())
		}
	})

	t.Run("run install project", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0755); err != nil {
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
		if err := mgr.Run(&outBuf, &errBuf, false, true, true, ""); err != nil {
			t.Fatalf("Run install project: %v", err)
		}

		projectFile := filepath.Join(projectDir, ".claude", "skills", "testtool", "SKILL.md")
		data, err := os.ReadFile(projectFile)
		if err != nil || string(data) != "# My Tool Skill\n" {
			t.Errorf("project file = %q, %v", string(data), err)
		}
	})
}

func TestSecurityNeverDeleteUserFiles(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()

	// Populate destDir with critical user files across different directories
	userFiles := map[string]string{
		"notes.txt":                     "important personal notes",
		"empty.txt":                     "",
		"user_script.sh":                "#!/bin/bash\necho hello",
		"references/my_own_guide.md":    "# My custom reference guide",
		"custom/data/database.json":     "{\"users\": 100}",
		"deep/nested/sub/dir/config.rc": "custom_config=1",
	}
	for rel, content := range userFiles {
		p := filepath.Join(destDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Install a skill into this directory with KnownSkills containing dummy hashes
	mgr := NewManager("tool", fstest.MapFS{
		"SKILL.md":          &fstest.MapFile{Data: []byte("skill markdown content")},
		"references/ref.md": &fstest.MapFile{Data: []byte("skill reference content")},
	})
	// Even if KnownSkills has arbitrary hashes and empty hash
	mgr.KnownSkills = []string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // sha256("")
		"1111111111111111111111111111111111111111111111111111111111111111",
	}

	ok, err := mgr.WriteToDir(destDir)
	if err != nil || !ok {
		t.Fatalf("WriteToDir failed: ok=%v, err=%v", ok, err)
	}

	// Verify all user files remain completely untouched
	for rel, wantContent := range userFiles {
		p := filepath.Join(destDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("user file %s was deleted or missing: %v", rel, err)
			continue
		}
		if string(data) != wantContent {
			t.Errorf("user file %s content altered: got %q, want %q", rel, string(data), wantContent)
		}
	}

	// 2. Perform an Update cycle
	wrote, kept := mgr.Update(filepath.Dir(destDir))
	_ = wrote
	_ = kept

	// Re-verify all user files
	for rel, wantContent := range userFiles {
		p := filepath.Join(destDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("after Update: user file %s missing: %v", rel, err)
			continue
		}
		if string(data) != wantContent {
			t.Errorf("after Update: user file %s altered", rel)
		}
	}
}

func TestSecuritySymlinkProtection(t *testing.T) {
	t.Parallel()

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret_outside.txt")
	if err := os.WriteFile(outsideFile, []byte("sensitive content"), 0600); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	symlinkPath := filepath.Join(destDir, "linked_dir")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	mgr := NewManager("tool", fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: []byte("skill content")},
	})
	mgr.KnownSkills = []string{"someoldhash"}

	ok, err := mgr.WriteToDir(destDir)
	if err != nil || !ok {
		t.Fatalf("WriteToDir with symlink: ok=%v, err=%v", ok, err)
	}

	// Verify outside file was NOT touched or deleted
	data, err := os.ReadFile(outsideFile)
	if err != nil || string(data) != "sensitive content" {
		t.Errorf("outside file altered or deleted through symlink: %v", err)
	}
}

func TestSecurityPathTraversalFS(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	badFS := fstest.MapFS{
		"../escape.txt": &fstest.MapFile{Data: []byte("escaped")},
	}
	mgr := NewManager("badtool", badFS)

	ok, err := mgr.WriteToDir(destDir)
	if err == nil {
		t.Error("WriteToDir with traversal path expected error, got nil")
	}
	if ok {
		t.Error("WriteToDir with traversal path ok = true, want false")
	}
}

func TestUnifiedDiff(t *testing.T) {
	t.Parallel()

	t.Run("identical", func(t *testing.T) {
		d := unifiedDiff("a.txt", "b.txt", []byte("line1\nline2\n"), []byte("line1\nline2\n"))
		if d != "" {
			t.Errorf("expected empty diff, got:\n%s", d)
		}
	})

	t.Run("addition", func(t *testing.T) {
		d := unifiedDiff("/dev/null", "b.txt", nil, []byte("line1\nline2\n"))
		if !strings.Contains(d, "--- /dev/null") || !strings.Contains(d, "+++ b.txt") || !strings.Contains(d, "+line1\n+line2\n") {
			t.Errorf("unexpected addition diff:\n%s", d)
		}
	})

	t.Run("deletion", func(t *testing.T) {
		d := unifiedDiff("a.txt", "/dev/null", []byte("line1\nline2\n"), nil)
		if !strings.Contains(d, "--- a.txt") || !strings.Contains(d, "+++ /dev/null") || !strings.Contains(d, "-line1\n-line2\n") {
			t.Errorf("unexpected deletion diff:\n%s", d)
		}
	})

	t.Run("modification_and_no_newline", func(t *testing.T) {
		d := unifiedDiff("a.txt", "b.txt", []byte("hello"), []byte("world\n"))
		if !strings.Contains(d, "-hello\n\\ No newline at end of file") || !strings.Contains(d, "+world\n") {
			t.Errorf("unexpected modification diff:\n%s", d)
		}
	})
}

func TestDiffDirAndForceInstall(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: []byte("skill original content\n")},
	}
	mgr := NewManager("difftool", fsys)
	destDir := t.TempDir()

	// Initially not installed
	d, err := mgr.DiffDir(destDir)
	if err != nil {
		t.Fatalf("DiffDir uninstalled: %v", err)
	}
	if !strings.Contains(d, "+skill original content") {
		t.Errorf("unexpected DiffDir for uninstalled dir:\n%s", d)
	}

	// Install
	ok, err := mgr.WriteToDir(destDir)
	if err != nil || !ok {
		t.Fatalf("WriteToDir: ok=%v, err=%v", ok, err)
	}

	// Now diff should be empty
	d, err = mgr.DiffDir(destDir)
	if err != nil {
		t.Fatalf("DiffDir after install: %v", err)
	}
	if d != "" {
		t.Errorf("DiffDir after install expected empty, got:\n%s", d)
	}

	// User modifies SKILL.md
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("user modified content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Diff should show difference
	d, err = mgr.DiffDir(destDir)
	if err != nil {
		t.Fatalf("DiffDir after user edit: %v", err)
	}
	if !strings.Contains(d, "-user modified content") || !strings.Contains(d, "+skill original content") {
		t.Errorf("unexpected DiffDir after user edit:\n%s", d)
	}

	// WriteToDir without force fails
	ok, err = mgr.WriteToDir(destDir)
	if err != nil {
		t.Fatalf("WriteToDir modified: %v", err)
	}
	if ok {
		t.Error("WriteToDir modified ok = true, want false")
	}

	// WriteToDirForce with force=true succeeds
	ok, err = mgr.WriteToDirForce(destDir, true)
	if err != nil {
		t.Fatalf("WriteToDirForce: %v", err)
	}
	if !ok {
		t.Error("WriteToDirForce ok = false, want true")
	}

	// Verify content was overwritten
	content, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	if err != nil || string(content) != "skill original content\n" {
		t.Errorf("content after force = %q, want 'skill original content\\n'", string(content))
	}
}

func TestRunWithOptions(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{Data: []byte("managed skill\n")},
	}
	mgr := NewManager("tool", fsys)
	destDir := t.TempDir()

	var outBuf, errBuf bytes.Buffer

	// Conflict error
	err := mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Print: true, Install: true})
	if err == nil {
		t.Error("RunWithOptions print+install expected error, got nil")
	}
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Install: true, Diff: true})
	if err == nil {
		t.Error("RunWithOptions install+diff expected error, got nil")
	}

	// Diff on outDir
	outBuf.Reset()
	errBuf.Reset()
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Diff: true, OutDir: destDir})
	if err != nil {
		t.Fatalf("RunWithOptions -diff: %v", err)
	}
	if !strings.Contains(outBuf.String(), "+managed skill") {
		t.Errorf("unexpected diff output:\n%s", outBuf.String())
	}

	// Install to outDir
	outBuf.Reset()
	errBuf.Reset()
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Install: true, OutDir: destDir})
	if err != nil {
		t.Fatalf("RunWithOptions -install: %v", err)
	}
	if !strings.Contains(outBuf.String(), "wrote ") {
		t.Errorf("unexpected install output:\n%s", outBuf.String())
	}

	// Diff up to date
	outBuf.Reset()
	errBuf.Reset()
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Diff: true, OutDir: destDir})
	if err != nil {
		t.Fatalf("RunWithOptions -diff up to date: %v", err)
	}
	if !strings.Contains(outBuf.String(), "up to date (no differences)") {
		t.Errorf("unexpected diff output for up to date dir:\n%s", outBuf.String())
	}

	// Modify file and test force install
	_ = os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("modified\n"), 0644)
	outBuf.Reset()
	errBuf.Reset()
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Install: true, Force: false, OutDir: destDir})
	if err != nil {
		t.Fatalf("RunWithOptions -install no force: %v", err)
	}
	if !strings.Contains(errBuf.String(), "not overwriting existing") {
		t.Errorf("expected not overwriting notice, got:\n%s", errBuf.String())
	}

	outBuf.Reset()
	errBuf.Reset()
	err = mgr.RunWithOptions(&outBuf, &errBuf, RunOptions{Install: true, Force: true, OutDir: destDir})
	if err != nil {
		t.Fatalf("RunWithOptions -install with force: %v", err)
	}
	if !strings.Contains(outBuf.String(), "wrote ") {
		t.Errorf("expected wrote notice after force, got:\n%s", outBuf.String())
	}
}
