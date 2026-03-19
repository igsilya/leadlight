package tui

import (
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	mboxHeaderLabel = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("15"))
	mboxHeaderValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("34"))
	diffDelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
	diffHunkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("6"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)
	quotedLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("168"))
	wrapIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("242"))
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

func ParseMbox(raw string) ParsedMbox {
	headers, body := splitHeadersBody(raw)
	p := ParsedMbox{
		Subject: decodeHeader(extractHeader(headers, "Subject")),
		From:    decodeHeader(extractHeader(headers, "From")),
		To:      decodeHeader(extractHeader(headers, "To")),
		Cc:      decodeHeader(extractHeader(headers, "Cc")),
		Date:    extractHeader(headers, "Date"),
	}
	p.Body, p.Diff = splitBodyDiff(body)
	return p
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
			result = append(result,
				strings.TrimSpace(line[len(prefix):]))
		} else if found && len(line) > 0 &&
			(line[0] == ' ' || line[0] == '\t') {
			result = append(result, strings.TrimSpace(line))
		} else {
			found = false
		}
	}
	return strings.Join(result, " ")
}

func splitBodyDiff(body string) (string, string) {
	markers := []string{"\ndiff --git ", "\n--- a/", "\n---\n"}
	for _, m := range markers {
		if idx := strings.Index(body, m); idx >= 0 {
			return strings.TrimSpace(body[:idx]),
				strings.TrimRight(body[idx+1:], "\n")
		}
	}
	return strings.TrimSpace(body), ""
}

func FormatMbox(p ParsedMbox, width int) string {
	var b strings.Builder
	labelWidth := 9 // "Subject: " length
	valWidth := width - labelWidth
	if valWidth < 20 {
		valWidth = 20
	}

	writeHeader := func(label, value string) {
		b.WriteString(mboxHeaderLabel.Render(label))
		lines := wrapHeaderValue(value, valWidth)
		for i, line := range lines {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", labelWidth))
			}
			b.WriteString(mboxHeaderValue.Render(line))
			b.WriteByte('\n')
		}
	}

	if p.Subject != "" {
		writeHeader("Subject: ", p.Subject)
	}
	if p.From != "" {
		writeHeader("From:    ", p.From)
	}
	if p.To != "" {
		writeHeader("To:      ", p.To)
	}
	if p.Cc != "" {
		writeHeader("Cc:      ", p.Cc)
	}
	if p.Date != "" {
		writeHeader("Date:    ", p.Date)
	}

	if p.Body != "" {
		b.WriteByte('\n')
		body := replaceControlChars(p.Body)
		for _, line := range strings.Split(body, "\n") {
			quoted := isQuotedLine(line)
			for _, wl := range wrapLine(line, width) {
				writeStyledLine(&b, wl, quoted)
				b.WriteByte('\n')
			}
		}
	}

	if p.Diff != "" {
		b.WriteByte('\n')
		b.WriteString(formatDiff(replaceControlChars(p.Diff), width))
	}

	return b.String()
}

func formatDiff(diff string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		line = truncateLine(line, width)
		switch {
		case strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "--- "):
			b.WriteString(diffHeaderStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffDelStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(diffHunkStyle.Render(line))
		case strings.HasPrefix(line, "diff --git"):
			b.WriteString(diffHeaderStyle.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func wrapHeaderValue(s string, width int) []string {
	runes := []rune(s)
	if len(runes) <= width {
		return []string{s}
	}
	var lines []string
	for len(runes) > 0 {
		end := width
		if end > len(runes) {
			end = len(runes)
		}
		// Try to break at a comma or space
		if end < len(runes) {
			lookback := end / 2
			for i := end; i > end-lookback && i > 0; i-- {
				if runes[i] == ',' || runes[i] == ' ' {
					end = i + 1
					break
				}
			}
		}
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
	}
	return lines
}

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

func writeStyledLine(b *strings.Builder, line string, quoted bool) {
	if quoted {
		b.WriteString(quotedLineStyle.Render(line))
	} else if strings.HasPrefix(line, "↳ ") {
		b.WriteString(wrapIndicatorStyle.Render("↳ "))
		b.WriteString(line[len("↳ "):])
	} else {
		b.WriteString(line)
	}
}

const (
	collapseMinBlock = 8
	collapseHead     = 3
	collapseTailFall = 20
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
	if width < 3 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

type CheckInfo struct {
	Context   string
	State     string
	TargetURL string
}

func FormatChecks(checks []CheckInfo, width int) string {
	if len(checks) == 0 {
		return ""
	}

	maxCtx := 0
	for _, c := range checks {
		if len(c.Context) > maxCtx {
			maxCtx = len(c.Context)
		}
	}

	var b strings.Builder
	b.WriteString(mboxHeaderLabel.Render("Checks:"))
	b.WriteByte('\n')
	for _, c := range checks {
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
			style = checksPendingStyle
		}
		line := fmt.Sprintf("  %s %-*s", icon, maxCtx, c.Context)
		if c.TargetURL != "" {
			line += "  " + c.TargetURL
		}
		line = truncateLine(line, width)
		b.WriteString(style.Render(line))
		b.WriteByte('\n')
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

func FormatComment(c CommentInfo, width int, collapseQuotes bool) string {
	var b strings.Builder
	labelWidth := 9
	valWidth := width - labelWidth
	if valWidth < 20 {
		valWidth = 20
	}

	writeHeader := func(label, value string) {
		b.WriteString(mboxHeaderLabel.Render(label))
		lines := wrapHeaderValue(value, valWidth)
		for i, line := range lines {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", labelWidth))
			}
			b.WriteString(mboxHeaderValue.Render(line))
			b.WriteByte('\n')
		}
	}

	if c.Subject != "" {
		writeHeader("Subject: ", c.Subject)
	}

	from := c.Submitter
	if c.SubmitterEmail != "" {
		from += " <" + c.SubmitterEmail + ">"
	}
	if from != "" {
		writeHeader("From:    ", from)
	}

	to := decodeHeader(extractHeader(c.Headers, "To"))
	if to != "" {
		writeHeader("To:      ", to)
	}

	cc := decodeHeader(extractHeader(c.Headers, "Cc"))
	if cc != "" {
		writeHeader("Cc:      ", cc)
	}

	date := extractHeader(c.Headers, "Date")
	if date == "" {
		date = formatDate(c.Date)
	}
	if date != "" {
		writeHeader("Date:    ", date)
	}

	url := c.ListArchiveURL
	if url == "" {
		url = c.WebURL
	}
	if url != "" {
		writeHeader("URL:     ", url)
	}

	if c.Content != "" {
		b.WriteByte('\n')
		content := replaceControlChars(c.Content)
		contentLines := strings.Split(content, "\n")
		if collapseQuotes {
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

func FormatMboxError(patchName string, err error) string {
	return fmt.Sprintf(
		"%s\n\n%s",
		mboxHeaderLabel.Render("Error fetching: "+patchName),
		err.Error())
}
