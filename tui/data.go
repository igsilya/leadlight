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
	{Title: "ID", FixedWidth: 10, Visible: true},
	{Title: "Ver", FixedWidth: 4, Visible: true},
	{Title: "Name", Visible: true},
	{Title: "State", FixedWidth: 9, Visible: true},
	{Title: "Submitter", FixedWidth: 18, Visible: true},
	{Title: "Age", FixedWidth: 5, Visible: true},
	{Title: "C", FixedWidth: 3, Visible: true},
	{Title: "Comments", FixedWidth: 15, Visible: false},
	{Title: "A F R T", FixedWidth: 9, Visible: true},
	{Title: "Checks", FixedWidth: 9, Visible: true},
	{Title: "Delegate", FixedWidth: 8, Visible: true},
}

var stateDisplay = map[string]string{
	"under-review":      "review",
	"accepted":          "accept",
	"rejected":          "reject",
	"superseded":        "supersed",
	"changes-requested": "changes",
	"not-applicable":    "n/a",
	"handled-elsewhere": "handled",
	"awaiting-upstream": "awaiting",
	"deferred":          "defer",
	"mainlined":         "mainline",
	"needs-ack":         "need-ack",
}

func displayState(state string) string {
	if short, ok := stateDisplay[state]; ok {
		return short
	}
	return state
}

func displaySubmitter(name, email string) string {
	if name != "" {
		return name
	}
	return email
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

type ColIndex int

const (
	ColID ColIndex = iota
	ColVer
	ColName
	ColState
	ColSubmitter
	ColAge
	ColC
	ColComments
	ColAFRT
	ColChecks
	ColDelegate
	ColMax // sentinel: number of columns
)

const ColNone ColIndex = -1

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
	allCommentNames := d.GetCommentSubmittersBatch(false, states)
	allPatchCommentNames := d.GetPatchCommentSubmittersBatch(false, states)

	rows := make([]RowData, 0, len(seriesList))
	for _, s := range seriesList {
		rows = append(rows, seriesToRow(
			s, allPatches[s.ID], listPrefix, delegateNames,
			allTags[s.ID], allComments[s.ID], allPatchComments,
			allCommentNames[s.ID], allPatchCommentNames))
	}
	return rows, nil
}

func seriesToRow(
	s db.SeriesRow, patches []db.PatchRow,
	listPrefix string, delegateNames map[string]string,
	tags []db.TagRow, commentCount int,
	patchComments map[int]int,
	commentNames []string, patchCommentNames map[int][]string,
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
			displaySubmitter(s.Submitter, s.SubmitterEmail),
			s.Date, // raw date; formatAge called at render time so cached rows stay fresh
			formatCount(commentCount),
			formatCommentCell(commentCount, commentNames, s.Submitter),
			formatSeriesReviews(patches, tags),
			formatSeriesChecks(patches),
			formatDelegate(aggregateDelegate(patches), delegateNames),
		},
		Style: RowStyle{
			Background: colorForSeries(s, patches, tags, commentCount),
		},
	}

	row.SubRows = make([][]string, len(patches))
	row.SubRowStyles = make([]RowStyle, len(patches))
	for i, p := range patches {
		row.SubRows[i] = patchToSubRow(
			p, listPrefix, delegateNames, tags,
			patchComments[p.ID], patchCommentNames[p.ID])
		row.SubRowStyles[i] = RowStyle{
			Background: "sub:" + colorForPatch(p, tags, patchComments[p.ID]),
		}
	}
	return row
}

func patchToSubRow(
	p db.PatchRow, listPrefix string,
	dlgNames map[string]string, tags []db.TagRow,
	commentCount int, commentNames []string,
) []string {
	cleaned, ver := parsePatchName(p.Name, listPrefix)
	return []string{
		strconv.Itoa(p.ID),
		ver,
		cleaned,
		displayState(p.State),
		displaySubmitter(p.Submitter, p.SubmitterEmail),
		p.Date, // raw date; formatAge called at render time so cached rows stay fresh
		formatCount(commentCount),
		formatCommentCell(commentCount, commentNames, p.Submitter),
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
	now := time.Now()
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		years := 0
		for t.AddDate(years+1, 0, 0).Before(now) {
			years++
		}
		if years >= 1 {
			return fmt.Sprintf("%dy", years)
		}
		months := 0
		for t.AddDate(0, months+1, 0).Before(now) {
			months++
		}
		if months >= 1 {
			return fmt.Sprintf("%dmo", months)
		}
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func computePatchAFRT(patchID int, tags []db.TagRow) (a, f, r, t int) {
	seen := map[string]bool{}
	for _, tag := range tags {
		if !tagAppliesToPatch(tag, patchID) {
			continue
		}
		key := tagKey(tag)
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

func firstName(s string) string {
	// Strip email: "Name <email>" → "Name", or "<email>" → email
	if i := strings.IndexByte(s, '<'); i >= 0 {
		name := strings.TrimSpace(s[:i])
		if name != "" {
			s = name
		} else {
			end := strings.IndexByte(s, '>')
			if end < 0 {
				end = len(s)
			}
			s = s[i+1 : end]
		}
	}
	// "Lastname, Firstname" → use Firstname
	if i := strings.IndexByte(s, ','); i >= 0 {
		after := strings.TrimSpace(s[i+1:])
		if after != "" {
			s = after
		}
	}
	// Email address → use local part before @
	if at := strings.IndexByte(s, '@'); at > 0 {
		return s[:at]
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

func formatCommentCell(count int, submitters []string, author string) string {
	c := formatCount(count)
	if len(submitters) == 0 {
		return c
	}
	seen := map[string]bool{}
	var names []string
	for _, s := range submitters {
		// Exclude the series author — their own replies don't
		// represent external review activity.
		if s == author {
			continue
		}
		name := firstName(s)
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return c
	}
	return c + " " + strings.Join(names, ", ")
}

func formatCount(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func formatPatchReviews(patchID int, tags []db.TagRow) string {
	a, f, r, t := computePatchAFRT(patchID, tags)
	return formatCount(a) + " " + formatCount(f) + " " + formatCount(r) + " " + formatCount(t)
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
	return formatCount(a) + " " + formatCount(f) + " " + formatCount(r) + " " + formatCount(t)
}

func isTerminalState(state string) bool {
	switch state {
	case "new", "under-review":
		return false
	}
	return true
}

func formatChecks(p db.PatchRow) string {
	return formatCount(p.ChecksPass) + " " + formatCount(p.ChecksFail) + " " + formatCount(p.ChecksWarn)
}

func formatSeriesChecks(patches []db.PatchRow) string {
	pass, fail, warn := 0, 0, 0
	for _, p := range patches {
		pass += p.ChecksPass
		fail += p.ChecksFail
		warn += p.ChecksWarn
	}
	return formatCount(pass) + " " + formatCount(fail) + " " + formatCount(warn)
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
		return "closed"
	}

	if isAllReviewed(patches, tags) {
		return "reviewed"
	}
	return colorByAge(s.Date, commentCount > 0)
}

// tagKey builds a unique key for a tag by combining type and identity
// with a NUL separator that can't appear in either field.
func tagKey(tag db.TagRow) string {
	return tag.Type + "\x00" + tag.Identity
}

// tagAppliesToPatch returns true if the tag is directly on this patch
// or on the series' cover letter (which applies to all patches).
func tagAppliesToPatch(tag db.TagRow, patchID int) bool {
	return tag.PatchID == patchID || tag.CoverID != 0
}

// commentReviewTags returns the set of unique review tag keys from
// comment-sourced tags. Only acked-by and reviewed-by count as reviews;
// tested-by and fixes don't establish that a patch was reviewed.
func commentReviewTags(tags []db.TagRow) map[string]bool {
	result := map[string]bool{}
	for _, tag := range tags {
		if tag.Source == "comment" && isReviewTag(tag.Type) {
			result[tagKey(tag)] = true
		}
	}
	return result
}

// colorByAge returns the semantic color name based on age and comment
// activity. Called after terminal and reviewed states are ruled out.
// Thresholds: >14 days = "old", >60 days = "very old" (likely abandoned).
func colorByAge(date string, hasComments bool) string {
	age := time.Since(parseDate(date))
	old := age > 14*24*time.Hour
	veryOld := age > 60*24*time.Hour
	switch {
	case old && hasComments:
		return "aging"
	case !old && hasComments:
		return "active"
	case veryOld:
		return "stale"
	case !old:
		return "pending"
	default:
		return "overdue"
	}
}

func isReviewTag(tagType string) bool {
	return tagType == "acked" || tagType == "reviewed"
}

// isAllReviewed returns true when every patch has at least one acked/reviewed
// tag whose author also left a review comment — distinguishing actual
// third-party reviews from self-applied tags in the original submission.
func isAllReviewed(patches []db.PatchRow, tags []db.TagRow) bool {
	if len(patches) == 0 {
		return false
	}
	commentTags := commentReviewTags(tags)
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

// patchOverlapsComments checks whether any review tag on this patch
// (or its series' cover letter) also appears in the set of comment-sourced
// review tags. This confirms that an external reviewer actually reviewed
// this specific patch, not just the original submission.
func patchOverlapsComments(
	patchID int, tags []db.TagRow, commentTags map[string]bool,
) bool {
	for _, tag := range tags {
		if !tagAppliesToPatch(tag, patchID) {
			continue
		}
		if !isReviewTag(tag.Type) {
			continue
		}
		if commentTags[tagKey(tag)] {
			return true
		}
	}
	return false
}

func isPatchReviewed(patchID int, tags []db.TagRow) bool {
	commentTags := commentReviewTags(tags)
	if len(commentTags) == 0 {
		return false
	}
	return patchOverlapsComments(patchID, tags, commentTags)
}

func colorForPatch(
	p db.PatchRow, tags []db.TagRow, commentCount int,
) string {
	if isTerminalState(p.State) {
		return "closed"
	}
	if isPatchReviewed(p.ID, tags) {
		return "reviewed"
	}
	return colorByAge(p.Date, commentCount > 0)
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
			Context:     r.Context,
			State:       r.State,
			TargetURL:   r.TargetURL,
			Description: r.Description,
		}
	}
	return checks
}
