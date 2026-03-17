package config

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type Config struct {
	Server      string
	Project     string
	Token       string
	Username    string
	Password    string
	DBPath      string
	States      []string
	LoreURL     string
	MailArchive string
	BaseURL     string
	APIVersion  string
}

func Load(dir string) (*Config, error) {
	cfg := &Config{
		Server:      getWithFallback(dir, "leadlight.server", "pw.server"),
		Project:     getWithFallback(dir, "leadlight.project", "pw.project"),
		Token:       getWithFallback(dir, "leadlight.token", "pw.token"),
		Username:    getWithFallback(dir, "leadlight.username", "pw.username"),
		Password:    getWithFallback(dir, "leadlight.password", "pw.password"),
		DBPath:      gitConfigGet(dir, "leadlight.db"),
		LoreURL:     gitConfigGet(dir, "leadlight.lore"),
		MailArchive: gitConfigGet(dir, "leadlight.mailarchive"),
	}

	if cfg.DBPath == "" {
		cfg.DBPath = ".leadlight.db"
	}

	rawStates := getWithFallback(dir, "leadlight.states", "pw.states")
	cfg.States = parseStates(rawStates)

	cfg.BaseURL = deriveBaseURL(cfg.Server)
	cfg.APIVersion = parseAPIVersion(cfg.Server)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func gitConfigGet(dir, key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getWithFallback(dir, primary, fallback string) string {
	if v := gitConfigGet(dir, primary); v != "" {
		return v
	}
	return gitConfigGet(dir, fallback)
}

func parseStates(raw string) []string {
	if raw == "" {
		return []string{"new", "under-review"}
	}
	parts := strings.Split(raw, ",")
	states := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			states = append(states, s)
		}
	}
	if len(states) == 0 {
		return []string{"new", "under-review"}
	}
	return states
}

var apiPathRe = regexp.MustCompile(`/api/[\d.]+/?$`)

func deriveBaseURL(server string) string {
	loc := apiPathRe.FindStringIndex(server)
	if loc == nil {
		return server
	}
	return server[:loc[0]]
}

var apiVersionRe = regexp.MustCompile(`/api/([\d.]+)/?$`)

func parseAPIVersion(server string) string {
	m := apiVersionRe.FindStringSubmatch(server)
	if m == nil {
		return ""
	}
	return m[1]
}

func validate(cfg *Config) error {
	var missing []string
	if cfg.Server == "" {
		missing = append(missing, "server (pw.server or leadlight.server)")
	}
	if cfg.Project == "" {
		missing = append(missing, "project (pw.project or leadlight.project)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
