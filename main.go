// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"

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

	m.FetchPatchDetail = func(patchID int) {
		go syncer.RequestDetail(appSync.DetailRequest{ID: patchID})
	}
	m.FetchCoverDetail = func(coverID int) {
		go syncer.RequestDetail(appSync.DetailRequest{
			ID: coverID, IsCover: true})
	}
	m.FetchSeriesDetail = func(seriesID int) {
		go syncer.RequestDetail(appSync.DetailRequest{
			ID: seriesID, IsSeries: true})
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
	m.RequestFetchAll = func(seriesID, patchID int) {
		syncer.RequestFetchAll(seriesID, patchID)
	}
	m.RequestSync = func() {
		go syncer.RequestSync()
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
	m.FixGmailWrapping = cfg.FixGmailWrapping == nil || *cfg.FixGmailWrapping

	go syncer.Run(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
