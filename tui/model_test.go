package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() *Model {
	columns := []ColumnDef{
		{Title: "ID", Percentage: 0.10},
		{Title: "Name", Percentage: 0.25},
		{Title: "Status", Percentage: 0.20},
		{Title: "Desc", Percentage: 0.45},
	}
	rows := []RowData{
		{
			Data:  []string{"1", "Lorem", "Active", "Ipsum"},
			Style: RowStyle{Background: "green"},
			SubRows: [][]string{
				{"1.1", "Sub A", "", "Detail A"},
				{"1.2", "Sub B", "", "Detail B"},
			},
		},
		{
			Data:  []string{"2", "Dolor", "Pending", "Amet"},
			Style: RowStyle{Background: "yellow"},
		},
		{
			Data:  []string{"3", "Sit", "Away", "Consect"},
			Style: RowStyle{Background: "grey"},
			SubRows: [][]string{
				{"3.1", "Sub C", "", "Detail C"},
			},
		},
	}
	opts := []string{"Active", "Inactive", "Pending", "Away"}
	m := NewModelWithData(columns, rows, opts, 2)
	m.width = 120
	m.height = 30
	return m
}

func pressKey(m *Model, key string) *Model {
	result, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(key),
	})
	return result.(*Model)
}

func pressSpecialKey(m *Model, keyType tea.KeyType) *Model {
	result, _ := m.Update(tea.KeyMsg{Type: keyType})
	return result.(*Model)
}

func TestNewModel(t *testing.T) {
	m := testModel()
	if m.selectedRow != 0 {
		t.Errorf("selectedRow = %d", m.selectedRow)
	}
	if len(m.ColumnDefs) != 4 {
		t.Errorf("columns = %d", len(m.ColumnDefs))
	}
	if len(m.RowData) != 3 {
		t.Errorf("rows = %d", len(m.RowData))
	}
	if !m.highlightAnimating {
		t.Error("highlightAnimating should be true")
	}
}

func TestKeyDown(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j")
	if m.selectedRow != 1 {
		t.Errorf("selectedRow = %d, want 1", m.selectedRow)
	}
	m = pressKey(m, "j")
	if m.selectedRow != 2 {
		t.Errorf("selectedRow = %d, want 2", m.selectedRow)
	}
	// At bottom, should not go further
	m = pressKey(m, "j")
	if m.selectedRow != 2 {
		t.Errorf("selectedRow = %d, want 2 (at bottom)",
			m.selectedRow)
	}
}

func TestKeyUp(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j") // go to 1
	m = pressKey(m, "k") // back to 0
	if m.selectedRow != 0 {
		t.Errorf("selectedRow = %d, want 0", m.selectedRow)
	}
	// At top, should not go further
	m = pressKey(m, "k")
	if m.selectedRow != 0 {
		t.Errorf("selectedRow = %d, want 0 (at top)",
			m.selectedRow)
	}
}

func TestKeyEnter_Expand(t *testing.T) {
	m := testModel()
	if m.RowData[0].Expanded {
		t.Error("row 0 should start collapsed")
	}

	m = pressSpecialKey(m, tea.KeyEnter)
	if !m.RowData[0].Expanded {
		t.Error("row 0 should be expanded after enter")
	}

	// Now there are 5 visible items (3 rows + 2 sub-rows)
	items := m.getVisibleItems()
	if len(items) != 5 {
		t.Errorf("visible items = %d, want 5", len(items))
	}

	// Enter again collapses
	m = pressSpecialKey(m, tea.KeyEnter)
	if m.RowData[0].Expanded {
		t.Error("row 0 should be collapsed after second enter")
	}
}

func TestKeyEnter_NoExpandOnLeafRow(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j") // row 1 has no sub-rows
	m = pressSpecialKey(m, tea.KeyEnter)
	if m.RowData[1].Expanded {
		t.Error("row 1 (no sub-rows) should not expand")
	}
}

func TestKeyD_OpenSelector(t *testing.T) {
	m := testModel()
	if m.selectorOpen {
		t.Error("selector should start closed")
	}

	m = pressKey(m, "d")
	if !m.selectorOpen {
		t.Error("selector should be open after d")
	}

	// Cursor should match current status value ("Active" = index 0)
	if m.selectorCursor != 0 {
		t.Errorf("selectorCursor = %d, want 0", m.selectorCursor)
	}
}

func TestKeyD_CursorMatchesCurrentValue(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j") // row 1, status = "Pending"
	m = pressKey(m, "d")
	// "Pending" is index 2 in the options
	if m.selectorCursor != 2 {
		t.Errorf("selectorCursor = %d, want 2", m.selectorCursor)
	}
}

func TestSelectorLeftRight(t *testing.T) {
	m := testModel()
	m = pressKey(m, "d")

	m = pressKey(m, "l") // right
	if m.selectorCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.selectorCursor)
	}

	m = pressKey(m, "h") // left back to 0
	if m.selectorCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.selectorCursor)
	}

	// Wrap left from 0 → last
	m = pressKey(m, "h")
	if m.selectorCursor != 3 {
		t.Errorf("cursor = %d, want 3 (wrapped)", m.selectorCursor)
	}

	// Wrap right from last → 0
	m = pressKey(m, "l")
	if m.selectorCursor != 0 {
		t.Errorf("cursor = %d, want 0 (wrapped)", m.selectorCursor)
	}
}

func TestSelectorNumber(t *testing.T) {
	m := testModel()
	m = pressKey(m, "d")

	m = pressKey(m, "2") // select "Inactive"
	if m.selectorOpen {
		t.Error("selector should close after number key")
	}
	if m.RowData[0].Data[2] != "Inactive" {
		t.Errorf("status = %q, want Inactive",
			m.RowData[0].Data[2])
	}
}

func TestSelectorEnter(t *testing.T) {
	m := testModel()
	m = pressKey(m, "d")
	m = pressKey(m, "l") // move to index 1 ("Inactive")
	m = pressSpecialKey(m, tea.KeyEnter)

	if m.selectorOpen {
		t.Error("selector should close after enter")
	}
	if m.RowData[0].Data[2] != "Inactive" {
		t.Errorf("status = %q, want Inactive",
			m.RowData[0].Data[2])
	}
}

func TestSelectorEsc(t *testing.T) {
	m := testModel()
	originalStatus := m.RowData[0].Data[2]
	m = pressKey(m, "d")
	m = pressKey(m, "l") // move cursor but don't confirm
	m = pressSpecialKey(m, tea.KeyEsc)

	if m.selectorOpen {
		t.Error("selector should close after esc")
	}
	if m.RowData[0].Data[2] != originalStatus {
		t.Errorf("status = %q, want unchanged %q",
			m.RowData[0].Data[2], originalStatus)
	}
}

func TestSelectorApply_Cascade(t *testing.T) {
	m := testModel()
	// Row 0 has sub-rows
	m = pressKey(m, "d")
	m = pressKey(m, "2") // "Inactive"

	// Parent row should be updated
	if m.RowData[0].Data[2] != "Inactive" {
		t.Errorf("parent = %q", m.RowData[0].Data[2])
	}
	// Sub-rows should also be updated
	for i, sub := range m.RowData[0].SubRows {
		if sub[2] != "Inactive" {
			t.Errorf("sub[%d] = %q, want Inactive", i, sub[2])
		}
	}
}

func TestHighlightTick(t *testing.T) {
	m := testModel()
	m.highlightProgress = 0
	m.highlightAnimating = true

	result, cmd := m.Update(highlightAnimTickMsg{})
	m = result.(*Model)

	if m.highlightProgress < highlightAnimStep-0.001 {
		t.Errorf("progress = %f, want >= %f",
			m.highlightProgress, highlightAnimStep)
	}
	if cmd == nil {
		t.Error("should return tick cmd while animating")
	}
}

func TestHighlightTick_Completes(t *testing.T) {
	m := testModel()
	m.highlightProgress = 0.95
	m.highlightAnimating = true

	result, cmd := m.Update(highlightAnimTickMsg{})
	m = result.(*Model)

	if m.highlightProgress != 1.0 {
		t.Errorf("progress = %f, want 1.0", m.highlightProgress)
	}
	if m.highlightAnimating {
		t.Error("should stop animating at 1.0")
	}
	if cmd != nil {
		t.Error("should return nil cmd when done")
	}
}

func TestWindowResize(t *testing.T) {
	m := testModel()
	result, _ := m.Update(tea.WindowSizeMsg{
		Width: 200, Height: 50,
	})
	m = result.(*Model)
	if m.width != 200 || m.height != 50 {
		t.Errorf("size = %dx%d", m.width, m.height)
	}
}

func TestView_ZeroSize(t *testing.T) {
	m := testModel()
	m.width = 0
	m.height = 0
	v := m.View()
	if v == "" {
		t.Error("View should return something even at zero size")
	}
}

func TestView_DoesNotPanic(t *testing.T) {
	m := testModel()
	// Should not panic
	_ = m.View()
}

func TestExpandedNavigation(t *testing.T) {
	m := testModel()
	// Expand row 0
	m = pressSpecialKey(m, tea.KeyEnter)
	// Navigate down through sub-rows
	m = pressKey(m, "j") // sub-row 1.1
	m = pressKey(m, "j") // sub-row 1.2
	m = pressKey(m, "j") // row 2
	if m.selectedRow != 3 {
		t.Errorf("selectedRow = %d, want 3", m.selectedRow)
	}
}
