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

func TestParsePatchName(t *testing.T) {
	tests := []struct {
		name, prefix string
		wantName     string
		wantVer      string
	}{
		{
			"[ovs-dev,v5,2/4] Fix foo", "ovs-dev",
			"[2/4] Fix foo", "v5",
		},
		{
			"[ovs-dev,RFC,v3,1/2] Add bar", "ovs-dev",
			"[RFC,1/2] Add bar", "v3",
		},
		{
			"[ovs-dev] Simple patch", "ovs-dev",
			"Simple patch", "",
		},
		{
			"Plain subject", "ovs-dev",
			"Plain subject", "",
		},
		{
			"[ovs-dev,v3] Fix baz", "ovs-dev",
			"Fix baz", "v3",
		},
		{
			"[ovs-dev,net] net: openvswitch: foo", "ovs-dev",
			"[net] net: openvswitch: foo", "",
		},
		{
			"[ovs-dev,07/10,net-next,v3] net: bar", "ovs-dev",
			"[07/10,net-next] net: bar", "v3",
		},
		{
			"[V5,1/7] net/hinic3: add support", "",
			"[1/7] net/hinic3: add support", "V5",
		},
		{
			"[RFC,v2] dpif: foo", "",
			"[RFC] dpif: foo", "v2",
		},
		{
			"[ovs-dev,RFC,series_484144,v2] foo", "ovs-dev",
			"[RFC,series_484144] foo", "v2",
		},
	}
	for _, tt := range tests {
		name, ver := parsePatchName(tt.name, tt.prefix)
		if name != tt.wantName {
			t.Errorf("parsePatchName(%q, %q) name = %q, want %q",
				tt.name, tt.prefix, name, tt.wantName)
		}
		if ver != tt.wantVer {
			t.Errorf("parsePatchName(%q, %q) ver = %q, want %q",
				tt.name, tt.prefix, ver, tt.wantVer)
		}
	}
}

func TestStripPosition(t *testing.T) {
	tests := []struct{ in, want string }{
		{"[07/10,net-next] net: convert", "[net-next] net: convert"},
		{"[2/4] Fix foo", "Fix foo"},
		{"[RFC,1/2] Add bar", "[RFC] Add bar"},
		{"[net] net: openvswitch", "[net] net: openvswitch"},
		{"Plain subject", "Plain subject"},
		{"[RFC] dpif: foo", "[RFC] dpif: foo"},
	}
	for _, tt := range tests {
		got := stripPosition(tt.in)
		if got != tt.want {
			t.Errorf("stripPosition(%q) = %q, want %q",
				tt.in, got, tt.want)
		}
	}
}

func TestDetectListPrefix(t *testing.T) {
	names := []string{
		"[ovs-dev,v1,1/3] Lorem ipsum",
		"[ovs-dev,v2] Dolor sit amet",
		"[ovs-dev] Consectetur",
		"Plain subject",
		"[ovs-dev,RFC] Adipiscing",
	}
	got := detectListPrefix(names)
	if got != "ovs-dev" {
		t.Errorf("got %q, want ovs-dev", got)
	}
}

func TestDetectListPrefix_Empty(t *testing.T) {
	names := []string{"Plain", "No brackets"}
	got := detectListPrefix(names)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"lorem", "Lorem"},
		{"Dolor", "Dolor"},
		{"", ""},
		{"k", "K"},
	}
	for _, tt := range tests {
		got := capitalize(tt.in)
		if got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q",
				tt.in, got, tt.want)
		}
	}
}

func TestAggregateDelegate(t *testing.T) {
	single := []db.PatchRow{
		{Delegate: "lorem"}, {Delegate: "lorem"},
	}
	if got := aggregateDelegate(single); got != "lorem" {
		t.Errorf("single = %q", got)
	}

	none := []db.PatchRow{{}, {}}
	if got := aggregateDelegate(none); got != "" {
		t.Errorf("none = %q", got)
	}

	mixed := []db.PatchRow{
		{Delegate: "lorem"}, {Delegate: "dolor"},
	}
	if got := aggregateDelegate(mixed); got != "" {
		t.Errorf("mixed = %q, want empty", got)
	}

	empty := []db.PatchRow{}
	if got := aggregateDelegate(empty); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestFormatDelegate(t *testing.T) {
	names := map[string]string{
		"lorem": "lorem",
		"dolor": "Dolor",
	}
	if got := formatDelegate("lorem", names); got != "Lorem" {
		t.Errorf("lorem = %q", got)
	}
	if got := formatDelegate("dolor", names); got != "Dolor" {
		t.Errorf("dolor = %q", got)
	}
	if got := formatDelegate("unknown", names); got != "unknown" {
		t.Errorf("unknown = %q", got)
	}
	if got := formatDelegate("", names); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestDisplayState(t *testing.T) {
	tests := []struct{ in, want string }{
		{"new", "new"},
		{"rfc", "rfc"},
		{"deferred", "deferred"},
		{"mixed", "mixed"},
		{"under-review", "review"},
		{"accepted", "accept"},
		{"rejected", "reject"},
		{"superseded", "supersed"},
		{"changes-requested", "changes"},
		{"not-applicable", "n/a"},
		{"handled-elsewhere", "handled"},
	}
	for _, tt := range tests {
		got := displayState(tt.in)
		if got != tt.want {
			t.Errorf("displayState(%q) = %q, want %q",
				tt.in, got, tt.want)
		}
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

	row := seriesToRow(s, patches, "", nil)

	if row.Data[0] != "50" {
		t.Errorf("ID = %q", row.Data[0])
	}
	if row.Data[1] != "" {
		t.Errorf("Ver = %q, want empty (v1)", row.Data[1])
	}
	if row.Data[2] != "Lorem series" {
		t.Errorf("Name = %q", row.Data[2])
	}
	if row.Data[3] != "new" {
		t.Errorf("State = %q", row.Data[3])
	}
	if row.Data[4] != "Lorem Ipsum" {
		t.Errorf("Submitter = %q", row.Data[4])
	}
	if row.Data[5] != "2d" {
		t.Errorf("Age = %q", row.Data[5])
	}
	if row.Data[8] != "" {
		t.Errorf("Dlg = %q, want empty", row.Data[8])
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
	row := seriesToRow(s, patches, "", nil)
	if row.Data[2] != "[PATCH] Lorem ipsum" {
		t.Errorf("Name = %q, want first patch name", row.Data[2])
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
