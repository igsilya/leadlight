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

// pressKeys feeds a sequence of named keys to the prompt.
func pressKeys(m *Model, names ...string) {
	for _, n := range names {
		m.handleSearchInputKey(keySpecial(n))
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keySpecial(name string) tea.KeyMsg {
	types := map[string]tea.KeyType{
		"esc": tea.KeyEsc, "enter": tea.KeyEnter, "up": tea.KeyUp,
		"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd,
		"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"ctrl+a": tea.KeyCtrlA, "ctrl+e": tea.KeyCtrlE,
		"ctrl+k": tea.KeyCtrlK, "ctrl+u": tea.KeyCtrlU,
		"ctrl+w": tea.KeyCtrlW, "ctrl+g": tea.KeyCtrlG,
		"ctrl+t":    tea.KeyCtrlT,
		"ctrl+left": tea.KeyCtrlLeft, "ctrl+right": tea.KeyCtrlRight,
	}
	if t, ok := types[name]; ok {
		return tea.KeyMsg{Type: t}
	}
	// Alt-modified keys, which the prompt ignores except for the two
	// ctrl+arrows that urxvt reports with the alt bit set.
	switch name {
	case "alt+x":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}
	case "alt+left":
		return tea.KeyMsg{Type: tea.KeyLeft, Alt: true}
	case "alt+backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}
	case "alt+ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft, Alt: true}
	case "alt+ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight, Alt: true}
	}
	panic("unknown key " + name)
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

// searchDocWithMatchesAt builds a document of n lines with "needle"
// on each of the given ones, so a test can say where the matches are
// rather than counting filler.
func searchDocWithMatchesAt(n int, lines ...int) *renderedDoc {
	at := map[int]bool{}
	for _, l := range lines {
		at[l] = true
	}
	body := make([]string, n)
	for i := range body {
		body[i] = "filler"
		if at[i] {
			body[i] = "needle here"
		}
	}
	return searchDoc(strings.Join(body, "\n"), 80)
}

// searchModel opens a patch view over body and runs query over it.
// An empty query leaves the search idle.
func searchModel(body, query string) *Model {
	return searchModelSize(body, query, 80, 24)
}

func searchModelSize(body, query string, width, height int) *Model {
	m := &Model{width: width, height: height, viewMode: viewPatch}
	m.setViewportDoc(searchDoc(body, width))
	if query != "" {
		m.searchText = query
		m.updateSearchMatches()
	}
	return m
}

// promptModel opens the search prompt with text already typed into it.
func promptModel(text string) *Model {
	m := &Model{}
	m.startSearch()
	typeSearch(m, text)
	return m
}

// patchViewWithSearch opens a patch view over the given lines and runs
// a committed search, the way a user would.
func patchViewWithSearch(query string, lines ...string) *Model {
	m := &Model{width: 80, height: 24, viewMode: viewPatch,
		viewingPatchID: 100, viewCommentIdx: -1}
	m.setViewportDoc(docFromLines(lines...))
	m.startSearch()
	typeSearch(m, query)
	m.handleSearchInputKey(keySpecial("enter"))
	return m
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

// assertMatchOnScreen fails unless the match the cursor is on is
// inside the visible window.
func assertMatchOnScreen(t *testing.T, m *Model, what string) {
	t.Helper()
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		t.Fatalf("%s: no current match", what)
	}
	row := m.searchOrder[m.searchIdx].row
	visible := m.viewportVisibleLines()
	if row < m.viewportOffset || row >= m.viewportOffset+visible {
		t.Errorf("%s: match on row %d outside the window [%d,%d)",
			what, row, m.viewportOffset, m.viewportOffset+visible)
	}
}

// ---------------------------------------------------------------
// Pattern parsing
// ---------------------------------------------------------------

func TestExtractCaseFlags(t *testing.T) {
	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			clean, ci := extractCaseFlags(tc.pattern)
			if clean != tc.wantClean || ci != tc.wantCI {
				t.Errorf("extractCaseFlags(%q) = %q, %v; want %q, %v",
					tc.pattern, clean, ci, tc.wantClean, tc.wantCI)
			}
		})
	}
}

// The query is a regular expression, so what this function owns is the
// \c and \C handling on top of regexp and the refusal to quietly treat
// a pattern that will not compile as literal text.
func TestParseVimSearchPattern(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pattern    string
		testString string
		want       bool
	}{
		{"simple literal", "foo", "this is foo bar", true},
		{"case sensitive by default", "Foo", "this is foo bar", false},
		{"case insensitive with trailing \\c", `Foo\c`, "this is foo bar", true},
		{"case insensitive with leading \\c", `\cFoo`, "this is FOO bar", true},
		{"flag applies to the whole alternation", `\cfoo|bar`, "this is BAR", true},
		{"last flag wins, \\c after \\C", `Foo\C\c`, "this is foo bar", true},
		{"last flag wins, \\C after \\c", `Foo\c\C`, "this is foo bar", false},
		{"escaped backslash is not a flag", `foo\\cbar`, `foo\cbar`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			re, err := parseVimSearchPattern(tc.pattern)
			if err != nil {
				t.Fatalf("parseVimSearchPattern(%q) errored: %v", tc.pattern, err)
			}
			if re == nil {
				t.Fatalf("parseVimSearchPattern(%q) returned no regex", tc.pattern)
			}
			if got := re.MatchString(tc.testString); got != tc.want {
				t.Errorf("pattern %q against %q: match = %v, want %v",
					tc.pattern, tc.testString, got, tc.want)
			}
		})
	}

	// An empty pattern is not an error, it just has no regex.
	if re, err := parseVimSearchPattern(""); re != nil || err != nil {
		t.Errorf(`parseVimSearchPattern("") = %v, %v; want nil, nil`, re, err)
	}

	// A pattern that does not compile is an error rather than
	// something quietly searched for as literal text, which would
	// answer a different question than the one that was asked. "*.c"
	// is the common reflex: a glob is not regex syntax, so it has to
	// fail rather than appear to work by matching its characters.
	for _, pattern := range []string{
		"foo(", "a[b", "*", `\cFOO(`, "x{2,1}", `a\`, "*.c",
	} {
		re, err := parseVimSearchPattern(pattern)
		if err == nil {
			t.Errorf("pattern %q compiled to %v, want an error", pattern, re)
		}
		if re != nil {
			t.Errorf("pattern %q returned a regex alongside its error", pattern)
		}
	}
}

// ---------------------------------------------------------------
// Prompt editing primitives
// ---------------------------------------------------------------

// The prompt edits by rune index, so the primitives behind it have to
// stay right on multi-byte text and clamp rather than panic on the
// out-of-range indices that pressing a key at the end of its reach
// naturally produces.

func TestInsertAt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		s       string
		cur     int
		ins     string
		want    string
		wantCur int
	}{
		{"at start", "bar", 0, "foo", "foobar", 3},
		{"at end", "foo", 3, "bar", "foobar", 6},
		{"mid string", "foobar", 3, "-", "foo-bar", 4},
		{"multi byte", "café", 4, "☕", "café☕", 5},
		{"into multi byte", "café", 2, "X", "caXfé", 3},
		{"empty insert", "foo", 1, "", "foo", 1},
		{"cursor past end clamps", "foo", 99, "!", "foo!", 4},
		{"negative cursor clamps", "foo", -3, "!", "!foo", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, gotCur := insertAt(tc.s, tc.cur, tc.ins)
			if got != tc.want || gotCur != tc.wantCur {
				t.Errorf("insertAt(%q, %d, %q) = %q, %d; want %q, %d",
					tc.s, tc.cur, tc.ins, got, gotCur, tc.want, tc.wantCur)
			}
		})
	}
}

func TestDeleteRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		s        string
		from, to int
		want     string
		wantCur  int
	}{
		{"head", "foobar", 0, 3, "bar", 0},
		{"tail", "foobar", 3, 6, "foo", 3},
		{"middle", "foobar", 2, 4, "foar", 2},
		{"whole", "foobar", 0, 6, "", 0},
		{"multi byte", "café☕", 4, 5, "café", 4},
		// A key pressed at the end of its reach asks for an empty or
		// inverted range; the text has to come back unchanged so that
		// the caller can tell nothing happened.
		{"empty range", "foobar", 3, 3, "foobar", 3},
		{"inverted range", "foobar", 4, 2, "foobar", 4},
		{"backspace at start", "foobar", -1, 0, "foobar", 0},
		{"delete at end", "foobar", 6, 7, "foobar", 6},
		{"both out of range", "foobar", 99, 99, "foobar", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, gotCur := deleteRange(tc.s, tc.from, tc.to)
			if got != tc.want || gotCur != tc.wantCur {
				t.Errorf("deleteRange(%q, %d, %d) = %q, %d; want %q, %d",
					tc.s, tc.from, tc.to, got, gotCur, tc.want, tc.wantCur)
			}
		})
	}
}

func TestWordMotions(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(string, int) int
		s    string
		cur  int
		want int
	}{
		{"start from end of word", wordStart, "foo bar", 7, 4},
		{"start from mid word", wordStart, "foo bar", 6, 4},
		{"start at a word start", wordStart, "foo bar", 4, 0},
		{"start over a run of spaces", wordStart, "foo bar  ", 9, 4},
		{"start at zero", wordStart, "foo bar", 0, 0},
		{"start in all spaces", wordStart, "   ", 3, 0},
		{"start in empty text", wordStart, "", 0, 0},
		// Punctuation is not a word boundary, so a whole pattern
		// fragment comes off in one press.
		{"start past punctuation", wordStart, `foo \.c$`, 8, 4},
		// Rune indices, not bytes: "café ☕ naïve" is 12 runes.
		{"start on multi byte", wordStart, "café ☕ naïve", 12, 7},
		{"end from start of word", wordEnd, "foo bar", 0, 3},
		{"end from mid word", wordEnd, "foo bar", 1, 3},
		{"end from a space", wordEnd, "foo bar", 3, 7},
		{"end over a run of spaces", wordEnd, "foo  bar", 3, 8},
		{"end at the end", wordEnd, "foo bar", 7, 7},
		{"end in all spaces", wordEnd, "   ", 0, 3},
		{"end on multi byte", wordEnd, "café ☕ naïve", 0, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.s, tc.cur); got != tc.want {
				t.Errorf("(%q, %d) = %d, want %d", tc.s, tc.cur, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------
// Prompt key handling
// ---------------------------------------------------------------

func TestSearchInputEditing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		text     string
		keys     []string
		paste    string
		thenType string
		want     string
		wantCur  int
	}{
		{name: "typing accepts multi-byte runes",
			text: "café ☕", want: "café ☕", wantCur: 6},
		{name: "backspace removes a whole rune, not a byte",
			text: "café ☕", keys: []string{"backspace", "backspace"},
			want: "café", wantCur: 4},
		{name: "a paste drops what is not printable",
			paste: "foo\nbär\tbaz", want: "foobärbaz", wantCur: 9},
		{name: "typing inserts at the cursor",
			text: "foobar", keys: []string{"left", "left", "left"},
			thenType: "X", want: "fooXbar", wantCur: 4},
		{name: "a paste lands at the cursor as a block",
			text: "foobaz", keys: []string{"left", "left", "left"},
			paste: "ba\nr-", want: "foobar-baz", wantCur: 7},
		{name: "backspace deletes before the cursor",
			text: "foobar", keys: []string{"left", "left", "left", "backspace"},
			want: "fobar", wantCur: 2},
		{name: "delete removes the rune under the cursor",
			text: "foobar", keys: []string{"left", "left", "left", "delete"},
			want: "fooar", wantCur: 3},
		{name: "delete at the end has nothing to remove",
			text: "foobar", keys: []string{"delete"}, want: "foobar", wantCur: 6},
		{name: "ctrl+w takes the word behind the cursor",
			text: "foo bar  baz", keys: []string{"ctrl+w"},
			want: "foo bar  ", wantCur: 9},
		{name: "ctrl+w then takes the spaces and the word before them",
			text: "foo bar  baz", keys: []string{"ctrl+w", "ctrl+w"},
			want: "foo ", wantCur: 4},
		{name: "ctrl+w at column zero has no word behind it",
			text: "foo bar", keys: []string{"home", "ctrl+w"},
			want: "foo bar", wantCur: 0},
		{name: "ctrl+k kills to the end of the line",
			text: "foobar", keys: []string{"left", "left", "left", "ctrl+k"},
			want: "foo", wantCur: 3},
		{name: "ctrl+u discards back to the start",
			text: "foobar", keys: []string{"left", "left", "left", "ctrl+u"},
			want: "bar", wantCur: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := promptModel(tc.text)
			pressKeys(m, tc.keys...)
			if tc.paste != "" {
				m.handleSearchInputKey(tea.KeyMsg{Type: tea.KeyRunes,
					Paste: true, Runes: []rune(tc.paste)})
			}
			typeSearch(m, tc.thenType)

			if m.searchText != tc.want || m.searchCursor != tc.wantCur {
				t.Errorf("searchText = %q cursor %d, want %q and %d",
					m.searchText, m.searchCursor, tc.want, tc.wantCur)
			}
			// Emptying the query is not the same as leaving: only
			// backspace on an already-empty query closes the prompt.
			if !m.searchInput {
				t.Error("the prompt closed")
			}
		})
	}
}

func TestSearchInputCursorMotion(t *testing.T) {
	// "café ☕" is six runes and ten bytes; the cursor counts runes.
	for _, tc := range []struct {
		name string
		text string
		keys []string
		want int
	}{
		{"left moves back", "foobar", []string{"left", "left", "left"}, 3},
		{"right moves on", "foobar", []string{"left", "left", "right"}, 5},
		{"left clamps at the start", "ab",
			[]string{"left", "left", "left", "left"}, 0},
		{"right clamps at the end", "ab", []string{"right", "right"}, 2},
		{"home", "café ☕", []string{"home"}, 0},
		{"end", "café ☕", []string{"home", "end"}, 6},
		{"ctrl+a", "café ☕", []string{"ctrl+a"}, 0},
		{"ctrl+e", "café ☕", []string{"ctrl+a", "ctrl+e"}, 6},
		{"ctrl+left jumps a word", "foo bar baz", []string{"ctrl+left"}, 8},
		{"ctrl+left jumps two", "foo bar baz",
			[]string{"ctrl+left", "ctrl+left"}, 4},
		{"ctrl+left clamps", "foo bar baz",
			[]string{"ctrl+left", "ctrl+left", "ctrl+left", "ctrl+left"}, 0},
		{"ctrl+right jumps a word", "foo bar baz",
			[]string{"home", "ctrl+right"}, 3},
		{"ctrl+right clamps", "foo bar baz",
			[]string{"ctrl+right", "ctrl+right"}, 11},
		// urxvt reports ctrl+arrow with the alt bit set, so those two
		// are let through by name where every other alt key is not.
		{"urxvt alt-flagged ctrl+left", "foo bar",
			[]string{"alt+ctrl+left"}, 4},
		{"urxvt alt-flagged ctrl+right", "foo bar",
			[]string{"home", "alt+ctrl+right"}, 3},
		// Alt is how a terminal sends the readline word motions this
		// prompt deliberately does not implement. They must stay inert
		// rather than fall through to the unmodified key.
		{"alt+x does not type an x", "foo", []string{"alt+x"}, 3},
		{"alt+left does not move", "foo", []string{"alt+left"}, 3},
		{"alt+backspace does not delete", "foo", []string{"alt+backspace"}, 3},
		{"unbound control keys do nothing", "foo",
			[]string{"ctrl+g", "ctrl+t"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := promptModel(tc.text)
			pressKeys(m, tc.keys...)
			if m.searchCursor != tc.want {
				t.Errorf("searchCursor = %d, want %d", m.searchCursor, tc.want)
			}
			if m.searchText != tc.text {
				t.Errorf("searchText = %q, want it unchanged at %q",
					m.searchText, tc.text)
			}
		})
	}
}

// Backspace leaves the prompt only when the query is already empty.
// At the left edge of a query that still has text it does nothing:
// the cursor only reaches column zero by deliberate motion, so
// discarding the query there would be a surprise.
func TestSearchInputBackspaceAtStartKeepsQuery(t *testing.T) {
	m := promptModel("foo")
	pressKeys(m, "home", "backspace")
	if m.searchText != "foo" {
		t.Errorf("searchText = %q, want %q", m.searchText, "foo")
	}
	if !m.searchInput {
		t.Error("backspace at column 0 left the prompt, want it open")
	}

	// Emptying the query and pressing it again does leave.
	pressKeys(m, "ctrl+k")
	if m.searchText != "" || !m.searchInput {
		t.Fatalf("after ctrl+k: text %q input %v, want empty and open",
			m.searchText, m.searchInput)
	}
	pressKeys(m, "backspace")
	if m.searchInput {
		t.Error("backspace on an empty query kept the prompt open")
	}
}

// Moving the cursor is not editing the query, so it must not re-run
// the search: updateSearchMatches drops back to the first match and
// scrolls there, which would yank the view away from the match being
// read. Edits that produce identical text have to be just as inert.
func TestSearchInputCursorMotionKeepsCurrentMatch(t *testing.T) {
	m := searchModelSize(strings.Repeat("filler\n", 20)+
		"needle one\nfiller\nneedle two\nfiller\nneedle three", "", 80, 10)
	m.startSearch()
	typeSearch(m, "needle")
	m.nextSearchMatch()

	wantIdx, wantOff := m.searchIdx, m.viewportOffset
	if wantIdx == 0 {
		t.Fatalf("test needs to be past the first match, searchIdx = 0")
	}

	keys := []string{"left", "right", "home", "end", "ctrl+a", "ctrl+e",
		"ctrl+left", "ctrl+right",
		// An edit key that deletes nothing is still not an edit.
		"home", "ctrl+w"}
	for _, key := range keys {
		pressKeys(m, key)
		if m.searchIdx != wantIdx || m.viewportOffset != wantOff {
			t.Errorf("%s moved to idx %d off %d, want %d and %d",
				key, m.searchIdx, m.viewportOffset, wantIdx, wantOff)
		}
	}
}

func TestSearchPromptTakesCoalescedRunes(t *testing.T) {
	// Bubbletea hands over every rune it can read at once as a single
	// KeyRunes message, so typing quickly can produce one whose name
	// collides with a key name.
	for _, typed := range []string{"up", "down", "enter", "esc", "tab"} {
		m := searchModel("nothing of interest", "")
		m.searchHistory = []string{"PREVIOUS"}
		m.startSearch()

		m.handleSearchInputKey(keyRunes(typed))
		if m.searchText != typed {
			t.Errorf("typing %q gave searchText %q", typed, m.searchText)
		}
		if !m.searchInput {
			t.Errorf("typing %q closed the prompt", typed)
		}
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

// ---------------------------------------------------------------
// Leaving the prompt and the view
// ---------------------------------------------------------------

func TestSearchPromptAndViewExitKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		committed bool
		keys      []string
		wantMode  viewMode
		wantInput bool
	}{
		// esc at the prompt cancels the query being typed; it must not
		// also close the patch view.
		{name: "esc at the prompt keeps the view",
			keys: []string{"esc"}, wantMode: viewPatch},
		{name: "backspacing past the start leaves the prompt only",
			keys:     []string{"backspace", "backspace", "backspace"},
			wantMode: viewPatch},
		// With a committed search, q clears it before it exits.
		{name: "q clears a committed search first",
			committed: true, keys: []string{"q"}, wantMode: viewPatch},
		{name: "a second q leaves",
			committed: true, keys: []string{"q", "q"}, wantMode: viewTable},
		// esc is the shortcut: it leaves outright either way.
		{name: "esc leaves a committed search outright",
			committed: true, keys: []string{"esc"}, wantMode: viewTable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m *Model
			if tc.committed {
				m = patchViewWithSearch("needle", "a needle here", "nothing")
				if len(m.searchOrder) != 1 {
					t.Fatalf("expected the search to match, got %d",
						len(m.searchOrder))
				}
			} else {
				m = searchModel("a needle here", "")
				m.startSearch()
				typeSearch(m, "ne")
			}

			for _, k := range tc.keys {
				if k == "q" {
					m.handleViewportKey(keyRunes("q"))
					continue
				}
				m.handleViewportKey(keySpecial(k))
			}

			if m.viewMode != tc.wantMode {
				t.Errorf("viewMode = %d, want %d", m.viewMode, tc.wantMode)
			}
			if m.searchInput != tc.wantInput {
				t.Errorf("searchInput = %v, want %v",
					m.searchInput, tc.wantInput)
			}
			if m.searchText != "" {
				t.Errorf("searchText = %q, want it cleared", m.searchText)
			}
			if tc.wantMode == viewTable &&
				(m.viewportDoc != nil || m.viewingPatchID != 0) {
				t.Error("leaving the view should drop the content it showed")
			}
		})
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
	m.handleSearchInputKey(keySpecial("enter"))
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
	m.setSearchQuery("nomatch")
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

// ---------------------------------------------------------------
// Finding and ordering matches
// ---------------------------------------------------------------

func TestSearchFindsEveryMatchInReadingOrder(t *testing.T) {
	m := &Model{viewportDoc: docFromLines(
		"foo bar foo baz foo", "no matches here", "another foo here")}
	m.setSearchQuery("foo")

	if len(m.searchOrder) != 4 {
		t.Fatalf("expected 4 matches, got %d", len(m.searchOrder))
	}
	// Three on the first line at distinct offsets, one on the third.
	found := m.viewportMatcher.matches
	var gotLines, gotStarts []int
	for _, f := range found {
		gotLines = append(gotLines, f.line)
		gotStarts = append(gotStarts, f.start)
	}
	if want := []int{0, 0, 0, 2}; !reflect.DeepEqual(gotLines, want) {
		t.Errorf("matches on lines %v, want %v", gotLines, want)
	}
	if want := []int{0, 8, 16, 8}; !reflect.DeepEqual(gotStarts, want) {
		t.Errorf("matches at offsets %v, want %v", gotStarts, want)
	}
	if m.searchIdx != 0 {
		t.Errorf("a fresh search starts at %d, want 0", m.searchIdx)
	}

	// n walks forward and wraps past the end; N walks back and wraps
	// past the start.
	for _, want := range []int{1, 2, 3, 0} {
		m.nextSearchMatch()
		if m.searchIdx != want {
			t.Fatalf("after n: searchIdx = %d, want %d", m.searchIdx, want)
		}
	}
	for _, want := range []int{3, 2, 1, 0} {
		m.prevSearchMatch()
		if m.searchIdx != want {
			t.Fatalf("after N: searchIdx = %d, want %d", m.searchIdx, want)
		}
	}

	// Neither key does anything, or panics, with no matches at all.
	m.clearSearch()
	m.nextSearchMatch()
	m.prevSearchMatch()
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d with no matches, want -1", m.searchIdx)
	}
}

func TestSearchFindsTextAcrossAWrap(t *testing.T) {
	withColor(t)

	// A long identifier with no spaces to break on, so the wrap falls
	// in the middle of it. Searching the rendered rows could never
	// match across that break.
	line := "ovs_alpha_bravo_charlie_delta_echo_foxtrot_golf_function"
	m := searchModelSize(line, "charlie_delta", 25, 20)

	if m.viewportDoc.Len() < 2 {
		t.Fatalf("expected the line to wrap, got %d rows", m.viewportDoc.Len())
	}
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

func TestSearchRescansWhenContentChanges(t *testing.T) {
	m := searchModelSize("needle one\nneedle two\nneedle three", "needle", 60, 20)
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
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		t.Errorf("searchIdx = %d is out of range", m.searchIdx)
	}
	checkSearchOrder(t, m)

	// And content with no match at all must clear the cursor.
	m.setViewportDoc(searchDoc("nothing here", 60))
	if len(m.searchOrder) != 0 {
		t.Errorf("expected no matches, got %d", len(m.searchOrder))
	}
	if m.searchIdx != -1 {
		t.Errorf("searchIdx = %d, want -1", m.searchIdx)
	}
}

// A pattern can be valid and still have nothing to find. "\c" is all
// flag and no pattern once stripped; "x*" and "^" match at every
// position without covering any text. Neither can be highlighted or
// stepped to, and counting them would bury the real matches.
func TestSearchPatternsThatMatchNothing(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    int
	}{
		{`\c`, 0}, {`\C`, 0}, {`\c\C`, 0},
		{"x*", 0}, {"^", 0}, {"$", 0},
		// "a*" still matches the runs of "a" it can actually cover.
		{"a*", 5}, {"alpha", 1},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			m := searchModelSize("alpha\nbeta\ngamma\n", tc.pattern, 60, 20)
			if got := len(m.searchOrder); got != tc.want {
				t.Errorf("pattern %q gave %d matches, want %d",
					tc.pattern, got, tc.want)
			}
			for i, mt := range m.viewportMatcher.matches {
				if mt.start == mt.end {
					t.Errorf("pattern %q: match %d is zero-width at line %d",
						tc.pattern, i, mt.line)
				}
			}
			if tc.want == 0 && m.searchIdx != -1 {
				t.Errorf("pattern %q: searchIdx = %d, want -1",
					tc.pattern, m.searchIdx)
			}
		})
	}
}

func TestSearchSkipsCollapsedHeaderText(t *testing.T) {
	// A long recipient list is folded to a few rows with a "N total"
	// marker. The addresses that were folded away are not on screen,
	// so they must not be findable: a match there could be stepped to
	// but never seen.
	var addrs []string
	for i := 0; i < 20; i++ {
		addrs = append(addrs, fmt.Sprintf("person%02d@ipsum.example", i))
	}
	addrs = append(addrs, "hidden-needle@ipsum.example")
	p := ParsedMbox{Subject: "Lorem ipsum", From: "a@b.example",
		Cc: strings.Join(addrs, ", ")}

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
	m.setSearchQuery("hidden-needle")
	if len(m.searchOrder) != 0 {
		t.Errorf("found %d matches in folded-away text, want none",
			len(m.searchOrder))
	}

	// An address that is still on screen is found as normal, and the
	// marker itself stays searchable.
	for _, q := range []string{"person00", "to expand"} {
		m.setSearchQuery(q)
		if len(m.searchOrder) == 0 {
			t.Errorf("query %q should still match", q)
			continue
		}
		checkSearchOrder(t, m)
	}
}

// ---------------------------------------------------------------
// Highlighting
// ---------------------------------------------------------------

func TestSearchHighlightPreservesText(t *testing.T) {
	withColor(t)
	line := "lorem needle ipsum needle dolor sit amet consectetur needle"
	m := searchModelSize(line, "needle", 28, 20)

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
	m := searchModelSize("needle one\nneedle two\nneedle three", "needle", 60, 20)
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
			if strings.Contains(renderDocRow(m.viewportDoc, row, mt, cur),
				searchCurrentStyle.Render("needle")) {
				current++
			}
		}
		if current != 1 {
			t.Errorf("idx %d: %d rows show the current style, want 1", idx, current)
		}
	}
}

// The prompt draws a block cursor on the rune it sits on. Past the end
// of the query there is no rune to mark, so it takes a space of its
// own; the query text itself must come through untouched either way.
func TestRenderSearchQuery(t *testing.T) {
	withColor(t)
	for _, tc := range []struct {
		name   string
		text   string
		cursor int
		want   string
		on     string
	}{
		{"past the end", "foobar", 6, "foobar ", " "},
		{"inside the query", "foobar", 3, "foobar", "b"},
		{"at the start", "foobar", 0, "foobar", "f"},
		{"empty query", "", 0, " ", " "},
		// Nothing outside the prompt maintains searchCursor, so a
		// stale one has to draw oddly rather than panic.
		{"cursor past the text clamps", "foo", 99, "foo ", " "},
		{"negative cursor clamps", "foo", -5, "foo", "f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderSearchQuery(tc.text, tc.cursor)
			if plain := ansi.Strip(got); plain != tc.want {
				t.Errorf("renderSearchQuery(%q, %d) = %q, want %q",
					tc.text, tc.cursor, plain, tc.want)
			}
			if want := searchCursorStyle.Render(tc.on); !strings.Contains(got, want) {
				t.Errorf("renderSearchQuery(%q, %d) has no cursor on %q",
					tc.text, tc.cursor, tc.on)
			}
		})
	}
}

func TestSearchHelpShowsMatchCounter(t *testing.T) {
	bright, desc, sep := (&Model{}).helpStyles()
	m := searchModel("needle one\nneedle two", "")

	// While typing, the prompt carries the query and the counter.
	m.startSearch()
	typeSearch(m, "needle")
	got := ansi.Strip(m.renderSearchInputHelp(bright, desc, sep))
	for _, want := range []string{"Search: ", "needle ", "[1/2]", "enter", "esc"} {
		if !strings.Contains(got, want) {
			t.Errorf("input help %q missing %q", got, want)
		}
	}

	// Stepping forward updates it, and a query matching nothing says so.
	m.nextSearchMatch()
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[2/2]") {
		t.Errorf("input help after n = %q, want [2/2]", got)
	}
	m.setSearchQuery("nomatch")
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[no matches]") {
		t.Errorf("input help = %q, want [no matches]", got)
	}

	// Once committed, the key help carries the count instead.
	m.setSearchQuery("needle")
	m.commitSearch()
	if got := m.renderSearchKeyHelp(bright, desc, sep); !strings.Contains(
		got, "n/N") || !strings.Contains(got, "[2]") {
		t.Errorf("key help %q should offer n/N and the count", got)
	}
	m.clearSearch()
	if got := m.renderSearchKeyHelp(bright, desc, sep); strings.Contains(
		got, "n/N") {
		t.Errorf("key help %q should not offer n/N with no search", got)
	}
}

func TestSearchHelpDropsNavWhenContentLosesMatches(t *testing.T) {
	// A search can end up with no matches because the content changed
	// under it. It stays active, so that going back restores the
	// highlights, but n/N must not be advertised meanwhile.
	bright, desc, sep := (&Model{}).helpStyles()
	m := searchModel("alpha needle\n", "needle")
	if !strings.Contains(m.renderSearchKeyHelp(bright, desc, sep), "n/N") {
		t.Fatal("expected n/N to be offered while matches exist")
	}

	m.setViewportDoc(searchDoc("nothing of interest\n", 80))
	if got := m.renderSearchKeyHelp(bright, desc, sep); strings.Contains(
		got, "n/N") {
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

// ---------------------------------------------------------------
// Rescanning when the content underneath changes
// ---------------------------------------------------------------

func TestSearchRescansOnCommentSwitch(t *testing.T) {
	m, _ := testModelWithDB(t)
	m.width, m.height = 80, 24
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewComments = []CommentInfo{
		{ID: 1, Submitter: "Lorem", Date: "2026-01-01",
			Subject: "Re: test", Content: "needle needle needle"},
		{ID: 2, Submitter: "Dolor", Date: "2026-01-02",
			Subject: "Re: test", Content: "nothing of interest"},
	}
	m.viewCommentIdx = 0
	m.switchToComment()
	m.setSearchQuery("needle")
	if len(m.searchOrder) != 3 {
		t.Fatalf("expected 3 matches in the first comment, got %d",
			len(m.searchOrder))
	}

	// Moving to a comment with no match must leave nothing behind.
	m.nextComment()
	if len(m.searchOrder) != 0 || m.searchIdx != -1 {
		t.Errorf("after the switch: %d matches idx %d, want 0 and -1",
			len(m.searchOrder), m.searchIdx)
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
	m.width, m.height = 80, 24
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewComments = []CommentInfo{{
		ID: 1, Submitter: "Lorem", Date: "2026-01-01", Subject: "Re: test",
		Content: "reply\n" + strings.Repeat("> needle in a quoted line\n", 20),
	}}
	m.viewCommentIdx = 0
	m.viewExpanded = false
	m.switchToComment()
	m.setSearchQuery("needle")
	collapsed := len(m.searchOrder)

	// "e" expands the collapsed quote block, bringing more text into
	// view and more matches with it.
	m.handleViewportKey(keyRunes("e"))
	if expanded := len(m.searchOrder); expanded <= collapsed {
		t.Errorf("expanding should reveal more matches: %d -> %d",
			collapsed, expanded)
	}
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		t.Errorf("searchIdx = %d is out of range after expanding", m.searchIdx)
	}
	checkSearchOrder(t, m)
}

// Resizing re-renders the content at the new width through the real
// window-message path, and the matches have to follow it: the same
// count, on the same source lines, still on screen.
func TestSearchFollowsTheMatchAcrossAResize(t *testing.T) {
	m, d := testModelWithDB(t)
	d.UpdatePatchDetail(100,
		strings.Repeat("lorem ipsum needle dolor sit amet consectetur\n", 40)+
			"the last needle is down here\n", "", "", "")
	m.viewMode = viewPatch
	m.viewingPatchID = 100
	m.viewCommentIdx = -1
	m.width, m.height = 100, 24
	m.refreshViewport()

	m.setSearchQuery("needle")
	before := len(m.searchOrder)
	if before < 2 {
		t.Fatalf("expected several matches, got %d", before)
	}
	m.searchIdx = before - 1
	m.scrollToMatch(m.searchIdx)
	wantLine := m.viewportMatcher.matches[m.searchOrder[m.searchIdx].idx].line
	assertMatchOnScreen(t, m, "before resize")

	for _, width := range []int{40, 160} {
		result, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = result.(*Model)
		what := fmt.Sprintf("at width %d", width)

		if got := len(m.searchOrder); got != before {
			t.Errorf("%s: match count changed %d -> %d", what, before, got)
		}
		if m.searchIdx != before-1 {
			t.Errorf("%s: cursor moved to %d, want %d",
				what, m.searchIdx, before-1)
		}
		got := m.viewportMatcher.matches[m.searchOrder[m.searchIdx].idx].line
		if got != wantLine {
			t.Errorf("%s: cursor is on source line %d, want %d",
				what, got, wantLine)
		}
		checkSearchOrder(t, m)
		assertMatchOnScreen(t, m, what)
	}
}

// ---------------------------------------------------------------
// Compare view
// ---------------------------------------------------------------

func TestCompareSearchKeepsTheTwoSidesApart(t *testing.T) {
	skipIfNoDiff(t)

	// "shared" is on both sides, "onlyleft" on one. The two columns
	// are indexed separately, so an identical row cannot produce one
	// match that appears to belong to both.
	left := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "shared line\nonlyleft line\nshared tail\n"}
	right := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "shared line\nshared tail\n"}
	m := buildTestCompare(t, left, right, 80)
	m.viewMode = viewCompare

	m.setSearchQuery("onlyleft")
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}
	if m.searchOrder[0].target != 0 {
		t.Errorf("match attributed to column %d, want 0", m.searchOrder[0].target)
	}
	if mt, cur := m.currentMatch(1); cur != nil || len(mt.matches) != 0 {
		t.Error("right column should have no matches for a left-only term")
	}

	// A term on both sides gives one match per column, not one match
	// counted twice.
	m.setSearchQuery("shared")
	leftN := len(m.compare[0].matcher.matches)
	rightN := len(m.compare[1].matcher.matches)
	if leftN == 0 || rightN == 0 {
		t.Fatalf("expected matches on both sides, got %d and %d", leftN, rightN)
	}
	if len(m.searchOrder) != leftN+rightN {
		t.Errorf("searchOrder has %d refs, want %d+%d",
			len(m.searchOrder), leftN, rightN)
	}

	// Exactly one match is current at a time and belongs to one column
	// only, and n walks down the screen rather than down one column.
	prev := -1
	for idx, ref := range m.searchOrder {
		m.searchIdx = idx
		_, lc := m.currentMatch(0)
		_, rc := m.currentMatch(1)
		if (lc != nil) == (rc != nil) {
			t.Errorf("idx %d: current match is in %v/%v columns",
				idx, lc != nil, rc != nil)
		}
		if ref.row < prev {
			t.Errorf("match %d is on row %d, after row %d", idx, ref.row, prev)
		}
		prev = ref.row
	}
	checkSearchOrder(t, m)
}

func TestCompareSearchMatchesAtEndOfLine(t *testing.T) {
	skipIfNoDiff(t)
	withColor(t)

	// The compare view pads each column out to its width after the row
	// is rendered. A match at the very end of a line must not be
	// disturbed by that padding.
	left := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "ends with needle\n"}
	right := ParsedMbox{Subject: "Lorem", From: "a@b.example",
		Body: "different text\n"}
	m := buildTestCompare(t, left, right, 80)
	m.viewMode = viewCompare

	m.setSearchQuery("needle$")
	if len(m.searchOrder) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
	}

	row := m.searchOrder[0].row
	mt, cur := m.currentMatch(0)
	rendered := renderDocRow(m.compare[0].doc, row, mt, cur)
	if !strings.Contains(rendered, searchCurrentStyle.Render("needle")) {
		t.Errorf("row %d did not highlight the trailing match: %q", row, rendered)
	}
	got, want := ansi.Strip(rendered), ansi.Strip(m.compare[0].doc.Line(row))
	if got != want {
		t.Errorf("highlighting changed the text\n got %q\nwant %q", got, want)
	}
}

// Both of the compare view's rebuild paths — a resize and cycling one
// column to another patch — drop the documents an active search was
// scanning, so it has to be re-run against the new ones.
func TestCompareSearchRescansOnRebuild(t *testing.T) {
	skipIfNoDiff(t)

	t.Run("resize", func(t *testing.T) {
		body := strings.Repeat(
			"lorem ipsum needle dolor sit amet consectetur adipiscing\n", 5)
		left := ParsedMbox{Subject: "Lorem", From: "a@b.example", Body: body}
		right := ParsedMbox{Subject: "Lorem", From: "a@b.example",
			Body: body + "an extra needle line\n"}
		m := buildTestCompare(t, left, right, 120)
		m.viewMode = viewCompare
		m.Status = status.NewRegistry(nil)

		m.setSearchQuery("needle")
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
	})

	t.Run("patch cycle", func(t *testing.T) {
		m := &Model{width: 100, height: 24, viewMode: viewCompare}
		m.Status = status.NewRegistry(nil)
		for i := range m.compare {
			m.compare[i] = compareSide{idx: 0, patches: []comparePatch{
				{id: 1, position: 1, parsed: ParsedMbox{Subject: "Lorem",
					From: "a@b.example", Body: "a needle here\n"}},
				{id: 2, position: 2, parsed: ParsedMbox{Subject: "Dolor",
					From: "a@b.example", Body: "no match at all\n"}},
			}}
		}
		m.buildCompareContent()

		m.setSearchQuery("needle")
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
	})
}

// ---------------------------------------------------------------
// Where a search starts and what it scrolls to
// ---------------------------------------------------------------

func TestSearchScrollsToTheMatch(t *testing.T) {
	// A match far down a long document has to bring the viewport with
	// it rather than just moving an index, and a match near the bottom
	// cannot be centred without scrolling past the end.
	for _, tc := range []struct {
		name     string
		filler   int
		matchAt  int
		wantZero bool
	}{
		{"match far down", 200, 150, false},
		{"match on the last line", 61, 60, false},
		{"document shorter than the window", 3, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := searchModelSize("", "", 80, 24)
			m.setViewportDoc(searchDocWithMatchesAt(tc.filler, tc.matchAt))
			m.setSearchQuery("needle")
			if len(m.searchOrder) != 1 {
				t.Fatalf("expected 1 match, got %d", len(m.searchOrder))
			}
			assertMatchOnScreen(t, m, tc.name)

			visible := m.viewportVisibleLines()
			maxOffset := max(m.viewportDoc.Len()-visible, 0)
			if m.viewportOffset > maxOffset {
				t.Errorf("viewportOffset = %d, past the last page at %d",
					m.viewportOffset, maxOffset)
			}
			if tc.wantZero && m.viewportOffset != 0 {
				t.Errorf("viewportOffset = %d for a short document, want 0",
					m.viewportOffset)
			}
		})
	}
}

// A search begun partway down a document picks up from the top of the
// window rather than yanking the reader back to the first match in the
// file, and wraps to the top once it runs off the end.
func TestSearchStartsFromViewportNotTopOfFile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		offset   int
		wantIdx  int
		wantRow  int
		thenRows []int // rows n steps through from there
	}{
		{name: "at top of file", offset: 0, wantIdx: 0, wantRow: 5},
		{name: "above the first match", offset: 3, wantIdx: 0, wantRow: 5},
		{name: "exactly on a match", offset: 15, wantIdx: 1, wantRow: 15},
		{name: "between matches", offset: 10, wantIdx: 1, wantRow: 15,
			// Stepping on still walks the whole document and wraps, so
			// nothing above the starting point is lost.
			thenRows: []int{25, 5, 15}},
		{name: "past the last match wraps", offset: 26, wantIdx: 0, wantRow: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := searchModelSize("", "", 80, 10)
			m.setViewportDoc(searchDocWithMatchesAt(40, 5, 15, 25))
			m.viewportOffset = tc.offset
			m.startSearch()
			typeSearch(m, "needle")

			if len(m.searchOrder) != 3 {
				t.Fatalf("searchOrder has %d matches, want 3", len(m.searchOrder))
			}
			if m.searchIdx != tc.wantIdx {
				t.Errorf("searchIdx = %d, want %d", m.searchIdx, tc.wantIdx)
			}
			if row := m.searchOrder[m.searchIdx].row; row != tc.wantRow {
				t.Errorf("landed on row %d, want %d", row, tc.wantRow)
			}
			for i, want := range tc.thenRows {
				m.nextSearchMatch()
				if row := m.searchOrder[m.searchIdx].row; row != want {
					t.Errorf("step %d landed on row %d, want %d", i+1, row, want)
				}
			}
			checkSearchOrder(t, m)
		})
	}
}

// The starting point is fixed when the prompt opens, so refining a
// query does not let it drift down the document as the view scrolls
// along with the incremental search.
func TestSearchStartPointDoesNotDriftWhileTyping(t *testing.T) {
	m := searchModelSize("", "", 80, 10)
	m.setViewportDoc(searchDocWithMatchesAt(40, 5, 15, 25))
	m.viewportOffset = 10
	m.startSearch()
	typeSearch(m, "needle")
	if row := m.searchOrder[m.searchIdx].row; row != 15 {
		t.Fatalf("first match is row %d, want 15", row)
	}

	// Backspacing to a shorter query that still matches the same three
	// lines has to come back to the same place, not to wherever the
	// view scrolled to in the meantime.
	pressKeys(m, "backspace", "backspace", "backspace")
	if m.searchText != "nee" {
		t.Fatalf("searchText = %q, want %q", m.searchText, "nee")
	}
	if row := m.searchOrder[m.searchIdx].row; row != 15 {
		t.Errorf("after backspacing, landed on row %d, want 15", row)
	}
}

// Searching is incremental, so the viewport moves while the query is
// typed. Anything that abandons the search has to undo that.
func TestSearchCancelRestoresTheViewport(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		finish      string
		wantRestore bool
	}{
		{"esc puts it back", "needle", "esc", true},
		{"a committed query that matched nothing puts it back",
			"needleXX", "enter", true},
		{"a committed match stays where it landed", "needle", "enter", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := searchModelSize("", "", 80, 24)
			m.setViewportDoc(searchDocWithMatchesAt(200, 150))
			m.viewportOffset = 20

			m.startSearch()
			typeSearch(m, tc.query)
			if tc.wantRestore && m.viewportOffset == 20 {
				t.Fatal("expected the incremental search to scroll away from 20")
			}

			pressKeys(m, tc.finish)
			if restored := m.viewportOffset == 20; restored != tc.wantRestore {
				t.Errorf("viewportOffset = %d, restored = %v, want %v",
					m.viewportOffset, restored, tc.wantRestore)
			}
		})
	}
}

// ---------------------------------------------------------------
// History
// ---------------------------------------------------------------

func TestSearchHistoryCommitsRecallsAndBrowses(t *testing.T) {
	m := &Model{viewportDoc: docFromLines("foo bar foo", "hello", "another foo")}
	for _, q := range []string{"foo", "bar", "hello"} {
		m.startSearch()
		typeSearch(m, q)
		m.commitSearch()
	}
	if want := []string{"hello", "bar", "foo"}; !reflect.DeepEqual(
		m.searchHistory, want) {
		t.Fatalf("history = %v, want %v", m.searchHistory, want)
	}

	// Re-running an old query moves it to the front rather than adding
	// a duplicate.
	m.startSearch()
	typeSearch(m, "foo")
	m.commitSearch()
	if want := []string{"foo", "hello", "bar"}; !reflect.DeepEqual(
		m.searchHistory, want) {
		t.Errorf("after a repeat, history = %v, want %v", m.searchHistory, want)
	}

	// Up and down browse it, and the cursor lands at the end of each
	// recalled query so that typing continues it.
	m.startSearch()
	typeSearch(m, "dr")
	for _, tc := range []struct{ key, want string }{
		{"up", "foo"},
		{"up", "hello"},
		{"down", "foo"},
		// Back past the newest entry restores the stashed draft.
		{"down", "dr"},
	} {
		pressKeys(m, tc.key)
		if m.searchText != tc.want {
			t.Fatalf("%s gave searchText %q, want %q",
				tc.key, m.searchText, tc.want)
		}
		if want := runeLen(m.searchText); m.searchCursor != want {
			t.Errorf("searchCursor after %s = %d, want %d (end of %q)",
				tc.key, m.searchCursor, want, m.searchText)
		}
	}
	if m.searchHistoryIdx != -1 {
		t.Errorf("searchHistoryIdx = %d back at the draft, want -1",
			m.searchHistoryIdx)
	}

	// n with no search running recalls the newest query; N recalls it
	// but lands on the last match instead of the first. Browsing
	// scrolled the view, and a recall resumes from where the view is,
	// so put it back at the top to pin the first/last distinction.
	m.clearSearch()
	m.viewportOffset = 0
	m.recallLastSearch(false)
	if m.searchText != "foo" || len(m.searchOrder) != 3 || m.searchIdx != 0 {
		t.Errorf("n recalled %q with %d matches at idx %d, want foo, 3, 0",
			m.searchText, len(m.searchOrder), m.searchIdx)
	}
	m.clearSearch()
	m.recallLastSearch(true)
	if m.searchIdx != 2 {
		t.Errorf("N landed on match %d, want the last one", m.searchIdx)
	}
}

func TestSearchHistoryIsDeduplicatedAndCapped(t *testing.T) {
	d := searchHistoryDB(t)
	m := &Model{db: d}
	m.loadSearchHistory()
	for i := 0; i < searchHistoryMax+20; i++ {
		m.startSearch()
		typeSearch(m, fmt.Sprintf("q%d", i))
		m.commitSearch()
	}
	// Re-running an old query moves it to the front.
	rerun := fmt.Sprintf("q%d", searchHistoryMax+5)
	m.startSearch()
	typeSearch(m, rerun)
	m.commitSearch()

	restored := &Model{db: d}
	restored.loadSearchHistory()

	if len(restored.searchHistory) != searchHistoryMax {
		t.Errorf("restored %d entries, want %d",
			len(restored.searchHistory), searchHistoryMax)
	}
	if got := restored.searchHistory[0]; got != rerun {
		t.Errorf("newest entry = %q, want the re-run query %q", got, rerun)
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

func TestSearchHistoryPersists(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seed    string
		noDB    bool
		queries []string
		want    []string
	}{
		{name: "across sessions",
			queries: []string{"alpha", "beta", "gamma"},
			want:    []string{"gamma", "beta", "alpha"}},
		{name: "an unreadable stored value is discarded, not fatal",
			seed: "{not json", queries: []string{"alpha"},
			want: []string{"alpha"}},
		// The views are usable without a database, in tests and in
		// NewModelWithData; history just stays in memory.
		{name: "without a database", noDB: true,
			queries: []string{"alpha"}, want: []string{"alpha"}},
		{name: "awkward queries round-trip",
			queries: []string{`needle`, `"quoted"`, `back\slash`,
				`a|b.*[0-9]+`, `ünïcödé ☕`, `  leading and trailing  `,
				"tab\tseparated"},
			want: []string{"tab\tseparated", `  leading and trailing  `,
				`ünïcödé ☕`, `a|b.*[0-9]+`, `back\slash`, `"quoted"`,
				`needle`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			if !tc.noDB {
				d := searchHistoryDB(t)
				if tc.seed != "" {
					d.SetSyncState(searchHistoryKey, tc.seed)
				}
				m.db = d
			}
			m.loadSearchHistory()
			if len(m.searchHistory) != 0 {
				t.Fatalf("a fresh model should have no history, got %v",
					m.searchHistory)
			}
			for _, q := range tc.queries {
				m.searchText = q
				m.commitSearch()
			}

			// A second model over the same database, as a relaunch.
			restored := &Model{db: m.db}
			restored.loadSearchHistory()
			if tc.noDB {
				restored = m
			}
			if !reflect.DeepEqual(restored.searchHistory, tc.want) {
				t.Errorf("restored = %q\n    want = %q",
					restored.searchHistory, tc.want)
			}
		})
	}
}

func TestSearchHistorySurvivesDatabaseReopen(t *testing.T) {
	// The in-memory tests share one connection. This is the real case:
	// quit leadlight and start it again in the same directory.
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

// ---------------------------------------------------------------
// Queries that find nothing
// ---------------------------------------------------------------

func TestSearchWithNoMatchesIsDroppedOnCommit(t *testing.T) {
	bright, desc, sep := (&Model{}).helpStyles()
	m := searchModel("alpha\nbeta\n", "")
	m.startSearch()
	typeSearch(m, "nomatch")

	// While still typing, the prompt reports the failure.
	if got := m.renderSearchInputHelp(bright, desc, sep); !strings.Contains(
		got, "[no matches]") {
		t.Errorf("prompt = %q, want it to report no matches", got)
	}

	// Committing it leaves the search entirely: there is nothing to
	// step through, so staying in it would offer an n and N that do
	// nothing and make q take two presses to leave.
	pressKeys(m, "enter")
	if m.searchInput || m.searchText != "" || m.searchRegex != nil ||
		m.searchIdx != -1 {
		t.Errorf("search not dropped: input=%v text=%q regex=%v idx=%d",
			m.searchInput, m.searchText, m.searchRegex != nil, m.searchIdx)
	}

	// So the help bar offers only "/", with no n/N and no counter.
	got := m.renderSearchKeyHelp(bright, desc, sep)
	if strings.Contains(got, "n/N") || strings.Contains(got, "[0]") ||
		!strings.Contains(got, "/") {
		t.Errorf("help = %q, want only / with no n/N and no zero counter", got)
	}

	// Dropping the search must not lose the query: a typo is exactly
	// what you want to recall and correct.
	if want := []string{"nomatch"}; !reflect.DeepEqual(m.searchHistory, want) {
		t.Errorf("history = %v, want %v", m.searchHistory, want)
	}

	// And q leaves the view at once, with no search to clear first.
	m.handleViewportKey(keyRunes("q"))
	if m.viewMode != viewTable {
		t.Errorf("q did not leave the view, mode = %d", m.viewMode)
	}
}

func TestSearchRecallWithNoMatchesIsDropped(t *testing.T) {
	// n recalls the last query; if it no longer matches anything in
	// this content, it should not leave a dead search behind either.
	m := patchViewWithSearch("needle", "alpha needle")
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

func TestSearchReportsNoResultsOnCommit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  string
	}{
		// Nothing is said while the query is still being typed: every
		// prefix of a real query is briefly matchless, and the prompt
		// already shows a live "[no matches]" hint.
		{"a query that finds nothing", "zzz", "no results"},
		// A pattern that will not compile is reported the same way,
		// rather than quietly searching for its characters.
		{"a pattern that will not compile", "*.c", "no results"},
		{"a query that matches", "alpha", ""},
		{"an empty query is not a failed search", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := searchModel("alpha\nbeta\n", "")
			m.Status = status.NewRegistry(nil)

			m.startSearch()
			typeSearch(m, tc.query)
			if msg, _ := m.Status.Active(); msg != "" {
				t.Errorf("status while typing = %q, want nothing", msg)
			}

			pressKeys(m, "enter")
			msg, _ := m.Status.Active()
			if tc.want == "" {
				if msg != "" {
					t.Errorf("status = %q, want nothing", msg)
				}
				return
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("status = %q, want it to contain %q", msg, tc.want)
			}
			if !strings.Contains(msg, tc.query) {
				t.Errorf("status = %q, want the query quoted back", msg)
			}
			if len(m.searchOrder) != 0 || m.searchText != "" {
				t.Error("a failed search should leave nothing behind")
			}
			// It has to clear on its own; the registry prunes on read
			// and the model schedules a redraw at NextExpiry.
			if d := m.Status.NextExpiry(); d <= 0 || d > searchReportFor {
				t.Errorf("NextExpiry = %v, want it within %v", d, searchReportFor)
			}
		})
	}
}
