package config

import (
	"os/exec"
	"testing"
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
		"pw.server": "https://patchwork.ozlabs.org/api/1.2",
	})
	got := gitConfigGet(dir, "pw.server")
	if got != "https://patchwork.ozlabs.org/api/1.2" {
		t.Errorf("got %q, want %q", got, "https://patchwork.ozlabs.org/api/1.2")
	}
}

func TestGitConfigGet_KeyMissing(t *testing.T) {
	dir := setupGitRepo(t, nil)
	got := gitConfigGet(dir, "pw.server")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestGitConfigGet_WhitespaceTrimmed(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server": "  https://example.com/api/1.2  ",
	})
	got := gitConfigGet(dir, "pw.server")
	if got != "https://example.com/api/1.2" {
		t.Errorf("got %q, want trimmed value", got)
	}
}

func TestGitConfigGet_NotARepo(t *testing.T) {
	dir := t.TempDir() // no git init
	got := gitConfigGet(dir, "pw.server")
	if got != "" {
		t.Errorf("got %q, want empty string for non-repo", got)
	}
}

func TestPriority_PwOnly(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://pw.example.com/api/1.2",
		"pw.project": "test",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw.example.com/api/1.2" {
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
		"pw.server":       "https://pw.example.com/api/1.2",
		"pw.project":      "pw-project",
		"leadlight.token": "ll-token",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://pw.example.com/api/1.2" {
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
		"pw.server":  "https://example.com/api/1.2",
		"pw.project": "test",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != ".leadlight.db" {
		t.Errorf("DBPath = %q, want .leadlight.db", cfg.DBPath)
	}
}

func TestDefaults_DBPathCustom(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":    "https://example.com/api/1.2",
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
		"pw.server":  "https://example.com/api/1.2",
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
		"pw.server":        "https://example.com/api/1.2",
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
		"pw.server":        "https://example.com/api/1.2",
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
	got := deriveBaseURL("https://patchwork.ozlabs.org/api/1.2")
	if got != "https://patchwork.ozlabs.org" {
		t.Errorf("got %q", got)
	}
}

func TestBaseURL_TrailingSlash(t *testing.T) {
	got := deriveBaseURL("https://patchwork.ozlabs.org/api/1.2/")
	if got != "https://patchwork.ozlabs.org" {
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
	got := parseAPIVersion("https://patchwork.ozlabs.org/api/1.2")
	if got != "1.2" {
		t.Errorf("got %q, want 1.2", got)
	}
}

func TestAPIVersion_13(t *testing.T) {
	got := parseAPIVersion("https://patches.dpdk.org/api/1.3")
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
		"pw.server":  "https://example.com/api/1.2",
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
		"pw.server": "https://example.com/api/1.2",
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

func TestLoad_FullConfig(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":        "https://patchwork.ozlabs.org/api/1.2",
		"pw.project":       "openvswitch",
		"pw.token":         "abc123",
		"pw.username":      "user",
		"pw.password":      "pass",
		"leadlight.db":     "/tmp/test.db",
		"leadlight.states": "new,accepted",
		"leadlight.lore":   "https://lore.kernel.org/ovs-dev/",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://patchwork.ozlabs.org/api/1.2" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Project != "openvswitch" {
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
	if cfg.LoreURL != "https://lore.kernel.org/ovs-dev/" {
		t.Errorf("LoreURL = %q", cfg.LoreURL)
	}
	if cfg.BaseURL != "https://patchwork.ozlabs.org" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.APIVersion != "1.2" {
		t.Errorf("APIVersion = %q", cfg.APIVersion)
	}
	if len(cfg.States) != 2 || cfg.States[0] != "new" || cfg.States[1] != "accepted" {
		t.Errorf("States = %v", cfg.States)
	}
}

func TestLoad_MinimalConfig(t *testing.T) {
	dir := setupGitRepo(t, map[string]string{
		"pw.server":  "https://patches.dpdk.org/api/1.3/",
		"pw.project": "dpdk",
	})
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://patches.dpdk.org/api/1.3/" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Project != "dpdk" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
	if cfg.DBPath != ".leadlight.db" {
		t.Errorf("DBPath = %q, want default", cfg.DBPath)
	}
	if cfg.BaseURL != "https://patches.dpdk.org" {
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
