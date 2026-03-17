package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	result := s.fetchMbox(context.Background(), 100)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != "cached mbox content" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestFetchMbox_FromLore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/lorem-001@ipsum.example/raw" {
				w.Write([]byte("lore mbox content"))
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

	result := s.fetchMbox(context.Background(), 100)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != "lore mbox content" {
		t.Errorf("Content = %q", result.Content)
	}

	row, _ := d.GetPatch(100)
	if row.MboxContent != "lore mbox content" {
		t.Errorf("cached = %q", row.MboxContent)
	}
}

func TestFetchMbox_FromPatchwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/patch/100/mbox/" {
				w.Write([]byte("patchwork mbox"))
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

	result := s.fetchMbox(context.Background(), 100)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Content != "patchwork mbox" {
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
