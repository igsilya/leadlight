package tui

import (
	"fmt"
	"strconv"
	"time"

	"leadlight/db"
)

var PatchworkColumns = []ColumnDef{
	{Title: "ID", Percentage: 0.06},
	{Title: "Name", Percentage: 0.30},
	{Title: "State", Percentage: 0.10},
	{Title: "Submitter", Percentage: 0.15},
	{Title: "Age", Percentage: 0.07},
	{Title: "A/F/R/T", Percentage: 0.12},
	{Title: "Checks", Percentage: 0.10},
}

const (
	ColState  = 2
	ColChecks = 6
)

func LoadFromDB(
	d *db.DB, states []string,
) ([]RowData, error) {
	seriesList := d.GetActiveSeries(states)
	rows := make([]RowData, 0, len(seriesList))

	for _, s := range seriesList {
		patches := d.GetPatchesForSeries(s.ID)
		rows = append(rows, seriesToRow(s, patches))
	}
	return rows, nil
}

func seriesToRow(
	s db.SeriesRow, patches []db.PatchRow,
) RowData {
	name := s.Name
	if name == "" && len(patches) > 0 {
		name = patches[0].Name
	}
	row := RowData{
		Data: []string{
			strconv.Itoa(s.ID),
			name,
			aggregateState(patches),
			s.Submitter,
			formatAge(s.Date),
			formatSeriesReviews(patches),
			formatSeriesChecks(patches),
		},
		Style: RowStyle{
			Background: colorForSeries(s, patches),
		},
	}

	row.SubRows = make([][]string, len(patches))
	for i, p := range patches {
		row.SubRows[i] = patchToSubRow(p)
	}
	return row
}

func patchToSubRow(p db.PatchRow) []string {
	return []string{
		strconv.Itoa(p.ID),
		p.Name,
		p.State,
		p.Submitter,
		formatAge(p.Date),
		formatPatchReviews(p),
		formatChecks(p),
	}
}

func aggregateState(patches []db.PatchRow) string {
	if len(patches) == 0 {
		return ""
	}
	states := map[string]bool{}
	for _, p := range patches {
		states[p.State] = true
	}
	if len(states) == 1 {
		for s := range states {
			return s
		}
	}
	return "mixed"
}

func formatAge(dateStr string) string {
	t, err := time.Parse("2006-01-02T15:04:05", dateStr)
	if err != nil {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}

func formatPatchReviews(p db.PatchRow) string {
	return fmt.Sprintf("%d/%d/%d/%d",
		p.AckedBy, p.Fixes, p.ReviewedBy, p.TestedBy)
}

func formatSeriesReviews(patches []db.PatchRow) string {
	a, f, r, te := 0, 0, 0, 0
	for _, p := range patches {
		a += p.AckedBy
		f += p.Fixes
		r += p.ReviewedBy
		te += p.TestedBy
	}
	return fmt.Sprintf("%d/%d/%d/%d", a, f, r, te)
}

func formatChecks(p db.PatchRow) string {
	if p.ChecksPass == 0 && p.ChecksFail == 0 &&
		p.ChecksPending == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d/%d",
		p.ChecksPass, p.ChecksFail, p.ChecksPending)
}

func formatSeriesChecks(patches []db.PatchRow) string {
	pass, fail, pending := 0, 0, 0
	for _, p := range patches {
		pass += p.ChecksPass
		fail += p.ChecksFail
		pending += p.ChecksPending
	}
	if pass == 0 && fail == 0 && pending == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d/%d", pass, fail, pending)
}

func parseDate(dateStr string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05", dateStr)
	return t
}

func colorForSeries(
	s db.SeriesRow, patches []db.PatchRow,
) string {
	totalAcked, totalFixes, totalReviewed := 0, 0, 0
	hasDelegated := false

	for _, p := range patches {
		totalAcked += p.AckedBy
		totalFixes += p.Fixes
		totalReviewed += p.ReviewedBy
		if p.Delegate != "" {
			hasDelegated = true
		}
	}

	age := time.Since(parseDate(s.Date))

	switch {
	case age > 60*24*time.Hour:
		return "black"
	case totalAcked > 0 || totalReviewed > 0 || totalFixes > 0:
		return "green"
	case hasDelegated:
		return "grey"
	case age > 28*24*time.Hour:
		return "darkred"
	case age > 14*24*time.Hour:
		return "lightred"
	case age > 7*24*time.Hour:
		return "white"
	default:
		return "yellow"
	}
}

func GetCommentsForPatch(d *db.DB, patchID int) []CommentInfo {
	if d == nil {
		return nil
	}
	rows := d.GetComments(patchID)
	comments := make([]CommentInfo, len(rows))
	for i, r := range rows {
		comments[i] = CommentInfo{
			ID:        r.ID,
			Submitter: r.Submitter,
			Date:      r.Date,
			Subject:   r.Subject,
			Content:   r.Content,
		}
	}
	return comments
}

func GetChecksForPatch(d *db.DB, patchID int) []CheckInfo {
	if d == nil {
		return nil
	}
	rows := d.GetChecksForPatch(patchID)
	checks := make([]CheckInfo, len(rows))
	for i, r := range rows {
		checks[i] = CheckInfo{
			Context:   r.Context,
			State:     r.State,
			TargetURL: r.TargetURL,
		}
	}
	return checks
}
