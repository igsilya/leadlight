// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leadlight/gitops"
)

func setupGitRepo(t *testing.T, configs map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// git requires user config for some operations
	for _, kv := range [][2]string{
		{"user.email", "test@test.com"},
		{"user.name", "Test"},
	} {
		cmd := exec.Command("git", "config", kv[0], kv[1])
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s failed: %v\n%s", kv[0], err, out)
		}
	}

	for key, value := range configs {
		cmd := exec.Command("git", "config", key, value)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s %s failed: %v\n%s", key, value, err, out)
		}
	}
	return dir
}

func TestGitConfigGet_KeyExists(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server": "https://pw.example.com/api/1.2",
	})
	got := gitops.ConfigGet(dir, "pw.server")
	if got != "https://pw.example.com/api/1.2" {
		t.Errorf("got %q, want %q", got, "https://pw.example.com/api/1.2")
	}
}

func TestGitConfigGet_KeyMissing(t *testing.T) {
	dir := setupGitRepo(t, nil)
	got := gitops.ConfigGet(dir, "pw.server")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestGitConfigGet_WhitespaceTrimmed(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server": "  https://example.com/api/1.2  ",
	})
	got := gitops.ConfigGet(dir, "pw.server")
	if got != "https://example.com/api/1.2" {
		t.Errorf("got %q, want trimmed value", got)
	}
}

func TestGitConfigGet_NotARepo(t *testing.T) {
	dir := t.TempDir() // no git init
	got := gitops.ConfigGet(dir, "pw.server")
	if got != "" {
		t.Errorf("got %q, want empty string for non-repo", got)
	}
}

func TestPriority_PwOnly(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://pw.example.com/api/1.3",
		"pw.project": "test",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw.example.com/api/1.3" {
		t.Errorf("Server = %q, want pw.server value", cfg.Server)
	}
}

func TestPriority_LeadlightOnly(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"leadlight.server": "https://ll.example.com/api/1.3",
		"pw.project":       "myproject",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://ll.example.com/api/1.3" {
		t.Errorf("Server = %q, want leadlight.server value", cfg.Server)
	}
}

func TestPriority_LeadlightOverridesPw(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":        "https://pw.example.com/api/1.2",
		"leadlight.server": "https://ll.example.com/api/1.3",
		"pw.project":       "myproject",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://ll.example.com/api/1.3" {
		t.Errorf("Server = %q, want leadlight.server override", cfg.Server)
	}
}

func TestPriority_MixedKeys(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":       "https://pw.example.com/api/1.3",
		"pw.project":      "pw-project",
		"leadlight.token": "ll-token",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw.example.com/api/1.3" {
		t.Errorf("Server = %q, want pw.server (no leadlight override)", cfg.Server)
	}
	if cfg.Project != "pw-project" {
		t.Errorf("Project = %q, want pw.project", cfg.Project)
	}
	if cfg.Token != "ll-token" {
		t.Errorf("Token = %q, want leadlight.token", cfg.Token)
	}
}

func TestDefaults_DBPath(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.3",
		"pw.project": "test",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(dir, ".git", "leadlight", "leadlight.db")
	if cfg.DBPath != expected {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, expected)
	}
	llDir := filepath.Join(dir, ".git", "leadlight")
	if _, err := os.Stat(llDir); os.IsNotExist(err) {
		t.Error(".git/leadlight/ directory not created")
	}
}

func TestDefaults_DBPathCustom(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":    "https://example.com/api/1.3",
		"pw.project":   "test",
		"leadlight.db": "/tmp/custom.db",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != "/tmp/custom.db" {
		t.Errorf("DBPath = %q, want /tmp/custom.db", cfg.DBPath)
	}
}

func TestDefaults_States(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.3",
		"pw.project": "test",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new", "under-review"}
	if len(cfg.States) != len(want) {
		t.Fatalf("States = %v, want %v", cfg.States, want)
	}
	for i := range want {
		if cfg.States[i] != want[i] {
			t.Errorf("States[%d] = %q, want %q", i, cfg.States[i], want[i])
		}
	}
}

func TestDefaults_StatesCustom(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":        "https://example.com/api/1.3",
		"pw.project":       "test",
		"leadlight.states": "accepted,rejected,rfc",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "rejected", "rfc"}
	if len(cfg.States) != len(want) {
		t.Fatalf("States = %v, want %v", cfg.States, want)
	}
	for i := range want {
		if cfg.States[i] != want[i] {
			t.Errorf("States[%d] = %q, want %q", i, cfg.States[i], want[i])
		}
	}
}

func TestDefaults_StatesWhitespace(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":        "https://example.com/api/1.3",
		"pw.project":       "test",
		"leadlight.states": " new , under-review , rfc ",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new", "under-review", "rfc"}
	if len(cfg.States) != len(want) {
		t.Fatalf("States = %v, want %v", cfg.States, want)
	}
	for i := range want {
		if cfg.States[i] != want[i] {
			t.Errorf("States[%d] = %q, want %q", i, cfg.States[i], want[i])
		}
	}
}

func TestBaseURL_Standard(t *testing.T) {
	got := deriveBaseURL("https://pw.example.com/api/1.2")
	if got != "https://pw.example.com" {
		t.Errorf("got %q", got)
	}
}

func TestBaseURL_TrailingSlash(t *testing.T) {
	got := deriveBaseURL("https://pw.example.com/api/1.2/")
	if got != "https://pw.example.com" {
		t.Errorf("got %q", got)
	}
}

func TestBaseURL_SubPath(t *testing.T) {
	got := deriveBaseURL("https://example.com/patchwork/api/1.2")
	if got != "https://example.com/patchwork" {
		t.Errorf("got %q", got)
	}
}

func TestBaseURL_NoAPI(t *testing.T) {
	got := deriveBaseURL("https://example.com/something")
	if got != "https://example.com/something" {
		t.Errorf("got %q, want unchanged URL when no /api/ found", got)
	}
}

func TestAPIVersion_12(t *testing.T) {
	got := parseAPIVersion("https://pw.example.com/api/1.2")
	if got != "1.2" {
		t.Errorf("got %q, want 1.2", got)
	}
}

func TestAPIVersion_13(t *testing.T) {
	got := parseAPIVersion("https://pw2.example.com/api/1.3")
	if got != "1.3" {
		t.Errorf("got %q, want 1.3", got)
	}
}

func TestAPIVersion_TrailingSlash(t *testing.T) {
	got := parseAPIVersion("https://example.com/api/1.3/")
	if got != "1.3" {
		t.Errorf("got %q, want 1.3", got)
	}
}

func TestAPIVersion_NoAPI(t *testing.T) {
	got := parseAPIVersion("https://example.com/something")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestValidation_BothPresent(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.3",
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidation_ServerMissing(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for missing server")
	}
}

func TestValidation_ProjectMissing(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server": "https://example.com/api/1.3",
	})
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for missing project")
	}
}

func TestValidation_BothMissing(t *testing.T) {
	dir := setupGitRepo(t, nil)
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for missing server and project")
	}
}

func TestValidation_APIVersionEmpty(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/patchwork",
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for missing API version in URL")
	}
	if err != nil && !strings.Contains(err.Error(), "unsupported API version") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidation_APIVersionTooOld(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.1",
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for API version < 1.2")
	}
	if err != nil && !strings.Contains(err.Error(), "unsupported API version") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidation_API12_NoMailArchive(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.2",
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for API 1.2 without mailarchive")
	}
	if err != nil && !strings.Contains(err.Error(), "comment events") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidation_API12_WithMailArchive(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":             "https://example.com/api/1.2",
		"pw.project":            "test",
		"leadlight.mailarchive": "https://mail.example.com/lorem-dev/",
	})
	_, err := Load(dir)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidation_API13_NoMailArchive(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://example.com/api/1.3",
		"pw.project": "test",
	})
	_, err := Load(dir)
	if err != nil {
		t.Errorf("expected no error for API 1.3 without mailarchive, got %v", err)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":             "https://pw.example.com/api/1.2",
		"pw.project":            "lorem-project",
		"pw.token":              "abc123",
		"pw.username":           "user",
		"pw.password":           "pass",
		"leadlight.db":          "/tmp/test.db",
		"leadlight.states":      "new,accepted",
		"leadlight.lore":        "https://lore.example.com/lorem-dev/",
		"leadlight.mailarchive": "https://mail.example.com/lorem-dev/",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw.example.com/api/1.2" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Project != "lorem-project" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if cfg.Token != "abc123" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.Username != "user" {
		t.Errorf("Username = %q", cfg.Username)
	}
	if cfg.Password != "pass" {
		t.Errorf("Password = %q", cfg.Password)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.LoreURL != "https://lore.example.com/lorem-dev/" {
		t.Errorf("LoreURL = %q", cfg.LoreURL)
	}
	if cfg.BaseURL != "https://pw.example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.APIVersion != "1.2" {
		t.Errorf("APIVersion = %q", cfg.APIVersion)
	}
	if len(cfg.States) != 2 || cfg.States[0] != "new" || cfg.States[1] != "accepted" {
		t.Errorf("States = %v", cfg.States)
	}
}

func TestLoad_Theme(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":       "https://pw.example.com/api/1.3",
		"pw.project":      "lorem",
		"leadlight.theme": "light",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light", cfg.Theme)
	}
}

func TestLoad_ThemeDefault(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://pw.example.com/api/1.3",
		"pw.project": "lorem",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "" {
		t.Errorf("Theme = %q, want empty (auto)", cfg.Theme)
	}
}

func TestLoad_MinimalConfig(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://pw2.example.com/api/1.3/",
		"pw.project": "dolor-project",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw2.example.com/api/1.3/" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Project != "dolor-project" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
	if !strings.HasSuffix(cfg.DBPath, filepath.Join(
		".git", "leadlight", "leadlight.db")) {
		t.Errorf("DBPath = %q, want .git/leadlight/leadlight.db",
			cfg.DBPath)
	}
	if cfg.BaseURL != "https://pw2.example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.APIVersion != "1.3" {
		t.Errorf("APIVersion = %q", cfg.APIVersion)
	}
	if len(cfg.States) != 2 || cfg.States[0] != "new" || cfg.States[1] != "under-review" {
		t.Errorf("States = %v, want default", cfg.States)
	}
}

func TestLoad_EmptyRepo(t *testing.T) {
	dir := setupGitRepo(t, nil)
	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for empty repo config")
	}
}

func TestParseHistoryLimit(t *testing.T) {
	tests := []struct {
		input   string
		years   int
		months  int
		days    int
		wantErr bool
	}{
		{"", 0, 0, 0, false},
		{"0d", 0, 0, 0, false},
		{"0", 0, 0, 0, false},
		{"30d", 0, 0, 30, false},
		{"4w", 0, 0, 28, false},
		{"6mo", 0, 6, 0, false},
		{"1y", 1, 0, 0, false},
		{"2y", 2, 0, 0, false},
		{"12mo", 0, 12, 0, false},
		{"90d", 0, 0, 90, false},
		{"abc", 0, 0, 0, true},
		{"-1d", 0, 0, 0, true},
		{"3x", 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			h, err := ParseHistoryLimit(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.Years != tt.years || h.Months != tt.months ||
				h.Days != tt.days {
				t.Errorf("got {%d, %d, %d}, want {%d, %d, %d}",
					h.Years, h.Months, h.Days,
					tt.years, tt.months, tt.days)
			}
		})
	}
}

func TestHistoryLimit_IsZero(t *testing.T) {
	if !(HistoryLimit{}).IsZero() {
		t.Error("zero value should be zero")
	}
	if (HistoryLimit{Days: 1}).IsZero() {
		t.Error("{Days:1} should not be zero")
	}
	if (HistoryLimit{Months: 6}).IsZero() {
		t.Error("{Months:6} should not be zero")
	}
}

func TestHistoryLimit_Before(t *testing.T) {
	h := HistoryLimit{Years: 1}
	before := h.Before()
	// Should be approximately 1 year ago (within a few seconds)
	expected := time.Now().UTC().AddDate(-1, 0, 0)
	diff := before.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Before() = %v, want ~%v", before, expected)
	}
}

func TestFixGmailWrapping_Default(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"leadlight.server":  "https://lorem.example/api/1.3/",
		"leadlight.project": "lorem",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FixGmailWrapping != nil {
		t.Errorf("default should be nil (true), got %v",
			*cfg.FixGmailWrapping)
	}
}

func TestFixGmailWrapping_Disabled(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"leadlight.server":             "https://lorem.example/api/1.3/",
		"leadlight.project":            "lorem",
		"leadlight.fix-gmail-wrapping": "false",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FixGmailWrapping == nil || *cfg.FixGmailWrapping {
		t.Error("should be false when explicitly disabled")
	}
}
