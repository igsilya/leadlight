package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"leadlight/api"
	"leadlight/config"
	"leadlight/db"
	"leadlight/status"
)

// Skip retrying failed fetches for 30 minutes to avoid hammering the API
// for items whose endpoint is broken or rate-limited.
const commentSkipCooldown = 30 * time.Minute

type Syncer struct {
	client *api.Client
	db     *db.DB
	cfg    *config.Config
	notify func()
	status *status.Registry

	// Channels for user-initiated requests, handled by runUserRequests.
	mboxC     chan MboxRequest
	updateC   chan PatchUpdateRequest
	commentC  chan CommentRequest
	checkC    chan int
	fetchAllC chan FetchAllRequest
	syncNowC  chan context.Context

	// Error cooldown: skip retrying failed fetches for 30 minutes.
	commentSkip map[int]time.Time
	detailSkip  map[int]time.Time
	checkSkip   map[int]time.Time

	// Terminal-state fetch cooldown: one fetch per 60 seconds per loop.
	lastTerminalComment      time.Time
	lastTerminalCoverComment time.Time
	lastTerminalDetail       time.Time
	lastTerminalCheck        time.Time
}

type MboxRequest struct {
	PatchID int
	IsCover bool
	ResultC chan<- MboxResult
}

type MboxResult struct {
	Content string
	Err     error
}

type PatchUpdateRequest struct {
	PatchID          int
	State            *string
	DelegateUsername *string
	UnsetDelegate    bool
	ResultC          chan<- error
}

type fetchOriginKey struct{}

const (
	OriginBackground = "background"
	OriginOnDemand   = "on-demand"
	OriginFetchAll   = "fetch-all"
)

func WithFetchOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, fetchOriginKey{}, origin)
}

func fetchOrigin(ctx context.Context) string {
	if v, ok := ctx.Value(fetchOriginKey{}).(string); ok {
		return v
	}
	return OriginBackground
}

type FetchAllRequest struct {
	SeriesID int // non-zero for series fetch
	PatchID  int // non-zero for single patch fetch
}

type CommentRequest struct {
	ID      int
	IsCover bool
}

func NewSyncer(
	client *api.Client,
	d *db.DB,
	cfg *config.Config,
	notify func(),
	st *status.Registry,
) *Syncer {
	return &Syncer{
		client:      client,
		db:          d,
		cfg:         cfg,
		notify:      notify,
		status:      st,
		mboxC:       make(chan MboxRequest, 4),
		updateC:     make(chan PatchUpdateRequest, 4),
		commentC:    make(chan CommentRequest, 4),
		checkC:      make(chan int, 4),
		fetchAllC:   make(chan FetchAllRequest, 4),
		syncNowC:    make(chan context.Context, 1),
		commentSkip: map[int]time.Time{},
		detailSkip:  map[int]time.Time{},
		checkSkip:   map[int]time.Time{},
	}
}

func (s *Syncer) RequestPatchUpdate(req PatchUpdateRequest) error {
	resultC := make(chan error, 1)
	req.ResultC = resultC
	s.updateC <- req
	return <-resultC
}

func (s *Syncer) SendMboxRequest(req MboxRequest) {
	s.mboxC <- req
}

func (s *Syncer) RequestMbox(patchID int) MboxResult {
	resultC := make(chan MboxResult, 1)
	s.mboxC <- MboxRequest{
		PatchID: patchID,
		ResultC: resultC,
	}
	return <-resultC
}

func (s *Syncer) RequestComments(id int, isCover bool) {
	select {
	case s.commentC <- CommentRequest{ID: id, IsCover: isCover}:
		s.status.Set(status.Comments, "Fetching comments...", true)
	default:
	}
}

func (s *Syncer) RequestChecks(patchID int) {
	select {
	case s.checkC <- patchID:
		s.status.Set(status.BgChecks, "Fetching checks...", true)
	default:
	}
}

func (s *Syncer) lookupSeriesID(id int, isCover bool) int {
	if isCover {
		cover, _ := s.db.GetCover(id)
		if cover != nil {
			return cover.SeriesID
		}
		return 0
	}
	row, _ := s.db.GetPatch(id)
	if row != nil {
		return row.SeriesID
	}
	return 0
}

func (s *Syncer) RequestFetchAll(seriesID, patchID int) {
	select {
	case s.fetchAllC <- FetchAllRequest{
		SeriesID: seriesID, PatchID: patchID}:
		s.status.Set(status.FetchAll, "Fetching...", true)
	default:
	}
}

func (s *Syncer) RequestSync() {
	select {
	case s.syncNowC <- api.WithNoRateLimit(context.Background()):
		s.status.Set(status.Sync, "Syncing...", true)
	default:
	}
}

const (
	syncInterval      = 5 * time.Minute  // check for new events
	activeInterval    = 5 * time.Second  // poll interval for all background loops
	terminalInterval  = 60 * time.Second // cooldown between terminal-state fetches
	archiveInterval   = 5 * time.Minute  // poll mail archive (only for Patchwork < 1.3)
	maintainerRefresh = 24 * time.Hour   // re-fetch project maintainer list
)

// Patchwork >= 1.3 emits comment events; older versions need mail
// archive polling to discover new comments.
func (s *Syncer) needsArchiveMonitoring() bool {
	return s.cfg.APIVersion < "1.3" && s.cfg.MailArchive != ""
}

func (s *Syncer) Run(ctx context.Context) {
	complete := s.db.GetSyncState("initial_sync_complete")
	if complete != "true" {
		s.initialSync(ctx)
		s.notify()
	}
	s.backfillHistory(ctx)

	var wg gosync.WaitGroup
	wg.Add(7)
	go s.runUserRequests(ctx, &wg)
	go s.runSyncLoop(ctx, &wg)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextComments)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextCoverComments)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextDetail)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextChecks)
	go s.runArchiveLoop(ctx, &wg)
	wg.Wait()
}

func (s *Syncer) runUserRequests(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.mboxC:
			noRL := api.WithNoRateLimit(ctx)
			if req.IsCover {
				log.Printf("SYNC: cover mbox request for %d",
					req.PatchID)
				req.ResultC <- s.fetchCoverMbox(
					noRL, req.PatchID)
			} else {
				log.Printf("SYNC: patch mbox request for %d",
					req.PatchID)
				req.ResultC <- s.fetchMbox(
					noRL, req.PatchID)
			}
		case req := <-s.updateC:
			log.Printf("SYNC: update request for patch %d",
				req.PatchID)
			req.ResultC <- s.handlePatchUpdate(ctx, req)
		case req := <-s.commentC:
			go func() {
				noRL := WithFetchOrigin(
					api.WithNoRateLimit(ctx), OriginOnDemand)
				sid := s.lookupSeriesID(req.ID, req.IsCover)
				if req.IsCover {
					s.fetchCommentsForCover(
						noRL, req.ID, sid, status.Comments)
				} else {
					s.fetchCommentsForPatch(
						noRL, req.ID, sid, status.Comments)
				}
				s.status.Clear(status.Comments)
				s.notify()
			}()
		case patchID := <-s.checkC:
			go func() {
				noRL := WithFetchOrigin(
					api.WithNoRateLimit(ctx), OriginOnDemand)
				sid := s.lookupSeriesID(patchID, false)
				s.fetchChecksForPatch(
					noRL, patchID, sid, status.BgChecks)
				s.status.Clear(status.BgChecks)
				s.notify()
			}()
		case req := <-s.fetchAllC:
			go func() {
				fctx := WithFetchOrigin(ctx, OriginFetchAll)
				if req.SeriesID != 0 {
					s.fetchAllForSeries(fctx, req.SeriesID)
				} else if req.PatchID != 0 {
					s.fetchAllForPatch(fctx, req.PatchID)
				}
				s.status.Clear(status.FetchAll)
				s.notify()
			}()
		}
	}
}

func (s *Syncer) runSyncLoop(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()

	s.status.Set(status.BgSync, "Checking events...", true)
	s.incrementalSync(ctx)
	s.fetchMissingSeries(ctx)
	s.fixIncompletePatches(ctx)
	s.status.Clear(status.BgSync)
	s.notify()

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	lastMaintainerRefresh := time.Now()

	doSync := func(syncCtx context.Context) {
		s.status.Set(status.BgSync, "Checking events...", true)
		s.incrementalSync(syncCtx)
		s.fixIncompletePatches(syncCtx)
		s.status.Clear(status.BgSync)
		s.status.Clear(status.Sync)
		s.notify()
		if time.Since(lastMaintainerRefresh) > maintainerRefresh {
			s.fetchMaintainers(syncCtx)
			lastMaintainerRefresh = time.Now()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doSync(ctx)
		case syncCtx := <-s.syncNowC:
			doSync(syncCtx)
		}
	}
}

// runBgLoop runs a background fetch function on a fixed interval.
// The fetch function returns true if work was done. When true,
// notify() is called to update the TUI.
func (s *Syncer) runBgLoop(
	ctx context.Context, wg *gosync.WaitGroup,
	interval time.Duration,
	fetch func(context.Context) bool,
) {
	defer wg.Done()
	if fetch(ctx) {
		s.notify()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			if fetch(ctx) {
				s.notify()
			}
		}
	}
}

func (s *Syncer) runArchiveLoop(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()
	if !s.needsArchiveMonitoring() {
		<-ctx.Done()
		return
	}

	s.checkMailArchive(ctx)

	ticker := time.NewTicker(archiveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkMailArchive(ctx)
		}
	}
}

func (s *Syncer) initialSync(ctx context.Context) {
	s.status.Set(status.Sync, "Fetching patch list...", true)
	s.fetchListPages(ctx)
	s.notify()
	s.status.Set(status.Sync, "Fetching maintainers...", true)
	s.fetchMaintainers(ctx)
	s.status.Set(status.Sync, "Fetching patches...", true)
	s.fetchAllPatches(ctx)
	s.status.Set(status.Sync, "Fetching series...", true)
	s.fetchMissingSeries(ctx)
	s.status.Set(status.Sync, "Fetching events...", true)
	s.fetchInitialEvents(ctx)
	s.status.Clear(status.Sync)
	s.db.SetSyncState("initial_sync_complete", "true")
}

func (s *Syncer) fetchListPages(ctx context.Context) {
	if s.cfg.BaseURL == "" {
		return
	}
	pageURL := api.BuildListURL(
		s.cfg.BaseURL, s.cfg.Project, s.cfg.States)

	for pageURL != "" {
		page, err := s.client.FetchListPage(ctx, pageURL)
		if err != nil {
			log.Printf(
				"list page fetch skipped: %v"+
					" (will use API instead)", err)
			return
		}
		for _, p := range page.Patches {
			s.db.SavePatchSummary(
				p.PatchID, p.SeriesID,
				p.Name, p.Date, "", "", "")

			if p.SeriesID != 0 {
				s.db.SaveSeriesSummary(
					p.SeriesID, p.SeriesName, "", 0)
			}

			s.db.UpdatePatchChecks(p.PatchID,
				p.ChecksPass, p.ChecksFail, p.ChecksWarn)

			if p.State != "" {
				s.db.UpdatePatchState(p.PatchID, p.State)
			}
		}

		pageURL = page.NextURL
		if pageURL != "" &&
			!strings.HasPrefix(pageURL, "http") {
			pageURL = s.cfg.BaseURL + pageURL
		}
	}
}

func (s *Syncer) fetchMaintainers(ctx context.Context) {
	project, err := s.client.GetProject(ctx, s.cfg.Project)
	if err != nil {
		log.Printf("fetch maintainers: %v", err)
		return
	}
	rows := make([]db.MaintainerRow, len(project.Maintainers))
	for i, u := range project.Maintainers {
		rows[i] = db.MaintainerRow{
			ID:        u.ID,
			Username:  u.Username,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			Email:     u.Email,
		}
	}
	s.db.SaveMaintainers(rows)
}

func (s *Syncer) fetchAllPatches(ctx context.Context) {
	pageURL := s.client.BuildPatchesURL(api.PatchListParams{
		State:   s.cfg.States,
		Project: s.cfg.Project,
	})

	pageNum := 0
	for pageURL != "" {
		pageNum++
		if pageNum > 1 {
			s.status.Set(status.Sync,
				fmt.Sprintf("Fetching patches (page %d)...", pageNum), true)
		}
		page, err := s.client.GetPatchesPage(ctx, pageURL)
		if err != nil {
			log.Printf("fetch patches: %v", err)
			return
		}
		for _, p := range page.Items {
			s.db.SavePatch(patchToRow(p))
			for _, ss := range p.Series {
				s.db.SaveSeriesSummary(
					ss.ID, ss.Name, ss.Date, ss.Version)
			}
		}
		s.notify()
		pageURL = page.NextURL
	}
}

func (s *Syncer) fetchInitialEvents(ctx context.Context) {
	oldest := s.db.GetOldestPatchDate()
	if oldest == "" {
		return
	}
	s.fetchEventsSince(ctx, oldest, status.Sync)
}

func (s *Syncer) incrementalSync(ctx context.Context) {
	since := s.db.GetSyncState("last_event_date")
	if since == "" {
		return
	}
	s.fetchEventsSince(ctx, since, status.BgSync)
}

func (s *Syncer) fetchEventsSince(
	ctx context.Context, since string, statusKey status.Key,
) {
	pageURL := s.client.BuildEventsURL(api.EventListParams{
		Since:   since,
		Project: s.cfg.Project,
		Order:   "date",
	})

	pageNum := 0
	for pageURL != "" {
		pageNum++
		if pageNum > 1 {
			s.status.Set(statusKey,
				fmt.Sprintf("Fetching events (page %d)...", pageNum), true)
		}
		page, err := s.client.GetEventsPage(ctx, pageURL)
		if err != nil {
			log.Printf("fetch events: %v", err)
			return
		}
		for _, ev := range page.Items {
			seriesID := seriesIDFromEvent(ev)
			if err := s.processEvent(ev, seriesID); err != nil {
				log.Printf("process event %d: %v",
					ev.ID, err)
			}
			s.db.SetSyncState("last_event_date", ev.Date)
		}
		s.notify()
		pageURL = page.NextURL
	}
}

func seriesIDFromEvent(ev api.Event) int {
	switch p := ev.Payload.(type) {
	case *api.PatchCompletedPayload:
		return p.Series.ID
	case *api.SeriesCreatedPayload:
		return p.Series.ID
	case *api.SeriesCompletedPayload:
		return p.Series.ID
	}
	return 0
}

func (s *Syncer) processEvent(ev api.Event, seriesID int) error {
	switch p := ev.Payload.(type) {
	case *api.PatchCreatedPayload:
		return s.db.SavePatchSummary(
			p.Patch.ID, seriesID,
			p.Patch.Name, p.Patch.Date,
			p.Patch.MsgID, p.Patch.Mbox, p.Patch.WebURL)
	case *api.PatchStateChangedPayload:
		return s.db.UpdatePatchState(p.Patch.ID, p.CurrentState)
	case *api.PatchDelegatedPayload:
		id, name, email := 0, "", ""
		if p.CurrentDelegate != nil {
			id = p.CurrentDelegate.ID
			name = p.CurrentDelegate.Username
			email = p.CurrentDelegate.Email
		}
		return s.db.UpdatePatchDelegate(p.Patch.ID, id, name, email)
	case *api.CheckCreatedPayload:
		err := s.db.SaveCheck(db.CheckRow{
			ID:          p.Check.ID,
			PatchID:     p.Patch.ID,
			Context:     p.Check.Context,
			State:       p.Check.State,
			TargetURL:   ptrStr(p.Check.TargetURL),
			Description: ptrStr(p.Check.Description),
			Date:        p.Check.Date,
		})
		if err != nil {
			// Reset so the background loop retries
			s.db.ResetChecksFetched(p.Patch.ID)
			return err
		}
		// If the event lacks a description, reset so the background
		// loop re-fetches with full data from the API.
		if p.Check.Description == nil || *p.Check.Description == "" {
			s.db.ResetChecksFetched(p.Patch.ID)
		}
		return s.db.RecountPatchChecks(p.Patch.ID)
	case *api.SeriesCreatedPayload:
		return s.db.SaveSeriesSummary(
			p.Series.ID, p.Series.Name,
			p.Series.Date, p.Series.Version)
	case *api.SeriesCompletedPayload:
		return s.db.SaveSeriesSummary(
			p.Series.ID, p.Series.Name,
			p.Series.Date, p.Series.Version)
	case *api.PatchCompletedPayload:
		s.db.SavePatchSummary(
			p.Patch.ID, p.Series.ID,
			p.Patch.Name, p.Patch.Date,
			p.Patch.MsgID, p.Patch.Mbox, p.Patch.WebURL)
		return s.db.SaveSeriesSummary(
			p.Series.ID, p.Series.Name,
			p.Series.Date, p.Series.Version)
	case *api.CoverCreatedPayload:
		return s.db.SaveCover(db.CoverRow{
			ID:       p.Cover.ID,
			SeriesID: seriesID,
			Name:     p.Cover.Name,
			Date:     p.Cover.Date,
			MsgID:    p.Cover.MsgID,
			MboxURL:  p.Cover.Mbox,
			WebURL:   p.Cover.WebURL,
		})
	case *api.PatchCommentCreatedPayload:
		return s.db.ResetCommentsFetched(p.Patch.ID)
	case *api.CoverCommentCreatedPayload:
		return s.db.ResetCoverCommentsFetched(p.Cover.ID)
	}
	return nil
}

func (s *Syncer) fetchMissingSeries(ctx context.Context) {
	for {
		since := s.db.GetOldestIncompleteSeriesDate()
		if since == "" {
			return
		}
		s.status.Set(status.BgSync, "Fetching series...", true)
		log.Printf("SYNC: fetching series since %s", since)

		pageURL := s.client.BuildSeriesURL(api.SeriesListParams{
			Project: s.cfg.Project,
			Since:   since,
		})
		page, err := s.client.GetSeriesPage(ctx, pageURL)
		if err != nil {
			log.Printf("SYNC: fetchMissingSeries: %v", err)
			return
		}
		if len(page.Items) == 0 {
			return
		}

		for _, sr := range page.Items {
			s.db.SaveSeries(db.SeriesRow{
				ID:              sr.ID,
				Name:            sr.Name,
				Date:            sr.Date,
				Version:         sr.Version,
				Submitter:       sr.Submitter.Name,
				SubmitterEmail:  sr.Submitter.Email,
				WebURL:          sr.WebURL,
				MboxURL:         sr.Mbox,
				Complete:        sr.ReceivedAll,
				TotalPatches:    sr.Total,
				ReceivedPatches: sr.ReceivedTotal,
			})
			s.db.UpdateSeriesPatches(
				sr.ID, sr.Submitter.Name, sr.Submitter.Email)
			if sr.CoverLetter != nil {
				s.db.SaveCover(db.CoverRow{
					ID:       sr.CoverLetter.ID,
					SeriesID: sr.ID,
					Name:     sr.CoverLetter.Name,
					Date:     sr.CoverLetter.Date,
					MsgID:    sr.CoverLetter.MsgID,
					MboxURL:  sr.CoverLetter.Mbox,
					WebURL:   sr.CoverLetter.WebURL,
				})
			}
		}

		log.Printf("SYNC: processed %d series", len(page.Items))
		s.notify()

		newSince := s.db.GetOldestIncompleteSeriesDate()
		if newSince == since {
			log.Printf("SYNC: no progress, stopping")
			return
		}
	}
}

func (s *Syncer) fixIncompletePatches(ctx context.Context) {
	ids := s.db.GetIncompletePatches()
	if len(ids) == 0 {
		return
	}
	id := ids[0]
	row, err := s.db.GetPatch(id)
	if err != nil {
		return
	}

	if row.SeriesID != 0 && row.Submitter == "" {
		s.status.Set(status.BgSync,
			fmt.Sprintf("Fetching series %d...", row.SeriesID), true)
		series, err := s.client.GetSeries(ctx, row.SeriesID)
		if err != nil {
			log.Printf("SYNC: fixIncomplete series %d: %v",
				row.SeriesID, err)
			return
		}
		log.Printf("SYNC: fixIncomplete series %d %q, %d patches, submitter %q",
			series.ID, series.Name, len(series.Patches),
			series.Submitter.Name)
		s.db.SaveSeries(db.SeriesRow{
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
		s.db.UpdateSeriesPatches(
			series.ID,
			series.Submitter.Name,
			series.Submitter.Email)
		if series.CoverLetter != nil {
			s.db.SaveCover(db.CoverRow{
				ID:       series.CoverLetter.ID,
				SeriesID: series.ID,
				Name:     series.CoverLetter.Name,
				Date:     series.CoverLetter.Date,
				MsgID:    series.CoverLetter.MsgID,
				MboxURL:  series.CoverLetter.Mbox,
				WebURL:   series.CoverLetter.WebURL,
			})
		}
		return
	}

	if row.SeriesID == 0 {
		s.status.Set(status.BgSync,
			fmt.Sprintf("Fetching patch %d...", id), true)
		detail, err := s.client.GetPatch(ctx, id)
		if err != nil {
			log.Printf("SYNC: fixIncomplete patch %d: %v",
				id, err)
			return
		}
		log.Printf("SYNC: fixIncomplete patch %d %q -> series %v",
			id, detail.Name, detail.Series)
		s.db.SavePatch(patchToRow(detail.Patch))
		for _, ss := range detail.Series {
			s.db.SaveSeriesSummary(
				ss.ID, ss.Name, ss.Date, ss.Version)
		}
	}
}

func (s *Syncer) fetchNextComments(ctx context.Context) bool {
	refs := s.db.GetPatchesNeedingComments(s.cfg.States)
	for i, ref := range refs {
		if t, ok := s.commentSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalComment) < terminalInterval {
			break
		}
		if !s.fetchCommentsForPatch(ctx, ref.ID, ref.SeriesID,
			status.BgComments) {
			s.commentSkip[ref.ID] = time.Now()
			return false
		}
		delete(s.commentSkip, ref.ID)
		s.status.SetTimed(status.BgComments,
			fmt.Sprintf("Comments fetched (%d remaining)",
				len(refs)-i-1), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalComment = time.Now()
		}
		return true
	}
	return false
}

func (s *Syncer) fetchNextCoverComments(ctx context.Context) bool {
	refs := s.db.GetCoversNeedingComments(s.cfg.States)
	for i, ref := range refs {
		if t, ok := s.commentSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalCoverComment) < terminalInterval {
			break
		}
		if !s.fetchCommentsForCover(ctx, ref.ID, ref.SeriesID,
			status.BgCoverComments) {
			s.commentSkip[ref.ID] = time.Now()
			return false
		}
		delete(s.commentSkip, ref.ID)
		s.status.SetTimed(status.BgCoverComments,
			fmt.Sprintf("Cover comments fetched (%d remaining)",
				len(refs)-i-1), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalCoverComment = time.Now()
		}
		return true
	}
	return false
}

func (s *Syncer) fetchCommentsForPatch(
	ctx context.Context, patchID, seriesID int, sk status.Key,
) bool {
	if !s.db.NeedsPatchComments(patchID) {
		return true
	}
	s.status.StartFetchAndSetStatus(patchID, seriesID, sk,
		fmt.Sprintf("Fetching comments for patch %d...", patchID))
	defer s.status.EndFetch(patchID)
	log.Printf("SYNC [%s]: fetching comments for patch %d",
		fetchOrigin(ctx), patchID)
	comments, err := s.client.GetPatchComments(ctx, patchID)
	if err != nil {
		log.Printf("SYNC [%s]: fetch comments %d: %v",
			fetchOrigin(ctx), patchID, err)
		return false
	}
	s.saveComments(comments, patchID, 0)
	s.updatePatchTagsFromComments(patchID)
	s.db.MarkCommentsFetched(patchID)
	log.Printf("SYNC [%s]: fetched %d comments for patch %d",
		fetchOrigin(ctx), len(comments), patchID)
	return true
}

func (s *Syncer) fetchCommentsForCover(
	ctx context.Context, coverID, seriesID int, sk status.Key,
) bool {
	if !s.db.NeedsCoverComments(coverID) {
		return true
	}
	s.status.StartFetchAndSetStatus(coverID, seriesID, sk,
		fmt.Sprintf("Fetching comments for cover %d...", coverID))
	defer s.status.EndFetch(coverID)
	log.Printf("SYNC [%s]: fetching comments for cover %d",
		fetchOrigin(ctx), coverID)
	comments, err := s.client.GetCoverComments(ctx, coverID)
	if err != nil {
		log.Printf("SYNC [%s]: fetch cover comments %d: %v",
			fetchOrigin(ctx), coverID, err)
		return false
	}
	s.saveComments(comments, 0, coverID)
	s.updateCoverTagsFromComments(coverID)
	s.db.MarkCoverCommentsFetched(coverID)
	log.Printf("SYNC [%s]: fetched %d comments for cover %d",
		fetchOrigin(ctx), len(comments), coverID)
	return true
}

func (s *Syncer) checkMailArchive(ctx context.Context) {
	s.status.Set(status.Archive, "Checking mail archive...", true)
	defer s.status.Clear(status.Archive)

	now := time.Now()

	// Check all months since the last archive check. Each month tracks
	// its own high-water mark (last_archive_msg:YYYY-Month), so re-checking
	// a month only processes messages newer than the last seen. This handles
	// gaps of any length — days, months, or years.
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if last := s.db.GetSyncState("last_archive_check"); last != "" {
		if t, err := time.Parse("2006-01", last); err == nil {
			start = t
		}
	}

	for m := start; !m.After(now); m = m.AddDate(0, 1, 0) {
		s.status.Set(status.Archive,
			fmt.Sprintf("Checking archive (%s %d)...",
				m.Month(), m.Year()), true)
		s.checkArchiveMonth(ctx, m.Year(), m.Month())
	}

	s.db.SetSyncState("last_archive_check", now.Format("2006-01"))
}

func (s *Syncer) checkArchiveMonth(
	ctx context.Context, year int, month time.Month,
) {
	monthKey := fmt.Sprintf(
		"last_archive_msg:%d-%s", year, month)
	lastSeenStr := s.db.GetSyncState(monthKey)
	lastSeen, _ := strconv.Atoi(lastSeenStr)

	pageURL := api.BuildArchiveURL(
		s.cfg.MailArchive, year, month)
	msgs, err := s.client.FetchArchiveMessages(ctx, pageURL)
	if err != nil {
		log.Printf("archive check: %v", err)
		return
	}

	newMsgs := api.FilterNewMessages(msgs, lastSeen)
	if len(newMsgs) == 0 {
		return
	}

	patchNames := s.db.GetAllPatchNames()
	matchedPatchIDs := api.MatchPatchSubjects(newMsgs, patchNames)
	for _, id := range matchedPatchIDs {
		s.db.ResetCommentsFetched(id)
	}

	coverNames := s.db.GetAllCoverNames()
	matchedCoverIDs := api.MatchPatchSubjects(newMsgs, coverNames)
	for _, id := range matchedCoverIDs {
		s.db.ResetCoverCommentsFetched(id)
	}

	if len(matchedPatchIDs) > 0 || len(matchedCoverIDs) > 0 {
		log.Printf(
			"archive: %d new messages, %d patches, %d covers to re-check",
			len(newMsgs), len(matchedPatchIDs), len(matchedCoverIDs))
	}

	maxNum := lastSeen
	for _, m := range newMsgs {
		if m.Number > maxNum {
			maxNum = m.Number
		}
	}
	s.db.SetSyncState(monthKey, strconv.Itoa(maxNum))
}

func (s *Syncer) fetchNextDetail(ctx context.Context) bool {
	patchRefs := s.db.GetPatchesNeedingDetail(s.cfg.States)
	coverRefs := s.db.GetCoversNeedingDetail(s.cfg.States)
	total := len(patchRefs) + len(coverRefs)

	for i, ref := range patchRefs {
		if t, ok := s.detailSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalDetail) < terminalInterval {
			break
		}
		if err := s.fetchDetailForPatch(ctx, ref.ID,
			ref.SeriesID, status.Detail); err != nil {
			s.detailSkip[ref.ID] = time.Now()
			return false
		}
		delete(s.detailSkip, ref.ID)
		s.status.SetTimed(status.Detail,
			fmt.Sprintf("Details fetched (%d remaining)",
				total-i-1), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalDetail = time.Now()
		}
		return true
	}

	for i, ref := range coverRefs {
		if t, ok := s.detailSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalDetail) < terminalInterval {
			break
		}
		if err := s.fetchDetailForCover(ctx, ref.ID,
			ref.SeriesID, status.Detail); err != nil {
			s.detailSkip[ref.ID] = time.Now()
			return false
		}
		delete(s.detailSkip, ref.ID)
		s.status.SetTimed(status.Detail,
			fmt.Sprintf("Details fetched (%d remaining)",
				total-len(patchRefs)-i-1), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalDetail = time.Now()
		}
		return true
	}
	return false
}

// backfillHistory fetches historical patches and series going back
// to the configured history limit. Runs at startup, blocking before
// the background loops start. Patches and series are backfilled
// independently — each tracks its own progress via the oldest date
// in the DB, so interruptions are self-healing.
func (s *Syncer) backfillHistory(ctx context.Context) {
	if s.cfg.HistoryLimit.IsZero() {
		return
	}
	targetStr := s.cfg.HistoryLimit.Before().
		Format("2006-01-02T15:04:05")

	s.backfillPaginatedPatches(ctx, targetStr)
	s.backfillPaginatedSeries(ctx, targetStr)
	s.status.Clear(status.History)
}

func (s *Syncer) backfillPaginatedPatches(
	ctx context.Context, targetStr string,
) {
	oldest := s.db.GetOldestPatchDate()
	if oldest != "" && oldest <= targetStr {
		return
	}
	// Start from just after our oldest known date (or now if empty).
	// The +1h overshoot avoids missing items at the boundary.
	before := time.Now().Add(1 * time.Hour).
		Format("2006-01-02T15:04:05")
	if oldest != "" {
		oldestTime, _ := time.Parse("2006-01-02T15:04:05", oldest)
		before = oldestTime.Add(1 * time.Hour).
			Format("2006-01-02T15:04:05")
	}

	pageURL := s.client.BuildPatchesURL(api.PatchListParams{
		Project: s.cfg.Project,
		Before:  before,
		Since:   targetStr,
		Order:   "-date",
	})
	pageNum := 0

	for pageURL != "" {
		pageNum++
		s.status.Set(status.History,
			fmt.Sprintf("Backfilling patches (page %d)...",
				pageNum), true)

		page, err := s.client.GetPatchesPage(ctx, pageURL)
		if err != nil {
			log.Printf("SYNC: backfill patches: %v", err)
			return
		}
		if len(page.Items) == 0 {
			return
		}
		for _, p := range page.Items {
			s.db.SavePatch(patchToRow(p))
			for _, ss := range p.Series {
				s.db.SaveSeriesSummary(
					ss.ID, ss.Name, ss.Date, ss.Version)
			}
		}
		s.notify()
		pageURL = page.NextURL
	}
}

func (s *Syncer) backfillPaginatedSeries(
	ctx context.Context, targetStr string,
) {
	oldest := s.db.GetOldestSeriesDate()
	if oldest != "" && oldest <= targetStr {
		return
	}
	before := time.Now().Add(1 * time.Hour).
		Format("2006-01-02T15:04:05")
	if oldest != "" {
		oldestTime, _ := time.Parse("2006-01-02T15:04:05", oldest)
		before = oldestTime.Add(1 * time.Hour).
			Format("2006-01-02T15:04:05")
	}

	pageURL := s.client.BuildSeriesURL(api.SeriesListParams{
		Project: s.cfg.Project,
		Before:  before,
		Since:   targetStr,
		Order:   "-date",
	})
	pageNum := 0

	for pageURL != "" {
		pageNum++
		s.status.Set(status.History,
			fmt.Sprintf("Backfilling series (page %d)...",
				pageNum), true)

		page, err := s.client.GetSeriesPage(ctx, pageURL)
		if err != nil {
			log.Printf("SYNC: backfill series: %v", err)
			return
		}
		if len(page.Items) == 0 {
			return
		}
		for _, sr := range page.Items {
			s.db.SaveSeriesSummary(
				sr.ID, sr.Name, sr.Date, sr.Version)
		}
		s.notify()
		pageURL = page.NextURL
	}
}

func (s *Syncer) fetchNextChecks(ctx context.Context) bool {
	refs := s.db.GetPatchesNeedingChecks(s.cfg.States)
	for i, ref := range refs {
		if t, ok := s.checkSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalCheck) < terminalInterval {
			break
		}
		s.fetchChecksForPatch(ctx, ref.ID, ref.SeriesID,
			status.BgChecks)
		if s.db.NeedsPatchChecks(ref.ID) {
			s.checkSkip[ref.ID] = time.Now()
			return false
		}
		delete(s.checkSkip, ref.ID)
		s.status.SetTimed(status.BgChecks,
			fmt.Sprintf("Checks fetched (%d remaining)",
				len(refs)-i-1), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalCheck = time.Now()
		}
		return true
	}
	return false
}

// fetchAllForSeries fetches all data for a series: cover detail +
// comments, and for each patch: detail + comments + checks. Uses
// the standard rate limit (not bypassed).
func (s *Syncer) fetchAllForSeries(
	ctx context.Context, seriesID int,
) {
	patches := s.db.GetPatchesForSeries(seriesID)
	total := len(patches)

	cover, _ := s.db.GetCover(seriesID)
	if cover != nil {
		if !cover.DetailFetched {
			s.fetchDetailForCover(ctx, cover.ID,
				seriesID, status.FetchAll)
		}
		s.fetchCommentsForCover(ctx, cover.ID,
			seriesID, status.FetchAll)
	}

	for i, p := range patches {
		if !p.DetailFetched {
			s.fetchDetailForPatch(ctx, p.ID,
				seriesID, status.FetchAll)
		}
		s.fetchCommentsForPatch(ctx, p.ID,
			seriesID, status.FetchAll)
		s.fetchChecksForPatch(ctx, p.ID,
			seriesID, status.FetchAll)
		s.status.SetTimed(status.FetchAll,
			fmt.Sprintf("Series %d: %d/%d patches done",
				seriesID, i+1, total),
			3*time.Second)
		s.notify()
	}
}

// fetchAllForPatch fetches all data for a single patch: detail +
// comments + checks. Uses the standard rate limit (not bypassed).
func (s *Syncer) fetchAllForPatch(
	ctx context.Context, patchID int,
) {
	row, _ := s.db.GetPatch(patchID)
	if row == nil {
		return
	}
	seriesID := row.SeriesID
	if !row.DetailFetched {
		s.fetchDetailForPatch(ctx, patchID,
			seriesID, status.FetchAll)
	}
	s.fetchCommentsForPatch(ctx, patchID,
		seriesID, status.FetchAll)
	s.fetchChecksForPatch(ctx, patchID,
		seriesID, status.FetchAll)
}

// fetchDetailForPatch fetches and saves detail for a single patch.
func (s *Syncer) fetchDetailForPatch(
	ctx context.Context, patchID, seriesID int, sk status.Key,
) error {
	s.status.StartFetchAndSetStatus(patchID, seriesID, sk,
		fmt.Sprintf("Fetching details for patch %d...", patchID))
	defer s.status.EndFetch(patchID)
	detail, err := s.client.GetPatch(ctx, patchID)
	if err != nil {
		log.Printf("SYNC [%s]: fetch details %d: %v",
			fetchOrigin(ctx), patchID, err)
		return err
	}
	prefixes, _ := json.Marshal(detail.Prefixes)
	headers, _ := json.Marshal(detail.Headers)
	s.db.UpdatePatchDetail(patchID,
		detail.Content, detail.Diff,
		string(headers), string(prefixes))
	if detail.Content != "" {
		s.db.ClearTags(patchID, 0, "original")
		tags := extractReviewTags(detail.Content)
		s.db.SaveTags(patchID, 0, 0, "original", tags)
	}
	log.Printf("SYNC [%s]: fetched details for patch %d (%d bytes)",
		fetchOrigin(ctx), patchID, len(detail.Content))
	return nil
}

// fetchDetailForCover fetches and saves detail for a single cover.
func (s *Syncer) fetchDetailForCover(
	ctx context.Context, coverID, seriesID int, sk status.Key,
) error {
	s.status.StartFetchAndSetStatus(coverID, seriesID, sk,
		fmt.Sprintf("Fetching details for cover %d...", coverID))
	defer s.status.EndFetch(coverID)
	cover, err := s.client.GetCover(ctx, coverID)
	if err != nil {
		log.Printf("SYNC [%s]: fetch cover details %d: %v",
			fetchOrigin(ctx), coverID, err)
		return err
	}
	hdrs, _ := json.Marshal(cover.Headers)
	s.db.UpdateCoverDetail(coverID,
		cover.Content, string(hdrs))
	if cover.Content != "" {
		s.db.ClearTags(0, coverID, "original")
		tags := extractReviewTags(cover.Content)
		s.db.SaveTags(0, coverID, 0, "original", tags)
	}
	log.Printf("SYNC [%s]: fetched details for cover %d (%d bytes)",
		fetchOrigin(ctx), coverID, len(cover.Content))
	return nil
}

// fetchChecksForPatch fetches all checks for a single patch.
func (s *Syncer) fetchChecksForPatch(
	ctx context.Context, patchID, seriesID int, sk status.Key,
) {
	if !s.db.NeedsPatchChecks(patchID) {
		return
	}
	s.status.StartFetchAndSetStatus(patchID, seriesID, sk,
		fmt.Sprintf("Fetching checks for patch %d...", patchID))
	defer s.status.EndFetch(patchID)
	log.Printf("SYNC [%s]: fetching checks for patch %d",
		fetchOrigin(ctx), patchID)
	checks, err := s.client.GetPatchChecks(ctx, patchID)
	if err != nil {
		log.Printf("SYNC [%s]: fetch checks %d: %v",
			fetchOrigin(ctx), patchID, err)
		return
	}
	for _, c := range checks {
		s.db.SaveCheck(db.CheckRow{
			ID:          c.ID,
			PatchID:     patchID,
			Context:     c.Context,
			State:       c.State,
			TargetURL:   ptrStr(c.TargetURL),
			Description: ptrStr(c.Description),
			Date:        c.Date,
		})
	}
	s.db.RecountPatchChecks(patchID)
	s.db.MarkChecksFetched(patchID)
	log.Printf("SYNC [%s]: fetched %d checks for patch %d",
		fetchOrigin(ctx), len(checks), patchID)
}

// Bump when tag extraction logic changes. MigrateTags re-extracts all
// tags from stored content when the version doesn't match.
const tagSchemaVersion = "2"

func MigrateTags(d *db.DB) {
	if d.GetSyncState("tag_schema") == tagSchemaVersion {
		return
	}

	patches := d.GetPatchContent()
	for id, content := range patches {
		tags := extractReviewTags(content)
		if len(tags) > 0 {
			d.ClearTags(id, 0, "original")
			d.SaveTags(id, 0, 0, "original", tags)
		}
	}
	covers := d.GetCoverContent()
	for id, content := range covers {
		tags := extractReviewTags(content)
		if len(tags) > 0 {
			d.ClearTags(0, id, "original")
			d.SaveTags(0, id, 0, "original", tags)
		}
	}

	for _, patchID := range d.GetPatchIDsWithComments() {
		comments := d.GetComments(patchID)
		d.ClearTags(patchID, 0, "comment")
		for _, c := range comments {
			tags := extractReviewTags(c.Content)
			d.SaveTags(patchID, 0, c.ID, "comment", tags)
		}
	}
	for _, coverID := range d.GetCoverIDsWithComments() {
		comments := d.GetCommentsForCover(coverID)
		d.ClearTags(0, coverID, "comment")
		for _, c := range comments {
			tags := extractReviewTags(c.Content)
			d.SaveTags(0, coverID, c.ID, "comment", tags)
		}
	}

	log.Printf("SYNC: migrated tags: %d patches, %d covers (content)",
		len(patches), len(covers))
	d.SetSyncState("tag_schema", tagSchemaVersion)
}

var keepHeaders = []string{
	"From", "Date", "To", "Cc",
	"In-Reply-To", "References", "Content-Type",
}

func filterHeaders(raw map[string]interface{}) string {
	if raw == nil {
		return ""
	}
	var b strings.Builder
	for _, key := range keepHeaders {
		val, ok := raw[key]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case string:
			b.WriteString(key + ": " + v + "\n")
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					b.WriteString(key + ": " + s + "\n")
				}
			}
		}
	}
	return b.String()
}

func commentArchiveURL(c api.Comment) string {
	if c.ListArchiveURL != nil {
		return *c.ListArchiveURL
	}
	return ""
}

func (s *Syncer) saveComments(comments []api.Comment, patchID, coverID int) {
	for _, c := range comments {
		s.db.InsertComment(db.CommentRow{
			ID:             c.ID,
			PatchID:        patchID,
			CoverID:        coverID,
			Submitter:      c.Submitter.Name,
			SubmitterEmail: c.Submitter.Email,
			Date:           c.Date,
			Subject:        c.Subject,
			Content:        c.Content,
			MsgID:          c.MsgID,
			Headers:        filterHeaders(c.Headers),
			WebURL:         c.WebURL,
			ListArchiveURL: commentArchiveURL(c),
		})
	}
}

func (s *Syncer) updatePatchTagsFromComments(patchID int) {
	comments := s.db.GetComments(patchID)
	s.db.ClearTags(patchID, 0, "comment")
	for _, c := range comments {
		tags := extractReviewTags(c.Content)
		s.db.SaveTags(patchID, 0, c.ID, "comment", tags)
	}
}

func (s *Syncer) updateCoverTagsFromComments(coverID int) {
	comments := s.db.GetCommentsForCover(coverID)
	s.db.ClearTags(0, coverID, "comment")
	for _, c := range comments {
		tags := extractReviewTags(c.Content)
		s.db.SaveTags(0, coverID, c.ID, "comment", tags)
	}
}

func (s *Syncer) resolveUserID(
	ctx context.Context, username string,
) (int, error) {
	if id := s.db.GetMaintainerUserID(username); id > 0 {
		return id, nil
	}
	log.Printf("SYNC: looking up user ID for %q", username)
	id, err := s.client.LookupUserID(ctx, username)
	if err != nil {
		return 0, err
	}
	s.db.SetMaintainerUserID(username, id)
	log.Printf("SYNC: resolved %q to user ID %d", username, id)
	return id, nil
}

func (s *Syncer) handlePatchUpdate(ctx context.Context, req PatchUpdateRequest) error {
	dlgStr := ptrStr(req.DelegateUsername)
	if req.UnsetDelegate {
		dlgStr = "(unset)"
	}
	log.Printf("SYNC: handlePatchUpdate patch %d state=%s delegate=%s",
		req.PatchID, ptrStr(req.State), dlgStr)
	s.status.Set(status.Update, "Updating...", true)
	ctx = api.WithNoRateLimit(ctx)

	update := api.PatchUpdate{State: req.State}
	if req.UnsetDelegate {
		update.UnsetDelegate = true
	} else if req.DelegateUsername != nil {
		uid, err := s.resolveUserID(
			ctx, *req.DelegateUsername)
		if err != nil {
			log.Printf("SYNC: resolve delegate %q: %v",
				*req.DelegateUsername, err)
			s.status.SetTimed(status.Update,
				"Failed to resolve delegate: "+
					err.Error(), 5*time.Second)
			return err
		}
		update.Delegate = &uid
	}
	_, err := s.client.UpdatePatch(ctx, req.PatchID, update)
	if err != nil {
		log.Printf("SYNC: patch update %d failed: %v", req.PatchID, err)
		// "Invalid pk" means the cached user ID is stale — clear it
		// so the next attempt re-resolves the username.
		if req.DelegateUsername != nil &&
			strings.Contains(err.Error(), "Invalid pk") {
			s.db.ClearMaintainerUserID(*req.DelegateUsername)
		}
		s.status.SetTimed(status.Update,
			fmt.Sprintf("Update failed: %v", err), 5*time.Second)
		return err
	}
	log.Printf("SYNC: patch update %d success, syncing events", req.PatchID)
	s.incrementalSync(ctx)
	s.notify()
	s.status.SetTimed(status.Update, "Updated", 3*time.Second)
	return nil
}

func (s *Syncer) fetchMbox(
	ctx context.Context, patchID int,
) MboxResult {
	row, err := s.db.GetPatch(patchID)
	if err != nil {
		log.Printf("SYNC: fetchMbox(%d): GetPatch error: %v",
			patchID, err)
		return MboxResult{Err: err}
	}
	log.Printf("SYNC: fetchMbox(%d) %q mboxURL=%q msgID=%q cached=%d",
		patchID, row.Name, row.MboxURL, row.MsgID,
		len(row.MboxContent))
	if row.MboxContent != "" {
		log.Printf("SYNC: fetchMbox(%d): returning cached", patchID)
		return MboxResult{Content: row.MboxContent}
	}

	if s.cfg.LoreURL != "" && row.MsgID != "" {
		// Strip angle brackets from Message-ID header for the URL path
		msgid := strings.Trim(row.MsgID, "<>")
		loreURL := strings.TrimRight(
			s.cfg.LoreURL, "/") + "/" + msgid + "/raw"
		log.Printf("SYNC: fetchMbox(%d): trying lore %s",
			patchID, loreURL)
		content, err := s.client.GetMbox(ctx, loreURL)
		if err == nil && isValidMbox(content) {
			log.Printf("SYNC: fetchMbox(%d): lore OK, %d bytes",
				patchID, len(content))
			s.db.UpdatePatchMbox(patchID, content)
			return MboxResult{Content: content}
		}
		if err != nil {
			log.Printf("SYNC: fetchMbox(%d): lore failed: %v",
				patchID, err)
		} else {
			preview := content
			if len(preview) > 200 {
				preview = preview[:200]
			}
			log.Printf("SYNC: fetchMbox(%d): lore returned "+
				"non-mbox content (%d bytes): %q",
				patchID, len(content), preview)
		}
	}

	if row.MboxURL != "" {
		// Some Patchwork instances return http:// URLs even over https
		mboxURL := row.MboxURL
		if strings.HasPrefix(s.cfg.Server, "https://") &&
			strings.HasPrefix(mboxURL, "http://") {
			mboxURL = "https://" + mboxURL[len("http://"):]
		}
		log.Printf("SYNC: fetchMbox(%d): trying patchwork %s",
			patchID, mboxURL)
		content, err := s.client.GetMbox(ctx, mboxURL)
		if err != nil {
			log.Printf("SYNC: fetchMbox(%d): patchwork failed: %v",
				patchID, err)
			return MboxResult{Err: err}
		}
		if !isValidMbox(content) {
			preview := content
			if len(preview) > 200 {
				preview = preview[:200]
			}
			log.Printf("SYNC: fetchMbox(%d): patchwork returned "+
				"non-mbox content (%d bytes): %q",
				patchID, len(content), preview)
			return MboxResult{
				Err: fmt.Errorf("unexpected response from server"),
			}
		}
		log.Printf("SYNC: fetchMbox(%d): patchwork OK, %d bytes",
			patchID, len(content))
		s.db.UpdatePatchMbox(patchID, content)
		return MboxResult{Content: content}
	}

	log.Printf("SYNC: fetchMbox(%d): no URL available", patchID)
	return MboxResult{Err: err}
}

// fetchCoverMbox fetches cover letter mbox. The ID parameter is
// the series ID — GetCover looks up by series_id.
func (s *Syncer) fetchCoverMbox(ctx context.Context, seriesID int) MboxResult {
	cover, err := s.db.GetCover(seriesID)
	if err != nil {
		log.Printf("SYNC: fetchCoverMbox(series %d): %v",
			seriesID, err)
		return MboxResult{Err: err}
	}
	if cover == nil {
		return MboxResult{Err: fmt.Errorf(
			"no cover letter for series %d", seriesID)}
	}
	log.Printf("SYNC: fetchCoverMbox(%d) %q mboxURL=%q cached=%d",
		cover.ID, cover.Name, cover.MboxURL, len(cover.MboxContent))
	if cover.MboxContent != "" {
		return MboxResult{Content: cover.MboxContent}
	}

	if s.cfg.LoreURL != "" && cover.MsgID != "" {
		msgid := strings.Trim(cover.MsgID, "<>")
		loreURL := strings.TrimRight(s.cfg.LoreURL, "/") + "/" + msgid + "/raw"
		log.Printf("SYNC: fetchCoverMbox(%d): trying lore %s",
			cover.ID, loreURL)
		content, err := s.client.GetMbox(ctx, loreURL)
		if err == nil && isValidMbox(content) {
			s.db.UpdateCoverMbox(cover.ID, content)
			return MboxResult{Content: content}
		}
	}

	if cover.MboxURL != "" {
		mboxURL := cover.MboxURL
		if strings.HasPrefix(s.cfg.Server, "https://") &&
			strings.HasPrefix(mboxURL, "http://") {
			mboxURL = "https://" + mboxURL[len("http://"):]
		}
		log.Printf("SYNC: fetchCoverMbox(%d): trying patchwork %s",
			cover.ID, mboxURL)
		content, err := s.client.GetMbox(ctx, mboxURL)
		if err != nil {
			return MboxResult{Err: err}
		}
		if !isValidMbox(content) {
			return MboxResult{
				Err: fmt.Errorf("unexpected response from server")}
		}
		s.db.UpdateCoverMbox(cover.ID, content)
		return MboxResult{Content: content}
	}

	return MboxResult{Err: fmt.Errorf(
		"no mbox URL for cover %d", cover.ID)}
}

func isValidMbox(content string) bool {
	if strings.HasPrefix(content, "From ") {
		return true
	}
	firstChunk := content
	if len(firstChunk) > 1000 {
		firstChunk = firstChunk[:1000]
	}
	emailHeaders := []string{
		"Subject:", "From:", "Date:",
		"Content-Type:", "MIME-Version:",
		"Message-Id:", "Message-ID:",
		"Received:", "Return-Path:",
		"Delivered-To:", "DKIM-Signature:",
	}
	for _, h := range emailHeaders {
		if strings.Contains(firstChunk, h) {
			return true
		}
	}
	return false
}

func patchToRow(p api.Patch) db.PatchRow {
	r := db.PatchRow{
		ID:             p.ID,
		Name:           p.Name,
		Date:           p.Date,
		State:          p.State,
		Submitter:      p.Submitter.Name,
		SubmitterEmail: p.Submitter.Email,
		WebURL:         p.WebURL,
		MsgID:          p.MsgID,
		MboxURL:        p.Mbox,
		Archived:       p.Archived,
	}
	if p.CommitRef != nil {
		r.CommitRef = *p.CommitRef
	}
	if p.Delegate != nil {
		r.DelegateID = p.Delegate.ID
		r.Delegate = p.Delegate.Username
		r.DelegateEmail = p.Delegate.Email
	}
	if len(p.Series) > 0 {
		r.SeriesID = p.Series[0].ID
	}
	return r
}

func ptrStr(p *string) string {
	if p == nil {
		return "<none>"
	}
	return *p
}

var (
	ackedByRe    = regexp.MustCompile(`(?i)^Acked-by:\s*(.+)`)
	fixesRe      = regexp.MustCompile(`(?i)^Fixes:\s*(.+)`)
	reviewedByRe = regexp.MustCompile(`(?i)^Reviewed-by:\s*(.+)`)
	testedByRe   = regexp.MustCompile(`(?i)^Tested-by:\s*(.+)`)
)

type tagMap = map[string]map[string]bool

func extractReviewTags(content string) tagMap {
	tags := tagMap{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, ">") {
			continue
		}
		if m := ackedByRe.FindStringSubmatch(line); m != nil {
			addTag(tags, "acked", strings.TrimSpace(m[1]))
		}
		if m := fixesRe.FindStringSubmatch(line); m != nil {
			addTag(tags, "fixes", strings.TrimSpace(m[1]))
		}
		if m := reviewedByRe.FindStringSubmatch(line); m != nil {
			addTag(tags, "reviewed", strings.TrimSpace(m[1]))
		}
		if m := testedByRe.FindStringSubmatch(line); m != nil {
			addTag(tags, "tested", strings.TrimSpace(m[1]))
		}
	}
	return tags
}

func addTag(tags tagMap, category, identity string) {
	if tags[category] == nil {
		tags[category] = map[string]bool{}
	}
	tags[category][identity] = true
}
