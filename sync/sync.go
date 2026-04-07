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

func pageProgress(pageNum, totalPages int) string {
	if totalPages > 0 {
		return fmt.Sprintf("page %d/%d", pageNum, totalPages)
	}
	return fmt.Sprintf("page %d", pageNum)
}

type Syncer struct {
	client *api.Client
	db     *db.DB
	cfg    *config.Config
	notify func(seriesIDs ...int)
	status *status.Registry

	// Channels for user-initiated requests, handled by runUserRequests.
	updateC   chan patchUpdateRequest
	commentC  chan CommentRequest
	checkC    chan int
	detailC   chan DetailRequest
	fetchAllC chan FetchAllRequest
	syncNowC  chan context.Context

	// Error cooldown: skip retrying failed fetches for 30 minutes.
	commentSkip map[int]time.Time
	detailSkip  map[int]time.Time
	checkSkip   map[int]time.Time
	seriesSkip  map[int]time.Time

	// Terminal-state fetch cooldown per loop.
	lastTerminalComment      time.Time
	lastTerminalCoverComment time.Time
	lastTerminalPatchDetail  time.Time
	lastTerminalCoverDetail  time.Time
	lastTerminalSeriesDetail time.Time
	lastTerminalCheck        time.Time
}

type patchUpdateRequest struct {
	patchID          int
	state            *string
	delegateUsername *string
	unsetDelegate    bool
	resultC          chan<- error
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

type DetailRequest struct {
	ID      int
	IsCover bool
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
	notify func(seriesIDs ...int),
	st *status.Registry,
) *Syncer {
	return &Syncer{
		client: client,
		db:     d,
		cfg:    cfg,
		notify: notify,
		status: st,

		updateC:     make(chan patchUpdateRequest, 4),
		commentC:    make(chan CommentRequest, 4),
		checkC:      make(chan int, 4),
		detailC:     make(chan DetailRequest, 4),
		fetchAllC:   make(chan FetchAllRequest, 4),
		syncNowC:    make(chan context.Context, 1),
		commentSkip: map[int]time.Time{},
		detailSkip:  map[int]time.Time{},
		checkSkip:   map[int]time.Time{},
		seriesSkip:  map[int]time.Time{},
	}
}

func (s *Syncer) RequestPatchUpdate(
	patchID int, state *string,
	delegateUsername *string, unsetDelegate bool,
) error {
	resultC := make(chan error, 1)
	s.updateC <- patchUpdateRequest{
		patchID:          patchID,
		state:            state,
		delegateUsername: delegateUsername,
		unsetDelegate:    unsetDelegate,
		resultC:          resultC,
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

func (s *Syncer) RequestDetail(id int, isCover bool) {
	select {
	case s.detailC <- DetailRequest{ID: id, IsCover: isCover}:
		s.status.Set(status.Detail, "Fetching...", true)
	default:
	}
}

func (s *Syncer) RequestFetchAll(seriesID, patchID int) {
	select {
	case s.fetchAllC <- FetchAllRequest{
		SeriesID: seriesID, PatchID: patchID}:
		s.status.Set(status.FetchAll, "Fetching...", true)
	default:
	}
}

func (s *Syncer) RequestSeriesCover(seriesID int) {
	ctx := api.WithNoRateLimit(context.Background())
	series, err := s.client.GetSeries(ctx, seriesID)
	if err != nil {
		log.Printf("SYNC: fetch series cover %d: %v",
			seriesID, err)
		return
	}
	s.db.SaveSeries(db.SeriesRow{
		ID: series.ID, Name: series.Name,
		Date: series.Date, Version: series.Version,
		Submitter:       series.Submitter.Name,
		SubmitterEmail:  series.Submitter.Email,
		WebURL:          series.WebURL,
		MboxURL:         series.Mbox,
		Complete:        series.ReceivedAll,
		TotalPatches:    series.Total,
		ReceivedPatches: series.ReceivedTotal,
	})
	if series.CoverLetter != nil {
		s.db.SaveCover(db.CoverRow{
			ID: series.CoverLetter.ID, SeriesID: series.ID,
			Name:    series.CoverLetter.Name,
			Date:    series.CoverLetter.Date,
			MsgID:   series.CoverLetter.MsgID,
			MboxURL: series.CoverLetter.Mbox,
			WebURL:  series.CoverLetter.WebURL,
		})
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
	syncInterval      = 5 * time.Minute // check for new events
	activeInterval    = 5 * time.Second // poll interval for all background loops
	terminalInterval  = 5 * time.Minute // cooldown between terminal-state fetches
	archiveInterval   = 5 * time.Minute // poll mail archive (only for Patchwork < 1.3)
	maintainerRefresh = 24 * time.Hour  // re-fetch project maintainer list
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
	s.incrementalSync(ctx)

	var wg gosync.WaitGroup
	wg.Add(9)
	go s.runUserRequests(ctx, &wg)
	go s.runSyncLoop(ctx, &wg)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextComments)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextCoverComments)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextPatchDetail)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextCoverDetail)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextChecks)
	go s.runBgLoop(ctx, &wg, activeInterval, s.fetchNextSeriesDetail)
	go s.runArchiveLoop(ctx, &wg)
	wg.Wait()
}

func (s *Syncer) runUserRequests(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.updateC:
			log.Printf("SYNC: update request for patch %d",
				req.patchID)
			req.resultC <- s.handlePatchUpdate(ctx, req)
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
				s.notify(sid)
			}()
		case req := <-s.detailC:
			go func() {
				noRL := WithFetchOrigin(
					api.WithNoRateLimit(ctx), OriginOnDemand)
				sid := s.lookupSeriesID(req.ID, req.IsCover)
				if req.IsCover {
					s.fetchDetailForCover(
						noRL, req.ID, sid, status.Detail)
				} else {
					s.fetchDetailForPatch(
						noRL, req.ID, sid, status.Detail)
				}
				s.status.Clear(status.Detail)
				s.notify(sid)
			}()
		case patchID := <-s.checkC:
			go func() {
				noRL := WithFetchOrigin(
					api.WithNoRateLimit(ctx), OriginOnDemand)
				sid := s.lookupSeriesID(patchID, false)
				s.fetchChecksForPatch(
					noRL, patchID, sid, status.BgChecks)
				s.status.Clear(status.BgChecks)
				s.notify(sid)
			}()
		case req := <-s.fetchAllC:
			go func() {
				fctx := WithFetchOrigin(ctx, OriginFetchAll)
				sid := req.SeriesID
				if sid != 0 {
					s.fetchAllForSeries(fctx, sid)
				} else if req.PatchID != 0 {
					sid = s.lookupSeriesID(req.PatchID, false)
					s.fetchAllForPatch(fctx, req.PatchID)
				}
				s.status.Clear(status.FetchAll)
				s.notify(sid)
			}()
		}
	}
}

func (s *Syncer) runSyncLoop(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	lastMaintainerRefresh := time.Now()

	doSync := func(syncCtx context.Context) {
		s.incrementalSync(syncCtx)
		s.status.Clear(status.Sync)
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
	fetch func(context.Context) int,
) {
	defer wg.Done()
	delay := interval
	if sid := fetch(ctx); sid != 0 {
		s.notify(sid)
	} else {
		delay += interval
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			if sid := fetch(ctx); sid != 0 {
				s.notify(sid)
				delay = interval
			} else if delay < 3*interval {
				delay += interval
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
	s.fetchAllActivePatches(ctx)

	// Fetch all patches (any state, including archived) and all
	// series from the older of the oldest active patch date and
	// the history target. This fills in terminal/archived patches
	// that fetchAllPatches skips.
	since := s.db.GetOldestPatchDate()
	if !s.cfg.HistoryLimit.IsZero() {
		target := s.cfg.HistoryLimit.Before().Format("2006-01-02T15:04:05")
		if since == "" || target < since {
			since = target
		}
	}
	if since != "" {
		s.fetchPatchesSince(ctx, since, status.Sync)
		s.fetchSeriesSince(ctx, since, status.Sync)
	}

	s.status.Set(status.Sync, "Fetching events...", true)
	s.fetchInitialEvents(ctx)
	s.db.RecomputeAllActiveFlags()
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

func (s *Syncer) fetchAllActivePatches(ctx context.Context) {
	pageURL := s.client.BuildPatchesURL(api.PatchListParams{
		State:    s.cfg.States,
		Project:  s.cfg.Project,
		Archived: "false",
	})

	pageNum := 0
	for pageURL != "" {
		pageNum++
		page, err := s.client.GetPatchesPage(ctx, pageURL)
		if err != nil {
			log.Printf("fetch patches: %v", err)
			return
		}
		if pageNum > 1 {
			s.status.Set(status.Sync,
				fmt.Sprintf("Fetching patches (%s)...",
					pageProgress(pageNum, page.TotalPages)), true)
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
	// Cap to 1 day back — older events are redundant since
	// patches and series were already fetched with full data.
	cap := time.Now().AddDate(0, 0, -1).Format("2006-01-02T15:04:05")
	since := oldest
	if since < cap {
		since = cap
	}
	s.fetchEventsSince(ctx, since, status.Sync)
}

func (s *Syncer) incrementalSync(ctx context.Context) {
	since := s.db.GetSyncState("last_event_date")
	if since == "" {
		// No event watermark — either initial fetch failed or
		// this is a fresh DB. Use 1-day fallback so we don't
		// permanently miss events.
		since = time.Now().AddDate(0, 0, -1).Format("2006-01-02T15:04:05")
		log.Printf("SYNC: no event watermark, using 1-day fallback")
	}
	s.status.Set(status.BgSync, "Checking events...", true)
	s.fetchEventsSince(ctx, since, status.BgSync)
	s.status.Clear(status.BgSync)
}

func (s *Syncer) fetchEventsSince(
	ctx context.Context, since string, statusKey status.Key,
) {
	lastID, _ := strconv.Atoi(s.db.GetSyncState("last_event_id"))
	pageURL := s.client.BuildEventsURL(api.EventListParams{
		Since:   since,
		Project: s.cfg.Project,
		Order:   "date",
	})

	pageNum := 0
	for pageURL != "" {
		pageNum++
		page, err := s.client.GetEventsPage(ctx, pageURL)
		if err != nil {
			log.Printf("fetch events: %v", err)
			return
		}
		if pageNum > 1 {
			s.status.Set(statusKey,
				fmt.Sprintf("Fetching events (%s)...",
					pageProgress(pageNum, page.TotalPages)), true)
		}
		skipped := 0
		affected := map[int]bool{}
		for _, ev := range page.Items {
			if ev.ID <= lastID {
				skipped++
				continue
			}
			log.Printf("SYNC: event %s: %s", ev.Category, eventSummary(ev))
			seriesID := seriesIDFromEvent(ev)
			if seriesID == 0 {
				seriesID = s.seriesIDForEventPatch(ev)
			}
			if err := s.processEvent(ev, seriesID); err != nil {
				log.Printf("SYNC: process event %d: %v", ev.ID, err)
			}
			if seriesID != 0 {
				s.db.RecomputeActiveFlag(seriesID)
				affected[seriesID] = true
			}
			s.db.SetSyncState("last_event_date", ev.Date)
			s.db.SetSyncState("last_event_id", strconv.Itoa(ev.ID))
		}
		log.Printf("SYNC: received %d events, %d new (page %d)",
			len(page.Items), len(page.Items)-skipped, pageNum)
		if len(affected) > 0 {
			ids := make([]int, 0, len(affected))
			for id := range affected {
				ids = append(ids, id)
			}
			s.notify(ids...)
		} else if len(page.Items)-skipped > 0 {
			s.notify()
		}
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

func (s *Syncer) seriesIDForEventPatch(ev api.Event) int {
	var patchID int
	switch p := ev.Payload.(type) {
	case *api.PatchCreatedPayload:
		patchID = p.Patch.ID
	case *api.PatchStateChangedPayload:
		patchID = p.Patch.ID
	case *api.PatchDelegatedPayload:
		patchID = p.Patch.ID
	case *api.CheckCreatedPayload:
		patchID = p.Patch.ID
	case *api.PatchCommentCreatedPayload:
		patchID = p.Patch.ID
	}
	if patchID == 0 {
		return 0
	}
	row, err := s.db.GetPatch(patchID)
	if err != nil || row == nil {
		return 0
	}
	return row.SeriesID
}

func eventSummary(ev api.Event) string {
	switch p := ev.Payload.(type) {
	case *api.PatchCreatedPayload:
		return fmt.Sprintf("patch %d %q", p.Patch.ID, p.Patch.Name)
	case *api.PatchStateChangedPayload:
		return fmt.Sprintf("patch %d %s → %s", p.Patch.ID, p.PreviousState, p.CurrentState)
	case *api.PatchDelegatedPayload:
		delegate := "(none)"
		if p.CurrentDelegate != nil {
			delegate = p.CurrentDelegate.Username
		}
		return fmt.Sprintf("patch %d → %s", p.Patch.ID, delegate)
	case *api.CheckCreatedPayload:
		return fmt.Sprintf("patch %d check %s %s", p.Patch.ID, p.Check.Context, p.Check.State)
	case *api.PatchCompletedPayload:
		return fmt.Sprintf("patch %d series %d", p.Patch.ID, p.Series.ID)
	case *api.SeriesCreatedPayload:
		return fmt.Sprintf("series %d %q", p.Series.ID, p.Series.Name)
	case *api.SeriesCompletedPayload:
		return fmt.Sprintf("series %d %q", p.Series.ID, p.Series.Name)
	case *api.CoverCreatedPayload:
		return fmt.Sprintf("cover %d %q", p.Cover.ID, p.Cover.Name)
	case *api.PatchCommentCreatedPayload:
		return fmt.Sprintf("patch %d", p.Patch.ID)
	case *api.CoverCommentCreatedPayload:
		return fmt.Sprintf("cover %d", p.Cover.ID)
	}
	return "(unknown)"
}

func (s *Syncer) processEvent(ev api.Event, seriesID int) error {
	switch p := ev.Payload.(type) {
	case *api.PatchCreatedPayload:
		return s.db.SavePatchSummary(p.Patch.ID, seriesID,
			p.Patch.Name, p.Patch.Date, p.Patch.MsgID, p.Patch.Mbox, p.Patch.WebURL)
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
		return s.db.SaveSeriesSummary(p.Series.ID, p.Series.Name, p.Series.Date, p.Series.Version)
	case *api.SeriesCompletedPayload:
		return s.db.SaveSeriesSummary(p.Series.ID, p.Series.Name, p.Series.Date, p.Series.Version)
	case *api.PatchCompletedPayload:
		s.db.SavePatchSummary(p.Patch.ID, p.Series.ID,
			p.Patch.Name, p.Patch.Date, p.Patch.MsgID, p.Patch.Mbox, p.Patch.WebURL)
		return s.db.SaveSeriesSummary(p.Series.ID, p.Series.Name, p.Series.Date, p.Series.Version)
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

func (s *Syncer) fetchNextComments(ctx context.Context) int {
	refs := s.db.GetPatchesNeedingComments(len(s.commentSkip) + 1)
	for _, ref := range refs {
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
			return 0
		}
		delete(s.commentSkip, ref.ID)
		s.status.SetTimed(status.BgComments,
			fmt.Sprintf("Comments fetched (%d remaining)",
				s.db.CountUnfetched("patches", "comments_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalComment = time.Now()
		}
		return ref.SeriesID
	}
	return 0
}

func (s *Syncer) fetchNextCoverComments(ctx context.Context) int {
	refs := s.db.GetCoversNeedingComments(len(s.commentSkip) + 1)
	for _, ref := range refs {
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
			return 0
		}
		delete(s.commentSkip, ref.ID)
		s.status.SetTimed(status.BgCoverComments,
			fmt.Sprintf("Cover comments fetched (%d remaining)",
				s.db.CountUnfetched("covers", "comments_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalCoverComment = time.Now()
		}
		return ref.SeriesID
	}
	return 0
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

func (s *Syncer) fetchNextPatchDetail(ctx context.Context) int {
	refs := s.db.GetPatchesNeedingDetail(len(s.detailSkip) + 1)
	for _, ref := range refs {
		if t, ok := s.detailSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalPatchDetail) < terminalInterval {
			break
		}
		if err := s.fetchDetailForPatch(ctx, ref.ID,
			ref.SeriesID, status.Detail); err != nil {
			s.detailSkip[ref.ID] = time.Now()
			return 0
		}
		delete(s.detailSkip, ref.ID)
		s.status.SetTimed(status.Detail,
			fmt.Sprintf("Patch details fetched (%d remaining)",
				s.db.CountUnfetched("patches", "detail_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalPatchDetail = time.Now()
		}
		return ref.SeriesID
	}
	return 0
}

func (s *Syncer) fetchNextCoverDetail(ctx context.Context) int {
	refs := s.db.GetCoversNeedingDetail(len(s.detailSkip) + 1)
	for _, ref := range refs {
		if t, ok := s.detailSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalCoverDetail) < terminalInterval {
			break
		}
		if err := s.fetchDetailForCover(ctx, ref.ID,
			ref.SeriesID, status.Detail); err != nil {
			s.detailSkip[ref.ID] = time.Now()
			return 0
		}
		delete(s.detailSkip, ref.ID)
		s.status.SetTimed(status.Detail,
			fmt.Sprintf("Cover details fetched (%d remaining)",
				s.db.CountUnfetched("covers", "detail_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalCoverDetail = time.Now()
		}
		return ref.SeriesID
	}
	return 0
}

// backfillHistory extends patch and series data backward to the
// configured history limit. Runs at every startup; self-healing
// via oldest-date checks so interrupted runs resume.
func (s *Syncer) backfillHistory(ctx context.Context) {
	if s.cfg.HistoryLimit.IsZero() {
		return
	}
	target := s.cfg.HistoryLimit.Before().Format("2006-01-02T15:04:05")
	s.fetchPatchesSince(ctx, target, status.History)
	s.fetchSeriesSince(ctx, target, status.History)
	s.db.RecomputeAllActiveFlags()
	s.status.Clear(status.History)
}

// fetchPatchesSince fetches all patches (any state, including archived)
// since the given date. Skips if the backfill_patches_since flag
// indicates this range was already searched.
func (s *Syncer) fetchPatchesSince(ctx context.Context, since string, statusKey status.Key) {
	searched := s.db.GetSyncState("backfill_patches_since")
	if searched != "" && searched <= since {
		return
	}

	pageURL := s.client.BuildPatchesURL(api.PatchListParams{
		Project: s.cfg.Project,
		Since:   since,
		Order:   "-date",
	})
	pageNum := 0

	for pageURL != "" {
		pageNum++
		page, err := s.client.GetPatchesPage(ctx, pageURL)
		if err != nil {
			log.Printf("SYNC: fetchPatchesSince: %v", err)
			return
		}
		s.status.Set(statusKey,
			fmt.Sprintf("Fetching all patches (%s)...",
				pageProgress(pageNum, page.TotalPages)), true)
		if len(page.Items) == 0 {
			return
		}
		for _, p := range page.Items {
			s.db.SavePatch(patchToRow(p))
			for _, ss := range p.Series {
				s.db.SaveSeriesSummary(ss.ID, ss.Name, ss.Date, ss.Version)
			}
		}
		s.notify()
		pageURL = page.NextURL
	}
	s.db.SetSyncState("backfill_patches_since", since)
}

// fetchSeriesSince fetches all series since the given date. Saves
// full series data (submitter, cover letter, completeness) and
// creates patch rows from each series' patch list.
func (s *Syncer) fetchSeriesSince(ctx context.Context, since string, statusKey status.Key) {
	searched := s.db.GetSyncState("backfill_series_since")
	if searched != "" && searched <= since {
		return
	}

	pageURL := s.client.BuildSeriesURL(api.SeriesListParams{
		Project: s.cfg.Project,
		Since:   since,
		Order:   "-date",
	})
	pageNum := 0

	for pageURL != "" {
		pageNum++
		page, err := s.client.GetSeriesPage(ctx, pageURL)
		if err != nil {
			log.Printf("SYNC: fetchSeriesSince: %v", err)
			return
		}
		s.status.Set(statusKey,
			fmt.Sprintf("Fetching all series (%s)...",
				pageProgress(pageNum, page.TotalPages)), true)
		if len(page.Items) == 0 {
			return
		}
		for _, sr := range page.Items {
			s.db.SaveSeries(db.SeriesRow{
				ID: sr.ID, Name: sr.Name, Date: sr.Date, Version: sr.Version,
				Submitter: sr.Submitter.Name, SubmitterEmail: sr.Submitter.Email,
				WebURL: sr.WebURL, MboxURL: sr.Mbox,
				Complete: sr.ReceivedAll, TotalPatches: sr.Total,
				ReceivedPatches: sr.ReceivedTotal,
			})
			for _, ps := range sr.Patches {
				s.db.SavePatchSummary(ps.ID, sr.ID,
					ps.Name, ps.Date, ps.MsgID, ps.Mbox, ps.WebURL)
			}
			s.db.UpdateSeriesPatches(sr.ID, sr.Submitter.Name, sr.Submitter.Email)
			if sr.CoverLetter != nil {
				s.db.SaveCover(db.CoverRow{
					ID: sr.CoverLetter.ID, SeriesID: sr.ID,
					Name: sr.CoverLetter.Name, Date: sr.CoverLetter.Date,
					MsgID: sr.CoverLetter.MsgID, MboxURL: sr.CoverLetter.Mbox,
					WebURL: sr.CoverLetter.WebURL,
				})
			}
		}
		s.notify()
		pageURL = page.NextURL
	}
	s.db.SetSyncState("backfill_series_since", since)
}

func (s *Syncer) fetchNextSeriesDetail(ctx context.Context) int {
	refs := s.db.GetSeriesNeedingDetail(len(s.seriesSkip) + 1)
	for _, ref := range refs {
		if t, ok := s.seriesSkip[ref.ID]; ok &&
			time.Since(t) < commentSkipCooldown {
			continue
		}
		if !ref.IsActive &&
			time.Since(s.lastTerminalSeriesDetail) < terminalInterval {
			break
		}
		s.status.StartFetchAndSetStatus(ref.ID, ref.ID, status.BgSync,
			fmt.Sprintf("Fetching series %d...", ref.ID))
		series, err := s.client.GetSeries(ctx, ref.ID)
		if err != nil {
			log.Printf("SYNC [%s]: fetch series detail %d: %v",
				fetchOrigin(ctx), ref.ID, err)
			s.status.EndFetch(ref.ID)
			s.seriesSkip[ref.ID] = time.Now()
			return 0
		}
		delete(s.seriesSkip, ref.ID)
		s.db.SaveSeries(db.SeriesRow{
			ID: series.ID, Name: series.Name, Date: series.Date, Version: series.Version,
			Submitter: series.Submitter.Name, SubmitterEmail: series.Submitter.Email,
			WebURL: series.WebURL, MboxURL: series.Mbox,
			Complete: series.ReceivedAll, TotalPatches: series.Total,
			ReceivedPatches: series.ReceivedTotal,
		})
		s.db.UpdateSeriesPatches(series.ID, series.Submitter.Name, series.Submitter.Email)
		if series.CoverLetter != nil {
			s.db.SaveCover(db.CoverRow{
				ID: series.CoverLetter.ID, SeriesID: series.ID,
				Name: series.CoverLetter.Name, Date: series.CoverLetter.Date,
				MsgID: series.CoverLetter.MsgID, MboxURL: series.CoverLetter.Mbox,
				WebURL: series.CoverLetter.WebURL,
			})
		}
		s.status.EndFetch(ref.ID)
		log.Printf("SYNC [%s]: fetched series detail %d %q (%d patches)",
			fetchOrigin(ctx), series.ID, series.Name, series.Total)
		s.status.SetTimed(status.BgSync,
			fmt.Sprintf("Series details fetched (%d remaining)",
				s.db.CountUnfetched("series", "detail_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalSeriesDetail = time.Now()
		}
		return ref.ID
	}
	return 0
}

func (s *Syncer) fetchNextChecks(ctx context.Context) int {
	refs := s.db.GetPatchesNeedingChecks(len(s.checkSkip) + 1)
	for _, ref := range refs {
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
			return 0
		}
		delete(s.checkSkip, ref.ID)
		s.status.SetTimed(status.BgChecks,
			fmt.Sprintf("Checks fetched (%d remaining)",
				s.db.CountUnfetched("patches", "checks_fetched")), 3*time.Second)
		if !ref.IsActive {
			s.lastTerminalCheck = time.Now()
		}
		return ref.SeriesID
	}
	return 0
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
	// Save full patch data (fixes series_id, submitter, delegate for
	// patches that arrived via events with incomplete info).
	s.db.SavePatch(patchToRow(detail.Patch))
	for _, ss := range detail.Series {
		s.db.SaveSeriesSummary(ss.ID, ss.Name, ss.Date, ss.Version)
	}
	if seriesID != 0 {
		s.db.RecomputeActiveFlag(seriesID)
	}
	prefixes, _ := json.Marshal(detail.Prefixes)
	headers := filterHeaders(detail.Headers)
	s.db.UpdatePatchDetail(patchID,
		detail.Content, detail.Diff,
		headers, string(prefixes))
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
	hdrs := filterHeaders(cover.Headers)
	s.db.UpdateCoverDetail(coverID,
		cover.Content, hdrs)
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
	"Subject", "From", "Reply-To", "To", "Cc", "Date",
	"In-Reply-To", "References", "Message-ID", "Message-Id",
	"Content-Type", "Content-Transfer-Encoding", "MIME-Version",
	"User-Agent", "X-Mailer",
	"List-Id", "Sender",
	"X-Patchwork-Delegate", "X-Patchwork-State",
	"X-B4-Tracking",
	"X-Developer-Key", "X-Developer-Signature",
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

func (s *Syncer) handlePatchUpdate(
	ctx context.Context, req patchUpdateRequest,
) error {
	dlgStr := ptrStr(req.delegateUsername)
	if req.unsetDelegate {
		dlgStr = "(unset)"
	}
	log.Printf("SYNC: handlePatchUpdate patch %d state=%s delegate=%s",
		req.patchID, ptrStr(req.state), dlgStr)
	s.status.Set(status.Update, "Updating...", true)
	ctx = api.WithNoRateLimit(ctx)

	update := api.PatchUpdate{State: req.state}
	if req.unsetDelegate {
		update.UnsetDelegate = true
	} else if req.delegateUsername != nil {
		uid, err := s.resolveUserID(ctx, *req.delegateUsername)
		if err != nil {
			log.Printf("SYNC: resolve delegate %q: %v",
				*req.delegateUsername, err)
			s.status.SetTimed(status.Update,
				"Failed to resolve delegate: "+
					err.Error(), 5*time.Second)
			return err
		}
		update.Delegate = &uid
	}
	_, err := s.client.UpdatePatch(ctx, req.patchID, update)
	if err != nil {
		log.Printf("SYNC: patch update %d failed: %v", req.patchID, err)
		// "Invalid pk" means the cached user ID is stale —
		// clear it so the next attempt re-resolves the username.
		if req.delegateUsername != nil &&
			strings.Contains(err.Error(), "Invalid pk") {
			s.db.ClearMaintainerUserID(*req.delegateUsername)
		}
		s.status.SetTimed(status.Update,
			fmt.Sprintf("Update failed: %v", err), 5*time.Second)
		return err
	}
	log.Printf("SYNC: patch update %d success, syncing events", req.patchID)
	s.incrementalSync(ctx)
	sid := s.lookupSeriesID(req.patchID, false)
	if sid != 0 {
		s.notify(sid)
	} else {
		s.notify()
	}
	s.status.SetTimed(status.Update, "Updated", 3*time.Second)
	return nil
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
