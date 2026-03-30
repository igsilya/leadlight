package db

import (
	"fmt"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpen(t *testing.T) {
	d := openTestDB(t)
	if d == nil {
		t.Fatal("DB is nil")
	}
}

func TestSyncState(t *testing.T) {
	d := openTestDB(t)

	if v := d.GetSyncState("missing"); v != "" {
		t.Errorf("got %q, want empty for missing key", v)
	}

	d.SetSyncState("last_event_date", "2026-03-10T12:00:00")
	if v := d.GetSyncState("last_event_date"); v != "2026-03-10T12:00:00" {
		t.Errorf("got %q", v)
	}

	d.SetSyncState("last_event_date", "2026-03-11T00:00:00")
	if v := d.GetSyncState("last_event_date"); v != "2026-03-11T00:00:00" {
		t.Errorf("got %q after update", v)
	}
}

func TestSaveSeries(t *testing.T) {
	d := openTestDB(t)

	s := SeriesRow{
		ID:              50,
		Name:            "[PATCH] Lorem ipsum dolor v2",
		Date:            "2026-03-10T12:00:00",
		Version:         2,
		TotalPatches:    3,
		ReceivedPatches: 3,
		Complete:        true,
		Submitter:       "Dolor Amet",
		SubmitterEmail:  "dolor@amet.example",
		WebURL:          "https://pw.example.com/series/50/",
		MboxURL:         "https://pw.example.com/series/50/mbox/",
	}
	if err := d.SaveSeries(s); err != nil {
		t.Fatal(err)
	}

	rows := d.GetActiveSeries([]string{"new"})
	// No patches yet, so no active series
	if len(rows) != 0 {
		t.Errorf("got %d active series, want 0 (no patches)", len(rows))
	}

	// Update the series
	s.Name = "[PATCH] Lorem ipsum dolor v3"
	s.Version = 3
	if err := d.SaveSeries(s); err != nil {
		t.Fatal(err)
	}
}

func TestSaveSeriesSummary(t *testing.T) {
	d := openTestDB(t)

	if err := d.SaveSeriesSummary(60, "[PATCH] Sit amet consectetur", "2026-03-10T12:00:00", 1); err != nil {
		t.Fatal(err)
	}
}

func TestSavePatch(t *testing.T) {
	d := openTestDB(t)

	// First save a series
	d.SaveSeriesSummary(50, "[PATCH] Adipiscing elit", "2026-03-10T12:00:00", 1)

	p := PatchRow{
		ID:             100,
		Name:           "[PATCH] Lorem ipsum dolor",
		Date:           "2026-03-10T12:00:00",
		State:          "new",
		MsgID:          "<lorem-ipsum@ipsum.example>",
		MboxURL:        "https://pw.example.com/patch/100/mbox/",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
		DelegateID:     42,
		Delegate:       "lorem",
		DelegateEmail:  "lorem@pw.example.com",
		SeriesID:       50,
	}
	if err := d.SavePatch(p); err != nil {
		t.Fatal(err)
	}

	row, err := d.GetPatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "[PATCH] Lorem ipsum dolor" {
		t.Errorf("Name = %q", row.Name)
	}
	if row.State != "new" {
		t.Errorf("State = %q", row.State)
	}
	if row.Submitter != "Lorem Ipsum" {
		t.Errorf("Submitter = %q", row.Submitter)
	}
	if row.Delegate != "lorem" {
		t.Errorf("Delegate = %q", row.Delegate)
	}
	if row.DelegateID != 42 {
		t.Errorf("DelegateID = %d", row.DelegateID)
	}
	if row.SeriesID != 50 {
		t.Errorf("SeriesID = %d", row.SeriesID)
	}
}

func TestSavePatch_PreservesDetail(t *testing.T) {
	d := openTestDB(t)

	p := PatchRow{
		ID:             100,
		Name:           "[PATCH] Consectetur adipiscing",
		Date:           "2026-03-10T12:00:00",
		State:          "new",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
	}
	d.SavePatch(p)

	// Store detail
	d.UpdatePatchDetail(100, "Lorem ipsum dolor sit amet.", "--- a/f\n+++ b/f\n-old\n+new", "{}", `["PATCH"]`)

	// Save the patch again (simulating a re-fetch from list API, which lacks content/diff)
	p.State = "under-review"
	d.SavePatch(p)

	row, err := d.GetPatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "under-review" {
		t.Errorf("State = %q, want updated", row.State)
	}
	if row.Content != "Lorem ipsum dolor sit amet." {
		t.Errorf("Content = %q, want preserved", row.Content)
	}
	if row.Diff != "--- a/f\n+++ b/f\n-old\n+new" {
		t.Errorf("Diff = %q, want preserved", row.Diff)
	}
	if !row.DetailFetched {
		t.Error("DetailFetched = false, want preserved")
	}
}

func TestSavePatchFromSummary(t *testing.T) {
	d := openTestDB(t)

	if err := d.SavePatchSummary(200, 60, "[PATCH] Dolor sit amet", "2026-03-11T00:00:00", "<dolor-sit@ipsum.example>", "", ""); err != nil {
		t.Fatal(err)
	}

	row, err := d.GetPatch(200)
	if err != nil {
		t.Fatal(err)
	}
	if row.SeriesID != 60 {
		t.Errorf("SeriesID = %d", row.SeriesID)
	}
	if row.State != "new" {
		t.Errorf("State = %q, want default 'new'", row.State)
	}
}

func TestUpdatePatchState(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	if err := d.UpdatePatchState(100, "under-review"); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.State != "under-review" {
		t.Errorf("State = %q", row.State)
	}
}

func TestUpdatePatchDelegate(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	if err := d.UpdatePatchDelegate(100, 55, "lorem", "lorem@pw.example.com"); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.Delegate != "lorem" {
		t.Errorf("Delegate = %q", row.Delegate)
	}
	if row.DelegateID != 55 {
		t.Errorf("DelegateID = %d", row.DelegateID)
	}

	// Clear delegate
	if err := d.UpdatePatchDelegate(100, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	row, _ = d.GetPatch(100)
	if row.Delegate != "" {
		t.Errorf("Delegate = %q, want empty after clear", row.Delegate)
	}
}

func TestUpdatePatchDetail(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	content := "Sed ut perspiciatis unde omnis.\n\nSigned-off-by: Lorem <lorem@ipsum.example>"
	diff := "--- a/file.c\n+++ b/file.c\n-old\n+new"
	if err := d.UpdatePatchDetail(100, content, diff, "{}", `["PATCH","v2","1/3"]`); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.Content != content {
		t.Errorf("Content = %q", row.Content)
	}
	if row.Diff != diff {
		t.Errorf("Diff = %q", row.Diff)
	}
	if !row.DetailFetched {
		t.Error("DetailFetched = false")
	}
}

func TestUpdatePatchChecks(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	if err := d.UpdatePatchChecks(100, 3, 1, 2); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.ChecksPass != 3 || row.ChecksFail != 1 || row.ChecksWarn != 2 {
		t.Errorf("Checks = %d/%d/%d", row.ChecksPass, row.ChecksFail, row.ChecksWarn)
	}
}

func TestSaveCheck_InsertAndUpsert(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	check := CheckRow{
		ID:        500,
		PatchID:   100,
		Date:      "2026-03-10T13:00:00",
		State:     "success",
		TargetURL: "https://pw.example.com/ci/123",
		Context:   "ci/build",
	}
	if err := d.SaveCheck(check); err != nil {
		t.Fatal(err)
	}

	// Insert same check again — should not error (idempotent)
	if err := d.SaveCheck(check); err != nil {
		t.Fatal(err)
	}
}

func TestSaveCheck_UpsertPreservesDescription(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Event inserts check without description
	d.SaveCheck(CheckRow{
		ID: 500, PatchID: 100, State: "pending",
		Context: "ci/build", Date: "2026-03-10T10:00:00",
	})
	// Check loop fetches same check with description
	d.SaveCheck(CheckRow{
		ID: 500, PatchID: 100, State: "success",
		Context:     "ci/build",
		Description: "All tests passed",
		Date:        "2026-03-10T10:05:00",
	})
	checks := d.GetChecksForPatch(100)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
	if checks[0].State != "success" {
		t.Errorf("state = %q, want success", checks[0].State)
	}
	if checks[0].Description != "All tests passed" {
		t.Errorf("description = %q, want 'All tests passed'",
			checks[0].Description)
	}
}

func TestSaveCheck_UpsertKeepsExistingDescription(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Check loop inserts with description
	d.SaveCheck(CheckRow{
		ID: 500, PatchID: 100, State: "success",
		Context:     "ci/build",
		Description: "All tests passed",
		Date:        "2026-03-10T10:05:00",
	})
	// Event re-inserts same check without description
	d.SaveCheck(CheckRow{
		ID: 500, PatchID: 100, State: "success",
		Context: "ci/build",
		Date:    "2026-03-10T10:05:00",
	})
	checks := d.GetChecksForPatch(100)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
	if checks[0].Description != "All tests passed" {
		t.Errorf("description = %q, want 'All tests passed' (should be preserved)",
			checks[0].Description)
	}
}

func TestInsertComment(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	comment := CommentRow{
		ID:        300,
		PatchID:   100,
		MsgID:     "<amet-reply@ipsum.example>",
		Date:      "2026-03-11T09:00:00",
		Subject:   "Re: [PATCH] Lorem ipsum dolor",
		Submitter: "Dolor Amet",
		Content:   "Nulla facilisi cras fermentum.\n\nAcked-by: Dolor Amet <dolor@amet.example>",
	}
	if err := d.InsertComment(comment); err != nil {
		t.Fatal(err)
	}

	comments := d.GetComments(100)
	if len(comments) != 1 {
		t.Fatalf("len(comments) = %d", len(comments))
	}
	if comments[0].Content != comment.Content {
		t.Errorf("Content = %q", comments[0].Content)
	}
	if comments[0].Submitter != "Dolor Amet" {
		t.Errorf("Submitter = %q", comments[0].Submitter)
	}
}

func TestSaveCover(t *testing.T) {
	d := openTestDB(t)

	cover := CoverRow{
		ID:             99,
		SeriesID:       50,
		Name:           "[PATCH 0/3] Lorem ipsum series",
		Date:           "2026-03-10T12:00:00",
		MsgID:          "<cover-lorem@ipsum.example>",
		Submitter:      "Dolor Amet",
		SubmitterEmail: "dolor@amet.example",
		MboxURL:        "https://pw.example.com/cover/99/mbox/",
	}
	if err := d.SaveCover(cover); err != nil {
		t.Fatal(err)
	}

	row, err := d.GetCover(50)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "[PATCH 0/3] Lorem ipsum series" {
		t.Errorf("Name = %q", row.Name)
	}
}

func TestUpdateCoverDetail(t *testing.T) {
	d := openTestDB(t)

	cover := CoverRow{
		ID:       99,
		SeriesID: 50,
		Name:     "[PATCH] Elit sed do eiusmod",
		Date:     "2026-03-10T12:00:00",
	}
	d.SaveCover(cover)

	if err := d.UpdateCoverDetail(99, "Ut enim ad minim veniam quis nostrud.", "{}"); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetCover(50)
	if row.Content != "Ut enim ad minim veniam quis nostrud." {
		t.Errorf("Content = %q", row.Content)
	}
	if !row.DetailFetched {
		t.Error("DetailFetched = false")
	}
}

func TestSaveMaintainers(t *testing.T) {
	d := openTestDB(t)

	users := []MaintainerRow{
		{ID: 22, Username: "amet", FirstName: "Dolor", LastName: "Amet", Email: "amet@ipsum.example"},
		{ID: 25, Username: "consectetur", FirstName: "Consec", LastName: "Tetur", Email: "consectetur@ipsum.example"},
	}
	if err := d.SaveMaintainers(users); err != nil {
		t.Fatal(err)
	}

	rows := d.GetMaintainers()
	if len(rows) != 2 {
		t.Fatalf("len = %d", len(rows))
	}
	if rows[0].Username != "amet" {
		t.Errorf("[0].Username = %q", rows[0].Username)
	}

	// Replace with a new set
	newUsers := []MaintainerRow{
		{ID: 1, Username: "adipiscing", FirstName: "Adipi", LastName: "Scing", Email: "adipiscing@ipsum.example"},
	}
	d.SaveMaintainers(newUsers)

	rows = d.GetMaintainers()
	if len(rows) != 1 {
		t.Fatalf("len = %d after replace", len(rows))
	}
	if rows[0].Username != "adipiscing" {
		t.Errorf("[0].Username = %q", rows[0].Username)
	}
}

func TestSaveTags(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	tags := map[string]map[string]bool{
		"acked":    {"Lorem <lorem@ipsum.example>": true},
		"reviewed": {"Dolor <dolor@amet.example>": true},
	}
	d.SaveTags(100, 0, 500, "comment", tags)

	rows := d.GetTagsForSeries(50)
	if len(rows) != 2 {
		t.Fatalf("len=%d, want 2", len(rows))
	}
}

func TestSaveTags_ClearsOnResave(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)

	tags1 := map[string]map[string]bool{
		"acked": {"Lorem <lorem@ipsum.example>": true},
	}
	d.SaveTags(100, 0, 0, "original", tags1)

	rows := d.GetTagsForSeries(50)
	if len(rows) != 1 {
		t.Fatalf("after first save: len=%d", len(rows))
	}

	d.ClearTags(100, 0, "original")
	tags2 := map[string]map[string]bool{
		"reviewed": {"Dolor <dolor@amet.example>": true},
	}
	d.SaveTags(100, 0, 0, "original", tags2)

	rows = d.GetTagsForSeries(50)
	if len(rows) != 1 {
		t.Fatalf("after clear+resave: len=%d, want 1", len(rows))
	}
	if rows[0].Type != "reviewed" {
		t.Errorf("type=%q, want reviewed", rows[0].Type)
	}
}

func TestSaveTags_MultipleComments(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	d.ClearTags(100, 0, "comment")
	d.SaveTags(100, 0, 500, "comment", map[string]map[string]bool{
		"acked": {"Lorem <lorem@ex>": true},
	})
	d.SaveTags(100, 0, 501, "comment", map[string]map[string]bool{
		"acked": {"Dolor <dolor@ex>": true},
	})

	rows := d.GetTagsForSeries(50)
	if len(rows) != 2 {
		t.Fatalf("len=%d, want 2 (both comments)", len(rows))
	}
}

func TestSaveTags_Cover(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})
	tags := map[string]map[string]bool{
		"acked": {"Lorem <lorem@ipsum.example>": true},
	}
	d.SaveTags(0, 99, 600, "comment", tags)

	rows := d.GetTagsForSeries(50)
	if len(rows) != 1 {
		t.Fatalf("len=%d, want 1", len(rows))
	}
	if rows[0].CoverID != 99 {
		t.Errorf("CoverID=%d", rows[0].CoverID)
	}
	if rows[0].CommentID != 600 {
		t.Errorf("CommentID=%d", rows[0].CommentID)
	}
}

func TestGetTagsForSeries(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})

	d.SaveTags(100, 0, 0, "original", map[string]map[string]bool{
		"acked": {"Lorem <lorem@ex>": true},
	})
	d.SaveTags(101, 0, 500, "comment", map[string]map[string]bool{
		"reviewed": {"Dolor <dolor@ex>": true},
	})
	d.SaveTags(0, 99, 600, "comment", map[string]map[string]bool{
		"acked": {"Sit <sit@ex>": true},
	})

	rows := d.GetTagsForSeries(50)
	if len(rows) != 3 {
		t.Fatalf("len=%d, want 3", len(rows))
	}
}

func TestGetTagsForSeries_Empty(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)

	rows := d.GetTagsForSeries(50)
	if len(rows) != 0 {
		t.Errorf("len=%d, want 0", len(rows))
	}
}

func TestGetCommentCountForSeries(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover", Date: "2026-03-10",
	})

	d.InsertComment(CommentRow{
		ID: 500, PatchID: 100, Submitter: "Dolor",
		Date: "2026-03-11", Subject: "Re: p1",
		Content: "lorem ipsum",
	})
	d.InsertComment(CommentRow{
		ID: 501, PatchID: 100, Submitter: "Sit",
		Date: "2026-03-12", Subject: "Re: p1",
		Content: "dolor amet",
	})
	d.InsertComment(CommentRow{
		ID: 600, CoverID: 99, Submitter: "Amet",
		Date: "2026-03-11", Subject: "Re: cover",
		Content: "consectetur",
	})

	count := d.GetCommentCountForSeries(50)
	if count != 3 {
		t.Errorf("count=%d, want 3", count)
	}

	count = d.GetCommentCountForSeries(999)
	if count != 0 {
		t.Errorf("nonexistent series: count=%d, want 0", count)
	}
}

func TestGetAllPatchesBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p3",
		Date: "2026-03-11", State: "accepted", Submitter: "Dolor",
	})

	m := d.GetAllPatchesBatch(false, []string{"new"})
	if len(m[50]) != 2 {
		t.Errorf("series 50: %d patches, want 2", len(m[50]))
	}
	if len(m[51]) != 0 {
		t.Errorf("series 51: %d, want 0 (filtered)", len(m[51]))
	}

	m = d.GetAllPatchesBatch(true, nil)
	if len(m[50]) != 2 || len(m[51]) != 1 {
		t.Errorf("show all: s50=%d s51=%d", len(m[50]), len(m[51]))
	}
}

func TestGetTagsBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "new", Submitter: "Dolor",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10",
	})
	d.SaveTags(100, 0, 0, "original", map[string]map[string]bool{
		"acked": {"A <a@ex>": true},
	})
	d.SaveTags(0, 99, 500, "comment", map[string]map[string]bool{
		"reviewed": {"B <b@ex>": true},
	})
	d.SaveTags(200, 0, 0, "original", map[string]map[string]bool{
		"tested": {"C <c@ex>": true},
	})

	m := d.GetTagsBatch(false, []string{"new"})
	if len(m[50]) != 2 {
		t.Errorf("series 50: %d tags, want 2", len(m[50]))
	}
	if len(m[51]) != 1 {
		t.Errorf("series 51: %d tags, want 1", len(m[51]))
	}
}

func TestGetCommentCountsBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "new", Submitter: "Dolor",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10",
	})
	d.InsertComment(CommentRow{
		ID: 500, PatchID: 100, Submitter: "Lorem",
		Date: "2026-03-11", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 501, CoverID: 99, Submitter: "Dolor",
		Date: "2026-03-11", Subject: "Re: c1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 600, PatchID: 200, Submitter: "Sit",
		Date: "2026-03-12", Subject: "Re: p2", Content: "ok",
	})

	m := d.GetCommentCountsBatch(false, []string{"new"})
	if m[50] != 2 {
		t.Errorf("series 50: %d comments, want 2", m[50])
	}
	if m[51] != 1 {
		t.Errorf("series 51: %d comments, want 1", m[51])
	}
}

func TestGetPatchCommentCountsBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Ipsum",
	})
	d.InsertComment(CommentRow{
		ID: 500, PatchID: 100, Submitter: "Dolor",
		Date: "2026-03-11", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 501, PatchID: 100, Submitter: "Sit",
		Date: "2026-03-12", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 502, PatchID: 101, Submitter: "Amet",
		Date: "2026-03-12", Subject: "Re: p2", Content: "ok",
	})

	m := d.GetPatchCommentCountsBatch(false, []string{"new"})
	if m[100] != 2 {
		t.Errorf("patch 100: %d comments, want 2", m[100])
	}
	if m[101] != 1 {
		t.Errorf("patch 101: %d comments, want 1", m[101])
	}
}

func TestGetCommentSubmittersBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10",
	})
	d.InsertComment(CommentRow{
		ID: 500, PatchID: 100, Submitter: "Dolor Amet",
		Date: "2026-03-11", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 501, CoverID: 99, Submitter: "Sit Ipsum",
		Date: "2026-03-12", Subject: "Re: c1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 502, PatchID: 100, Submitter: "Dolor Amet",
		Date: "2026-03-13", Subject: "Re: p1", Content: "ok again",
	})

	m := d.GetCommentSubmittersBatch(false, []string{"new"})
	names := m[50]
	if len(names) != 2 {
		t.Fatalf("series 50: %d names, want 2", len(names))
	}
	if names[0] != "Dolor Amet" {
		t.Errorf("names[0] = %q, want Dolor Amet", names[0])
	}
	if names[1] != "Sit Ipsum" {
		t.Errorf("names[1] = %q, want Sit Ipsum", names[1])
	}
}

func TestGetPatchCommentSubmittersBatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.InsertComment(CommentRow{
		ID: 500, PatchID: 100, Submitter: "Dolor Amet",
		Date: "2026-03-11", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 501, PatchID: 100, Submitter: "Sit Ipsum",
		Date: "2026-03-12", Subject: "Re: p1", Content: "ok",
	})
	d.InsertComment(CommentRow{
		ID: 502, PatchID: 100, Submitter: "Dolor Amet",
		Date: "2026-03-13", Subject: "Re: p1", Content: "ok again",
	})

	m := d.GetPatchCommentSubmittersBatch(false, []string{"new"})
	names := m[100]
	if len(names) != 2 {
		t.Fatalf("patch 100: %d names, want 2", len(names))
	}
	if names[0] != "Dolor Amet" {
		t.Errorf("names[0] = %q, want Dolor Amet", names[0])
	}
	if names[1] != "Sit Ipsum" {
		t.Errorf("names[1] = %q, want Sit Ipsum", names[1])
	}
}

func TestGetAllPatchesBatch_MultipleStates(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "under-review",
		Submitter: "Dolor",
	})

	m := d.GetAllPatchesBatch(false, []string{"new", "under-review"})
	if len(m[50]) != 1 || len(m[51]) != 1 {
		t.Errorf("s50=%d s51=%d, want 1,1", len(m[50]), len(m[51]))
	}
}

func TestGetAllPatchesBatch_MixedStates(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})

	m := d.GetAllPatchesBatch(false, []string{"new"})
	if len(m[50]) != 2 {
		t.Errorf("should return ALL patches (both states): got %d",
			len(m[50]))
	}
}

func TestGetAllPatchesBatch_Ordering(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 103, SeriesID: 50, Name: "p3",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 102, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	m := d.GetAllPatchesBatch(true, nil)
	patches := m[50]
	if len(patches) != 3 {
		t.Fatalf("got %d patches", len(patches))
	}
	if patches[0].ID != 101 || patches[1].ID != 102 ||
		patches[2].ID != 103 {
		t.Errorf("wrong order: %d, %d, %d",
			patches[0].ID, patches[1].ID, patches[2].ID)
	}
}

func TestGetTagsBatch_CoverOnly(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10",
	})
	d.SaveTags(0, 99, 500, "comment", map[string]map[string]bool{
		"acked": {"Lorem <lorem@ex>": true},
	})

	m := d.GetTagsBatch(false, []string{"new"})
	if len(m[50]) != 1 {
		t.Errorf("cover-only tags: got %d, want 1", len(m[50]))
	}
	if m[50][0].CoverID != 99 {
		t.Errorf("cover_id = %d, want 99", m[50][0].CoverID)
	}
}

func TestGetTagsBatch_OrphanTags(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "accepted", Submitter: "Dolor",
	})
	d.SaveTags(100, 0, 0, "original", map[string]map[string]bool{
		"acked": {"A <a@ex>": true},
	})
	d.SaveTags(200, 0, 0, "original", map[string]map[string]bool{
		"acked": {"B <b@ex>": true},
	})

	m := d.GetTagsBatch(false, []string{"new"})
	if len(m[50]) != 1 {
		t.Errorf("active series tags: got %d, want 1", len(m[50]))
	}
	if len(m[51]) != 0 {
		t.Errorf("filtered series should have no tags: got %d",
			len(m[51]))
	}
}

func TestGetTagsBatch_MultipleSeries(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-11", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "new", Submitter: "Dolor",
	})
	d.SaveTags(100, 0, 0, "comment", map[string]map[string]bool{
		"acked": {"Same <same@ex>": true},
	})
	d.SaveTags(200, 0, 0, "comment", map[string]map[string]bool{
		"acked": {"Same <same@ex>": true},
	})

	m := d.GetTagsBatch(true, nil)
	if len(m[50]) != 1 || len(m[51]) != 1 {
		t.Errorf("same identity, different series: s50=%d s51=%d",
			len(m[50]), len(m[51]))
	}
}

func TestGetCommentCountsBatch_CoverOnly(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10",
	})
	d.InsertComment(CommentRow{
		ID: 500, CoverID: 99, Submitter: "Dolor",
		Date: "2026-03-11", Subject: "Re: c1", Content: "ok",
	})

	m := d.GetCommentCountsBatch(false, []string{"new"})
	if m[50] != 1 {
		t.Errorf("cover-only comments: got %d, want 1", m[50])
	}
}

func TestGetCommentCountsBatch_NoComments(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	m := d.GetCommentCountsBatch(false, []string{"new"})
	if m[50] != 0 {
		t.Errorf("no comments: got %d, want 0", m[50])
	}
}

func TestBatchConsistency(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "active", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "terminal", "2026-03-11", 1)
	d.SaveSeriesSummary(52, "mixed", "2026-03-12", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-11", State: "accepted", Submitter: "Dolor",
	})
	d.SavePatch(PatchRow{
		ID: 300, SeriesID: 52, Name: "p3",
		Date: "2026-03-12", State: "new", Submitter: "Sit",
	})
	d.SavePatch(PatchRow{
		ID: 301, SeriesID: 52, Name: "p4",
		Date: "2026-03-12", State: "accepted", Submitter: "Sit",
	})

	states := []string{"new", "under-review"}
	patches := d.GetAllPatchesBatch(false, states)
	tags := d.GetTagsBatch(false, states)
	comments := d.GetCommentCountsBatch(false, states)

	// Series 50 (active) and 52 (mixed) should be present
	// Series 51 (terminal) should not
	if _, ok := patches[50]; !ok {
		t.Error("active series 50 missing from patches")
	}
	if _, ok := patches[51]; ok {
		t.Error("terminal series 51 should not be in patches")
	}
	if _, ok := patches[52]; !ok {
		t.Error("mixed series 52 missing from patches")
	}

	// Tags and comments should cover the same series set
	// (even if empty for some)
	_ = tags
	_ = comments
}

func TestGetAllPatchesBatch_EmptyStates(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})

	m := d.GetAllPatchesBatch(false, []string{})
	if len(m) != 0 {
		t.Errorf("empty states should return nothing: got %d",
			len(m))
	}
}

func TestGetPatchesNeedingComments_Order(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "old-active",
		Date: "2026-03-01", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "new-terminal",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 300, SeriesID: 52, Name: "new-active",
		Date: "2026-03-15", State: "new", Submitter: "Lorem",
	})

	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 3 {
		t.Fatalf("len = %d, want 3", len(ids))
	}
	// Active newest first, then terminal
	if ids[0].ID != 300 {
		t.Errorf("[0] = %d, want 300 (newest active)", ids[0].ID)
	}
	if ids[1].ID != 100 {
		t.Errorf("[1] = %d, want 100 (older active)", ids[1].ID)
	}
	if ids[2].ID != 200 {
		t.Errorf("[2] = %d, want 200 (terminal)", ids[2].ID)
	}
}

func TestGetCoversNeedingComments_Order(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-01", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-10", 1)
	d.SaveSeriesSummary(52, "s3", "2026-03-15", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-01", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 300, SeriesID: 52, Name: "p3",
		Date: "2026-03-15", State: "new", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 90, SeriesID: 50, Name: "c1", Date: "2026-03-01",
	})
	d.SaveCover(CoverRow{
		ID: 91, SeriesID: 51, Name: "c2", Date: "2026-03-10",
	})
	d.SaveCover(CoverRow{
		ID: 92, SeriesID: 52, Name: "c3", Date: "2026-03-15",
	})
	d.RecomputeAllActiveFlags()

	ids := d.GetCoversNeedingComments(100)
	if len(ids) != 3 {
		t.Fatalf("len = %d, want 3", len(ids))
	}
	// Active series covers first (newest), then terminal
	if ids[0].ID != 92 {
		t.Errorf("[0] = %d, want 92 (newest active)", ids[0].ID)
	}
	if ids[1].ID != 90 {
		t.Errorf("[1] = %d, want 90 (older active)", ids[1].ID)
	}
	if ids[2].ID != 91 {
		t.Errorf("[2] = %d, want 91 (terminal)", ids[2].ID)
	}
}

func TestGetPatchesNeedingDetail_Order(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "old-active",
		Date: "2026-03-01", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "terminal",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 300, SeriesID: 52, Name: "new-active",
		Date: "2026-03-15", State: "under-review",
		Submitter: "Lorem",
	})

	ids := d.GetPatchesNeedingDetail(100)
	if len(ids) != 3 {
		t.Fatalf("len = %d, want 3", len(ids))
	}
	if ids[0].ID != 300 {
		t.Errorf("[0] = %d, want 300 (newest active)", ids[0].ID)
	}
	if ids[1].ID != 100 {
		t.Errorf("[1] = %d, want 100 (older active)", ids[1].ID)
	}
	if ids[2].ID != 200 {
		t.Errorf("[2] = %d, want 200 (terminal)", ids[2].ID)
	}
}

func TestGetCoversNeedingDetail_Order(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-01", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-01", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 200, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{
		ID: 90, SeriesID: 50, Name: "c1", Date: "2026-03-01",
	})
	d.SaveCover(CoverRow{
		ID: 91, SeriesID: 51, Name: "c2", Date: "2026-03-10",
	})
	d.RecomputeAllActiveFlags()

	ids := d.GetCoversNeedingDetail(100)
	if len(ids) != 2 {
		t.Fatalf("len = %d, want 2", len(ids))
	}
	if ids[0].ID != 90 {
		t.Errorf("[0] = %d, want 90 (active series)", ids[0].ID)
	}
	if ids[1].ID != 91 {
		t.Errorf("[1] = %d, want 91 (terminal series)", ids[1].ID)
	}
}

func TestBatchMethods_Empty(t *testing.T) {
	d := openTestDB(t)
	p := d.GetAllPatchesBatch(true, nil)
	if len(p) != 0 {
		t.Errorf("patches: %d, want 0", len(p))
	}
	tags := d.GetTagsBatch(true, nil)
	if len(tags) != 0 {
		t.Errorf("tags: %d, want 0", len(tags))
	}
	counts := d.GetCommentCountsBatch(true, nil)
	if len(counts) != 0 {
		t.Errorf("counts: %d, want 0", len(counts))
	}
}

func TestGetPatchesNeedingDetail(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2", Date: "2026-03-11",
		State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchDetail(100, "body", "diff", "{}", "[]")

	ids := d.GetPatchesNeedingDetail(100)
	if len(ids) != 1 || ids[0].ID != 101 {
		t.Errorf("got %v, want [101]", ids)
	}
}

func TestGetPatchesNeedingDetail_Limit(t *testing.T) {
	d := openTestDB(t)
	for i := 100; i < 110; i++ {
		d.SavePatch(PatchRow{
			ID: i, Name: fmt.Sprintf("p%d", i), Date: "2026-03-10",
			State: "new", Submitter: "Lorem",
		})
	}
	refs := d.GetPatchesNeedingDetail(3)
	if len(refs) != 3 {
		t.Errorf("got %d refs, want 3", len(refs))
	}
}

func TestGetPatchesNeedingDetail_ActiveFirst(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "terminal", Date: "2026-03-10",
		State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "active", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	refs := d.GetPatchesNeedingDetail(100)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	if refs[0].ID != 101 || !refs[0].IsActive {
		t.Errorf("first should be active 101, got id=%d active=%v",
			refs[0].ID, refs[0].IsActive)
	}
	if refs[1].ID != 100 || refs[1].IsActive {
		t.Errorf("second should be terminal 100, got id=%d active=%v",
			refs[1].ID, refs[1].IsActive)
	}
}

func TestGetPatchesNeedingDetail_ArchivedNotActive(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "archived new", Date: "2026-03-10",
		State: "new", Submitter: "Lorem", Archived: true,
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "non-archived new", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	refs := d.GetPatchesNeedingDetail(100)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	if refs[0].ID != 101 || !refs[0].IsActive {
		t.Errorf("first should be non-archived active, got id=%d active=%v",
			refs[0].ID, refs[0].IsActive)
	}
	if refs[1].ID != 100 || refs[1].IsActive {
		t.Errorf("archived should not be active, got id=%d active=%v",
			refs[1].ID, refs[1].IsActive)
	}
}

func TestGetCoversNeedingDetail(t *testing.T) {
	d := openTestDB(t)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "cover1", Date: "2026-03-10",
	})
	d.SaveCover(CoverRow{
		ID: 100, SeriesID: 51,
		Name: "cover2", Date: "2026-03-11",
	})
	d.UpdateCoverDetail(99, "body", "{}")

	ids := d.GetCoversNeedingDetail(100)
	if len(ids) != 1 || ids[0].ID != 100 {
		t.Errorf("got %v, want [100]", ids)
	}
}

func TestGetAllCoverNames(t *testing.T) {
	d := openTestDB(t)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[lorem,0/3] Dolor sit amet",
		Date: "2026-03-10",
	})
	d.SaveCover(CoverRow{
		ID: 100, SeriesID: 51,
		Name: "[lorem,0/2] Consectetur adipiscing",
		Date: "2026-03-11",
	})

	names := d.GetAllCoverNames()
	if len(names) != 2 {
		t.Fatalf("len = %d, want 2", len(names))
	}
	if names[99] != "[lorem,0/3] Dolor sit amet" {
		t.Errorf("[99] = %q", names[99])
	}
	if names[100] != "[lorem,0/2] Consectetur adipiscing" {
		t.Errorf("[100] = %q", names[100])
	}
}

func TestGetDelegateDisplayNames(t *testing.T) {
	d := openTestDB(t)

	d.SaveMaintainers([]MaintainerRow{
		{ID: 1, Username: "lorem",
			FirstName: "Lorem", LastName: "Ipsum"},
		{ID: 2, Username: "dolor",
			FirstName: "dolor", LastName: "Amet"},
		{ID: 3, Username: "noname",
			FirstName: "", LastName: "Sit"},
	})

	names := d.GetDelegateDisplayNames()
	if names["lorem"] != "Lorem" {
		t.Errorf("lorem = %q", names["lorem"])
	}
	if names["dolor"] != "dolor" {
		t.Errorf("dolor = %q", names["dolor"])
	}
	if _, ok := names["noname"]; ok {
		t.Error("noname should not be in map")
	}
}

func TestGetActiveSeries(t *testing.T) {
	d := openTestDB(t)

	d.SaveSeriesSummary(50, "[PATCH] Tempor incididunt", "2026-03-10T12:00:00", 1)
	d.SaveSeriesSummary(51, "[PATCH] Labore et dolore", "2026-03-09T12:00:00", 1)
	d.SaveSeriesSummary(52, "[PATCH] Magna aliqua ut", "2026-03-08T12:00:00", 1)

	d.SavePatch(PatchRow{
		ID: 100, Name: "p1",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
		SeriesID:  50,
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2",
		Date: "2026-03-10T12:00:00", State: "under-review",
		Submitter: "Lorem",
		SeriesID:  50,
	})
	d.SavePatch(PatchRow{
		ID: 102, Name: "p3",
		Date: "2026-03-09T12:00:00", State: "accepted",
		Submitter: "Lorem",
		SeriesID:  51,
	})

	rows := d.GetActiveSeries([]string{"new", "under-review"})
	if len(rows) != 1 {
		t.Fatalf("got %d series, want 1", len(rows))
	}
	if rows[0].ID != 50 {
		t.Errorf("ID = %d, want 50", rows[0].ID)
	}

	rows = d.GetActiveSeries([]string{"accepted"})
	if len(rows) != 1 || rows[0].ID != 51 {
		t.Errorf("accepted series = %v", rows)
	}
}

func TestGetPatchesForSeries(t *testing.T) {
	d := openTestDB(t)

	d.SaveSeriesSummary(50, "[PATCH] Quis nostrud exercitation", "2026-03-10T12:00:00", 1)
	d.SavePatch(PatchRow{
		ID: 100, Name: "[PATCH 1/2] Duis aute irure",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
		SeriesID:  50,
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "[PATCH 2/2] Excepteur sint occaecat",
		Date: "2026-03-10T12:01:00", State: "new",
		Submitter: "Lorem",
		SeriesID:  50,
	})

	rows := d.GetPatchesForSeries(50)
	if len(rows) != 2 {
		t.Fatalf("got %d patches", len(rows))
	}
	if rows[0].ID != 100 {
		t.Errorf("[0].ID = %d, want 100 (ordered by date)", rows[0].ID)
	}
	if rows[1].ID != 101 {
		t.Errorf("[1].ID = %d", rows[1].ID)
	}
}

func TestGetOldestPatchDate(t *testing.T) {
	d := openTestDB(t)

	if d.GetOldestPatchDate() != "" {
		t.Error("want empty for no patches")
	}

	d.SavePatch(PatchRow{
		ID: 100, Name: "newer",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "older",
		Date: "2026-01-05T08:00:00", State: "new",
		Submitter: "Lorem",
	})

	if v := d.GetOldestPatchDate(); v != "2026-01-05T08:00:00" {
		t.Errorf("got %q", v)
	}
}

func TestGetChecksForPatch(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1",
		Date: "2026-03-10", State: "new",
		Submitter: "Lorem",
	})
	d.SaveCheck(CheckRow{
		ID: 1, PatchID: 100,
		Context: "ci/build", State: "success",
		TargetURL: "https://ci.example.com/1",
	})
	d.SaveCheck(CheckRow{
		ID: 2, PatchID: 100,
		Context: "ci/test", State: "fail",
		TargetURL: "https://ci.example.com/2",
	})
	d.SaveCheck(CheckRow{
		ID: 3, PatchID: 200,
		Context: "ci/other", State: "success",
	})

	checks := d.GetChecksForPatch(100)
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(checks))
	}
	if checks[0].Context != "ci/build" {
		t.Errorf("[0].Context = %q", checks[0].Context)
	}
	if checks[1].State != "fail" {
		t.Errorf("[1].State = %q", checks[1].State)
	}
}

func TestRecountPatchChecks(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	d.SaveCheck(CheckRow{
		ID: 1, PatchID: 100,
		State: "success", Context: "ci/build",
	})
	d.SaveCheck(CheckRow{
		ID: 2, PatchID: 100,
		State: "success", Context: "ci/lint",
	})
	d.SaveCheck(CheckRow{
		ID: 3, PatchID: 100,
		State: "fail", Context: "ci/test",
	})
	d.SaveCheck(CheckRow{
		ID: 4, PatchID: 100,
		State: "pending", Context: "ci/deploy",
	})
	d.SaveCheck(CheckRow{
		ID: 5, PatchID: 100,
		State: "warning", Context: "ci/style",
	})

	d.RecountPatchChecks(100)

	row, _ := d.GetPatch(100)
	if row.ChecksPass != 2 {
		t.Errorf("pass = %d, want 2", row.ChecksPass)
	}
	if row.ChecksFail != 1 {
		t.Errorf("fail = %d, want 1", row.ChecksFail)
	}
	// Only warning counts — pending checks are excluded
	if row.ChecksWarn != 1 {
		t.Errorf("warn = %d, want 1", row.ChecksWarn)
	}
}

func TestRecountPatchChecks_WarnExcludesPending(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Only pending checks — no warnings
	d.SaveCheck(CheckRow{
		ID: 1, PatchID: 100,
		State: "pending", Context: "ci/build",
	})
	d.SaveCheck(CheckRow{
		ID: 2, PatchID: 100,
		State: "pending", Context: "ci/test",
	})
	d.RecountPatchChecks(100)
	row, _ := d.GetPatch(100)
	if row.ChecksWarn != 0 {
		t.Errorf("warn = %d, want 0 (pending should not count)", row.ChecksWarn)
	}
	if row.ChecksPass != 0 {
		t.Errorf("pass = %d, want 0", row.ChecksPass)
	}
}

func TestRecountPatchChecks_AllStates(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SaveCheck(CheckRow{ID: 1, PatchID: 100, State: "success", Context: "ci/a"})
	d.SaveCheck(CheckRow{ID: 2, PatchID: 100, State: "fail", Context: "ci/b"})
	d.SaveCheck(CheckRow{ID: 3, PatchID: 100, State: "warning", Context: "ci/c"})
	d.SaveCheck(CheckRow{ID: 4, PatchID: 100, State: "warning", Context: "ci/d"})
	d.SaveCheck(CheckRow{ID: 5, PatchID: 100, State: "pending", Context: "ci/e"})
	d.RecountPatchChecks(100)
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 1 {
		t.Errorf("pass = %d, want 1", row.ChecksPass)
	}
	if row.ChecksFail != 1 {
		t.Errorf("fail = %d, want 1", row.ChecksFail)
	}
	if row.ChecksWarn != 2 {
		t.Errorf("warn = %d, want 2 (only warnings, not pending)",
			row.ChecksWarn)
	}
}

func TestUpdatePatchChecks_WarnField(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.UpdatePatchChecks(100, 5, 2, 3)
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 5 || row.ChecksFail != 2 || row.ChecksWarn != 3 {
		t.Errorf("checks = %d/%d/%d, want 5/2/3",
			row.ChecksPass, row.ChecksFail, row.ChecksWarn)
	}
}

func TestRecountPatchChecks_LatestPerContext(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// ai-review: pending then success (two records, same context)
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "pending",
		Context: "ai-review", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 20, PatchID: 100, State: "success",
		Context: "ai-review", Date: "2026-03-10T10:05:00",
	})
	d.RecountPatchChecks(100)
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 1 {
		t.Errorf("pass = %d, want 1 (latest ai-review is success)",
			row.ChecksPass)
	}
	if row.ChecksWarn != 0 {
		t.Errorf("warn = %d, want 0", row.ChecksWarn)
	}
}

func TestRecountPatchChecks_MultipleContextsWithSuperseded(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// build: pending → success
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "pending",
		Context: "build", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 20, PatchID: 100, State: "success",
		Context: "build", Date: "2026-03-10T10:05:00",
	})
	// lint: pending → warning
	d.SaveCheck(CheckRow{
		ID: 30, PatchID: 100, State: "pending",
		Context: "lint", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 40, PatchID: 100, State: "warning",
		Context: "lint", Date: "2026-03-10T10:05:00",
	})
	// test: pending → fail
	d.SaveCheck(CheckRow{
		ID: 50, PatchID: 100, State: "pending",
		Context: "test", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 60, PatchID: 100, State: "fail",
		Context: "test", Date: "2026-03-10T10:05:00",
	})
	d.RecountPatchChecks(100)
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 1 {
		t.Errorf("pass = %d, want 1 (build)", row.ChecksPass)
	}
	if row.ChecksFail != 1 {
		t.Errorf("fail = %d, want 1 (test)", row.ChecksFail)
	}
	if row.ChecksWarn != 1 {
		t.Errorf("warn = %d, want 1 (lint)", row.ChecksWarn)
	}
}

func TestGetChecksForPatch_LatestPerContext(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// ai-review: pending → success
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "pending",
		Context: "ai-review", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 20, PatchID: 100, State: "success",
		Context: "ai-review", Date: "2026-03-10T10:05:00",
	})
	// build: only one record
	d.SaveCheck(CheckRow{
		ID: 30, PatchID: 100, State: "success",
		Context: "build", Date: "2026-03-10T10:05:00",
	})

	checks := d.GetChecksForPatch(100)
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2 (one per context)",
			len(checks))
	}
	// Ordered by context: ai-review, build
	if checks[0].Context != "ai-review" || checks[0].State != "success" {
		t.Errorf("check 0: context=%q state=%q, want ai-review/success",
			checks[0].Context, checks[0].State)
	}
	if checks[1].Context != "build" || checks[1].State != "success" {
		t.Errorf("check 1: context=%q state=%q, want build/success",
			checks[1].Context, checks[1].State)
	}
}

func TestGetChecksForPatch_PendingNotSuperseded(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Only a pending check — no superseding record yet
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "pending",
		Context: "ai-review", Date: "2026-03-10T10:00:00",
	})
	checks := d.GetChecksForPatch(100)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1", len(checks))
	}
	if checks[0].State != "pending" {
		t.Errorf("state = %q, want pending", checks[0].State)
	}
}

func TestRecountChecks_Batch_LatestPerContext(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// build: pending → success (two records)
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "pending",
		Context: "build", Date: "2026-03-10T10:00:00",
	})
	d.SaveCheck(CheckRow{
		ID: 20, PatchID: 100, State: "success",
		Context: "build", Date: "2026-03-10T10:05:00",
	})
	// Set wrong counters, then run the batch recount SQL
	d.UpdatePatchChecks(100, 0, 0, 0)
	row, _ := d.GetPatch(100)
	if row.ChecksPass != 0 {
		t.Fatalf("precondition: pass should be 0")
	}
	// Run the same batch recount that runs on startup
	d.RunRecountChecks()
	row, _ = d.GetPatch(100)
	if row.ChecksPass != 1 {
		t.Errorf("pass = %d after batch recount, want 1",
			row.ChecksPass)
	}
}

func TestGetPatchesNeedingChecks(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "active",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51, Name: "terminal",
		Date: "2026-03-10", State: "accepted", Submitter: "Ipsum",
	})
	d.SavePatch(PatchRow{
		ID: 102, SeriesID: 52, Name: "already fetched",
		Date: "2026-03-10", State: "new", Submitter: "Dolor",
	})
	d.MarkChecksFetched(102)

	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2 (102 already fetched)", len(refs))
	}
	// Active first
	if refs[0].ID != 100 || !refs[0].IsActive {
		t.Errorf("refs[0] = {ID:%d IsActive:%v}, want {100 true}",
			refs[0].ID, refs[0].IsActive)
	}
	if refs[1].ID != 101 || refs[1].IsActive {
		t.Errorf("refs[1] = {ID:%d IsActive:%v}, want {101 false}",
			refs[1].ID, refs[1].IsActive)
	}
}

func TestMarkChecksFetched(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 1 {
		t.Fatalf("before: got %d refs, want 1", len(refs))
	}
	d.MarkChecksFetched(100)
	refs = d.GetPatchesNeedingChecks(100)
	if len(refs) != 0 {
		t.Errorf("after: got %d refs, want 0", len(refs))
	}
}

func TestResetChecksFetched(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.MarkChecksFetched(100)
	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 0 {
		t.Fatal("should not need checks after marking")
	}
	d.ResetChecksFetched(100)
	refs = d.GetPatchesNeedingChecks(100)
	if len(refs) != 1 {
		t.Errorf("got %d refs after reset, want 1", len(refs))
	}
}

func TestStartupReset_ChecksWithoutDescriptions(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Check without description
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "success",
		Context: "ci/build", Date: "2026-03-10",
	})
	d.MarkChecksFetched(100)

	// Run the startup reset
	d.RunResetChecksWithoutDescriptions()

	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 1 {
		t.Errorf("got %d refs, want 1 (reset because no description)",
			len(refs))
	}
}

func TestStartupReset_ChecksWithDescriptions_NoReset(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Check with description
	d.SaveCheck(CheckRow{
		ID: 10, PatchID: 100, State: "success",
		Context: "ci/build", Description: "All passed",
		Date: "2026-03-10",
	})
	d.MarkChecksFetched(100)

	// Run the startup reset — should NOT reset
	d.RunResetChecksWithoutDescriptions()

	refs := d.GetPatchesNeedingChecks(100)
	if len(refs) != 0 {
		t.Errorf("got %d refs, want 0 (has description, no reset)",
			len(refs))
	}
}

func TestNeedsPatchDetail(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	if !d.NeedsPatchDetail(100) {
		t.Error("should need detail initially")
	}
	d.UpdatePatchDetail(100, "body", "---", "{}", "[]")
	if d.NeedsPatchDetail(100) {
		t.Error("should not need detail after update")
	}
}

func TestNeedsCoverDetail(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "series", "2026-03-10", 1)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50, Name: "cover",
		Date: "2026-03-10",
	})
	if !d.NeedsCoverDetail(99) {
		t.Error("should need detail initially")
	}
	d.UpdateCoverDetail(99, "body", "{}")
	if d.NeedsCoverDetail(99) {
		t.Error("should not need detail after update")
	}
}

func TestNeedsPatchChecks(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	if !d.NeedsPatchChecks(100) {
		t.Error("should need checks initially")
	}
	d.MarkChecksFetched(100)
	if d.NeedsPatchChecks(100) {
		t.Error("should not need checks after marking")
	}
	d.ResetChecksFetched(100)
	if !d.NeedsPatchChecks(100) {
		t.Error("should need checks after reset")
	}
}

func TestGetPatchesNeedingComments(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 2 {
		t.Fatalf("got %d, want 2", len(ids))
	}

	d.MarkCommentsFetched(100)

	ids = d.GetPatchesNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 101 {
		t.Errorf("got %v, want [101]", ids)
	}

	d.MarkCommentsFetched(101)

	ids = d.GetPatchesNeedingComments(100)
	if len(ids) != 0 {
		t.Errorf("got %v, want empty", ids)
	}
}

func TestGetPatchesNeedingComments_Priority(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 102, Name: "p3", Date: "2026-03-10",
		State: "under-review", Submitter: "Lorem",
	})

	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 3 {
		t.Fatalf("got %d, want 3", len(ids))
	}
	if ids[0].ID != 102 {
		t.Errorf("[0] = %d, want 102 (newest active)", ids[0].ID)
	}
	if ids[1].ID != 101 {
		t.Errorf("[1] = %d, want 101 (older active)", ids[1].ID)
	}
	if ids[2].ID != 100 {
		t.Errorf("[2] = %d, want 100 (terminal, last)",
			ids[2].ID)
	}
}

func TestResetCommentsFetched(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.MarkCommentsFetched(100)

	ids := d.GetPatchesNeedingComments(100)
	if len(ids) != 0 {
		t.Fatalf("should be empty after mark")
	}

	d.ResetCommentsFetched(100)

	ids = d.GetPatchesNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 100 {
		t.Errorf("got %v, want [100] after reset", ids)
	}
}

func TestGetCommentsForCover(t *testing.T) {
	d := openTestDB(t)

	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "[PATCH 0/3] Lorem ipsum",
		Date: "2026-03-10T12:00:00",
	})

	d.InsertComment(CommentRow{
		ID: 500, CoverID: 99, Submitter: "Dolor Amet",
		Date: "2026-03-11T09:00:00", Subject: "Re: Lorem",
		Content: "Acked-by: Dolor Amet <dolor@amet.example>",
	})
	d.InsertComment(CommentRow{
		ID: 501, CoverID: 99, Submitter: "Sit Amet",
		Date: "2026-03-11T10:00:00", Subject: "Re: Lorem",
		Content: "Looks good to me.",
	})
	// Comment on a patch, not on the cover
	d.InsertComment(CommentRow{
		ID: 502, PatchID: 100, Submitter: "Tempor",
		Date: "2026-03-11T11:00:00", Subject: "Re: patch",
		Content: "Unrelated patch comment.",
	})

	comments := d.GetCommentsForCover(99)
	if len(comments) != 2 {
		t.Fatalf("len(comments) = %d, want 2", len(comments))
	}
	if comments[0].ID != 500 {
		t.Errorf("[0].ID = %d", comments[0].ID)
	}
	if comments[1].CoverID != 99 {
		t.Errorf("[1].CoverID = %d", comments[1].CoverID)
	}
}

func TestGetCoversNeedingComments(t *testing.T) {
	d := openTestDB(t)

	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Cover A", Date: "2026-03-10",
	})
	d.SaveCover(CoverRow{
		ID: 100, SeriesID: 51,
		Name: "Cover B", Date: "2026-03-11",
	})

	ids := d.GetCoversNeedingComments(100)
	if len(ids) != 2 {
		t.Fatalf("len = %d, want 2", len(ids))
	}

	d.MarkCoverCommentsFetched(99)
	ids = d.GetCoversNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 100 {
		t.Errorf("after mark: ids = %v", ids)
	}

	d.MarkCoverCommentsFetched(100)
	ids = d.GetCoversNeedingComments(100)
	if len(ids) != 0 {
		t.Errorf("after mark all: ids = %v", ids)
	}
}

func TestResetCoverCommentsFetched(t *testing.T) {
	d := openTestDB(t)

	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Cover", Date: "2026-03-10",
	})

	d.MarkCoverCommentsFetched(99)
	ids := d.GetCoversNeedingComments(100)
	if len(ids) != 0 {
		t.Fatalf("after mark: len = %d", len(ids))
	}

	d.ResetCoverCommentsFetched(99)
	ids = d.GetCoversNeedingComments(100)
	if len(ids) != 1 || ids[0].ID != 99 {
		t.Errorf("after reset: ids = %v", ids)
	}
}

func TestNeedsPatchComments(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	if !d.NeedsPatchComments(100) {
		t.Error("new patch should need comments")
	}

	d.MarkCommentsFetched(100)
	if d.NeedsPatchComments(100) {
		t.Error("marked patch should not need comments")
	}

	if d.NeedsPatchComments(999) {
		t.Error("non-existent patch should return false")
	}
}

func TestNeedsCoverComments(t *testing.T) {
	d := openTestDB(t)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Cover", Date: "2026-03-10",
	})

	if !d.NeedsCoverComments(99) {
		t.Error("new cover should need comments")
	}

	d.MarkCoverCommentsFetched(99)
	if d.NeedsCoverComments(99) {
		t.Error("marked cover should not need comments")
	}

	if d.NeedsCoverComments(999) {
		t.Error("non-existent cover should return false")
	}
}

func TestUpdateSeriesPatches(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50,
		Name: "p2", Date: "2026-03-10",
		State: "new", Submitter: "",
	})
	d.SavePatch(PatchRow{
		ID: 102, SeriesID: 51,
		Name: "p3", Date: "2026-03-10",
		State: "new", Submitter: "",
	})

	d.UpdateSeriesPatches(50, "Lorem Ipsum", "lorem@ipsum.example")

	r1, _ := d.GetPatch(100)
	if r1.Submitter != "Lorem Ipsum" {
		t.Errorf("patch 100 submitter = %q", r1.Submitter)
	}
	r2, _ := d.GetPatch(101)
	if r2.Submitter != "Lorem Ipsum" {
		t.Errorf("patch 101 submitter = %q", r2.Submitter)
	}
	r3, _ := d.GetPatch(102)
	if r3.Submitter != "" {
		t.Errorf("patch 102 (different series) submitter = %q, want empty",
			r3.Submitter)
	}
}

func TestGetSeriesNeedingDetail(t *testing.T) {
	d := openTestDB(t)

	// No series — empty result
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) != 0 {
		t.Errorf("want empty, got %d", len(refs))
	}

	// SaveSeriesSummary creates series with detail_fetched=0
	d.SaveSeriesSummary(50, "Lorem series", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "Dolor series", "2026-03-09", 1)

	refs = d.GetSeriesNeedingDetail(100)
	if len(refs) != 2 {
		t.Fatalf("want 2, got %d", len(refs))
	}

	// SaveSeries sets detail_fetched=1
	d.SaveSeries(SeriesRow{
		ID: 50, Name: "Lorem series", Date: "2026-03-10",
		Version: 1, Submitter: "Lorem", TotalPatches: 1,
	})
	refs = d.GetSeriesNeedingDetail(100)
	if len(refs) != 1 || refs[0].ID != 51 {
		t.Errorf("want [51], got %v", refs)
	}
}

func TestRecomputeActiveFlag_ActivePatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.RecomputeActiveFlag(50)
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) == 0 || !refs[0].IsActive {
		t.Error("series should be active")
	}
	crefs := d.GetCoversNeedingDetail(100)
	if len(crefs) == 0 || !crefs[0].IsActive {
		t.Error("cover should be active")
	}
}

func TestRecomputeActiveFlag_TerminalPatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.RecomputeActiveFlag(50)
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) == 0 || refs[0].IsActive {
		t.Error("series should not be active")
	}
}

func TestRecomputeActiveFlag_ArchivedActivePatch(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
		Archived: true,
	})
	d.RecomputeActiveFlag(50)
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) == 0 || refs[0].IsActive {
		t.Error("archived active patch should not set flag")
	}
}

func TestRecomputeActiveFlag_MixedPatches(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 50, Name: "p2",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.RecomputeActiveFlag(50)
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) == 0 || !refs[0].IsActive {
		t.Error("one active patch should be enough to set flag")
	}
}

func TestRecomputeActiveFlag_Transition(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.RecomputeActiveFlag(50)
	// Should be active
	refs := d.GetCoversNeedingDetail(100)
	if len(refs) == 0 || !refs[0].IsActive {
		t.Fatal("should start active")
	}
	// Transition to terminal
	d.UpdatePatchState(100, "accepted")
	d.RecomputeActiveFlag(50)
	refs = d.GetCoversNeedingDetail(100)
	if len(refs) == 0 || refs[0].IsActive {
		t.Error("should be terminal after state change")
	}
	// Transition back to active
	d.UpdatePatchState(100, "under-review")
	d.RecomputeActiveFlag(50)
	refs = d.GetCoversNeedingDetail(100)
	if len(refs) == 0 || !refs[0].IsActive {
		t.Error("should be active again")
	}
}

func TestRecomputeActiveFlag_DoesNotAffectOtherSeries(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "s2", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.RecomputeAllActiveFlags()
	// Recompute only series 50
	d.UpdatePatchState(100, "accepted")
	d.RecomputeActiveFlag(50)
	// Series 51 should be unchanged (still terminal)
	refs := d.GetSeriesNeedingDetail(100)
	for _, r := range refs {
		if r.ID == 51 && r.IsActive {
			t.Error("series 51 should not be affected")
		}
	}
}

func TestRecomputeActiveFlag_AllActiveStates(t *testing.T) {
	for _, state := range ActiveStates {
		t.Run(state, func(t *testing.T) {
			d := openTestDB(t)
			d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
			d.SavePatch(PatchRow{
				ID: 100, SeriesID: 50, Name: "p1",
				Date: "2026-03-10", State: state, Submitter: "Lorem",
			})
			d.RecomputeActiveFlag(50)
			refs := d.GetSeriesNeedingDetail(100)
			if len(refs) == 0 || !refs[0].IsActive {
				t.Errorf("state %q should be active", state)
			}
		})
	}
}

func TestRecomputeActiveFlag_TerminalStates(t *testing.T) {
	terminal := []string{
		"accepted", "rejected", "superseded", "rfc",
		"not-applicable", "changes-requested", "deferred",
		"handled-elsewhere",
	}
	for _, state := range terminal {
		t.Run(state, func(t *testing.T) {
			d := openTestDB(t)
			d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
			d.SavePatch(PatchRow{
				ID: 100, SeriesID: 50, Name: "p1",
				Date: "2026-03-10", State: state, Submitter: "Lorem",
			})
			d.RecomputeActiveFlag(50)
			refs := d.GetSeriesNeedingDetail(100)
			if len(refs) > 0 && refs[0].IsActive {
				t.Errorf("state %q should not be active", state)
			}
		})
	}
}

func TestRecomputeActiveFlag_NonexistentSeries(t *testing.T) {
	d := openTestDB(t)
	// Should not panic or error
	d.RecomputeActiveFlag(999999)
}

func TestRecomputeAllActiveFlags_Basic(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "active", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "terminal", "2026-03-10", 1)
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SaveCover(CoverRow{ID: 98, SeriesID: 51, Name: "c2", Date: "2026-03-10"})
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.RecomputeAllActiveFlags()

	srefs := d.GetSeriesNeedingDetail(100)
	if len(srefs) < 2 {
		t.Fatalf("want 2 series, got %d", len(srefs))
	}
	if srefs[0].ID != 50 || !srefs[0].IsActive {
		t.Errorf("first should be active series 50")
	}
	if srefs[1].ID != 51 || srefs[1].IsActive {
		t.Errorf("second should be terminal series 51")
	}
	crefs := d.GetCoversNeedingDetail(100)
	if len(crefs) < 2 {
		t.Fatalf("want 2 covers, got %d", len(crefs))
	}
	if crefs[0].ID != 99 || !crefs[0].IsActive {
		t.Errorf("first cover should be active")
	}
	if crefs[1].ID != 98 || crefs[1].IsActive {
		t.Errorf("second cover should be terminal")
	}
}

func TestRecomputeAllActiveFlags_ResetsStaleFlags(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "s1", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	// Manually set stale flag
	d.RecomputeActiveFlag(50) // should set to 0
	// Force it to 1 (stale)
	d.conn.Exec("UPDATE series SET has_active_patch = 1 WHERE id = 50")
	// Bulk recompute should fix it
	d.RecomputeAllActiveFlags()
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) > 0 && refs[0].IsActive {
		t.Error("stale flag should be corrected to 0")
	}
}

func TestGetCoversNeedingDetail_FlagBasedPriority(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "active", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "terminal", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SaveCover(CoverRow{ID: 98, SeriesID: 51, Name: "c2", Date: "2026-03-10"})
	d.RecomputeAllActiveFlags()

	refs := d.GetCoversNeedingDetail(1)
	if len(refs) != 1 || refs[0].ID != 99 || !refs[0].IsActive {
		t.Errorf("LIMIT 1 should return the active cover, got %v", refs)
	}
}

func TestGetCoversNeedingComments_FlagBasedPriority(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "active", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "terminal", "2026-03-10", 1)
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50, Name: "p1",
		Date: "2026-03-10", State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51, Name: "p2",
		Date: "2026-03-10", State: "accepted", Submitter: "Lorem",
	})
	d.SaveCover(CoverRow{ID: 99, SeriesID: 50, Name: "c1", Date: "2026-03-10"})
	d.SaveCover(CoverRow{ID: 98, SeriesID: 51, Name: "c2", Date: "2026-03-10"})
	d.RecomputeAllActiveFlags()

	refs := d.GetCoversNeedingComments(1)
	if len(refs) != 1 || refs[0].ID != 99 || !refs[0].IsActive {
		t.Errorf("LIMIT 1 should return the active cover, got %v", refs)
	}
}

func TestCountUnfetched(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	if n := d.CountUnfetched("patches", "detail_fetched"); n != 2 {
		t.Errorf("want 2 unfetched, got %d", n)
	}
	d.UpdatePatchDetail(100, "body", "diff", "{}", "[]")
	if n := d.CountUnfetched("patches", "detail_fetched"); n != 1 {
		t.Errorf("want 1 unfetched after fetch, got %d", n)
	}
	d.UpdatePatchDetail(101, "body", "diff", "{}", "[]")
	if n := d.CountUnfetched("patches", "detail_fetched"); n != 0 {
		t.Errorf("want 0 unfetched after all fetched, got %d", n)
	}
}

func TestGetSeriesNeedingDetail_ActiveFirst(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "Active series", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "Terminal series", "2026-03-09", 1)

	// Patch in active state for series 50
	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	// Patch in terminal state for series 51
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51,
		Name: "p2", Date: "2026-03-09",
		State: "accepted", Submitter: "Dolor",
	})
	d.RecomputeAllActiveFlags()

	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) != 2 {
		t.Fatalf("want 2, got %d", len(refs))
	}
	if refs[0].ID != 50 || !refs[0].IsActive {
		t.Errorf("first should be active series 50, got id=%d active=%v",
			refs[0].ID, refs[0].IsActive)
	}
	if refs[1].ID != 51 || refs[1].IsActive {
		t.Errorf("second should be terminal series 51, got id=%d active=%v",
			refs[1].ID, refs[1].IsActive)
	}
}

func TestSaveSeries_SetsDetailFetched(t *testing.T) {
	d := openTestDB(t)

	// SaveSeriesSummary — detail_fetched stays 0
	d.SaveSeriesSummary(50, "Lorem", "2026-03-10", 1)
	refs := d.GetSeriesNeedingDetail(100)
	if len(refs) != 1 {
		t.Fatalf("summary should leave detail_fetched=0, got %d needing", len(refs))
	}

	// SaveSeries — sets detail_fetched=1
	d.SaveSeries(SeriesRow{
		ID: 50, Name: "Lorem", Date: "2026-03-10",
		Version: 1, Submitter: "Lorem Ipsum", TotalPatches: 2,
	})
	refs = d.GetSeriesNeedingDetail(100)
	if len(refs) != 0 {
		t.Errorf("SaveSeries should set detail_fetched=1, got %d needing", len(refs))
	}
}

func TestGetAllSeries(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeriesSummary(50, "Active series", "2026-03-10", 1)
	d.SaveSeriesSummary(51, "Old series", "2026-01-01", 1)

	d.SavePatch(PatchRow{
		ID: 100, SeriesID: 50,
		Name: "p1", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, SeriesID: 51,
		Name: "p2", Date: "2026-01-01",
		State: "accepted", Submitter: "Lorem",
	})

	active := d.GetActiveSeries([]string{"new"})
	if len(active) != 1 {
		t.Errorf("active = %d, want 1", len(active))
	}

	all := d.GetAllSeries()
	if len(all) != 2 {
		t.Errorf("all = %d, want 2", len(all))
	}
}

func TestGetCover_BySeriesID(t *testing.T) {
	d := openTestDB(t)
	d.SaveCover(CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Lorem cover", Date: "2026-03-10",
	})
	d.SaveCover(CoverRow{
		ID: 100, SeriesID: 51,
		Name: "Dolor cover", Date: "2026-03-09",
	})

	cover, err := d.GetCover(50)
	if err != nil {
		t.Fatal(err)
	}
	if cover.ID != 99 {
		t.Errorf("ID = %d, want 99", cover.ID)
	}
	if cover.Name != "Lorem cover" {
		t.Errorf("Name = %q", cover.Name)
	}

	_, err = d.GetCover(999)
	if err == nil {
		t.Error("expected error for non-existent series")
	}
}
