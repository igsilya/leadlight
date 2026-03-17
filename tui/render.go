package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	widths := m.columnWidths()
	var out strings.Builder

	m.renderHeader(&out, widths)

	m.mu.Lock()
	items := m.getVisibleItems()
	m.mu.Unlock()

	m.renderRows(&out, items, widths)
	m.padToBottom(&out)
	m.renderStatusBar(&out)

	return out.String()
}

func (m *Model) renderHeader(out *strings.Builder, widths []int) {
	out.WriteString(strings.Repeat(" ", indicatorWidth))
	for i, col := range m.ColumnDefs {
		cell := renderCell(col.Title, widths[i])
		out.WriteString(headerStyle.Render(cell))
	}
	out.WriteByte('\n')

	sep := lipgloss.NewStyle().Width(m.width).Render("─")
	out.WriteString(separatorStyle.Render(sep))
	out.WriteByte('\n')
}

func (m *Model) renderRows(
	out *strings.Builder,
	items []visibleItem,
	widths []int,
) {
	var rows strings.Builder
	rendered := 0
	base := out.String()

	indicator := "▸ "
	blank := strings.Repeat(" ", indicatorWidth)

	for i := m.scrollOffset; i < len(items); i++ {
		raw := m.buildRawRow(items[i], widths)

		var row string
		if i == m.selectedRow {
			if m.selectorOpen {
				row = indicator + m.buildRow(
					items[i], widths, m.StatusColIdx)
			} else {
				row = m.renderGradientRow(
					indicator+raw, items[i].style.Background)
			}
		} else {
			row = items[i].style.lipgloss(items[i].isSubRow).
				Render(blank + raw)
		}

		test := base + rows.String() + row + "\n"
		if lipgloss.Height(test) >= m.height-1 &&
			i > m.scrollOffset {
			break
		}

		rows.WriteString(row)
		rows.WriteByte('\n')
		rendered++
	}

	m.lastRowsVisible = rendered
	out.WriteString(rows.String())
}

func (m *Model) buildRawRow(
	item visibleItem, widths []int,
) string {
	return m.buildRow(item, widths, -1)
}

func (m *Model) buildRow(
	item visibleItem, widths []int, highlightCol int,
) string {
	var b strings.Builder
	for j, cellData := range item.data {
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		cell := renderCell(text, widths[j])
		if j == highlightCol {
			b.WriteString(cellHighlightStyle.Render(cell))
		} else {
			b.WriteString(cell)
		}
	}
	return b.String()
}

func (m *Model) renderGradientRow(
	rawRow string, bgName string,
) string {
	runes := []rune(rawRow)
	total := len(runes)
	fill := min(
		int(m.highlightProgress*float64(total)), total)

	palette, ok := gradientPalettes[bgName]
	if !ok {
		palette = gradientPalettes["black"]
	}

	gradientWidth := max(fill*75/100, 1)

	var b strings.Builder
	b.Grow(total * 30)

	for pos := 0; pos < gradientWidth && pos < total; pos++ {
		idx := pos * 255 / max(gradientWidth-1, 1)
		entry := palette[idx]
		style := lipgloss.NewStyle().
			Background(lipgloss.Color(entry.bg)).
			Foreground(lipgloss.Color(entry.fg)).
			Bold(true)
		b.WriteString(style.Render(string(runes[pos : pos+1])))
	}

	if c, ok := bgColors[bgName]; ok && gradientWidth < total {
		restStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(
				fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b))).
			Foreground(lipgloss.Color(
				fmt.Sprintf("#%02x%02x%02x", c.fgR, c.fgG, c.fgB)))
		b.WriteString(restStyle.Render(string(runes[gradientWidth:])))
	} else if gradientWidth < total {
		b.WriteString(string(runes[gradientWidth:]))
	}
	return b.String()
}

func (m *Model) padToBottom(out *strings.Builder) {
	bottomLines := 1
	if m.selectorOpen {
		bottomLines = 2
	}
	current := lipgloss.Height(out.String())
	remaining := m.height - current - bottomLines
	for i := 0; i < remaining; i++ {
		out.WriteByte('\n')
	}
}

func (m *Model) renderStatusBar(out *strings.Builder) {
	if m.selectorOpen {
		m.renderSelectorBar(out)
		return
	}

	help := helpStyle.Render(
		"Press q to quit | ↑/↓ to navigate" +
			" | enter to expand/collapse | d to change status")

	if m.status == "" {
		out.WriteString(help)
		return
	}

	spinner := spinnerFrames[m.spinnerFrame]
	status := statusStyle.Render(
		fmt.Sprintf("%s %s", spinner, m.status))

	gap := m.width - lipgloss.Width(help) - lipgloss.Width(status)
	if gap < 2 {
		gap = 2
	}

	out.WriteString(help)
	out.WriteString(lipgloss.NewStyle().Width(gap).Render(""))
	out.WriteString(status)
}

func (m *Model) renderSelectorBar(out *strings.Builder) {
	var parts []string
	for i, opt := range m.StatusOptions {
		num := optionNumStyle.Render(fmt.Sprintf("%d", i+1))
		label := " " + opt + " "
		if i == m.selectorCursor {
			parts = append(parts,
				num+highlightedOptionStyle.Render(label))
		} else {
			parts = append(parts,
				num+normalOptionStyle.Render(label))
		}
	}
	sep := helpStyle.Render(" │ ")
	out.WriteString(
		helpStyle.Render("Status: ") + strings.Join(parts, sep))
	out.WriteByte('\n')
	out.WriteString(helpStyle.Render(
		"←/→ or 1-4 to select, enter confirm, esc cancel"))
}

func renderCell(text string, width int) string {
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Inline(true).
		Render(truncate(text, width))
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width < 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}
