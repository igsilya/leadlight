package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	gosync "sync"
	"testing"
	"time"

	"leadlight/api"
	"leadlight/config"
	"leadlight/db"
)

const testProjectJSON = `{
	"id":1, "url":"", "name":"test",
	"link_name":"test", "list_id":"",
	"list_email":"", "web_url":"",
	"scm_url":"", "webscm_url":"",
	"list_archive_url":"",
	"list_archive_url_format":"",
	"commit_url_format":""
}`

func setupSyncer(
	t *testing.T,
	handler http.Handler,
) (*Syncer, *db.DB) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	cfg := &config.Config{
		Server:     srv.URL,
		Project:    "test-project",
		BaseURL:    srv.URL,
		APIVersion: "1.2",
		States:     []string{"new", "under-review"},
	}

	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})
	return s, d
}

func savePatch(d *db.DB, id int, name, date, state string) {
	d.SavePatch(db.PatchRow{
		ID: id, Name: name,
		Date: date, State: state,
		Submitter: "Lorem",
	})
}

func TestProcessEvent_PatchCreated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	d.SaveSeriesSummary(50, "Lorem", "2026-03-10", 1)

	ev := api.Event{
		ID:       1,
		Category: "patch-created",
		Date:     "2026-03-10T12:00:00",
		Payload: &api.PatchCreatedPayload{
			Patch: api.PatchSummary{
				ID:   100,
				Name: "[dev] Lorem ipsum",
				Date: "2026-03-10T12:00:00",
			},
		},
	}

	if err := s.processEvent(ev, 50); err != nil {
		t.Fatal(err)
	}

	row, err := d.GetPatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "[dev] Lorem ipsum" {
		t.Errorf("Name = %q", row.Name)
	}
}

func TestProcessEvent_PatchStateChanged(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")

	ev := api.Event{
		Category: "patch-state-changed",
		Payload: &api.PatchStateChangedPayload{
			Patch:         api.PatchSummary{ID: 100},
			PreviousState: "new",
			CurrentState:  "under-review",
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.State != "under-review" {
		t.Errorf("State = %q", row.State)
	}
}

func TestProcessEvent_PatchDelegated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")

	ev := api.Event{
		Category: "patch-delegated",
		Payload: &api.PatchDelegatedPayload{
			Patch:            api.PatchSummary{ID: 100},
			PreviousDelegate: nil,
			CurrentDelegate: &api.User{
				ID:       55,
				Username: "dolor",
				Email:    "dolor@ipsum.example",
			},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.Delegate != "dolor" {
		t.Errorf("Delegate = %q", row.Delegate)
	}
}

func TestProcessEvent_CheckCreated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")

	ev := api.Event{
		Category: "check-created",
		Payload: &api.CheckCreatedPayload{
			Patch: api.PatchSummary{ID: 100},
			Check: api.CheckSummary{
				ID:      500,
				State:   "success",
				Context: "ci/build",
				Date:    "2026-03-10T13:00:00",
			},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}
	_ = d
}

func TestProcessEvent_SeriesCreated(t *testing.T) {
	s, _ := setupSyncer(t, http.NotFoundHandler())

	ev := api.Event{
		Category: "series-created",
		Payload: &api.SeriesCreatedPayload{
			Series: api.SeriesSummary{
				ID:      50,
				Name:    "Lorem series",
				Date:    "2026-03-10T12:00:00",
				Version: 1,
			},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}
}

func TestProcessEvent_UnknownPayload(t *testing.T) {
	s, _ := setupSyncer(t, http.NotFoundHandler())

	ev := api.Event{
		Category: "some-future-event",
		Payload:  nil,
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractReviewTags(t *testing.T) {
	content := "Looks good to me.\n\n" +
		"Acked-by: Lorem <lorem@ipsum.example>\n" +
		"Reviewed-by: Dolor <dolor@amet.example>\n" +
		"Tested-by: Sit <sit@amet.example>\n"

	tags := extractReviewTags(content)
	if len(tags["acked"]) != 1 {
		t.Errorf("acked = %d", len(tags["acked"]))
	}
	if len(tags["reviewed"]) != 1 {
		t.Errorf("reviewed = %d", len(tags["reviewed"]))
	}
	if len(tags["tested"]) != 1 {
		t.Errorf("tested = %d", len(tags["tested"]))
	}
}

func TestExtractReviewTags_SkipsQuoted(t *testing.T) {
	content := "> Acked-by: Lorem <lorem@ipsum.example>\n" +
		"> Reviewed-by: Dolor <dolor@amet.example>\n" +
		"Tested-by: Sit <sit@amet.example>\n"

	tags := extractReviewTags(content)
	if len(tags["acked"]) != 0 {
		t.Errorf("acked = %d, want 0 (quoted)",
			len(tags["acked"]))
	}
	if len(tags["reviewed"]) != 0 {
		t.Errorf("reviewed = %d, want 0 (quoted)",
			len(tags["reviewed"]))
	}
	if len(tags["tested"]) != 1 {
		t.Errorf("tested = %d", len(tags["tested"]))
	}
}

func TestExtractReviewTags_Dedup(t *testing.T) {
	c1 := "Acked-by: Lorem <lorem@ipsum.example>\n"
	c2 := "Acked-by: Lorem <lorem@ipsum.example>\n" +
		"Acked-by: Dolor <dolor@amet.example>\n"

	all := mergeTagMaps(
		extractReviewTags(c1),
		extractReviewTags(c2),
	)
	if len(all["acked"]) != 2 {
		t.Errorf("acked = %d, want 2 (deduped)",
			len(all["acked"]))
	}
}

func TestExtractReviewTags_Empty(t *testing.T) {
	tags := extractReviewTags("")
	if len(tags) != 0 {
		t.Errorf("tags = %v, want empty", tags)
	}
}

func TestExtractReviewTags_Fixes(t *testing.T) {
	content := "Fixes: abc123def (\"lorem ipsum\")\n"
	tags := extractReviewTags(content)
	if len(tags["fixes"]) != 1 {
		t.Errorf("fixes = %d", len(tags["fixes"]))
	}
}

func TestFetchMbox_Cached(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")
	d.UpdatePatchMbox(100, "cached mbox content")

	result := s.fetchMbox(context.Background(), 100, false)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != "cached mbox content" {
		t.Errorf("Content = %q", result.Content)
	}
}

const testMboxContent = "From patchwork Mon Mar 10 12:00:00 2026\n" +
	"Subject: [PATCH] Lorem ipsum\n" +
	"From: Lorem <lorem@ipsum.example>\n" +
	"\nLorem ipsum dolor sit amet.\n"

func TestFetchMbox_FromLore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/lorem-001@ipsum.example/raw" {
				w.Write([]byte(testMboxContent))
				return
			}
			w.WriteHeader(404)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		LoreURL: srv.URL + "/",
	}

	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	d.SavePatch(db.PatchRow{
		ID: 100, Name: "test",
		Date:      "2026-03-10",
		State:     "new",
		MsgID:     "<lorem-001@ipsum.example>",
		Submitter: "Lorem",
	})

	result := s.fetchMbox(context.Background(), 100, false)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != testMboxContent {
		t.Errorf("Content = %q", result.Content)
	}

	row, _ := d.GetPatch(100)
	if row.MboxContent != testMboxContent {
		t.Errorf("cached = %q", row.MboxContent)
	}
}

func TestFetchMbox_FromPatchwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patch/100/mbox/" {
				w.Write([]byte(testMboxContent))
				return
			}
			w.WriteHeader(404)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
	}

	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	d.SavePatch(db.PatchRow{
		ID: 100, Name: "test",
		Date:      "2026-03-10",
		State:     "new",
		MboxURL:   srv.URL + "/patch/100/mbox/",
		Submitter: "Lorem",
	})

	result := s.fetchMbox(context.Background(), 100, false)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != testMboxContent {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestIncrementalSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events/" {
				w.Write([]byte(`[{
					"id": 10,
					"category": "patch-state-changed",
					"project": ` + testProjectJSON + `,
					"date": "2026-03-11T00:00:00",
					"actor": null,
					"payload": {
						"patch": {
							"id": 100, "url": "",
							"web_url": "", "msgid": "",
							"list_archive_url": null,
							"date": "", "name": "",
							"mbox": ""
						},
						"previous_state": "new",
						"current_state": "accepted"
					}
				}]`))
				return
			}
			w.WriteHeader(404)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	savePatch(d, 100, "test", "2026-03-10", "new")
	d.SetSyncState("last_event_date", "2026-03-10T00:00:00")
	d.SetSyncState("initial_sync_complete", "true")

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test-project",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})
	s.incrementalSync(context.Background())

	row, _ := d.GetPatch(100)
	if row.State != "accepted" {
		t.Errorf("State = %q, want accepted", row.State)
	}

	lastDate := d.GetSyncState("last_event_date")
	if lastDate != "2026-03-11T00:00:00" {
		t.Errorf("last_event_date = %q", lastDate)
	}
}

func TestInitialSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/projects/test-project/":
				json.NewEncoder(w).Encode(api.Project{
					ProjectSummary: api.ProjectSummary{
						ID:   1,
						Name: "Lorem Project",
					},
					Maintainers: []api.User{
						{ID: 10, Username: "lorem"},
					},
				})
			case r.URL.Path == "/patches/":
				json.NewEncoder(w).Encode([]api.Patch{
					{
						ID:    100,
						Name:  "Lorem patch",
						Date:  "2026-03-10T12:00:00",
						State: "new",
						Submitter: api.Person{
							Name:  "Lorem",
							Email: "l@i.example",
						},
						Series: []api.SeriesSummary{
							{
								ID:   50,
								Name: "Lorem series",
								Date: "2026-03-10",
							},
						},
					},
				})
			case r.URL.Path == "/events/":
				json.NewEncoder(w).Encode([]api.Event{})
			default:
				w.WriteHeader(404)
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	cfg := &config.Config{
		Server:     srv.URL,
		Project:    "test-project",
		BaseURL:    srv.URL,
		APIVersion: "1.2",
		States:     []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})
	s.initialSync(context.Background())

	v := d.GetSyncState("initial_sync_complete")
	if v != "true" {
		t.Error("initial_sync_complete not set")
	}

	row, err := d.GetPatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "Lorem patch" {
		t.Errorf("Name = %q", row.Name)
	}

	maintainers := d.GetMaintainers()
	if len(maintainers) != 1 {
		t.Errorf("maintainers = %d", len(maintainers))
	}
}

func TestProcessEvent_CoverCreated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	d.SaveSeriesSummary(50, "Lorem", "2026-03-10", 1)

	ev := api.Event{
		Category: "cover-created",
		Payload: &api.CoverCreatedPayload{
			Cover: api.CoverSummary{
				ID:   200,
				Name: "[PATCH 0/3] Lorem cover",
				Date: "2026-03-10T12:00:00",
			},
		},
	}

	if err := s.processEvent(ev, 50); err != nil {
		t.Fatal(err)
	}

	// Cover should be in covers table, not patches
	cover, err := d.GetCover(50)
	if err != nil {
		t.Fatal("cover not found:", err)
	}
	if cover.Name != "[PATCH 0/3] Lorem cover" {
		t.Errorf("cover.Name = %q", cover.Name)
	}

	// Should NOT be in patches table
	_, err = d.GetPatch(200)
	if err == nil {
		t.Error("cover ID should not be in patches table")
	}
}

func TestFetchNextDetail_404MarksAsFetched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"Not found."}`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	savePatch(d, 100, "test", "2026-03-10", "new")

	// Verify it needs detail
	ids := d.GetPatchesNeedingDetail()
	if len(ids) != 1 || ids[0] != 100 {
		t.Fatalf("needs detail = %v, want [100]", ids)
	}

	// Fetch detail — will get 404
	s.fetchNextDetail(context.Background())

	// Should be marked as fetched despite 404
	ids = d.GetPatchesNeedingDetail()
	if len(ids) != 0 {
		t.Errorf("needs detail = %v, want empty (404 should mark as fetched)",
			ids)
	}
}

func TestFetchEvents_NotifiesPerPage(t *testing.T) {
	pageNum := 0
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			pageNum++
			switch pageNum {
			case 1:
				w.Header().Set("Link",
					fmt.Sprintf(
						`<%s/events/?page=2>; rel="next"`,
						srvURL))
				w.Write([]byte(`[{
					"id":1,
					"category":"patch-state-changed",
					"project":` + testProjectJSON + `,
					"date":"2026-03-11T01:00:00",
					"actor":null,
					"payload":{
						"patch":{"id":100,"url":"",
							"web_url":"","msgid":"",
							"list_archive_url":null,
							"date":"","name":"","mbox":""},
						"previous_state":"new",
						"current_state":"under-review"
					}
				}]`))
			case 2:
				w.Write([]byte(`[{
					"id":2,
					"category":"patch-state-changed",
					"project":` + testProjectJSON + `,
					"date":"2026-03-11T02:00:00",
					"actor":null,
					"payload":{
						"patch":{"id":101,"url":"",
							"web_url":"","msgid":"",
							"list_archive_url":null,
							"date":"","name":"","mbox":""},
						"previous_state":"new",
						"current_state":"accepted"
					}
				}]`))
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 101, "p2", "2026-03-10", "new")
	d.SetSyncState("last_event_date", "2026-03-10")
	srvURL = srv.URL

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test-project",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	notifyCount := 0
	s := NewSyncer(client, d, cfg, func() {
		notifyCount++
	})

	s.fetchEventsSince(context.Background(), "2026-03-10")

	if notifyCount != 2 {
		t.Errorf("notify count = %d, want 2 (one per page)",
			notifyCount)
	}

	// Verify both events were processed
	r1, _ := d.GetPatch(100)
	if r1.State != "under-review" {
		t.Errorf("patch 100 state = %q", r1.State)
	}
	r2, _ := d.GetPatch(101)
	if r2.State != "accepted" {
		t.Errorf("patch 101 state = %q", r2.State)
	}
}

func TestFetchPatches_NotifiesPerPage(t *testing.T) {
	pageNum := 0
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/patches/" {
				w.WriteHeader(404)
				return
			}
			pageNum++
			switch pageNum {
			case 1:
				w.Header().Set("Link",
					fmt.Sprintf(
						`<%s/patches/?page=2>; rel="next"`,
						srvURL))
				json.NewEncoder(w).Encode([]api.Patch{{
					ID: 100, Name: "p1",
					Date: "2026-03-10", State: "new",
					Submitter: api.Person{Name: "Lorem"},
				}})
			case 2:
				json.NewEncoder(w).Encode([]api.Patch{{
					ID: 200, Name: "p2",
					Date: "2026-03-10", State: "new",
					Submitter: api.Person{Name: "Ipsum"},
				}})
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	srvURL = srv.URL

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test-project",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	notifyCount := 0
	s := NewSyncer(client, d, cfg, func() {
		notifyCount++
	})

	s.fetchAllPatches(context.Background())

	if notifyCount != 2 {
		t.Errorf("notify count = %d, want 2 (one per page)",
			notifyCount)
	}

	// Verify both patches saved
	_, err := d.GetPatch(100)
	if err != nil {
		t.Error("patch 100 not found")
	}
	_, err = d.GetPatch(200)
	if err != nil {
		t.Error("patch 200 not found")
	}
}

func TestFetchNextComments_Progresses(t *testing.T) {
	reqPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			reqPaths = append(reqPaths, r.URL.Path)
			switch r.URL.Path {
			case "/patches/100/comments/":
				json.NewEncoder(w).Encode([]api.Comment{{
					ID:      301,
					Date:    "2026-03-11",
					Subject: "Re: p1",
					Submitter: api.Person{
						Name: "Lorem"},
					Content: "Acked-by: Lorem " +
						"<lorem@ipsum.example>",
					MsgID: "<r1@example>",
				}})
			case "/patches/101/comments/":
				json.NewEncoder(w).Encode([]api.Comment{})
			default:
				w.WriteHeader(404)
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 101, "p2", "2026-03-10", "new")

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	// First call: fetches comments for patch 100
	s.fetchNextComments(context.Background())

	row, _ := d.GetPatch(100)
	if row.AckedBy != 1 {
		t.Errorf("patch 100 AckedBy = %d, want 1",
			row.AckedBy)
	}

	// Second call: should progress to patch 101
	s.fetchNextComments(context.Background())

	// Verify patch 101 was fetched (not 100 again)
	needs := d.GetPatchesNeedingComments([]string{"new"})
	if len(needs) != 0 {
		t.Errorf("still needing comments: %v", needs)
	}

	// Verify we requested both patches' comments
	gotPaths := map[string]bool{}
	for _, p := range reqPaths {
		gotPaths[p] = true
	}
	if !gotPaths["/patches/100/comments/"] {
		t.Error("never fetched comments for 100")
	}
	if !gotPaths["/patches/101/comments/"] {
		t.Error("never fetched comments for 101")
	}
}

func TestProcessEvent_PatchCommentCreated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")
	d.MarkCommentsFetched(100)

	// Verify it's marked as fetched
	ids := d.GetPatchesNeedingComments([]string{"new"})
	if len(ids) != 0 {
		t.Fatal("should be fetched before event")
	}

	ev := api.Event{
		Category: "patch-comment-created",
		Payload: &api.PatchCommentCreatedPayload{
			Patch: api.PatchSummary{ID: 100},
			Comment: api.CommentSummary{
				ID:   301,
				Name: "Re: test",
			},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	// Should be reset — needs re-fetch
	ids = d.GetPatchesNeedingComments([]string{"new"})
	if len(ids) != 1 || ids[0] != 100 {
		t.Errorf("got %v, want [100] (reset by event)",
			ids)
	}
}

func TestCheckMailArchive(t *testing.T) {
	archiveHTML := `<HTML><BODY><ul>
<LI><A HREF="100.html">[dev] [PATCH] Lorem ipsum dolor
</A><A NAME="100">&nbsp;</A>
<I>Lorem</I>
<LI><A HREF="101.html">Re: [dev] [PATCH] Lorem ipsum dolor
</A><A NAME="101">&nbsp;</A>
<I>Dolor</I>
<LI><A HREF="102.html">[dev] [PATCH] Unrelated subject
</A><A NAME="102">&nbsp;</A>
<I>Amet</I>
</ul></BODY></HTML>`

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(archiveHTML))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 1000, SeriesID: 50,
		Name:      "[dev,v1] Lorem ipsum dolor",
		Date:      "2026-03-10",
		State:     "new",
		Submitter: "Lorem",
	})
	d.SavePatch(db.PatchRow{
		ID: 1001, SeriesID: 50,
		Name:      "[dev] Something completely different",
		Date:      "2026-03-10",
		State:     "new",
		Submitter: "Lorem",
	})
	// Non-active patch that still matches the archive
	d.SaveSeriesSummary(51, "Old series", "2026-01-01", 1)
	d.SavePatch(db.PatchRow{
		ID: 1002, SeriesID: 51,
		Name:      "[dev] Unrelated subject",
		Date:      "2026-01-01",
		State:     "accepted",
		Submitter: "Lorem",
	})
	d.MarkCommentsFetched(1000)
	d.MarkCommentsFetched(1001)
	d.MarkCommentsFetched(1002)

	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	s.checkMailArchive(context.Background())

	// Patch 1000 should be reset (subject matches)
	ids := d.GetPatchesNeedingComments([]string{"new"})
	got := map[int]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[1000] {
		t.Error("patch 1000 should be reset (matches archive)")
	}
	if got[1001] {
		t.Error("patch 1001 should NOT be reset (no match)")
	}
	// Non-active patch should also be matched
	if !got[1002] {
		t.Error("patch 1002 (accepted) should be reset" +
			" (matches 'Unrelated subject' in archive)")
	}

	// Last seen should be updated
	lastSeen := d.GetSyncState(
		fmt.Sprintf("last_archive_msg:%d-%s",
			time.Now().Year(), time.Now().Month()))
	if lastSeen != "102" {
		t.Errorf("last_archive_msg = %q, want 102",
			lastSeen)
	}
}

func TestCheckMailArchive_SkipsOldMessages(t *testing.T) {
	archiveHTML := `<HTML><BODY><ul>
<LI><A HREF="100.html">[dev] Old message
</A><A NAME="100">&nbsp;</A>
<LI><A HREF="200.html">[dev] New message matching Lorem
</A><A NAME="200">&nbsp;</A>
</ul></BODY></HTML>`

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(archiveHTML))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "Lorem", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 1000, SeriesID: 50,
		Name:      "[dev] Lorem",
		Date:      "2026-03-10",
		State:     "new",
		Submitter: "Lorem",
	})
	d.MarkCommentsFetched(1000)

	// Set last seen to 150 — so only message 200 is new
	monthKey := fmt.Sprintf("last_archive_msg:%d-%s",
		time.Now().Year(), time.Now().Month())
	d.SetSyncState(monthKey, "150")

	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})
	s.checkMailArchive(context.Background())

	// Patch 1000 should be reset (new message 200 matches)
	ids := d.GetPatchesNeedingComments([]string{"new"})
	if len(ids) != 1 || ids[0] != 1000 {
		t.Errorf("got %v, want [1000]", ids)
	}

	lastSeen := d.GetSyncState(monthKey)
	if lastSeen != "200" {
		t.Errorf("last_archive_msg = %q, want 200",
			lastSeen)
	}
}

func TestNeedsArchiveMonitoring(t *testing.T) {
	tests := []struct {
		version string
		archive string
		want    bool
	}{
		{"1.2", "https://mail.example.org/", true},
		{"1.3", "https://mail.example.org/", false},
		{"1.2", "", false},
		{"1.3", "", false},
	}
	for _, tt := range tests {
		cfg := &config.Config{
			APIVersion:  tt.version,
			MailArchive: tt.archive,
		}
		s := &Syncer{cfg: cfg}
		got := s.needsArchiveMonitoring()
		if got != tt.want {
			t.Errorf("v=%s archive=%q: got %v, want %v",
				tt.version, tt.archive, got, tt.want)
		}
	}
}

func TestIsValidMbox(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"mbox envelope",
			"From patchwork Sun Nov 30 15:49:21 2025\n" +
				"Content-Type: text/plain\n", true},
		{"email headers",
			"Subject: [PATCH] Lorem ipsum\n\nbody\n", true},
		{"content type header",
			"Content-Type: text/plain; charset=utf-8\n" +
				"Subject: test\n", true},
		{"message id header",
			"Message-ID: <lorem@ipsum.example>\n" +
				"Subject: test\n", true},
		{"received header",
			"Received: from smtp.example.com\n" +
				"Subject: test\n", true},
		{"return path header",
			"Return-Path: <lorem@ipsum.example>\n", true},
		{"dkim header",
			"DKIM-Signature: v=1; a=rsa-sha256\n", true},
		{"bot challenge page",
			`<!doctype html><html><head>` +
				`<title>Making sure you're not a bot!</title>` +
				`</head></html>`, false},
		{"html page",
			`<html><body>Oh noes!</body></html>`, false},
		{"301 redirect",
			`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN">` +
				`<html><body>301 Moved</body></html>`, false},
		{"empty string", "", false},
		{"random garbage", "asdfghjkl", false},
	}
	for _, tt := range tests {
		got := isValidMbox(tt.content)
		if got != tt.want {
			t.Errorf("%s: isValidMbox = %v, want %v",
				tt.name, got, tt.want)
		}
	}
}

func TestFetchMbox_DoesNotCacheBotChallenge(t *testing.T) {
	challengeHTML := `<!doctype html><html><head>` +
		`<title>Making sure you're not a bot!</title>` +
		`</head><body>challenge</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(challengeHTML))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SavePatch(db.PatchRow{
		ID: 100, Name: "Lorem patch",
		Date: "2026-03-10", State: "new",
		MboxURL:   srv.URL + "/patch/100/mbox/",
		Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})
	result := s.fetchMbox(context.Background(), 100, false)

	if result.Err == nil {
		t.Error("expected error for bot challenge response")
	}

	row, _ := d.GetPatch(100)
	if row.MboxContent != "" {
		t.Errorf("mbox_content = %q, want empty (should not cache)",
			row.MboxContent[:min(40, len(row.MboxContent))])
	}
}

func TestUserRequestLoop_HandlesMbox(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SavePatch(db.PatchRow{
		ID: 100, Name: "Lorem patch",
		Date: "2026-03-10", State: "new",
		MboxURL:   "http://example.com/mbox/",
		Submitter: "Lorem",
	})
	d.UpdatePatchMbox(100, "cached mbox content")

	cfg := &config.Config{
		Server:  "https://example.com",
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		"https://example.com", "test", nil,
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func() {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg gosync.WaitGroup
	wg.Add(1)
	go s.runUserRequests(ctx, &wg)

	resultC := make(chan MboxResult, 1)
	s.mboxC <- MboxRequest{
		PatchID: 100,
		ResultC: resultC,
	}

	result := <-resultC
	if result.Content != "cached mbox content" {
		t.Errorf("content = %q", result.Content)
	}

	cancel()
	wg.Wait()
}
