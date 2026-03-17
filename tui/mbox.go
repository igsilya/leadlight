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

	if p.Subject != "" {
		b.WriteString(mboxHeaderLabel.Render("Subject: "))
		b.WriteString(mboxHeaderValue.Render(
			truncateLine(p.Subject, valWidth)))
		b.WriteByte('\n')
	}
	if p.From != "" {
		b.WriteString(mboxHeaderLabel.Render("From:    "))
		b.WriteString(mboxHeaderValue.Render(
			truncateLine(p.From, valWidth)))
		b.WriteByte('\n')
	}
	if p.Cc != "" {
		b.WriteString(mboxHeaderLabel.Render("Cc:      "))
		b.WriteString(mboxHeaderValue.Render(
			truncateLine(p.Cc, valWidth)))
		b.WriteByte('\n')
	}
	if p.Date != "" {
		b.WriteString(mboxHeaderLabel.Render("Date:    "))
		b.WriteString(mboxHeaderValue.Render(
			truncateLine(p.Date, valWidth)))
		b.WriteByte('\n')
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

func FormatMboxError(patchName string, err error) string {
	return fmt.Sprintf(
		"%s\n\n%s",
		mboxHeaderLabel.Render("Error fetching: "+patchName),
		err.Error())
}
