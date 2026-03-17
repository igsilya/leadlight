package tui

import (
	"log"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

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
		m.mu.Lock()
		items := m.getVisibleItems()
		m.mu.Unlock()
		if m.selectedRow < len(items)-1 {
			m.selectedRow++
			m.adjustScrollDown(len(items))
			m.updateSelectedID()
			return m, m.resetHighlight()
		}

	case "enter":
		m.mu.Lock()
		items := m.getVisibleItems()
		if m.selectedRow < len(items) {
			item := items[m.selectedRow]
			if item.isSubRow {
				log.Printf("TUI: enter on sub-row %d (parent %d)",
					m.selectedRow, item.parentIdx)
				m.mu.Unlock()
				return m, m.openPatchView(item)
			}
			if item.canExpand {
				idx := item.parentIdx
				m.RowData[idx].Expanded = !m.RowData[idx].Expanded
				log.Printf("TUI: toggle expand row %d -> %v",
					idx, m.RowData[idx].Expanded)
			}
		}
		m.mu.Unlock()
		m.updateSelectedID()

	case "s":
		log.Println("TUI: key 's' pressed")
		m.openStateSelector()

	case "d":
		log.Println("TUI: key 'd' pressed")
		m.openDelegateSelector()
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
		m.viewportContent = ""
	case "up", "k":
		m.viewportScroll(-1)
	case "down", "j":
		m.viewportScroll(1)
	case "pgup", "ctrl+u":
		m.viewportScroll(-(m.height - 4))
	case "pgdown", "ctrl+d":
		m.viewportScroll(m.height - 4)
	case "home", "g":
		m.viewportOffset = 0
	case "end", "G":
		m.viewportScrollToEnd()
	}
	return m, nil
}

func (m *Model) openStateSelector() {
	if m.token == "" {
		log.Println("TUI: no auth token for state selector")
		m.status = "No auth token configured"
		return
	}
	log.Printf("TUI: opening state selector with %d options",
		len(m.states))
	m.selectorOptions = m.states
	m.selectorIDs = nil
	m.selectorCursor = 0

	m.mu.Lock()
	items := m.getVisibleItems()
	m.mu.Unlock()
	if m.selectedRow < len(items) {
		current := ""
		if m.StatusColIdx < len(items[m.selectedRow].data) {
			current = items[m.selectedRow].data[m.StatusColIdx]
		}
		for i, opt := range m.selectorOptions {
			if opt == current {
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
		m.status = "No auth token configured"
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
		m.status = "No maintainers found"
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

	m.mu.Lock()
	items := m.getVisibleItems()
	m.mu.Unlock()
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
		m.status = "Updating state..."
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
		m.status = "Updating delegate..."
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
	lines := strings.Count(m.viewportContent, "\n")
	maxOffset := lines - m.height + 4
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.viewportOffset > maxOffset {
		m.viewportOffset = maxOffset
	}
}

func (m *Model) viewportScrollToEnd() {
	lines := strings.Count(m.viewportContent, "\n")
	m.viewportOffset = lines - m.height + 4
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
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
	m.viewportOffset = 0

	if row.MboxContent != "" {
		log.Printf("TUI: mbox cached (%d bytes) for %q",
			len(row.MboxContent), row.Name)
		parsed := ParseMbox(row.MboxContent)
		m.viewportContent = FormatMbox(parsed)
	} else {
		log.Printf("TUI: mbox not cached for %q, "+
			"mboxURL=%q msgID=%q", row.Name, row.MboxURL, row.MsgID)
		m.viewportContent = "Fetching..."
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
		visibleRows = max(m.height-reservedLines-1, 1)
	}
	if totalItems > visibleRows &&
		m.selectedRow >= m.scrollOffset+visibleRows-scrollBuffer {
		m.scrollOffset++
	}
}
