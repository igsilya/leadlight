package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"leadlight/db"
)

var PatchworkColumns = []ColumnDef{
	{Title: "ID", FixedWidth: 9},
	{Title: "Ver", FixedWidth: 4},
	{Title: "Name"},
	{Title: "State", FixedWidth: 8},
	{Title: "Submitter", FixedWidth: 20},
	{Title: "Age", FixedWidth: 5},
	{Title: "A/F/R/T", FixedWidth: 8},
	{Title: "Checks", FixedWidth: 8},
	{Title: "Dlg", FixedWidth: 8},
}

var stateDisplay = map[string]string{
	"under-review":      "review",
	"accepted":          "accept",
	"rejected":          "reject",
	"superseded":        "supersed",
	"changes-requested": "changes",
	"not-applicable":    "n/a",
	"handled-elsewhere": "handled",
}

func displayState(state string) string {
	if short, ok := stateDisplay[state]; ok {
		return short
	}
	return state
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func aggregateDelegate(patches []db.PatchRow) string {
	delegates := map[string]bool{}
	for _, p := range patches {
		if p.Delegate != "" {
			delegates[p.Delegate] = true
		}
	}
	if len(delegates) == 1 {
		for d := range delegates {
			return d
		}
	}
	return ""
}

func formatDelegate(username string, names map[string]string) string {
	if username == "" {
		return ""
	}
	if name, ok := names[username]; ok {
		return capitalize(name)
	}
	return username
}

const (
	ColState  = 3
	ColChecks = 7
)

var (
	versionRe  = regexp.MustCompile(`(?i)^v\d+$`)
	positionRe = regexp.MustCompile(`^\d+/\d+$`)
)

func stripPosition(name string) string {
	if !strings.HasPrefix(name, "[") {
		return name
	}
	close := strings.Index(name, "]")
	if close < 0 {
		return name
	}
	bracket := name[1:close]
	subject := strings.TrimSpace(name[close+1:])

	var kept []string
	for _, tok := range strings.Split(bracket, ",") {
		tok = strings.TrimSpace(tok)
		if !positionRe.MatchString(tok) {
			kept = append(kept, tok)
		}
	}
	if len(kept) > 0 {
		return "[" + strings.Join(kept, ",") + "] " + subject
	}
	return subject
}

func parsePatchName(
	name, listPrefix string,
) (cleaned, version string) {
	if !strings.HasPrefix(name, "[") {
		return name, ""
	}
	close := strings.Index(name, "]")
	if close < 0 {
		return name, ""
	}
	bracket := name[1:close]
	subject := strings.TrimSpace(name[close+1:])

	var kept []string
	for _, tok := range strings.Split(bracket, ",") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == listPrefix:
		case versionRe.MatchString(tok):
			version = tok
		default:
			kept = append(kept, tok)
		}
	}
	if len(kept) > 0 {
		cleaned = "[" + strings.Join(kept, ",") + "] " + subject
	} else {
		cleaned = subject
	}
	return cleaned, version
}

func detectListPrefix(names []string) string {
	counts := map[string]int{}
	for _, name := range names {
		if !strings.HasPrefix(name, "[") {
			continue
		}
		close := strings.Index(name, "]")
		if close < 0 {
			continue
		}
		bracket := name[1:close]
		tok := strings.TrimSpace(
			strings.SplitN(bracket, ",", 2)[0])
		if tok != "" && !versionRe.MatchString(tok) {
			counts[tok]++
		}
	}
	best, bestN := "", 0
	for tok, n := range counts {
		if n > bestN {
			best, bestN = tok, n
		}
	}
	return best
}

func LoadFromDB(
	d *db.DB, states []string,
) ([]RowData, error) {
	seriesList := d.GetActiveSeries(states)

	var names []string
	for _, s := range seriesList {
		names = append(names, s.Name)
	}
	listPrefix := detectListPrefix(names)
	delegateNames := d.GetDelegateDisplayNames()

	rows := make([]RowData, 0, len(seriesList))
	for _, s := range seriesList {
		patches := d.GetPatchesForSeries(s.ID)
		rows = append(rows,
			seriesToRow(s, patches, listPrefix, delegateNames))
	}
	return rows, nil
}

func seriesToRow(
	s db.SeriesRow, patches []db.PatchRow,
	listPrefix string, delegateNames map[string]string,
) RowData {
	name := s.Name
	if name == "" && len(patches) > 0 {
		name = patches[0].Name
	}
	cleaned, _ := parsePatchName(name, listPrefix)
	cleaned = stripPosition(cleaned)
	if s.TotalPatches > 1 {
		cleaned = fmt.Sprintf("[0/%d] %s",
			s.TotalPatches, cleaned)
	}
	ver := ""
	if s.Version > 1 {
		ver = fmt.Sprintf("v%d", s.Version)
	}
	row := RowData{
		Data: []string{
			strconv.Itoa(s.ID),
			ver,
			cleaned,
			aggregateState(patches),
			s.Submitter,
			formatAge(s.Date),
			formatSeriesReviews(patches),
			formatSeriesChecks(patches),
			formatDelegate(aggregateDelegate(patches), delegateNames),
		},
		Style: RowStyle{
			Background: colorForSeries(s, patches),
		},
	}

	row.SubRows = make([][]string, len(patches))
	for i, p := range patches {
		row.SubRows[i] = patchToSubRow(p, listPrefix, delegateNames)
	}
	return row
}

func patchToSubRow(p db.PatchRow, listPrefix string, dlgNames map[string]string) []string {
	cleaned, ver := parsePatchName(p.Name, listPrefix)
	return []string{
		strconv.Itoa(p.ID),
		ver,
		cleaned,
		displayState(p.State),
		p.Submitter,
		formatAge(p.Date),
		formatPatchReviews(p),
		formatChecks(p),
		formatDelegate(p.Delegate, dlgNames),
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
			return displayState(s)
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

func GetCommentsForCover(d *db.DB, coverID int) []CommentInfo {
	if d == nil {
		return nil
	}
	rows := d.GetCommentsForCover(coverID)
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
