package tui

import (
	"log"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/status"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "`" {
		m.logConsole = !m.logConsole
		m.logFocused = m.logConsole
		m.logOffset = 0
		m.invalidateRowCache()
		return m, nil
	}
	if m.logConsole && key == "tab" {
		m.logFocused = !m.logFocused
		return m, nil
	}
	if m.logConsole && m.logFocused {
		switch key {
		case "q", "esc":
			m.logConsole = false
			m.logFocused = false
			m.invalidateRowCache()
			return m, nil
		case "up", "k", "down", "j",
			"pgup", "ctrl+u", "pgdown", "ctrl+d",
			"home", "g", "end", "G", "w":
			return m.handleLogKey(msg)
		case "f5":
			// fall through to main pane dispatch
		default:
			return m, nil
		}
	}
	switch m.viewMode {
	case viewPatch:
		return m.handleViewportKey(msg)
	default:
		if m.selectorMode != selectorNone {
			return m.handleSelectorKey(msg)
		}
		return m.handleTableKey(msg)
	}
}

func (m *Model) handleTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.filterMode {
		switch key {
		case "esc":
			m.clearFilter()
			return m, nil
		case "backspace":
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.applyFilter()
			} else {
				m.clearFilter()
			}
			return m, nil
		default:
			if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
				m.filterText += key
				m.applyFilter()
				return m, nil
			}
		}
	}

	switch key {
	case "q", "ctrl+c":
		if m.filterMode {
			m.clearFilter()
			return m, nil
		}
		return m, tea.Quit

	case "/":
		m.startFilter()
		return m, nil

	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
			if m.selectedRow < m.scrollOffset {
				m.scrollOffset = m.selectedRow
			}
			m.updateSelectedID()
			return m, m.resetHighlight()
		}

	case "down", "j":
		items := m.getVisibleItems()
		if m.selectedRow < len(items)-1 {
			m.selectedRow++
			m.adjustScrollDown(len(items))
			m.updateSelectedID()
			return m, m.resetHighlight()
		}

	case "pgdown", "ctrl+d":
		items := m.getVisibleItems()
		halfPage := m.maxVisibleRows() / 2
		if halfPage < 1 {
			halfPage = 1
		}
		m.selectedRow += halfPage
		if m.selectedRow >= len(items) {
			m.selectedRow = len(items) - 1
		}
		m.ensureSelectedVisible()
		m.updateSelectedID()
		return m, m.resetHighlight()

	case "pgup", "ctrl+u":
		halfPage := m.maxVisibleRows() / 2
		if halfPage < 1 {
			halfPage = 1
		}
		m.selectedRow -= halfPage
		if m.selectedRow < 0 {
			m.selectedRow = 0
		}
		m.ensureSelectedVisible()
		m.updateSelectedID()
		return m, m.resetHighlight()

	case "home":
		m.selectedRow = 0
		m.scrollOffset = 0
		m.updateSelectedID()
		return m, m.resetHighlight()

	case "end":
		items := m.getVisibleItems()
		m.selectedRow = len(items) - 1
		m.ensureSelectedVisible()
		m.updateSelectedID()
		return m, m.resetHighlight()

	case " ":
		items := m.getVisibleItems()
		if m.selectedRow < len(items) {
			item := items[m.selectedRow]
			if !item.isSubRow && item.canExpand {
				idx := item.parentIdx
				m.RowData[idx].Expanded = !m.RowData[idx].Expanded
				m.invalidateRowCache()
				m.ensureSelectedVisible()
			}
		}
		m.updateSelectedID()

	case "enter":
		items := m.getVisibleItems()
		if m.selectedRow < len(items) {
			item := items[m.selectedRow]
			if item.isSubRow {
				return m, m.openPatchView(item)
			}
			return m, m.openSeriesView(item)
		}

	case "s":
		log.Println("TUI: key 's' pressed")
		m.openStateSelector()

	case "d":
		log.Println("TUI: key 'd' pressed")
		m.openDelegateSelector()

	case "a":
		m.showAll = !m.showAll
		m.reloadData()

	case "f5":
		if m.RequestSync != nil {
			m.RequestSync()
		}
	}

	return m, nil
}

func (m *Model) handleSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	filtered, _ := m.filteredOptions()
	n := len(filtered)

	switch key {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '1')
		if idx < n {
			return m, m.applyFilteredSelection(idx)
		}
	case "left":
		if n > 0 {
			m.selectorCursor--
			if m.selectorCursor < 0 {
				m.selectorCursor = n - 1
			}
		}
	case "right":
		if n > 0 {
			m.selectorCursor++
			if m.selectorCursor >= n {
				m.selectorCursor = 0
			}
		}
	case "enter":
		if n > 0 {
			return m, m.applyFilteredSelection(
				m.selectorCursor)
		}
	case "esc":
		if m.selectorFilter != "" {
			m.selectorFilter = ""
			m.selectorCursor = 0
		} else {
			m.selectorMode = selectorNone
		}
	case "backspace":
		if len(m.selectorFilter) > 0 {
			m.selectorFilter = m.selectorFilter[:len(m.selectorFilter)-1]
			m.selectorCursor = 0
		}
	default:
		if len(key) == 1 && key[0] >= 'a' && key[0] <= 'z' {
			m.selectorFilter += key
			m.selectorCursor = 0
		}
	}

	return m, nil
}

func (m *Model) handleViewportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.viewMode = viewTable
		m.viewingPatchID = 0
		m.viewingCoverID = 0
		m.viewComments = nil
		m.viewCommentIdx = -1
		m.viewportLines = nil
		m.quotesExpanded = false
	case "e":
		if m.viewCommentIdx >= 0 {
			m.quotesExpanded = !m.quotesExpanded
			m.switchToComment()
		}
	case "up", "k":
		m.viewportScroll(-1)
	case "down", "j":
		m.viewportScroll(1)
	case "pgup", "ctrl+u":
		m.viewportScroll(-m.viewportVisibleLines() / 2)
	case "pgdown", "ctrl+d":
		m.viewportScroll(m.viewportVisibleLines() / 2)
	case "home", "g":
		m.viewportOffset = 0
	case "end", "G":
		m.viewportScrollToEnd()
	case "right", "l", "f8":
		m.nextComment()
	case "left", "h", "f7":
		m.prevComment()
	case "f5":
		if m.RequestSync != nil {
			m.RequestSync()
		}
	}
	return m, nil
}

func (m *Model) handleLogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	consoleLines := m.height - m.renderHeight() - 2
	switch msg.String() {
	case "up", "k":
		m.logOffset++
	case "down", "j":
		m.logOffset--
	case "pgup", "ctrl+u":
		m.logOffset += consoleLines / 2
	case "pgdown", "ctrl+d":
		m.logOffset -= consoleLines / 2
	case "home", "g":
		m.logOffset = logBufMaxLines * 3
	case "end", "G":
		m.logOffset = 0
	case "w":
		if m.LogBuf != nil {
			m.LogBuf.SaveToFile("leadlight.log")
			m.Status.SetTimed(status.Info,
				"Wrote leadlight.log", 3*time.Second)
		}
	}
	if m.logOffset < 0 {
		m.logOffset = 0
	}
	return m, nil
}

func (m *Model) openStateSelector() {
	if m.token == "" {
		log.Println("TUI: no auth token for state selector")
		m.Status.SetTimed(status.Info,
			"No auth token configured", 3*time.Second)
		return
	}
	log.Printf("TUI: opening state selector with %d options",
		len(m.states))
	m.selectorOptions = m.states
	m.selectorIDs = nil
	m.selectorCursor = 0

	items := m.getVisibleItems()
	if m.selectedRow < len(items) {
		current := ""
		if m.StatusColIdx < len(items[m.selectedRow].data) {
			current = items[m.selectedRow].data[m.StatusColIdx]
		}
		for i, opt := range m.selectorOptions {
			if displayState(opt) == current {
				m.selectorCursor = i
				break
			}
		}
	}
	m.selectorFilter = ""
	m.selectorMode = selectorState
}

func (m *Model) openDelegateSelector() {
	if m.token == "" {
		log.Println("TUI: no auth token for delegate selector")
		m.Status.SetTimed(status.Info,
			"No auth token configured", 3*time.Second)
		return
	}
	if m.db == nil {
		log.Println("TUI: no DB for delegate selector")
		return
	}
	maintainers := m.db.GetMaintainers()
	log.Printf("TUI: opening delegate selector with %d maintainers",
		len(maintainers))
	if len(maintainers) == 0 {
		m.Status.SetTimed(status.Info,
			"No maintainers found", 3*time.Second)
		return
	}

	m.selectorOptions = make([]string, len(maintainers))
	m.selectorIDs = make([]int, len(maintainers))
	for i, mt := range maintainers {
		m.selectorOptions[i] = mt.Username
		m.selectorIDs[i] = mt.ID
	}
	m.selectorCursor = 0
	m.selectorFilter = ""
	m.selectorMode = selectorDelegate
}

func (m *Model) applyFilteredSelection(filteredIdx int) tea.Cmd {
	filtered, filteredIDs := m.filteredOptions()
	if filteredIdx < 0 || filteredIdx >= len(filtered) {
		log.Printf("TUI: filtered selection idx %d out of range %d",
			filteredIdx, len(filtered))
		return nil
	}
	log.Printf("TUI: applying selection idx=%d mode=%d value=%q",
		filteredIdx, m.selectorMode, filtered[filteredIdx])

	items := m.getVisibleItems()
	if m.selectedRow >= len(items) {
		m.selectorMode = selectorNone
		return nil
	}

	item := items[m.selectedRow]
	patchIDs := m.getPatchIDs(item)
	mode := m.selectorMode
	m.selectorMode = selectorNone
	m.selectorFilter = ""

	if m.RequestPatchUpdate == nil || len(patchIDs) == 0 {
		return nil
	}

	switch mode {
	case selectorState:
		state := filtered[filteredIdx]
		return func() tea.Msg {
			for _, pid := range patchIDs {
				m.RequestPatchUpdate(pid, &state, nil)
			}
			return patchUpdateResultMsg{}
		}
	case selectorDelegate:
		if filteredIdx >= len(filteredIDs) {
			return nil
		}
		delegateID := filteredIDs[filteredIdx]
		return func() tea.Msg {
			for _, pid := range patchIDs {
				m.RequestPatchUpdate(
					pid, nil, &delegateID)
			}
			return patchUpdateResultMsg{}
		}
	}
	return nil
}

func (m *Model) getPatchIDs(item visibleItem) []int {
	if m.db == nil || len(item.data) == 0 {
		log.Printf("TUI: getPatchIDs: no db or empty data")
		return nil
	}
	id, err := strconv.Atoi(item.data[0])
	if err != nil {
		log.Printf("TUI: getPatchIDs: can't parse ID %q: %v",
			item.data[0], err)
		return nil
	}
	name := ""
	if len(item.data) > 1 {
		name = item.data[1]
	}
	log.Printf("TUI: getPatchIDs: id=%d isSubRow=%v name=%q",
		id, item.isSubRow, name)
	if item.isSubRow {
		return []int{id}
	}
	patches := m.db.GetPatchesForSeries(id)
	ids := make([]int, len(patches))
	for i, p := range patches {
		ids[i] = p.ID
	}
	return ids
}

func (m *Model) filteredOptions() ([]string, []int) {
	if m.selectorFilter == "" {
		return m.selectorOptions, m.selectorIDs
	}
	filter := strings.ToLower(m.selectorFilter)
	var opts []string
	var ids []int
	for i, opt := range m.selectorOptions {
		if strings.Contains(strings.ToLower(opt), filter) {
			opts = append(opts, opt)
			if i < len(m.selectorIDs) {
				ids = append(ids, m.selectorIDs[i])
			}
		}
	}
	return opts, ids
}

func (m *Model) viewportScroll(delta int) {
	m.viewportOffset += delta
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
	maxOffset := len(m.viewportLines) - m.viewportVisibleLines()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.viewportOffset > maxOffset {
		m.viewportOffset = maxOffset
	}
}

func (m *Model) viewportScrollToEnd() {
	m.viewportOffset = len(m.viewportLines) - m.viewportVisibleLines()
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
}

func (m *Model) buildViewportContent(
	mboxContent string, checks []CheckInfo,
) {
	parsed := ParseMbox(mboxContent)
	formatted := FormatMbox(parsed, m.width)
	checksSection := FormatChecks(checks, m.width)
	if checksSection != "" {
		formatted = checksSection + "\n" + formatted
	}
	m.viewportLines = splitLines(formatted)
}

func (m *Model) nextComment() {
	if len(m.viewComments) == 0 {
		return
	}
	m.viewCommentIdx++
	if m.viewCommentIdx >= len(m.viewComments) {
		m.viewCommentIdx = -1
	}
	m.quotesExpanded = false
	m.switchToComment()
}

func (m *Model) prevComment() {
	if len(m.viewComments) == 0 {
		return
	}
	m.viewCommentIdx--
	if m.viewCommentIdx < -1 {
		m.viewCommentIdx = len(m.viewComments) - 1
	}
	m.quotesExpanded = false
	m.switchToComment()
}

func (m *Model) switchToComment() {
	m.viewportOffset = 0
	if m.viewCommentIdx == -1 {
		if m.db == nil {
			return
		}
		if m.viewingCoverID != 0 {
			cover, _ := m.db.GetCover(m.viewingCoverID)
			if cover != nil && cover.MboxContent != "" {
				m.buildViewportContent(cover.MboxContent, nil)
			}
		} else if m.viewingPatchID != 0 {
			row, _ := m.db.GetPatch(m.viewingPatchID)
			if row != nil && row.MboxContent != "" {
				checks := GetChecksForPatch(m.db, m.viewingPatchID)
				m.buildViewportContent(row.MboxContent, checks)
			}
		}
		return
	}
	if m.viewCommentIdx < len(m.viewComments) {
		comment := m.viewComments[m.viewCommentIdx]
		formatted := FormatComment(
			comment, m.width, !m.quotesExpanded)
		m.viewportLines = splitLines(formatted)
	}
}

func (m *Model) openSeriesView(item visibleItem) tea.Cmd {
	if m.db == nil {
		return nil
	}
	seriesID, err := strconv.Atoi(item.data[0])
	if err != nil {
		return nil
	}

	cover, _ := m.db.GetCover(seriesID)
	if cover == nil && m.FetchSeriesCover != nil {
		if m.db.GetSeriesTotalPatches(seriesID) == 0 {
			m.FetchSeriesCover(seriesID)
			cover, _ = m.db.GetCover(seriesID)
		}
	}
	if cover != nil {
		m.viewMode = viewPatch
		m.viewingPatchID = 0
		m.viewingCoverID = seriesID
		m.viewComments = GetCommentsForCover(m.db, cover.ID)
		m.viewCommentIdx = -1
		m.viewportOffset = 0

		if m.FetchCoverComments != nil && m.db.NeedsCoverComments(cover.ID) {
			m.FetchCoverComments(cover.ID)
		}

		if cover.MboxContent != "" {
			m.buildViewportContent(cover.MboxContent, nil)
		} else {
			m.viewportLines = []string{"Fetching cover letter..."}
			if m.RequestCoverMbox != nil {
				m.RequestCoverMbox(seriesID)
			}
		}
		return nil
	}

	// No cover — open first patch instead
	patches := m.db.GetPatchesForSeries(seriesID)
	if len(patches) > 0 {
		return m.openPatchView(visibleItem{
			data:      []string{strconv.Itoa(patches[0].ID)},
			isSubRow:  true,
			parentIdx: item.parentIdx,
			subRowIdx: 0,
		})
	}

	m.Status.SetTimed(status.Info,
		"No patches in this series", 3*time.Second)
	return nil
}

func (m *Model) openPatchView(item visibleItem) tea.Cmd {
	if m.db == nil {
		log.Println("TUI: openPatchView: no DB")
		return nil
	}

	patchIDs := m.getPatchIDs(item)
	if len(patchIDs) == 0 {
		log.Println("TUI: openPatchView: no patch IDs")
		return nil
	}
	patchID := patchIDs[0]

	row, err := m.db.GetPatch(patchID)
	if err != nil {
		log.Printf("TUI: openPatchView: GetPatch(%d) error: %v",
			patchID, err)
		return nil
	}
	log.Printf("TUI: openPatchView: patch %d %q", patchID, row.Name)

	m.viewMode = viewPatch
	m.viewingPatchID = patchID
	m.viewingCoverID = 0
	m.viewComments = GetCommentsForPatch(m.db, patchID)
	m.viewCommentIdx = -1
	m.viewportOffset = 0

	if m.FetchPatchComments != nil && m.db.NeedsPatchComments(patchID) {
		m.FetchPatchComments(patchID)
	}

	if row.MboxContent != "" {
		log.Printf("TUI: mbox cached (%d bytes) for %q",
			len(row.MboxContent), row.Name)
		checks := GetChecksForPatch(m.db, patchID)
		m.buildViewportContent(row.MboxContent, checks)
	} else {
		log.Printf("TUI: mbox not cached for %q, "+
			"mboxURL=%q msgID=%q",
			row.Name, row.MboxURL, row.MsgID)
		m.viewportLines = []string{"Fetching..."}
		if m.RequestMbox != nil {
			log.Printf("TUI: calling RequestMbox(%d) for %q",
				patchID, row.Name)
			m.RequestMbox(patchID)
		} else {
			log.Println("TUI: RequestMbox callback is nil!")
		}
	}

	return nil
}

func (m *Model) resetHighlight() tea.Cmd {
	m.highlightProgress = 0
	m.highlightAnimating = true
	return highlightAnimTickCmd()
}

func (m *Model) adjustScrollDown(totalItems int) {
	visibleRows := m.lastRowsVisible
	if visibleRows == 0 {
		visibleRows = max(m.renderHeight()-reservedLines-1, 1)
	}
	if totalItems > visibleRows &&
		m.selectedRow >= m.scrollOffset+visibleRows-scrollBuffer {
		m.scrollOffset++
	}
}
