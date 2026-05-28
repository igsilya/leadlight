// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package gitops

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// CommonDir returns the git common directory for the given working
// directory. Handles relative paths from git rev-parse output.
func CommonDir(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	gd := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	return gd
}

// ConfigGet reads a git config value for the given working directory.
func ConfigGet(dir, key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsRepo checks if the current directory is inside a git repository.
func IsRepo() bool {
	return exec.Command("git", "rev-parse", "--git-dir").Run() == nil
}

// IsDirty checks if tracked files have uncommitted changes.
// Untracked files are ignored.
func IsDirty() (bool, error) {
	err := exec.Command("git", "diff-index", "--quiet", "HEAD", "--").Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// Signoff returns the current user's Signed-off-by line,
// or empty string if git user is not configured.
func Signoff() string {
	name, _ := exec.Command("git", "config", "user.name").Output()
	email, _ := exec.Command("git", "config", "user.email").Output()
	n := strings.TrimSpace(string(name))
	e := strings.TrimSpace(string(email))
	if n == "" || e == "" {
		return ""
	}
	return "Signed-off-by: " + n + " <" + e + ">"
}

// Am applies patches from an mbox file using git am -3.
// If signoff is true, -s is added. Returns combined output.
func Am(mboxPath string, signoff bool) (string, error) {
	args := []string{"am", "-3"}
	if signoff {
		args = append(args, "-s")
	}
	args = append(args, mboxPath)
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

// AmAbort aborts an in-progress git am. Returns combined output.
func AmAbort() (string, error) {
	out, err := exec.Command("git", "am", "--abort").CombinedOutput()
	return string(out), err
}
