// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package skills provides utilities for embedding, printing, and installing
// coding agent skill specifications into local agent directories.
package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AgentTool defines the destination path and detection marker for a coding agent.
type AgentTool struct {
	Name         string
	Marker       string // Directory under $HOME indicating the tool is in use (e.g. ".claude")
	SkillDir     string // Relative path to destination skill folder from $HOME
	ProjectSkill string // Relative path to destination skill folder from project root
}

// DefaultAgentTools returns the standard agent tool configurations for the given skill name.
func DefaultAgentTools(skillName string) []AgentTool {
	return []AgentTool{
		{
			Name:         "Claude Code",
			Marker:       ".claude",
			SkillDir:     filepath.Join(".claude", "skills", skillName),
			ProjectSkill: filepath.Join(".claude", "skills", skillName),
		},
		{
			Name:         "Antigravity",
			Marker:       ".gemini",
			SkillDir:     filepath.Join(".gemini", "config", "skills", skillName),
			ProjectSkill: filepath.Join(".agents", "skills", skillName),
		},
		{
			Name:         "Codex",
			Marker:       ".codex",
			SkillDir:     filepath.Join(".agents", "skills", skillName),
			ProjectSkill: filepath.Join(".agents", "skills", skillName),
		},
	}
}

// Manager manages printing, installing, and updating skill files from an embedded fs.FS.
type Manager struct {
	SkillName   string
	ToolCommand string
	FS          fs.FS
	AgentTools  []AgentTool
	KnownSkills []string
}

// NewManager creates a new Manager for the given skill name and embedded file system.
func NewManager(skillName string, fsys fs.FS) *Manager {
	return &Manager{
		SkillName:   skillName,
		ToolCommand: skillName,
		FS:          fsys,
		AgentTools:  DefaultAgentTools(skillName),
	}
}

func isKnownFile(data []byte, known []string) bool {
	if len(data) == 0 || len(known) == 0 {
		return false
	}
	sum := sha256.Sum256(data)
	return slices.Contains(known, hex.EncodeToString(sum[:]))
}

func ourFile(data, want []byte, known []string) bool {
	if bytes.Equal(data, want) {
		return true
	}
	return isKnownFile(data, known)
}

// Print writes the skill contents to w.
func (m *Manager) Print(w io.Writer) error {
	if m.FS == nil {
		return errors.New("nil skill filesystem")
	}

	// Try opening SKILL.md directly if present
	if data, err := fs.ReadFile(m.FS, "SKILL.md"); err == nil {
		_, err := w.Write(data)
		return err
	}

	// Otherwise walk all regular files in order
	return fs.WalkDir(m.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(m.FS, path)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		return nil
	})
}

// WriteToDir copies all files from m.FS into destDir.
// It handles newly added files in m.FS, updates outdated known files,
// removes obsolete files that belonged to previous known skill versions,
// and refuses to overwrite if any file was modified by the user.
// It returns true if all files were successfully written, or false if user-modified files were found.
func (m *Manager) WriteToDir(destDir string) (bool, error) {
	return m.WriteToDirForce(destDir, false)
}

// WriteToDirForce copies all files from m.FS into destDir.
// If force is true, existing user-modified files are overwritten.
func (m *Manager) WriteToDirForce(destDir string, force bool) (bool, error) {
	if m.FS == nil {
		return false, errors.New("nil skill filesystem")
	}

	fsFiles := make(map[string][]byte)
	err := fs.WalkDir(m.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.Clean(filepath.FromSlash(path))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("invalid path in skill filesystem: %q", path)
		}
		data, err := fs.ReadFile(m.FS, path)
		if err != nil {
			return err
		}
		fsFiles[rel] = data
		return nil
	})
	if err != nil {
		return false, err
	}

	var toDelete []string

	if _, err := os.Stat(destDir); err == nil {
		var userModified bool
		walkErr := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Security: Do not follow symlinks to avoid escaping destDir
			if d.Type()&fs.ModeSymlink != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(destDir, path)
			if err != nil {
				return err
			}
			rel = filepath.Clean(rel)
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			if want, ok := fsFiles[rel]; ok {
				if !ourFile(data, want, m.KnownSkills) {
					userModified = true
					if !force {
						return fs.SkipAll
					}
				}
			} else {
				// File exists on disk but is not in current m.FS.
				// Security: ONLY delete if non-empty and explicitly matches a known previous skill file hash.
				// User-created files (not in KnownSkills) are ALWAYS preserved.
				if len(data) > 0 && isKnownFile(data, m.KnownSkills) {
					toDelete = append(toDelete, path)
				}
			}
			return nil
		})
		if walkErr != nil {
			return false, walkErr
		}
		if userModified && !force {
			return false, nil
		}
	}

	for rel, data := range fsFiles {
		destPath := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0777); err != nil {
			return false, err
		}
		if err := os.WriteFile(destPath, data, 0666); err != nil {
			return false, err
		}
	}

	for _, path := range toDelete {
		_ = os.Remove(path)
		parent := filepath.Dir(path)
		for {
			rel, err := filepath.Rel(destDir, parent)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			if err := os.Remove(parent); err != nil {
				// Directory not empty or cannot remove
				break
			}
			parent = filepath.Dir(parent)
		}
	}

	return true, nil
}

// Update rewrites installed copies of the skill under home that have fallen behind.
func (m *Manager) Update(home string) (wrote, kept []string) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, nil
		}
	}

	if m.FS == nil {
		return nil, nil
	}

	fsFiles := make(map[string][]byte)
	if err := fs.WalkDir(m.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(m.FS, path)
		if err != nil {
			return err
		}
		fsFiles[filepath.FromSlash(path)] = data
		return nil
	}); err != nil {
		return nil, nil
	}

	seen := map[string]bool{}
	for _, t := range m.AgentTools {
		if t.SkillDir == "" {
			continue
		}
		dir := filepath.Join(home, t.SkillDir)
		if seen[dir] {
			continue
		}
		seen[dir] = true

		if _, err := os.Stat(dir); err != nil {
			// Directory not installed
			continue
		}

		skillFile := filepath.Join(dir, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			// Not a skill directory
			continue
		}

		needsUpdate := false
		for rel, want := range fsFiles {
			diskPath := filepath.Join(dir, rel)
			diskData, err := os.ReadFile(diskPath)
			if err != nil || !bytes.Equal(diskData, want) {
				needsUpdate = true
				break
			}
		}

		if !needsUpdate {
			_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(dir, path)
				if err != nil {
					return nil
				}
				if _, ok := fsFiles[filepath.Clean(rel)]; !ok {
					data, err := os.ReadFile(path)
					if err == nil && isKnownFile(data, m.KnownSkills) {
						needsUpdate = true
						return fs.SkipAll
					}
				}
				return nil
			})
		}

		if !needsUpdate {
			continue
		}

		ok, err := m.WriteToDir(dir)
		if err != nil {
			log.Printf("updating skill at %s: %v", dir, err)
			continue
		}
		if !ok {
			kept = append(kept, skillFile)
			continue
		}
		wrote = append(wrote, skillFile)
	}
	return wrote, kept
}

// DiffDir compares the embedded skill files with the files in destDir,
// returning a unified diff string, or empty string if there are no differences.
func (m *Manager) DiffDir(destDir string) (string, error) {
	if m.FS == nil {
		return "", errors.New("nil skill filesystem")
	}

	fsFiles := make(map[string][]byte)
	err := fs.WalkDir(m.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.Clean(filepath.FromSlash(path))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("invalid path in skill filesystem: %q", path)
		}
		data, err := fs.ReadFile(m.FS, path)
		if err != nil {
			return err
		}
		fsFiles[rel] = data
		return nil
	})
	if err != nil {
		return "", err
	}

	var diffs []string

	// Check files in m.FS against destDir
	for rel, want := range fsFiles {
		diskPath := filepath.Join(destDir, rel)
		data, err := os.ReadFile(diskPath)
		if err != nil {
			if os.IsNotExist(err) {
				d := unifiedDiff("/dev/null", diskPath+" (embedded)", nil, want)
				if d != "" {
					diffs = append(diffs, d)
				}
				continue
			}
			return "", err
		}
		d := unifiedDiff(diskPath+" (installed)", diskPath+" (embedded)", data, want)
		if d != "" {
			diffs = append(diffs, d)
		}
	}

	// Check for files in destDir that are not in m.FS
	if _, err := os.Stat(destDir); err == nil {
		_ = filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			rel, err := filepath.Rel(destDir, path)
			if err != nil {
				return nil
			}
			rel = filepath.Clean(rel)
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
			if _, ok := fsFiles[rel]; !ok {
				data, err := os.ReadFile(path)
				if err == nil && len(data) > 0 {
					d := unifiedDiff(path+" (installed)", "/dev/null", data, nil)
					if d != "" {
						diffs = append(diffs, d)
					}
				}
			}
			return nil
		})
	}

	return strings.Join(diffs, "\n"), nil
}

// Diff writes the unified diff between embedded skill files and installed directories to wOut.
func (m *Manager) Diff(wOut, wErr io.Writer, project bool, outDir string) error {
	if outDir != "" {
		d, err := m.DiffDir(outDir)
		if err != nil {
			return err
		}
		if d == "" {
			fmt.Fprintf(wOut, "%s: up to date (no differences)\n", outDir)
		} else {
			fmt.Fprint(wOut, d)
		}
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	root := home
	if project {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		root = dir
	}

	var order []string
	serves := map[string][]string{}
	add := func(rel, name string) {
		if rel == "" {
			return
		}
		dir := filepath.Join(root, rel)
		if _, ok := serves[dir]; !ok {
			order = append(order, dir)
		}
		serves[dir] = append(serves[dir], name)
	}

	found := 0
	for _, t := range m.AgentTools {
		if _, err := os.Stat(filepath.Join(home, t.Marker)); err != nil {
			continue
		}
		found++
		dir := t.SkillDir
		if project {
			dir = t.ProjectSkill
		}
		add(dir, t.Name)
	}

	if found == 0 {
		fmt.Fprintf(wOut, "no coding agents found in %s\n", home)
		return nil
	}

	for _, dir := range order {
		title := fmt.Sprintf("%s (%s)", strings.Join(serves[dir], ", "), dir)
		if _, err := os.Stat(dir); err != nil {
			fmt.Fprintf(wOut, "=== %s: not installed ===\n", title)
			continue
		}
		d, err := m.DiffDir(dir)
		if err != nil {
			return fmt.Errorf("diffing %s: %w", dir, err)
		}
		if d == "" {
			fmt.Fprintf(wOut, "=== %s: up to date (no differences) ===\n", title)
		} else {
			fmt.Fprintf(wOut, "=== %s ===\n", title)
			fmt.Fprint(wOut, d)
		}
	}

	return nil
}

// RunOptions configures execution of the skill subcommand.
type RunOptions struct {
	Print   bool
	Install bool
	Diff    bool
	Force   bool
	Project bool
	OutDir  string
}

// Run executes the skill CLI subcommand based on flags.
func (m *Manager) Run(wOut, wErr io.Writer, doPrint, doInstall, project bool, outDir string) error {
	return m.RunWithOptions(wOut, wErr, RunOptions{
		Print:   doPrint,
		Install: doInstall,
		Project: project,
		OutDir:  outDir,
	})
}

// RunWithOptions executes the skill CLI subcommand based on options.
func (m *Manager) RunWithOptions(wOut, wErr io.Writer, opts RunOptions) error {
	if opts.Print && opts.Install {
		return errors.New("cannot use both -print and -install")
	}
	if opts.Print && opts.Diff {
		return errors.New("cannot use both -print and -diff")
	}
	if opts.Install && opts.Diff {
		return errors.New("cannot use both -install and -diff")
	}

	if opts.Diff {
		return m.Diff(wOut, wErr, opts.Project, opts.OutDir)
	}

	if !opts.Install {
		return m.Print(wOut)
	}

	if opts.OutDir != "" {
		ok, err := m.WriteToDirForce(opts.OutDir, opts.Force)
		if err != nil {
			return err
		}
		path := filepath.Join(opts.OutDir, "SKILL.md")
		if !ok {
			fmt.Fprintf(wErr, "%s: not overwriting existing %s (use -force to overwrite)\n", m.SkillName, path)
			return nil
		}
		fmt.Fprintf(wOut, "wrote %s\n", path)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	root := home
	if opts.Project {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		root = dir
	}

	var order []string
	serves := map[string][]string{}
	add := func(rel, name string) {
		if rel == "" {
			return
		}
		dir := filepath.Join(root, rel)
		if _, ok := serves[dir]; !ok {
			order = append(order, dir)
		}
		serves[dir] = append(serves[dir], name)
	}

	found := 0
	for _, t := range m.AgentTools {
		if _, err := os.Stat(filepath.Join(home, t.Marker)); err != nil {
			continue
		}
		found++
		dir := t.SkillDir
		if opts.Project {
			dir = t.ProjectSkill
		}
		add(dir, t.Name)
	}

	for _, dir := range order {
		ok, err := m.WriteToDirForce(dir, opts.Force)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "SKILL.md")
		if !ok {
			fmt.Fprintf(wErr, "%s: not overwriting existing %s (use -force to overwrite)\n", m.SkillName, path)
			continue
		}
		fmt.Fprintf(wOut, "%s: wrote %s\n", strings.Join(serves[dir], ", "), path)
	}

	cmdName := m.ToolCommand
	if cmdName == "" {
		cmdName = m.SkillName
	}

	if found == 0 {
		fmt.Fprintf(wOut, "no coding agents found in %s\n", home)
		fmt.Fprintf(wOut, "Tell yours to run \"%s skill -print\" for instructions.\n", cmdName)
		return nil
	}
	fmt.Fprintf(wOut, "Ask an agent to use %s and it will find this.\n", cmdName)
	return nil
}
