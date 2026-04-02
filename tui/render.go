package tui

import (
	"fmt"
	"strconv"
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
	m.renderBuf.Reset()
	m.renderBuf.Grow(m.width * m.height * 4)

	m.renderHeader(&m.renderBuf, widths)

	items := m.getVisibleItems()
	m.renderRows(&m.renderBuf, items, widths)
	m.padToBottom(&m.renderBuf)
	m.renderStatusLine(&m.renderBuf)
	m.renderHelpBar(&m.renderBuf)

	return m.renderBuf.String()
}

func (m *Model) viewportVisibleLines() int {
	v := m.renderHeight() - 2 // status line + help bar
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

	// Assemble exactly `visible` lines separated by newlines.
	lines := make([]string, visible)
	for i := 0; i < end-start; i++ {
		lines[i] = m.viewportLines[start+i]
	}
	body := strings.Join(lines, "\n")

	pct := 0
	maxOff := total - visible
	if maxOff > 0 {
		pct = m.viewportOffset * 100 / maxOff
	}

	bright, desc, sep := m.helpStyles()

	var status string
	expandKey := func(hb *strings.Builder) {
		hb.WriteString(helpSepStr(sep))
		if m.viewExpanded {
			hb.WriteString(helpKey(bright, desc, "e", "collapse"))
		} else {
			hb.WriteString(helpKey(bright, desc, "e", "expand"))
		}
	}
	if len(m.viewComments) > 0 {
		var hb strings.Builder
		hb.WriteString(sep.Render(" "))
		hb.WriteString(bright.Render("←/→"))
		expandKey(&hb)
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(bright.Render("↑/↓") +
			sep.Render(" ") + bright.Render("pgup/dn"))
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(bright.Render("esc"))
		if m.logConsole {
			hb.WriteString(helpSepStr(sep))
			hb.WriteString(helpKey(bright, desc, "tab", "log"))
		}
		hb.WriteString(desc.Render(fmt.Sprintf("  %d%%", pct)))
		helpText := hb.String()
		barWidth := m.width - lipgloss.Width(helpText)
		commentBar := m.renderCommentBar(barWidth)
		status = commentBar + helpText
	} else {
		var hb strings.Builder
		expandKey(&hb)
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(bright.Render("↑/↓") +
			sep.Render(" ") + bright.Render("pgup/dn"))
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(helpKey(bright, desc, "esc", "back"))
		if m.logConsole {
			hb.WriteString(helpSepStr(sep))
			hb.WriteString(helpKey(bright, desc, "tab", "log"))
		}
		hb.WriteString(desc.Render(fmt.Sprintf("  %d%%", pct)))
		status = hb.String()
	}

	var statusLine string
	msg, spinning := m.Status.Active()
	if msg != "" {
		if spinning {
			statusLine = statusStyle.Render(
				fmt.Sprintf("%s %s", spinnerFrames[m.spinnerFrame], msg))
		} else {
			statusLine = statusStyle.Render(msg)
		}
	}
	return body + "\n" + statusLine + "\n" + status
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
	bottomLines := 2 // status line + help bar
	if m.selectorMode != selectorNone {
		bottomLines = 3 // status line + selector (2 lines)
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

	for i := m.scrollOffset; i < len(items); i++ {
		if rendered >= maxRows {
			break
		}

		fetching := m.isRowFetching(items[i])
		var row string
		if i == m.selectedRow {
			ind := indicator
			if fetching {
				ind = "▸" + spinnerFrames[m.spinnerFrame]
			}
			if m.selectorMode != selectorNone {
				row = ind + m.buildRow(
					items[i], widths, m.selectorHighlightCol)
			} else {
				row = m.renderSelectedRow(
					items[i], widths, ind,
					checksStart, checksWidth,
					afrtStart, afrtWidth,
					commentStart, commentWidth)
			}
		} else if fetching {
			prefix := " " + spinnerFrames[m.spinnerFrame]
			row = m.buildStyledRow(items[i], widths, prefix, false)
		} else {
			row = m.buildStyledRow(items[i], widths, blank, true)
		}

		out.WriteString(row)
		out.WriteByte('\n')
		rendered++
	}

	m.lastRowsVisible = rendered
}

// formatCellValue applies column-specific formatting. Age is
// formatted at render time (not during data preparation) so that
// cached rows detect staleness as time passes.
func formatCellValue(col ColIndex, value string) string {
	if col == ColAge {
		return formatAge(value)
	}
	return value
}

func (m *Model) buildRawRow(
	item visibleItem, widths []int,
) string {
	var b strings.Builder
	b.Grow(m.width * 2)
	for j, cellData := range item.data {
		if widths[j] == 0 {
			continue
		}
		text := formatCellValue(ColIndex(j), cellData)
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
	b.Grow(m.width * 4)
	for j, cellData := range item.data {
		if widths[j] == 0 {
			continue
		}
		col := ColIndex(j)
		text := formatCellValue(col, cellData)
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
	item visibleItem, widths []int, prefix string, cache bool,
) string {
	seriesID, _ := strconv.Atoi(m.RowData[item.parentIdx].Data[ColID])

	// Check cache — also verify the age hasn't changed since
	// the row was cached (age is formatted at render time).
	var currentAge string
	if int(ColAge) < len(item.data) {
		currentAge = formatAge(item.data[ColAge])
	}
	if cache {
		if sc := m.cachedRenderedRows[seriesID]; sc != nil {
			if item.isSubRow {
				if item.subRowIdx < len(sc.subRows) &&
					sc.subRows[item.subRowIdx] != "" &&
					sc.subRowAges[item.subRowIdx] == currentAge {
					return sc.subRows[item.subRowIdx]
				}
			} else if sc.seriesRow != "" && sc.seriesAge == currentAge {
				return sc.seriesRow
			}
		}
	}

	// Build the row
	rowStyle := item.style.lipgloss()
	var b strings.Builder
	b.Grow(m.width * 4)
	b.WriteString(rowStyle.Render(prefix))
	for j, cellData := range item.data {
		if widths[j] == 0 {
			continue
		}
		col := ColIndex(j)
		text := formatCellValue(col, cellData)
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
	row := b.String()

	// Store in cache
	if cache {
		sc := m.cachedRenderedRows[seriesID]
		if sc == nil {
			nSubs := len(m.RowData[item.parentIdx].SubRows)
			sc = &seriesRowCache{
				subRows:    make([]string, nSubs),
				subRowAges: make([]string, nSubs),
			}
			m.cachedRenderedRows[seriesID] = sc
		}
		if item.isSubRow {
			if item.subRowIdx < len(sc.subRows) {
				sc.subRows[item.subRowIdx] = row
				sc.subRowAges[item.subRowIdx] = currentAge
			}
		} else {
			sc.seriesRow = row
			sc.seriesAge = currentAge
		}
	}

	return row
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
		commentStart, commentWidth, commentText)
}

func (m *Model) renderGradientRow(
	rawRow, bgName string,
	checksStart, checksWidth int, checksText string,
	afrtStart, afrtWidth int, afrtText string,
	commentStart, commentWidth int, commentText string,
) string {
	runes := []rune(rawRow)
	total := len(runes)
	// fill = how many characters the gradient has reached in the animation.
	fill := min(
		int(m.highlightProgress*float64(total)), total)

	palette, ok := gradientPalettes[bgName]
	if !ok {
		palette = gradientPalettes["stale"]
	}

	// Gradient occupies 40% on each edge, leaving a center gap that fills
	// last — creates a "pinch" effect rather than a uniform sweep.
	leftWidth := max(fill*40/100, 1)
	rightWidth := max(fill*40/100, 1)
	rightStart := total - rightWidth

	checkColors := buildCheckColors(checksText)
	checksEnd := checksStart + checksWidth

	afrtColors := buildBoldDimColors(afrtText)
	afrtEnd := afrtStart + afrtWidth

	commentColors := buildBoldDimColors(commentText)
	commentEnd := commentStart + commentWidth
	commentCountLen := len(commentText)
	if si := strings.IndexByte(commentText, ' '); si >= 0 {
		commentCountLen = si
	}

	cached := bgStyles[bgName]
	var flatBg, flatFg string
	if cached != nil {
		flatBg = cached.bgHex
		flatFg = cached.fgHex
	}

	m.gradientBuf.Reset()
	m.gradientBuf.Grow(total * 30)

	for pos := 0; pos < total; pos++ {
		var bg, fg string
		bold := false

		if pos < leftWidth {
			idx := pos * 255 / max(leftWidth-1, 1) // max prevents div-by-zero for single-char regions
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
				fg = afrtColors[ai]
				bold = afrtColors[ai] != checkZeroColor
			}
		}

		// Apply lavender/dim overlay for the count prefix in both
		// narrow (ColC) and wide (ColComments) modes. In wide mode
		// only the count prefix gets the overlay — the names keep
		// the gradient/flat styling.
		if pos >= commentStart && pos < commentEnd {
			ci := pos - commentStart
			if ci < commentCountLen && ci < len(commentColors) {
				fg = commentColors[ci]
				bold = commentColors[ci] != checkZeroColor
			}
		}

		style := lipgloss.NewStyle().
			Background(lipgloss.Color(bg)).
			Foreground(lipgloss.Color(fg))
		if bold {
			style = style.Bold(true)
		}
		m.gradientBuf.WriteString(style.Render(string(runes[pos : pos+1])))
	}
	return m.gradientBuf.String()
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
				b.WriteString(cached.checkWarn.Render(part))
			}
		} else {
			styles := []lipgloss.Style{
				checksPassStyle, checksFailStyle,
				checksWarnStyle,
			}
			b.WriteString(styles[i].Render(part))
		}
	}
	return padStyledCell(b.String(), width, cached)
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
		checkPassColor, checkFailColor, checkWarnColor,
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

// padStyledCell pads a pre-rendered cell to the target width using
// the row's background style for consistent coloring.
func padStyledCell(rendered string, width int, cached *cachedBgStyle) string {
	visualWidth := lipgloss.Width(rendered)
	if visualWidth < width {
		pad := strings.Repeat(" ", width-visualWidth)
		if cached != nil {
			return rendered + cached.row.Render(pad)
		}
		return rendered + pad
	}
	return rendered
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
				b.WriteString(cached.afrt.Render(part))
			} else {
				b.WriteString(afrtStyle.Render(part))
			}
		}
	}
	return padStyledCell(b.String(), width, cached)
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
			b.WriteString(cached.afrt.Render(cell))
		} else {
			b.WriteString(afrtStyle.Render(cell))
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
		color := afrtColor
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
			if used+w+4 <= maxWidth { // +4 reserves space for ◀ and ▶ indicators
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
			name = firstName(c.SubmitterEmail)
		}
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
	bottomLines := 2 // status line + help bar
	if m.selectorMode != selectorNone {
		bottomLines = 3 // status line + selector (2 lines)
	}
	target := m.renderHeight() - bottomLines + 1
	current := lipgloss.Height(out.String())
	if current < target {
		out.WriteString(strings.Repeat("\n", target-current))
	}
}

func (m *Model) renderStatusLine(out *strings.Builder) {
	msg, spinning := m.Status.Active()
	if msg != "" {
		if spinning {
			frame := spinnerFrames[m.spinnerFrame]
			out.WriteString(statusStyle.Render(
				fmt.Sprintf("%s %s", frame, msg)))
		} else {
			out.WriteString(statusStyle.Render(msg))
		}
	}
	out.WriteByte('\n')
}

func (m *Model) renderHelpBar(out *strings.Builder) {
	if m.applyState != applyIdle {
		m.renderApplyStatusBar(out)
		return
	}
	if m.selectorMode != selectorNone {
		m.renderSelectorBar(out)
		return
	}

	bright, desc, sep := m.helpStyles()

	if m.filterEditing {
		out.WriteString(desc.Render("Filter: "))
		out.WriteString(normalOptionStyle.Render(m.filterText + "_"))
		out.WriteString(sep.Render("  "))
		out.WriteString(bright.Render("↑/↓") +
			sep.Render(" ") + bright.Render("pgup/dn"))
		out.WriteString(helpSepStr(sep))
		out.WriteString(helpKey(bright, desc, "enter", "commit"))
		out.WriteString(helpSepStr(sep))
		out.WriteString(helpKey(bright, desc, "esc", "clear"))
		return
	}

	// Filter indicator: [filter: active] / [filter: all] / [filter: text]
	var b strings.Builder
	if m.filterText != "" {
		b.WriteString(sep.Render("[") +
			bright.Render("filter") +
			sep.Render(": ") +
			bright.Render(m.filterText) +
			sep.Render("] "))
	} else if m.showAll {
		b.WriteString(sep.Render("[") +
			desc.Render("filter") +
			sep.Render(": ") +
			bright.Render("all") +
			sep.Render("] "))
	} else {
		b.WriteString(sep.Render("[") +
			desc.Render("filter") +
			sep.Render(": ") +
			bright.Render("active") +
			sep.Render("] "))
	}

	if m.filterText != "" {
		b.WriteString(helpKey(bright, desc, "q", "clear"))
	} else {
		b.WriteString(helpKey(bright, desc, "q", "quit"))
	}
	b.WriteString(helpSepStr(sep))
	b.WriteString(bright.Render("↑/↓") +
		sep.Render(" ") + bright.Render("pgup/dn"))
	b.WriteString(helpSepStr(sep))
	b.WriteString(helpKey(bright, desc, "enter", "view"))
	b.WriteString(helpSepStr(sep))
	b.WriteString(helpKey(bright, desc, "space", "expand"))
	b.WriteString(helpSepStr(sep))
	if m.filterText != "" {
		b.WriteString(helpKey(bright, desc, "/", "edit"))
	} else {
		b.WriteString(helpKey(bright, desc, "/", "filter"))
	}
	b.WriteString(helpSepStr(sep))
	b.WriteString(helpKey(bright, desc, "f", "fetch"))
	b.WriteString(helpSepStr(sep))
	b.WriteString(helpKey(bright, desc, "p", "apply"))
	b.WriteString(helpSepStr(sep))
	if m.showAll {
		b.WriteString(helpKey(bright, desc, "a", "active"))
	} else {
		b.WriteString(helpKey(bright, desc, "a", "all"))
	}
	if m.logConsole {
		b.WriteString(helpSepStr(sep))
		b.WriteString(helpKey(bright, desc, "tab", "log"))
	}

	out.WriteString(b.String())
}

func (m *Model) renderSelectorBar(out *strings.Builder) {
	bright, desc, sep := m.helpStyles()
	var prefix string
	if m.selectorMode == selectorDelegate {
		prefix = bright.Render("Delegate")
		if m.selectorFilter != "" {
			prefix += sep.Render(" [") +
				bright.Render(m.selectorFilter) +
				sep.Render("]")
		}
	} else {
		prefix = bright.Render("Status")
	}
	prefix += sep.Render(": ")

	filtered, _ := m.filteredOptions()
	entries := make([]barEntry, len(filtered))
	for i, opt := range filtered {
		entries[i] = barEntry{opt, 2 + len(opt)}
	}

	maxWidth := m.width - lipgloss.Width(prefix)
	rendered, lo, hi := renderScrollBar(entries, m.selectorCursor, maxWidth, 9)
	m.selectorBarLo = lo
	m.selectorBarHi = hi
	out.WriteString(prefix + rendered)
	out.WriteByte('\n')

	out.WriteString(helpKey(bright, desc, "←/→", "select"))
	out.WriteString(helpSepStr(sep))
	out.WriteString(helpKey(bright, desc, "enter", "confirm"))
	out.WriteString(helpSepStr(sep))
	if m.selectorFilter != "" {
		out.WriteString(helpKey(bright, desc, "esc", "clear filter"))
	} else {
		out.WriteString(helpKey(bright, desc, "esc", "cancel"))
	}
	if m.selectorMode == selectorDelegate {
		out.WriteString(desc.Render(", type to filter"))
	}
}

// helpStyles returns bright (keys), desc (descriptions), and sep
// (separators/brackets) styles for help bar elements. When the main
// pane is inactive, all return the same dim style for a monotone
// unfocused look.
func (m *Model) helpStyles() (bright, desc, sep lipgloss.Style) {
	if m.logConsole && m.logFocused {
		return helpDimStyle, helpDimStyle, helpDimStyle
	}
	return helpBrightStyle, helpStyle, helpSepStyle
}

func (m *Model) logHelpStyles() (bright, desc, sep lipgloss.Style) {
	if m.logFocused || m.applyState != applyIdle {
		return helpBrightStyle, helpStyle, helpSepStyle
	}
	return helpDimStyle, helpDimStyle, helpDimStyle
}

func helpKey(bright, desc lipgloss.Style, key, descText string) string {
	if descText == "" {
		return bright.Render(key)
	}
	return bright.Render(key) + desc.Render(" "+descText)
}

func helpSepStr(sep lipgloss.Style) string {
	return sep.Render(" | ")
}

// extractHTTPStatus finds "-> NNN" or "-> error" in a log line.
// Returns (code, byteOffset, length). Code is -1 for "error",
// 100-599 for status codes, 0 if not an HTTP log line.
func extractHTTPStatus(line string) (code, offset, length int) {
	idx := strings.Index(line, "-> ")
	if idx < 0 {
		return 0, 0, 0
	}
	rest := line[idx+3:]
	if strings.HasPrefix(rest, "error") {
		return -1, idx + 3, 5
	}
	if len(rest) >= 3 {
		n, err := strconv.Atoi(rest[:3])
		if err == nil && n >= 100 && n <= 599 {
			return n, idx + 3, 3
		}
	}
	return 0, 0, 0
}

func httpStatusStyle(code int) lipgloss.Style {
	switch {
	case code < 0:
		return logHTTPErrStyle
	case code < 300:
		return logHTTP2xxStyle
	case code < 500:
		return logHTTP4xxStyle
	default:
		return logHTTPErrStyle
	}
}

func renderHTTPLogLine(
	out *strings.Builder, text string,
	code, codeAt, codeLen int,
) {
	out.WriteString(logLineStyle.Render(text[:codeAt]))
	out.WriteString(httpStatusStyle(code).Render(
		text[codeAt : codeAt+codeLen]))
	out.WriteString(logLineStyle.Render(text[codeAt+codeLen:]))
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
	currentCount := m.LogBuf.Count()

	// Auto-scroll: when anchor tracks lastSeen, both advance
	if m.logLastSeen == m.logAnchor {
		m.logAnchor = currentCount
	}
	m.logLastSeen = currentCount

	// Clamp anchor if entries expired from the ring buffer
	firstAvailable := currentCount - len(lines)
	if m.logAnchor <= firstAvailable {
		m.logAnchor = firstAvailable + 1
	}

	// Collect visual lines from anchor backward. If the viewport
	// can't be filled (entries expired), push anchor forward until
	// it fills or we reach logLastSeen.
	type styledLine struct {
		text        string
		isApply     bool
		httpCode    int // 0=not HTTP, -1=error, 100-599=status
		httpCodeAt  int // byte offset of status/error in text
		httpCodeLen int // byte length of status/error word
	}
	var visual []styledLine
	for m.logAnchor <= m.logLastSeen {
		visual = visual[:0]
		anchorIdx := len(lines) - 1 - (currentCount - m.logAnchor)
		if anchorIdx < 0 {
			anchorIdx = 0
		}
		for i := anchorIdx; i >= 0; i-- {
			isApply := strings.Contains(lines[i], "[apply]")
			wrapped := wrapLogLine(lines[i], m.width)
			for j := len(wrapped) - 1; j >= 0; j-- {
				code, at, codeLen := extractHTTPStatus(wrapped[j])
				visual = append(visual, styledLine{
					text: wrapped[j], isApply: isApply,
					httpCode: code, httpCodeAt: at,
					httpCodeLen: codeLen,
				})
			}
			if len(visual) >= visibleLines {
				break
			}
		}
		if len(visual) >= visibleLines || m.logAnchor >= m.logLastSeen {
			break
		}
		m.logAnchor++
	}

	// Reverse to chronological order and take the last visibleLines
	for i, j := 0, len(visual)-1; i < j; i, j = i+1, j-1 {
		visual[i], visual[j] = visual[j], visual[i]
	}
	if len(visual) > visibleLines {
		visual = visual[len(visual)-visibleLines:]
	}

	for _, vl := range visual {
		if vl.isApply {
			out.WriteString(applyLogStyle.Render(vl.text))
		} else if vl.httpCode != 0 {
			renderHTTPLogLine(&out, vl.text,
				vl.httpCode, vl.httpCodeAt, vl.httpCodeLen)
		} else {
			out.WriteString(logLineStyle.Render(vl.text))
		}
		out.WriteByte('\n')
	}
	for i := len(visual); i < visibleLines; i++ {
		out.WriteByte('\n')
	}

	hb, hd, hs := m.logHelpStyles()
	if m.applyState == applyIdle {
		out.WriteString(helpKey(hb, hd, "tab", "switch"))
		out.WriteString(helpSepStr(hs))
		out.WriteString(helpKey(hb, hd, "`", "close"))
		out.WriteString(helpSepStr(hs))
	}
	out.WriteString(hb.Render("↑/↓") +
		hs.Render(" ") + hb.Render("pgup/dn"))
	out.WriteString(helpSepStr(hs))
	out.WriteString(helpKey(hb, hd, "w", "write"))
	if m.logAnchor < m.logLastSeen {
		newCount := m.logLastSeen - m.logAnchor
		out.WriteString(helpSepStr(hs))
		out.WriteString(hd.Render(
			fmt.Sprintf("↓ %d new", newCount)))
	}
	return out.String()
}

func renderCell(text string, width int) string {
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Inline(true).
		Render(truncate(text, width))
}

func renderApplyOption(out *strings.Builder, num int, label string, selected bool) {
	var text string
	if num > 0 {
		text = fmt.Sprintf(" %d %s ", num, label)
	} else {
		text = " " + label + " "
	}
	if selected {
		out.WriteString(highlightedOptionStyle.Render(text))
	} else {
		out.WriteString(normalOptionStyle.Render(text))
	}
	out.WriteString(" ")
}

func (m *Model) renderApplyStatusBar(out *strings.Builder) {
	bright, _, _ := m.helpStyles()
	switch m.applyState {
	case applyConfirm:
		out.WriteString(bright.Render(fmt.Sprintf(
			"Apply %d patches from %q?  ",
			len(m.applyPatchIDs), truncate(m.applyName, 40))))
		renderApplyOption(out, 1, "Apply", m.applySelectedOption == 0)
		renderApplyOption(out, 2, "Cancel", m.applySelectedOption == 1)
	case applyFetching:
		out.WriteString(bright.Render(
			"Applying... fetching data  "))
		renderApplyOption(out, 0, "Cancel", true)
	case applyRunning:
		out.WriteString(bright.Render(
			"Applying... running git am"))
	case applyConflict:
		out.WriteString(bright.Render("Apply failed.  "))
		renderApplyOption(out, 1, "Revert", m.applySelectedOption == 0)
		renderApplyOption(out, 2, "Keep", m.applySelectedOption == 1)
	case applyDone:
		out.WriteString(bright.Render(m.applyDoneMsg + "  "))
		renderApplyOption(out, 0, "OK", true)
	}
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width < 3 {
		return string(runes[:width])
	}
	return string(runes[:width-2]) + "… "
}
