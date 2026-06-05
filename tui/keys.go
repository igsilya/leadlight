// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/status"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+z" {
		return m, tea.Suspend
	}
	if m.applyState != applyIdle {
		return m.handleApplyKey(msg)
	}
	if key == "`" && !m.filterEditing {
		m.logConsole = !m.logConsole
		m.logFocused = false
		if m.logConsole && m.LogBuf != nil {
			count := m.LogBuf.Count()
			m.logLastSeen = count
			m.logAnchor = count
		}
		m.invalidateAllCaches()
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
			m.invalidateAllCaches()
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
	case viewCompare:
		return m.handleCompareKey(msg)
	default:
		if m.selectorMode != selectorNone {
			return m.handleSelectorKey(msg)
		}
		return m.handleTableKey(msg)
	}
}

func (m *Model) handleTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.filterEditing {
		switch key {
		case "enter":
			m.commitFilter()
			return m, nil
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
		case "up", "down", "pgup", "ctrl+u",
			"pgdown", "ctrl+d", "home", "end":
			// Fall through to normal navigation
		default:
			if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
				m.filterText += key
				m.applyFilter()
			}
			return m, nil
		}
	}

	switch key {
	case "q", "ctrl+c":
		if m.filterText != "" {
			m.clearFilter()
			return m, nil
		}
		return m, tea.Quit

	case "esc":
		if m.filterText != "" {
			m.clearFilter()
			return m, nil
		}
		if m.compareCount > 0 {
			m.compareCount = 0
			return m, nil
		}

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

	case "home", "g":
		m.selectedRow = 0
		m.scrollOffset = 0
		m.updateSelectedID()
		return m, m.resetHighlight()

	case "end", "G":
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
				m.invalidateVisibleItems()
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

	case "f":
		items := m.getVisibleItems()
		if m.selectedRow < len(items) && m.RequestFetchAll != nil {
			item := items[m.selectedRow]
			id, _ := strconv.Atoi(item.data[ColID])
			if item.isSubRow {
				m.RequestFetchAll(0, id)
			} else {
				m.RequestFetchAll(id, 0)
			}
		}
	case "p":
		items := m.getVisibleItems()
		if m.selectedRow < len(items) {
			item := items[m.selectedRow]
			if item.isSubRow {
				patchID, _ := strconv.Atoi(item.data[ColID])
				seriesID, _ := strconv.Atoi(
					m.RowData[item.parentIdx].Data[ColID])
				m.startApplyConfirm(seriesID, []int{patchID},
					item.data[ColName])
			} else {
				seriesID, _ := strconv.Atoi(item.data[ColID])
				patches := m.db.GetPatchesForSeries(seriesID)
				if len(patches) == 0 {
					log.Printf("[apply] No patches in series")
					break
				}
				ids := make([]int, len(patches))
				for i, p := range patches {
					ids[i] = p.ID
				}
				m.startApplyConfirm(seriesID, ids, item.data[ColName])
			}
		}

	case "c":
		if m.db == nil {
			break
		}
		items := m.getVisibleItems()
		if m.selectedRow >= len(items) {
			break
		}
		item := items[m.selectedRow]
		if len(item.data) == 0 {
			break
		}
		rowID := item.data[ColID]
		var seriesID int
		var patchID int
		if item.isSubRow {
			seriesID, _ = strconv.Atoi(m.RowData[item.parentIdx].Data[ColID])
			patchID, _ = strconv.Atoi(rowID)
		} else {
			seriesID, _ = strconv.Atoi(rowID)
			patchID = 0
		}
		// Toggle off if same item already marked
		if m.compareCount == 1 && m.compare[0].mark.rowID == rowID {
			m.compareCount = 0
			return m, nil
		}
		m.compare[m.compareCount].mark = compareMark{
			seriesID: seriesID,
			patchID:  patchID,
			rowID:    rowID,
		}
		m.compareCount++
		if m.compareCount == 2 {
			m.enterCompareView()
		} else {
			m.Status.SetTimed(status.Info,
				"Marked for comparison — press c on another", 5*time.Second)
		}
		return m, nil

	case "v":
		items := m.getVisibleItems()
		if m.selectedRow >= len(items) {
			break
		}
		if int(ColName) >= len(items[m.selectedRow].data) {
			break
		}
		name := stripBrackets(items[m.selectedRow].data[ColName])
		name = strings.TrimRight(name, ".")
		if name == "" {
			break
		}
		// Save showAll state only when entering from no filter
		if m.filterText == "" {
			m.showAllBeforeFilter = m.showAll
		}
		m.filterText = name
		if !m.showAll {
			m.showAll = true
			m.reloadData()
		}
		// Enter editing mode and invalidate cached items so
		// commitFilter's getVisibleItems recomputes with
		// auto-expanded sub-rows, deriving which series have
		// matching sub-rows for the Expanded flags.
		m.filterEditing = true
		m.invalidateVisibleItems()
		m.commitFilter()

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
		rel := int(key[0] - '1')
		idx := m.selectorBarLo + rel
		if rel < m.selectorBarHi-m.selectorBarLo && idx < n {
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
			m.selectorHighlightCol = ColNone
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
		m.viewExpanded = false
	case "e":
		m.viewExpanded = !m.viewExpanded
		m.switchToComment()
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
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if len(m.viewComments) > 0 {
			rel := int(msg.String()[0] - '1')
			barIdx := m.commentBarLo + rel
			if rel < m.commentBarHi-m.commentBarLo {
				// Bar entry 0 is "patch" (viewCommentIdx -1), entry 1 is
				// comment 0, etc. Subtract 1 to convert bar→comment index.
				commentIdx := barIdx - 1
				if commentIdx >= -1 && commentIdx < len(m.viewComments) {
					m.viewCommentIdx = commentIdx
					m.viewExpanded = false
					m.switchToComment()
				}
			}
		}
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
		m.logAnchor--
		if m.logAnchor < 1 {
			m.logAnchor = 1
		}
	case "down", "j":
		m.logAnchor++
		if m.logAnchor > m.logLastSeen {
			m.logAnchor = m.logLastSeen
		}
	case "pgup", "ctrl+u":
		m.logAnchor -= max(consoleLines/2, 1)
		if m.logAnchor < 1 {
			m.logAnchor = 1
		}
	case "pgdown", "ctrl+d":
		m.logAnchor += max(consoleLines/2, 1)
		if m.logAnchor > m.logLastSeen {
			m.logAnchor = m.logLastSeen
		}
	case "home", "g":
		m.logAnchor = 1
	case "end", "G":
		m.logAnchor = m.logLastSeen
	case "w":
		if m.LogBuf != nil {
			m.LogBuf.SaveToFile("leadlight.log")
			m.Status.SetTimed(status.Info,
				"Wrote leadlight.log", 3*time.Second)
		}
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
		len(AllPatchStates))
	m.selectorOptions = AllPatchStates
	m.selectorIDs = nil
	m.selectorCursor = 0

	items := m.getVisibleItems()
	if m.selectedRow < len(items) {
		current := ""
		if int(m.stateColIdx) < len(items[m.selectedRow].data) {
			current = items[m.selectedRow].data[m.stateColIdx]
		}
		for i, opt := range m.selectorOptions {
			if displayState(opt) == current {
				m.selectorCursor = i
				break
			}
		}
	}
	m.selectorFilter = ""
	m.selectorHighlightCol = m.stateColIdx
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

	m.selectorOptions = make([]string, len(maintainers)+1)
	m.selectorIDs = make([]int, len(maintainers)+1)
	m.selectorOptions[0] = "(unset)"
	for i, mt := range maintainers {
		m.selectorOptions[i+1] = mt.Username
		m.selectorIDs[i+1] = mt.ID
	}
	m.selectorCursor = 0
	m.selectorFilter = ""
	m.selectorHighlightCol = ColDelegate
	m.selectorMode = selectorDelegate
}

func (m *Model) applyFilteredSelection(filteredIdx int) tea.Cmd {
	filtered, _ := m.filteredOptions()
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
		m.selectorHighlightCol = ColNone
		return nil
	}

	item := items[m.selectedRow]
	patchIDs := m.getPatchIDs(item)
	mode := m.selectorMode
	m.selectorMode = selectorNone
	m.selectorHighlightCol = ColNone
	m.selectorFilter = ""

	if m.RequestPatchUpdate == nil || len(patchIDs) == 0 {
		return nil
	}

	switch mode {
	case selectorState:
		state := filtered[filteredIdx]
		return func() tea.Msg {
			for _, pid := range patchIDs {
				m.RequestPatchUpdate(
					pid, &state, nil, false)
			}
			return patchUpdateResultMsg{}
		}
	case selectorDelegate:
		username := filtered[filteredIdx]
		if username == "(unset)" {
			return func() tea.Msg {
				for _, pid := range patchIDs {
					m.RequestPatchUpdate(pid, nil, nil, true)
				}
				return patchUpdateResultMsg{}
			}
		}
		return func() tea.Msg {
			for _, pid := range patchIDs {
				m.RequestPatchUpdate(
					pid, nil, &username, false)
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
	id, err := strconv.Atoi(item.data[ColID])
	if err != nil {
		log.Printf("TUI: getPatchIDs: can't parse ID %q: %v",
			item.data[ColID], err)
		return nil
	}
	name := ""
	if int(ColName) < len(item.data) {
		name = item.data[ColName]
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
	parsed ParsedMbox, checks []CheckInfo,
) {
	formatted := FormatMbox(parsed, m.width, !m.viewExpanded)
	checksSection := FormatChecks(checks, m.width, !m.viewExpanded)
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
	m.viewExpanded = false
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
	m.viewExpanded = false
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
			if cover != nil && cover.DetailFetched {
				parsed := BuildParsedMboxFromCover(*cover)
				m.buildViewportContent(parsed, nil)
			}
		} else if m.viewingPatchID != 0 {
			row, _ := m.db.GetPatch(m.viewingPatchID)
			if row != nil && row.DetailFetched {
				parsed := BuildParsedMboxFromPatch(*row)
				checks := GetChecksForPatch(m.db, m.viewingPatchID)
				m.buildViewportContent(parsed, checks)
			}
		}
		return
	}
	if m.viewCommentIdx < len(m.viewComments) {
		comment := m.viewComments[m.viewCommentIdx]
		formatted := FormatComment(
			comment, m.width, !m.viewExpanded, m.viewSourceLines)
		m.viewportLines = splitLines(formatted)
	}
}

func (m *Model) openSeriesView(item visibleItem) tea.Cmd {
	if m.db == nil {
		return nil
	}
	seriesID, err := strconv.Atoi(item.data[ColID])
	if err != nil {
		return nil
	}

	cover, _ := m.db.GetCover(seriesID)
	if cover == nil && m.FetchSeriesDetail != nil {
		if m.db.GetSeriesTotalPatches(seriesID) == 0 {
			m.viewMode = viewPatch
			m.viewingPatchID = 0
			m.viewingCoverID = seriesID
			m.viewComments = nil
			m.viewCommentIdx = -1
			m.viewportOffset = 0
			m.viewSourceLines = nil
			m.viewportLoading = true
			m.viewportLines = []string{"Fetching series details..."}
			m.FetchSeriesDetail(seriesID)
			return nil
		}
	}
	if cover != nil {
		m.viewMode = viewPatch
		m.viewingPatchID = 0
		m.viewingCoverID = seriesID
		m.viewComments = GetCommentsForCover(m.db, cover.ID)
		m.viewCommentIdx = -1
		m.viewportOffset = 0
		if m.FixGmailWrapping && cover.DetailFetched {
			m.viewSourceLines = buildSourceLines(
				cover.Content, "", m.viewComments)
		} else {
			m.viewSourceLines = nil
		}

		if m.FetchCoverComments != nil && m.db.NeedsCoverComments(cover.ID) {
			m.FetchCoverComments(cover.ID)
		}

		if cover.DetailFetched {
			parsed := BuildParsedMboxFromCover(*cover)
			m.buildViewportContent(parsed, nil)
		} else {
			m.viewportLoading = true
			m.viewportLines = []string{"Fetching..."}
			if m.FetchCoverDetail != nil {
				m.FetchCoverDetail(cover.ID)
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
	if m.FixGmailWrapping && row.DetailFetched {
		m.viewSourceLines = buildSourceLines(
			row.Content, row.Diff, m.viewComments)
	} else {
		m.viewSourceLines = nil
	}

	if m.FetchPatchComments != nil && m.db.NeedsPatchComments(patchID) {
		m.FetchPatchComments(patchID)
	}
	if m.FetchPatchChecks != nil && m.db.NeedsPatchChecks(patchID) {
		m.FetchPatchChecks(patchID)
	}

	if row.DetailFetched {
		log.Printf("TUI: detail available for patch %d %q",
			patchID, row.Name)
		parsed := BuildParsedMboxFromPatch(*row)
		checks := GetChecksForPatch(m.db, patchID)
		m.buildViewportContent(parsed, checks)
	} else {
		log.Printf("TUI: detail not fetched for patch %d %q",
			patchID, row.Name)
		m.viewportLoading = true
		m.viewportLines = []string{"Fetching..."}
		if m.FetchPatchDetail != nil {
			m.FetchPatchDetail(patchID)
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
		visibleRows = max(m.renderHeight()-reservedLines, 1)
	}
	if totalItems > visibleRows &&
		m.selectedRow >= m.scrollOffset+visibleRows-scrollBuffer {
		m.scrollOffset++
	}
	maxScroll := totalItems - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
}

func (m *Model) enterCompareView() {
	m.Status.Clear(status.Info)

	for i := range m.compare {
		mark := m.compare[i].mark
		m.compare[i].patches = buildComparePatches(m.db.GetPatchesForSeries(mark.seriesID))
		cover, _ := m.db.GetCover(mark.seriesID)
		m.compare[i].cover = buildCompareCover(cover)
		m.compare[i].ver = fmt.Sprintf("v%d", m.db.GetSeriesVersion(mark.seriesID))
		m.compare[i].idx = resolveCompareIdx(mark, m.compare[i].patches, m.compare[i].cover)
	}

	// If both started from series rows, try to align: both show
	// covers if available, otherwise both show first patches.
	if m.compare[0].mark.patchID == 0 && m.compare[1].mark.patchID == 0 {
		if m.compare[0].cover != nil && m.compare[1].cover != nil {
			m.compare[0].idx = -1
			m.compare[1].idx = -1
		} else {
			m.compare[0].idx = 0
			m.compare[1].idx = 0
		}
	}

	m.comparePrefix = 0
	m.viewportOffset = 0
	m.viewExpanded = false
	m.compareDiffCache = nil
	m.viewMode = viewCompare
	m.buildCompareContent()
	m.fetchCompareDetails()
}

// fetchCompareDetails triggers async fetches for any unfetched
// patches or covers currently visible in the compare view.
func (m *Model) fetchCompareDetails() {
	for i := range m.compare {
		idx := m.compare[i].idx
		if idx == -1 {
			cover, _ := m.db.GetCover(m.compare[i].mark.seriesID)
			if cover != nil && !cover.DetailFetched && m.FetchCoverDetail != nil {
				m.FetchCoverDetail(cover.ID)
			}
		} else if idx >= 0 && idx < len(m.compare[i].patches) {
			patchID := m.compare[i].patches[idx].id
			row, _ := m.db.GetPatch(patchID)
			if row != nil && !row.DetailFetched && m.FetchPatchDetail != nil {
				m.FetchPatchDetail(patchID)
			}
		}
	}
}

func resolveCompareIdx(
	mark compareMark, patches []comparePatch, cover *ParsedMbox,
) int {
	if mark.patchID == 0 {
		if cover != nil {
			return -1
		}
		return 0
	}
	for i, p := range patches {
		if p.id == mark.patchID {
			return i
		}
	}
	return 0
}

func (m *Model) compareMinIdx() int {
	if m.compare[0].cover != nil && m.compare[1].cover != nil {
		return -1
	}
	return 0
}

func (m *Model) handleCompareKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "q", "esc":
		m.viewMode = viewTable
		m.compareCount = 0
		return m, nil

	case "up", "k":
		m.compareScroll(-1)
	case "down", "j":
		m.compareScroll(1)
	case "pgup", "ctrl+u":
		m.compareScroll(-m.viewportVisibleLines() / 2)
	case "pgdown", "ctrl+d":
		m.compareScroll(m.viewportVisibleLines() / 2)
	case "home", "g":
		m.viewportOffset = 0
	case "end", "G":
		m.compareScrollToEnd()

	case "e":
		m.viewExpanded = !m.viewExpanded
		m.viewportOffset = 0
		m.compareDiffCache = nil
		m.buildCompareContent()

	case "1":
		m.comparePrefix = 1
	case "2":
		m.comparePrefix = 2

	case "left", "h":
		m.compareCycle(-1)
	case "right", "l":
		m.compareCycle(1)
	}
	return m, nil
}

func (m *Model) compareCycle(delta int) {
	if m.comparePrefix == 1 || m.comparePrefix == 2 {
		s := &m.compare[m.comparePrefix-1]
		s.idx = m.clampCompareIdx(s.idx+delta, s.patches, s.cover)
	} else {
		new0 := m.compare[0].idx + delta
		new1 := m.compare[1].idx + delta
		minIdx := m.compareMinIdx()
		outOfBounds := new0 < minIdx || new1 < minIdx ||
			new0 >= len(m.compare[0].patches) || new1 >= len(m.compare[1].patches)
		if outOfBounds {
			new0 = minIdx
			new1 = minIdx
		}
		m.compare[0].idx = new0
		m.compare[1].idx = new1
	}
	m.comparePrefix = 0
	m.viewportOffset = 0
	m.buildCompareContent()
	m.fetchCompareDetails()
}

func (m *Model) clampCompareIdx(idx int, patches []comparePatch, cover *ParsedMbox) int {
	minIdx := 0
	if cover != nil {
		minIdx = -1
	}
	if idx < minIdx {
		return len(patches) - 1
	}
	if idx >= len(patches) {
		return minIdx
	}
	return idx
}

func (m *Model) compareMaxLines() int {
	return max(len(m.compare[0].lines), len(m.compare[1].lines))
}

func (m *Model) compareScroll(delta int) {
	m.viewportOffset += delta
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
	maxOffset := m.compareMaxLines() - m.viewportVisibleLines()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.viewportOffset > maxOffset {
		m.viewportOffset = maxOffset
	}
}

func (m *Model) compareScrollToEnd() {
	m.viewportOffset = m.compareMaxLines() - m.viewportVisibleLines()
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
}
