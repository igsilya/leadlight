package tui

import (
	"encoding/json"
	"fmt"
	"mime"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"leadlight/db"
)

type ParsedMbox struct {
	Subject string
	From    string
	To      string
	Cc      string
	Date    string
	Body    string
	Diff    string
}

// parseJSONHeaders parses the JSON-encoded headers string from the DB
// into a map. Returns an empty map on error.
func parseJSONHeaders(headersJSON string) map[string]interface{} {
	if headersJSON == "" || headersJSON == "{}" {
		return nil
	}
	var m map[string]interface{}
	json.Unmarshal([]byte(headersJSON), &m)
	return m
}

// headerFoldRe matches RFC 2822 header continuation: a newline
// (optionally preceded by \r) followed by one or more spaces/tabs.
var headerFoldRe = regexp.MustCompile(`\r?\n[ \t]+`)

// compactHeader joins RFC 2822 folded header lines into a single line.
func compactHeader(s string) string {
	return strings.TrimSpace(headerFoldRe.ReplaceAllString(s, " "))
}

// headerString extracts a string value from a JSON headers map.
// Handles both string and []interface{} (returns first element).
func headerString(headers map[string]interface{}, key string) string {
	v, ok := headers[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		if len(val) > 0 {
			if s, ok := val[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

// getHeader extracts a header value from either the compact
// "Key: value\n" format (used for new data) or the legacy JSON
// format (existing data). Format is detected by the leading "{".
func getHeader(headers, key string) string {
	if strings.HasPrefix(headers, "{") {
		h := parseJSONHeaders(headers)
		return headerString(h, key)
	}
	return extractHeader(headers, key)
}

// fromHeader returns the sender from email headers. Prefers Reply-To
// (avoids mailing list mangling like "Name via dev <list@example>")
// and falls back to From. Values are MIME-decoded for non-ASCII names.
func fromHeader(headers map[string]interface{}) string {
	if replyTo := headerString(headers, "Reply-To"); replyTo != "" {
		return decodeHeader(compactHeader(replyTo))
	}
	return decodeHeader(compactHeader(headerString(headers, "From")))
}

// formatFrom constructs a "Name <email>" string, handling the case
// where the name is empty (returns just the email without brackets
// or leading spaces).
func formatFrom(name, email string) string {
	if name != "" && email != "" {
		return name + " <" + email + ">"
	}
	if name != "" {
		return name
	}
	return email
}

// BuildParsedMboxFromPatch constructs a ParsedMbox from patch detail
// data, eliminating the need for a separate mbox HTTP fetch.
func BuildParsedMboxFromPatch(row db.PatchRow) ParsedMbox {
	if row.Headers == "" || row.Headers == "{}" {
		return ParsedMbox{
			Subject: row.Name,
			From:    formatFrom(row.Submitter, row.SubmitterEmail),
			Date:    row.Date,
			Body:    row.Content,
			Diff:    row.Diff,
		}
	}
	subject := decodeHeader(compactHeader(getHeader(row.Headers, "Subject")))
	if subject == "" {
		subject = compactHeader(row.Name)
	}
	from := decodeHeader(compactHeader(getHeader(row.Headers, "Reply-To")))
	if from == "" {
		from = decodeHeader(compactHeader(getHeader(row.Headers, "From")))
	}
	date := compactHeader(getHeader(row.Headers, "Date"))
	if date == "" {
		date = row.Date
	}
	return ParsedMbox{
		Subject: subject,
		From:    from,
		To:      decodeHeader(compactHeader(getHeader(row.Headers, "To"))),
		Cc:      decodeHeader(compactHeader(getHeader(row.Headers, "Cc"))),
		Date:    date,
		Body:    row.Content,
		Diff:    row.Diff,
	}
}

// BuildParsedMboxFromCover constructs a ParsedMbox from cover letter
// detail data.
func BuildParsedMboxFromCover(row db.CoverRow) ParsedMbox {
	if row.Headers == "" || row.Headers == "{}" {
		return ParsedMbox{
			Subject: row.Name,
			Date:    row.Date,
			Body:    row.Content,
		}
	}
	subject := decodeHeader(compactHeader(getHeader(row.Headers, "Subject")))
	if subject == "" {
		subject = compactHeader(row.Name)
	}
	from := decodeHeader(compactHeader(getHeader(row.Headers, "Reply-To")))
	if from == "" {
		from = decodeHeader(compactHeader(getHeader(row.Headers, "From")))
	}
	date := compactHeader(getHeader(row.Headers, "Date"))
	if date == "" {
		date = row.Date
	}
	return ParsedMbox{
		Subject: subject,
		From:    from,
		To:      decodeHeader(compactHeader(getHeader(row.Headers, "To"))),
		Cc:      decodeHeader(compactHeader(getHeader(row.Headers, "Cc"))),
		Date:    date,
		Body:    row.Content,
	}
}

func decodeHeader(s string) string {
	dec := new(mime.WordDecoder)
	result, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return result
}

func splitHeadersBody(raw string) (string, string) {
	idx := strings.Index(raw, "\n\n")
	if idx < 0 {
		return raw, ""
	}
	return raw[:idx], raw[idx+2:]
}

func extractHeader(headers, name string) string {
	prefix := name + ":"
	var result []string
	found := false
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = true
			result = append(result, strings.TrimSpace(line[len(prefix):]))
		} else if found && len(line) > 0 &&
			(line[0] == ' ' || line[0] == '\t') {
			result = append(result, strings.TrimSpace(line))
		} else {
			found = false
		}
	}
	return strings.Join(result, " ")
}

// writeHeader renders a labeled header value, optionally collapsing
// long recipient lists to collapseHeaderMax lines with a count marker.
func writeHeader(b *strings.Builder, label, value string, collapse bool, labelWidth, valWidth int) {
	if value == "" {
		return
	}
	b.WriteString(mboxHeaderLabel.Render(label))
	lines := wrapHeaderValue(value, valWidth)
	if collapse && len(lines) > collapseHeaderMax {
		for i := 0; i < collapseHeaderMax; i++ {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", labelWidth))
			}
			b.WriteString(mboxHeaderValue.Render(lines[i]))
			b.WriteByte('\n')
		}
		total := strings.Count(value, ", ") + 1
		marker := fmt.Sprintf("··· %d total (e to expand) ···", total)
		b.WriteString(strings.Repeat(" ", labelWidth))
		b.WriteString(quotedLineStyle.Render(marker))
		b.WriteByte('\n')
		return
	}
	for i, line := range lines {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", labelWidth))
		}
		b.WriteString(mboxHeaderValue.Render(line))
		b.WriteByte('\n')
	}
}

func FormatMbox(p ParsedMbox, width int, collapse bool) string {
	headers, body, diff := FormatMboxParts(p, width, collapse)
	var b strings.Builder
	b.WriteString(headers)
	b.WriteString(body)
	b.WriteString(diff)
	return b.String()
}

// FormatMboxParts returns the three sections of a formatted mbox
// separately so the compare view can align them between two patches.
func FormatMboxParts(
	p ParsedMbox, width int, collapse bool,
) (headers, body, diff string) {
	labelWidth := 9 // "Subject: " length
	valWidth := width - labelWidth
	if valWidth < 20 {
		valWidth = 20
	}

	var hb strings.Builder
	writeHeader(&hb, "Subject: ", p.Subject, false, labelWidth, valWidth)
	writeHeader(&hb, "From:    ", p.From, false, labelWidth, valWidth)
	writeHeader(&hb, "To:      ", p.To, collapse, labelWidth, valWidth)
	writeHeader(&hb, "Cc:      ", p.Cc, collapse, labelWidth, valWidth)
	writeHeader(&hb, "Date:    ", p.Date, false, labelWidth, valWidth)
	headers = hb.String()

	if p.Body != "" {
		var bb strings.Builder
		bb.WriteByte('\n')
		cleaned := replaceControlChars(p.Body)
		for _, line := range strings.Split(cleaned, "\n") {
			quoted := isQuotedLine(line)
			for _, wl := range wrapLine(line, width) {
				writeStyledLine(&bb, wl, quoted)
				bb.WriteByte('\n')
			}
		}
		body = bb.String()
	}

	if p.Diff != "" {
		var df strings.Builder
		df.WriteByte('\n')
		df.WriteString(formatDiff(replaceControlChars(p.Diff), width))
		diff = df.String()
	}

	return headers, body, diff
}

func formatDiff(diff string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		line = expandTabs(line, 8)
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "--- "):
			style = diffHeaderStyle
		case strings.HasPrefix(line, "+"):
			style = diffAddStyle
		case strings.HasPrefix(line, "-"):
			style = diffDelStyle
		case strings.HasPrefix(line, "@@"):
			style = diffHunkStyle
		case strings.HasPrefix(line, "diff --git"):
			style = diffHeaderStyle
		default:
			style = plainTextStyle
		}
		for _, wl := range wrapLine(line, width) {
			b.WriteString(style.Render(wl))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// wrapHeaderValue wraps a header value to fit within the given width.
// Splits on commas first (natural delimiter for email lists), then
// falls back to spaces for long segments without commas. Accumulates
// chunks until adding the next one would exceed the width.
func wrapHeaderValue(s string, width int) []string {
	if len(s) <= width {
		return []string{s}
	}
	// Split on ", " to get natural chunks (email addresses)
	parts := strings.Split(s, ", ")
	var lines []string
	var current string
	for _, part := range parts {
		if current == "" {
			current = part
		} else if len(current)+len(", ")+len(part) <= width-1 {
			// -1 reserves space for trailing comma on flush
			current += ", " + part
		} else {
			lines = append(lines, current+",")
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	// Handle individual lines that are still too long (single
	// very long email address or non-comma-separated value)
	var result []string
	for _, line := range lines {
		for len(line) > width {
			cut := width
			if idx := strings.LastIndex(line[:width], " "); idx > width/2 {
				cut = idx
			}
			result = append(result, line[:cut])
			line = strings.TrimSpace(line[cut:])
		}
		result = append(result, line)
	}
	return result
}

// replaceControlChars converts control characters to caret notation
// (e.g., ^L for form feed). Adding 0x40 maps the control code to its
// letter: 0x0C + 0x40 = 'L'. Prevents terminal rendering glitches from
// characters like form feed (^L) in C source diffs.
func replaceControlChars(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' {
			b.WriteByte('^')
			b.WriteRune(r + 0x40)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatDate(s string) string {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return s
	}
	return t.Format("Mon, 02 Jan 2006 15:04:05 +0000")
}

func isQuotedLine(s string) bool {
	return strings.HasPrefix(strings.TrimLeft(s, " "), ">")
}

// expandTabs replaces tab characters with spaces using position-aware
// 8-column tab stops, matching terminal defaults and git diff output.
// Lipgloss uses fixed 4-space tab replacement which misaligns lines
// with different numbers of characters before the tab (e.g., diff
// context lines with "  \t" vs +/- lines with "+\t").
func expandTabs(s string, tabWidth int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var buf strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			spaces := tabWidth - (col % tabWidth)
			buf.WriteString(strings.Repeat(" ", spaces))
			col += spaces
		} else if r == '\n' {
			buf.WriteRune(r)
			col = 0
		} else {
			buf.WriteRune(r)
			col++
		}
	}
	return buf.String()
}

func writeStyledLine(b *strings.Builder, line string, quoted bool) {
	line = expandTabs(line, 8) // position-aware tabs before lipgloss
	if quoted {
		b.WriteString(quotedLineStyle.Render(line))
	} else if strings.HasPrefix(line, "↳ ") {
		b.WriteString(wrapIndicatorStyle.Render("↳ "))
		b.WriteString(plainTextStyle.Render(line[len("↳ "):]))
	} else {
		b.WriteString(plainTextStyle.Render(line))
	}
}

const (
	collapseMinBlock  = 8  // don't collapse quotes shorter than this
	collapseHead      = 3  // show first N lines of a collapsed quote
	collapseTailFall  = 20 // tail lines shown when no @@ hunk header found
	collapseHeaderMax = 3  // max header lines before collapsing To/Cc
)

func collapseQuotedBlocks(lines []string) []string {
	var result []string
	i := 0
	for i < len(lines) {
		if !isQuotedLine(lines[i]) {
			result = append(result, lines[i])
			i++
			continue
		}
		blockStart := i
		for i < len(lines) && isQuotedLine(lines[i]) {
			i++
		}
		block := lines[blockStart:i]
		if len(block) <= collapseMinBlock {
			result = append(result, block...)
			continue
		}
		tail := collapseTail(block)
		head := collapseHead
		if head+len(tail) >= len(block) {
			result = append(result, block...)
			continue
		}
		hidden := len(block) - head - len(tail)
		marker := fmt.Sprintf(
			"  ··· %d quoted lines hidden ···", hidden)
		result = append(result, block[:head]...)
		result = append(result, marker)
		result = append(result, tail...)
	}
	return result
}

func collapseTail(block []string) []string {
	// Search backward for a diff hunk header — the most useful anchor
	// point when a reply quotes a diff. Fall back to the last N lines.
	for i := len(block) - 1; i >= 0; i-- {
		trimmed := strings.TrimLeft(block[i], " >")
		if strings.HasPrefix(trimmed, "@@ ") {
			return block[i:]
		}
	}
	if len(block) <= collapseTailFall {
		return block
	}
	return block[len(block)-collapseTailFall:]
}

func wrapLine(s string, width int) []string {
	runes := []rune(s)
	if len(runes) <= width {
		return []string{s}
	}
	if width < 5 {
		return []string{truncateLine(s, width)}
	}
	var lines []string
	first := true
	for len(runes) > 0 {
		limit := width
		if !first {
			limit = width - 2
		}
		if limit > len(runes) {
			limit = len(runes)
		}
		if limit < len(runes) {
			lookback := limit / 2
			for i := limit; i > limit-lookback && i > 0; i-- {
				if runes[i] == ' ' {
					limit = i + 1
					break
				}
			}
		}
		seg := string(runes[:limit])
		if !first {
			seg = "↳ " + seg
		}
		lines = append(lines, seg)
		runes = runes[limit:]
		first = false
	}
	return lines
}

func wrapLogLine(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	if width < 5 {
		return []string{truncateLine(s, width)}
	}

	var lines []string
	cur := words[0]
	for _, word := range words[1:] {
		if len([]rune(cur))+1+len([]rune(word)) <= width {
			cur += " " + word
			continue
		}
		lines = append(lines, cur)
		cur = "↳ " + word
	}
	lines = append(lines, cur)

	var result []string
	for _, line := range lines {
		runes := []rune(line)
		for len(runes) > width {
			result = append(result, string(runes[:width]))
			runes = append([]rune("↳ "), runes[width:]...)
		}
		result = append(result, string(runes))
	}
	return result
}

func truncateLine(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width < 2 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

type CheckInfo struct {
	Context     string
	State       string
	TargetURL   string
	Description string
}

func FormatChecks(
	checks []CheckInfo, width int, collapse bool,
) string {
	if len(checks) == 0 {
		return ""
	}

	// Sort into groups, alphabetical by context within each.
	var fails, warns, succs, pends []CheckInfo
	for _, c := range checks {
		switch c.State {
		case "fail":
			fails = append(fails, c)
		case "warning":
			warns = append(warns, c)
		case "success":
			succs = append(succs, c)
		default:
			pends = append(pends, c)
		}
	}
	byCtx := func(s []CheckInfo) {
		sort.Slice(s, func(i, j int) bool {
			return s[i].Context < s[j].Context
		})
	}
	byCtx(fails)
	byCtx(warns)
	byCtx(succs)
	byCtx(pends)

	maxCtx := 0
	for _, c := range checks {
		if len(c.Context) > maxCtx {
			maxCtx = len(c.Context)
		}
	}

	var b strings.Builder
	b.WriteString(mboxHeaderLabel.Render("Checks:"))
	b.WriteByte('\n')

	indent := "      "
	writeCheck := func(c CheckInfo) {
		icon := "?"
		style := checksPendingStyle
		switch c.State {
		case "success":
			icon = "✓"
			style = checksPassStyle
		case "fail":
			icon = "✗"
			style = checksFailStyle
		case "warning":
			icon = "!"
			style = checksWarnStyle
		}
		prefix := fmt.Sprintf("  %s %-*s", icon, maxCtx, c.Context)
		b.WriteString(style.Render(prefix))
		if c.TargetURL != "" {
			b.WriteByte('\n')
			url := indent + c.TargetURL
			b.WriteString(plainTextStyle.Render(
				truncateLine(url, width)))
		}
		b.WriteByte('\n')
		if c.Description != "" {
			for _, dl := range strings.Split(c.Description, "\n") {
				dl = strings.TrimSpace(dl)
				if dl == "" {
					continue
				}
				for _, wl := range wrapLine(dl, width-len(indent)) {
					b.WriteString(
						plainTextStyle.Render(indent + wl))
					b.WriteByte('\n')
				}
			}
		}
	}

	// Failures and warnings — always shown in full.
	for _, c := range fails {
		writeCheck(c)
	}
	for _, c := range warns {
		writeCheck(c)
	}

	// Successes — collapse to 3 when there are many.
	if collapse && len(succs) > collapseHeaderMax {
		for _, c := range succs[:collapseHeaderMax] {
			writeCheck(c)
		}
		marker := fmt.Sprintf(
			"··· %d successful checks total (e to expand) ···",
			len(succs))
		b.WriteString("  " + quotedLineStyle.Render(marker))
		b.WriteByte('\n')
	} else {
		for _, c := range succs {
			writeCheck(c)
		}
	}

	// Pending — hide entirely when collapsed.
	if collapse && len(pends) > 0 {
		marker := fmt.Sprintf(
			"··· %d checks pending (e to expand) ···",
			len(pends))
		b.WriteString("  " + quotedLineStyle.Render(marker))
		b.WriteByte('\n')
	} else {
		for _, c := range pends {
			writeCheck(c)
		}
	}

	return b.String()
}

type CommentInfo struct {
	ID             int
	Submitter      string
	SubmitterEmail string
	Date           string
	Subject        string
	Content        string
	Headers        string
	WebURL         string
	ListArchiveURL string
}

func FormatComment(c CommentInfo, width int, collapse bool) string {
	var b strings.Builder
	labelWidth := 9
	valWidth := width - labelWidth
	if valWidth < 20 {
		valWidth = 20
	}

	subject := decodeHeader(compactHeader(extractHeader(c.Headers, "Subject")))
	if subject == "" {
		subject = compactHeader(c.Subject)
	}
	writeHeader(&b, "Subject: ", subject, false, labelWidth, valWidth)

	// Reply-To → API submitter → From. The raw From header is often
	// mangled by mailing lists ("Name via dev <list@example>"), so
	// API submitter (resolved by Patchwork) is preferred. From is
	// the last resort when neither Reply-To nor submitter exist.
	from := decodeHeader(compactHeader(extractHeader(c.Headers, "Reply-To")))
	if from == "" {
		from = formatFrom(c.Submitter, c.SubmitterEmail)
	}
	if from == "" {
		from = decodeHeader(compactHeader(extractHeader(c.Headers, "From")))
	}
	writeHeader(&b, "From:    ", from, false, labelWidth, valWidth)

	to := decodeHeader(compactHeader(extractHeader(c.Headers, "To")))
	writeHeader(&b, "To:      ", to, collapse, labelWidth, valWidth)

	cc := decodeHeader(compactHeader(extractHeader(c.Headers, "Cc")))
	writeHeader(&b, "Cc:      ", cc, collapse, labelWidth, valWidth)

	date := compactHeader(extractHeader(c.Headers, "Date"))
	if date == "" {
		date = formatDate(c.Date)
	}
	writeHeader(&b, "Date:    ", date, false, labelWidth, valWidth)

	url := c.ListArchiveURL
	if url == "" {
		url = c.WebURL
	}
	writeHeader(&b, "URL:     ", url, false, labelWidth, valWidth)

	if c.Content != "" {
		b.WriteByte('\n')
		content := replaceControlChars(c.Content)
		contentLines := strings.Split(content, "\n")
		if collapse {
			contentLines = collapseQuotedBlocks(contentLines)
		}
		for _, line := range contentLines {
			quoted := isQuotedLine(line)
			for _, wl := range wrapLine(line, width) {
				writeStyledLine(&b, wl, quoted)
				b.WriteByte('\n')
			}
		}
	}

	return b.String()
}
