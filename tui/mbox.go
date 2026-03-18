package tui

import (
	"fmt"
	"mime"
	"strings"

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
)

type ParsedMbox struct {
	Subject string
	From    string
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
	if p.Cc != "" {
		writeHeader("Cc:      ", p.Cc)
	}
	if p.Date != "" {
		writeHeader("Date:    ", p.Date)
	}

	if p.Body != "" {
		b.WriteByte('\n')
		for _, line := range strings.Split(p.Body, "\n") {
			b.WriteString(truncateLine(line, width))
			b.WriteByte('\n')
		}
	}

	if p.Diff != "" {
		b.WriteByte('\n')
		b.WriteString(formatDiff(p.Diff, width))
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
			for i := end; i > end-20 && i > 0; i-- {
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
	ID        int
	Submitter string
	Date      string
	Subject   string
	Content   string
}

func FormatComment(c CommentInfo, width int) string {
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
	if c.Submitter != "" {
		writeHeader("From:    ", c.Submitter)
	}
	if c.Date != "" {
		writeHeader("Date:    ", c.Date)
	}

	if c.Content != "" {
		b.WriteByte('\n')
		for _, line := range strings.Split(c.Content, "\n") {
			b.WriteString(truncateLine(line, width))
			b.WriteByte('\n')
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
