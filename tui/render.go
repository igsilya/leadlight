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

	if m.viewMode == viewPatch {
		return m.renderPatchView()
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

func (m *Model) viewportVisibleLines() int {
	v := m.height - 1
	if v < 1 {
		v = 1
	}
	return v
}

func (m *Model) renderPatchView() string {
	visible := m.viewportVisibleLines()
	total := len(m.viewportLines)

	start := m.viewportOffset
	if start > total {
		start = total
	}
	end := start + visible
	if end > total {
		end = total
	}

	content := strings.Join(m.viewportLines[start:end], "\n")
	body := lipgloss.NewStyle().
		Width(m.width).
		Height(visible).
		Render(content)

	pct := 0
	maxOff := total - visible
	if maxOff > 0 {
		pct = m.viewportOffset * 100 / maxOff
	}
	status := helpStyle.Render(fmt.Sprintf(
		"↑/↓ scroll | pgup/pgdn page | esc back  %d%%",
		pct))

	return body + "\n" + status
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

func (m *Model) maxVisibleRows() int {
	bottomLines := 1
	if m.selectorMode != selectorNone {
		bottomLines = 2
	}
	rows := m.height - 3 - bottomLines
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) renderRows(
	out *strings.Builder,
	items []visibleItem,
	widths []int,
) {
	rendered := 0
	maxRows := m.maxVisibleRows()

	indicator := "▸ "
	blank := strings.Repeat(" ", indicatorWidth)

	checksStart := indicatorWidth
	for c := 0; c < m.ChecksColIdx && c < len(widths); c++ {
		checksStart += widths[c]
	}
	checksWidth := 0
	if m.ChecksColIdx >= 0 && m.ChecksColIdx < len(widths) {
		checksWidth = widths[m.ChecksColIdx]
	}

	if !m.cacheValid {
		m.cachedRows = make([]string, len(items))
		m.cacheValid = true
	}

	for i := m.scrollOffset; i < len(items); i++ {
		if rendered >= maxRows {
			break
		}

		var row string
		if i == m.selectedRow {
			if m.selectorMode != selectorNone {
				row = indicator + m.buildRow(
					items[i], widths, m.StatusColIdx)
			} else {
				row = m.renderSelectedRow(
					items[i], widths, indicator,
					checksStart, checksWidth)
			}
		} else if i < len(m.cachedRows) && m.cachedRows[i] != "" {
			row = m.cachedRows[i]
		} else {
			row = m.buildStyledRow(items[i], widths, blank)
			if i < len(m.cachedRows) {
				m.cachedRows[i] = row
			}
		}

		out.WriteString(row)
		out.WriteByte('\n')
		rendered++
	}

	m.lastRowsVisible = rendered
}

func (m *Model) buildRawRow(
	item visibleItem, widths []int,
) string {
	var b strings.Builder
	for j, cellData := range item.data {
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		b.WriteString(renderCell(text, widths[j]))
	}
	return b.String()
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
		if j == highlightCol {
			cell := renderCell(text, widths[j])
			b.WriteString(cellHighlightStyle.Render(cell))
		} else if j == m.ChecksColIdx {
			b.WriteString(renderChecksCellWithBg(
				text, widths[j], item.style.Background))
		} else {
			b.WriteString(renderCell(text, widths[j]))
		}
	}
	return b.String()
}

func (m *Model) buildStyledRow(
	item visibleItem, widths []int, prefix string,
) string {
	rowStyle := item.style.lipgloss(item.isSubRow)
	var b strings.Builder
	b.WriteString(rowStyle.Render(prefix))
	for j, cellData := range item.data {
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		if j == m.ChecksColIdx {
			b.WriteString(renderChecksCellWithBg(
				text, widths[j], item.style.Background))
		} else {
			cell := renderCell(text, widths[j])
			b.WriteString(rowStyle.Render(cell))
		}
	}
	return b.String()
}

func (m *Model) renderSelectedRow(
	item visibleItem, widths []int, indicator string,
	checksStart, checksWidth int,
) string {
	raw := m.buildRawRow(item, widths)
	fullRaw := indicator + raw
	bgName := item.style.Background

	if m.ChecksColIdx < 0 || checksWidth == 0 {
		return m.renderGradientRow(fullRaw, bgName)
	}

	runes := []rune(fullRaw)
	total := len(runes)

	if checksStart >= total {
		return m.renderGradientRow(fullRaw, bgName)
	}

	checksEnd := checksStart + checksWidth
	if checksEnd > total {
		checksEnd = total
	}

	prefix := string(runes[:checksStart])
	checksText := item.data[m.ChecksColIdx]
	suffix := ""
	if checksEnd < total {
		suffix = string(runes[checksEnd:])
	}

	gradientPart := m.renderGradientRow(prefix, bgName)
	checksPart := renderChecksCellWithBg(
		checksText, checksWidth, bgName)
	suffixPart := ""
	if suffix != "" {
		if cached, ok := bgStyles[bgName]; ok {
			suffixPart = cached.row.Render(suffix)
		} else {
			suffixPart = suffix
		}
	}

	return gradientPart + checksPart + suffixPart
}

func renderChecksCellWithBg(text string, width int, bgName string) string {
	cached := bgStyles[bgName]

	if text == "-" || text == "" {
		if cached != nil {
			return cached.checkZero.Render(
				renderCell("-", width))
		}
		return checksZeroStyle.Render(renderCell("-", width))
	}

	parts := strings.SplitN(text, "/", 3)
	if len(parts) != 3 {
		if cached != nil {
			return cached.row.Render(renderCell(text, width))
		}
		return renderCell(text, width)
	}

	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			if cached != nil {
				b.WriteString(cached.checkZero.Render("/"))
			} else {
				b.WriteString(checksZeroStyle.Render("/"))
			}
		}
		if part == "0" {
			if cached != nil {
				b.WriteString(cached.checkZero.Render(part))
			} else {
				b.WriteString(checksZeroStyle.Render(part))
			}
		} else if cached != nil {
			switch i {
			case 0:
				b.WriteString(cached.checkPass.Render(part))
			case 1:
				b.WriteString(cached.checkFail.Render(part))
			case 2:
				b.WriteString(cached.checkPend.Render(part))
			}
		} else {
			styles := []lipgloss.Style{
				checksPassStyle, checksFailStyle,
				checksPendingStyle,
			}
			b.WriteString(styles[i].Render(part))
		}
	}

	rendered := b.String()
	visualWidth := lipgloss.Width(rendered)
	if visualWidth < width {
		if cached != nil {
			rendered += cached.row.Render(
				strings.Repeat(" ", width-visualWidth))
		} else {
			rendered += strings.Repeat(" ", width-visualWidth)
		}
	}
	return rendered
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

	if cached, ok := bgStyles[bgName]; ok && gradientWidth < total {
		b.WriteString(cached.row.Render(string(runes[gradientWidth:])))
	} else if gradientWidth < total {
		b.WriteString(string(runes[gradientWidth:]))
	}
	return b.String()
}

func (m *Model) padToBottom(out *strings.Builder) {
	bottomLines := 1
	if m.selectorMode != selectorNone {
		bottomLines = 2
	}
	current := lipgloss.Height(out.String())
	remaining := m.height - current - bottomLines
	for i := 0; i < remaining; i++ {
		out.WriteByte('\n')
	}
}

func (m *Model) renderStatusBar(out *strings.Builder) {
	if m.selectorMode != selectorNone {
		m.renderSelectorBar(out)
		return
	}

	filterLabel := "[" + strings.Join(m.states, ", ") + "]"
	toggleHint := "a all"
	if m.showAll {
		filterLabel = "[all]"
		toggleHint = "a active"
	}

	help := helpStyle.Render(
		filterLabel + " q quit | ↑/↓ pgup/dn navigate" +
			" | enter expand | s state | d delegate | " + toggleHint)

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
	prefix := "Status"
	if m.selectorMode == selectorDelegate {
		prefix = "Delegate"
	}
	if m.selectorFilter != "" {
		prefix += " [" + m.selectorFilter + "]"
	}
	prefix += ": "

	filtered, _ := m.filteredOptions()

	var parts []string
	for i, opt := range filtered {
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
		helpStyle.Render(prefix) + strings.Join(parts, sep))
	out.WriteByte('\n')
	hint := "←/→ select, enter confirm, esc "
	if m.selectorFilter != "" {
		hint += "clear filter"
	} else {
		hint += "cancel"
	}
	if m.selectorMode == selectorDelegate {
		hint += ", type to filter"
	}
	out.WriteString(helpStyle.Render(hint))
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
