package tui

import (
	"fmt"
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
		Subject: extractHeader(headers, "Subject"),
		From:    extractHeader(headers, "From"),
		Cc:      extractHeader(headers, "Cc"),
		Date:    extractHeader(headers, "Date"),
	}
	p.Body, p.Diff = splitBodyDiff(body)
	return p
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

func FormatMbox(p ParsedMbox) string {
	var b strings.Builder

	if p.Subject != "" {
		b.WriteString(mboxHeaderLabel.Render("Subject: "))
		b.WriteString(mboxHeaderValue.Render(p.Subject))
		b.WriteByte('\n')
	}
	if p.From != "" {
		b.WriteString(mboxHeaderLabel.Render("From:    "))
		b.WriteString(mboxHeaderValue.Render(p.From))
		b.WriteByte('\n')
	}
	if p.Cc != "" {
		b.WriteString(mboxHeaderLabel.Render("Cc:      "))
		b.WriteString(mboxHeaderValue.Render(p.Cc))
		b.WriteByte('\n')
	}
	if p.Date != "" {
		b.WriteString(mboxHeaderLabel.Render("Date:    "))
		b.WriteString(mboxHeaderValue.Render(p.Date))
		b.WriteByte('\n')
	}

	if p.Body != "" {
		b.WriteByte('\n')
		b.WriteString(p.Body)
		b.WriteByte('\n')
	}

	if p.Diff != "" {
		b.WriteByte('\n')
		b.WriteString(formatDiff(p.Diff))
	}

	return b.String()
}

func formatDiff(diff string) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
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

func FormatMboxError(patchName string, err error) string {
	return fmt.Sprintf(
		"%s\n\n%s",
		mboxHeaderLabel.Render("Error fetching: "+patchName),
		err.Error())
}
