package tui

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/unicode/norm"

	"leadlight/status"

	"leadlight/db"
)

type ColumnDef struct {
	Title      string
	FixedWidth int  // 0 = flex column (gets remainder)
	Visible    bool // false = skip during rendering
}

type RowStyle struct {
	Foreground string
	Background string
	Bold       bool
	Italic     bool
}

func (rs RowStyle) lipgloss() lipgloss.Style {
	if cached, ok := bgStyles[rs.Background]; ok {
		return cached.row
	}
	s := lipgloss.NewStyle()
	if rs.Foreground != "" {
		s = s.Foreground(lipgloss.Color(rs.Foreground))
	}
	if rs.Bold {
		s = s.Bold(true)
	}
	if rs.Italic {
		s = s.Italic(true)
	}
	return s
}

type RowData struct {
	Data         []string
	Style        RowStyle
	SubRows      [][]string
	SubRowStyles []RowStyle
	Expanded     bool
}

type visibleItem struct {
	data      []string
	style     RowStyle
	isSubRow  bool
	parentIdx int
	subRowIdx int
	canExpand bool
}

func (m *Model) isRowFetching(item visibleItem) bool {
	if m.Status == nil || len(item.data) == 0 {
		return false
	}
	id, err := strconv.Atoi(item.data[ColID])
	if err != nil {
		return false
	}
	if item.isSubRow {
		return m.Status.IsFetchingPatch(id)
	}
	return m.Status.IsFetchingSeries(id)
}

type SyncUpdateMsg struct{}
type StatusUpdateMsg struct{}
type patchUpdateResultMsg struct{ err error }

type highlightAnimTickMsg struct{}
type spinnerTickMsg time.Time

type selectorMode int

const (
	selectorNone selectorMode = iota
	selectorState
	selectorDelegate
)

type viewMode int

const (
	viewTable viewMode = iota
	viewPatch
)

func highlightAnimTickCmd() tea.Cmd {
	return tea.Tick(
		time.Duration(highlightAnimInterval)*time.Millisecond,
		func(t time.Time) tea.Msg { return highlightAnimTickMsg{} })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(
		time.Duration(spinnerInterval)*time.Millisecond,
		func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

type Model struct {
	ColumnDefs           []ColumnDef
	RowData              []RowData
	stateColIdx          ColIndex
	ChecksColIdx         ColIndex
	selectorHighlightCol ColIndex
	db                   *db.DB
	states               []string
	token                string
	selectedRow          int
	width                int
	height               int
	scrollOffset         int
	lastRowsVisible      int
	Status               *status.Registry
	spinnerFrame         int
	spinnerRunning       bool

	// Gradient "reveal" animation: sweeps from 0.0 to 1.0 when the
	// user moves to a new row, expanding inward from both edges.
	highlightProgress  float64
	highlightAnimating bool

	selectorMode    selectorMode
	selectorCursor  int
	selectorOptions []string
	selectorIDs     []int
	selectorFilter  string
	// [lo, hi) range of entries visible in the scroll bars.
	// Number keys 1-9 map relative to this window.
	selectorBarLo int
	selectorBarHi int
	commentBarLo  int
	commentBarHi  int

	selectedID              string
	showAll                 bool
	filterMode              bool
	filterText              string
	cachedRenderedRows      []string
	cachedRenderedRowsValid bool

	cachedVisibleItems      []visibleItem
	cachedVisibleItemsValid bool

	viewMode       viewMode
	viewingPatchID int
	viewingCoverID int
	viewComments   []CommentInfo
	viewCommentIdx int // -1 = patch/cover, 0+ = comment
	viewportLines  []string
	viewportOffset int
	quotesExpanded bool
	listPrefix     string
	delegateNames  map[string]string
	logConsole     bool
	logFocused     bool
	LogBuf         *LogBuffer
	logLastSeen    int // LogBuf.Count() as of last render
	logAnchor      int // absolute log entry the viewport bottom is pinned to
	logLastCount   int

	FetchSeriesCover   func(seriesID int)
	RequestSync        func()
	FetchPatchComments func(patchID int)
	FetchCoverComments func(coverID int)
	FetchPatchChecks   func(patchID int)
	FetchPatchDetail   func(patchID int)
	FetchCoverDetail   func(coverID int)
	RequestFetchAll    func(seriesID, patchID int)
	RequestPatchUpdate func(
		patchID int, state *string,
		delegateUsername *string, unsetDelegate bool,
	)
}

func NewModel(d *db.DB, states []string, token string) *Model {
	m := &Model{
		ColumnDefs:           PatchworkColumns,
		stateColIdx:          ColState,
		ChecksColIdx:         ColChecks,
		selectorHighlightCol: ColNone,
		db:                   d,
		states:               states,
		token:                token,
		highlightAnimating:   true,
	}
	m.reloadData()
	return m
}

func NewModelWithData(
	columns []ColumnDef,
	rows []RowData,
	stateColIdx ColIndex,
) *Model {
	return &Model{
		ColumnDefs:           columns,
		RowData:              rows,
		stateColIdx:          stateColIdx,
		ChecksColIdx:         ColNone,
		selectorHighlightCol: ColNone,
		highlightAnimating:   true,
	}
}

func (m *Model) reloadData() {
	if m.db == nil {
		return
	}

	expanded := map[string]bool{}
	for _, rd := range m.RowData {
		if rd.Expanded && len(rd.Data) > 0 {
			expanded[rd.Data[0]] = true
		}
	}

	var seriesList []db.SeriesRow
	if m.showAll {
		seriesList = m.db.GetAllSeries()
	} else {
		seriesList = m.db.GetActiveSeries(m.states)
	}
	if m.listPrefix == "" && len(seriesList) > 0 {
		names := make([]string, len(seriesList))
		for i, s := range seriesList {
			names[i] = s.Name
		}
		m.listPrefix = detectListPrefix(names)
	}
	m.delegateNames = m.db.GetDelegateDisplayNames()
	allPatches := m.db.GetAllPatchesBatch(m.showAll, m.states)
	allTags := m.db.GetTagsBatch(m.showAll, m.states)
	allComments := m.db.GetCommentCountsBatch(m.showAll, m.states)
	allPatchComments := m.db.GetPatchCommentCountsBatch(m.showAll, m.states)
	allCommentNames := m.db.GetCommentSubmittersBatch(m.showAll, m.states)
	allPatchCommentNames := m.db.GetPatchCommentSubmittersBatch(m.showAll, m.states)

	rows := make([]RowData, 0, len(seriesList))
	for _, s := range seriesList {
		row := seriesToRow(
			s, allPatches[s.ID], m.listPrefix, m.delegateNames,
			allTags[s.ID], allComments[s.ID], allPatchComments,
			allCommentNames[s.ID], allPatchCommentNames)
		sid := strconv.Itoa(s.ID)
		if expanded[sid] {
			row.Expanded = true
		}
		rows = append(rows, row)
	}

	m.RowData = rows
	m.invalidateRowCache()
	m.restoreSelection()
	m.ensureSelectedVisible()
}

func (m *Model) restoreSelection() {
	items := m.getVisibleItems()
	for i, item := range items {
		if len(item.data) > 0 && item.data[0] == m.selectedID {
			m.selectedRow = i
			return
		}
	}
	if m.selectedRow >= len(items) && len(items) > 0 {
		m.selectedRow = len(items) - 1
	}
	m.updateSelectedID()
}

// NFD decomposition handles most accented Latin characters (é→e,
// ñ→n, ö→o) by splitting into base + combining mark, then stripping
// marks. These characters are distinct codepoints that don't
// decompose under NFD, so they need explicit mapping.
var specialFold = map[rune]rune{
	'ø': 'o', 'Ø': 'O',
	'æ': 'a', 'Æ': 'A',
	'ð': 'd', 'Ð': 'D',
	'ł': 'l', 'Ł': 'L',
	'đ': 'd', 'Đ': 'D',
	'ß': 's',
}

func foldAccents(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if mapped, ok := specialFold[r]; ok {
			b.WriteRune(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func matchesFilter(data []string, filter string) bool {
	for _, field := range data {
		folded := strings.ToLower(foldAccents(field))
		if strings.Contains(folded, filter) {
			return true
		}
	}
	return false
}

func (m *Model) startFilter() {
	m.filterMode = true
	m.filterText = ""
	m.invalidateRowCache()
}

func (m *Model) applyFilter() {
	m.selectedRow = 0
	m.scrollOffset = 0
	m.invalidateRowCache()
	m.updateSelectedID()
}

func (m *Model) clearFilter() {
	// Find which parent contains the selected item
	// BEFORE collapsing, so we can re-expand it
	expandParent := -1
	items := m.getVisibleItems()
	if m.selectedRow < len(items) {
		expandParent = items[m.selectedRow].parentIdx
	}

	m.filterMode = false
	m.filterText = ""

	for i := range m.RowData {
		m.RowData[i].Expanded = false
	}

	if expandParent >= 0 && expandParent < len(m.RowData) {
		m.RowData[expandParent].Expanded = true
	}

	m.invalidateRowCache()
	m.restoreSelection()
	m.ensureSelectedVisible()
}

func (m *Model) ensureSelectedVisible() {
	maxRows := m.maxVisibleRows()
	if m.selectedRow < m.scrollOffset {
		m.scrollOffset = m.selectedRow
	}
	if m.selectedRow >= m.scrollOffset+maxRows {
		m.scrollOffset = m.selectedRow - maxRows + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m *Model) invalidateRowCache() {
	m.cachedRenderedRowsValid = false
	m.cachedVisibleItemsValid = false
}

func (m *Model) updateSelectedID() {
	items := m.getVisibleItems()
	if m.selectedRow < len(items) && len(items[m.selectedRow].data) > 0 {
		m.selectedID = items[m.selectedRow].data[0]
	}
}

func (m *Model) Init() tea.Cmd {
	return highlightAnimTickCmd()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerTickMsg:
		_, spinning := m.Status.Active()
		if spinning {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, spinnerTickCmd()
		}
		m.spinnerRunning = false
		return m, nil

	case StatusUpdateMsg:
		_, spinning := m.Status.Active()
		if spinning && !m.spinnerRunning {
			m.spinnerRunning = true
			return m, spinnerTickCmd()
		}
		// If no spinner is running but a timed status entry exists,
		// schedule a re-render when it expires so Active() cleans it up.
		if !spinning {
			if d := m.Status.NextExpiry(); d > 0 {
				return m, tea.Tick(d, func(time.Time) tea.Msg {
					return StatusUpdateMsg{}
				})
			}
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

	case SyncUpdateMsg:
		m.reloadData()
		if m.viewMode == viewPatch {
			if len(m.viewportLines) == 1 &&
				(m.viewportLines[0] == "Fetching..." ||
					m.viewportLines[0] == "Fetching cover letter...") {
				m.refreshViewport()
			}
			m.refreshViewportComments()
		}
		return m, nil

	case patchUpdateResultMsg:
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.invalidateRowCache()
		if m.viewMode == viewPatch && m.viewingPatchID != 0 {
			m.refreshViewport()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) refreshViewport() {
	if m.db == nil {
		return
	}
	if m.viewingCoverID != 0 {
		cover, err := m.db.GetCover(m.viewingCoverID)
		if err != nil || cover == nil || !cover.DetailFetched {
			return
		}
		parsed := BuildParsedMboxFromCover(*cover)
		m.buildViewportContent(parsed, nil)
		return
	}
	if m.viewingPatchID == 0 {
		return
	}
	row, err := m.db.GetPatch(m.viewingPatchID)
	if err != nil || !row.DetailFetched {
		return
	}
	parsed := BuildParsedMboxFromPatch(*row)
	checks := GetChecksForPatch(m.db, m.viewingPatchID)
	m.buildViewportContent(parsed, checks)
}

func (m *Model) refreshViewportComments() {
	if m.db == nil {
		return
	}
	if m.viewingCoverID != 0 {
		cover, err := m.db.GetCover(m.viewingCoverID)
		if err != nil || cover == nil {
			return
		}
		m.viewComments = GetCommentsForCover(m.db, cover.ID)
	} else if m.viewingPatchID != 0 {
		m.viewComments = GetCommentsForPatch(m.db, m.viewingPatchID)
	}
}

func splitLines(content string) []string {
	return strings.Split(content, "\n")
}

func (m *Model) getVisibleItems() []visibleItem {
	if m.cachedVisibleItemsValid && m.cachedVisibleItems != nil {
		return m.cachedVisibleItems
	}

	filter := strings.ToLower(foldAccents(m.filterText))
	var items []visibleItem

	for i, rd := range m.RowData {
		seriesMatch := filter == "" || matchesFilter(rd.Data, filter)

		var matchingSubs []int
		if filter != "" {
			for si, sub := range rd.SubRows {
				if matchesFilter(sub, filter) {
					matchingSubs = append(matchingSubs, si)
				}
			}
		}

		if filter != "" && !seriesMatch && len(matchingSubs) == 0 {
			continue
		}

		items = append(items, visibleItem{
			data:      rd.Data,
			style:     rd.Style,
			isSubRow:  false,
			parentIdx: i,
			subRowIdx: -1,
			canExpand: len(rd.SubRows) > 0,
		})

		// Auto-expand series with matching sub-rows during filtering
		showSubs := rd.Expanded ||
			(filter != "" && len(matchingSubs) > 0 && !singlePatchSameName(rd))
		if showSubs {
			for si, sub := range rd.SubRows {
				if filter != "" && !seriesMatch {
					match := false
					for _, mi := range matchingSubs {
						if mi == si {
							match = true
							break
						}
					}
					if !match {
						continue
					}
				}
				subStyle := RowStyle{}
				if si < len(rd.SubRowStyles) {
					subStyle = rd.SubRowStyles[si]
				}
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
	m.cachedVisibleItems = items
	m.cachedVisibleItemsValid = true
	return items
}

func singlePatchSameName(rd RowData) bool {
	return len(rd.SubRows) == 1 &&
		len(rd.Data) > int(ColName) &&
		len(rd.SubRows[0]) > int(ColName) &&
		rd.Data[ColName] == rd.SubRows[0][ColName]
}

func (m *Model) renderHeight() int {
	if m.logConsole {
		return m.height / 2
	}
	return m.height
}

func (m *Model) columnWidths() []int {
	if m.width == 0 {
		return nil
	}
	available := m.width - indicatorWidth
	widths := make([]int, len(m.ColumnDefs))
	used := 0
	flex := -1
	hasDynamic := int(ColC) < len(m.ColumnDefs) && int(ColComments) < len(m.ColumnDefs)
	for i, col := range m.ColumnDefs {
		ci := ColIndex(i)
		if hasDynamic && (ci == ColC || ci == ColComments) {
			continue
		}
		if !col.Visible {
			continue
		}
		if col.FixedWidth > 0 {
			widths[i] = col.FixedWidth
			used += col.FixedWidth
		} else {
			flex = i
		}
	}
	if flex < 0 {
		return widths
	}
	remaining := available - used
	if hasDynamic {
		commentsW := m.ColumnDefs[ColComments].FixedWidth
		cW := m.ColumnDefs[ColC].FixedWidth
		// 90 = minimum Name column width for the expanded Comments
		// column to be useful. Below that, show the narrow C column.
		if remaining-commentsW >= 90 {
			m.ColumnDefs[ColC].Visible = false
			m.ColumnDefs[ColComments].Visible = true
			widths[ColComments] = commentsW
			widths[ColC] = 0
			widths[flex] = remaining - commentsW
		} else {
			m.ColumnDefs[ColC].Visible = true
			m.ColumnDefs[ColComments].Visible = false
			widths[ColC] = cW
			widths[ColComments] = 0
			widths[flex] = remaining - cW
		}
	} else {
		widths[flex] = remaining
	}
	if widths[flex] < 1 {
		widths[flex] = 1
	}
	return widths
}
