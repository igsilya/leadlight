package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/api"
	"leadlight/config"
	"leadlight/db"
	appSync "leadlight/sync"
	"leadlight/tui"
)

func main() {
	logFile, err := os.OpenFile(
		"leadlight.log",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Log file error:", err)
		os.Exit(1)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
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

	m := tui.NewModel(database, cfg.States, cfg.Token)
	p := tea.NewProgram(m, tea.WithAltScreen())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncer := appSync.NewSyncer(
		client, database, cfg, func() {
			p.Send(tui.SyncUpdateMsg{})
		})

	m.RequestMbox = func(patchID int) {
		log.Printf("MAIN: RequestMbox callback called, patchID=%d",
			patchID)
		go func() {
			log.Printf("MAIN: RequestMbox goroutine started, patchID=%d",
				patchID)
			result := syncer.RequestMbox(patchID)
			log.Printf("MAIN: RequestMbox goroutine done, "+
				"patchID=%d err=%v contentLen=%d",
				patchID, result.Err, len(result.Content))
			p.Send(tui.SyncUpdateMsg{})
			log.Printf("MAIN: SyncUpdateMsg sent after mbox fetch")
		}()
	}

	m.RequestPatchUpdate = func(
		patchID int, state *string, delegateID *int,
	) {
		log.Printf("MAIN: RequestPatchUpdate patchID=%d "+
			"state=%v delegate=%v", patchID, state, delegateID)
		err := syncer.RequestPatchUpdate(
			appSync.PatchUpdateRequest{
				PatchID:    patchID,
				State:      state,
				DelegateID: delegateID,
			})
		log.Printf("MAIN: RequestPatchUpdate done, err=%v", err)
	}

	go syncer.Run(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
