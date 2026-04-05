package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime/pprof"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/api"
	"leadlight/config"
	"leadlight/db"
	"leadlight/status"
	appSync "leadlight/sync"
	"leadlight/tui"
)

func main() {
	pprofFlag := flag.Bool("pprof", false,
		"write CPU profile to leadlight.cpu.prof")
	flag.Parse()

	if *pprofFlag {
		f, err := os.Create("leadlight.cpu.prof")
		if err != nil {
			fmt.Fprintf(os.Stderr, "pprof: %v\n", err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(f)
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
		}()
	}

	logBuf := tui.NewLogBuffer()
	log.SetOutput(logBuf)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	log.Println("starting leadlight")

	cfg, err := config.Load(".")
	if err != nil {
		log.Printf("config error: %v", err)
		fmt.Fprintln(os.Stderr, "Config error:", err)
		fmt.Fprintln(os.Stderr,
			"Set pw.server and pw.project in gitconfig")
		os.Exit(1)
	}
	log.Printf("config: server=%q project=%q base=%q",
		cfg.Server, cfg.Project, cfg.BaseURL)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database error:", err)
		os.Exit(1)
	}
	defer database.Close()

	client := api.NewClient(cfg)

	appSync.MigrateTags(database)

	if cfg.Theme != "" {
		tui.SetTheme(cfg.Theme)
	}

	m := tui.NewModel(database, cfg.States, cfg.Token)
	m.LogBuf = logBuf
	p := tea.NewProgram(m, tea.WithAltScreen())

	// onChange may fire from within bubbletea's Update handler (e.g.
	// Status.SetTimed in key handlers). bubbletea's msgs channel is
	// unbuffered, so a synchronous Send from inside Update deadlocks.
	// The goroutine makes Send non-blocking for both callers: syncer
	// goroutines and the TUI's own Update.
	statusReg := status.NewRegistry(func() {
		go p.Send(tui.StatusUpdateMsg{})
	})
	m.Status = statusReg

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncer := appSync.NewSyncer(
		client, database, cfg, func(seriesIDs ...int) {
			p.Send(tui.SyncUpdateMsg{SeriesIDs: seriesIDs})
		}, statusReg)

	m.FetchSeriesCover = func(seriesID int) {
		syncer.RequestSeriesCover(seriesID)
	}

	m.RequestSync = func() {
		go syncer.RequestSync()
	}

	m.FetchPatchComments = func(patchID int) {
		go syncer.RequestComments(patchID, false)
	}
	m.FetchCoverComments = func(coverID int) {
		go syncer.RequestComments(coverID, true)
	}
	m.FetchPatchChecks = func(patchID int) {
		go syncer.RequestChecks(patchID)
	}
	m.FetchPatchDetail = func(patchID int) {
		go syncer.RequestDetail(patchID, false)
	}
	m.FetchCoverDetail = func(coverID int) {
		go syncer.RequestDetail(coverID, true)
	}
	m.RequestFetchAll = func(seriesID, patchID int) {
		syncer.RequestFetchAll(seriesID, patchID)
	}

	m.RequestPatchUpdate = func(
		patchID int, state *string,
		delegateUsername *string, unsetDelegate bool,
	) {
		syncer.RequestPatchUpdate(
			patchID, state, delegateUsername, unsetDelegate)
	}

	m.Signoff = true
	if cfg.Signoff != nil && !*cfg.Signoff {
		m.Signoff = false
	}

	m.CheckGitRepo = func() bool {
		return exec.Command("git", "rev-parse", "--git-dir").Run() == nil
	}
	m.CheckGitDirty = func() (bool, error) {
		// Check for uncommitted changes to tracked files only.
		// Untracked files are ignored — they don't affect git am.
		err := exec.Command("git", "diff-index", "--quiet", "HEAD", "--").Run()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				return true, nil // non-zero exit = dirty
			}
			return false, err
		}
		return false, nil
	}
	m.GetGitSignoff = func() string {
		name, _ := exec.Command("git", "config", "user.name").Output()
		email, _ := exec.Command("git", "config", "user.email").Output()
		n := strings.TrimSpace(string(name))
		e := strings.TrimSpace(string(email))
		if n == "" || e == "" {
			return ""
		}
		return "Signed-off-by: " + n + " <" + e + ">"
	}
	m.RunGitAm = func(mboxPath string, signoff bool) (string, error) {
		args := []string{"am", "-3"}
		if signoff {
			args = append(args, "-s")
		}
		args = append(args, mboxPath)
		out, err := exec.Command("git", args...).CombinedOutput()
		return string(out), err
	}
	m.AbortGitAm = func() (string, error) {
		out, err := exec.Command("git", "am", "--abort").CombinedOutput()
		return string(out), err
	}

	go syncer.Run(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
