// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"leadlight/gitops"
	"time"
)

// HistoryLimit controls how far back to fetch historical patches.
// Uses calendar units for correct date math (e.g., "1y" from March
// 2026 = March 2025, handling leap years and varying month lengths).
type HistoryLimit struct {
	Years  int
	Months int
	Days   int
}

func (h HistoryLimit) IsZero() bool {
	return h.Years == 0 && h.Months == 0 && h.Days == 0
}

// Before returns the cutoff time for history backfill.
func (h HistoryLimit) Before() time.Time {
	return time.Now().UTC().AddDate(-h.Years, -h.Months, -h.Days)
}

// ParseHistoryLimit parses a duration string like "30d", "4w", "6mo",
// "1y" into calendar components. Returns zero value for "" and "0d".
func ParseHistoryLimit(s string) (HistoryLimit, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" || s == "0d" {
		return HistoryLimit{}, nil
	}
	if strings.HasSuffix(s, "y") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Years: n}, nil
	}
	if strings.HasSuffix(s, "mo") {
		n, err := strconv.Atoi(s[:len(s)-2])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Months: n}, nil
	}
	if strings.HasSuffix(s, "w") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Days: n * 7}, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Days: n}, nil
	}
	return HistoryLimit{}, fmt.Errorf(
		"invalid history limit: %q (use Nd, Nw, Nmo, or Ny)", s)
}

type Config struct {
	Server           string
	Project          string
	Token            string
	Username         string
	Password         string
	DBPath           string
	States           []string
	LoreURL          string
	MailArchive      string
	Theme            string
	BaseURL          string
	APIVersion       string
	HistoryLimit     HistoryLimit
	Signoff          *bool // nil = default (true), false = don't add -s to git am
	FixGmailWrapping *bool // nil = default (true), false = disable quote rejoin
}

func Load(dir string) (*Config, error) {
	cfg := &Config{
		Server:      getWithFallback(dir, "leadlight.server", "pw.server"),
		Project:     getWithFallback(dir, "leadlight.project", "pw.project"),
		Token:       getWithFallback(dir, "leadlight.token", "pw.token"),
		Username:    getWithFallback(dir, "leadlight.username", "pw.username"),
		Password:    getWithFallback(dir, "leadlight.password", "pw.password"),
		DBPath:      gitops.ConfigGet(dir, "leadlight.db"),
		LoreURL:     gitops.ConfigGet(dir, "leadlight.lore"),
		MailArchive: gitops.ConfigGet(dir, "leadlight.mailarchive"),
		Theme:       gitops.ConfigGet(dir, "leadlight.theme"),
	}

	if cfg.DBPath == "" {
		gd := gitops.CommonDir(dir)
		if gd != "" {
			llDir := filepath.Join(gd, "leadlight")
			os.MkdirAll(llDir, 0755)
			cfg.DBPath = filepath.Join(llDir, "leadlight.db")
		} else {
			cfg.DBPath = ".leadlight.db"
		}
	}

	rawStates := getWithFallback(dir, "leadlight.states", "pw.states")
	cfg.States = parseStates(rawStates)

	cfg.BaseURL = deriveBaseURL(cfg.Server)
	cfg.APIVersion = parseAPIVersion(cfg.Server)

	historyStr := gitops.ConfigGet(dir, "leadlight.history")
	if historyStr != "" {
		limit, err := ParseHistoryLimit(historyStr)
		if err != nil {
			return nil, err
		}
		cfg.HistoryLimit = limit
	}

	signoffStr := gitops.ConfigGet(dir, "leadlight.signoff")
	if signoffStr == "false" {
		f := false
		cfg.Signoff = &f
	}

	fixWrapStr := gitops.ConfigGet(dir, "leadlight.fix-gmail-wrapping")
	if fixWrapStr == "false" {
		f := false
		cfg.FixGmailWrapping = &f
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func getWithFallback(dir, primary, fallback string) string {
	if v := gitops.ConfigGet(dir, primary); v != "" {
		return v
	}
	return gitops.ConfigGet(dir, fallback)
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
	if cfg.APIVersion == "" || cfg.APIVersion < "1.2" {
		return fmt.Errorf("unsupported API version %q (need >= 1.2);"+
			" server URL should end with /api/1.2 or /api/1.3", cfg.APIVersion)
	}
	if cfg.APIVersion < "1.3" && cfg.MailArchive == "" {
		return fmt.Errorf("API %s lacks comment events;"+
			" set leadlight.mailarchive to enable comment tracking", cfg.APIVersion)
	}
	return nil
}
