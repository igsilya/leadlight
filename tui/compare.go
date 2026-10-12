// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"os"
	"os/exec"
	"strings"
)

type diffLineKind int

const (
	diffUnchanged diffLineKind = iota
	diffAdded                  // line only on this side (green bg)
	diffRemoved                // line only on the other side (red bg)
	diffPadding                // blank padding opposite an add/remove
)

type compareCacheEntry struct {
	docs [2]*renderedDoc
}

func (m *Model) buildCompareContent() {
	// Whatever path this takes, the matches have to be recomputed
	// against the documents it leaves behind.
	defer m.rescanSearch()

	colWidth := (m.width - 1) / 2 // 1 char for │ separator
	if colWidth < 20 {
		colWidth = 20
	}
	collapse := !m.viewExpanded

	var parsed [2]ParsedMbox
	var have [2]bool
	for i := range m.compare {
		parsed[i], have[i] = compareParsedMbox(
			m.compare[i].idx, m.compare[i].patches, m.compare[i].cover)
	}

	// Check the diff cache.
	cacheKey := [2]int{m.compare[0].idx, m.compare[1].idx}
	if m.compareDiffCache != nil {
		if entry, ok := m.compareDiffCache[cacheKey]; ok {
			for i := range m.compare {
				m.compare[i].doc = entry.docs[i]
			}
			return
		}
	}

	if !have[0] || !have[1] {
		// Fall back to non-diff rendering when content is missing.
		var parts [2][3]*renderedDoc
		for i := range m.compare {
			if have[i] {
				h, b, d := formatMboxDoc(parsed[i], colWidth, collapse)
				parts[i] = [3]*renderedDoc{h, b, d}
				continue
			}
			parts[i] = [3]*renderedDoc{
				docFromLines("(no content)"), {}, {},
			}
		}
		for s := 0; s < 3; s++ {
			padDocsToEqual(parts[0][s], parts[1][s])
		}
		for i := range m.compare {
			doc := &renderedDoc{}
			for _, part := range parts[i] {
				doc.appendDoc(part)
			}
			m.compare[i].doc = doc
		}
		return
	}

	// Diff each section on raw lines, then style with bg tint.
	sections := make([][2]*renderedDoc, 3)

	// Headers: compare raw values directly.
	sections[0] = m.diffHeaders(parsed, colWidth, collapse)

	// Body: diff raw lines, then wrap+style.
	leftBody := splitRawLines(replaceControlChars(parsed[0].Body))
	rightBody := splitRawLines(replaceControlChars(parsed[1].Body))
	sections[1] = diffAndStyle(leftBody, rightBody, colWidth, appendBodyLineDoc)

	// Diff section: diff raw lines, then wrap+style.
	leftDiff := splitRawLines(replaceControlChars(parsed[0].Diff))
	rightDiff := splitRawLines(replaceControlChars(parsed[1].Diff))
	sections[2] = diffAndStyle(leftDiff, rightDiff, colWidth, appendDiffLineDoc)

	// Pad headers and body for section alignment.
	padDocsToEqual(sections[0][0], sections[0][1])
	padDocsToEqual(sections[1][0], sections[1][1])

	for i := range m.compare {
		doc := &renderedDoc{}
		for _, sec := range sections {
			doc.appendDoc(sec[i])
		}
		m.compare[i].doc = doc
	}

	// Cache the result.
	if m.compareDiffCache == nil {
		m.compareDiffCache = map[[2]int]*compareCacheEntry{}
	}
	m.compareDiffCache[cacheKey] = &compareCacheEntry{
		docs: [2]*renderedDoc{m.compare[0].doc, m.compare[1].doc},
	}
}

func compareParsedMbox(
	idx int, patches []comparePatch, cover *ParsedMbox,
) (ParsedMbox, bool) {
	switch {
	case idx == -1 && cover != nil:
		return *cover, true
	case idx >= 0 && idx < len(patches):
		return patches[idx].parsed, true
	}
	return ParsedMbox{}, false
}

// diffHeaders compares header values directly and renders with bg tint.
func (m *Model) diffHeaders(
	parsed [2]ParsedMbox, width int, collapse bool,
) [2]*renderedDoc {
	labelWidth := headerLabelWidth
	valWidth := width - labelWidth
	if valWidth < 20 {
		valWidth = 20
	}

	type hdr struct {
		label string
		vals  [2]string
		coll  bool // collapsible
	}
	headers := []hdr{
		{"Subject: ", [2]string{parsed[0].Subject, parsed[1].Subject}, false},
		{"From:    ", [2]string{parsed[0].From, parsed[1].From}, false},
		{"To:      ", [2]string{parsed[0].To, parsed[1].To}, collapse},
		{"Cc:      ", [2]string{parsed[0].Cc, parsed[1].Cc}, collapse},
		{"Date:    ", [2]string{parsed[0].Date, parsed[1].Date}, false},
	}

	docs := [2]*renderedDoc{{}, {}}
	for _, h := range headers {
		changed := h.vals[0] != h.vals[1]
		for i := 0; i < 2; i++ {
			kind := diffUnchanged
			if changed {
				if h.vals[i] == "" {
					kind = diffPadding
				} else if h.vals[1-i] == "" {
					kind = diffAdded
				} else if i == 0 {
					kind = diffRemoved
				} else {
					kind = diffAdded
				}
			}
			if h.vals[i] == "" {
				if kind == diffPadding {
					docs[i].addPaddingMarker()
				}
				continue
			}
			appendHeaderDoc(docs[i], h.label, h.vals[i], h.coll,
				labelWidth, valWidth, kind)
		}
	}
	return docs
}

// splitRawLines splits text into lines, returning nil for empty text.
func splitRawLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// runCompareDiff runs diff on two sets of lines and returns a
// classification string where each character is 'U' (unchanged),
// 'O' (old/left only), or 'N' (new/right only).
func runCompareDiff(left, right []string) string {
	if len(left) == 0 && len(right) == 0 {
		return ""
	}
	if len(left) == 0 {
		return strings.Repeat("N", len(right))
	}
	if len(right) == 0 {
		return strings.Repeat("O", len(left))
	}

	if _, err := exec.LookPath("diff"); err != nil {
		// No diff binary — treat everything as unchanged.
		return ""
	}

	tmpL, err := os.CreateTemp("", "leadlight-diff-l-*")
	if err != nil {
		return ""
	}
	defer os.Remove(tmpL.Name())
	tmpR, err := os.CreateTemp("", "leadlight-diff-r-*")
	if err != nil {
		tmpL.Close()
		return ""
	}
	defer os.Remove(tmpR.Name())

	for _, l := range left {
		tmpL.WriteString(l)
		tmpL.WriteString("\n")
	}
	tmpL.Close()
	for _, l := range right {
		tmpR.WriteString(l)
		tmpR.WriteString("\n")
	}
	tmpR.Close()

	cmd := exec.Command("diff",
		"--old-line-format=O", "--new-line-format=N",
		"--unchanged-line-format=U",
		tmpL.Name(), tmpR.Name())
	out, _ := cmd.Output()
	// diff exits 1 when files differ — not an error for us.
	return string(out)
}

// alignDiffLines walks a classification string and produces aligned
// line arrays. Consecutive O/N blocks are zipped side by side so
// replacements appear on the same row. Padding is only added when
// one side of a change block has more lines than the other.
func alignDiffLines(
	left, right []string, classes string,
) (alignedL, alignedR []string, kindsL, kindsR []diffLineKind) {
	if classes == "" {
		// No diff result — show side by side with no highlighting.
		for len(left) < len(right) {
			left = append(left, "")
		}
		for len(right) < len(left) {
			right = append(right, "")
		}
		kindsL = make([]diffLineKind, len(left))
		kindsR = make([]diffLineKind, len(right))
		return left, right, kindsL, kindsR
	}
	li, ri := 0, 0
	i := 0
	for i < len(classes) {
		if classes[i] == 'U' {
			l, r := "", ""
			if li < len(left) {
				l = left[li]
			}
			if ri < len(right) {
				r = right[ri]
			}
			alignedL = append(alignedL, l)
			alignedR = append(alignedR, r)
			kindsL = append(kindsL, diffUnchanged)
			kindsR = append(kindsR, diffUnchanged)
			li++
			ri++
			i++
			continue
		}
		// Skip unexpected characters to avoid an infinite loop.
		if classes[i] != 'O' && classes[i] != 'N' {
			i++
			continue
		}
		// Collect consecutive O then N into a change block.
		var olds, news []string
		for i < len(classes) && classes[i] == 'O' {
			if li < len(left) {
				olds = append(olds, left[li])
				li++
			}
			i++
		}
		for i < len(classes) && classes[i] == 'N' {
			if ri < len(right) {
				news = append(news, right[ri])
				ri++
			}
			i++
		}
		// Zip old and new lines side by side.
		n := len(olds)
		if len(news) > n {
			n = len(news)
		}
		for j := 0; j < n; j++ {
			if j < len(olds) && j < len(news) {
				alignedL = append(alignedL, olds[j])
				alignedR = append(alignedR, news[j])
				kindsL = append(kindsL, diffRemoved)
				kindsR = append(kindsR, diffAdded)
			} else if j < len(olds) {
				alignedL = append(alignedL, olds[j])
				alignedR = append(alignedR, "")
				kindsL = append(kindsL, diffRemoved)
				kindsR = append(kindsR, diffPadding)
			} else {
				alignedL = append(alignedL, "")
				alignedR = append(alignedR, news[j])
				kindsL = append(kindsL, diffPadding)
				kindsR = append(kindsR, diffAdded)
			}
		}
	}
	return
}

// lineStyler appends one raw line to a document, wrapped and styled.
type lineStyler func(d *renderedDoc, line string, width int, kind diffLineKind)

// diffAndStyle diffs raw lines and produces styled output with
// background tinting for changed lines. The styleFn wraps and styles
// each raw line (e.g. appendBodyLineDoc or appendDiffLineDoc).
// When wrapping produces different row counts on each side, the
// shorter side is padded so both stay vertically aligned.
func diffAndStyle(
	leftRaw, rightRaw []string, width int, styleFn lineStyler,
) [2]*renderedDoc {
	docs := [2]*renderedDoc{{}, {}}
	if len(leftRaw) == 0 && len(rightRaw) == 0 {
		return docs
	}
	// Leading blank line to match formatMboxDoc.
	for i := 0; i < 2; i++ {
		docs[i].addBlank()
	}

	classes := runCompareDiff(leftRaw, rightRaw)
	alignedL, alignedR, kindsL, kindsR :=
		alignDiffLines(leftRaw, rightRaw, classes)

	for idx := range alignedL {
		raws := [2]string{alignedL[idx], alignedR[idx]}
		ks := [2]diffLineKind{kindsL[idx], kindsR[idx]}

		var added [2]int
		for i := 0; i < 2; i++ {
			before := docs[i].Len()
			if ks[i] == diffPadding {
				docs[i].addPaddingMarker()
			} else {
				styleFn(docs[i], raws[i], width, ks[i])
			}
			added[i] = docs[i].Len() - before
		}
		// Pad the shorter side so both produce equal rendered rows
		// for this raw line pair (wrapping alignment). The filler
		// keeps the tint of the side it belongs to.
		for ; added[0] < added[1]; added[0]++ {
			docs[0].addPad(ks[0])
		}
		for ; added[1] < added[0]; added[1]++ {
			docs[1].addPad(ks[1])
		}
	}
	return docs
}
