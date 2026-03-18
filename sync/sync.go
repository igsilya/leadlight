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
)

type Syncer struct {
	client    *api.Client
	db        *db.DB
	cfg       *config.Config
	notify    func()
	priorityC chan int
	mboxC     chan MboxRequest
	updateC   chan PatchUpdateRequest
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
	PatchID    int
	State      *string
	DelegateID *int
	ResultC    chan<- error
}

func NewSyncer(
	client *api.Client,
	d *db.DB,
	cfg *config.Config,
	notify func(),
) *Syncer {
	return &Syncer{
		client:    client,
		db:        d,
		cfg:       cfg,
		notify:    notify,
		priorityC: make(chan int, 16),
		mboxC:     make(chan MboxRequest, 4),
		updateC:   make(chan PatchUpdateRequest, 4),
	}
}

func (s *Syncer) PrioritizeSeries(seriesID int) {
	select {
	case s.priorityC <- seriesID:
	default:
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

const (
	syncInterval      = 30 * time.Second
	commentInterval   = 5 * time.Second
	archiveInterval   = 5 * time.Minute
	maintainerRefresh = 24 * time.Hour
)

func (s *Syncer) needsArchiveMonitoring() bool {
	return s.cfg.APIVersion < "1.3" && s.cfg.MailArchive != ""
}

func (s *Syncer) Run(ctx context.Context) {
	complete := s.db.GetSyncState("initial_sync_complete")
	if complete != "true" {
		s.initialSync(ctx)
		s.notify()
	}

	var wg gosync.WaitGroup
	wg.Add(4)
	go s.runUserRequests(ctx, &wg)
	go s.runSyncLoop(ctx, &wg)
	go s.runCommentLoop(ctx, &wg)
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
			if req.IsCover {
				log.Printf("SYNC: cover mbox request for %d",
					req.PatchID)
				req.ResultC <- s.fetchCoverMbox(
					ctx, req.PatchID)
			} else {
				log.Printf("SYNC: patch mbox request for %d",
					req.PatchID)
				req.ResultC <- s.fetchMbox(
					ctx, req.PatchID, false)
			}
		case req := <-s.updateC:
			log.Printf("SYNC: update request for patch %d",
				req.PatchID)
			req.ResultC <- s.handlePatchUpdate(ctx, req)
		case seriesID := <-s.priorityC:
			s.fetchSeriesDetail(ctx, seriesID)
			s.notify()
		}
	}
}

func (s *Syncer) runSyncLoop(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()

	s.incrementalSync(ctx)
	s.fetchMissingSeries(ctx)
	s.fixIncompletePatches(ctx)
	s.notify()

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	lastMaintainerRefresh := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.incrementalSync(ctx)
			s.fixIncompletePatches(ctx)
			s.notify()

			if time.Since(lastMaintainerRefresh) > maintainerRefresh {
				s.fetchMaintainers(ctx)
				lastMaintainerRefresh = time.Now()
			}
		}
	}
}

func (s *Syncer) runCommentLoop(ctx context.Context, wg *gosync.WaitGroup) {
	defer wg.Done()

	s.fetchNextComments(ctx)
	s.notify()

	ticker := time.NewTicker(commentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fetchNextComments(ctx)
			s.notify()
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
	s.fetchListPages(ctx)
	s.notify()
	s.fetchMaintainers(ctx)
	s.fetchAllPatches(ctx)
	s.fetchMissingSeries(ctx)
	s.fetchInitialEvents(ctx)
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

			s.db.UpdatePatchTags(p.PatchID,
				p.AckedBy, p.Fixes,
				p.ReviewedBy, p.TestedBy)
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

	for pageURL != "" {
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
	s.fetchEventsSince(ctx, oldest)
}

func (s *Syncer) incrementalSync(ctx context.Context) {
	since := s.db.GetSyncState("last_event_date")
	if since == "" {
		return
	}
	s.fetchEventsSince(ctx, since)
}

func (s *Syncer) fetchEventsSince(ctx context.Context, since string) {
	pageURL := s.client.BuildEventsURL(api.EventListParams{
		Since:   since,
		Project: s.cfg.Project,
		Order:   "date",
	})

	for pageURL != "" {
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
		err := s.db.InsertCheck(db.CheckRow{
			ID:        p.Check.ID,
			PatchID:   p.Patch.ID,
			Context:   p.Check.Context,
			State:     p.Check.State,
			TargetURL: ptrStr(p.Check.TargetURL),
			Date:      p.Check.Date,
		})
		if err != nil {
			return err
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
		return nil
	}
	return nil
}

func (s *Syncer) fetchMissingSeries(ctx context.Context) {
	for {
		since := s.db.GetOldestIncompleteSeriesDate()
		if since == "" {
			return
		}
		log.Printf("SYNC: fetching series since %s", since)

		pageURL := s.client.BuildSeriesURL(s.cfg.Project, since)
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
		detail, err := s.client.GetPatch(ctx, id)
		if err != nil {
			log.Printf("SYNC: fixIncomplete patch %d: %v",
				id, err)
			s.db.UpdatePatchDetail(id, "", "", "", "")
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

func (s *Syncer) fetchSeriesDetail(
	ctx context.Context, seriesID int,
) {
	patches := s.db.GetPatchesForSeries(seriesID)
	for _, p := range patches {
		if p.DetailFetched {
			continue
		}
		detail, err := s.client.GetPatch(ctx, p.ID)
		if err != nil {
			log.Printf("fetch detail %d: %v", p.ID, err)
			s.db.UpdatePatchDetail(p.ID, "", "", "", "")
			continue
		}
		prefixes, _ := json.Marshal(detail.Prefixes)
		headers, _ := json.Marshal(detail.Headers)
		s.db.UpdatePatchDetail(p.ID,
			detail.Content, detail.Diff,
			string(headers), string(prefixes))
	}
}

func (s *Syncer) fetchNextComments(ctx context.Context) {
	ids := s.db.GetPatchesNeedingComments(s.cfg.States)
	if len(ids) == 0 {
		return
	}

	patchID := ids[0]
	row, _ := s.db.GetPatch(patchID)
	if row != nil {
		log.Printf("SYNC: fetchNextComments: patch %d %q",
			patchID, row.Name)
	}
	comments, err := s.client.GetPatchComments(ctx, patchID)
	if err != nil {
		log.Printf("fetch comments %d: %v", patchID, err)
		s.db.MarkCommentsFetched(patchID)
		return
	}

	for _, c := range comments {
		s.db.InsertComment(db.CommentRow{
			ID:        c.ID,
			PatchID:   patchID,
			Submitter: c.Submitter.Name,
			Date:      c.Date,
			Subject:   c.Subject,
			Content:   c.Content,
			MsgID:     c.MsgID,
		})
	}
	s.updatePatchTagsFromComments(patchID)
	s.db.MarkCommentsFetched(patchID)
}

func (s *Syncer) checkMailArchive(ctx context.Context) {
	now := time.Now()
	s.checkArchiveMonth(ctx, now.Year(), now.Month())
	if now.Day() <= 2 {
		prev := now.AddDate(0, -1, 0)
		s.checkArchiveMonth(
			ctx, prev.Year(), prev.Month())
	}
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
	matchedIDs := api.MatchPatchSubjects(newMsgs, patchNames)
	for _, id := range matchedIDs {
		s.db.ResetCommentsFetched(id)
	}

	if len(matchedIDs) > 0 {
		log.Printf("archive: %d new messages, %d patches to re-check",
			len(newMsgs), len(matchedIDs))
	}

	maxNum := lastSeen
	for _, m := range newMsgs {
		if m.Number > maxNum {
			maxNum = m.Number
		}
	}
	s.db.SetSyncState(monthKey, strconv.Itoa(maxNum))
}

func (s *Syncer) updatePatchTagsFromComments(
	patchID int,
) {
	comments := s.db.GetComments(patchID)
	merged := tagMap{}

	for _, c := range comments {
		tags := extractReviewTags(c.Content)
		merged = mergeTagMaps(merged, tags)
	}

	s.db.UpdatePatchTags(patchID,
		len(merged["acked"]),
		len(merged["fixes"]),
		len(merged["reviewed"]),
		len(merged["tested"]),
	)
}

func (s *Syncer) handlePatchUpdate(ctx context.Context, req PatchUpdateRequest) error {
	log.Printf("SYNC: handlePatchUpdate patch %d state=%v delegate=%v",
		req.PatchID, req.State, req.DelegateID)
	update := api.PatchUpdate{
		State:    req.State,
		Delegate: req.DelegateID,
	}
	_, err := s.client.UpdatePatch(ctx, req.PatchID, update)
	if err != nil {
		log.Printf("SYNC: patch update %d failed: %v", req.PatchID, err)
		return err
	}
	log.Printf("SYNC: patch update %d success, syncing events", req.PatchID)
	s.incrementalSync(ctx)
	s.notify()
	return nil
}

func (s *Syncer) fetchMbox(
	ctx context.Context, patchID int, rateLimit bool,
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
		msgid := strings.Trim(row.MsgID, "<>")
		loreURL := strings.TrimRight(
			s.cfg.LoreURL, "/") + "/" + msgid + "/raw"
		log.Printf("SYNC: fetchMbox(%d): trying lore %s",
			patchID, loreURL)
		content, err := s.client.GetMbox(ctx, loreURL, rateLimit)
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
		mboxURL := row.MboxURL
		if strings.HasPrefix(s.cfg.Server, "https://") &&
			strings.HasPrefix(mboxURL, "http://") {
			mboxURL = "https://" + mboxURL[len("http://"):]
		}
		log.Printf("SYNC: fetchMbox(%d): trying patchwork %s",
			patchID, mboxURL)
		content, err := s.client.GetMbox(ctx, mboxURL, rateLimit)
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
		content, err := s.client.GetMbox(ctx, loreURL, false)
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
		content, err := s.client.GetMbox(ctx, mboxURL, false)
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
		return ""
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

func mergeTagMaps(maps ...tagMap) tagMap {
	result := tagMap{}
	for _, m := range maps {
		for cat, identities := range m {
			if result[cat] == nil {
				result[cat] = map[string]bool{}
			}
			for id := range identities {
				result[cat][id] = true
			}
		}
	}
	return result
}
