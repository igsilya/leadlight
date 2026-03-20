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

	appSync.MigrateTagsFromContent(database)

	if cfg.Theme != "" {
		tui.SetTheme(cfg.Theme)
	}

	m := tui.NewModel(database, cfg.States, cfg.Token)
	m.LogBuf = logBuf
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

	m.FetchSeriesCover = func(seriesID int) {
		log.Printf("MAIN: FetchSeriesCover series=%d", seriesID)
		series, err := client.GetSeries(ctx, seriesID)
		if err != nil {
			log.Printf("MAIN: FetchSeriesCover error: %v", err)
			return
		}
		if series.CoverLetter != nil {
			database.SaveCover(db.CoverRow{
				ID:       series.CoverLetter.ID,
				SeriesID: series.ID,
				Name:     series.CoverLetter.Name,
				Date:     series.CoverLetter.Date,
				MsgID:    series.CoverLetter.MsgID,
				MboxURL:  series.CoverLetter.Mbox,
				WebURL:   series.CoverLetter.WebURL,
			})
		}
		database.SaveSeries(db.SeriesRow{
			ID:              series.ID,
			Name:            series.Name,
			Date:            series.Date,
			Version:         series.Version,
			Submitter:       series.Submitter.Name,
			SubmitterEmail:  series.Submitter.Email,
			WebURL:          series.WebURL,
			MboxURL:         series.Mbox,
			Complete:        series.ReceivedAll,
			TotalPatches:    series.Total,
			ReceivedPatches: series.ReceivedTotal,
		})
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

	m.RequestCoverMbox = func(seriesID int) {
		log.Printf("MAIN: RequestCoverMbox series=%d", seriesID)
		go func() {
			resultC := make(chan appSync.MboxResult, 1)
			syncer.SendMboxRequest(appSync.MboxRequest{
				PatchID: seriesID,
				IsCover: true,
				ResultC: resultC,
			})
			<-resultC
			p.Send(tui.SyncUpdateMsg{})
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
