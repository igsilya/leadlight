// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"leadlight/api"
	"leadlight/config"
	"leadlight/db"
	"leadlight/status"
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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	// Pin the clock so date-bounded logic (comment backfill window) is
	// deterministic and independent of the real wall-clock date.
	s.now = func() time.Time { return testNow }
	return s, d
}

// testNow is the fixed "current time" used by tests, chosen to sit just
// after the 2026-03 fixtures so the comment backfill window includes them.
var testNow = time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

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
	content := "Acked-by: Lorem <lorem@ipsum.example>\n" +
		"Acked-by: Lorem <lorem@ipsum.example>\n" +
		"Acked-by: Dolor <dolor@amet.example>\n"
	tags := extractReviewTags(content)
	if len(tags["acked"]) != 2 {
		t.Errorf("acked = %d, want 2 (deduped)",
			len(tags["acked"]))
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

func TestIncrementalSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events/" {
				page := r.URL.Query().Get("page")
				if page == "2" {
					w.WriteHeader(404)
					w.Write([]byte(`{"detail":"Invalid page."}`))
					return
				}
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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
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

func TestFetchEvents_ProcessesDescending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			page := r.URL.Query().Get("page")
			if page == "2" {
				w.WriteHeader(404)
				w.Write([]byte(`{"detail":"Invalid page."}`))
				return
			}
			// Page 1: events in descending order (newest first).
			w.Write([]byte(`[
				{"id":2, "category":"patch-state-changed",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-11T02:00:00", "actor":null,
				 "payload":{"patch":{"id":101,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"","name":"","mbox":""},
				   "previous_state":"new","current_state":"accepted"}},
				{"id":1, "category":"patch-state-changed",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-11T01:00:00", "actor":null,
				 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"","name":"","mbox":""},
				   "previous_state":"new","current_state":"under-review"}}
			]`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 101, "p2", "2026-03-10", "new")
	d.SetSyncState("last_event_date", "2026-03-10")

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test-project",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test-project", srv.Client(),
		10*time.Millisecond)

	notified := false
	s := NewSyncer(client, d, cfg, func(...int) {
		notified = true
	}, status.NewRegistry(nil))

	s.fetchEvents(context.Background(), status.BgSync)

	if !notified {
		t.Error("should have notified")
	}

	// Verify both events were processed in correct order
	r1, _ := d.GetPatch(100)
	if r1.State != "under-review" {
		t.Errorf("patch 100 state = %q", r1.State)
	}
	r2, _ := d.GetPatch(101)
	if r2.State != "accepted" {
		t.Errorf("patch 101 state = %q", r2.State)
	}
}

func TestFetchEvents_SkipsAlreadyProcessed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			// Return 3 events in descending order (newest first).
			page := r.URL.Query().Get("page")
			if page == "2" {
				w.WriteHeader(404)
				w.Write([]byte(`{"detail":"Invalid page."}`))
				return
			}
			w.Write([]byte(`[
				{"id":12, "category":"patch-state-changed",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-11T03:00:00", "actor":null,
				 "payload":{"patch":{"id":102,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"","name":"","mbox":""},
				   "previous_state":"new","current_state":"rejected"}},
				{"id":11, "category":"patch-state-changed",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-11T02:00:00", "actor":null,
				 "payload":{"patch":{"id":101,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"","name":"","mbox":""},
				   "previous_state":"new","current_state":"accepted"}},
				{"id":10, "category":"patch-state-changed",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-11T01:00:00", "actor":null,
				 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"","name":"","mbox":""},
				   "previous_state":"new","current_state":"under-review"}}
			]`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 101, "p2", "2026-03-10", "new")
	savePatch(d, 102, "p3", "2026-03-10", "new")
	d.SetSyncState("last_event_date", "2026-03-10")
	// Mark event 11 as already processed — events 10 and 11 should be skipped
	d.SetSyncState("last_event_id", "11")

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test-project",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test-project", srv.Client(), 10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {}, status.NewRegistry(nil))

	s.fetchEvents(context.Background(), status.BgSync)

	// Event 10 (patch 100) should NOT have been processed — state stays "new"
	r1, _ := d.GetPatch(100)
	if r1.State != "new" {
		t.Errorf("patch 100 state = %q, want %q (should be skipped)", r1.State, "new")
	}
	// Event 11 (patch 101) should NOT have been processed — state stays "new"
	r2, _ := d.GetPatch(101)
	if r2.State != "new" {
		t.Errorf("patch 101 state = %q, want %q (should be skipped)", r2.State, "new")
	}
	// Event 12 (patch 102) SHOULD have been processed
	r3, _ := d.GetPatch(102)
	if r3.State != "rejected" {
		t.Errorf("patch 102 state = %q, want %q", r3.State, "rejected")
	}
	// last_event_id should be updated to 12
	if got := d.GetSyncState("last_event_id"); got != "12" {
		t.Errorf("last_event_id = %q, want %q", got, "12")
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
	s := NewSyncer(client, d, cfg, func(...int) {
		notifyCount++
	}, status.NewRegistry(nil))

	s.fetchAllActivePatches(context.Background())

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

	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(db.PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	// With id DESC ordering, patch 101 (higher ID) is fetched first
	s.fetchNextComments(context.Background())
	// Then patch 100
	s.fetchNextComments(context.Background())

	tags := d.GetTagsForSeries(50)
	hasAcked := false
	for _, tag := range tags {
		if tag.PatchID == 100 && tag.Type == "acked" {
			hasAcked = true
		}
	}
	if !hasAcked {
		t.Error("patch 100 should have acked tag")
	}
	needs := d.GetPatchesNeedingComments(100)
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
	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 0 {
		t.Fatal("should be fetched before event")
	}

	ev := api.Event{
		Category: "patch-comment-created",
		Payload: &api.PatchCommentCreatedPayload{
			Patch: api.PatchSummary{ID: 100},
			Comment: api.CommentSummary{
				ID: 301,
			},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	// Should be reset — needs re-fetch
	ids = d.GetPatchesNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 100 {
		t.Errorf("got %v, want [100] (reset by event)",
			ids)
	}
}

func TestProcessEvent_PatchCommentCreated_SkipsWhenCommentKnown(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")
	d.MarkCommentsFetched(100)
	// We already have comment 301.
	d.InsertComment(db.CommentRow{ID: 301, PatchID: 100})

	ev := api.Event{
		Category: "patch-comment-created",
		Payload: &api.PatchCommentCreatedPayload{
			Patch:   api.PatchSummary{ID: 100},
			Comment: api.CommentSummary{ID: 301},
		},
	}
	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}
	// Comment already present -> no reset, stays fetched.
	if d.NeedsPatchComments(100) {
		t.Error("patch should remain fetched (comment already known)")
	}
}

func TestProcessEvent_PatchCommentCreated_ResetsForNewComment(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	savePatch(d, 100, "test", "2026-03-10", "new")
	d.MarkCommentsFetched(100)
	// We have an older comment, but not the new one (302).
	d.InsertComment(db.CommentRow{ID: 301, PatchID: 100})

	ev := api.Event{
		Category: "patch-comment-created",
		Payload: &api.PatchCommentCreatedPayload{
			Patch:   api.PatchSummary{ID: 100},
			Comment: api.CommentSummary{ID: 302},
		},
	}
	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}
	// New comment -> reset for re-fetch even though row was fetched.
	if !d.NeedsPatchComments(100) {
		t.Error("patch should be reset for a genuinely new comment")
	}
}

func TestProcessEvent_CoverCommentCreated(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[PATCH 0/3] Lorem cover",
		Date: "2026-03-10T12:00:00",
	})
	d.MarkCoverCommentsFetched(99)

	ids := d.GetCoversNeedingComments(100)
	if len(ids) != 0 {
		t.Fatal("should be fetched before event")
	}

	ev := api.Event{
		Category: "cover-comment-created",
		Payload: &api.CoverCommentCreatedPayload{
			Cover:   api.CoverSummary{ID: 99},
			Comment: api.CommentSummary{ID: 400},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	ids = d.GetCoversNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 99 {
		t.Errorf("got %v, want [99] (reset by event)", ids)
	}
}

func TestProcessEvent_StateChangeCreatesMissingPatch(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())

	// Patch 500 was never seen (its patch-created event was skipped).
	if _, err := d.GetPatch(500); err == nil {
		t.Fatal("patch 500 should not exist yet")
	}

	ev := api.Event{
		ID:       9,
		Category: "patch-state-changed",
		Date:     "2026-03-11T00:00:00",
		Payload: &api.PatchStateChangedPayload{
			Patch: api.PatchSummary{
				ID:   500,
				Name: "[PATCH] recovered",
				Date: "2026-03-10T00:00:00",
			},
			PreviousState: "new",
			CurrentState:  "accepted",
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	row, err := d.GetPatch(500)
	if err != nil {
		t.Fatalf("patch 500 should have been created: %v", err)
	}
	if row.State != "accepted" {
		t.Errorf("state = %q, want accepted", row.State)
	}
	if row.Name != "[PATCH] recovered" {
		t.Errorf("name = %q, want recovered", row.Name)
	}
}

func TestProcessEvent_PatchCommentCreatesMissingPatch(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())

	ev := api.Event{
		Category: "patch-comment-created",
		Payload: &api.PatchCommentCreatedPayload{
			Patch: api.PatchSummary{
				ID:   600,
				Name: "[PATCH] with comment",
				Date: "2026-03-10T00:00:00",
			},
			Comment: api.CommentSummary{ID: 700},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	if _, err := d.GetPatch(600); err != nil {
		t.Fatalf("patch 600 should have been created: %v", err)
	}
	// Newly created row defaults comments_fetched=0, so it needs a fetch.
	ids := d.GetPatchesNeedingComments(10)
	found := false
	for _, r := range ids {
		if r.ID == 600 {
			found = true
		}
	}
	if !found {
		t.Error("patch 600 should be queued for comment fetch")
	}
}

func TestProcessEvent_CoverCommentCreatesMissingCover(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())

	ev := api.Event{
		Category: "cover-comment-created",
		Payload: &api.CoverCommentCreatedPayload{
			Cover: api.CoverSummary{
				ID:   800,
				Name: "[PATCH 0/2] cover",
				Date: "2026-03-10T00:00:00",
			},
			Comment: api.CommentSummary{ID: 900},
		},
	}

	if err := s.processEvent(ev, 0); err != nil {
		t.Fatal(err)
	}

	if !d.CoverExists(800) {
		t.Fatal("cover 800 should have been created")
	}
	ids := d.GetCoversNeedingComments(10)
	found := false
	for _, r := range ids {
		if r.ID == 800 {
			found = true
		}
	}
	if !found {
		t.Error("cover 800 should be queued for comment fetch")
	}
}

func TestProcessEvent_PatchRelationCreatesMissingPatch(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	previous, current := "", "related"
	ev := api.Event{
		Category: "patch-relation-changed",
		Payload: &api.PatchRelationChangedPayload{
			Patch: api.PatchSummary{
				ID:   100,
				Name: "[PATCH] relation update",
				Date: "2026-03-10T00:00:00",
			},
			PreviousRelation: &previous,
			CurrentRelation:  &current,
		},
	}

	if err := s.processEvent(ev, 50); err != nil {
		t.Fatal(err)
	}
	row, err := d.GetPatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if row.SeriesID != 50 || row.Name != "[PATCH] relation update" {
		t.Errorf("patch = %+v", row)
	}
	if got := s.seriesIDForEventPatch(ev); got != 50 {
		t.Errorf("series ID = %d, want 50", got)
	}
}

func TestCheckEventCategories_MarksAndAdvancesCursor(t *testing.T) {
	queried := map[string]bool{}
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			cat := r.URL.Query().Get("category")
			queried[cat] = true
			page := r.URL.Query().Get("page")
			if page == "2" {
				w.WriteHeader(404)
				w.Write([]byte(`{"detail":"Invalid page."}`))
				return
			}
			if cat == "patch-comment-created" {
				// Comment 1002 is new, 1001 already stored.
				w.Write([]byte(`[
					{"id":50, "category":"patch-comment-created",
					 "project":` + testProjectJSON + `,
					 "date":"2026-03-18T03:00:00", "actor":null,
					 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
					   "list_archive_url":null,"date":"2026-03-18","name":"p","mbox":""},
					   "comment":{"id":1002,"url":"","web_url":"","msgid":"",
					   "list_archive_url":null,"date":"2026-03-18"}}},
					{"id":49, "category":"patch-comment-created",
					 "project":` + testProjectJSON + `,
					 "date":"2026-03-18T02:00:00", "actor":null,
					 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
					   "list_archive_url":null,"date":"2026-03-18","name":"p","mbox":""},
					   "comment":{"id":1001,"url":"","web_url":"","msgid":"",
					   "list_archive_url":null,"date":"2026-03-18"}}}
				]`))
				return
			}
			w.Write([]byte(`[]`))
		})
	s, d := setupSyncer(t, handler)
	s.cfg.APIVersion = "1.3" // comment events require API >= 1.3

	savePatch(d, 100, "p", "2026-03-10", "new")
	d.MarkCommentsFetched(100)
	// We already have comment 1001.
	d.InsertComment(db.CommentRow{ID: 1001, PatchID: 100})

	s.checkEventCategories(context.Background())

	// All critical categories are walked unconditionally.
	for _, cat := range []string{
		"patch-created", "patch-completed", "cover-created", "series-created",
		"patch-comment-created", "cover-comment-created",
	} {
		if !queried[cat] {
			t.Errorf("category %q should be queried", cat)
		}
	}
	// The new comment (1002) should have reset patch 100 for refetch.
	ids := d.GetPatchesNeedingComments(10)
	found := false
	for _, r := range ids {
		if r.ID == 100 {
			found = true
		}
	}
	if !found {
		t.Error("patch 100 should be queued for comment refetch")
	}
}

func TestCheckEventCategories_CreatesMissingPatch(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("category") != "patch-created" {
			w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"Invalid page."}`))
			return
		}
		w.Write([]byte(`[
			{"id":70, "category":"patch-created",
			 "project":` + testProjectJSON + `,
			 "date":"2026-03-18T00:00:00", "actor":null,
			 "payload":{"patch":{"id":500,"url":"","web_url":"","msgid":"",
			   "list_archive_url":null,"date":"2026-03-18","name":"new patch","mbox":""}}}
		]`))
	})
	s, d := setupSyncer(t, handler)

	s.checkEventCategories(context.Background())

	row, err := d.GetPatch(500)
	if err != nil || row == nil {
		t.Fatalf("patch 500 should have been created: %v", err)
	}
	if got := d.GetSyncState("last_patch_created_event_id"); got != "70" {
		t.Errorf("patch-created cursor = %q, want 70", got)
	}
}

// A patch-completed event links a patch to its series, so the series becomes
// visible in the TUI list without waiting for a detail fetch.
func TestCheckEventCategories_CompletedLinksSeriesForListing(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("category") != "patch-completed" {
			w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"Invalid page."}`))
			return
		}
		w.Write([]byte(`[
			{"id":80, "category":"patch-completed",
			 "project":` + testProjectJSON + `,
			 "date":"2026-03-18T00:00:00", "actor":null,
			 "payload":{
			   "patch":{"id":600,"url":"","web_url":"","msgid":"",
			     "list_archive_url":null,"date":"2026-03-18","name":"p","mbox":""},
			   "series":{"id":700,"url":"","web_url":"","name":"my series",
			     "date":"2026-03-18","version":1,"mbox":""}}}
		]`))
	})
	s, d := setupSyncer(t, handler)

	s.checkEventCategories(context.Background())

	// Patch stored linked to its series.
	row, err := d.GetPatch(600)
	if err != nil || row == nil {
		t.Fatalf("patch 600 should exist: %v", err)
	}
	if row.SeriesID != 700 {
		t.Errorf("patch 600 series = %d, want 700", row.SeriesID)
	}
	// Series is listable in the default active view (patch is 'new'),
	// with no detail fetch performed.
	active := d.GetActiveSeries([]string{"new"})
	found := false
	for _, sr := range active {
		if sr.ID == 700 {
			found = true
		}
	}
	if !found {
		t.Error("series 700 should be listed via its completed patch")
	}
	if got := d.GetSyncState("last_patch_completed_event_id"); got != "80" {
		t.Errorf("patch-completed cursor = %q, want 80", got)
	}
}

func TestProcessEvent_PropagatesEnsureError(t *testing.T) {
	s, d := setupSyncer(t, http.NotFoundHandler())
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	err := s.processEvent(api.Event{
		Category: "patch-state-changed",
		Payload: &api.PatchStateChangedPayload{
			Patch:        api.PatchSummary{ID: 100},
			CurrentState: "accepted",
		},
	}, 0)
	if err == nil {
		t.Fatal("expected database error")
	}
}

func TestCheckEventCategories_KnownCommentDoesNotHideOlderEvent(t *testing.T) {
	var patchPages []string
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			if r.URL.Query().Get("category") != "patch-comment-created" {
				w.Write([]byte(`[]`))
				return
			}
			page := r.URL.Query().Get("page")
			patchPages = append(patchPages, page)
			switch page {
			case "1":
				w.Write([]byte(`[
				{"id":50, "category":"patch-comment-created",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-18T00:00:00", "actor":null,
				 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-03-18","name":"p","mbox":""},
				   "comment":{"id":1001,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-03-18"}}}
			]`))
			case "2":
				w.Write([]byte(`[
				{"id":49, "category":"patch-comment-created",
				 "project":` + testProjectJSON + `,
				 "date":"2026-03-17T00:00:00", "actor":null,
				 "payload":{"patch":{"id":101,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-03-17","name":"older","mbox":""},
				   "comment":{"id":1002,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-03-17"}}}
			]`))
			default:
				w.WriteHeader(404)
				w.Write([]byte(`{"detail":"Invalid page."}`))
			}
		})
	s, d := setupSyncer(t, handler)
	s.cfg.APIVersion = "1.3" // comment events require API >= 1.3

	savePatch(d, 100, "p", "2026-03-10", "new")
	savePatch(d, 101, "older", "2026-03-10", "new")
	d.MarkCommentsFetched(100)
	d.MarkCommentsFetched(101)
	d.InsertComment(db.CommentRow{ID: 1001, PatchID: 100})

	s.checkEventCategories(context.Background())

	if !d.NeedsPatchComments(101) {
		t.Error("older unknown comment should queue patch 101")
	}
	if got := d.GetSyncState("last_patch_comment_event_id"); got != "50" {
		t.Errorf("comment cursor = %q, want 50", got)
	}
	patchPages = nil
	s.checkEventCategories(context.Background())
	if len(patchPages) != 1 || patchPages[0] != "1" {
		t.Errorf("second walk pages = %v, want page 1 only", patchPages)
	}
}

func TestWalkEvents_StopsProcessingOnHandlerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"Invalid page."}`))
			return
		}
		w.Write([]byte(`[
			{"id":2,"category":"unknown","project":` + testProjectJSON + `,
			 "date":"2026-03-11T02:00:00","actor":null,"payload":{}},
			{"id":1,"category":"unknown","project":` + testProjectJSON + `,
			 "date":"2026-03-11T01:00:00","actor":null,"payload":{}}
		]`))
	})
	s, _ := setupSyncer(t, handler)
	wantErr := errors.New("write failed")
	var handled []int

	got := s.walkEvents(context.Background(), status.BgSync,
		api.EventListParams{}, 0, eventWalkTarget{},
		func(api.Event) bool { return false },
		func(ev api.Event, _ int) error {
			handled = append(handled, ev.ID)
			return wantErr
		})

	if got != 0 {
		t.Errorf("processed = %d, want 0", got)
	}
	if len(handled) != 1 || handled[0] != 1 {
		t.Errorf("handled = %v, want [1]", handled)
	}
}

func TestWalkEvents_EstimatesRemainingPages(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(`[
				{"id":100,"category":"unknown","project":` + testProjectJSON + `,
				 "date":"2026-03-11T02:00:00","actor":null,"payload":{}},
				{"id":90,"category":"unknown","project":` + testProjectJSON + `,
				 "date":"2026-03-11T01:00:00","actor":null,"payload":{}}
			]`))
		case "2":
			w.Write([]byte(`[
				{"id":80,"category":"unknown","project":` + testProjectJSON + `,
				 "date":"2026-03-10T02:00:00","actor":null,"payload":{}},
				{"id":70,"category":"unknown","project":` + testProjectJSON + `,
				 "date":"2026-03-10T01:00:00","actor":null,"payload":{}}
			]`))
		default:
			w.Write([]byte(`[
				{"id":10,"category":"unknown","project":` + testProjectJSON + `,
				 "date":"2026-03-09T01:00:00","actor":null,"payload":{}}
			]`))
		}
	})
	s, _ := setupSyncer(t, handler)

	s.walkEvents(context.Background(), status.BgSync,
		api.EventListParams{}, 0, eventWalkTarget{cursorID: 10},
		func(ev api.Event) bool { return ev.ID <= 10 },
		func(api.Event, int) error { return nil })

	msg, _ := s.status.Active()
	if !strings.Contains(msg, "page 3, ~4 remaining") {
		t.Errorf("status = %q", msg)
	}
}

func TestWalkEvents_EstimatesRemainingPagesByDate(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(`[
				{"id":100,"category":"patch-comment-created","project":` + testProjectJSON + `,
				 "date":"2026-03-11T00:00:00","actor":null,"payload":{}},
				{"id":90,"category":"patch-comment-created","project":` + testProjectJSON + `,
				 "date":"2026-03-10T00:00:00","actor":null,"payload":{}}
			]`))
		case "2":
			w.Write([]byte(`[
				{"id":80,"category":"patch-comment-created","project":` + testProjectJSON + `,
				 "date":"2026-03-09T00:00:00","actor":null,"payload":{}},
				{"id":70,"category":"patch-comment-created","project":` + testProjectJSON + `,
				 "date":"2026-03-08T00:00:00","actor":null,"payload":{}}
			]`))
		default:
			w.Write([]byte(`[
				{"id":10,"category":"patch-comment-created","project":` + testProjectJSON + `,
				 "date":"2026-03-01T00:00:00","actor":null,"payload":{}}
			]`))
		}
	})
	s, _ := setupSyncer(t, handler)
	cutoff := parseEventDate("2026-03-04T00:00:00")

	s.walkEvents(context.Background(), status.BgSync,
		api.EventListParams{}, 0, eventWalkTarget{cursorDate: cutoff},
		func(ev api.Event) bool {
			return parseEventDate(ev.Date).Before(cutoff)
		},
		func(api.Event, int) error { return nil })

	msg, _ := s.status.Active()
	if !strings.Contains(msg, "remaining") {
		t.Errorf("status = %q, want a remaining estimate", msg)
	}
}

func TestCheckEventCategories_WalksEmptyDB(t *testing.T) {
	// Creation walks must run even on an empty DB: that is how newly
	// created patches/covers are discovered when the unfiltered stream
	// omits *-created events.
	queried := map[string]bool{}
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events/" {
				queried[r.URL.Query().Get("category")] = true
			}
			w.Write([]byte(`[]`))
		})
	s, _ := setupSyncer(t, handler)

	s.checkEventCategories(context.Background())

	if !queried["patch-created"] || !queried["cover-created"] {
		t.Errorf("creation categories should be walked on empty DB: %v", queried)
	}
}

func TestCheckEventCategories_DateBackstop(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events/" {
				w.WriteHeader(404)
				return
			}
			if r.URL.Query().Get("category") != "patch-comment-created" {
				w.Write([]byte(`[]`))
				return
			}
			page := r.URL.Query().Get("page")
			if page == "2" {
				// If the walk ever asks for page 2, the backstop failed.
				t.Error("walk should have stopped on page 1 via date backstop")
				w.WriteHeader(404)
				w.Write([]byte(`{"detail":"Invalid page."}`))
				return
			}
			// Comment event well before the backfill window (testNow-14d),
			// so it is not processed and the walk stops on page 1.
			w.Write([]byte(`[
				{"id":40, "category":"patch-comment-created",
				 "project":` + testProjectJSON + `,
				 "date":"2026-01-01T00:00:00", "actor":null,
				 "payload":{"patch":{"id":100,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-01-01","name":"p","mbox":""},
				   "comment":{"id":2001,"url":"","web_url":"","msgid":"",
				   "list_archive_url":null,"date":"2026-01-01"}}}
			]`))
		})
	s, d := setupSyncer(t, handler)
	s.cfg.APIVersion = "1.3" // comment events require API >= 1.3

	savePatch(d, 100, "p", "2026-03-10", "new")
	d.MarkCommentsFetched(100)

	s.checkEventCategories(context.Background())

	// Comment 2001 is older than the backfill window -> not processed,
	// so patch 100 stays fetched.
	if d.NeedsPatchComments(100) {
		t.Error("old comment event should not have reset patch 100")
	}
}

// When API < 1.3 and a mail archive is configured, comments come from the
// archive, so the comment category walks must be skipped; only the creation
// categories are queried.
func TestCheckEventCategories_SkipsCommentWalksWithArchive(t *testing.T) {
	queried := map[string]bool{}
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events/" {
				queried[r.URL.Query().Get("category")] = true
			}
			w.Write([]byte(`[]`))
		})
	s, _ := setupSyncer(t, handler) // default APIVersion 1.2
	s.cfg.MailArchive = "https://mail.example.org/"

	s.checkEventCategories(context.Background())

	for _, cat := range []string{
		"patch-created", "patch-completed", "cover-created", "series-created",
	} {
		if !queried[cat] {
			t.Errorf("category %q should be queried", cat)
		}
	}
	for _, cat := range []string{
		"patch-comment-created", "cover-comment-created",
	} {
		if queried[cat] {
			t.Errorf("category %q should be skipped when archive polling", cat)
		}
	}
}

func TestFetchNextCoverComments(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/comments/" {
				json.NewEncoder(w).Encode([]api.Comment{
					{
						ID:      400,
						Subject: "Re: Lorem cover",
						Date:    "2026-03-11T09:00:00",
						MsgID:   "<reply-cover@ipsum.example>",
						Submitter: api.Person{
							Name:  "Dolor Amet",
							Email: "dolor@amet.example",
						},
						Content: "Looks good.\n\nAcked-by: Dolor Amet <dolor@amet.example>",
					},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[PATCH 0/3] Lorem cover",
		Date: "2026-03-10T12:00:00",
	})

	ids := d.GetCoversNeedingComments(100)
	if len(ids) != 1 {
		t.Fatalf("before: len = %d", len(ids))
	}

	s.fetchNextCoverComments(context.Background())

	ids = d.GetCoversNeedingComments(100)
	if len(ids) != 0 {
		t.Errorf("after: ids = %v, want empty", ids)
	}

	comments := d.GetCommentsForCover(99)
	if len(comments) != 1 {
		t.Fatalf("comments len = %d", len(comments))
	}
	if comments[0].ID != 400 {
		t.Errorf("comment ID = %d", comments[0].ID)
	}
	if comments[0].CoverID != 99 {
		t.Errorf("CoverID = %d", comments[0].CoverID)
	}
	if comments[0].Submitter != "Dolor Amet" {
		t.Errorf("Submitter = %q", comments[0].Submitter)
	}
}

func TestFetchCommentsForPatch(t *testing.T) {
	var apiCalled bool
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/100/comments/" {
				apiCalled = true
				json.NewEncoder(w).Encode([]api.Comment{
					{
						ID:      500,
						Subject: "Re: Lorem",
						Date:    "2026-03-11T09:00:00",
						Submitter: api.Person{
							Name: "Dolor Amet",
						},
						Content: "Acked-by: Dolor Amet <dolor@amet.example>",
					},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "test", "2026-03-10", "new")

	s.fetchCommentsForPatch(context.Background(), 100, 50, status.BgComments)
	if !apiCalled {
		t.Error("API should be called when comments_fetched = 0")
	}

	comments := d.GetComments(100)
	if len(comments) != 1 || comments[0].ID != 500 {
		t.Errorf("comments = %v", comments)
	}

	// Second call should not hit API (already fetched)
	apiCalled = false
	s.fetchCommentsForPatch(context.Background(), 100, 50, status.BgComments)
	if apiCalled {
		t.Error("API should NOT be called when comments_fetched = 1")
	}
}

func TestFetchCommentsForCover(t *testing.T) {
	var apiCalled bool
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/comments/" {
				apiCalled = true
				json.NewEncoder(w).Encode([]api.Comment{
					{
						ID:      600,
						Subject: "Re: Lorem cover",
						Date:    "2026-03-11T10:00:00",
						Submitter: api.Person{
							Name: "Sit Amet",
						},
						Content: "Looks good overall.",
					},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[PATCH 0/3] Lorem cover",
		Date: "2026-03-10T12:00:00",
	})

	s.fetchCommentsForCover(context.Background(), 99, 50, status.BgCoverComments)
	if !apiCalled {
		t.Error("API should be called when comments_fetched = 0")
	}

	comments := d.GetCommentsForCover(99)
	if len(comments) != 1 || comments[0].ID != 600 {
		t.Errorf("comments = %v", comments)
	}

	// Second call should not hit API
	apiCalled = false
	s.fetchCommentsForCover(context.Background(), 99, 50, status.BgCoverComments)
	if apiCalled {
		t.Error("API should NOT be called when comments_fetched = 1")
	}
}

func TestFetchNextComments_SkipsFailedPatch(t *testing.T) {
	var called []string
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			called = append(called, r.URL.Path)
			if r.URL.Path == "/patches/200/comments/" {
				w.WriteHeader(500)
				return
			}
			if r.URL.Path == "/patches/100/comments/" {
				json.NewEncoder(w).Encode([]api.Comment{})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 200, "p2", "2026-03-11", "new")

	// With id DESC, patch 200 is tried first (fails)
	s.fetchNextComments(context.Background())
	if len(called) != 1 || called[0] != "/patches/200/comments/" {
		t.Fatalf("first call: %v", called)
	}

	// Second call should skip 200 (cooldown) and try 100
	called = nil
	s.fetchNextComments(context.Background())
	if len(called) != 1 || called[0] != "/patches/100/comments/" {
		t.Errorf("second call should skip 200: %v", called)
	}
}

func TestFetchNextComments_CooldownExpires(t *testing.T) {
	var called []string
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			called = append(called, r.URL.Path)
			if r.URL.Path == "/patches/100/comments/" {
				w.WriteHeader(500)
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "p1", "2026-03-10", "new")

	// First call fails, adds to skip set
	s.fetchNextComments(context.Background())
	if len(called) != 1 {
		t.Fatalf("first call: %v", called)
	}

	// Second call within cooldown — should skip
	called = nil
	s.fetchNextComments(context.Background())
	if len(called) != 0 {
		t.Errorf("should skip during cooldown: %v", called)
	}

	// Simulate cooldown expiry by backdating the skip entry
	s.commentSkip[100] = time.Now().Add(-31 * time.Minute)

	// Third call after cooldown — should retry
	called = nil
	s.fetchNextComments(context.Background())
	if len(called) != 1 || called[0] != "/patches/100/comments/" {
		t.Errorf("should retry after cooldown: %v", called)
	}
}

func TestFetchNextPatchDetail(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/100/" {
				json.NewEncoder(w).Encode(api.PatchDetail{
					Patch: api.Patch{
						ID:   100,
						Name: "Lorem ipsum",
						Date: "2026-03-10",
					},
					Content:  "Commit message body\n\nSigned-off-by: Lorem",
					Diff:     "diff --git a/f b/f\n",
					Headers:  map[string]interface{}{},
					Prefixes: []string{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "Lorem ipsum", "2026-03-10", "new")

	ids := d.GetPatchesNeedingDetail(100)
	if len(ids) != 1 {
		t.Fatalf("before: %v", ids)
	}

	s.fetchNextPatchDetail(context.Background())

	ids = d.GetPatchesNeedingDetail(100)
	if len(ids) != 0 {
		t.Errorf("after: %v, want empty", ids)
	}
}

func TestFetchNextPatchDetail_StatusShowsRemainingCount(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(api.PatchDetail{
				Patch: api.Patch{
					ID: 100, Name: "p1", Date: "2026-03-10",
					State: "new", Submitter: api.Person{Name: "Lorem"},
				},
				Content: "body", Diff: "diff",
				Headers: map[string]interface{}{}, Prefixes: []string{},
			})
		})

	s, d := setupSyncer(t, handler)
	// 3 patches needing detail
	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 101, "p2", "2026-03-10", "new")
	savePatch(d, 102, "p3", "2026-03-10", "new")

	s.fetchNextPatchDetail(context.Background())

	msg, _ := s.status.Active()
	if !strings.Contains(msg, "2 remaining") {
		t.Errorf("status should say '2 remaining', got %q", msg)
	}
}

func TestFetchNextPatchDetail_ErrorNoMark(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "Lorem ipsum", "2026-03-10", "new")

	s.fetchNextPatchDetail(context.Background())

	ids := d.GetPatchesNeedingDetail(100)
	if len(ids) != 1 || ids[0].ID != 100 {
		t.Errorf("should stay unfetched: %v", ids)
	}
}

func TestFetchNextPatchDetail_SkipsFailed(t *testing.T) {
	var called []string
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			called = append(called, r.URL.Path)
			if r.URL.Path == "/patches/200/" {
				w.WriteHeader(500)
				return
			}
			if r.URL.Path == "/patches/100/" {
				json.NewEncoder(w).Encode(api.PatchDetail{
					Patch: api.Patch{
						ID: 100, Name: "p1", Date: "2026-03-10",
					},
					Content: "body", Diff: "diff",
					Headers:  map[string]interface{}{},
					Prefixes: []string{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "p1", "2026-03-10", "new")
	savePatch(d, 200, "p2", "2026-03-11", "new")

	// First call hits 200 (higher ID, returned first), fails
	s.fetchNextPatchDetail(context.Background())
	if len(called) != 1 || called[0] != "/patches/200/" {
		t.Fatalf("first call: %v", called)
	}

	// Second call skips 200 (cooldown), fetches 100
	called = nil
	s.fetchNextPatchDetail(context.Background())
	if len(called) != 1 || called[0] != "/patches/100/" {
		t.Errorf("second call should skip 200: %v", called)
	}

	// Third call: 200 still on cooldown, 100 already fetched
	called = nil
	s.fetchNextPatchDetail(context.Background())
	if len(called) != 0 {
		t.Errorf("third call should do nothing: %v", called)
	}
}

func TestFetchNextCoverDetail(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/" {
				json.NewEncoder(w).Encode(api.CoverDetail{
					Cover: api.Cover{
						ID:   99,
						Name: "Lorem cover",
						Date: "2026-03-10",
					},
					Content: "Cover body text",
					Headers: map[string]interface{}{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Lorem cover", Date: "2026-03-10",
	})

	ids := d.GetCoversNeedingDetail(100)
	if len(ids) != 1 {
		t.Fatalf("before: %v", ids)
	}

	s.fetchNextCoverDetail(context.Background())

	ids = d.GetCoversNeedingDetail(100)
	if len(ids) != 0 {
		t.Errorf("after: %v, want empty", ids)
	}
}

// TestCommentSkipConcurrent exercises the two comment loops that share the
// commentSkip map from separate goroutines. Run with -race to catch the
// concurrent-map-writes regression. The server always errors so both loops
// hit the skipSet/skipActive/skipDelete paths repeatedly.
func TestCommentSkipConcurrent(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		})
	s, d := setupSyncer(t, handler)
	for i := 1; i <= 20; i++ {
		savePatch(d, i, "p", "2026-03-10", "new")
		d.SaveCover(db.CoverRow{
			ID: 1000 + i, SeriesID: i, Name: "c", Date: "2026-03-10"})
	}

	var wg gosync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.fetchNextComments(context.Background())
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.fetchNextCoverComments(context.Background())
		}
	}()
	wg.Wait()
}

func TestFetchDetailForCover_RepairsSeriesLink(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/" {
				json.NewEncoder(w).Encode(api.CoverDetail{
					Cover: api.Cover{
						ID:   99,
						Name: "Lorem cover",
						Date: "2026-03-10",
						Series: []api.SeriesSummary{
							{ID: 50, Name: "series", Date: "2026-03-10", Version: 1},
						},
					},
					Content: "Cover body",
					Headers: map[string]interface{}{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	// Bare cover backfilled from a comment event: no series link.
	d.InsertCoverIfAbsent(db.CoverRow{
		ID: 99, Name: "Lorem cover", Date: "2026-03-10",
	})
	if _, err := d.GetCover(50); err == nil {
		t.Fatal("cover should not be linked to series 50 yet")
	}

	if got := s.fetchNextCoverDetail(context.Background()); got != 50 {
		t.Fatalf("series ID = %d, want 50", got)
	}

	// Series link repaired from the cover's series list.
	row, err := d.GetCover(50)
	if err != nil {
		t.Fatalf("cover should be linked to series 50 after detail: %v", err)
	}
	if row.ID != 99 {
		t.Errorf("cover ID = %d, want 99", row.ID)
	}
}

func TestFetchNextCoverDetail_RetriesUnlinkedCoverWithoutSeries(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/covers/99/" {
			requests++
			json.NewEncoder(w).Encode(api.CoverDetail{
				Cover: api.Cover{
					ID: 99, Name: "Unlinked cover", Date: "2026-03-10",
				},
				Content: "Cover body",
				Headers: map[string]interface{}{},
			})
			return
		}
		http.NotFound(w, r)
	})
	s, d := setupSyncer(t, handler)
	d.InsertCoverIfAbsent(db.CoverRow{
		ID: 99, Name: "Unlinked cover", Date: "2026-03-10",
	})

	if got := s.fetchNextCoverDetail(context.Background()); got != 0 {
		t.Fatalf("series ID = %d, want 0", got)
	}
	if !d.NeedsCoverDetail(99) {
		t.Fatal("unlinked cover should remain queued for detail retry")
	}
	if !s.skipActive(s.detailSkip, 99) {
		t.Fatal("unlinked cover should be placed on retry cooldown")
	}

	s.fetchNextCoverDetail(context.Background())
	if requests != 1 {
		t.Errorf("detail requests = %d, want 1 during cooldown", requests)
	}
}

func TestFetchDetailForCover_PropagatesDatabaseError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/covers/99/" {
			json.NewEncoder(w).Encode(api.CoverDetail{
				Cover:   api.Cover{ID: 99, Name: "cover"},
				Content: "body",
				Headers: map[string]interface{}{},
			})
			return
		}
		http.NotFound(w, r)
	})
	s, d := setupSyncer(t, handler)
	d.SaveCover(db.CoverRow{ID: 99, SeriesID: 50, Name: "cover"})
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	err := s.fetchDetailForCover(
		context.Background(), 99, 50, status.Detail)
	if err == nil {
		t.Fatal("expected database error")
	}
}

func TestFetchNextPatchDetail_ThenCover(t *testing.T) {
	var called []string
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			called = append(called, r.URL.Path)
			if r.URL.Path == "/patches/100/" {
				json.NewEncoder(w).Encode(api.PatchDetail{
					Patch: api.Patch{
						ID: 100, Name: "p1", Date: "2026-03-10",
					},
					Content: "body", Diff: "diff",
					Headers:  map[string]interface{}{},
					Prefixes: []string{},
				})
				return
			}
			if r.URL.Path == "/covers/99/" {
				json.NewEncoder(w).Encode(api.CoverDetail{
					Cover: api.Cover{
						ID: 99, Name: "cover", Date: "2026-03-10",
					},
					Content: "cover body",
					Headers: map[string]interface{}{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	savePatch(d, 100, "p1", "2026-03-10", "new")
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})

	// Patch detail loop fetches the patch
	s.fetchNextPatchDetail(context.Background())
	if len(called) != 1 || called[0] != "/patches/100/" {
		t.Fatalf("patch detail: %v", called)
	}

	// Cover detail loop fetches the cover
	called = nil
	s.fetchNextCoverDetail(context.Background())
	if len(called) != 1 || called[0] != "/covers/99/" {
		t.Errorf("cover detail: %v", called)
	}

	// Both done — nothing left
	called = nil
	if s.fetchNextPatchDetail(context.Background()) != 0 {
		t.Error("patch detail should return 0")
	}
	if s.fetchNextCoverDetail(context.Background()) != 0 {
		t.Error("cover detail should return 0")
	}
}

func TestUpdatePatchTagsFromComments_SavesTags(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/100/comments/" {
				json.NewEncoder(w).Encode([]api.Comment{
					{
						ID:      500,
						Subject: "Re: Lorem",
						Date:    "2026-03-11T09:00:00",
						Submitter: api.Person{
							Name: "Dolor Amet",
						},
						Content: "Acked-by: Dolor Amet <dolor@amet.example>\nReviewed-by: Sit <sit@ex>",
					},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	savePatch(d, 100, "p1", "2026-03-10", "new")
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	s.fetchCommentsForPatch(context.Background(), 100, 50, status.BgComments)

	tags := d.GetTagsForSeries(50)
	if len(tags) != 2 {
		t.Fatalf("tags len=%d, want 2", len(tags))
	}
	hasAcked, hasReviewed := false, false
	for _, tag := range tags {
		if tag.Type == "acked" && tag.Source == "comment" {
			hasAcked = true
		}
		if tag.Type == "reviewed" && tag.Source == "comment" {
			hasReviewed = true
		}
	}
	if !hasAcked {
		t.Error("missing acked tag")
	}
	if !hasReviewed {
		t.Error("missing reviewed tag")
	}
}

func TestUpdateCoverTagsFromComments(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/comments/" {
				json.NewEncoder(w).Encode([]api.Comment{
					{
						ID:      600,
						Subject: "Re: Cover",
						Date:    "2026-03-11T10:00:00",
						Submitter: api.Person{
							Name: "Dolor Amet",
						},
						Content: "Acked-by: Dolor Amet <dolor@amet.example>",
					},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})

	s.fetchCommentsForCover(context.Background(), 99, 50, status.BgCoverComments)

	tags := d.GetTagsForSeries(50)
	if len(tags) != 1 {
		t.Fatalf("tags len=%d, want 1", len(tags))
	}
	if tags[0].CoverID != 99 || tags[0].Source != "comment" || tags[0].Type != "acked" {
		t.Errorf("unexpected tag: %+v", tags[0])
	}
}

func TestFetchNextPatchDetail_ExtractsOriginalTags(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/100/" {
				json.NewEncoder(w).Encode(api.PatchDetail{
					Patch: api.Patch{
						ID: 100, Name: "p1", Date: "2026-03-10",
					},
					Content: "Lorem ipsum dolor sit amet\n\n" +
						"Acked-by: Dolor <dolor@ex>\n" +
						"Reviewed-by: Sit <sit@ex>\n",
					Diff:     "diff --git a/f b/f\n",
					Headers:  map[string]interface{}{},
					Prefixes: []string{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	s.fetchNextPatchDetail(context.Background())

	tags := d.GetTagsForSeries(50)
	if len(tags) != 2 {
		t.Fatalf("tags len=%d, want 2", len(tags))
	}
	for _, tag := range tags {
		if tag.Source != "original" {
			t.Errorf("source=%q, want original", tag.Source)
		}
		if tag.PatchID != 100 {
			t.Errorf("patch_id=%d, want 100", tag.PatchID)
		}
	}
}

func TestFetchNextCoverDetail_ExtractsOriginalTags(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/covers/99/" {
				json.NewEncoder(w).Encode(api.CoverDetail{
					Cover: api.Cover{
						ID: 99, Name: "cover", Date: "2026-03-10",
					},
					Content: "Cover body\n\n" +
						"Tested-by: Lorem <lorem@ex>\n",
					Headers: map[string]interface{}{},
				})
				return
			}
			http.NotFound(w, r)
		})

	s, d := setupSyncer(t, handler)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})

	s.fetchNextCoverDetail(context.Background())

	tags := d.GetTagsForSeries(50)
	if len(tags) != 1 {
		t.Fatalf("tags len=%d, want 1", len(tags))
	}
	if tags[0].CoverID != 99 || tags[0].Source != "original" || tags[0].Type != "tested" {
		t.Errorf("unexpected tag: %+v", tags[0])
	}
}

func TestMigrateTags(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchDetail(100,
		"Commit message\n\nAcked-by: Lorem <lorem@ex>\n",
		"diff --git a/f b/f\n", "{}", "[]")
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})
	d.UpdateCoverDetail(99,
		"Cover body\n\nReviewed-by: Dolor <dolor@ex>\n", "{}")
	d.InsertComment(db.CommentRow{
		ID: 500, PatchID: 100, Submitter: "Sit",
		Date: "2026-03-11", Subject: "Re: p1",
		Content: "Acked-by: Sit <sit@ex>",
	})

	MigrateTags(d)

	tags := d.GetTagsForSeries(50)
	origAcked, origReviewed, commentAcked := false, false, false
	for _, tag := range tags {
		if tag.PatchID == 100 && tag.Source == "original" &&
			tag.Type == "acked" {
			origAcked = true
		}
		if tag.CoverID == 99 && tag.Source == "original" &&
			tag.Type == "reviewed" {
			origReviewed = true
		}
		if tag.PatchID == 100 && tag.Source == "comment" &&
			tag.Type == "acked" {
			commentAcked = true
		}
	}
	if !origAcked {
		t.Error("missing patch original acked tag")
	}
	if !origReviewed {
		t.Error("missing cover original reviewed tag")
	}
	if !commentAcked {
		t.Error("missing patch comment acked tag")
	}
}

func TestMigrateTags_SkipsIfDone(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchDetail(100,
		"Acked-by: Lorem <lorem@ex>\n",
		"", "{}", "[]")

	d.SetSyncState("tag_schema", tagSchemaVersion)
	MigrateTags(d)

	tags := d.GetTagsForSeries(50)
	if len(tags) != 0 {
		t.Errorf("should skip: len=%d, want 0", len(tags))
	}
}

func TestFilterHeaders(t *testing.T) {
	raw := map[string]interface{}{
		"From":                    "Lorem Ipsum <lorem@ipsum.example>",
		"Date":                    "Thu, 04 Dec 2025 14:57:28 +0530",
		"To":                      "Dolor Amet <dolor@amet.example>, dev@lorem.example",
		"Cc":                      "sit@amet.example",
		"In-Reply-To":             "<20260306-lorem@ipsum.example>",
		"References":              "<20260226-dolor@ipsum.example>",
		"Content-Type":            "text/plain; charset=UTF-8",
		"Subject":                 "Re: [PATCH] Lorem ipsum",
		"Message-ID":              "<abc123@ipsum.example>",
		"DKIM-Signature":          "v=1; a=rsa-sha256; ...",
		"X-MS-Exchange-Something": "trash",
		"Return-Path":             "<bounces@lorem.example>",
		"Received": []interface{}{
			"from lorem.example (lorem.example [1.2.3.4])",
			"from dolor.example (dolor.example [5.6.7.8])",
		},
	}
	result := filterHeaders(raw)
	if !strings.Contains(result, "From: Lorem Ipsum") {
		t.Error("missing From")
	}
	if !strings.Contains(result, "Date: Thu, 04 Dec") {
		t.Error("missing Date")
	}
	if !strings.Contains(result, "To: Dolor Amet") {
		t.Error("missing To")
	}
	if !strings.Contains(result, "Cc: sit@amet") {
		t.Error("missing Cc")
	}
	if !strings.Contains(result, "In-Reply-To:") {
		t.Error("missing In-Reply-To")
	}
	if !strings.Contains(result, "References:") {
		t.Error("missing References")
	}
	if !strings.Contains(result, "Content-Type:") {
		t.Error("missing Content-Type")
	}
	if strings.Contains(result, "DKIM") {
		t.Error("should not contain DKIM")
	}
	if strings.Contains(result, "X-MS") {
		t.Error("should not contain X-MS")
	}
	if strings.Contains(result, "Return-Path") {
		t.Error("should not contain Return-Path")
	}
	if strings.Contains(result, "Received") {
		t.Error("should not contain Received")
	}
	if !strings.Contains(result, "Subject: Re:") {
		t.Error("missing Subject")
	}
	if !strings.Contains(result, "Message-ID: <abc123") {
		t.Error("missing Message-ID")
	}
}

func TestFilterHeaders_Empty(t *testing.T) {
	if filterHeaders(nil) != "" {
		t.Error("nil map should return empty")
	}
	if filterHeaders(map[string]interface{}{}) != "" {
		t.Error("empty map should return empty")
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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.checkMailArchive(context.Background())

	// Patch 1000 should be reset (subject matches)
	ids := d.GetPatchesNeedingComments(100)
	got := map[int]bool{}
	for _, ref := range ids {
		got[ref.ID] = true
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
			time.Now().UTC().Year(), time.Now().UTC().Month()))
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
		time.Now().UTC().Year(), time.Now().UTC().Month())
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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	s.checkMailArchive(context.Background())

	// Patch 1000 should be reset (new message 200 matches)
	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 1000 {
		t.Errorf("got %v, want [1000]", ids)
	}

	lastSeen := d.GetSyncState(monthKey)
	if lastSeen != "200" {
		t.Errorf("last_archive_msg = %q, want 200",
			lastSeen)
	}
}

func TestCheckMailArchive_CoverComments(t *testing.T) {
	archiveHTML := `<HTML><BODY><ul>
<LI><A HREF="300.html">Re: [dev] [PATCH 0/3] Lorem ipsum dolor
</A><A NAME="300">&nbsp;</A>
<I>Dolor</I>
</ul></BODY></HTML>`

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(archiveHTML))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 3)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[dev,0/3] Lorem ipsum dolor",
		Date: "2026-03-10",
	})
	d.MarkCoverCommentsFetched(99)

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

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	s.checkMailArchive(context.Background())

	ids := d.GetCoversNeedingComments(100)
	got := map[int]bool{}
	for _, ref := range ids {
		got[ref.ID] = true
	}
	if !got[99] {
		t.Error("cover 99 should be reset (matches archive)")
	}
}

func TestCheckMailArchive_MultiMonthCatchup(t *testing.T) {
	requestedPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)
			w.Write([]byte(`<HTML><BODY><ul></ul></BODY></HTML>`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	// Simulate last check was 3 months ago
	now := time.Now().UTC()
	threeMonthsAgo := time.Date(now.Year(), now.Month()-3, 15, 0, 0, 0, 0, time.UTC)
	d.SetSyncState("last_archive_check",
		threeMonthsAgo.Format("2006-01"))

	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/archive/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.checkMailArchive(context.Background())

	// Should have checked at least 4 months (3 ago + 2 ago + 1 ago + current)
	if len(requestedPaths) < 4 {
		t.Errorf("expected at least 4 archive requests, got %d: %v",
			len(requestedPaths), requestedPaths)
	}

	// last_archive_check should be updated to current month
	check := d.GetSyncState("last_archive_check")
	want := time.Now().UTC().Format("2006-01")
	if check != want {
		t.Errorf("last_archive_check = %q, want %q", check, want)
	}
}

func TestCheckMailArchive_FirstRun(t *testing.T) {
	requestedPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)
			w.Write([]byte(`<HTML><BODY><ul></ul></BODY></HTML>`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	// No last_archive_check set — first run
	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/archive/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.checkMailArchive(context.Background())

	// Should check only current month
	if len(requestedPaths) != 1 {
		t.Errorf("expected 1 archive request (current month), got %d",
			len(requestedPaths))
	}

	// last_archive_check should be set
	check := d.GetSyncState("last_archive_check")
	if check == "" {
		t.Error("last_archive_check should be set after first run")
	}
}

func TestCheckMailArchive_SameMonth(t *testing.T) {
	requestedPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)
			w.Write([]byte(`<HTML><BODY><ul></ul></BODY></HTML>`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	// Last check was this month — no gap
	d.SetSyncState("last_archive_check",
		time.Now().UTC().Format("2006-01"))

	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/archive/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.checkMailArchive(context.Background())

	// Should check only current month (re-check for new messages)
	if len(requestedPaths) != 1 {
		t.Errorf("expected 1 archive request, got %d",
			len(requestedPaths))
	}
}

func TestCheckMailArchive_YearBoundary(t *testing.T) {
	requestedPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)
			w.Write([]byte(`<HTML><BODY><ul></ul></BODY></HTML>`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	// Set last check to November of previous year (crosses year boundary)
	lastNov := time.Date(time.Now().UTC().Year()-1, 11, 1, 0, 0, 0, 0, time.UTC)
	d.SetSyncState("last_archive_check",
		lastNov.Format("2006-01"))

	cfg := &config.Config{
		Server:      srv.URL,
		Project:     "test",
		APIVersion:  "1.2",
		MailArchive: srv.URL + "/archive/",
		States:      []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.checkMailArchive(context.Background())

	// Nov, Dec of last year + Jan through current month of this year
	now := time.Now().UTC()
	expectedMonths := int(now.Month()) + 2 // Nov + Dec + Jan..now
	if len(requestedPaths) != expectedMonths {
		t.Errorf("expected %d archive requests (year boundary), got %d",
			expectedMonths, len(requestedPaths))
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

func TestFetchNextPatchDetail_FixesOrphan(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/100/" {
				json.NewEncoder(w).Encode(api.PatchDetail{
					Patch: api.Patch{
						ID: 100, Name: "Lorem orphan patch",
						Date: "2026-03-10", State: "new",
						Submitter: api.Person{Name: "Lorem"},
						Series: []api.SeriesSummary{{
							ID: 50, Name: "Lorem series",
							Date: "2026-03-10", Version: 1,
						}},
					},
					Content:  "body",
					Diff:     "diff",
					Headers:  map[string]interface{}{},
					Prefixes: []string{},
				})
				return
			}
			w.WriteHeader(404)
		})

	s, d := setupSyncer(t, handler)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 0,
		Name: "Lorem orphan patch", Date: "2026-03-10",
		State: "new", Submitter: "",
	})

	s.fetchNextPatchDetail(context.Background())

	row, _ := d.GetPatch(100)
	if row.SeriesID != 50 {
		t.Errorf("SeriesID = %d, want 50", row.SeriesID)
	}
	if row.Submitter != "Lorem" {
		t.Errorf("Submitter = %q", row.Submitter)
	}
}

func TestFetchNextSeriesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/series/50/" {
				json.NewEncoder(w).Encode(api.Series{
					ID: 50, Name: "Lorem series",
					Date: "2026-03-10", Version: 1,
					Total: 2, ReceivedTotal: 2, ReceivedAll: true,
					Submitter: api.Person{
						Name: "Lorem Ipsum", Email: "lorem@ipsum.example"},
					Patches: []api.PatchSummary{
						{ID: 100, Name: "p1"},
						{ID: 101, Name: "p2"},
					},
				})
				return
			}
			w.WriteHeader(404)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "",
	})
	d.SavePatch(db.PatchRow{
		ID: 101, SeriesID: 50,
		Name: "p2", Date: "2026-03-10",
		State: "new", Submitter: "",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test", srv.Client(), 10*time.Millisecond)

	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	sid := s.fetchNextSeriesDetail(context.Background())

	// Both patches should have submitter set via UpdateSeriesPatches
	r1, _ := d.GetPatch(100)
	if r1.Submitter != "Lorem Ipsum" {
		t.Errorf("patch 100 Submitter = %q", r1.Submitter)
	}
	r2, _ := d.GetPatch(101)
	if r2.Submitter != "Lorem Ipsum" {
		t.Errorf("patch 101 Submitter = %q", r2.Submitter)
	}

	// Series should be marked as detail_fetched
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) != 0 {
		t.Errorf("series should be complete, got %d needing detail", len(refs))
	}

	if sid != 50 {
		t.Errorf("fetchNextSeriesDetail returned %d, want 50", sid)
	}
}

func TestFetchNextSeriesDetail_SkipsComplete(t *testing.T) {
	apiCalled := false
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			apiCalled = true
			w.WriteHeader(404)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	d.SaveSeries(db.SeriesRow{
		ID: 50, Name: "Lorem", Date: "2026-03-10",
		Submitter: "Lorem Ipsum", TotalPatches: 1,
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test", srv.Client(), 10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {}, status.NewRegistry(nil))

	if s.fetchNextSeriesDetail(context.Background()) != 0 {
		t.Error("should return 0 when no series need detail")
	}
	if apiCalled {
		t.Error("should not call API when all series have detail")
	}
}

func TestFetchNextComments_ReturnsSeriesID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test", srv.Client(), 10*time.Millisecond)

	var notifiedIDs []int
	s := NewSyncer(client, d, cfg, func(ids ...int) {
		notifiedIDs = append(notifiedIDs, ids...)
	}, status.NewRegistry(nil))

	sid := s.fetchNextComments(context.Background())
	if sid != 50 {
		t.Errorf("fetchNextComments returned %d, want 50", sid)
	}
}

func TestFetchNextComments_NoWork_ReturnsZero(t *testing.T) {
	s, _ := setupSyncer(t, http.NotFoundHandler())
	// No patches needing comments — should return 0
	sid := s.fetchNextComments(context.Background())
	if sid != 0 {
		t.Errorf("fetchNextComments returned %d, want 0 (no work)", sid)
	}
}

func TestFetchNextPatchDetail_ReturnsSeriesID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(api.PatchDetail{
				Patch: api.Patch{
					ID: 100, Name: "p1", Date: "2026-03-10",
					State: "new", Submitter: api.Person{Name: "Lorem"},
					Series: []api.SeriesSummary{{
						ID: 50, Name: "s1", Date: "2026-03-10", Version: 1,
					}},
				},
				Content: "body", Diff: "diff",
				Headers:  map[string]interface{}{},
				Prefixes: []string{},
			})
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test", srv.Client(), 10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {}, status.NewRegistry(nil))

	sid := s.fetchNextPatchDetail(context.Background())
	if sid != 50 {
		t.Errorf("fetchNextPatchDetail returned %d, want 50", sid)
	}
}

func TestFetchNextChecks(t *testing.T) {
	checksJSON := `[
		{"id": 1, "state": "success", "context": "ci/build",
		 "target_url": "https://ci.example.com/1",
		 "description": "All tests passed", "date": "2026-03-10"},
		{"id": 2, "state": "warning", "context": "ci/lint",
		 "target_url": "https://ci.example.com/2",
		 "description": "Minor issues", "date": "2026-03-10"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(checksJSON))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	result := s.fetchNextChecks(context.Background())
	if result == 0 {
		t.Error("expected non-zero series ID (work done)")
	}

	checks := d.GetChecksForPatch(100)
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(checks))
	}
	if checks[0].Context != "ci/build" || checks[0].State != "success" {
		t.Errorf("check 0: %s/%s", checks[0].Context, checks[0].State)
	}
	if checks[0].Description != "All tests passed" {
		t.Errorf("check 0 desc = %q", checks[0].Description)
	}
	if checks[1].Context != "ci/lint" || checks[1].State != "warning" {
		t.Errorf("check 1: %s/%s", checks[1].Context, checks[1].State)
	}

	row, _ := d.GetPatch(100)
	if row.ChecksPass != 1 || row.ChecksWarn != 1 {
		t.Errorf("counters: pass=%d warn=%d, want 1/1",
			row.ChecksPass, row.ChecksWarn)
	}

	// Should be marked as fetched — no more work
	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 0 {
		t.Errorf("still %d patches needing checks after fetch",
			len(refs))
	}
}

func TestFetchNextChecks_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	result := s.fetchNextChecks(context.Background())
	if result != 0 {
		t.Error("expected 0 (error)")
	}

	// Should be in skip set
	if _, ok := s.checkSkip[100]; !ok {
		t.Error("patch 100 should be in checkSkip after error")
	}

	// Should NOT be marked as fetched
	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 1 {
		t.Errorf("got %d patches needing checks, want 1 (not marked)",
			len(refs))
	}
}

func TestProcessEvent_CheckCreated_NoDescription_ResetsFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.MarkChecksFetched(100)

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	// Event without description
	s.processEvent(api.Event{
		Category: "check-created",
		Payload: &api.CheckCreatedPayload{
			Patch: api.PatchSummary{ID: 100},
			Check: api.CheckSummary{
				ID: 500, State: "success",
				Context: "ci/build",
			},
		},
	}, 50)

	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 1 {
		t.Errorf("got %d refs, want 1 (should be reset)", len(refs))
	}
}

func TestProcessEvent_CheckCreated_WithDescription_KeepsFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.MarkChecksFetched(100)

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	// Event with description
	desc := "All tests passed"
	s.processEvent(api.Event{
		Category: "check-created",
		Payload: &api.CheckCreatedPayload{
			Patch: api.PatchSummary{ID: 100},
			Check: api.CheckSummary{
				ID: 500, State: "success",
				Context:     "ci/build",
				Description: &desc,
			},
		},
	}, 50)

	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 0 {
		t.Errorf("got %d refs, want 0 (has description, flag kept)",
			len(refs))
	}
}

func TestFetchAllForPatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			path := r.URL.Path
			if strings.Contains(path, "/checks/") {
				w.Write([]byte(`[{"id":1,"state":"success",` +
					`"context":"ci/build","description":"ok",` +
					`"date":"2026-03-10"}]`))
			} else if strings.Contains(path, "/comments/") {
				w.Write([]byte(`[{"id":10,"date":"2026-03-10",` +
					`"subject":"Re: test","submitter":` +
					`{"name":"Lorem","email":"l@ex"},` +
					`"content":"looks good","msgid":"<c@ex>",` +
					`"headers":{}}]`))
			} else if strings.Contains(path, "/patches/") {
				w.Write([]byte(`{"id":100,"name":"test",` +
					`"date":"2026-03-10","state":"new",` +
					`"submitter":{"name":"Ipsum","email":"i@ex"},` +
					`"content":"body","diff":"---",` +
					`"headers":{},"prefixes":[],"web_url":"",` +
					`"msgid":"<p@ex>","mbox":"","commit_ref":null,` +
					`"archived":false,"series":[]}`))
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchAllForPatch(context.Background(), 100)

	row, _ := d.GetPatch(100)
	if !row.DetailFetched {
		t.Error("detail should be fetched")
	}
	if d.NeedsPatchComments(100) {
		t.Error("comments should be fetched")
	}
	if d.NeedsPatchChecks(100) {
		t.Error("checks should be fetched")
	}
	checks := d.GetChecksForPatch(100)
	if len(checks) != 1 {
		t.Errorf("got %d checks, want 1", len(checks))
	}
}

func TestFetchAllForPatch_AlreadyFetched(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchDetail(100, "body", "---", "{}", "[]")
	d.MarkCommentsFetched(100)
	d.MarkChecksFetched(100)

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchAllForPatch(context.Background(), 100)

	if requestCount != 0 {
		t.Errorf("expected 0 requests (all fetched), got %d",
			requestCount)
	}
}

func TestFetchAllForSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			path := r.URL.Path
			if strings.Contains(path, "/checks/") {
				w.Write([]byte("[]"))
			} else if strings.Contains(path, "/comments/") {
				w.Write([]byte("[]"))
			} else if strings.Contains(path, "/covers/") {
				w.Write([]byte(`{"id":99,"name":"cover",` +
					`"date":"2026-03-10","content":"body",` +
					`"headers":{}}`))
			} else if strings.Contains(path, "/patches/") {
				w.Write([]byte(`{"id":100,"name":"test",` +
					`"date":"2026-03-10","state":"new",` +
					`"submitter":{"name":"Ipsum","email":"i@ex"},` +
					`"content":"body","diff":"---",` +
					`"headers":{},"prefixes":[],"web_url":"",` +
					`"msgid":"<p@ex>","mbox":"","commit_ref":null,` +
					`"archived":false,"series":[]}`))
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SaveSeriesSummary(50, "test series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "patch 1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(db.PatchRow{
		ID: 101, SeriesID: 50, Name: "patch 2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50, Name: "cover",
		Date: "2026-03-10",
	})

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchAllForSeries(context.Background(), 50)

	// Patches should be fully fetched
	row, _ := d.GetPatch(100)
	if !row.DetailFetched {
		t.Error("patch 100 detail not fetched")
	}
	row, _ = d.GetPatch(101)
	if !row.DetailFetched {
		t.Error("patch 101 detail not fetched")
	}
	if d.NeedsPatchComments(100) {
		t.Error("patch 100 comments not fetched")
	}
	if d.NeedsPatchComments(101) {
		t.Error("patch 101 comments not fetched")
	}
	if d.NeedsPatchChecks(100) {
		t.Error("patch 100 checks not fetched")
	}
	if d.NeedsPatchChecks(101) {
		t.Error("patch 101 checks not fetched")
	}

	// Cover should be fetched
	cover, _ := d.GetCover(50)
	if !cover.DetailFetched {
		t.Error("cover detail not fetched")
	}
	if d.NeedsCoverComments(99) {
		t.Error("cover comments not fetched")
	}
}

func TestFetchAllForSeries_PartiallyFetched(t *testing.T) {
	requestPaths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestPaths = append(requestPaths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			path := r.URL.Path
			if strings.Contains(path, "/checks/") {
				w.Write([]byte("[]"))
			} else if strings.Contains(path, "/comments/") {
				w.Write([]byte("[]"))
			} else if strings.Contains(path, "/patches/") {
				w.Write([]byte(`{"id":101,"name":"test",` +
					`"date":"2026-03-10","state":"new",` +
					`"submitter":{"name":"Ipsum","email":"i@ex"},` +
					`"content":"body","diff":"---",` +
					`"headers":{},"prefixes":[],"web_url":"",` +
					`"msgid":"<p@ex>","mbox":"","commit_ref":null,` +
					`"archived":false,"series":[]}`))
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SaveSeriesSummary(50, "test series", "2026-03-10", 1)
	// Patch 100: fully fetched
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "patch 1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchDetail(100, "body", "---", "{}", "[]")
	d.MarkCommentsFetched(100)
	d.MarkChecksFetched(100)
	// Patch 101: not fetched
	d.SavePatch(db.PatchRow{
		ID: 101, SeriesID: 50, Name: "patch 2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchAllForSeries(context.Background(), 50)

	// Should only have fetched for patch 101 (100 already done)
	has100 := false
	for _, p := range requestPaths {
		if strings.Contains(p, "/100/") {
			has100 = true
		}
	}
	if has100 {
		t.Error("should not fetch for patch 100 (already complete)")
	}
	if d.NeedsPatchChecks(101) {
		t.Error("patch 101 checks should be fetched")
	}
}

func TestFetchDetailForPatch_Extracted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":100,"name":"test",` +
				`"date":"2026-03-10","state":"new",` +
				`"submitter":{"name":"Lorem","email":"l@ex"},` +
				`"content":"Reviewed-by: Ipsum <i@ex>",` +
				`"diff":"---","headers":{},"prefixes":[],` +
				`"web_url":"","msgid":"<p@ex>","mbox":"",` +
				`"commit_ref":null,"archived":false,"series":[]}`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	err := s.fetchDetailForPatch(context.Background(), 100, 50, status.Detail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	row, _ := d.GetPatch(100)
	if !row.DetailFetched {
		t.Error("detail not marked as fetched")
	}
}

func TestRequestDetail_Patch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":100,"name":"test",` +
				`"date":"2026-03-10","state":"new",` +
				`"submitter":{"name":"Lorem","email":"l@ex"},` +
				`"content":"body","diff":"---",` +
				`"headers":{"To":"dev@ex"},"prefixes":[],` +
				`"web_url":"","msgid":"","mbox":"",` +
				`"commit_ref":null,"archived":false,"series":[]}`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	if !d.NeedsPatchDetail(100) {
		t.Fatal("should need detail before fetch")
	}

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchDetailForPatch(context.Background(), 100, 50,
		status.Detail)

	if d.NeedsPatchDetail(100) {
		t.Error("should not need detail after fetch")
	}
	row, _ := d.GetPatch(100)
	if row.Content != "body" {
		t.Errorf("content = %q", row.Content)
	}
}

func TestRequestDetail_Cover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":99,"name":"cover",` +
				`"date":"2026-03-10","content":"overview",` +
				`"headers":{"To":"dev@ex"}}`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50, Name: "cover",
		Date: "2026-03-10",
	})
	if !d.NeedsCoverDetail(99) {
		t.Fatal("should need detail before fetch")
	}

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchDetailForCover(context.Background(), 99, 50,
		status.Detail)

	if d.NeedsCoverDetail(99) {
		t.Error("should not need detail after fetch")
	}
}

func TestFetchDetailForCover_Extracted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":99,"name":"cover",` +
				`"date":"2026-03-10","content":"overview",` +
				`"headers":{}}`))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50, Name: "cover",
		Date: "2026-03-10",
	})

	cfg := &config.Config{
		Server: srv.URL, Project: "test",
		States: []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	err := s.fetchDetailForCover(context.Background(), 99, 50, status.Detail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cover, _ := d.GetCover(50)
	if !cover.DetailFetched {
		t.Error("cover detail not marked as fetched")
	}
}

func TestBackfillHistory_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			t.Error("no requests expected when history disabled")
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
		// HistoryLimit is zero (default)
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	s.backfillHistory(context.Background())
}

func TestBackfillHistory_FetchesPages(t *testing.T) {
	// Return patches that get progressively older
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			callCount++
			path := r.URL.Path
			if strings.Contains(path, "/patches/") {
				// Return one patch per page, getting older
				date := time.Now().UTC().AddDate(0, 0,
					-callCount*30).Format("2006-01-02T15:04:05")
				fmt.Fprintf(w,
					`[{"id":%d,"name":"patch %d","date":"%s",`+
						`"state":"accepted","submitter":{"name":"Lorem","email":"l@ex"},`+
						`"delegate":null,"series":[],"web_url":"","msgid":"","mbox":"",`+
						`"commit_ref":null,"archived":false}]`,
					1000+callCount, callCount, date)
			} else if strings.Contains(path, "/series/") {
				w.Write([]byte("[]"))
			}
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	// Seed a recent patch so oldest date is recent
	d.SavePatch(db.PatchRow{
		ID: 1, Name: "recent", State: "new",
		Submitter: "Lorem",
		Date:      time.Now().UTC().Format("2006-01-02T15:04:05"),
	})

	cfg := &config.Config{
		Server:       srv.URL,
		Project:      "test",
		States:       []string{"new"},
		HistoryLimit: config.HistoryLimit{Years: 1},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	s.backfillHistory(context.Background())

	if callCount < 2 {
		t.Errorf("expected multiple API calls, got %d", callCount)
	}
}

func TestBackfillHistory_AlreadyComplete(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	now := time.Now().UTC()
	oldDate := time.Date(now.Year()-2, now.Month(), 15, 0, 0, 0, 0, time.UTC).
		Format("2006-01-02T15:04:05")
	// Simulate a previous backfill run that already covered this range
	d.SetSyncState("backfill_patches_since", oldDate)
	d.SetSyncState("backfill_series_since", oldDate)

	cfg := &config.Config{
		Server:       srv.URL,
		Project:      "test",
		States:       []string{"new"},
		HistoryLimit: config.HistoryLimit{Years: 1},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))
	s.backfillHistory(context.Background())

	if requestCount != 0 {
		t.Errorf("expected 0 requests (already complete), got %d",
			requestCount)
	}
}

func TestFetchSeriesSince_SavesPatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/series/" {
				json.NewEncoder(w).Encode([]api.Series{
					{
						ID: 50, Name: "Lorem series",
						Date: "2026-03-09", Version: 1,
						Total: 2, ReceivedTotal: 2, ReceivedAll: true,
						Submitter: api.Person{
							Name: "Lorem Ipsum", Email: "lorem@ipsum.example"},
						Patches: []api.PatchSummary{
							{ID: 100, Name: "p1", Date: "2026-03-09",
								MsgID: "<100@ex>", Mbox: "https://pw.example/p/100/mbox/"},
							{ID: 101, Name: "p2", Date: "2026-03-09",
								MsgID: "<101@ex>", Mbox: "https://pw.example/p/101/mbox/"},
						},
					},
				})
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
		States:  []string{"new"},
	}
	client := api.NewClientForTest(srv.URL, "test", srv.Client(), 10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {}, status.NewRegistry(nil))

	s.fetchSeriesSince(context.Background(), "2026-03-01", status.Sync)

	// Series should exist with full data
	allSeries := d.GetAllSeries()
	if len(allSeries) != 1 || allSeries[0].ID != 50 {
		t.Fatalf("GetAllSeries = %d series, want 1", len(allSeries))
	}
	if allSeries[0].Submitter != "Lorem Ipsum" {
		t.Errorf("submitter = %q, want Lorem Ipsum", allSeries[0].Submitter)
	}

	// Patches should have been created from the series response
	r1, _ := d.GetPatch(100)
	if r1 == nil {
		t.Fatal("patch 100 should exist")
	}
	if r1.SeriesID != 50 {
		t.Errorf("patch 100 series_id = %d, want 50", r1.SeriesID)
	}
	if r1.Submitter != "Lorem Ipsum" {
		t.Errorf("patch 100 submitter = %q, want Lorem Ipsum", r1.Submitter)
	}

	r2, _ := d.GetPatch(101)
	if r2 == nil {
		t.Fatal("patch 101 should exist")
	}
	if r2.SeriesID != 50 {
		t.Errorf("patch 101 series_id = %d, want 50", r2.SeriesID)
	}
}

func TestFetchChecksForPatch_OnDemand(t *testing.T) {
	checksJSON := `[
		{"id": 1, "state": "success", "context": "ci/build",
		 "target_url": "https://ci.example.com/1",
		 "description": "All tests passed", "date": "2026-03-10"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(checksJSON))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	if !d.NeedsPatchChecks(100) {
		t.Fatal("should need checks before fetch")
	}

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	s.fetchChecksForPatch(context.Background(), 100, 50, status.BgChecks)

	if d.NeedsPatchChecks(100) {
		t.Error("should not need checks after on-demand fetch")
	}
	checks := d.GetChecksForPatch(100)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
	if checks[0].Description != "All tests passed" {
		t.Errorf("description = %q", checks[0].Description)
	}
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 1 {
		t.Errorf("pass = %d, want 1", row.ChecksPass)
	}
}

func TestFetchNextChecks_Terminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
		}))
	defer srv.Close()

	d, _ := db.Open(":memory:")
	defer d.Close()
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50, Name: "test",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})

	cfg := &config.Config{
		Server:  srv.URL,
		Project: "test",
		States:  []string{"new"},
	}
	client := api.NewClientForTest(
		srv.URL, "test", srv.Client(),
		10*time.Millisecond)
	s := NewSyncer(client, d, cfg, func(...int) {},
		status.NewRegistry(nil))

	result := s.fetchNextChecks(context.Background())
	if result == 0 {
		t.Error("expected non-zero (terminal work done)")
	}

	// Second call within cooldown should skip terminal work
	result = s.fetchNextChecks(context.Background())
	if result != 0 {
		t.Error("expected 0 (terminal cooldown active)")
	}
}

func TestRefreshArchivedPatches(t *testing.T) {
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patches/" &&
				r.URL.Query().Get("archived") == "true" {
				json.NewEncoder(w).Encode([]api.Patch{{
					ID: 100, Name: "Lorem archived",
					Date: "2026-03-10", State: "new",
					Archived:  true,
					Submitter: api.Person{Name: "Lorem"},
					Series: []api.SeriesSummary{{
						ID: 50, Name: "Lorem series",
					}},
				}})
				return
			}
			json.NewEncoder(w).Encode([]api.Patch{})
		})

	s, d := setupSyncer(t, handler)

	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "Lorem archived", State: "new",
		Date: "2026-03-10T10:00:00", Archived: false,
	})

	// Verify patch is active in DB.
	row, _ := d.GetPatch(100)
	if row.Archived {
		t.Fatal("patch should start as non-archived")
	}

	s.refreshArchivedPatches(context.Background())

	// Verify patch is now archived.
	row, _ = d.GetPatch(100)
	if !row.Archived {
		t.Error("patch should be archived after refresh")
	}
}

func TestRefreshArchivedPatches_NoActivePatches(t *testing.T) {
	called := false
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			called = true
			json.NewEncoder(w).Encode([]api.Patch{})
		})

	s, _ := setupSyncer(t, handler)

	// No patches in DB — should not make any API calls.
	s.refreshArchivedPatches(context.Background())

	if called {
		t.Error("should not call API when no active patches exist")
	}
}
