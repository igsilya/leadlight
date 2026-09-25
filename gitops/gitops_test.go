// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPatchDirectory_Default(t *testing.T) {
	// When no config is set, should return current directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Create a temp directory with a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	dir, err := PatchDirectory()
	if err != nil {
		t.Fatalf("PatchDirectory() error: %v", err)
	}
	if dir != "." {
		t.Errorf("PatchDirectory() = %q, want %q", dir, ".")
	}
}

func TestPatchDirectory_Configured(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Create a temp directory with a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Set the patch directory config
	exec.Command("git", "config", "leadlight.patchDirectory", "patches").Run()

	dir, err := PatchDirectory()
	if err != nil {
		t.Fatalf("PatchDirectory() error: %v", err)
	}

	want := filepath.Join(tmpDir, "patches")
	if dir != want {
		t.Errorf("PatchDirectory() = %q, want %q", dir, want)
	}

	// Verify directory was created
	if info, err := os.Stat(dir); err != nil {
		t.Errorf("Directory not created: %v", err)
	} else if !info.IsDir() {
		t.Errorf("Path exists but is not a directory")
	}
}

func TestPatchDirectory_NestedPath(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Create a temp directory with a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Set nested path config
	exec.Command("git", "config", "leadlight.patchDirectory", "output/patches").Run()

	dir, err := PatchDirectory()
	if err != nil {
		t.Fatalf("PatchDirectory() error: %v", err)
	}

	want := filepath.Join(tmpDir, "output/patches")
	if dir != want {
		t.Errorf("PatchDirectory() = %q, want %q", dir, want)
	}

	// Verify nested directory was created
	if info, err := os.Stat(dir); err != nil {
		t.Errorf("Nested directory not created: %v", err)
	} else if !info.IsDir() {
		t.Errorf("Path exists but is not a directory")
	}
}

func TestTopLevel(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Create a temp directory with a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	exec.Command("git", "init").Run()

	top := TopLevel()
	if top != tmpDir {
		t.Errorf("TopLevel() = %q, want %q", top, tmpDir)
	}

	// Create subdirectory and test from there
	subDir := filepath.Join(tmpDir, "subdir", "nested")
	os.MkdirAll(subDir, 0755)
	os.Chdir(subDir)

	top = TopLevel()
	if top != tmpDir {
		t.Errorf("TopLevel() from subdirectory = %q, want %q", top, tmpDir)
	}
}
