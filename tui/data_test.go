package tui

import (
	"testing"
	"time"

	"leadlight/db"
)

func TestFormatAge_Minutes(t *testing.T) {
	d := time.Now().Add(-5 * time.Minute)
	got := formatAge(d.Format("2006-01-02T15:04:05"))
	if got != "5m" {
		t.Errorf("got %q, want 5m", got)
	}
}

func TestFormatAge_Hours(t *testing.T) {
	d := time.Now().Add(-3 * time.Hour)
	got := formatAge(d.Format("2006-01-02T15:04:05"))
	if got != "3h" {
		t.Errorf("got %q, want 3h", got)
	}
}

func TestFormatAge_Days(t *testing.T) {
	d := time.Now().Add(-2 * 24 * time.Hour)
	got := formatAge(d.Format("2006-01-02T15:04:05"))
	if got != "2d" {
		t.Errorf("got %q, want 2d", got)
	}
}

func TestFormatAge_Weeks(t *testing.T) {
	d := time.Now().Add(-21 * 24 * time.Hour)
	got := formatAge(d.Format("2006-01-02T15:04:05"))
	if got != "3w" {
		t.Errorf("got %q, want 3w", got)
	}
}

func TestFormatAge_Months(t *testing.T) {
	d := time.Now().Add(-65 * 24 * time.Hour)
	got := formatAge(d.Format("2006-01-02T15:04:05"))
	if got != "2mo" {
		t.Errorf("got %q, want 2mo", got)
	}
}

func TestFormatAge_Invalid(t *testing.T) {
	got := formatAge("not-a-date")
	if got != "?" {
		t.Errorf("got %q, want ?", got)
	}
}

func TestFormatReviews(t *testing.T) {
	got := formatPatchReviews(db.PatchRow{
		AckedBy: 2, Fixes: 1, ReviewedBy: 3, TestedBy: 0,
	})
	if got != "2/1/3/0" {
		t.Errorf("got %q, want 2/1/3/0", got)
	}
}

func TestFormatReviews_AllZero(t *testing.T) {
	got := formatPatchReviews(db.PatchRow{})
	if got != "0/0/0/0" {
		t.Errorf("got %q, want 0/0/0/0", got)
	}
}

func TestFormatSeriesReviews(t *testing.T) {
	patches := []db.PatchRow{
		{AckedBy: 1, Fixes: 0, ReviewedBy: 2, TestedBy: 1},
		{AckedBy: 1, Fixes: 1, ReviewedBy: 0, TestedBy: 0},
	}
	got := formatSeriesReviews(patches)
	if got != "2/1/2/1" {
		t.Errorf("got %q, want 2/1/2/1", got)
	}
}

func TestFormatChecks(t *testing.T) {
	got := formatChecks(db.PatchRow{
		ChecksPass: 3, ChecksFail: 1, ChecksPending: 2,
	})
	if got != "3/1/2" {
		t.Errorf("got %q, want 3/1/2", got)
	}
}

func TestFormatChecks_AllZero(t *testing.T) {
	got := formatChecks(db.PatchRow{})
	if got != "-" {
		t.Errorf("got %q, want -", got)
	}
}

func TestFormatSeriesChecks(t *testing.T) {
	patches := []db.PatchRow{
		{ChecksPass: 2, ChecksFail: 0, ChecksPending: 1},
		{ChecksPass: 1, ChecksFail: 1, ChecksPending: 0},
	}
	got := formatSeriesChecks(patches)
	if got != "3/1/1" {
		t.Errorf("got %q, want 3/1/1", got)
	}
}

func TestAggregateState_Uniform(t *testing.T) {
	patches := []db.PatchRow{
		{State: "new"}, {State: "new"},
	}
	got := aggregateState(patches)
	if got != "new" {
		t.Errorf("got %q, want new", got)
	}
}

func TestAggregateState_Mixed(t *testing.T) {
	patches := []db.PatchRow{
		{State: "new"}, {State: "accepted"},
	}
	got := aggregateState(patches)
	if got != "mixed" {
		t.Errorf("got %q, want mixed", got)
	}
}

func TestAggregateState_Empty(t *testing.T) {
	got := aggregateState(nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func testSeriesWithAge(days int) (db.SeriesRow, []db.PatchRow) {
	d := time.Now().Add(
		-time.Duration(days) * 24 * time.Hour)
	date := d.Format("2006-01-02T15:04:05")
	s := db.SeriesRow{ID: 1, Name: "test", Date: date}
	p := []db.PatchRow{{
		ID: 1, State: "new", Date: date,
	}}
	return s, p
}

func TestColorForSeries_New(t *testing.T) {
	s, p := testSeriesWithAge(3)
	got := colorForSeries(s, p)
	if got != "yellow" {
		t.Errorf("got %q, want yellow", got)
	}
}

func TestColorForSeries_UnderReview(t *testing.T) {
	s, p := testSeriesWithAge(10)
	got := colorForSeries(s, p)
	if got != "white" {
		t.Errorf("got %q, want white", got)
	}
}

func TestColorForSeries_Aging(t *testing.T) {
	s, p := testSeriesWithAge(20)
	got := colorForSeries(s, p)
	if got != "lightred" {
		t.Errorf("got %q, want lightred", got)
	}
}

func TestColorForSeries_Old(t *testing.T) {
	s, p := testSeriesWithAge(35)
	got := colorForSeries(s, p)
	if got != "darkred" {
		t.Errorf("got %q, want darkred", got)
	}
}

func TestColorForSeries_Stale(t *testing.T) {
	s, p := testSeriesWithAge(90)
	got := colorForSeries(s, p)
	if got != "black" {
		t.Errorf("got %q, want black", got)
	}
}

func TestColorForSeries_HasReviews(t *testing.T) {
	s, p := testSeriesWithAge(10)
	p[0].ReviewedBy = 1
	got := colorForSeries(s, p)
	if got != "green" {
		t.Errorf("got %q, want green", got)
	}
}

func TestColorForSeries_HasAcked(t *testing.T) {
	s, p := testSeriesWithAge(10)
	p[0].AckedBy = 1
	got := colorForSeries(s, p)
	if got != "green" {
		t.Errorf("got %q, want green", got)
	}
}

func TestColorForSeries_HasFixes(t *testing.T) {
	s, p := testSeriesWithAge(10)
	p[0].Fixes = 1
	got := colorForSeries(s, p)
	if got != "green" {
		t.Errorf("got %q, want green", got)
	}
}

func TestColorForSeries_Delegated(t *testing.T) {
	s, p := testSeriesWithAge(10)
	p[0].Delegate = "someone"
	got := colorForSeries(s, p)
	if got != "grey" {
		t.Errorf("got %q, want grey", got)
	}
}

func TestSeriesToRow(t *testing.T) {
	d := time.Now().Add(-2 * 24 * time.Hour)
	date := d.Format("2006-01-02T15:04:05")

	s := db.SeriesRow{
		ID: 50, Name: "Lorem series",
		Date: date, Submitter: "Lorem Ipsum",
	}
	patches := []db.PatchRow{
		{
			ID: 100, Name: "[PATCH 1/2] Lorem",
			Date: date, State: "new",
			Submitter:  "Lorem Ipsum",
			AckedBy:    1,
			ChecksPass: 2,
		},
		{
			ID: 101, Name: "[PATCH 2/2] Ipsum",
			Date: date, State: "new",
			Submitter: "Lorem Ipsum",
		},
	}

	row := seriesToRow(s, patches)

	if row.Data[0] != "50" {
		t.Errorf("ID = %q", row.Data[0])
	}
	if row.Data[1] != "Lorem series" {
		t.Errorf("Name = %q", row.Data[1])
	}
	if row.Data[2] != "new" {
		t.Errorf("State = %q", row.Data[2])
	}
	if row.Data[3] != "Lorem Ipsum" {
		t.Errorf("Submitter = %q", row.Data[3])
	}
	if row.Data[4] != "2d" {
		t.Errorf("Age = %q", row.Data[4])
	}
	if row.Style.Background != "green" {
		t.Errorf("Background = %q, want green (has acked)",
			row.Style.Background)
	}
	if len(row.SubRows) != 2 {
		t.Fatalf("SubRows = %d", len(row.SubRows))
	}
	if row.SubRows[0][0] != "100" {
		t.Errorf("SubRow[0] ID = %q", row.SubRows[0][0])
	}
}

func TestSeriesToRow_EmptyNameFallback(t *testing.T) {
	d := time.Now().Format("2006-01-02T15:04:05")
	s := db.SeriesRow{ID: 50, Name: "", Date: d}
	patches := []db.PatchRow{
		{ID: 100, Name: "[PATCH] Lorem ipsum", Date: d,
			State: "new", Submitter: "Lorem"},
	}
	row := seriesToRow(s, patches)
	if row.Data[1] != "[PATCH] Lorem ipsum" {
		t.Errorf("Name = %q, want first patch name", row.Data[1])
	}
}

func TestGetCommentsForCover(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Lorem cover", Date: "2026-03-10",
	})
	d.InsertComment(db.CommentRow{
		ID: 500, CoverID: 99, Submitter: "Dolor Amet",
		Date: "2026-03-11T09:00:00", Subject: "Re: Lorem",
		Content: "Acked-by: Dolor Amet <dolor@amet.example>",
	})

	comments := GetCommentsForCover(d, 99)
	if len(comments) != 1 {
		t.Fatalf("len = %d, want 1", len(comments))
	}
	if comments[0].Submitter != "Dolor Amet" {
		t.Errorf("Submitter = %q", comments[0].Submitter)
	}
	if comments[0].Content != "Acked-by: Dolor Amet <dolor@amet.example>" {
		t.Errorf("Content = %q", comments[0].Content)
	}
}

func TestGetCommentsForCover_NilDB(t *testing.T) {
	comments := GetCommentsForCover(nil, 99)
	if comments != nil {
		t.Errorf("got %v, want nil", comments)
	}
}

func TestLoadFromDB(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	now := time.Now().Format("2006-01-02T15:04:05")
	d.SaveSeriesSummary(50, "Lorem series", now, 1)
	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "Lorem patch", Date: now,
		State: "new", Submitter: "Lorem",
	})

	rows, err := LoadFromDB(d, []string{"new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Data[0] != "50" {
		t.Errorf("series ID = %q", rows[0].Data[0])
	}
	if len(rows[0].SubRows) != 1 {
		t.Errorf("sub-rows = %d", len(rows[0].SubRows))
	}
}
