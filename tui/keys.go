package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
				idx := item.parentIdx
				m.RowData[idx].Expanded = !m.RowData[idx].Expanded
			}
		}
		m.mu.Unlock()

	case "d":
		m.openSelector()
	}

	return m, nil
}

func (m *Model) handleSelectorKey(
	msg tea.KeyMsg,
) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '1')
		if idx < len(m.StatusOptions) {
			m.applyStatus(idx)
			m.selectorOpen = false
		}
	case "left", "h":
		m.selectorCursor--
		if m.selectorCursor < 0 {
			m.selectorCursor = len(m.StatusOptions) - 1
		}
	case "right", "l":
		m.selectorCursor++
		if m.selectorCursor >= len(m.StatusOptions) {
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

func (m *Model) openSelector() {
	m.mu.Lock()
	items := m.getVisibleItems()
	m.mu.Unlock()
	if m.selectedRow >= len(items) {
		return
	}
	item := items[m.selectedRow]
	currentStatus := ""
	if m.StatusColIdx < len(item.data) {
		currentStatus = item.data[m.StatusColIdx]
	}
	m.selectorCursor = 0
	for i, opt := range m.StatusOptions {
		if opt == currentStatus {
			m.selectorCursor = i
			break
		}
	}
	m.selectorOpen = true
}

func (m *Model) applyStatus(optionIdx int) {
	if optionIdx < 0 || optionIdx >= len(m.StatusOptions) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	items := m.getVisibleItems()
	if m.selectedRow >= len(items) {
		return
	}
	item := items[m.selectedRow]
	value := m.StatusOptions[optionIdx]

	if item.isSubRow {
		if item.subRowIdx >= 0 &&
			item.parentIdx < len(m.RowData) {
			subRows := m.RowData[item.parentIdx].SubRows
			if item.subRowIdx < len(subRows) &&
				m.StatusColIdx < len(subRows[item.subRowIdx]) {
				subRows[item.subRowIdx][m.StatusColIdx] = value
			}
		}
	} else {
		if item.parentIdx < len(m.RowData) {
			rd := &m.RowData[item.parentIdx]
			if m.StatusColIdx < len(rd.Data) {
				rd.Data[m.StatusColIdx] = value
			}
			for i := range rd.SubRows {
				if m.StatusColIdx < len(rd.SubRows[i]) {
					rd.SubRows[i][m.StatusColIdx] = value
				}
			}
		}
	}
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
