// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"leadlight/status"
)

// searchHistoryMax caps the number of remembered queries. The list is
// only ever walked one entry at a time, so a long tail is of no use to
// anyone and would just sit in the database.
const searchHistoryMax = 50

// searchHistoryKey is where the history lives in the sync_state table.
// It is stored in the project database rather than in the config,
// which leadlight only ever reads, and which is shared between
// projects where searches would mean little.
const searchHistoryKey = "search_history"

// loadSearchHistory restores the history saved by a previous session.
// A value that cannot be read is dropped rather than reported: an
// unusable history is not worth interrupting startup over.
func (m *Model) loadSearchHistory() {
	m.searchHistory = nil
	if m.db == nil {
		return
	}
	stored := m.db.GetSyncState(searchHistoryKey)
	if stored == "" {
		return
	}
	var history []string
	if err := json.Unmarshal([]byte(stored), &history); err != nil {
		log.Printf("search: discarding unreadable history: %v", err)
		return
	}
	for _, q := range history {
		if q != "" && len(m.searchHistory) < searchHistoryMax {
			m.searchHistory = append(m.searchHistory, q)
		}
	}
}

// saveSearchHistory persists the history for the next session.
func (m *Model) saveSearchHistory() {
	if m.db == nil {
		return
	}
	encoded, err := json.Marshal(m.searchHistory)
	if err != nil {
		log.Printf("search: cannot save history: %v", err)
		return
	}
	m.db.SetSyncState(searchHistoryKey, string(encoded))
}

func (m *Model) startSearch() {
	m.searchInput = true
	m.searchOrigin = m.viewportOffset
	m.searchText = ""
	m.searchDraft = ""
	m.searchHistoryIdx = -1
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
	m.searchDraft = ""
	m.searchRegex = nil
	m.searchOrder = nil
	m.searchIdx = -1
	m.rescanSearch()
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
	m.searchDraft = ""
	if m.searchText != "" {
		// Deduplicate: remove previous occurrence so the latest is always at front.
		for i, h := range m.searchHistory {
			if h == m.searchText {
				m.searchHistory = append(m.searchHistory[:i], m.searchHistory[i+1:]...)
				break
			}
		}
		m.searchHistory = append([]string{m.searchText}, m.searchHistory...)
		if len(m.searchHistory) > searchHistoryMax {
			m.searchHistory = m.searchHistory[:searchHistoryMax]
		}
		m.saveSearchHistory()
	}
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

// recallLastSearch re-runs the most recent query, as vim does when n
// or N is pressed with no search active. backward puts the cursor on
// the last match rather than the first, so that N goes backwards from
// where the view is rather than jumping to the top.
func (m *Model) recallLastSearch(backward bool) {
	if len(m.searchHistory) == 0 {
		return
	}
	m.searchText = m.searchHistory[0]
	m.updateSearchMatches()
	if m.dropSearchIfUnmatched() {
		return
	}
	if backward && len(m.searchOrder) > 0 {
		m.searchIdx = len(m.searchOrder) - 1
		m.scrollToMatch(m.searchIdx)
	}
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

// updateSearchMatches recompiles the query and runs it over the
// current view, leaving the cursor on the first match.
func (m *Model) updateSearchMatches() {
	m.searchRegex = nil
	if m.searchText != "" {
		if re, err := parseVimSearchPattern(m.searchText); err == nil {
			m.searchRegex = re
		}
	}
	m.searchIdx = 0
	m.rescanSearch()
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

func (m *Model) renderSearchInputHelp(bright, desc, sep lipgloss.Style) string {
	var hb strings.Builder
	hb.WriteString(desc.Render("Search: "))
	hb.WriteString(normalOptionStyle.Render(m.searchText + "_"))
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
