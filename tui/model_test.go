package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/db"
	"leadlight/status"
)

func testModel() *Model {
	columns := []ColumnDef{
		{Title: "ID", FixedWidth: 10, Visible: true},
		{Title: "Name", Visible: true},
		{Title: "Status", FixedWidth: 15, Visible: true},
		{Title: "Desc", FixedWidth: 15, Visible: true},
	}
	rows := []RowData{
		{
			Data:  []string{"1", "Lorem", "Active", "Ipsum"},
			Style: RowStyle{Background: "reviewed"},
			SubRows: [][]string{
				{"1.1", "Sub A", "", "Detail A"},
				{"1.2", "Sub B", "", "Detail B"},
			},
		},
		{
			Data:  []string{"2", "Dolor", "Pending", "Amet"},
			Style: RowStyle{Background: "aging"},
		},
		{
			Data:  []string{"3", "Sit", "Away", "Consect"},
			Style: RowStyle{Background: "closed"},
			SubRows: [][]string{
				{"3.1", "Sub C", "", "Detail C"},
			},
		},
	}
	m := NewModelWithData(columns, rows, ColIndex(2))
	m.states = []string{"Active", "Inactive", "Pending", "Away"}
	m.token = "test-token"
	m.Status = status.NewRegistry(nil)
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

func TestSpaceExpand(t *testing.T) {
	m := testModel()
	if m.RowData[0].Expanded {
		t.Error("row 0 should start collapsed")
	}

	m = pressKey(m, " ")
	if !m.RowData[0].Expanded {
		t.Error("row 0 should be expanded after space")
	}

	items := m.getVisibleItems()
	if len(items) != 5 {
		t.Errorf("visible items = %d, want 5", len(items))
	}

	// Space again collapses
	m = pressKey(m, " ")
	if m.RowData[0].Expanded {
		t.Error("row 0 should be collapsed after second space")
	}
}

func TestSpaceNoExpandOnLeafRow(t *testing.T) {
	m := testModel()
	m = pressKey(m, "j") // row 1 has no sub-rows
	m = pressKey(m, " ")
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
	// Row 0 has state "Active" which doesn't match any real
	// state, so cursor stays at 0
	m = pressKey(m, "s")
	if m.selectorCursor != 0 {
		t.Errorf("selectorCursor = %d, want 0",
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
	last := len(AllPatchStates) - 1
	if m.selectorCursor != last {
		t.Errorf("cursor = %d, want %d (wrapped)",
			m.selectorCursor, last)
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

	// Type "rfc" to filter — should match only "rfc"
	m = pressKey(m, "r")
	m = pressKey(m, "f")
	m = pressKey(m, "c")
	if m.selectorFilter != "rfc" {
		t.Errorf("filter = %q", m.selectorFilter)
	}
	filtered, _ := m.filteredOptions()
	if len(filtered) != 1 || filtered[0] != "rfc" {
		t.Errorf("filtered = %v, want [rfc]", filtered)
	}

	// Backspace removes characters
	m = pressSpecialKey(m, tea.KeyBackspace)
	m = pressSpecialKey(m, tea.KeyBackspace)
	m = pressSpecialKey(m, tea.KeyBackspace)
	if m.selectorFilter != "" {
		t.Errorf("filter = %q after backspace", m.selectorFilter)
	}
	filtered, _ = m.filteredOptions()
	if len(filtered) != len(AllPatchStates) {
		t.Errorf("filtered = %d, want %d",
			len(filtered), len(AllPatchStates))
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
	msg, _ := m.Status.Active()
	if msg == "" {
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

func copyColumns() []ColumnDef {
	cols := make([]ColumnDef, len(PatchworkColumns))
	copy(cols, PatchworkColumns)
	return cols
}

func TestColumnWidths_CommentsAutoSwitch(t *testing.T) {
	// Fixed sum excluding C and Comments:
	// ID(10)+Ver(4)+State(9)+Submitter(18)+Age(5)+AFRT(9)+Checks(9)+Delegate(8) = 72
	// Indicator = 2, so remaining = width - 74
	// Comments visible when remaining - 15 >= 90, i.e. remaining >= 105, i.e. width >= 179
	tests := []struct {
		name             string
		width            int
		wantCVisible     bool
		wantCommVisible  bool
		wantNameWidth    int
		wantCommentWidth int
	}{
		{"wide shows Comments", 179, false, true, 90, 15},
		{"narrow shows C", 178, true, false, 101, 3},
		{"very wide", 250, false, true, 161, 15},
		{"small terminal", 100, true, false, 23, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{ColumnDefs: copyColumns(), width: tt.width}
			widths := m.columnWidths()
			if m.ColumnDefs[ColC].Visible != tt.wantCVisible {
				t.Errorf("ColC.Visible = %v, want %v",
					m.ColumnDefs[ColC].Visible, tt.wantCVisible)
			}
			if m.ColumnDefs[ColComments].Visible != tt.wantCommVisible {
				t.Errorf("ColComments.Visible = %v, want %v",
					m.ColumnDefs[ColComments].Visible, tt.wantCommVisible)
			}
			if widths[2] != tt.wantNameWidth {
				t.Errorf("Name width = %d, want %d", widths[2], tt.wantNameWidth)
			}
			activeCol := ColC
			if tt.wantCommVisible {
				activeCol = ColComments
			}
			if widths[activeCol] != tt.wantCommentWidth {
				t.Errorf("comment width = %d, want %d",
					widths[activeCol], tt.wantCommentWidth)
			}
		})
	}
}

func TestColumnWidths_NoOscillation(t *testing.T) {
	for width := 80; width <= 300; width++ {
		m := &Model{ColumnDefs: copyColumns(), width: width}
		w1 := m.columnWidths()
		vis1 := m.ColumnDefs[ColComments].Visible
		name1 := w1[2]
		w2 := m.columnWidths()
		vis2 := m.ColumnDefs[ColComments].Visible
		name2 := w2[2]
		if vis1 != vis2 || name1 != name2 {
			t.Fatalf("width %d: oscillation: vis %v->%v, name %d->%d",
				width, vis1, vis2, name1, name2)
		}
	}
}

func TestColumnWidths_InvisibleSkipped(t *testing.T) {
	m := &Model{ColumnDefs: copyColumns(), width: 150}
	widths := m.columnWidths()
	if widths[ColComments] != 0 {
		t.Errorf("hidden Comments width = %d, want 0", widths[ColComments])
	}
	if widths[ColC] != 3 {
		t.Errorf("visible C width = %d, want 3", widths[ColC])
	}
}

func TestColumnWidths_CustomColumns(t *testing.T) {
	cols := []ColumnDef{
		{Title: "A", FixedWidth: 10, Visible: true},
		{Title: "B", Visible: true},
		{Title: "C", FixedWidth: 5, Visible: true},
	}
	m := &Model{ColumnDefs: cols, width: 80}
	widths := m.columnWidths()
	want := 80 - indicatorWidth - 15
	if widths[1] != want {
		t.Errorf("flex width = %d, want %d", widths[1], want)
	}
}

func TestIsRowFetching_SeriesRow(t *testing.T) {
	reg := status.NewRegistry(nil)
	m := &Model{Status: reg}
	item := visibleItem{
		data:     []string{"50", "", "Lorem series"},
		isSubRow: false,
	}
	if m.isRowFetching(item) {
		t.Error("should not be fetching before start")
	}
	reg.StartFetchAndSetStatus(100, 50, status.BgComments, "fetching...")
	if !m.isRowFetching(item) {
		t.Error("series 50 should be fetching (patch 100 in series)")
	}
	reg.EndFetch(100)
	if m.isRowFetching(item) {
		t.Error("should not be fetching after end")
	}
}

func TestIsRowFetching_SubRow(t *testing.T) {
	reg := status.NewRegistry(nil)
	m := &Model{Status: reg}
	item := visibleItem{
		data:     []string{"100", "", "Lorem patch"},
		isSubRow: true,
	}
	if m.isRowFetching(item) {
		t.Error("should not be fetching before start")
	}
	reg.StartFetchAndSetStatus(100, 50, status.Detail, "fetching...")
	if !m.isRowFetching(item) {
		t.Error("patch 100 should be fetching")
	}
	// Different patch in same series — sub-row 100 not fetching
	reg.EndFetch(100)
	reg.StartFetchAndSetStatus(101, 50, status.Detail, "fetching...")
	if m.isRowFetching(item) {
		t.Error("patch 100 should not be fetching (101 is)")
	}
	reg.EndFetch(101)
}

func TestIsRowFetching_EmptyData(t *testing.T) {
	reg := status.NewRegistry(nil)
	m := &Model{Status: reg}
	item := visibleItem{data: nil}
	if m.isRowFetching(item) {
		t.Error("should not crash on empty data")
	}
}

func TestIsRowFetching_NilStatus(t *testing.T) {
	m := &Model{}
	item := visibleItem{data: []string{"50"}}
	if m.isRowFetching(item) {
		t.Error("should not crash with nil status")
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
	m = pressKey(m, " ")
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
	m = pressKey(m, " ")
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

func TestFilterMode(t *testing.T) {
	m := testModel()

	m = pressKey(m, "/")
	if !m.filterEditing {
		t.Error("filterEditing should be true after /")
	}

	m = pressKey(m, "l")
	m = pressKey(m, "o")
	if m.filterText != "lo" {
		t.Errorf("filterText = %q, want 'lo'", m.filterText)
	}

	// Should filter visible items
	items := m.getVisibleItems()
	for _, item := range items {
		found := false
		for _, field := range item.data {
			if strings.Contains(strings.ToLower(field), "lo") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("item %v doesn't match filter 'lo'",
				item.data)
		}
	}
}

func TestFilterMode_Backspace(t *testing.T) {
	m := testModel()
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyBackspace)
	if m.filterText != "l" {
		t.Errorf("filterText = %q after backspace", m.filterText)
	}
}

func TestFilterMode_Esc(t *testing.T) {
	m := testModel()
	m = pressKey(m, "/")
	m = pressKey(m, "x")
	m = pressSpecialKey(m, tea.KeyEsc)
	if m.filterEditing {
		t.Error("filterEditing should be false after esc")
	}
	if m.filterText != "" {
		t.Errorf("filterText = %q, want empty", m.filterText)
	}
}

func TestFilterMode_Navigation(t *testing.T) {
	m := testModel()
	m = pressKey(m, "/")
	m = pressKey(m, "l") // filter to matching items

	// Arrow keys navigate during filter mode
	m = pressSpecialKey(m, tea.KeyDown)
	if m.selectedRow != 1 {
		t.Errorf("selectedRow = %d, want 1", m.selectedRow)
	}
}

func TestFilterMode_AutoExpand(t *testing.T) {
	m := testModel()
	// Row 0 has sub-rows with "Sub A", "Sub B"
	// Filter for "sub" should auto-expand to show them

	m = pressKey(m, "/")
	m = pressKey(m, "s")
	m = pressKey(m, "u")
	m = pressKey(m, "b")

	items := m.getVisibleItems()
	hasSubRow := false
	for _, item := range items {
		if item.isSubRow {
			hasSubRow = true
			break
		}
	}
	if !hasSubRow {
		t.Error("filter should auto-expand series with matching sub-rows")
	}
}

func TestFilterMode_ClearPreservesSelection(t *testing.T) {
	m := testModel()

	// Row 0 has sub-rows "Sub A", "Sub B"
	// Filter for "sub b" should show only Sub B
	m = pressKey(m, "/")
	m = pressKey(m, "s")
	m = pressKey(m, "u")
	m = pressKey(m, "b")
	m = pressKey(m, " ")
	m = pressKey(m, "b")

	items := m.getVisibleItems()
	if len(items) == 0 {
		t.Fatal("no items after filter")
	}

	// Navigate to a sub-row in the filtered results
	for i, item := range items {
		if item.isSubRow {
			m.selectedRow = i
			m.updateSelectedID()
			break
		}
	}

	savedID := m.selectedID
	if savedID == "" {
		t.Fatal("no selectedID after navigation")
	}

	// Clear the filter
	m = pressSpecialKey(m, tea.KeyEsc)

	if m.filterEditing {
		t.Error("filterEditing should be false")
	}
	if m.selectedID != savedID {
		t.Errorf("selectedID changed: %q -> %q",
			savedID, m.selectedID)
	}

	// The selected item should be visible
	items = m.getVisibleItems()
	if m.selectedRow >= len(items) {
		t.Fatalf("selectedRow %d out of range %d",
			m.selectedRow, len(items))
	}
	if items[m.selectedRow].data[0] != savedID {
		t.Errorf("item at selectedRow has ID %q, want %q",
			items[m.selectedRow].data[0], savedID)
	}
}

func TestFilterMode_ClearCollapsesExceptSelected(t *testing.T) {
	m := testModel()

	// Expand multiple series
	m = pressKey(m, " ") // expand row 0
	m = pressKey(m, "j")
	m = pressKey(m, "j")
	m = pressKey(m, "j") // now on row 1 (second series)
	m = pressKey(m, " ") // can't expand (no sub-rows)

	// Start filter and navigate
	m = pressKey(m, "/")
	m = pressKey(m, "l")               // filter
	m = pressSpecialKey(m, tea.KeyEsc) // clear

	// All series should be collapsed except the selected one's parent
	expandedCount := 0
	for _, rd := range m.RowData {
		if rd.Expanded {
			expandedCount++
		}
	}
	if expandedCount > 1 {
		t.Errorf("expanded count = %d, want <= 1", expandedCount)
	}
}

func TestFilterEditing_AllPrintableKeysAreTextInput(t *testing.T) {
	// Keys that have special meaning in normal table view
	// but should be typed into the filter during editing.
	keys := []string{
		"j", "k", "g", "G",
		"q", "a", "f", "p", "s", "d",
		"/", "`", "e", "w",
		" ",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			m := testModel()
			m = pressKey(m, "/")
			m = pressKey(m, key)
			if m.filterText != key {
				t.Errorf("key %q: filterText = %q, want %q",
					key, m.filterText, key)
			}
		})
	}
}

func TestFilterCommit(t *testing.T) {
	m := testModel()
	// Enter filter editing, type "lo", commit
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyEnter)

	if m.filterEditing {
		t.Error("filterEditing should be false after commit")
	}
	if m.filterText != "lo" {
		t.Errorf("filterText = %q, want 'lo'", m.filterText)
	}
	// Items should still be filtered
	items := m.getVisibleItems()
	for _, item := range items {
		if item.isSubRow {
			continue
		}
		found := false
		for _, f := range item.data {
			if strings.Contains(strings.ToLower(f), "lo") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("item %v should match filter 'lo'",
				item.data)
		}
	}
}

func TestFilterCommit_CollapsesAutoExpanded(t *testing.T) {
	m := testModel()
	// Filter for "sub" — auto-expands during editing
	m = pressKey(m, "/")
	m = pressKey(m, "s")
	m = pressKey(m, "u")
	m = pressKey(m, "b")

	// Verify auto-expanded during editing
	items := m.getVisibleItems()
	hasSubRow := false
	for _, item := range items {
		if item.isSubRow {
			hasSubRow = true
			break
		}
	}
	if !hasSubRow {
		t.Fatal("sub-rows should be auto-expanded during editing")
	}

	// Commit — should collapse auto-expanded rows
	m = pressSpecialKey(m, tea.KeyEnter)

	// After commit, only manually expanded rows should show subs
	expandedCount := 0
	for _, rd := range m.RowData {
		if rd.Expanded {
			expandedCount++
		}
	}
	if expandedCount > 1 {
		t.Errorf("at most 1 row should be expanded after commit, got %d",
			expandedCount)
	}
}

func TestFilterReEdit(t *testing.T) {
	m := testModel()
	// Commit a filter
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyEnter)

	// Re-enter editing with /
	m = pressKey(m, "/")
	if !m.filterEditing {
		t.Error("should be in editing mode after /")
	}
	if m.filterText != "lo" {
		t.Errorf("filterText = %q, want 'lo' (preserved)",
			m.filterText)
	}

	// Type more
	m = pressKey(m, "r")
	if m.filterText != "lor" {
		t.Errorf("filterText = %q, want 'lor'", m.filterText)
	}
}

func TestFilterCommit_EscClears(t *testing.T) {
	m := testModel()
	// Commit a filter
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyEnter)

	// Esc should clear the committed filter
	m = pressSpecialKey(m, tea.KeyEsc)
	if m.filterText != "" {
		t.Errorf("filterText = %q, want empty after esc",
			m.filterText)
	}
	if m.filterEditing {
		t.Error("filterEditing should be false after esc")
	}
}

func TestFilterCommit_QClears(t *testing.T) {
	m := testModel()
	// Commit a filter
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyEnter)

	// q should clear the committed filter, not quit
	m = pressKey(m, "q")
	if m.filterText != "" {
		t.Errorf("filterText = %q, want empty after q",
			m.filterText)
	}
}

func TestFilterCommit_NormalNavigation(t *testing.T) {
	m := testModel()
	// Commit a filter
	m = pressKey(m, "/")
	m = pressKey(m, "l")
	m = pressKey(m, "o")
	m = pressSpecialKey(m, tea.KeyEnter)

	// Space should toggle expand (not interpreted as filter char)
	items := m.getVisibleItems()
	if m.selectedRow < len(items) {
		parentIdx := items[m.selectedRow].parentIdx
		wasBefore := m.RowData[parentIdx].Expanded
		m = pressKey(m, " ")
		wasAfter := m.RowData[parentIdx].Expanded
		if wasBefore == wasAfter {
			t.Error("space should toggle expansion in committed state")
		}
	}
}

func TestFoldAccents(t *testing.T) {
	tests := []struct{ in, want string }{
		{"lorem", "lorem"},
		{"lorém", "lorem"},
		{"dölor", "dolor"},
		{"lørem", "lorem"},
		{"dølör", "dolor"},
		{"ßit", "sit"},
		{"LORÉM", "LOREM"},
	}
	for _, tt := range tests {
		got := foldAccents(tt.in)
		if got != tt.want {
			t.Errorf("foldAccents(%q) = %q, want %q",
				tt.in, got, tt.want)
		}
	}
}

func TestMatchesFilter_Accented(t *testing.T) {
	data := []string{"1", "Lorém Ípsum", "new"}
	if !matchesFilter(data, "lorem") {
		t.Error("should match accented lorem")
	}
	if !matchesFilter(data, "ipsum") {
		t.Error("should match accented ipsum")
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

func TestOpenSeriesView_CoverLetter(t *testing.T) {
	m, d := testModelWithDB(t)

	// Add a cover letter for series 50 with detail fetched
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name: "Lorem cover letter",
		Date: "2026-03-10",
	})
	d.UpdateCoverDetail(99, "Cover body", `{"To":"dev@ex"}`)

	// Press enter on the parent row (series 50)
	m = pressSpecialKey(m, tea.KeyEnter)

	if m.viewMode != viewPatch {
		t.Error("should be in patch view mode")
	}
	if m.viewingCoverID == 0 {
		t.Error("viewingCoverID should be set")
	}
	if len(m.viewportLines) == 0 {
		t.Error("viewportLines should have content")
	}
}

func TestOpenSeriesView_NoCover_SinglePatch(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	now := time.Now()
	date := now.Format("2006-01-02T15:04:05")

	d.SaveSeriesSummary(60, "Single patch series", date, 1)
	d.SavePatch(db.PatchRow{
		ID: 300, SeriesID: 60,
		Name: "Only patch", State: "new",
		Date: date, Submitter: "Lorem",
	})
	d.UpdatePatchDetail(300, "Body", "---", `{"To":"dev@ex"}`, "[]")

	m := NewModel(d, []string{"new"}, "test-token")
	m.width = 120
	m.height = 30

	// Press enter on the parent row
	m = pressSpecialKey(m, tea.KeyEnter)

	if m.viewMode != viewPatch {
		t.Error("should open patch view")
	}
	if m.viewingPatchID == 0 {
		t.Error("should fall back to viewing the single patch")
	}
}

func TestOpenSeriesView_NoCover_OpensFirstPatch(t *testing.T) {
	m, _ := testModelWithDB(t)
	// testModelWithDB creates series 50 with 2 patches, no cover

	m = pressSpecialKey(m, tea.KeyEnter)

	if m.viewMode != viewPatch {
		t.Error("should open first patch when no cover")
	}
	if m.viewingPatchID == 0 {
		t.Error("should be viewing a patch")
	}
}

func TestCommentNavigation(t *testing.T) {
	m := testModel()
	// Set up viewport mode with comments
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.viewComments = []CommentInfo{
		{ID: 1, Submitter: "Lorem", Date: "2026-03-12",
			Subject: "Re: patch", Content: "Looks good"},
		{ID: 2, Submitter: "Dolor", Date: "2026-03-14",
			Subject: "Re: patch", Content: "Agreed"},
	}
	m.viewportLines = []string{"patch content"}

	// Right → first comment
	m = pressSpecialKey(m, tea.KeyRight)
	if m.viewCommentIdx != 0 {
		t.Errorf("idx = %d, want 0", m.viewCommentIdx)
	}

	// Right → second comment
	m = pressSpecialKey(m, tea.KeyRight)
	if m.viewCommentIdx != 1 {
		t.Errorf("idx = %d, want 1", m.viewCommentIdx)
	}

	// Right → wrap to patch
	m = pressSpecialKey(m, tea.KeyRight)
	if m.viewCommentIdx != -1 {
		t.Errorf("idx = %d, want -1 (wrapped)", m.viewCommentIdx)
	}

	// Left → wrap to last comment
	m = pressSpecialKey(m, tea.KeyLeft)
	if m.viewCommentIdx != 1 {
		t.Errorf("idx = %d, want 1 (wrapped back)", m.viewCommentIdx)
	}
}

func TestCommentNavigation_NoComments(t *testing.T) {
	m := testModel()
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.viewComments = nil
	m.viewportLines = []string{"content"}

	// Right should do nothing
	m = pressSpecialKey(m, tea.KeyRight)
	if m.viewCommentIdx != -1 {
		t.Errorf("idx = %d, want -1 (no comments)", m.viewCommentIdx)
	}
}

func TestCommentNavigation_LoadsOnOpen(t *testing.T) {
	m, d := testModelWithDB(t)

	// Add a comment for patch 100
	d.InsertComment(db.CommentRow{
		ID: 300, PatchID: 100,
		Submitter: "Lorem", Date: "2026-03-12",
		Subject: "Re: patch", Content: "Looks good",
	})

	// Expand first series and navigate to sub-row
	m = pressKey(m, " ")
	m = pressKey(m, "j") // sub-row

	// Open the patch view
	m = pressSpecialKey(m, tea.KeyEnter)

	if len(m.viewComments) != 1 {
		t.Errorf("viewComments = %d, want 1",
			len(m.viewComments))
	}
	if m.viewCommentIdx != -1 {
		t.Errorf("viewCommentIdx = %d, want -1", m.viewCommentIdx)
	}
}

// --- Log anchor tests ---

func setupLogModel(t *testing.T, lineCount int) *Model {
	t.Helper()
	m := testModel()
	m.LogBuf = NewLogBuffer()
	for i := 0; i < lineCount; i++ {
		m.LogBuf.Write([]byte(fmt.Sprintf("line %d\n", i)))
	}
	m = pressKey(m, "`")
	m = pressKey(m, "tab")
	m.View()
	return m
}

func setupLogModelWrapped(t *testing.T, lineCount int) *Model {
	t.Helper()
	m := testModel()
	m.LogBuf = NewLogBuffer()
	long := strings.Repeat("x", 200)
	for i := 0; i < lineCount; i++ {
		m.LogBuf.Write([]byte(fmt.Sprintf("%d %s\n", i, long)))
	}
	m = pressKey(m, "`")
	m = pressKey(m, "tab")
	m.View()
	return m
}

func addLogLines(m *Model, n int) {
	for i := 0; i < n; i++ {
		m.LogBuf.Write([]byte(fmt.Sprintf("new %d\n", i)))
	}
}

func logVisibleLines(m *Model) int {
	return m.height - m.renderHeight() - 2
}

func logAnchorIdx(m *Model) int {
	lines := m.LogBuf.Lines()
	currentCount := m.LogBuf.Count()
	return len(lines) - 1 - (currentCount - m.logAnchor)
}

func countVisualLines(m *Model, anchorIdx int) int {
	lines := m.LogBuf.Lines()
	total := 0
	for i := 0; i <= anchorIdx && i < len(lines); i++ {
		total += len(wrapLogLine(lines[i], m.width))
	}
	return total
}

func TestLogAnchor_AutoScroll(t *testing.T) {
	m := setupLogModel(t, 30)
	if m.logLastSeen != m.logAnchor {
		t.Errorf("want equal: lastSeen=%d anchor=%d",
			m.logLastSeen, m.logAnchor)
	}
	addLogLines(m, 5)
	m.View()
	if m.logLastSeen != m.LogBuf.Count() {
		t.Errorf("lastSeen=%d, want %d",
			m.logLastSeen, m.LogBuf.Count())
	}
	if m.logLastSeen != m.logAnchor {
		t.Errorf("auto-scroll broken: lastSeen=%d anchor=%d",
			m.logLastSeen, m.logAnchor)
	}
}

func TestLogAnchor_ScrollUpFreezes(t *testing.T) {
	m := setupLogModel(t, 20)
	m = pressKey(m, "k")
	anchor := m.logAnchor
	addLogLines(m, 5)
	m.View()
	if m.logAnchor != anchor {
		t.Errorf("anchor moved: was %d, now %d", anchor, m.logAnchor)
	}
	if m.logLastSeen != m.LogBuf.Count() {
		t.Errorf("lastSeen=%d, want %d",
			m.logLastSeen, m.LogBuf.Count())
	}
}

func TestLogAnchor_ScrollDownToBottom(t *testing.T) {
	m := setupLogModel(t, 20)
	m = pressKey(m, "k")
	m = pressKey(m, "k")
	m = pressKey(m, "k")
	m = pressKey(m, "j")
	m = pressKey(m, "j")
	m = pressKey(m, "j")
	m.View()
	if m.logLastSeen != m.logAnchor {
		t.Errorf("not auto-scrolling: lastSeen=%d anchor=%d",
			m.logLastSeen, m.logAnchor)
	}
}

func TestLogAnchor_GJumpsToLatest(t *testing.T) {
	m := setupLogModel(t, 20)
	m = pressKey(m, "k")
	addLogLines(m, 5)
	m.View()
	if m.logAnchor >= m.logLastSeen {
		t.Fatal("should be anchored away from latest")
	}
	m = pressKey(m, "G")
	m.View()
	if m.logLastSeen != m.logAnchor {
		t.Errorf("after G: lastSeen=%d anchor=%d",
			m.logLastSeen, m.logAnchor)
	}
}

func TestLogAnchor_NewMessagesCount(t *testing.T) {
	m := setupLogModel(t, 30)
	m = pressKey(m, "k")
	addLogLines(m, 3)
	m.View()
	want := 4 // 1 (from up) + 3 (new messages)
	got := m.logLastSeen - m.logAnchor
	if got != want {
		t.Errorf("new count = %d, want %d", got, want)
	}
}

func TestLogAnchor_DownClampedAtLastSeen(t *testing.T) {
	m := setupLogModel(t, 5)
	m = pressKey(m, "j")
	if m.logAnchor > m.logLastSeen {
		t.Errorf("anchor %d > lastSeen %d",
			m.logAnchor, m.logLastSeen)
	}
}

func TestLogAnchor_ExpiredEntriesFillLoop(t *testing.T) {
	m := setupLogModel(t, logBufMaxLines)
	m = pressKey(m, "g") // home
	m.View()
	anchorBefore := m.logAnchor

	addLogLines(m, 50)
	m.View()

	if m.logAnchor <= anchorBefore {
		t.Errorf("anchor should advance: was %d, now %d",
			anchorBefore, m.logAnchor)
	}
	idx := logAnchorIdx(m)
	vis := logVisibleLines(m)
	if idx+1 < vis {
		t.Errorf("viewport not full: %d entries, need %d",
			idx+1, vis)
	}
}

func TestLogAnchor_AllEntriesExpired(t *testing.T) {
	m := setupLogModel(t, 20)
	m = pressKey(m, "k")
	m.View()

	addLogLines(m, logBufMaxLines+100)
	m.View()

	currentCount := m.LogBuf.Count()
	firstAvailable := currentCount - len(m.LogBuf.Lines())
	if m.logAnchor <= firstAvailable {
		t.Errorf("anchor %d <= firstAvailable %d",
			m.logAnchor, firstAvailable)
	}
	if m.logAnchor == m.logLastSeen {
		t.Error("should still be anchored, not auto-scrolling")
	}
	idx := logAnchorIdx(m)
	vis := logVisibleLines(m)
	if idx+1 < vis {
		t.Errorf("viewport not full: %d entries, need %d",
			idx+1, vis)
	}
}

func TestLogAnchor_ExpiredButFewEntries(t *testing.T) {
	m := setupLogModel(t, 5)
	m = pressKey(m, "g")
	m.View()
	if m.logAnchor > m.logLastSeen {
		t.Errorf("anchor %d > lastSeen %d",
			m.logAnchor, m.logLastSeen)
	}
}

func TestLogAnchor_WrappedLinesFillViewport(t *testing.T) {
	m := setupLogModelWrapped(t, logBufMaxLines)
	m = pressKey(m, "g") // home
	m.View()

	addLogLines(m, 50)
	m.View()

	idx := logAnchorIdx(m)
	vis := logVisibleLines(m)
	total := countVisualLines(m, idx)
	if total < vis {
		t.Errorf("viewport not full with wrapped lines: "+
			"%d visual lines, need %d", total, vis)
	}
}

func TestLogAnchor_WrappedLinesScrollByWholeEntry(t *testing.T) {
	m := testModel()
	m.LogBuf = NewLogBuffer()
	long := strings.Repeat("L", 300)
	for i := 0; i < 20; i++ {
		if i%3 == 0 {
			m.LogBuf.Write([]byte(
				fmt.Sprintf("%d %s\n", i, long)))
		} else {
			m.LogBuf.Write([]byte(
				fmt.Sprintf("short %d\n", i)))
		}
	}
	m = pressKey(m, "`")
	m = pressKey(m, "tab")
	m.View()

	a0 := m.logAnchor
	m = pressKey(m, "k")
	if m.logAnchor != a0-1 {
		t.Errorf("after up: anchor=%d, want %d",
			m.logAnchor, a0-1)
	}
	m = pressKey(m, "k")
	if m.logAnchor != a0-2 {
		t.Errorf("after 2 up: anchor=%d, want %d",
			m.logAnchor, a0-2)
	}
	m = pressKey(m, "j")
	if m.logAnchor != a0-1 {
		t.Errorf("after down: anchor=%d, want %d",
			m.logAnchor, a0-1)
	}
}

func TestLogAnchor_WrappedLinesPageUp(t *testing.T) {
	m := setupLogModelWrapped(t, 50)
	a0 := m.logAnchor
	delta := max(logVisibleLines(m)/2, 1)
	m = pressSpecialKey(m, tea.KeyPgUp)
	if m.logAnchor != a0-delta {
		t.Errorf("after pgup: anchor=%d, want %d (delta %d)",
			m.logAnchor, a0-delta, delta)
	}
}

func TestGetVisibleItems_CacheHit(t *testing.T) {
	m := testModel()
	items1 := m.getVisibleItems()
	items2 := m.getVisibleItems()
	if !m.cachedVisibleItemsValid {
		t.Error("cache should be valid after getVisibleItems")
	}
	if len(items1) != len(items2) {
		t.Errorf("cached result differs: %d vs %d", len(items1), len(items2))
	}
	// Verify it's the exact same slice (pointer equality)
	if &items1[0] != &items2[0] {
		t.Error("second call should return cached slice, not recompute")
	}
}

func TestGetVisibleItems_InvalidateOnDataChange(t *testing.T) {
	m := testModel()
	items1 := m.getVisibleItems()
	if !m.cachedVisibleItemsValid {
		t.Error("cache should be valid")
	}

	// Simulate data change
	m.RowData = append(m.RowData, RowData{
		Data:  []string{"4", "Amet", "Active", "New row"},
		Style: RowStyle{Background: "active"},
	})
	m.invalidateAllCaches()

	if m.cachedVisibleItemsValid {
		t.Error("cache should be invalid after invalidateRowCache")
	}
	items2 := m.getVisibleItems()
	if len(items2) <= len(items1) {
		t.Error("recomputed items should include the new row")
	}
}

func TestGetVisibleItems_InvalidateOnFilter(t *testing.T) {
	m := testModel()
	items1 := m.getVisibleItems()

	// Apply a filter
	m.filterEditing = true
	m.filterText = "lorem"
	m.applyFilter()

	items2 := m.getVisibleItems()
	if len(items2) >= len(items1) {
		t.Error("filtered items should be fewer than unfiltered")
	}
}

func TestGetVisibleItems_InvalidateOnExpand(t *testing.T) {
	m := testModel()
	items1 := m.getVisibleItems()

	// Expand first row (has sub-rows)
	m.RowData[0].Expanded = true
	m.invalidateAllCaches()

	items2 := m.getVisibleItems()
	if len(items2) <= len(items1) {
		t.Error("expanded items should include sub-rows")
	}
}

func TestExtractHTTPStatus(t *testing.T) {
	tests := []struct {
		line     string
		wantCode int
		wantLen  int
	}{
		{"HTTP GET (go) -> 200 https://ex", 200, 3},
		{"HTTP GET (curl) -> 404 https://ex", 404, 3},
		{"HTTP GET (go) -> 502 https://ex", 502, 3},
		{"HTTP GET (go) -> error: timeout", -1, 5},
		{"SYNC: fetching details", 0, 0},
		{"no arrow here", 0, 0},
		{"-> abc not a number", 0, 0},
		{"-> 99 too short", 0, 0},
		{"-> 200", 200, 3},
	}
	for _, tt := range tests {
		code, _, length := extractHTTPStatus(tt.line)
		if code != tt.wantCode || length != tt.wantLen {
			t.Errorf("extractHTTPStatus(%q) = (%d, _, %d), "+
				"want (%d, _, %d)",
				tt.line, code, length,
				tt.wantCode, tt.wantLen)
		}
	}
}

func TestSeriesRowCache_Hit(t *testing.T) {
	m := testModel()
	// Populate cache via buildStyledRow with cache=true
	widths := m.columnWidths()
	items := m.getVisibleItems()
	if len(items) == 0 {
		t.Fatal("no visible items")
	}
	blank := "  "
	row1 := m.buildStyledRow(items[0], widths, blank, true)
	// Second call should return cached value
	row2 := m.buildStyledRow(items[0], widths, blank, true)
	if row1 != row2 {
		t.Error("second call should return cached row")
	}
	// Verify cache entry exists
	seriesID, _ := strconv.Atoi(m.RowData[items[0].parentIdx].Data[ColID])
	sc := m.cachedRenderedRows[seriesID]
	if sc == nil || sc.seriesRow == "" {
		t.Error("cache entry should exist for series row")
	}
}

func TestSeriesRowCache_SubRowHit(t *testing.T) {
	m := testModel()
	// Expand first row to get sub-rows
	m.RowData[0].Expanded = true
	m.invalidateVisibleItems()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	// Find the first sub-row
	var subItem visibleItem
	found := false
	for _, item := range items {
		if item.isSubRow {
			subItem = item
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no sub-rows found after expanding")
	}
	blank := "  "
	row1 := m.buildStyledRow(subItem, widths, blank, true)
	row2 := m.buildStyledRow(subItem, widths, blank, true)
	if row1 != row2 {
		t.Error("second call should return cached sub-row")
	}
}

func TestSeriesRowCache_NoCacheWhenFetching(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	if len(items) == 0 {
		t.Fatal("no visible items")
	}
	// Build with cache=false (simulates fetching row with spinner)
	m.buildStyledRow(items[0], widths, "▸⠋", false)
	seriesID, _ := strconv.Atoi(m.RowData[items[0].parentIdx].Data[ColID])
	if sc := m.cachedRenderedRows[seriesID]; sc != nil && sc.seriesRow != "" {
		t.Error("should not cache when cache=false")
	}
}

func TestInvalidateSeriesCache_TargetedClear(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	// Populate cache for all series rows
	seriesIDs := map[int]bool{}
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
			sid, _ := strconv.Atoi(m.RowData[item.parentIdx].Data[ColID])
			seriesIDs[sid] = true
		}
	}
	if len(m.cachedRenderedRows) < 2 {
		t.Fatalf("need at least 2 cached series, got %d",
			len(m.cachedRenderedRows))
	}
	// Pick one series to invalidate
	var targetID int
	for id := range seriesIDs {
		targetID = id
		break
	}
	m.invalidateSeriesCache([]int{targetID})
	// Target should be gone
	if _, ok := m.cachedRenderedRows[targetID]; ok {
		t.Errorf("series %d should be cleared", targetID)
	}
	// Others should remain
	remaining := 0
	for id := range seriesIDs {
		if id != targetID {
			if _, ok := m.cachedRenderedRows[id]; ok {
				remaining++
			}
		}
	}
	if remaining == 0 {
		t.Error("other series should still be cached")
	}
}

func TestInvalidateSeriesCache_MultipleSeries(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	var allIDs []int
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
			sid, _ := strconv.Atoi(m.RowData[item.parentIdx].Data[ColID])
			allIDs = append(allIDs, sid)
		}
	}
	if len(allIDs) < 3 {
		t.Skipf("need at least 3 series, got %d", len(allIDs))
	}
	// Invalidate first two, keep the third
	m.invalidateSeriesCache(allIDs[:2])
	for _, id := range allIDs[:2] {
		if _, ok := m.cachedRenderedRows[id]; ok {
			t.Errorf("series %d should be cleared", id)
		}
	}
	if _, ok := m.cachedRenderedRows[allIDs[2]]; !ok {
		t.Errorf("series %d should still be cached", allIDs[2])
	}
}

func TestInvalidateSeriesCache_NonexistentSeries(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	before := len(m.cachedRenderedRows)
	m.invalidateSeriesCache([]int{999999})
	if len(m.cachedRenderedRows) != before {
		t.Error("invalidating nonexistent series should not affect cache")
	}
}

func TestInvalidateAllCaches_ClearsEverything(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	if len(m.cachedRenderedRows) == 0 {
		t.Fatal("cache should be populated")
	}
	m.invalidateAllCaches()
	if len(m.cachedRenderedRows) != 0 {
		t.Error("invalidateAllCaches should clear all rendered rows")
	}
	if m.cachedVisibleItemsValid {
		t.Error("invalidateAllCaches should clear visible items cache")
	}
}

func TestFilterChange_KeepsRenderedCache(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	before := len(m.cachedRenderedRows)
	if before == 0 {
		t.Fatal("cache should be populated")
	}
	// Start filter — should NOT clear rendered row cache
	m.startFilter()
	if len(m.cachedRenderedRows) != before {
		t.Error("startFilter should not clear rendered row cache")
	}
	// Apply filter — should NOT clear rendered row cache
	m.filterText = "lorem"
	m.applyFilter()
	if len(m.cachedRenderedRows) != before {
		t.Error("applyFilter should not clear rendered row cache")
	}
	// Clear filter — should NOT clear rendered row cache
	m.clearFilter()
	if len(m.cachedRenderedRows) != before {
		t.Error("clearFilter should not clear rendered row cache")
	}
}

func TestExpandCollapse_KeepsRenderedCache(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	before := len(m.cachedRenderedRows)
	// Expand a row — should NOT clear rendered row cache
	m.RowData[0].Expanded = true
	m.invalidateVisibleItems()
	if len(m.cachedRenderedRows) != before {
		t.Error("expand should not clear rendered row cache")
	}
}

func TestResize_ClearsRenderedCache(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	if len(m.cachedRenderedRows) == 0 {
		t.Fatal("cache should be populated")
	}
	// Resize — SHOULD clear rendered row cache
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	if len(m.cachedRenderedRows) != 0 {
		t.Error("resize should clear rendered row cache")
	}
}

func TestSyncUpdateMsg_TargetedInvalidation(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	var allIDs []int
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
			sid, _ := strconv.Atoi(m.RowData[item.parentIdx].Data[ColID])
			allIDs = append(allIDs, sid)
		}
	}
	if len(allIDs) < 2 {
		t.Skipf("need at least 2 series, got %d", len(allIDs))
	}
	// Targeted invalidation
	m.Update(SyncUpdateMsg{SeriesIDs: []int{allIDs[0]}})
	if _, ok := m.cachedRenderedRows[allIDs[0]]; ok {
		t.Errorf("series %d should be cleared", allIDs[0])
	}
	if _, ok := m.cachedRenderedRows[allIDs[1]]; !ok {
		t.Errorf("series %d should still be cached", allIDs[1])
	}
}

func TestSyncUpdateMsg_FullInvalidation(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	if len(m.cachedRenderedRows) == 0 {
		t.Fatal("cache should be populated")
	}
	// Full invalidation (empty SeriesIDs)
	m.Update(SyncUpdateMsg{})
	if len(m.cachedRenderedRows) != 0 {
		t.Error("empty SeriesIDs should clear all rendered rows")
	}
}

func TestCacheSurvivesScrolling(t *testing.T) {
	m := testModel()
	widths := m.columnWidths()
	items := m.getVisibleItems()
	blank := "  "
	for _, item := range items {
		if !item.isSubRow {
			m.buildStyledRow(item, widths, blank, true)
		}
	}
	before := len(m.cachedRenderedRows)
	// Scroll down
	m.selectedRow = 1
	m.scrollOffset = 0
	if len(m.cachedRenderedRows) != before {
		t.Error("scrolling should not clear rendered row cache")
	}
}

func TestSeriesRowCache_StaleAge(t *testing.T) {
	m, _ := testModelWithDB(t)
	widths := m.columnWidths()
	items := m.getVisibleItems()
	if len(items) == 0 {
		t.Skip("no visible items")
	}
	if int(ColAge) >= len(items[0].data) {
		t.Skip("test model has no age column")
	}
	blank := "  "
	// Cache a row
	row1 := m.buildStyledRow(items[0], widths, blank, true)
	// Same data → cache hit
	row2 := m.buildStyledRow(items[0], widths, blank, true)
	if row1 != row2 {
		t.Error("same age should be a cache hit")
	}
	// Change the raw date to a very different time → cache miss
	m.RowData[items[0].parentIdx].Data[ColAge] = "2020-01-01T00:00:00"
	m.invalidateVisibleItems()
	items = m.getVisibleItems()
	row3 := m.buildStyledRow(items[0], widths, blank, true)
	if row3 == row1 {
		t.Error("different age should cause cache miss")
	}
}

func TestRenderPatchView_NoTrailingSpaces(t *testing.T) {
	m := testModel()
	m.viewportLines = []string{
		"Subject: Lorem ipsum dolor sit amet",
		"From: Lorem <lorem@ipsum.example>",
		"",
		"Short line",
		"A much longer line with more content to test varying widths across the viewport",
		"  indented context line",
		"+ added line",
		"- removed line",
	}

	output := m.renderPatchView()
	// The status bar is the last line — check all lines above it
	lines := strings.Split(output, "\n")
	for i, line := range lines[:len(lines)-1] {
		stripped := strings.TrimRight(line, " ")
		if len(stripped) != len(line) {
			t.Errorf("line %d has trailing spaces: %q", i, line)
		}
	}
}

func TestRenderPatchView_LineCount(t *testing.T) {
	m := testModel()
	visible := m.viewportVisibleLines()

	tests := []struct {
		name     string
		numLines int
	}{
		{"fewer than visible", visible / 2},
		{"exactly visible", visible},
		{"more than visible", visible * 2},
		{"empty", 0},
		{"single line", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.viewportOffset = 0
			m.viewportLines = make([]string, tt.numLines)
			for i := range m.viewportLines {
				m.viewportLines[i] = fmt.Sprintf("line %d", i)
			}

			output := m.renderPatchView()
			lines := strings.Split(output, "\n")
			// Output = visible content + status line + help bar
			want := visible + 2
			if len(lines) != want {
				t.Errorf("got %d lines, want %d (visible=%d + 2 bottom)",
					len(lines), want, visible)
			}
		})
	}
}
