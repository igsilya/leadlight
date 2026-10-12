// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"leadlight/status"
)

func (m *Model) startSearch() {
	m.searchInput = true
	m.searchOrigin = m.viewportOffset
	m.searchText = ""
	m.searchCursor = 0
	m.searchRegex = nil
	m.searchOrder = nil
	m.searchIdx = -1
	m.rescanSearch()
}

// cancelSearch abandons the query being typed. Searching is
// incremental, so the viewport has been scrolling along with it; put
// it back where the prompt found it.
func (m *Model) cancelSearch() {
	m.viewportOffset = m.searchOrigin
	m.clearSearch()
}

func (m *Model) clearSearch() {
	m.searchInput = false
	m.searchText = ""
	m.searchCursor = 0
	m.searchRegex = nil
	m.searchOrder = nil
	m.searchIdx = -1
	m.rescanSearch()
}

// The prompt edits its query by rune index, the same unit the rest of
// the search counts in. These four do the index arithmetic, clamping
// their own arguments so that no caller has to: a key pressed at the
// end of its reach asks for an empty range and gets the text back
// unchanged.

// runeLen is the length of s in runes, the unit the cursor counts in.
func runeLen(s string) int { return utf8.RuneCountInString(s) }

// insertAt splices ins into s at rune index cur and returns the text
// with the cursor just past what was inserted.
func insertAt(s string, cur int, ins string) (string, int) {
	r := []rune(s)
	cur = min(max(cur, 0), len(r))
	out := make([]rune, 0, len(r)+runeLen(ins))
	out = append(out, r[:cur]...)
	out = append(out, []rune(ins)...)
	out = append(out, r[cur:]...)
	return string(out), cur + runeLen(ins)
}

// deleteRange removes the runes in [from,to) and leaves the cursor
// where the removed text began.
func deleteRange(s string, from, to int) (string, int) {
	r := []rune(s)
	from = min(max(from, 0), len(r))
	to = min(max(to, 0), len(r))
	if from >= to {
		return s, from
	}
	return string(append(append([]rune{}, r[:from]...), r[to:]...)), from
}

// wordStart is where the whitespace-delimited word before cur begins:
// the run of spaces just before the cursor, then the run of non-spaces
// before them. This is readline's unix-word-rubout, which takes a
// whole "\.c$" off the end of a pattern in one press rather than
// stopping at the punctuation.
func wordStart(s string, cur int) int {
	r := []rune(s)
	i := min(max(cur, 0), len(r))
	for i > 0 && unicode.IsSpace(r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(r[i-1]) {
		i--
	}
	return i
}

// wordEnd is where the word after cur ends, the mirror of wordStart:
// the run of spaces at the cursor, then the run of non-spaces after
// them.
func wordEnd(s string, cur int) int {
	r := []rune(s)
	i := min(max(cur, 0), len(r))
	for i < len(r) && unicode.IsSpace(r[i]) {
		i++
	}
	for i < len(r) && !unicode.IsSpace(r[i]) {
		i++
	}
	return i
}

// editSearchText applies an edit to the query. An edit that changes
// nothing is dropped: updateSearchMatches puts the match cursor back
// on the first match and scrolls there, so a ctrl+w at the start of
// the line must not yank the view away from the match being read.
func (m *Model) editSearchText(text string, cursor int) {
	m.searchCursor = cursor
	if text == m.searchText {
		return
	}
	m.searchText = text
	m.updateSearchMatches()
}

// moveSearchCursor moves the insertion point without touching the
// query, so it neither recompiles the pattern nor scrolls the view.
func (m *Model) moveSearchCursor(to int) {
	m.searchCursor = min(max(to, 0), runeLen(m.searchText))
}

// searchReportFor is how long the "no results" message stays up, and
// searchReportMax how much of the query it quotes back.
const (
	searchReportFor = 3 * time.Second
	searchReportMax = 40
)

// reportNoResults puts a message on the status line for a few seconds
// when a committed query came back empty. A pattern that would not
// compile lands here too: the search found nothing either way, and
// the query is a regular expression whose syntax is the user's to
// get right.
func (m *Model) reportNoResults(query string) {
	if query == "" || m.Status == nil {
		return
	}
	m.Status.SetTimed(status.Search,
		fmt.Sprintf("Search returned no results: %q",
			truncateLine(query, searchReportMax)),
		searchReportFor)
}

func (m *Model) commitSearch() {
	m.searchInput = false
	query := m.searchText
	if m.dropSearchIfUnmatched() {
		// Nothing was found, so the incremental scrolling done while
		// the query was typed is undone too.
		m.viewportOffset = m.searchOrigin
		m.reportNoResults(query)
		return
	}
	m.scrollToMatch(m.searchIdx)
}

// dropSearchIfUnmatched leaves the search entirely when the query it
// was given found nothing, and reports whether it did. There is
// nothing to step through or highlight, so staying in it would only
// offer an n and N that do nothing and make q take two presses to
// leave the view. The query is in the history either way, so it can
// still be recalled and corrected.
func (m *Model) dropSearchIfUnmatched() bool {
	if len(m.searchOrder) > 0 {
		return false
	}
	m.clearSearch()
	return true
}

// searchMatcher holds every match of one pattern in one document.
// Matches are in logical coordinates: a rune range inside one of the
// document's unwrapped source lines. Those coordinates survive a
// resize, because re-wrapping changes the rows but not the source.
type searchMatcher struct {
	matches []searchMatch
	byLine  map[int][]searchMatch
}

func (s *searchMatcher) reset() {
	s.matches = nil
	s.byLine = nil
}

// scan finds every match of re in the document's source lines.
func (s *searchMatcher) scan(d *renderedDoc, re *regexp.Regexp) {
	s.reset()
	if d == nil || re == nil {
		return
	}
	for line, text := range d.logical {
		found := re.FindAllStringIndex(text, -1)
		if found == nil {
			continue
		}
		if s.byLine == nil {
			s.byLine = map[int][]searchMatch{}
		}
		for _, pos := range found {
			if pos[0] == pos[1] {
				// A zero-width match, as "x*" or "^" produces on
				// every position. There is nothing to highlight and
				// nothing to step to, and counting them would bury
				// the real matches.
				continue
			}
			// regexp reports byte offsets; everything downstream
			// counts in runes.
			match := searchMatch{
				line:  line,
				start: utf8.RuneCountInString(text[:pos[0]]),
				end:   utf8.RuneCountInString(text[:pos[1]]),
			}
			s.matches = append(s.matches, match)
			s.byLine[line] = append(s.byLine[line], match)
		}
	}
}

// onLine returns the matches on one source line, in order.
func (s *searchMatcher) onLine(line int) []searchMatch {
	if s == nil || line < 0 {
		return nil
	}
	return s.byLine[line]
}

// searchTarget is one document the current view searches.
type searchTarget struct {
	doc *renderedDoc
	mt  *searchMatcher
}

// searchTargets returns the documents the current view searches, in
// the order their columns appear on screen. This is the only place
// that has to know the difference between the two views.
func (m *Model) searchTargets() []searchTarget {
	if m.viewMode == viewCompare {
		return []searchTarget{
			{m.compare[0].doc, &m.compare[0].matcher},
			{m.compare[1].doc, &m.compare[1].matcher},
		}
	}
	return []searchTarget{{m.viewportDoc, &m.viewportMatcher}}
}

// matchRef points at one match across all of the view's documents.
// The refs are ordered the way the eye reads them: down the screen,
// and left column before right on the same row.
type matchRef struct {
	target int
	idx    int
	row    int
}

// rescanSearch recomputes the matches for the active query. It is
// called whenever the content changes, so that a document can never
// be displayed alongside matches that were found in an older one.
// The cursor keeps its place when it still points at a match, so
// that a resize does not lose track of which match you were on.
// Scrolling to it again is the caller's business, since a rebuild
// that replaces the content wants the top of it instead.
func (m *Model) rescanSearch() {
	targets := m.searchTargets()
	m.searchOrder = nil

	for ti, t := range targets {
		t.mt.scan(t.doc, m.searchRegex)
		for i, match := range t.mt.matches {
			m.searchOrder = append(m.searchOrder, matchRef{
				target: ti,
				idx:    i,
				row:    t.doc.rowOf(match.line, match.start),
			})
		}
	}
	sort.SliceStable(m.searchOrder, func(a, b int) bool {
		x, y := m.searchOrder[a], m.searchOrder[b]
		if x.row != y.row {
			return x.row < y.row
		}
		return x.target < y.target
	})

	if len(m.searchOrder) == 0 {
		m.searchIdx = -1
		return
	}
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		m.searchIdx = 0
	}
}

// matchFrom is the index of the first match at or after row, or 0
// when every match is above it. searchOrder is kept sorted by row,
// so this is a binary search.
func (m *Model) matchFrom(row int) int {
	if len(m.searchOrder) == 0 {
		return -1
	}
	i := sort.Search(len(m.searchOrder), func(i int) bool {
		return m.searchOrder[i].row >= row
	})
	if i == len(m.searchOrder) {
		// Everything matched above the starting point; wrap.
		return 0
	}
	return i
}

// updateSearchMatches recompiles the query and runs it over the
// current view, leaving the cursor on the first match at or after
// where the search started from. A search begun partway down a
// document runs to the end and then wraps to the top, rather than
// yanking the reader back to the first match in the file.
func (m *Model) updateSearchMatches() {
	m.searchRegex = nil
	if m.searchText != "" {
		if re, err := parseVimSearchPattern(m.searchText); err == nil {
			m.searchRegex = re
		}
	}
	m.searchIdx = 0
	m.rescanSearch()
	m.searchIdx = m.matchFrom(m.searchOrigin)
	m.scrollToMatch(m.searchIdx)
}

// currentMatch returns the matcher for one of the view's documents,
// and the match the cursor is on when it falls in that document.
func (m *Model) currentMatch(target int) (*searchMatcher, *searchMatch) {
	targets := m.searchTargets()
	if target < 0 || target >= len(targets) {
		return nil, nil
	}
	mt := targets[target].mt
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchOrder) {
		return mt, nil
	}
	ref := m.searchOrder[m.searchIdx]
	if ref.target != target || ref.idx >= len(mt.matches) {
		return mt, nil
	}
	cur := mt.matches[ref.idx]
	return mt, &cur
}

// searchMaxLines is the number of rows the current view can scroll.
func (m *Model) searchMaxLines() int {
	n := 0
	for _, t := range m.searchTargets() {
		n = max(n, t.doc.Len())
	}
	return n
}

func (m *Model) nextSearchMatch() {
	if len(m.searchOrder) == 0 {
		return
	}
	m.searchIdx = (m.searchIdx + 1) % len(m.searchOrder)
	m.scrollToMatch(m.searchIdx)
}

func (m *Model) prevSearchMatch() {
	if len(m.searchOrder) == 0 {
		return
	}
	m.searchIdx--
	if m.searchIdx < 0 {
		m.searchIdx = len(m.searchOrder) - 1
	}
	m.scrollToMatch(m.searchIdx)
}

// scrollToMatch centres the viewport on a match. Because the ref
// records the row the match actually lands on, a match deep inside a
// wrapped line scrolls to the row showing it rather than to the top
// of the line.
func (m *Model) scrollToMatch(idx int) {
	if idx < 0 || idx >= len(m.searchOrder) {
		return
	}
	visible := m.viewportVisibleLines()
	m.viewportOffset = m.searchOrder[idx].row - visible/2
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
	maxOffset := m.searchMaxLines() - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.viewportOffset > maxOffset {
		m.viewportOffset = maxOffset
	}
}

// renderDocRow renders one row with the search highlight applied. The
// highlight is a different style on part of a span, so no escape
// sequence is ever inspected or edited.
func renderDocRow(
	d *renderedDoc, row int, mt *searchMatcher, cur *searchMatch,
) string {
	if d == nil || row < 0 || row >= len(d.visual) {
		return ""
	}
	v := d.visual[row]
	matches := mt.onLine(v.logical)
	if len(matches) == 0 {
		return d.Line(row)
	}

	var b strings.Builder
	off := v.off
	for _, sp := range v.spans {
		if sp.synthetic {
			b.WriteString(sp.style.Render(sp.text))
			continue
		}
		runes := []rune(sp.text)
		pos := 0
		for _, match := range matches {
			start, end := match.start-off, match.end-off
			if end <= pos || start >= len(runes) {
				continue
			}
			start = max(start, pos)
			end = min(end, len(runes))
			if start > pos {
				b.WriteString(sp.style.Render(string(runes[pos:start])))
			}
			style := searchOtherStyle
			if cur != nil && *cur == match {
				style = searchCurrentStyle
			}
			b.WriteString(style.Render(string(runes[start:end])))
			pos = end
		}
		if pos < len(runes) {
			b.WriteString(sp.style.Render(string(runes[pos:])))
		}
		off += len(runes)
	}
	return b.String()
}

// parseVimSearchPattern parses a vim-style search pattern and returns a compiled regex.
// Supports \c for case-insensitive, \C for case-sensitive (default), and standard regex.
// A pattern that is not a valid regex is an error; the query is a
// regular expression and silently searching for it as literal text
// would answer a question the user did not ask.
func parseVimSearchPattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}

	cleanPattern, caseInsensitive := extractCaseFlags(pattern)
	if cleanPattern == "" {
		return nil, nil
	}

	prefix := ""
	if caseInsensitive {
		prefix = "(?i)"
	}

	return regexp.Compile(prefix + cleanPattern)
}

// extractCaseFlags strips the vim case flags \c and \C from a pattern and
// reports whether matching should be case-insensitive. Like vim, the flags
// may appear anywhere in the pattern and the last one wins. Only top-level
// flags count: an escaped backslash (\\c) and anything inside a bracket
// expression ([\c]) are left in the pattern untouched.
func extractCaseFlags(pattern string) (clean string, caseInsensitive bool) {
	var b strings.Builder
	b.Grow(len(pattern))

	inClass := false
	for i := 0; i < len(pattern); {
		c := pattern[i]
		if c == '\\' && i+1 < len(pattern) {
			if n := pattern[i+1]; !inClass && (n == 'c' || n == 'C') {
				caseInsensitive = n == 'c'
				i += 2
				continue
			}
			// Copy the escape as a unit so that a "\\" pair cannot
			// mask the character that follows it.
			b.WriteString(pattern[i : i+2])
			i += 2
			continue
		}
		switch {
		case c == '[' && !inClass:
			inClass = true
		case c == ']' && inClass:
			inClass = false
		}
		b.WriteByte(c)
		i++
	}

	return b.String(), caseInsensitive
}

// renderSearchQuery draws the query with a block cursor on the rune
// the cursor is on. Past the end of the text there is no rune to
// invert, so the cursor gets a space of its own; an underscore there
// could not be told apart from one typed into the pattern.
//
// The cursor is clamped rather than trusted: tests and any future
// caller that sets searchText directly leave it stale, and a stale
// cursor should draw oddly rather than panic.
func renderSearchQuery(text string, cursor int) string {
	r := []rune(text)
	cursor = min(max(cursor, 0), len(r))
	at, after := " ", ""
	if cursor < len(r) {
		at = string(r[cursor])
		after = string(r[cursor+1:])
	}
	var b strings.Builder
	if cursor > 0 {
		b.WriteString(normalOptionStyle.Render(string(r[:cursor])))
	}
	b.WriteString(searchCursorStyle.Render(at))
	if after != "" {
		b.WriteString(normalOptionStyle.Render(after))
	}
	return b.String()
}

func (m *Model) renderSearchInputHelp(bright, desc, sep lipgloss.Style) string {
	var hb strings.Builder
	hb.WriteString(desc.Render("Search: "))
	hb.WriteString(renderSearchQuery(m.searchText, m.searchCursor))
	if len(m.searchOrder) > 0 {
		hb.WriteString(desc.Render(fmt.Sprintf(" [%d/%d]",
			m.searchIdx+1, len(m.searchOrder))))
	} else if m.searchText != "" {
		hb.WriteString(desc.Render(" [no matches]"))
	}
	hb.WriteString(helpSepStr(sep))
	hb.WriteString(helpKey(bright, desc, "enter", "done"))
	hb.WriteString(helpSepStr(sep))
	hb.WriteString(helpKey(bright, desc, "esc", "cancel"))
	return hb.String()
}

func (m *Model) renderSearchKeyHelp(bright, desc, sep lipgloss.Style) string {
	var hb strings.Builder
	hb.WriteString(helpSepStr(sep))
	hb.WriteString(helpKey(bright, desc, "/", "search"))
	// Offer n and N only when there is something to step through.
	// A committed search that found nothing is dropped, but one can
	// also be left with no matches by the content changing under it.
	if len(m.searchOrder) > 0 {
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(helpKey(bright, desc, "n/N", "next/prev"))
		hb.WriteString(sep.Render(fmt.Sprintf(" [%d]", len(m.searchOrder))))
	}
	return hb.String()
}
