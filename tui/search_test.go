// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"leadlight/db"
	"leadlight/status"
)

// withColor makes lipgloss emit escape sequences for the duration of
// a test. Tests run without a TTY, where every style renders as plain
// text and highlighting cannot be observed at all.
func withColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// typeSearch feeds runes to the search prompt the way bubbletea
// delivers them for ordinary key presses.
func typeSearch(m *Model, s string) {
	for _, r := range s {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			key.Type = tea.KeySpace
		}
		m.handleSearchInputKey(key)
	}
}

func TestSearchInputAcceptsMultiByteRunes(t *testing.T) {
	m := &Model{}
	m.startSearch()

	typeSearch(m, "café ☕")
	if m.searchText != "café ☕" {
		t.Errorf("searchText = %q, want %q", m.searchText, "café ☕")
	}

	// Backspace must remove a whole rune, not a single byte.
	bs := tea.KeyMsg{Type: tea.KeyBackspace}
	m.handleSearchInputKey(bs)
	if m.searchText != "café " {
		t.Errorf("after backspace searchText = %q, want %q", m.searchText, "café ")
	}
	m.handleSearchInputKey(bs)
	m.handleSearchInputKey(bs)
	if m.searchText != "caf" {
		t.Errorf("after backspaces searchText = %q, want %q", m.searchText, "caf")
	}
}

func TestSearchInputPasteDropsControlChars(t *testing.T) {
	m := &Model{}
	m.startSearch()

	paste := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Paste: true,
		Runes: []rune("foo\nbär\tbaz"),
	}
	m.handleSearchInputKey(paste)

	if want := "foobärbaz"; m.searchText != want {
		t.Errorf("searchText = %q, want %q", m.searchText, want)
	}
}

func TestSearchInputIgnoresAltAndControlKeys(t *testing.T) {
	m := &Model{}
	m.startSearch()
	typeSearch(m, "foo")

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true},
		{Type: tea.KeyCtrlA},
		{Type: tea.KeyHome},
	} {
		m.handleSearchInputKey(msg)
	}

	if m.searchText != "foo" {
		t.Errorf("searchText = %q, want %q", m.searchText, "foo")
	}
}

func TestMultipleOccurrencesOnSameLine(t *testing.T) {
	m := &Model{
		viewportDoc: docFromLines(
			"foo bar foo baz foo",
			"no matches here",
			"another foo here",
		),
	}

	// Search for "foo"
	m.searchText = "foo"
	m.updateSearchMatches()

	// Should find 4 total matches: 3 on line 0, 1 on line 2
	if len(m.searchOrder) != 4 {
		t.Errorf("Expected 4 matches, got %d", len(m.searchOrder))
	}

	found := m.viewportMatcher.matches

	// Check that the first 3 matches are on line 0
	for i := 0; i < 3; i++ {
		if found[i].line != 0 {
			t.Errorf("Match %d should be on line 0, got line %d", i, found[i].line)
		}
	}

	// Check that they have different positions
	if found[0].start == found[1].start {
		t.Error("First and second matches should have different positions")
	}
	if found[1].start == found[2].start {
		t.Error("Second and third matches should have different positions")
	}

	// Fourth match should be on line 2
	if found[3].line != 2 {
		t.Errorf("Match 3 should be on line 2, got line %d", found[3].line)
	}

	// Current index should be 0
	if m.searchIdx != 0 {
		t.Errorf("Current index should be 0, got %d", m.searchIdx)
	}

	// Navigate through all matches
	m.nextSearchMatch()
	if m.searchIdx != 1 {
		t.Errorf("After next, index should be 1, got %d", m.searchIdx)
	}

	m.nextSearchMatch()
	if m.searchIdx != 2 {
		t.Errorf("After next, index should be 2, got %d", m.searchIdx)
	}

	m.nextSearchMatch()
	if m.searchIdx != 3 {
		t.Errorf("After next, index should be 3, got %d", m.searchIdx)
	}

	// Should wrap around
	m.nextSearchMatch()
	if m.searchIdx != 0 {
		t.Errorf("After wrapping, index should be 0, got %d", m.searchIdx)
	}
}

func TestSearchHistory(t *testing.T) {
	m := &Model{
		viewportDoc: docFromLines(
			"foo bar baz",
			"hello world",
			"foo again",
		),
	}

	// Commit a few searches
	m.searchText = "foo"
	m.commitSearch()
	m.searchText = "bar"
	m.commitSearch()
	m.searchText = "hello"
	m.commitSearch()

	if len(m.searchHistory) != 3 {
		t.Fatalf("Expected 3 history entries, got %d", len(m.searchHistory))
	}
	if m.searchHistory[0] != "hello" || m.searchHistory[1] != "bar" || m.searchHistory[2] != "foo" {
		t.Errorf("History order wrong: %v", m.searchHistory)
	}

	// Duplicate should move to front, not add again
	m.searchText = "foo"
	m.commitSearch()
	if len(m.searchHistory) != 3 {
		t.Fatalf("Expected 3 history entries after dedup, got %d", len(m.searchHistory))
	}
	if m.searchHistory[0] != "foo" {
		t.Errorf("Expected 'foo' at front, got %q", m.searchHistory[0])
	}

	// Start a new search and browse history with up arrow
	m.startSearch()
	if m.searchHistoryIdx != -1 {
		t.Errorf("Expected historyIdx -1 at start, got %d", m.searchHistoryIdx)
	}

	// Simulate up arrow — should recall most recent ("foo")
	m.searchHistoryIdx = 0
	m.searchText = m.searchHistory[0]
	if m.searchText != "foo" {
		t.Errorf("Expected 'foo' on first up, got %q", m.searchText)
	}

	// Another up — should get "hello"
	m.searchHistoryIdx = 1
	m.searchText = m.searchHistory[1]
	if m.searchText != "hello" {
		t.Errorf("Expected 'hello' on second up, got %q", m.searchText)
	}

	// Down arrow — back to "foo"
	m.searchHistoryIdx = 0
	m.searchText = m.searchHistory[0]
	if m.searchText != "foo" {
		t.Errorf("Expected 'foo' on down, got %q", m.searchText)
	}
}

func TestRecallLastSearch(t *testing.T) {
	m := &Model{
		viewportDoc: docFromLines(
			"foo bar foo",
			"no match",
			"another foo",
		),
	}

	// Search and commit
	m.searchText = "foo"
	m.updateSearchMatches()
	m.commitSearch()

	// Clear search
	m.clearSearch()
	if len(m.searchOrder) != 0 {
		t.Fatal("Expected no matches after clear")
	}

	// Recall via n
	m.recallLastSearch(false)
	if m.searchText != "foo" {
		t.Errorf("Expected recalled text 'foo', got %q", m.searchText)
	}
	if len(m.searchOrder) != 3 {
		t.Errorf("Expected 3 matches after recall, got %d", len(m.searchOrder))
	}
	if m.searchIdx != 0 {
		t.Errorf("n should land on the first match, got %d", m.searchIdx)
	}

	// Recall via N goes to the last match instead.
	m.clearSearch()
	m.recallLastSearch(true)
	if len(m.searchOrder) != 3 {
		t.Fatalf("Expected 3 matches after recall, got %d", len(m.searchOrder))
	}
	if m.searchIdx != 2 {
		t.Errorf("N should land on the last match, got %d", m.searchIdx)
	}
}

func TestSearchNavKeyFallsThroughWhenIdle(t *testing.T) {
	m := &Model{viewportDoc: docFromLines("foo bar")}

	// Nothing searched yet and no history: n/N belong to the view.
	for _, key := range []string{"n", "N"} {
		if m.handleSearchNavKey(key) {
			t.Errorf("%q was consumed with no search and no history", key)
		}
	}

	// "/" always opens the prompt.
	if !m.handleSearchNavKey("/") {
		t.Error(`"/" should always be consumed`)
	}
	m.clearSearch()

	// A query that matched nothing still owns n/N, so that the keys
	// do not suddenly scroll instead.
	m.searchText = "nomatch"
	m.updateSearchMatches()
	if len(m.searchOrder) != 0 {
		t.Fatalf("expected no matches, got %d", len(m.searchOrder))
	}
	for _, key := range []string{"n", "N"} {
		if !m.handleSearchNavKey(key) {
			t.Errorf("%q should be consumed by an active search", key)
		}
	}

	// With history available, n recalls the last query.
	m.clearSearch()
	m.searchHistory = []string{"foo"}
	if !m.handleSearchNavKey("n") {
		t.Error(`"n" should be consumed when history can be recalled`)
	}
	if m.searchText != "foo" {
		t.Errorf("searchText = %q, want %q", m.searchText, "foo")
	}
}

func TestSearchHistoryKeepsTypedDraft(t *testing.T) {
	m := &Model{}
	for _, q := range []string{"older", "newer"} {
		m.startSearch()
		typeSearch(m, q)
		m.commitSearch()
	}

	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	m.startSearch()
	typeSearch(m, "fo")

	m.handleSearchInputKey(up)
	if m.searchText != "newer" {
		t.Fatalf("after up searchText = %q, want %q", m.searchText, "newer")
	}
	m.handleSearchInputKey(up)
	if m.searchText != "older" {
		t.Fatalf("after second up searchText = %q, want %q", m.searchText, "older")
	}

	m.handleSearchInputKey(down)
	if m.searchText != "newer" {
		t.Fatalf("after down searchText = %q, want %q", m.searchText, "newer")
	}
	m.handleSearchInputKey(down)
	if m.searchText != "fo" {
		t.Errorf("returning to the draft gave %q, want %q", m.searchText, "fo")
	}
	if m.searchHistoryIdx != -1 {
		t.Errorf("searchHistoryIdx = %d, want -1", m.searchHistoryIdx)
	}
}

func TestSearchHistoryIsCapped(t *testing.T) {
	m := &Model{}
	for i := 0; i < searchHistoryMax+10; i++ {
		m.startSearch()
		typeSearch(m, fmt.Sprintf("q%d", i))
		m.commitSearch()
	}

	if len(m.searchHistory) != searchHistoryMax {
		t.Errorf("history length = %d, want %d",
			len(m.searchHistory), searchHistoryMax)
	}
	newest := fmt.Sprintf("q%d", searchHistoryMax+9)
	if m.searchHistory[0] != newest {
		t.Errorf("newest entry = %q, want %q", m.searchHistory[0], newest)
	}
	for _, h := range m.searchHistory {
		if h == "q0" {
			t.Error("oldest entry should have been dropped")
		}
	}
}

func TestExtractCaseFlags(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantClean string
		wantCI    bool
	}{
		{"no flags", "foo", "foo", false},
		{"leading \\c", `\cfoo`, "foo", true},
		{"trailing \\C", `foo\C`, "foo", false},
		{"flag in the middle", `fo\co`, "foo", true},
		{"last flag wins, \\c last", `\C\cfoo`, "foo", true},
		{"last flag wins, \\C last", `\c\Cfoo`, "foo", false},
		{"escaped backslash is not a flag", `foo\\cbar`, `foo\\cbar`, false},
		{"flag inside a bracket expression", `[\c]`, `[\c]`, false},
		{"flag outside a bracket expression", `a[\C]b\c`, `a[\C]b`, true},
		{"only a flag", `\c`, "", true},
		{"trailing lone backslash", `foo\`, `foo\`, false},
		{"spaces are preserved", " foo ", " foo ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, ci := extractCaseFlags(tt.pattern)
			if clean != tt.wantClean {
				t.Errorf("extractCaseFlags(%q) clean = %q, want %q",
					tt.pattern, clean, tt.wantClean)
			}
			if ci != tt.wantCI {
				t.Errorf("extractCaseFlags(%q) caseInsensitive = %v, want %v",
					tt.pattern, ci, tt.wantCI)
			}
		})
	}
}

func TestParseVimSearchPattern(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		testString  string
		shouldMatch bool
		wantErr     bool
	}{
		{
			name:        "simple literal",
			pattern:     "foo",
			testString:  "this is foo bar",
			shouldMatch: true,
		},
		{
			name:        "case sensitive by default",
			pattern:     "Foo",
			testString:  "this is foo bar",
			shouldMatch: false,
		},
		{
			name:        "case insensitive with \\c",
			pattern:     `Foo\c`,
			testString:  "this is foo bar",
			shouldMatch: true,
		},
		{
			name:        "case insensitive with \\c at start",
			pattern:     `\cFoo`,
			testString:  "this is FOO bar",
			shouldMatch: true,
		},
		{
			name:        "alternation foo|bar",
			pattern:     "foo|bar",
			testString:  "this is bar",
			shouldMatch: true,
		},
		{
			name:        "alternation foo|bar matches first",
			pattern:     "foo|bar",
			testString:  "this is foo",
			shouldMatch: true,
		},
		{
			name:        "case insensitive alternation",
			pattern:     `\cfoo|bar`,
			testString:  "this is BAR",
			shouldMatch: true,
		},
		{
			name:        "regex character class",
			pattern:     "[0-9]+",
			testString:  "version 123",
			shouldMatch: true,
		},
		{
			name:        "regex with dot",
			pattern:     "f.o",
			testString:  "this is foo bar",
			shouldMatch: true,
		},
		{
			name:        "empty pattern",
			pattern:     "",
			testString:  "anything",
			shouldMatch: false,
		},
		{
			name:        "last case flag wins, \\c after \\C",
			pattern:     `Foo\C\c`,
			testString:  "this is foo bar",
			shouldMatch: true,
		},
		{
			name:        "last case flag wins, \\C after \\c",
			pattern:     `Foo\c\C`,
			testString:  "this is foo bar",
			shouldMatch: false,
		},
		{
			name:        "escaped backslash is not a case flag",
			pattern:     `foo\\cbar`,
			testString:  `foo\cbar`,
			shouldMatch: true,
		},
		{
			name:        "literal space",
			pattern:     " ",
			testString:  "a b",
			shouldMatch: true,
		},
		{
			name:        "trailing space is significant",
			pattern:     "foo ",
			testString:  "a foo bar",
			shouldMatch: true,
		},
		{
			name:        "trailing space does not match end of line",
			pattern:     "foo ",
			testString:  "ends with foo",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := parseVimSearchPattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVimSearchPattern() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if re == nil && tt.pattern != "" {
				t.Errorf("parseVimSearchPattern() returned nil regex for pattern %q", tt.pattern)
				return
			}
			if re != nil {
				matched := re.MatchString(tt.testString)
				if matched != tt.shouldMatch {
					t.Errorf("pattern %q against %q: got match=%v, want match=%v",
						tt.pattern, tt.testString, matched, tt.shouldMatch)
				}
			}
		})
	}
}

// searchDoc lays a body out the way the patch view does, so that
// tests exercise real wrapping rather than one-line-per-row text.
func searchDoc(body string, width int) *renderedDoc {
	d := &renderedDoc{}
	for _, line := range strings.Split(body, "\n") {
		appendBodyLineDoc(d, line, width, diffUnchanged)
	}
	return d
}

func TestSearchFindsTextAcrossAWrap(t *testing.T) {
	withColor(t)

	// A long identifier with no spaces to break on, so the wrap
	// falls in the middle of it. Searching the rendered rows could
	// never match across that break.
	line := "ovs_alpha_bravo_charlie_delta_echo_foxtrot_golf_function"
	const width = 25
	m := &Model{width: width, height: 20}
	m.setViewportDoc(searchDoc(line, width))

	if m.viewportDoc.Len() < 2 {
		t.Fatalf("expected the line to wrap, got %d rows", m.viewportDoc.Len())
	}

	m.searchText = "charlie_delta"
	m.updateSearchMatches()

	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match across the wrap, got %d", len(m.searchOrder))
	}
	match := m.viewportMatcher.matches[0]
	if got := string([]rune(line)[match.start:match.end]); got != m.searchText {
		t.Errorf("match covers %q, want %q", got, m.searchText)
	}

	// The match really has to straddle a row boundary, or this test
	// is not testing anything.
	boundary := m.viewportDoc.visual[1].off
	if match.start >= boundary || match.end <= boundary {
		t.Fatalf("match [%d,%d) does not straddle the row break at %d",
			match.start, match.end, boundary)
	}

	// It must be highlighted on both of the rows it spans.
	mt, cur := m.currentMatch(0)
	var highlighted []int
	for row := 0; row < m.viewportDoc.Len(); row++ {
		if renderDocRow(m.viewportDoc, row, mt, cur) != m.viewportDoc.Line(row) {
			highlighted = append(highlighted, row)
		}
	}
	if len(highlighted) != 2 {
		t.Errorf("match highlighted on rows %v, want exactly 2", highlighted)
	}
}

func TestSearchSurvivesResize(t *testing.T) {
	body := strings.Repeat(
		"lorem ipsum needle dolor sit amet consectetur adipiscing\n", 8)

	m := &Model{width: 80, height: 20}
	m.setViewportDoc(searchDoc(body, 80))
	m.searchText = "needle"
	m.updateSearchMatches()

	before := len(m.searchOrder)
	if before != 8 {
		t.Fatalf("expected 8 matches, got %d", before)
	}
	m.searchIdx = 3
	wantLine := m.viewportMatcher.matches[m.searchOrder[3].idx].line

	// Re-lay the same text out at a much narrower width.
	m.width = 30
	m.setViewportDoc(searchDoc(body, 30))

	if got := len(m.searchOrder); got != before {
		t.Errorf("match count changed across resize: %d -> %d", before, got)
	}
	if m.searchIdx != 3 {
		t.Errorf("cursor moved across resize: %d, want 3", m.searchIdx)
	}
	gotLine := m.viewportMatcher.matches[m.searchOrder[3].idx].line
	if gotLine != wantLine {
		t.Errorf("cursor is on source line %d, want %d", gotLine, wantLine)
	}
	checkSearchOrder(t, m)
}

func TestSearchRescansWhenContentChanges(t *testing.T) {
	m := &Model{width: 60, height: 20}
	m.setViewportDoc(searchDoc("needle one\nneedle two\nneedle three", 60))
	m.searchText = "needle"
	m.updateSearchMatches()
	if len(m.searchOrder) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m.searchOrder))
	}

	// Swapping in shorter content must not leave matches behind that
	// point past the end of it.
	m.setViewportDoc(searchDoc("needle only", 60))
	if len(m.searchOrder) != 1 {
		t.Errorf("expected 1 match after the content changed, got %d",
			len(m.searchOrder))
	}
	for _, ref := range m.searchOrder {
		if ref.row >= m.viewportDoc.Len() {
			t.Errorf("stale row %d past the end of %d rows",
				ref.row, m.viewportDoc.Len())
		}
	}
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		t.Errorf("searchIdx = %d is out of range", m.searchIdx)
	}

	// And content with no match at all must clear the cursor.
	m.setViewportDoc(searchDoc("nothing here", 60))
	if len(m.searchOrder) != 0 {
		t.Errorf("expected no matches, got %d", len(m.searchOrder))
	}
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d, want -1", m.searchIdx)
	}
}

func TestSearchHighlightPreservesText(t *testing.T) {
	withColor(t)
	line := "lorem needle ipsum needle dolor sit amet consectetur needle"
	m := &Model{width: 28, height: 20}
	m.setViewportDoc(searchDoc(line, 28))
	m.searchText = "needle"
	m.updateSearchMatches()

	mt, cur := m.currentMatch(0)
	for row := 0; row < m.viewportDoc.Len(); row++ {
		got := ansi.Strip(renderDocRow(m.viewportDoc, row, mt, cur))
		want := ansi.Strip(m.viewportDoc.Line(row))
		if got != want {
			t.Errorf("row %d: highlighting changed the text\n got %q\nwant %q",
				row, got, want)
		}
	}
}

func TestSearchOnlyOneMatchIsCurrent(t *testing.T) {
	withColor(t)
	m := &Model{width: 60, height: 20}
	m.setViewportDoc(searchDoc("needle one\nneedle two\nneedle three", 60))
	m.searchText = "needle"
	m.updateSearchMatches()

	if len(m.searchOrder) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m.searchOrder))
	}
	for idx := 0; idx < 3; idx++ {
		m.searchIdx = idx
		mt, cur := m.currentMatch(0)
		if cur == nil {
			t.Fatalf("idx %d: no current match", idx)
		}
		current := 0
		for row := 0; row < m.viewportDoc.Len(); row++ {
			if strings.Contains(
				renderDocRow(m.viewportDoc, row, mt, cur),
				searchCurrentStyle.Render("needle"),
			) {
				current++
			}
		}
		if current != 1 {
			t.Errorf("idx %d: %d rows show the current style, want 1",
				idx, current)
		}
	}
}

func TestCompareSearchKeepsTheTwoSidesApart(t *testing.T) {
	skipIfNoDiff(t)
	withColor(t)

	// "shared" is on both sides, "onlyleft" on one. The two columns
	// are indexed separately, so an identical row cannot produce one
	// match that appears to belong to both.
	left := ParsedMbox{
		Subject: "Lorem", From: "a@b.example",
		Body: "shared line\nonlyleft line\nshared tail\n",
	}
	right := ParsedMbox{
		Subject: "Lorem", From: "a@b.example",
		Body: "shared line\nshared tail\n",
	}
	m := buildTestCompare(t, left, right, 80)
	m.viewMode = viewCompare

	m.searchText = "onlyleft"
	m.updateSearchMatches()
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}
	if m.searchOrder[0].target != 0 {
		t.Errorf("match attributed to column %d, want 0",
			m.searchOrder[0].target)
	}
	// The right column must not light up for it.
	if mt, cur := m.currentMatch(1); cur != nil || len(mt.matches) != 0 {
		t.Error("right column should have no matches for a left-only term")
	}

	// A term on both sides gives one match per column, not one match
	// counted twice.
	m.searchText = "shared"
	m.updateSearchMatches()
	leftN := len(m.compare[0].matcher.matches)
	rightN := len(m.compare[1].matcher.matches)
	if leftN == 0 || rightN == 0 {
		t.Fatalf("expected matches on both sides, got %d and %d",
			leftN, rightN)
	}
	if len(m.searchOrder) != leftN+rightN {
		t.Errorf("searchOrder has %d refs, want %d+%d",
			len(m.searchOrder), leftN, rightN)
	}
	// Exactly one match is current at a time, and it belongs to one
	// column only.
	for idx := range m.searchOrder {
		m.searchIdx = idx
		_, lc := m.currentMatch(0)
		_, rc := m.currentMatch(1)
		if (lc != nil) == (rc != nil) {
			t.Errorf("idx %d: current match is in %v/%v columns",
				idx, lc != nil, rc != nil)
		}
	}

	// n walks down the screen, left column before right on a row.
	prev := -1
	for idx, ref := range m.searchOrder {
		if ref.row < prev {
			t.Errorf("match %d is on row %d, after row %d", idx, ref.row, prev)
		}
		prev = ref.row
	}
}

func TestCompareSearchMatchesAtEndOfLine(t *testing.T) {
	skipIfNoDiff(t)
	withColor(t)

	// The compare view pads each column out to its width after the
	// row is rendered. A match at the very end of a line must not be
	// disturbed by that padding.
	left := ParsedMbox{
		Subject: "Lorem", From: "a@b.example",
		Body: "ends with needle\n",
	}
	right := ParsedMbox{
		Subject: "Lorem", From: "a@b.example",
		Body: "different text\n",
	}
	m := buildTestCompare(t, left, right, 80)
	m.viewMode = viewCompare

	m.searchText = "needle$"
	m.updateSearchMatches()
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}

	row := m.searchOrder[0].row
	mt, cur := m.currentMatch(0)
	rendered := renderDocRow(m.compare[0].doc, row, mt, cur)
	if !strings.Contains(rendered, searchCurrentStyle.Render("needle")) {
		t.Errorf("row %d did not highlight the trailing match: %q",
			row, rendered)
	}
	got, want := ansi.Strip(rendered), ansi.Strip(m.compare[0].doc.Line(row))
	if got != want {
		t.Errorf("highlighting changed the text\n got %q\nwant %q", got, want)
	}
}

// searchHistoryDB opens an empty in-memory database for history tests.
func searchHistoryDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSearchHistoryPersistsAcrossSessions(t *testing.T) {
	d := searchHistoryDB(t)

	// First session: run a few searches.
	first := &Model{db: d}
	first.loadSearchHistory()
	if len(first.searchHistory) != 0 {
		t.Fatalf("a fresh database should have no history, got %v",
			first.searchHistory)
	}
	for _, q := range []string{"alpha", "beta", "gamma"} {
		first.startSearch()
		typeSearch(first, q)
		first.commitSearch()
	}

	// Second session over the same database.
	second := &Model{db: d}
	second.loadSearchHistory()

	want := []string{"gamma", "beta", "alpha"}
	if !reflect.DeepEqual(second.searchHistory, want) {
		t.Errorf("restored history = %v, want %v", second.searchHistory, want)
	}

	// And it is usable, not just present.
	second.viewportDoc = docFromLines("alpha beta gamma")
	if !second.handleSearchNavKey("n") {
		t.Fatal("n should recall the restored history")
	}
	if second.searchText != "gamma" {
		t.Errorf("recalled %q, want %q", second.searchText, "gamma")
	}
}

func TestSearchHistoryPersistsDeduplicatedAndCapped(t *testing.T) {
	d := searchHistoryDB(t)

	m := &Model{db: d}
	m.loadSearchHistory()
	for i := 0; i < searchHistoryMax+20; i++ {
		m.startSearch()
		typeSearch(m, fmt.Sprintf("q%d", i))
		m.commitSearch()
	}
	// Re-running an old query moves it to the front rather than
	// adding a duplicate.
	m.startSearch()
	typeSearch(m, fmt.Sprintf("q%d", searchHistoryMax+5))
	m.commitSearch()

	restored := &Model{db: d}
	restored.loadSearchHistory()

	if len(restored.searchHistory) != searchHistoryMax {
		t.Errorf("restored %d entries, want %d",
			len(restored.searchHistory), searchHistoryMax)
	}
	if got := restored.searchHistory[0]; got !=
		fmt.Sprintf("q%d", searchHistoryMax+5) {
		t.Errorf("newest entry = %q, want the re-run query", got)
	}
	seen := map[string]bool{}
	for _, q := range restored.searchHistory {
		if seen[q] {
			t.Errorf("duplicate entry %q in restored history", q)
		}
		seen[q] = true
	}
	if seen["q0"] {
		t.Error("oldest entry should have been dropped by the cap")
	}
}

func TestSearchHistoryHandlesUnusableStoredValue(t *testing.T) {
	d := searchHistoryDB(t)
	d.SetSyncState(searchHistoryKey, "{not json")

	m := &Model{db: d}
	m.loadSearchHistory()
	if len(m.searchHistory) != 0 {
		t.Errorf("history = %v, want empty after an unreadable value",
			m.searchHistory)
	}

	// A new search still saves, overwriting the bad value.
	m.startSearch()
	typeSearch(m, "alpha")
	m.commitSearch()

	restored := &Model{db: d}
	restored.loadSearchHistory()
	if !reflect.DeepEqual(restored.searchHistory, []string{"alpha"}) {
		t.Errorf("restored = %v, want [alpha]", restored.searchHistory)
	}
}

func TestSearchHistoryWithoutDatabase(t *testing.T) {
	// The compare and patch views are usable without a database in
	// tests and in NewModelWithData; history just stays in memory.
	m := &Model{}
	m.loadSearchHistory()
	m.startSearch()
	typeSearch(m, "alpha")
	m.commitSearch()

	if !reflect.DeepEqual(m.searchHistory, []string{"alpha"}) {
		t.Errorf("history = %v, want [alpha]", m.searchHistory)
	}
}

func TestSearchHistoryRoundTripsAwkwardQueries(t *testing.T) {
	d := searchHistoryDB(t)

	queries := []string{
		`needle`,
		`"quoted"`,
		`back\slash`,
		`a|b.*[0-9]+`,
		`ünïcödé ☕`,
		`  leading and trailing  `,
		`tab	separated`,
	}
	m := &Model{db: d}
	m.loadSearchHistory()
	for _, q := range queries {
		m.searchText = q
		m.commitSearch()
	}

	restored := &Model{db: d}
	restored.loadSearchHistory()

	want := make([]string, len(queries))
	for i, q := range queries {
		want[len(queries)-1-i] = q
	}
	if !reflect.DeepEqual(restored.searchHistory, want) {
		t.Errorf("restored = %q\n    want = %q", restored.searchHistory, want)
	}
}

func TestSearchHistorySurvivesDatabaseReopen(t *testing.T) {
	// The in-memory tests share one connection. This is the real
	// case: quit leadlight and start it again in the same directory.
	path := filepath.Join(t.TempDir(), "leadlight.db")

	first, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &Model{db: first}
	m.loadSearchHistory()
	for _, q := range []string{"alpha", "beta"} {
		m.startSearch()
		typeSearch(m, q)
		m.commitSearch()
	}
	first.Close()

	second, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	restored := &Model{db: second}
	restored.loadSearchHistory()
	if want := []string{"beta", "alpha"}; !reflect.DeepEqual(
		restored.searchHistory, want) {
		t.Errorf("after reopen history = %v, want %v",
			restored.searchHistory, want)
	}
}

func TestSearchNavigationWrapsBothWays(t *testing.T) {
	m := &Model{width: 60, height: 20}
	m.setViewportDoc(searchDoc("needle one\nneedle two\nneedle three", 60))
	m.searchText = "needle"
	m.updateSearchMatches()

	if len(m.searchOrder) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m.searchOrder))
	}
	if m.searchIdx != 0 {
		t.Fatalf("a fresh search should start at 0, got %d", m.searchIdx)
	}

	// n walks forward and wraps past the end.
	for _, want := range []int{1, 2, 0, 1} {
		m.nextSearchMatch()
		if m.searchIdx != want {
			t.Fatalf("after n: searchIdx = %d, want %d", m.searchIdx, want)
		}
	}
	// N walks back and wraps past the start.
	for _, want := range []int{0, 2, 1, 0} {
		m.prevSearchMatch()
		if m.searchIdx != want {
			t.Fatalf("after N: searchIdx = %d, want %d", m.searchIdx, want)
		}
	}

	// Each step moves the match to a different row.
	rows := map[int]bool{}
	for idx := range m.searchOrder {
		rows[m.searchOrder[idx].row] = true
	}
	if len(rows) != 3 {
		t.Errorf("matches land on %d distinct rows, want 3", len(rows))
	}

	// Neither key does anything, or panics, with no matches at all.
	m.clearSearch()
	m.nextSearchMatch()
	m.prevSearchMatch()
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d with no matches, want -1", m.searchIdx)
	}
}

func TestSearchNavigationScrollsToTheMatch(t *testing.T) {
	// A match far down a long document has to bring the viewport
	// with it rather than just moving an index.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		if i == 150 {
			b.WriteString("the needle is here\n")
			continue
		}
		b.WriteString(fmt.Sprintf("filler line %d\n", i))
	}
	m := &Model{width: 60, height: 24}
	m.setViewportDoc(searchDoc(b.String(), 60))
	m.searchText = "needle"
	m.updateSearchMatches()

	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}
	row := m.searchOrder[0].row
	visible := m.viewportVisibleLines()
	if m.viewportOffset > row || row >= m.viewportOffset+visible {
		t.Errorf("match on row %d is outside the visible window [%d,%d)",
			row, m.viewportOffset, m.viewportOffset+visible)
	}
}

func TestSearchHelpShowsMatchCounter(t *testing.T) {
	bright, desc, sep := (&Model{}).helpStyles()

	m := &Model{width: 80, height: 20}
	m.setViewportDoc(searchDoc("needle one\nneedle two", 80))

	// While typing, with matches.
	m.startSearch()
	typeSearch(m, "needle")
	got := m.renderSearchInputHelp(bright, desc, sep)
	for _, want := range []string{"Search: ", "needle", "[1/2]", "enter", "esc"} {
		if !strings.Contains(got, want) {
			t.Errorf("input help %q missing %q", got, want)
		}
	}

	// Stepping forward updates the counter.
	m.nextSearchMatch()
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[2/2]") {
		t.Errorf("input help after n = %q, want [2/2]", got)
	}

	// A query that matches nothing says so.
	m.searchText = "nomatch"
	m.updateSearchMatches()
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[no matches]") {
		t.Errorf("input help = %q, want [no matches]", got)
	}

	// Once committed, the key help carries the count.
	m.searchText = "needle"
	m.updateSearchMatches()
	m.commitSearch()
	got = m.renderSearchKeyHelp(bright, desc, sep)
	for _, want := range []string{"/", "n/N", "[2]"} {
		if !strings.Contains(got, want) {
			t.Errorf("key help %q missing %q", got, want)
		}
	}

	// With no search active it offers only "/".
	m.clearSearch()
	got = m.renderSearchKeyHelp(bright, desc, sep)
	if strings.Contains(got, "n/N") {
		t.Errorf("key help %q should not offer n/N with no search", got)
	}
}

func TestParseVimSearchPatternRejectsInvalidRegex(t *testing.T) {
	// The query is a regular expression. A pattern that does not
	// compile is an error rather than something quietly searched for
	// as literal text, which would answer a different question than
	// the one that was asked.
	for _, pattern := range []string{
		"foo(", "a[b", "*", `\cFOO(`, "x{2,1}", `a\`,
	} {
		re, err := parseVimSearchPattern(pattern)
		if err == nil {
			t.Errorf("pattern %q compiled to %v, want an error", pattern, re)
		}
		if re != nil {
			t.Errorf("pattern %q returned a regex alongside its error", pattern)
		}
	}

	// A glob is not regex syntax, so it does not find files either.
	// "/*.c" is a common reflex; it has to fail rather than appear
	// to work by matching the literal characters.
	if _, err := parseVimSearchPattern("*.c"); err == nil {
		t.Error(`"*.c" should be rejected, not treated as literal text`)
	}
	// What does work for the same job.
	for _, pattern := range []string{`\.c`, `[a-z-]+\.c`, `\.c$`} {
		re, err := parseVimSearchPattern(pattern)
		if err != nil {
			t.Errorf("pattern %q: unexpected error %v", pattern, err)
			continue
		}
		if !re.MatchString("diff --git a/lib/flow.c b/lib/flow.c") {
			t.Errorf("pattern %q did not match a .c diff header", pattern)
		}
	}
}

// patchViewWithSearch opens a patch view over the given lines and runs
// a committed search, the way a user would.
func patchViewWithSearch(query string, lines ...string) *Model {
	m := &Model{width: 80, height: 24, viewMode: viewPatch,
		viewingPatchID: 100, viewCommentIdx: -1}
	m.setViewportDoc(docFromLines(lines...))
	m.startSearch()
	typeSearch(m, query)
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	return m
}

// checkSearchOrder asserts that every match ref still points at the
// row that actually shows it. A stale ref can easily stay within
// range of the document, so comparing against rowOf is what catches
// matches left over from older content.
func checkSearchOrder(t *testing.T, m *Model) {
	t.Helper()
	targets := m.searchTargets()
	for i, ref := range m.searchOrder {
		if ref.target < 0 || ref.target >= len(targets) {
			t.Errorf("match %d names column %d of %d",
				i, ref.target, len(targets))
			continue
		}
		doc, mt := targets[ref.target].doc, targets[ref.target].mt
		if ref.idx < 0 || ref.idx >= len(mt.matches) {
			t.Errorf("match %d indexes %d of %d",
				i, ref.idx, len(mt.matches))
			continue
		}
		match := mt.matches[ref.idx]
		if want := doc.rowOf(match.line, match.start); ref.row != want {
			t.Errorf("match %d is recorded on row %d, but source line %d "+
				"offset %d is shown on row %d",
				i, ref.row, match.line, match.start, want)
		}
		if ref.row < 0 || ref.row >= doc.Len() {
			t.Errorf("match %d points at row %d of %d",
				i, ref.row, doc.Len())
		}
	}
}

func TestPatchViewQClearsSearchThenExits(t *testing.T) {
	m := patchViewWithSearch("needle", "a needle here", "nothing")
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected the search to match, got %d", len(m.searchOrder))
	}

	// First q drops the search but stays in the patch view.
	m.handleViewportKey(keyRunes("q"))
	if m.viewMode != viewPatch {
		t.Errorf("first q left the patch view, mode = %d", m.viewMode)
	}
	if m.searchText != "" || len(m.searchOrder) != 0 {
		t.Errorf("first q did not clear the search: text=%q matches=%d",
			m.searchText, len(m.searchOrder))
	}

	// Second q leaves.
	m.handleViewportKey(keyRunes("q"))
	if m.viewMode != viewTable {
		t.Errorf("second q did not leave the patch view, mode = %d", m.viewMode)
	}
	if m.viewportDoc != nil || m.viewingPatchID != 0 {
		t.Error("leaving the view should drop the content it was showing")
	}
}

func TestPatchViewQExitsAtOnceWhenNothingMatched(t *testing.T) {
	// A search that found nothing has nothing to clear, so q should
	// behave as if no search were running.
	m := patchViewWithSearch("nomatch", "a needle here", "nothing")
	if len(m.searchOrder) != 0 {
		t.Fatalf("expected no matches, got %d", len(m.searchOrder))
	}

	m.handleViewportKey(keyRunes("q"))
	if m.viewMode != viewTable {
		t.Errorf("q should have left the view at once, mode = %d", m.viewMode)
	}
}

func TestPatchViewEscExitsEvenDuringSearch(t *testing.T) {
	m := patchViewWithSearch("needle", "a needle here", "nothing")
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected the search to match, got %d", len(m.searchOrder))
	}

	m.handleViewportKey(keySpecial("esc"))
	if m.viewMode != viewTable {
		t.Errorf("esc should leave the view outright, mode = %d", m.viewMode)
	}
	if m.searchText != "" {
		t.Errorf("esc should clear the search, text = %q", m.searchText)
	}
}

func TestSearchPromptEscKeepsThePatchView(t *testing.T) {
	// esc at the "/" prompt cancels the query being typed; it must
	// not also close the patch view.
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(docFromLines("a needle here"))
	m.startSearch()
	typeSearch(m, "need")

	m.handleViewportKey(keySpecial("esc"))
	if m.viewMode != viewPatch {
		t.Errorf("esc at the prompt left the view, mode = %d", m.viewMode)
	}
	if m.searchInput || m.searchText != "" {
		t.Errorf("prompt should be cancelled: input=%v text=%q",
			m.searchInput, m.searchText)
	}
}

func TestSearchPromptBackspacePastStartCancels(t *testing.T) {
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(docFromLines("a needle here"))
	m.startSearch()
	typeSearch(m, "ab")

	bs := tea.KeyMsg{Type: tea.KeyBackspace}
	for i := 0; i < 3; i++ {
		m.handleViewportKey(bs)
	}
	if m.searchInput {
		t.Error("backspacing past the start should leave the prompt")
	}
	if m.viewMode != viewPatch {
		t.Errorf("it should not leave the patch view, mode = %d", m.viewMode)
	}
}

func TestCompareViewQClearsSearchThenExits(t *testing.T) {
	skipIfNoDiff(t)
	left := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "a needle here\n"}
	right := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "something else\n"}
	m := buildTestCompare(t, left, right, 80)
	m.viewMode = viewCompare
	m.compareCount = 2

	m.startSearch()
	typeSearch(m, "needle")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected the search to match, got %d", len(m.searchOrder))
	}

	m.handleCompareKey(keyRunes("q"))
	if m.viewMode != viewCompare {
		t.Errorf("first q left the compare view, mode = %d", m.viewMode)
	}
	m.handleCompareKey(keyRunes("q"))
	if m.viewMode != viewTable {
		t.Errorf("second q did not leave, mode = %d", m.viewMode)
	}
	if m.compareCount != 0 {
		t.Errorf("leaving compare should unmark both sides, count = %d",
			m.compareCount)
	}
}

func TestSearchRescansOnWindowResize(t *testing.T) {
	// The real resize path, not just a second setViewportDoc: the
	// window message re-renders the content at the new width and the
	// matches have to follow it.
	m, d := testModelWithDB(t)
	d.UpdatePatchDetail(100,
		strings.Repeat("lorem ipsum needle dolor sit amet consectetur\n", 6),
		"", "", "")

	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.width = 100
	m.refreshViewport()

	m.searchText = "needle"
	m.updateSearchMatches()
	before := len(m.searchOrder)
	if before == 0 {
		t.Fatal("expected matches before the resize")
	}

	result, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = result.(*Model)

	if got := len(m.searchOrder); got != before {
		t.Errorf("match count changed across resize: %d -> %d", before, got)
	}
	checkSearchOrder(t, m)
}

func TestSearchRescansOnCommentSwitch(t *testing.T) {
	m, _ := testModelWithDB(t)
	m.width = 80
	m.height = 24
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.viewComments = []CommentInfo{
		{ID: 1, Submitter: "Lorem", Date: "2026-01-01",
			Subject: "Re: test", Content: "needle needle needle"},
		{ID: 2, Submitter: "Dolor", Date: "2026-01-02",
			Subject: "Re: test", Content: "nothing of interest"},
	}
	m.viewCommentIdx = 0
	m.switchToComment()

	m.searchText = "needle"
	m.updateSearchMatches()
	if len(m.searchOrder) != 3 {
		t.Fatalf("expected 3 matches in the first comment, got %d",
			len(m.searchOrder))
	}

	// Moving to a comment with no match must leave nothing behind.
	m.nextComment()
	if len(m.searchOrder) != 0 {
		t.Errorf("expected no matches after the switch, got %d",
			len(m.searchOrder))
	}
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d, want -1", m.searchIdx)
	}

	// And moving back finds them again.
	m.prevComment()
	if len(m.searchOrder) != 3 {
		t.Errorf("expected 3 matches again, got %d", len(m.searchOrder))
	}
	checkSearchOrder(t, m)
}

func TestSearchRescansOnExpandToggle(t *testing.T) {
	m, _ := testModelWithDB(t)
	m.width = 80
	m.height = 24
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewComments = []CommentInfo{{
		ID: 1, Submitter: "Lorem", Date: "2026-01-01", Subject: "Re: test",
		Content: "reply\n" +
			strings.Repeat("> needle in a quoted line\n", 20),
	}}
	m.viewCommentIdx = 0
	m.viewExpanded = false
	m.switchToComment()

	m.searchText = "needle"
	m.updateSearchMatches()
	collapsed := len(m.searchOrder)

	// "e" expands the collapsed quote block, which brings more of
	// the text into view.
	m.handleViewportKey(keyRunes("e"))
	expanded := len(m.searchOrder)

	if expanded <= collapsed {
		t.Errorf("expanding should reveal more matches: %d -> %d",
			collapsed, expanded)
	}
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		t.Errorf("searchIdx = %d is out of range after expanding", m.searchIdx)
	}
	checkSearchOrder(t, m)
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keySpecial(name string) tea.KeyMsg {
	switch name {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	panic("unknown key " + name)
}

func TestCompareSearchRescansOnResize(t *testing.T) {
	skipIfNoDiff(t)

	// Resizing the compare view rebuilds both columns from scratch.
	// A search running at the time has to be re-run against them.
	body := strings.Repeat(
		"lorem ipsum needle dolor sit amet consectetur adipiscing\n", 5)
	left := ParsedMbox{Subject: "Lorem", From: "a@b.example", Body: body}
	right := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: body + "an extra needle line\n"}

	m := buildTestCompare(t, left, right, 120)
	m.viewMode = viewCompare
	m.Status = status.NewRegistry(nil)

	m.searchText = "needle"
	m.updateSearchMatches()
	before := len(m.searchOrder)
	if before == 0 {
		t.Fatal("expected matches before the resize")
	}
	checkSearchOrder(t, m)

	result, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	m = result.(*Model)

	if got := len(m.searchOrder); got != before {
		t.Errorf("match count changed across resize: %d -> %d", before, got)
	}
	checkSearchOrder(t, m)
}

func TestCompareSearchRescansOnPatchCycle(t *testing.T) {
	skipIfNoDiff(t)

	// Cycling one column to a different patch also rebuilds the
	// documents underneath an active search.
	m := &Model{width: 100, height: 24, viewMode: viewCompare}
	m.Status = status.NewRegistry(nil)
	for i := range m.compare {
		m.compare[i] = compareSide{
			idx: 0,
			patches: []comparePatch{
				{id: 1, position: 1, parsed: ParsedMbox{
					Subject: "Lorem", From: "a@b.example",
					Body: "a needle here\n"}},
				{id: 2, position: 2, parsed: ParsedMbox{
					Subject: "Dolor", From: "a@b.example",
					Body: "no match at all\n"}},
			},
		}
	}
	m.buildCompareContent()

	m.searchText = "needle"
	m.updateSearchMatches()
	if len(m.searchOrder) != 2 {
		t.Fatalf("expected a match in each column, got %d", len(m.searchOrder))
	}
	checkSearchOrder(t, m)

	// Move the left column to the patch with no match.
	m.compare[0].idx = 1
	m.buildCompareContent()

	if len(m.searchOrder) != 1 {
		t.Errorf("expected 1 match after the cycle, got %d", len(m.searchOrder))
	}
	for _, ref := range m.searchOrder {
		if ref.target != 1 {
			t.Errorf("remaining match is in column %d, want 1", ref.target)
		}
	}
	checkSearchOrder(t, m)
}

func TestSearchScrollClampsAtTheEnd(t *testing.T) {
	// A match near the bottom cannot be centred without scrolling
	// past the end of the document.
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString(fmt.Sprintf("filler line %d\n", i))
	}
	b.WriteString("the needle is on the last line\n")

	m := &Model{width: 60, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc(b.String(), 60))
	m.searchText = "needle"
	m.updateSearchMatches()

	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}
	visible := m.viewportVisibleLines()
	maxOffset := m.viewportDoc.Len() - visible
	if m.viewportOffset > maxOffset {
		t.Errorf("viewportOffset = %d, past the last page at %d",
			m.viewportOffset, maxOffset)
	}
	// The match still has to be on screen.
	row := m.searchOrder[0].row
	if m.viewportOffset > row || row >= m.viewportOffset+visible {
		t.Errorf("match on row %d is outside the window [%d,%d)",
			row, m.viewportOffset, m.viewportOffset+visible)
	}

	// A document shorter than the window never scrolls at all.
	m.setViewportDoc(searchDoc("a needle here\n", 60))
	m.updateSearchMatches()
	if m.viewportOffset != 0 {
		t.Errorf("viewportOffset = %d for a short document, want 0",
			m.viewportOffset)
	}
}

func TestSearchPatternOfOnlyACaseFlag(t *testing.T) {
	// "/\c" leaves nothing to match once the flag is stripped. It
	// should behave as an empty query rather than matching every
	// position in the document.
	m := &Model{width: 60, height: 20, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha\nbeta\n", 60))

	for _, pattern := range []string{`\c`, `\C`, `\c\C`} {
		re, err := parseVimSearchPattern(pattern)
		if err != nil {
			t.Errorf("pattern %q: unexpected error %v", pattern, err)
		}
		if re != nil {
			t.Errorf("pattern %q compiled to %v, want no regex", pattern, re)
		}

		m.searchText = pattern
		m.updateSearchMatches()
		if len(m.searchOrder) != 0 {
			t.Errorf("pattern %q produced %d matches, want none",
				pattern, len(m.searchOrder))
		}
		if m.searchIdx != -1 {
			t.Errorf("pattern %q: searchIdx = %d, want -1",
				pattern, m.searchIdx)
		}
	}
}

func TestSearchWithNoMatchesIsDroppedOnCommit(t *testing.T) {
	bright, desc, sep := (&Model{}).helpStyles()

	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha\nbeta\n", 80))

	m.startSearch()
	typeSearch(m, "nomatch")

	// While still typing, the prompt reports the failure.
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[no matches]") {
		t.Errorf("prompt = %q, want it to report no matches", got)
	}

	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})

	// Committing it leaves the search entirely.
	if m.searchInput {
		t.Error("prompt should be closed")
	}
	if m.searchText != "" {
		t.Errorf("searchText = %q, want it cleared", m.searchText)
	}
	if m.searchRegex != nil {
		t.Error("searchRegex should be cleared")
	}
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d, want -1", m.searchIdx)
	}

	// So the help bar offers only "/", with no n/N and no counter.
	got := m.renderSearchKeyHelp(bright, desc, sep)
	if strings.Contains(got, "n/N") {
		t.Errorf("help = %q, should not offer n/N with nothing to step to", got)
	}
	if strings.Contains(got, "[0]") {
		t.Errorf("help = %q, should not show a zero counter", got)
	}
	if !strings.Contains(got, "/") {
		t.Errorf("help = %q, should still offer /", got)
	}

	// And q leaves the view at once, with no search to clear first.
	m.handleViewportKey(keyRunes("q"))
	if m.viewMode != viewTable {
		t.Errorf("q did not leave the view, mode = %d", m.viewMode)
	}
}

func TestSearchWithNoMatchesIsStillRemembered(t *testing.T) {
	// Dropping the search must not lose the query: a typo is exactly
	// what you want to recall and correct.
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha\nbeta\n", 80))

	m.startSearch()
	typeSearch(m, "nomatch")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})

	if want := []string{"nomatch"}; !reflect.DeepEqual(m.searchHistory, want) {
		t.Errorf("history = %v, want %v", m.searchHistory, want)
	}

	// Arrowing up at the prompt brings it back for editing.
	m.startSearch()
	m.handleSearchInputKey(keySpecial("up"))
	if m.searchText != "nomatch" {
		t.Errorf("recalled %q, want %q", m.searchText, "nomatch")
	}
}

func TestSearchRecallWithNoMatchesIsDropped(t *testing.T) {
	// n recalls the last query; if it no longer matches anything in
	// this content, it should not leave a dead search behind either.
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha needle\n", 80))
	m.startSearch()
	typeSearch(m, "needle")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected the search to match, got %d", len(m.searchOrder))
	}

	// Move to content the query does not match, then clear and recall.
	m.setViewportDoc(searchDoc("nothing of interest\n", 80))
	m.clearSearch()
	m.handleSearchNavKey("n")

	if m.searchText != "" {
		t.Errorf("searchText = %q, want a failed recall to leave nothing",
			m.searchText)
	}
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d, want -1", m.searchIdx)
	}
}

func TestSearchHelpDropsNavWhenContentLosesMatches(t *testing.T) {
	// A search can also end up with no matches because the content
	// changed under it. It stays active, so that going back restores
	// the highlights, but n/N must not be advertised meanwhile.
	bright, desc, sep := (&Model{}).helpStyles()

	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha needle\n", 80))
	m.searchText = "needle"
	m.updateSearchMatches()
	if !strings.Contains(m.renderSearchKeyHelp(bright, desc, sep), "n/N") {
		t.Fatal("expected n/N to be offered while matches exist")
	}

	m.setViewportDoc(searchDoc("nothing of interest\n", 80))
	got := m.renderSearchKeyHelp(bright, desc, sep)
	if strings.Contains(got, "n/N") {
		t.Errorf("help = %q, should not offer n/N with no matches", got)
	}
	if m.searchText != "needle" {
		t.Errorf("searchText = %q, the search itself should stay active",
			m.searchText)
	}

	// Going back to matching content brings the offer back.
	m.setViewportDoc(searchDoc("alpha needle\n", 80))
	if !strings.Contains(m.renderSearchKeyHelp(bright, desc, sep), "n/N") {
		t.Error("n/N should be offered again once matches return")
	}
}

func TestSearchSkipsCollapsedHeaderText(t *testing.T) {
	// A long recipient list is folded to a few rows with a "N total"
	// marker. The addresses that were folded away are not on screen,
	// so they must not be findable: a match there could be stepped
	// to but never seen.
	var addrs []string
	for i := 0; i < 20; i++ {
		addrs = append(addrs, fmt.Sprintf("person%02d@ipsum.example", i))
	}
	addrs = append(addrs, "hidden-needle@ipsum.example")
	p := ParsedMbox{
		Subject: "Lorem ipsum",
		From:    "a@b.example",
		Cc:      strings.Join(addrs, ", "),
	}

	headers, body, diff := formatMboxDoc(p, 80, true)
	doc := &renderedDoc{}
	doc.appendDoc(headers)
	doc.appendDoc(body)
	doc.appendDoc(diff)

	if strings.Contains(doc.String(), "hidden-needle") {
		t.Fatal("fixture is wrong: the address was not folded away")
	}
	if !strings.Contains(doc.String(), "total (e to expand)") {
		t.Fatal("fixture is wrong: the header did not collapse")
	}

	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(doc)
	m.searchText = "hidden-needle"
	m.updateSearchMatches()

	if len(m.searchOrder) != 0 {
		t.Errorf("found %d matches in folded-away text, want none",
			len(m.searchOrder))
	}

	// An address that is still on screen is found as normal, and the
	// marker itself stays searchable.
	for _, q := range []string{"person00", "to expand"} {
		m.searchText = q
		m.updateSearchMatches()
		if len(m.searchOrder) == 0 {
			t.Errorf("query %q should still match", q)
			continue
		}
		checkSearchOrder(t, m)
	}
}

func TestSearchPromptKeepsTheLogConsoleKeys(t *testing.T) {
	// The log console toggles live above the view dispatch, so they
	// used to fire while a query was being typed.
	m := testModel()
	m.viewMode = viewPatch
	m.setViewportDoc(docFromLines("a `backtick` and a tab"))
	m.startSearch()

	m = pressKey(m, "`")
	if m.logConsole {
		t.Error("a backtick typed at the prompt opened the log console")
	}
	if m.searchText != "`" {
		t.Errorf("searchText = %q, want the backtick inserted", m.searchText)
	}

	// With the console already open, tab must still reach the prompt
	// rather than moving focus into it.
	m.logConsole = true
	m.logFocused = false
	m = pressSpecialKey(m, tea.KeyTab)
	if m.logFocused {
		t.Error("tab at the prompt moved focus to the log console")
	}

	// And outside the prompt the backtick still toggles.
	m.logConsole = false
	m.clearSearch()
	m = pressKey(m, "`")
	if !m.logConsole {
		t.Error("the backtick should still toggle the console outside a search")
	}
}

func TestSearchPromptTakesCoalescedRunes(t *testing.T) {
	// Bubbletea hands over every rune it can read at once as a single
	// KeyRunes message, so typing quickly can produce one whose name
	// collides with a key name.
	for _, typed := range []string{"up", "down", "enter", "esc", "tab"} {
		m := &Model{width: 80, height: 24, viewMode: viewPatch}
		m.setViewportDoc(docFromLines("nothing of interest"))
		m.searchHistory = []string{"PREVIOUS"}
		m.startSearch()

		m.handleSearchInputKey(tea.KeyMsg{
			Type: tea.KeyRunes, Runes: []rune(typed),
		})
		if m.searchText != typed {
			t.Errorf("typing %q gave searchText %q", typed, m.searchText)
		}
		if !m.searchInput {
			t.Errorf("typing %q closed the prompt", typed)
		}
	}
}

func TestSearchCancelRestoresTheViewport(t *testing.T) {
	// Searching is incremental, so the viewport moves while the query
	// is typed. Cancelling has to undo that.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		if i == 150 {
			b.WriteString("the needle is here\n")
			continue
		}
		b.WriteString(fmt.Sprintf("filler line %d\n", i))
	}
	m := &Model{width: 60, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc(b.String(), 60))
	m.viewportOffset = 20

	m.startSearch()
	typeSearch(m, "needle")
	if m.viewportOffset == 20 {
		t.Fatal("expected the incremental search to scroll away from 20")
	}

	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.viewportOffset != 20 {
		t.Errorf("after esc viewportOffset = %d, want 20", m.viewportOffset)
	}

	// Committing a query that matched nothing also puts it back.
	m.viewportOffset = 20
	m.startSearch()
	typeSearch(m, "needle")
	typeSearch(m, "XX")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.viewportOffset != 20 {
		t.Errorf("after a failed search viewportOffset = %d, want 20",
			m.viewportOffset)
	}

	// But a successful one stays on its match.
	m.viewportOffset = 20
	m.startSearch()
	typeSearch(m, "needle")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.viewportOffset == 20 {
		t.Error("committing a match should leave the viewport on it")
	}
}

func TestSearchFollowsTheMatchAcrossAResize(t *testing.T) {
	m, d := testModelWithDB(t)
	d.UpdatePatchDetail(100,
		strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing\n", 40)+
			"the needle is down here\n", "", "", "")
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.width = 100
	m.height = 24
	m.refreshViewport()

	m.searchText = "needle"
	m.updateSearchMatches()
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}

	onScreen := func(what string) {
		t.Helper()
		row := m.searchOrder[m.searchIdx].row
		visible := m.viewportVisibleLines()
		if row < m.viewportOffset || row >= m.viewportOffset+visible {
			t.Errorf("%s: match on row %d outside the window [%d,%d)",
				what, row, m.viewportOffset, m.viewportOffset+visible)
		}
	}
	onScreen("before resize")

	result, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = result.(*Model)
	checkSearchOrder(t, m)
	onScreen("after narrowing")

	result, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m = result.(*Model)
	checkSearchOrder(t, m)
	onScreen("after widening")
}

func TestSearchIgnoresZeroWidthMatches(t *testing.T) {
	// "x*" and "^" match at every position without covering any
	// text. They cannot be highlighted or stepped to, and counting
	// them would bury whatever real matches there are.
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.setViewportDoc(searchDoc("alpha\nbeta\ngamma\n", 80))

	for _, q := range []string{"x*", "^", "$", "a*"} {
		m.searchText = q
		m.updateSearchMatches()
		for i, mt := range m.viewportMatcher.matches {
			if mt.start == mt.end {
				t.Errorf("query %q: match %d is zero-width at line %d off %d",
					q, i, mt.line, mt.start)
			}
		}
	}

	// A pattern whose only matches are empty finds nothing at all.
	m.searchText = "x*"
	m.updateSearchMatches()
	if len(m.searchOrder) != 0 {
		t.Errorf(`"x*" gave %d matches, want none`, len(m.searchOrder))
	}
	// One that can match real text still does.
	m.searchText = "a*"
	m.updateSearchMatches()
	if len(m.searchOrder) == 0 {
		t.Error(`"a*" should still match the runs of "a"`)
	}
}

func TestSearchReportsNoResultsOnCommit(t *testing.T) {
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.Status = status.NewRegistry(nil)
	m.setViewportDoc(searchDoc("alpha\nbeta\n", 80))

	// Nothing is said while the query is still being typed: every
	// prefix of a real query is briefly matchless, and the prompt
	// already shows a live "[no matches]" hint.
	m.startSearch()
	typeSearch(m, "zzz")
	if msg, _ := m.Status.Active(); msg != "" {
		t.Errorf("status while typing = %q, want nothing", msg)
	}

	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	msg, _ := m.Status.Active()
	if !strings.Contains(msg, "no results") {
		t.Errorf("status = %q, want it to report no results", msg)
	}
	if !strings.Contains(msg, "zzz") {
		t.Errorf("status = %q, want the query quoted back", msg)
	}

	// A pattern that will not compile is reported the same way,
	// rather than quietly searching for its characters.
	m.Status = status.NewRegistry(nil)
	m.startSearch()
	typeSearch(m, "*.c")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, _ := m.Status.Active(); !strings.Contains(msg, "no results") {
		t.Errorf("status for an invalid pattern = %q", msg)
	}
	if len(m.searchOrder) != 0 || m.searchText != "" {
		t.Error("an invalid pattern should leave no search behind")
	}

	// A query that matches says nothing at all.
	m.Status = status.NewRegistry(nil)
	m.startSearch()
	typeSearch(m, "alpha")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, _ := m.Status.Active(); msg != "" {
		t.Errorf("status after a successful search = %q, want nothing", msg)
	}

	// Pressing enter on an empty prompt is not a failed search.
	m.Status = status.NewRegistry(nil)
	m.startSearch()
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, _ := m.Status.Active(); msg != "" {
		t.Errorf("status after an empty query = %q, want nothing", msg)
	}
}

func TestSearchNoResultsMessageExpires(t *testing.T) {
	m := &Model{width: 80, height: 24, viewMode: viewPatch}
	m.Status = status.NewRegistry(nil)
	m.setViewportDoc(searchDoc("alpha\n", 80))

	m.startSearch()
	typeSearch(m, "zzz")
	m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, _ := m.Status.Active(); msg == "" {
		t.Fatal("expected the message to be up")
	}
	// It has to clear on its own; the registry prunes on read and
	// the model schedules a redraw at NextExpiry.
	if d := m.Status.NextExpiry(); d <= 0 || d > searchReportFor {
		t.Errorf("NextExpiry = %v, want it within %v", d, searchReportFor)
	}
}
