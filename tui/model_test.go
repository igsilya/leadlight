package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/db"
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
	m := NewModelWithData(columns, rows, 2)
	m.states = []string{"Active", "Inactive", "Pending", "Away"}
	m.token = "test-token"
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

func TestKeyS_OpenStateSelector(t *testing.T) {
	m := testModel()
	if m.selectorMode != selectorNone {
		t.Error("selector should start closed")
	}

	m = pressKey(m, "s")
	if m.selectorMode != selectorState {
		t.Errorf("selectorMode = %d, want selectorState",
			m.selectorMode)
	}

	if m.selectorCursor != 0 {
		t.Errorf("selectorCursor = %d, want 0",
			m.selectorCursor)
	}
}

func TestKeyS_CursorMatchesCurrentValue(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j") // row 1, status = "Pending"
	m = pressKey(m, "s")
	// "Pending" is index 2 in the states
	if m.selectorCursor != 2 {
		t.Errorf("selectorCursor = %d, want 2",
			m.selectorCursor)
	}
}

func TestSelectorLeftRight(t *testing.T) {
	m := testModel()
	m = pressKey(m, "s")

	m = pressSpecialKey(m, tea.KeyRight)
	if m.selectorCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.selectorCursor)
	}

	m = pressSpecialKey(m, tea.KeyLeft)
	if m.selectorCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.selectorCursor)
	}

	// Wrap left from 0 → last
	m = pressSpecialKey(m, tea.KeyLeft)
	if m.selectorCursor != 3 {
		t.Errorf("cursor = %d, want 3 (wrapped)",
			m.selectorCursor)
	}

	// Wrap right from last → 0
	m = pressSpecialKey(m, tea.KeyRight)
	if m.selectorCursor != 0 {
		t.Errorf("cursor = %d, want 0 (wrapped)",
			m.selectorCursor)
	}
}

func TestSelectorFilter(t *testing.T) {
	m := testModel()
	m = pressKey(m, "s")

	// Type "p" to filter — should match "Pending"
	m = pressKey(m, "p")
	if m.selectorFilter != "p" {
		t.Errorf("filter = %q", m.selectorFilter)
	}
	filtered, _ := m.filteredOptions()
	if len(filtered) != 1 || filtered[0] != "Pending" {
		t.Errorf("filtered = %v, want [Pending]", filtered)
	}

	// Backspace clears filter
	m = pressSpecialKey(m, tea.KeyBackspace)
	if m.selectorFilter != "" {
		t.Errorf("filter = %q after backspace", m.selectorFilter)
	}
	filtered, _ = m.filteredOptions()
	if len(filtered) != 4 {
		t.Errorf("filtered = %d, want 4", len(filtered))
	}
}

func TestSelectorFilter_EscClearsFirst(t *testing.T) {
	m := testModel()
	m = pressKey(m, "s")
	m = pressKey(m, "a")               // filter = "a"
	m = pressSpecialKey(m, tea.KeyEsc) // clears filter
	if m.selectorMode == selectorNone {
		t.Error("first esc should clear filter, not close")
	}
	if m.selectorFilter != "" {
		t.Errorf("filter = %q, want empty", m.selectorFilter)
	}
	m = pressSpecialKey(m, tea.KeyEsc) // now closes
	if m.selectorMode != selectorNone {
		t.Error("second esc should close selector")
	}
}

func TestSelectorEsc(t *testing.T) {
	m := testModel()
	m = pressKey(m, "s")
	m = pressSpecialKey(m, tea.KeyEsc)

	if m.selectorMode != selectorNone {
		t.Error("selector should close after esc")
	}
}

func TestKeyD_NoToken(t *testing.T) {
	m := testModel()
	m.token = ""
	m = pressKey(m, "d")
	if m.selectorMode != selectorNone {
		t.Error("selector should not open without token")
	}
	if m.status == "" {
		t.Error("should show error status")
	}
}

func TestKeyS_NoToken(t *testing.T) {
	m := testModel()
	m.token = ""
	m = pressKey(m, "s")
	if m.selectorMode != selectorNone {
		t.Error("selector should not open without token")
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

func testModelWithDB(t *testing.T) (*Model, *db.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	now := time.Now()
	d.SaveSeriesSummary(
		50, "Lorem ipsum series",
		now.Add(-2*24*time.Hour).Format("2006-01-02T15:04:05"), 1)
	d.SaveSeriesSummary(
		51, "Dolor amet series",
		now.Add(-5*24*time.Hour).Format("2006-01-02T15:04:05"), 1)

	d.SavePatch(db.PatchRow{
		ID: 100, SeriesID: 50,
		Name: "Lorem patch one", State: "new",
		Date:      now.Add(-2 * 24 * time.Hour).Format("2006-01-02T15:04:05"),
		Submitter: "Lorem",
	})
	d.SavePatch(db.PatchRow{
		ID: 101, SeriesID: 50,
		Name: "Lorem patch two", State: "new",
		Date:      now.Add(-2 * 24 * time.Hour).Format("2006-01-02T15:04:05"),
		Submitter: "Lorem",
	})
	d.SavePatch(db.PatchRow{
		ID: 200, SeriesID: 51,
		Name: "Dolor patch one", State: "new",
		Date:      now.Add(-5 * 24 * time.Hour).Format("2006-01-02T15:04:05"),
		Submitter: "Dolor",
	})

	m := NewModel(d, []string{"new"}, "test-token")
	m.width = 120
	m.height = 30
	return m, d
}

func TestReloadData_PreservesExpanded(t *testing.T) {
	m, _ := testModelWithDB(t)

	if len(m.RowData) < 2 {
		t.Fatalf("rows = %d, want >= 2", len(m.RowData))
	}

	m.RowData[0].Expanded = true
	m.reloadData()

	if !m.RowData[0].Expanded {
		t.Error("row 0 should still be expanded after reload")
	}
	if m.RowData[1].Expanded {
		t.Error("row 1 should still be collapsed after reload")
	}
}

func TestReloadData_PreservesSelection(t *testing.T) {
	m, _ := testModelWithDB(t)

	m = pressKey(m, "j") // move to row 1
	savedID := m.selectedID
	if savedID == "" {
		t.Fatal("selectedID is empty after navigation")
	}

	m.reloadData()

	if m.selectedID != savedID {
		t.Errorf("selectedID = %q, want %q",
			m.selectedID, savedID)
	}
}

func TestReloadData_SelectionStableOnNewSeries(t *testing.T) {
	m, d := testModelWithDB(t)

	m = pressKey(m, "j") // move to row 1 (second series)
	savedID := m.selectedID
	savedRow := m.selectedRow

	// Insert a newer series — it will appear at index 0
	// because GetActiveSeries orders by date DESC
	now := time.Now()
	d.SaveSeriesSummary(
		52, "Sit amet new series",
		now.Format("2006-01-02T15:04:05"), 1)
	d.SavePatch(db.PatchRow{
		ID: 300, SeriesID: 52,
		Name: "New patch", State: "new",
		Date:      now.Format("2006-01-02T15:04:05"),
		Submitter: "Sit",
	})

	m.reloadData()

	if m.selectedID != savedID {
		t.Errorf("selectedID changed: %q -> %q",
			savedID, m.selectedID)
	}
	// Index should have shifted by 1 (new series at top)
	if m.selectedRow <= savedRow {
		t.Errorf("selectedRow should have shifted: was %d, now %d",
			savedRow, m.selectedRow)
	}
	// Verify the item at selectedRow has the right ID
	items := m.getVisibleItems()
	if m.selectedRow < len(items) {
		if items[m.selectedRow].data[0] != savedID {
			t.Errorf("item at selectedRow has ID %q, want %q",
				items[m.selectedRow].data[0], savedID)
		}
	}
}

func TestReloadData_SubRowSelectionPreserved(t *testing.T) {
	m, _ := testModelWithDB(t)

	// Expand first series
	m = pressSpecialKey(m, tea.KeyEnter)
	// Navigate to first sub-row
	m = pressKey(m, "j")

	savedID := m.selectedID
	if savedID == "" {
		t.Fatal("selectedID empty on sub-row")
	}

	m.reloadData()

	if m.selectedID != savedID {
		t.Errorf("selectedID changed: %q -> %q",
			savedID, m.selectedID)
	}
	items := m.getVisibleItems()
	if m.selectedRow >= len(items) {
		t.Fatalf("selectedRow %d out of range %d",
			m.selectedRow, len(items))
	}
	item := items[m.selectedRow]
	if !item.isSubRow {
		t.Error("selection should still be on a sub-row")
	}
	if item.data[0] != savedID {
		t.Errorf("selected item ID = %q, want %q",
			item.data[0], savedID)
	}
}

func TestToggleShowAll(t *testing.T) {
	m, d := testModelWithDB(t)

	// Add an accepted patch in a different series
	now := time.Now()
	d.SaveSeriesSummary(
		52, "Accepted series",
		now.Add(-10*24*time.Hour).Format("2006-01-02T15:04:05"), 1)
	d.SavePatch(db.PatchRow{
		ID: 300, SeriesID: 52,
		Name: "Accepted patch", State: "accepted",
		Date:      now.Add(-10 * 24 * time.Hour).Format("2006-01-02T15:04:05"),
		Submitter: "Lorem",
	})

	// Default: only active (new) patches
	initialRows := len(m.RowData)

	// Toggle to show all
	m = pressKey(m, "a")
	if !m.showAll {
		t.Error("showAll should be true after pressing 'a'")
	}
	if len(m.RowData) <= initialRows {
		t.Errorf("should show more rows in 'all' mode: %d vs %d",
			len(m.RowData), initialRows)
	}

	// Toggle back to active only
	m = pressKey(m, "a")
	if m.showAll {
		t.Error("showAll should be false after second 'a'")
	}
	if len(m.RowData) != initialRows {
		t.Errorf("should show same rows as before: %d vs %d",
			len(m.RowData), initialRows)
	}
}
