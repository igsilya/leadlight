package db

import (
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

func TestUpdatePatchTags(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	if err := d.UpdatePatchTags(100, 2, 1, 3, 1); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.AckedBy != 2 {
		t.Errorf("AckedBy = %d", row.AckedBy)
	}
	if row.Fixes != 1 {
		t.Errorf("Fixes = %d", row.Fixes)
	}
	if row.ReviewedBy != 3 {
		t.Errorf("ReviewedBy = %d", row.ReviewedBy)
	}
	if row.TestedBy != 1 {
		t.Errorf("TestedBy = %d", row.TestedBy)
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
	if row.ChecksPass != 3 || row.ChecksFail != 1 || row.ChecksPending != 2 {
		t.Errorf("Checks = %d/%d/%d", row.ChecksPass, row.ChecksFail, row.ChecksPending)
	}
}

func TestUpdatePatchMbox(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "test",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	mbox := "From: Lorem <lorem@ipsum.example>\nSubject: [PATCH] Lorem ipsum\n\nlorem ipsum content"
	if err := d.UpdatePatchMbox(100, mbox); err != nil {
		t.Fatal(err)
	}

	row, _ := d.GetPatch(100)
	if row.MboxContent != mbox {
		t.Errorf("MboxContent = %q", row.MboxContent)
	}
}

func TestInsertCheck(t *testing.T) {
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
	if err := d.InsertCheck(check); err != nil {
		t.Fatal(err)
	}

	// Insert same check again — should not error (idempotent)
	if err := d.InsertCheck(check); err != nil {
		t.Fatal(err)
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

func TestGetPatchesNeedingDetail(t *testing.T) {
	d := openTestDB(t)

	d.SavePatch(PatchRow{
		ID: 100, Name: "p1",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})
	d.SavePatch(PatchRow{
		ID: 101, Name: "p2",
		Date: "2026-03-10T12:00:00", State: "new",
		Submitter: "Lorem",
	})

	ids := d.GetPatchesNeedingDetail()
	if len(ids) != 2 {
		t.Fatalf("got %d, want 2", len(ids))
	}

	// Mark one as fetched
	d.UpdatePatchDetail(100, "c", "d", "{}", "[]")

	ids = d.GetPatchesNeedingDetail()
	if len(ids) != 1 || ids[0] != 101 {
		t.Errorf("got %v, want [101]", ids)
	}
}

func TestRecountPatchChecks(t *testing.T) {
	d := openTestDB(t)
	d.SavePatch(PatchRow{
		ID: 100, Name: "test", Date: "2026-03-10",
		State: "new", Submitter: "Lorem",
	})

	d.InsertCheck(CheckRow{
		ID: 1, PatchID: 100,
		State: "success", Context: "ci/build",
	})
	d.InsertCheck(CheckRow{
		ID: 2, PatchID: 100,
		State: "success", Context: "ci/lint",
	})
	d.InsertCheck(CheckRow{
		ID: 3, PatchID: 100,
		State: "fail", Context: "ci/test",
	})
	d.InsertCheck(CheckRow{
		ID: 4, PatchID: 100,
		State: "pending", Context: "ci/deploy",
	})
	d.InsertCheck(CheckRow{
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
	// pending + warning = 2
	if row.ChecksPending != 2 {
		t.Errorf("pending = %d, want 2", row.ChecksPending)
	}
}
