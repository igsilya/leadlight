package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	highlightAnimInterval = 20 * time.Millisecond
	highlightAnimStep     = 0.1
	spinnerInterval       = 100 * time.Millisecond
	dataUpdateInterval    = time.Second
	dataStepDelay         = 200 * time.Millisecond

	// Gradient highlight start color (requires COLORTERM=truecolor)
	gradientStartR = 95
	gradientStartG = 0
	gradientStartB = 255

	subRowIndent   = "    "
	scrollBuffer   = 2
	reservedLines  = 3
	statusColIdx   = 2
	indicatorWidth = 2
)

var (
	headerStyle            = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	separatorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	spinnerFrames          = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	statusOptions          = []string{"Active", "Inactive", "Pending", "Away"}
	normalOptionStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	highlightedOptionStyle = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("57")).
				Foreground(lipgloss.Color("15"))
	optionNumStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	cellHighlightStyle = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("57")).
				Foreground(lipgloss.Color("15"))
)

type bgColor struct {
	r, g, b       int
	fgR, fgG, fgB int
}

var bgColors = map[string]bgColor{
	"yellow":   {0x55, 0x4d, 0x00, 0xff, 0xf0, 0x80},
	"white":    {0x3a, 0x3a, 0x3a, 0xee, 0xee, 0xee},
	"lightred": {0x55, 0x20, 0x20, 0xff, 0xb0, 0xb0},
	"darkred":  {0x8b, 0x10, 0x10, 0xff, 0xdd, 0xdd},
	"green":    {0x15, 0x50, 0x20, 0x90, 0xff, 0xa0},
	"grey":     {0x35, 0x35, 0x35, 0xcc, 0xcc, 0xcc},
	"black":    {0x12, 0x12, 0x12, 0x99, 0x99, 0x99},
}

type gradientEntry struct{ bg, fg string }

// gradientPalettes holds a pre-computed 256-step gradient for each
// background color, interpolating from the highlight start to that color.
var gradientPalettes = map[string][256]gradientEntry{}

func init() {
	for name, c := range bgColors {
		var palette [256]gradientEntry
		for i := range palette {
			// Power curve: transition to row color happens early, indigo only at far left
			t := math.Pow(float64(i)/255.0, 0.35)
			r := int(float64(gradientStartR)*(1-t) + float64(c.r)*t)
			g := int(float64(gradientStartG)*(1-t) + float64(c.g)*t)
			b := int(float64(gradientStartB)*(1-t) + float64(c.b)*t)
			fgR := int(255.0*(1-t) + float64(c.fgR)*t)
			fgG := int(255.0*(1-t) + float64(c.fgG)*t)
			fgB := int(255.0*(1-t) + float64(c.fgB)*t)
			palette[i] = gradientEntry{
				bg: fmt.Sprintf("#%02x%02x%02x", r, g, b),
				fg: fmt.Sprintf("#%02x%02x%02x", fgR, fgG, fgB),
			}
		}
		gradientPalettes[name] = palette
	}
}

type columnDef struct {
	title      string
	percentage float64
}

type rowStyle struct {
	foreground string
	background string
	bold       bool
	italic     bool
}

func (rs rowStyle) lipgloss(faint bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c, ok := bgColors[rs.background]; ok {
		s = s.Background(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)))
		s = s.Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.fgR, c.fgG, c.fgB)))
	} else {
		if rs.foreground != "" {
			s = s.Foreground(lipgloss.Color(rs.foreground))
		}
	}
	if rs.bold {
		s = s.Bold(true)
	}
	if rs.italic {
		s = s.Italic(true)
	}
	if faint {
		s = s.Faint(true)
	}
	return s
}

type rowData struct {
	data     []string
	style    rowStyle
	subRows  [][]string
	expanded bool
}

type visibleItem struct {
	data      []string
	style     rowStyle
	isSubRow  bool
	parentIdx int
	subRowIdx int // index within parent's subRows, -1 for parent rows
	canExpand bool
}

type (
	tickMsg              time.Time
	spinnerTickMsg       time.Time
	highlightAnimTickMsg struct{}
	updateCompleteMsg    struct{}
	updateStep1Msg       struct{}
	updateStep2Msg       struct{ rowIdx, colIdx int }
	updateStep3Msg       struct {
		rowIdx, colIdx int
		value          string
	}
	updateStep4Msg struct {
		rowIdx, colIdx int
		value          string
	}
)

func tickCmd() tea.Cmd {
	return tea.Tick(dataUpdateInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

func highlightAnimTickCmd() tea.Cmd {
	return tea.Tick(highlightAnimInterval, func(t time.Time) tea.Msg { return highlightAnimTickMsg{} })
}

// step1Cmd through step4Cmd form a pipeline that simulates an async
// multi-stage data update, each step separated by a delay.

func step1Cmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(dataStepDelay)
		return updateStep1Msg{}
	}
}

func step2Cmd(numRows int, colCounts []int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(dataStepDelay)
		if numRows == 0 {
			return updateCompleteMsg{}
		}
		rowIdx := rand.Intn(numRows)
		colIdx := 1 + rand.Intn(colCounts[rowIdx]-1) // skip ID column
		return updateStep2Msg{rowIdx: rowIdx, colIdx: colIdx}
	}
}

func step3Cmd(rowIdx, colIdx int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(dataStepDelay)
		var value string
		if colIdx == statusColIdx {
			value = statusOptions[rand.Intn(len(statusOptions))]
		} else {
			value = fmt.Sprintf("Updated %s", time.Now().Format("15:04:05"))
		}
		return updateStep3Msg{rowIdx: rowIdx, colIdx: colIdx, value: value}
	}
}

func step4Cmd(rowIdx, colIdx int, value string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(dataStepDelay)
		return updateStep4Msg{rowIdx: rowIdx, colIdx: colIdx, value: value}
	}
}

type model struct {
	columnDefs      []columnDef
	rowData         []rowData
	selectedRow     int
	width           int
	height          int
	scrollOffset    int
	lastRowsVisible int
	mu              sync.Mutex
	status          string
	spinnerFrame    int

	highlightProgress  float64
	highlightAnimating bool

	selectorOpen   bool
	selectorCursor int
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), highlightAnimTickCmd())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.status = "Selecting row..."
		m.spinnerFrame = 0
		return m, tea.Batch(step1Cmd(), spinnerTickCmd())
	case updateStep1Msg:
		m.status = "Calculating value..."
		colCounts := make([]int, len(m.rowData))
		for i, rd := range m.rowData {
			colCounts[i] = len(rd.data)
		}
		return m, step2Cmd(len(m.rowData), colCounts)
	case updateStep2Msg:
		m.status = "Generating update..."
		return m, step3Cmd(msg.rowIdx, msg.colIdx)
	case updateStep3Msg:
		m.status = "Updating data..."
		return m, step4Cmd(msg.rowIdx, msg.colIdx, msg.value)
	case updateStep4Msg:
		if msg.rowIdx < len(m.rowData) && msg.colIdx < len(m.rowData[msg.rowIdx].data) {
			m.rowData[msg.rowIdx].data[msg.colIdx] = msg.value
		}
		m.status = ""
		return m, tickCmd()
	case updateCompleteMsg:
		m.status = ""
		return m, tickCmd()

	case spinnerTickMsg:
		if m.status != "" {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, spinnerTickCmd()
		}
		return m, nil

	case highlightAnimTickMsg:
		if !m.highlightAnimating {
			return m, nil
		}
		m.highlightProgress += highlightAnimStep
		if m.highlightProgress >= 1.0 {
			m.highlightProgress = 1.0
			m.highlightAnimating = false
			return m, nil
		}
		return m, highlightAnimTickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.selectorOpen {
		return m.handleSelectorKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
			if m.selectedRow < m.scrollOffset {
				m.scrollOffset = m.selectedRow
			}
			return m, m.resetHighlight()
		}

	case "down", "j":
		m.mu.Lock()
		items := m.getVisibleItems()
		m.mu.Unlock()
		if m.selectedRow < len(items)-1 {
			m.selectedRow++
			m.adjustScrollDown(len(items))
			return m, m.resetHighlight()
		}

	case "enter":
		m.mu.Lock()
		items := m.getVisibleItems()
		if m.selectedRow < len(items) {
			item := items[m.selectedRow]
			if item.canExpand {
				m.rowData[item.parentIdx].expanded = !m.rowData[item.parentIdx].expanded
			}
		}
		m.mu.Unlock()

	case "d":
		m.openSelector()
	}

	return m, nil
}

func (m *model) handleSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "1", "2", "3", "4":
		idx := int(key[0] - '1')
		if idx < len(statusOptions) {
			m.applyStatus(idx)
			m.selectorOpen = false
		}
	case "left", "h":
		m.selectorCursor--
		if m.selectorCursor < 0 {
			m.selectorCursor = len(statusOptions) - 1
		}
	case "right", "l":
		m.selectorCursor++
		if m.selectorCursor >= len(statusOptions) {
			m.selectorCursor = 0
		}
	case "enter":
		m.applyStatus(m.selectorCursor)
		m.selectorOpen = false
	case "esc", "d":
		m.selectorOpen = false
	}

	return m, nil
}

func (m *model) openSelector() {
	m.mu.Lock()
	items := m.getVisibleItems()
	m.mu.Unlock()
	if m.selectedRow >= len(items) {
		return
	}
	item := items[m.selectedRow]
	currentStatus := ""
	if statusColIdx < len(item.data) {
		currentStatus = item.data[statusColIdx]
	}
	m.selectorCursor = 0
	for i, opt := range statusOptions {
		if opt == currentStatus {
			m.selectorCursor = i
			break
		}
	}
	m.selectorOpen = true
}

func (m *model) applyStatus(optionIdx int) {
	if optionIdx < 0 || optionIdx >= len(statusOptions) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	items := m.getVisibleItems()
	if m.selectedRow >= len(items) {
		return
	}
	item := items[m.selectedRow]
	value := statusOptions[optionIdx]

	if item.isSubRow {
		if item.subRowIdx >= 0 && item.parentIdx < len(m.rowData) {
			subRows := m.rowData[item.parentIdx].subRows
			if item.subRowIdx < len(subRows) && statusColIdx < len(subRows[item.subRowIdx]) {
				subRows[item.subRowIdx][statusColIdx] = value
			}
		}
	} else {
		if item.parentIdx < len(m.rowData) {
			rd := &m.rowData[item.parentIdx]
			if statusColIdx < len(rd.data) {
				rd.data[statusColIdx] = value
			}
			for i := range rd.subRows {
				if statusColIdx < len(rd.subRows[i]) {
					rd.subRows[i][statusColIdx] = value
				}
			}
		}
	}
}

func (m *model) resetHighlight() tea.Cmd {
	m.highlightProgress = 0
	m.highlightAnimating = true
	return highlightAnimTickCmd()
}

func (m *model) adjustScrollDown(totalItems int) {
	visibleRows := m.lastRowsVisible
	if visibleRows == 0 {
		visibleRows = max(m.height-reservedLines-1, 1)
	}
	if totalItems > visibleRows && m.selectedRow >= m.scrollOffset+visibleRows-scrollBuffer {
		m.scrollOffset++
	}
}

func (m *model) View() string {
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

func (m *model) renderHeader(out *strings.Builder, widths []int) {
	out.WriteString(strings.Repeat(" ", indicatorWidth))
	for i, col := range m.columnDefs {
		cell := renderCell(col.title, widths[i])
		out.WriteString(headerStyle.Render(cell))
	}
	out.WriteByte('\n')

	sep := lipgloss.NewStyle().Width(m.width).Render("─")
	out.WriteString(separatorStyle.Render(sep))
	out.WriteByte('\n')
}

func (m *model) renderRows(out *strings.Builder, items []visibleItem, widths []int) {
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
				row = indicator + m.buildRow(items[i], widths, statusColIdx)
			} else {
				row = m.renderGradientRow(indicator+raw, items[i].style.background)
			}
		} else {
			row = items[i].style.lipgloss(items[i].isSubRow).Render(blank + raw)
		}

		// Measure rendered height to avoid overflowing the terminal
		test := base + rows.String() + row + "\n"
		if lipgloss.Height(test) >= m.height-1 && i > m.scrollOffset {
			break
		}

		rows.WriteString(row)
		rows.WriteByte('\n')
		rendered++
	}

	m.lastRowsVisible = rendered
	out.WriteString(rows.String())
}

func (m *model) buildRawRow(item visibleItem, widths []int) string {
	return m.buildRow(item, widths, -1)
}

// buildRow renders a row's cells. If highlightCol >= 0, that column is
// rendered with cellHighlightStyle while others remain unstyled.
func (m *model) buildRow(item visibleItem, widths []int, highlightCol int) string {
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

func (m *model) renderGradientRow(rawRow string, bgName string) string {
	runes := []rune(rawRow)
	total := len(runes)
	fill := min(int(m.highlightProgress*float64(total)), total)

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
			Background(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b))).
			Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.fgR, c.fgG, c.fgB)))
		b.WriteString(restStyle.Render(string(runes[gradientWidth:])))
	} else if gradientWidth < total {
		b.WriteString(string(runes[gradientWidth:]))
	}
	return b.String()
}

func (m *model) padToBottom(out *strings.Builder) {
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

func (m *model) renderStatusBar(out *strings.Builder) {
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
	status := statusStyle.Render(fmt.Sprintf("%s %s", spinner, m.status))

	gap := m.width - lipgloss.Width(help) - lipgloss.Width(status)
	if gap < 2 {
		gap = 2
	}

	out.WriteString(help)
	out.WriteString(lipgloss.NewStyle().Width(gap).Render(""))
	out.WriteString(status)
}

func (m *model) renderSelectorBar(out *strings.Builder) {
	var parts []string
	for i, opt := range statusOptions {
		num := optionNumStyle.Render(fmt.Sprintf("%d", i+1))
		label := " " + opt + " "
		if i == m.selectorCursor {
			parts = append(parts, num+highlightedOptionStyle.Render(label))
		} else {
			parts = append(parts, num+normalOptionStyle.Render(label))
		}
	}
	sep := helpStyle.Render(" │ ")
	out.WriteString(helpStyle.Render("Status: ") + strings.Join(parts, sep))
	out.WriteByte('\n')
	out.WriteString(helpStyle.Render("←/→ or 1-4 to select, enter confirm, esc cancel"))
}

func (m *model) columnWidths() []int {
	if m.width == 0 {
		return nil
	}
	available := m.width - indicatorWidth
	widths := make([]int, len(m.columnDefs))
	for i, col := range m.columnDefs {
		widths[i] = int(float64(available) * col.percentage)
	}
	return widths
}

func (m *model) getVisibleItems() []visibleItem {
	var items []visibleItem
	for i, rd := range m.rowData {
		items = append(items, visibleItem{
			data:      rd.data,
			style:     rd.style,
			isSubRow:  false,
			parentIdx: i,
			subRowIdx: -1,
			canExpand: len(rd.subRows) > 0,
		})
		if rd.expanded {
			subStyle := rowStyle{}
			for si, sub := range rd.subRows {
				items = append(items, visibleItem{
					data:      sub,
					style:     subStyle,
					isSubRow:  true,
					parentIdx: i,
					subRowIdx: si,
				})
			}
		}
	}
	return items
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

func main() {
	columns := []columnDef{
		{title: "ID", percentage: 0.10},
		{title: "Name", percentage: 0.25},
		{title: "Status", percentage: 0.20},
		{title: "Description", percentage: 0.45},
	}

	rows := []rowData{
		{
			data:  []string{"1", "Alice", "Active", "Software Engineer"},
			style: rowStyle{background: "green"},
			subRows: [][]string{
				{"1.1", "Skills: Go, Python, Rust", "", "5 years experience"},
				{"1.2", "Team: Backend", "", "Projects: API Gateway"},
			},
		},
		{
			data:  []string{"2", "Bob", "Inactive", "Product Manager"},
			style: rowStyle{background: "grey", italic: true},
			subRows: [][]string{
				{"2.1", "On leave", "", "Returns: March 2026"},
			},
		},
		{
			data:  []string{"3", "Charlie", "Active", "Designer"},
			style: rowStyle{background: "yellow", bold: true},
			subRows: [][]string{
				{"3.1", "Skills: Figma, Sketch", "", "UI/UX specialist"},
				{"3.2", "Team: Design", "", "Projects: Mobile App"},
			},
		},
		{
			data:  []string{"4", "Diana", "Active", "Data Scientist"},
			style: rowStyle{background: "white"},
			subRows: [][]string{
				{"4.1", "Skills: Python, R, ML", "", "PhD in Statistics"},
				{"4.2", "Team: Data Science", "", "Projects: Analytics Platform"},
			},
		},
		{
			data:  []string{"5", "Eve", "Inactive", "DevOps Engineer"},
			style: rowStyle{background: "black", italic: true},
			subRows: [][]string{
				{"5.1", "On sabbatical", "", "Returns: April 2026"},
			},
		},
		{
			data:  []string{"6", "Frank", "Active", "Backend Developer"},
			style: rowStyle{background: "darkred"},
			subRows: [][]string{
				{"6.1", "Skills: Java, Spring", "", "10 years experience"},
				{"6.2", "Team: Backend", "", "Projects: Payment System"},
			},
		},
		{
			data:  []string{"7", "Grace", "Active", "Frontend Developer"},
			style: rowStyle{background: "lightred"},
			subRows: [][]string{
				{"7.1", "Skills: React, TypeScript", "", "3 years experience"},
				{"7.2", "Team: Frontend", "", "Projects: Dashboard"},
			},
		},
		{
			data:  []string{"8", "Henry", "Inactive", "QA Engineer"},
			style: rowStyle{background: "grey", italic: true},
			subRows: [][]string{
				{"8.1", "Contract ended", "", "Last day: Feb 15, 2026"},
			},
		},
	}

	m := &model{
		columnDefs:         columns,
		rowData:            rows,
		highlightAnimating: true,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error running program:", err)
		os.Exit(1)
	}
}
