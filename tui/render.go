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

	top := m.renderMainView()
	if !m.logConsole {
		return top
	}
	consoleHeight := m.height - m.renderHeight()
	return top + "\n" + m.renderLogConsole(consoleHeight)
}

func (m *Model) renderMainView() string {
	if m.viewMode == viewPatch {
		return m.renderPatchView()
	}

	widths := m.columnWidths()
	var out strings.Builder

	m.renderHeader(&out, widths)

	items := m.getVisibleItems()
	m.renderRows(&out, items, widths)
	m.padToBottom(&out)
	m.renderStatusBar(&out)

	return out.String()
}

func (m *Model) viewportVisibleLines() int {
	v := m.renderHeight() - 1
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

	var status string
	if len(m.viewComments) > 0 {
		expandHint := ""
		if m.viewCommentIdx >= 0 {
			if m.quotesExpanded {
				expandHint = "  e collapse"
			} else {
				expandHint = "  e expand"
			}
		}
		hs := m.mainHelp()
		tabHint := ""
		if m.logConsole {
			tabHint = "  tab"
		}
		helpText := hs.Render(fmt.Sprintf(
			" ←/→%s | ↑/↓ pgup/dn | esc%s  %d%%", expandHint, tabHint, pct))
		barWidth := m.width - lipgloss.Width(helpText)
		commentBar := m.renderCommentBar(barWidth)
		status = commentBar + helpText
	} else {
		hs := m.mainHelp()
		tabHint := ""
		if m.logConsole {
			tabHint = " | tab log"
		}
		status = hs.Render(fmt.Sprintf(
			"↑/↓ pgup/dn | esc back%s  %d%%", tabHint, pct))
	}

	status = m.appendActiveStatus(status)
	return body + "\n" + status
}

func (m *Model) renderHeader(out *strings.Builder, widths []int) {
	out.WriteString(strings.Repeat(" ", indicatorWidth))
	for i, col := range m.ColumnDefs {
		if widths[i] == 0 {
			continue
		}
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
	rows := m.renderHeight() - 2 - bottomLines
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
	for c := 0; c < int(m.ChecksColIdx) && c < len(widths); c++ {
		checksStart += widths[c]
	}
	checksWidth := 0
	if m.ChecksColIdx >= 0 && int(m.ChecksColIdx) < len(widths) {
		checksWidth = widths[m.ChecksColIdx]
	}

	// Use whichever comment column is visible (ColC or ColComments)
	cCol := ColC
	if int(ColComments) < len(widths) && widths[ColComments] > 0 {
		cCol = ColComments
	}
	commentStart := indicatorWidth
	for c := 0; c < int(cCol) && c < len(widths); c++ {
		commentStart += widths[c]
	}
	commentWidth := 0
	if int(cCol) < len(widths) {
		commentWidth = widths[cCol]
	}

	afrtStart := indicatorWidth
	for c := 0; c < int(ColAFRT) && c < len(widths); c++ {
		afrtStart += widths[c]
	}
	afrtWidth := 0
	if int(ColAFRT) < len(widths) {
		afrtWidth = widths[ColAFRT]
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
					items[i], widths, m.selectorHighlightCol)
			} else {
				row = m.renderSelectedRow(
					items[i], widths, indicator,
					checksStart, checksWidth,
					afrtStart, afrtWidth,
					commentStart, commentWidth)
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
		if widths[j] == 0 {
			continue
		}
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		b.WriteString(renderCell(text, widths[j]))
	}
	return b.String()
}

func (m *Model) buildRow(
	item visibleItem, widths []int, highlightCol ColIndex,
) string {
	cached := bgStyles[item.style.Background]
	var b strings.Builder
	for j, cellData := range item.data {
		if widths[j] == 0 {
			continue
		}
		col := ColIndex(j)
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		cell := renderCell(text, widths[j])
		if col == highlightCol && highlightCol != ColNone {
			b.WriteString(cellHighlightStyle.Render(cell))
		} else if col == m.ChecksColIdx {
			b.WriteString(renderChecksCellWithBg(
				text, widths[j], item.style.Background))
		} else if col == ColC || col == ColAFRT {
			b.WriteString(renderBoldDimCellWithBg(
				text, widths[j], item.style.Background))
		} else if col == ColComments {
			b.WriteString(renderCommentCellExpanded(
				text, widths[j], item.style.Background))
		} else if cached != nil {
			b.WriteString(cached.row.Render(cell))
		} else {
			b.WriteString(cell)
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
		if widths[j] == 0 {
			continue
		}
		col := ColIndex(j)
		text := cellData
		if item.isSubRow && j == 0 {
			text = subRowIndent + text
		}
		if col == m.ChecksColIdx {
			b.WriteString(renderChecksCellWithBg(
				text, widths[j], item.style.Background))
		} else if col == ColC || col == ColAFRT {
			b.WriteString(renderBoldDimCellWithBg(
				text, widths[j], item.style.Background))
		} else if col == ColComments {
			b.WriteString(renderCommentCellExpanded(
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
	afrtStart, afrtWidth int,
	commentStart, commentWidth int,
) string {
	raw := m.buildRawRow(item, widths)
	fullRaw := indicator + raw
	bgName := item.style.Background
	checksText := ""
	if checksWidth > 0 {
		checksText = item.data[m.ChecksColIdx]
	}
	afrtText := ""
	if afrtWidth > 0 {
		afrtText = item.data[ColAFRT]
	}
	commentText := ""
	if commentWidth > 0 {
		cCol := ColC
		if int(ColComments) < len(widths) && widths[ColComments] > 0 {
			cCol = ColComments
		}
		commentText = item.data[cCol]
	}
	return m.renderGradientRow(
		fullRaw, bgName, checksStart, checksWidth, checksText,
		afrtStart, afrtWidth, afrtText,
		commentStart, commentWidth, commentText, item.isSubRow)
}

func (m *Model) renderGradientRow(
	rawRow, bgName string,
	checksStart, checksWidth int, checksText string,
	afrtStart, afrtWidth int, afrtText string,
	commentStart, commentWidth int, commentText string,
	isSubRow bool,
) string {
	runes := []rune(rawRow)
	total := len(runes)
	fill := min(
		int(m.highlightProgress*float64(total)), total)

	palettes := gradientPalettes
	if isSubRow {
		palettes = subRowGradientPalettes
	}
	palette, ok := palettes[bgName]
	if !ok {
		palette = palettes["black"]
	}

	leftWidth := max(fill*40/100, 1)
	rightWidth := max(fill*40/100, 1)
	rightStart := total - rightWidth

	checkColors := buildCheckColors(checksText)
	checksEnd := checksStart + checksWidth

	afrtColors := buildBoldDimColors(afrtText)
	afrtEnd := afrtStart + afrtWidth

	commentColors := buildBoldDimColors(commentText)
	commentEnd := commentStart + commentWidth

	cached := bgStyles[bgName]
	var flatBg, flatFg string
	if isSubRow {
		flatBg = termBgHex
	} else if cached != nil {
		flatBg = cached.bgHex
	}
	if cached != nil {
		flatFg = cached.fgHex
	}

	var b strings.Builder
	b.Grow(total * 30)

	for pos := 0; pos < total; pos++ {
		var bg, fg string
		bold := false

		if pos < leftWidth {
			idx := pos * 255 / max(leftWidth-1, 1)
			bg = palette[idx].bg
			fg = palette[idx].fg
			bold = true
		} else if pos >= rightStart && rightStart > leftWidth {
			idx := (total - 1 - pos) * 255 / max(rightWidth-1, 1)
			bg = palette[idx].bg
			fg = palette[idx].fg
			bold = true
		} else {
			bg = flatBg
			fg = flatFg
		}

		if pos >= checksStart && pos < checksEnd {
			ci := pos - checksStart
			if ci < len(checkColors) && checkColors[ci] != "" {
				fg = checkColors[ci]
				bold = true
			}
		}

		if pos >= afrtStart && pos < afrtEnd {
			ai := pos - afrtStart
			if ai < len(afrtColors) {
				if afrtColors[ai] != "" {
					fg = afrtColors[ai]
					bold = false
				} else {
					bold = true
				}
			}
		}

		// Only apply bold/dim overlay for narrow comment column (ColC).
		// Wide Comments column uses gradient/flat styling naturally.
		if commentWidth <= 3 && pos >= commentStart && pos < commentEnd {
			ci := pos - commentStart
			if ci < len(commentColors) {
				if commentColors[ci] != "" {
					fg = commentColors[ci]
					bold = false
				} else {
					bold = true
				}
			}
		}

		style := lipgloss.NewStyle().
			Background(lipgloss.Color(bg)).
			Foreground(lipgloss.Color(fg))
		if bold {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(string(runes[pos : pos+1])))
	}
	return b.String()
}

func renderChecksCellWithBg(text string, width int, bgName string) string {
	cached := bgStyles[bgName]
	if text == "-" || text == "" {
		if cached != nil {
			return cached.checkZero.Render(renderCell("-", width))
		}
		return checksZeroStyle.Render(renderCell("-", width))
	}
	parts := strings.SplitN(text, " ", 3)
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
				b.WriteString(cached.checkZero.Render(" "))
			} else {
				b.WriteString(checksZeroStyle.Render(" "))
			}
		}
		if part == "0" || part == "-" {
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

func buildCheckColors(text string) []string {
	if text == "" || text == "-" {
		return nil
	}
	parts := strings.SplitN(text, " ", 3)
	if len(parts) != 3 {
		return nil
	}
	fgColors := [3]string{
		checkPassColor, checkFailColor, checkPendColor,
	}
	var result []string
	for i, part := range parts {
		if i > 0 {
			result = append(result, checkZeroColor)
		}
		color := fgColors[i]
		if part == "0" || part == "-" {
			color = checkZeroColor
		}
		for range part {
			result = append(result, color)
		}
	}
	return result
}

// renderBoldDimCellWithBg renders a space-separated cell with per-part
// styling: parts that are "-" or "0" are dim, all others are bold in
// the row's fg color. Spaces between parts are dim. Used for ColC and
// ColAFRT. Handles multi-digit numbers correctly ("10" is bold, not
// per-character).
func renderBoldDimCellWithBg(text string, width int, bgName string) string {
	cached := bgStyles[bgName]
	parts := strings.Split(text, " ")
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			if cached != nil {
				b.WriteString(cached.checkZero.Render(" "))
			} else {
				b.WriteString(checksZeroStyle.Render(" "))
			}
		}
		if part == "-" || part == "0" {
			if cached != nil {
				b.WriteString(cached.checkZero.Render(part))
			} else {
				b.WriteString(checksZeroStyle.Render(part))
			}
		} else {
			if cached != nil {
				b.WriteString(cached.rowBold.Render(part))
			} else {
				b.WriteString(afrtNonZeroStyle.Render(part))
			}
		}
	}
	rendered := b.String()
	visualWidth := lipgloss.Width(rendered)
	if visualWidth < width {
		if cached != nil {
			rendered += cached.row.Render(strings.Repeat(" ", width-visualWidth))
		} else {
			rendered += strings.Repeat(" ", width-visualWidth)
		}
	}
	return rendered
}

// renderCommentCellExpanded renders the wide Comments column:
// count prefix as a whole token (bold/dim), names in normal row style.
func renderCommentCellExpanded(text string, width int, bgName string) string {
	cached := bgStyles[bgName]
	spaceIdx := strings.IndexByte(text, ' ')
	countPart := text
	namesPart := ""
	if spaceIdx >= 0 {
		countPart = text[:spaceIdx]
		namesPart = text[spaceIdx:]
	}
	var b strings.Builder
	cell := renderCell(countPart, len(countPart))
	if countPart == "-" {
		if cached != nil {
			b.WriteString(cached.checkZero.Render(cell))
		} else {
			b.WriteString(checksZeroStyle.Render(cell))
		}
	} else {
		if cached != nil {
			b.WriteString(cached.rowBold.Render(cell))
		} else {
			b.WriteString(afrtNonZeroStyle.Render(cell))
		}
	}
	rest := renderCell(namesPart, width-len(countPart))
	if cached != nil {
		b.WriteString(cached.row.Render(rest))
	} else {
		b.WriteString(rest)
	}
	return b.String()
}

// buildBoldDimColors returns per-character fg overrides for the gradient
// row renderer. Splits on spaces and decides per-part: "-" or "0" parts
// get checkZeroColor, other parts get "" (no override = bold). Spaces
// between parts get checkZeroColor. Handles multi-digit numbers
// correctly. Used for ColC and ColAFRT.
func buildBoldDimColors(text string) []string {
	parts := strings.Split(text, " ")
	result := make([]string, 0, len(text))
	for i, part := range parts {
		if i > 0 {
			result = append(result, checkZeroColor)
		}
		color := ""
		if part == "-" || part == "0" {
			color = checkZeroColor
		}
		for range part {
			result = append(result, color)
		}
	}
	return result
}

type barEntry struct {
	label string
	width int // visual width: 1 (number) + 1 (space) + len(label)
}

// renderScrollBar renders a scrollable bar of entries centered on selectedIdx.
// Window is capped at maxVisible entries (9 for number-key support).
// Returns the rendered string and the [lo, hi) range of visible entries.
func renderScrollBar(entries []barEntry, selectedIdx, maxWidth, maxVisible int) (string, int, int) {
	if len(entries) == 0 {
		return "", 0, 0
	}

	sepWidth := 3
	lo, hi := selectedIdx, selectedIdx+1
	used := entries[selectedIdx].width
	for {
		grew := false
		if lo > 0 && hi-lo < maxVisible {
			w := entries[lo-1].width + sepWidth
			if used+w+4 <= maxWidth {
				lo--
				used += w
				grew = true
			}
		}
		if hi < len(entries) && hi-lo < maxVisible {
			w := entries[hi].width + sepWidth
			if used+w+4 <= maxWidth {
				hi++
				used += w
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	sep := helpStyle.Render(" | ")
	var b strings.Builder
	if lo > 0 {
		b.WriteString(helpStyle.Render("◀ "))
	}
	for i := lo; i < hi; i++ {
		if i > lo {
			b.WriteString(sep)
		}
		num := optionNumStyle.Render(fmt.Sprintf("%d", i-lo+1))
		e := entries[i]
		if i == selectedIdx {
			b.WriteString(num + highlightedOptionStyle.Render(" "+e.label))
		} else {
			b.WriteString(num + normalOptionStyle.Render(" "+e.label))
		}
	}
	if hi < len(entries) {
		b.WriteString(helpStyle.Render(" ▶"))
	}
	return b.String(), lo, hi
}

func (m *Model) renderCommentBar(maxWidth int) string {
	entries := make([]barEntry, 0, len(m.viewComments)+1)
	entries = append(entries, barEntry{"patch", 2 + len("patch")})
	for _, c := range m.viewComments {
		name := firstName(c.Submitter)
		if name == "" {
			name = "reply"
		}
		label := name + " (" + formatAge(c.Date) + ")"
		entries = append(entries, barEntry{label, 2 + len(label)})
	}
	selectedIdx := m.viewCommentIdx + 1
	rendered, lo, hi := renderScrollBar(entries, selectedIdx, maxWidth, 9)
	m.commentBarLo = lo
	m.commentBarHi = hi
	return rendered
}

func (m *Model) padToBottom(out *strings.Builder) {
	bottomLines := 1
	if m.selectorMode != selectorNone {
		bottomLines = 2
	}
	target := m.renderHeight() - bottomLines + 1
	current := lipgloss.Height(out.String())
	if current < target {
		out.WriteString(strings.Repeat("\n", target-current))
	}
}

func (m *Model) renderStatusBar(out *strings.Builder) {
	if m.selectorMode != selectorNone {
		m.renderSelectorBar(out)
		return
	}

	hs := m.mainHelp()

	if m.filterMode {
		label := hs.Render("Filter: ")
		text := normalOptionStyle.Render(m.filterText + "_")
		hint := hs.Render("  ↑/↓ pgup/dn | esc clear")
		out.WriteString(label + text + hint)
		return
	}

	filterLabel := "[" + strings.Join(m.states, ", ") + "]"
	toggleHint := "a all"
	if m.showAll {
		filterLabel = "[all]"
		toggleHint = "a active"
	}
	if m.filterText != "" {
		filterLabel += " /" + m.filterText
	}

	tabHint := ""
	if m.logConsole {
		tabHint = " | tab log"
	}
	help := hs.Render(
		filterLabel + " q quit | ↑/↓ pgup/dn" +
			" | enter view | space expand | / filter | " + toggleHint + tabHint)

	out.WriteString(m.appendActiveStatus(help))
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
	entries := make([]barEntry, len(filtered))
	for i, opt := range filtered {
		entries[i] = barEntry{opt, 2 + len(opt)}
	}

	maxWidth := m.width - len(prefix)
	rendered, lo, hi := renderScrollBar(entries, m.selectorCursor, maxWidth, 9)
	m.selectorBarLo = lo
	m.selectorBarHi = hi
	out.WriteString(helpStyle.Render(prefix) + rendered)
	out.WriteByte('\n')

	hint := "←/→ select | enter confirm | esc "
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

func (m *Model) mainHelp() lipgloss.Style {
	if m.logConsole && m.logFocused {
		return helpDimStyle
	}
	return helpBrightStyle
}

func (m *Model) logHelp() lipgloss.Style {
	if m.logFocused {
		return helpBrightStyle
	}
	return helpDimStyle
}

func (m *Model) renderLogConsole(height int) string {
	var out strings.Builder

	sep := strings.Repeat("─", m.width-6)
	out.WriteString(separatorStyle.Render("─── Log " + sep))
	out.WriteByte('\n')

	visibleLines := height - 2
	if visibleLines < 1 {
		visibleLines = 1
	}

	lines := m.LogBuf.Lines()
	need := visibleLines + m.logOffset

	var visual []string
	for i := len(lines) - 1; i >= 0 && len(visual) < need; i-- {
		wrapped := wrapLogLine(lines[i], m.width)
		for j := len(wrapped) - 1; j >= 0; j-- {
			visual = append(visual, wrapped[j])
		}
	}
	for i, j := 0, len(visual)-1; i < j; i, j = i+1, j-1 {
		visual[i], visual[j] = visual[j], visual[i]
	}

	maxOff := len(visual) - visibleLines
	if maxOff < 0 {
		maxOff = 0
	}
	if m.logOffset > maxOff {
		m.logOffset = maxOff
	}

	end := len(visual) - m.logOffset
	start := end - visibleLines
	if start < 0 {
		start = 0
	}

	for _, vl := range visual[start:end] {
		out.WriteString(logLineStyle.Render(vl))
		out.WriteByte('\n')
	}
	for i := end - start; i < visibleLines; i++ {
		out.WriteByte('\n')
	}

	out.WriteString(m.logHelp().Render(
		"tab switch | ` close | ↑/↓ pgup/dn | w write"))
	return out.String()
}

func (m *Model) appendActiveStatus(left string) string {
	msg, spinner := m.Status.Active()
	if msg == "" {
		return left
	}
	var right string
	if spinner {
		frame := spinnerFrames[m.spinnerFrame]
		right = statusStyle.Render(fmt.Sprintf("%s %s", frame, msg))
	} else {
		right = statusStyle.Render(msg)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + lipgloss.NewStyle().Width(gap).Render("") + right
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
