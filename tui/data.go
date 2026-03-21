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
	{Title: "C", FixedWidth: 3},
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
	ColNone     = -1
	ColState    = 3
	ColComments = 6
	ColAFRT     = 7
	ColChecks   = 8
	ColDlg      = 9
)

var AllPatchStates = []string{
	"new", "under-review", "accepted", "rejected",
	"rfc", "superseded", "changes-requested",
	"deferred", "not-applicable", "handled-elsewhere",
}

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

	allPatches := d.GetAllPatchesBatch(false, states)
	allTags := d.GetTagsBatch(false, states)
	allComments := d.GetCommentCountsBatch(false, states)
	allPatchComments := d.GetPatchCommentCountsBatch(false, states)

	rows := make([]RowData, 0, len(seriesList))
	for _, s := range seriesList {
		rows = append(rows, seriesToRow(
			s, allPatches[s.ID], listPrefix, delegateNames,
			allTags[s.ID], allComments[s.ID], allPatchComments))
	}
	return rows, nil
}

func seriesToRow(
	s db.SeriesRow, patches []db.PatchRow,
	listPrefix string, delegateNames map[string]string,
	tags []db.TagRow, commentCount int,
	patchComments map[int]int,
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
			formatCount(commentCount),
			formatSeriesReviews(patches, tags),
			formatSeriesChecks(patches),
			formatDelegate(aggregateDelegate(patches), delegateNames),
		},
		Style: RowStyle{
			Background: colorForSeries(s, patches, tags, commentCount),
		},
	}

	row.SubRows = make([][]string, len(patches))
	for i, p := range patches {
		row.SubRows[i] = patchToSubRow(
			p, listPrefix, delegateNames, tags,
			patchComments[p.ID])
	}
	return row
}

func patchToSubRow(
	p db.PatchRow, listPrefix string,
	dlgNames map[string]string, tags []db.TagRow,
	commentCount int,
) []string {
	cleaned, ver := parsePatchName(p.Name, listPrefix)
	return []string{
		strconv.Itoa(p.ID),
		ver,
		cleaned,
		displayState(p.State),
		p.Submitter,
		formatAge(p.Date),
		formatCount(commentCount),
		formatPatchReviews(p.ID, tags),
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

func computePatchAFRT(patchID int, tags []db.TagRow) (a, f, r, t int) {
	seen := map[string]bool{}
	for _, tag := range tags {
		if tag.PatchID != patchID && tag.CoverID == 0 {
			continue
		}
		key := tag.Type + "\x00" + tag.Identity
		if seen[key] {
			continue
		}
		seen[key] = true
		switch tag.Type {
		case "acked":
			a++
		case "fixes":
			f++
		case "reviewed":
			r++
		case "tested":
			t++
		}
	}
	return
}

func formatCount(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func formatPatchReviews(patchID int, tags []db.TagRow) string {
	a, f, r, t := computePatchAFRT(patchID, tags)
	return formatCount(a) + "/" + formatCount(f) + "/" + formatCount(r) + "/" + formatCount(t)
}

func formatSeriesReviews(patches []db.PatchRow, tags []db.TagRow) string {
	a, f, r, t := 0, 0, 0, 0
	for _, p := range patches {
		pa, pf, pr, pt := computePatchAFRT(p.ID, tags)
		a += pa
		f += pf
		r += pr
		t += pt
	}
	return formatCount(a) + "/" + formatCount(f) + "/" + formatCount(r) + "/" + formatCount(t)
}

func isTerminalState(state string) bool {
	switch state {
	case "new", "under-review":
		return false
	}
	return true
}

func formatChecks(p db.PatchRow) string {
	return formatCount(p.ChecksPass) + "/" + formatCount(p.ChecksFail) + "/" + formatCount(p.ChecksPending)
}

func formatSeriesChecks(patches []db.PatchRow) string {
	pass, fail, pending := 0, 0, 0
	for _, p := range patches {
		pass += p.ChecksPass
		fail += p.ChecksFail
		pending += p.ChecksPending
	}
	return formatCount(pass) + "/" + formatCount(fail) + "/" + formatCount(pending)
}

func parseDate(dateStr string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05", dateStr)
	return t
}

func colorForSeries(
	s db.SeriesRow, patches []db.PatchRow,
	tags []db.TagRow, commentCount int,
) string {
	allTerminal := len(patches) > 0
	for _, p := range patches {
		if !isTerminalState(p.State) {
			allTerminal = false
			break
		}
	}
	if allTerminal {
		return "grey"
	}

	if isAllReviewed(patches, tags) {
		return "green"
	}

	age := time.Since(parseDate(s.Date))
	hasComments := commentCount > 0
	old := age > 14*24*time.Hour
	veryOld := age > 60*24*time.Hour

	switch {
	case old && hasComments:
		return "yellow"
	case !old && hasComments:
		return "white"
	case veryOld:
		return "black"
	case !old:
		return "lightred"
	default:
		return "darkred"
	}
}

func isAllReviewed(patches []db.PatchRow, tags []db.TagRow) bool {
	if len(patches) == 0 {
		return false
	}
	// R = all comment tags across the series
	commentTags := map[string]bool{}
	for _, tag := range tags {
		if tag.Source == "comment" {
			commentTags[tag.Type+"\x00"+tag.Identity] = true
		}
	}
	if len(commentTags) == 0 {
		return false
	}
	for _, p := range patches {
		if !patchOverlapsComments(p.ID, tags, commentTags) {
			return false
		}
	}
	return true
}

func patchOverlapsComments(
	patchID int, tags []db.TagRow, commentTags map[string]bool,
) bool {
	for _, tag := range tags {
		if tag.PatchID != patchID && tag.CoverID == 0 {
			continue
		}
		key := tag.Type + "\x00" + tag.Identity
		if commentTags[key] {
			return true
		}
	}
	return false
}

func convertComments(rows []db.CommentRow) []CommentInfo {
	comments := make([]CommentInfo, len(rows))
	for i, r := range rows {
		comments[i] = CommentInfo{
			ID:             r.ID,
			Submitter:      r.Submitter,
			SubmitterEmail: r.SubmitterEmail,
			Date:           r.Date,
			Subject:        r.Subject,
			Content:        r.Content,
			Headers:        r.Headers,
			WebURL:         r.WebURL,
			ListArchiveURL: r.ListArchiveURL,
		}
	}
	return comments
}

func GetCommentsForPatch(d *db.DB, patchID int) []CommentInfo {
	if d == nil {
		return nil
	}
	return convertComments(d.GetComments(patchID))
}

func GetCommentsForCover(d *db.DB, coverID int) []CommentInfo {
	if d == nil {
		return nil
	}
	return convertComments(d.GetCommentsForCover(coverID))
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
