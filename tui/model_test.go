package tui

import (
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
	// ID(9)+Ver(4)+State(8)+Submitter(20)+Age(5)+AFRT(9)+Checks(8)+Dlg(8) = 71
	// Indicator = 2, so remaining = width - 73
	// Comments visible when remaining - 15 >= 90, i.e. remaining >= 105, i.e. width >= 178
	tests := []struct {
		name             string
		width            int
		wantCVisible     bool
		wantCommVisible  bool
		wantNameWidth    int
		wantCommentWidth int
	}{
		{"wide shows Comments", 178, false, true, 90, 15},
		{"narrow shows C", 177, true, false, 101, 3},
		{"very wide", 250, false, true, 162, 15},
		{"small terminal", 100, true, false, 24, 3},
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
	if !m.filterMode {
		t.Error("filterMode should be true after /")
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
	if m.filterMode {
		t.Error("filterMode should be false after esc")
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

	if m.filterMode {
		t.Error("filterMode should be false")
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

	// Add a cover letter for series 50 with cached content
	d.SaveCover(db.CoverRow{
		ID: 99, SeriesID: 50,
		Name:        "Lorem cover letter",
		Date:        "2026-03-10",
		MboxURL:     "https://pw.example.com/cover/99/mbox/",
		MboxContent: "From cover\nSubject: Lorem cover\n\nCover body",
	})

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
		MboxContent: "From patch\nSubject: Only\n\nBody",
	})

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
