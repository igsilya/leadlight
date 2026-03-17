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

	m := tui.NewModel(database, cfg.States)
	p := tea.NewProgram(m, tea.WithAltScreen())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncer := appSync.NewSyncer(client, database, cfg, func() {
		p.Send(tui.SyncUpdateMsg{})
	})
	go syncer.Run(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
