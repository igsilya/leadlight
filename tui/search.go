// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) startSearch() {
	m.searching = true
	m.searchText = ""
	m.searchMatches = nil
	m.searchIdx = -1
}

func (m *Model) clearSearch() {
	m.searching = false
	m.searchText = ""
	m.searchRegex = nil
	m.searchMatches = nil
	m.searchIdx = -1
}

func (m *Model) commitSearch() {
	m.searching = false
	if len(m.searchMatches) > 0 && m.searchIdx >= 0 {
		m.scrollToMatch(m.searchIdx, m.searchMaxLines())
	}
}

func (m *Model) handleSearchInputMode(msg tea.KeyMsg, updateMatches func()) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter":
		m.commitSearch()
	case "esc":
		m.clearSearch()
	case "backspace":
		if len(m.searchText) > 0 {
			m.searchText = m.searchText[:len(m.searchText)-1]
			updateMatches()
		} else {
			m.clearSearch()
		}
	default:
		if msg.Paste {
			for _, r := range msg.Runes {
				if r >= ' ' {
					m.searchText += string(r)
				}
			}
			updateMatches()
		} else if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
			m.searchText += key
			updateMatches()
		}
	}
	return m, nil
}

func (m *Model) handleSearchNavigation(key string) (handled bool) {
	switch key {
	case "/":
		m.startSearch()
		return true
	case "n":
		if len(m.searchMatches) > 0 {
			m.nextSearchMatch()
		}
		return true
	case "N":
		if len(m.searchMatches) > 0 {
			m.prevSearchMatch()
		}
		return true
	}
	return false
}

// searchMaxLines returns the total line count for the current view mode.
func (m *Model) searchMaxLines() int {
	if m.viewMode == viewCompare {
		return m.compareMaxLines()
	}
	return len(m.viewportLines)
}

func (m *Model) compileSearchPattern() (*regexp.Regexp, bool) {
	if m.searchText == "" {
		m.searchMatches = nil
		m.searchIdx = -1
		m.searchRegex = nil
		return nil, false
	}

	re, err := parseVimSearchPattern(m.searchText)
	if err != nil || re == nil {
		m.searchMatches = nil
		m.searchIdx = -1
		m.searchRegex = nil
		return nil, false
	}

	m.searchRegex = re
	m.searchMatches = nil
	return re, true
}

func (m *Model) collectLineMatches(re *regexp.Regexp, lineIdx int, text string) {
	plain := stripANSI(text)
	for _, pos := range re.FindAllStringIndex(plain, -1) {
		m.searchMatches = append(m.searchMatches, searchMatch{
			lineIdx: lineIdx,
			start:   pos[0],
			end:     pos[1],
		})
	}
}

func (m *Model) updateViewportSearchMatches() {
	re, ok := m.compileSearchPattern()
	if !ok {
		return
	}

	for i, line := range m.viewportLines {
		m.collectLineMatches(re, i, line)
	}

	m.finalizeSearchMatches()
}

func (m *Model) updateCompareSearchMatches() {
	re, ok := m.compileSearchPattern()
	if !ok {
		return
	}

	maxLines := m.compareMaxLines()
	for i := 0; i < maxLines; i++ {
		if i < len(m.compare[0].lines) {
			m.collectLineMatches(re, i, m.compare[0].lines[i])
		}
		if i < len(m.compare[1].lines) {
			m.collectLineMatches(re, i, m.compare[1].lines[i])
		}
	}

	m.finalizeSearchMatches()
}

func (m *Model) finalizeSearchMatches() {
	if len(m.searchMatches) > 0 {
		m.searchIdx = 0
		m.scrollToMatch(0, m.searchMaxLines())
	} else {
		m.searchIdx = -1
	}
}

func (m *Model) nextSearchMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	m.searchIdx = (m.searchIdx + 1) % len(m.searchMatches)
	m.scrollToMatch(m.searchIdx, m.searchMaxLines())
}

func (m *Model) prevSearchMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	m.searchIdx--
	if m.searchIdx < 0 {
		m.searchIdx = len(m.searchMatches) - 1
	}
	m.scrollToMatch(m.searchIdx, m.searchMaxLines())
}

func (m *Model) scrollToMatch(matchIdx int, maxLines int) {
	if matchIdx < 0 || matchIdx >= len(m.searchMatches) {
		return
	}
	lineIdx := m.searchMatches[matchIdx].lineIdx
	visible := m.viewportVisibleLines()

	m.viewportOffset = lineIdx - visible/2
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
	maxOffset := maxLines - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.viewportOffset > maxOffset {
		m.viewportOffset = maxOffset
	}
}

// parseVimSearchPattern parses a vim-style search pattern and returns a compiled regex.
// Supports \c for case-insensitive, \C for case-sensitive (default), and standard regex.
func parseVimSearchPattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}

	caseInsensitive := false
	cleanPattern := pattern

	if strings.Contains(pattern, `\c`) {
		caseInsensitive = true
		cleanPattern = strings.ReplaceAll(cleanPattern, `\c`, "")
	}
	if strings.Contains(pattern, `\C`) {
		caseInsensitive = false
		cleanPattern = strings.ReplaceAll(cleanPattern, `\C`, "")
	}

	cleanPattern = strings.TrimSpace(cleanPattern)
	if cleanPattern == "" {
		return nil, nil
	}

	regexPattern := cleanPattern
	if caseInsensitive {
		regexPattern = "(?i)" + regexPattern
	}

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		literal := regexp.QuoteMeta(cleanPattern)
		if caseInsensitive {
			literal = "(?i)" + literal
		}
		re, err = regexp.Compile(literal)
	}

	return re, err
}

// highlightLineIfMatched applies search highlighting to a line if it contains matches.
func (m *Model) highlightLineIfMatched(line string, lineIdx int) string {
	if m.searchRegex == nil {
		return line
	}

	hasMatch := false
	for _, match := range m.searchMatches {
		if match.lineIdx == lineIdx {
			hasMatch = true
			break
		}
	}
	if !hasMatch {
		return line
	}

	var currentMatchPos *searchMatch
	if m.searchIdx >= 0 && m.searchIdx < len(m.searchMatches) {
		if m.searchMatches[m.searchIdx].lineIdx == lineIdx {
			currentMatchPos = &m.searchMatches[m.searchIdx]
		}
	}

	return highlightSearchMatch(line, m.searchRegex, currentMatchPos)
}

// highlightSearchMatch highlights regex matches in a line while preserving ANSI codes.
func highlightSearchMatch(line string, re *regexp.Regexp, currentMatch *searchMatch) string {
	if re == nil {
		return line
	}

	plainLine := stripANSI(line)
	allMatches := re.FindAllStringIndex(plainLine, -1)
	if len(allMatches) == 0 {
		return line
	}

	var result strings.Builder
	plainIdx := 0
	lineIdx := 0
	matchIdx := 0

	for lineIdx < len(line) {
		ch := line[lineIdx]

		if ch == '\x1b' {
			end := skipANSIEscape(line, lineIdx)
			result.WriteString(line[lineIdx:end])
			lineIdx = end
			continue
		}

		if matchIdx < len(allMatches) && plainIdx == allMatches[matchIdx][0] {
			matchEnd := allMatches[matchIdx][1]

			style := searchOtherStyle
			if currentMatch != nil &&
				currentMatch.start == allMatches[matchIdx][0] &&
				currentMatch.end == allMatches[matchIdx][1] {
				style = searchCurrentStyle
			}

			plainText := extractPlainSpan(line, &lineIdx, &plainIdx, matchEnd)
			result.WriteString(style.Render(plainText))
			matchIdx++
			continue
		}

		result.WriteByte(ch)
		plainIdx++
		lineIdx++
	}

	return result.String()
}

// extractPlainSpan advances through the styled line collecting plain text
// until plainIdx reaches plainEnd, skipping ANSI escapes.
func extractPlainSpan(line string, lineIdx *int, plainIdx *int, plainEnd int) string {
	var buf strings.Builder
	for *plainIdx < plainEnd && *lineIdx < len(line) {
		ch := line[*lineIdx]
		if ch == '\x1b' {
			*lineIdx = skipANSIEscape(line, *lineIdx)
			continue
		}
		buf.WriteByte(ch)
		*lineIdx++
		*plainIdx++
	}
	return buf.String()
}

// skipANSIEscape returns the index past the end of an ANSI escape starting at pos.
func skipANSIEscape(s string, pos int) int {
	i := pos + 1
	for i < len(s) {
		ch := s[i]
		i++
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			return i
		}
	}
	return i
}

func (m *Model) renderSearchInputHelp(bright, desc, sep lipgloss.Style) string {
	var hb strings.Builder
	hb.WriteString(desc.Render("Search: "))
	hb.WriteString(normalOptionStyle.Render(m.searchText + "_"))
	if len(m.searchMatches) > 0 {
		hb.WriteString(desc.Render(fmt.Sprintf(" [%d/%d]",
			m.searchIdx+1, len(m.searchMatches))))
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
	if m.searchText != "" {
		hb.WriteString(helpKey(bright, desc, "/", "search"))
		hb.WriteString(helpSepStr(sep))
		hb.WriteString(helpKey(bright, desc, "n/N", "next/prev"))
		hb.WriteString(sep.Render(fmt.Sprintf(" [%d]", len(m.searchMatches))))
	} else {
		hb.WriteString(helpKey(bright, desc, "/", "search"))
	}
	return hb.String()
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
