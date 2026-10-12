// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// styledSpan is a run of plain text rendered with a single style.
//
// Synthetic spans are chrome the formatter invented: wrap arrows,
// header indents, column padding. They occupy screen columns but do
// not correspond to anything in the source, so a search never matches
// inside them.
type styledSpan struct {
	text      string
	style     lipgloss.Style
	synthetic bool
}

func plainSpan(text string, style lipgloss.Style) styledSpan {
	return styledSpan{text: text, style: style}
}

func chromeSpan(text string, style lipgloss.Style) styledSpan {
	return styledSpan{text: text, style: style, synthetic: true}
}

// visualLine is one terminal row.
type visualLine struct {
	spans []styledSpan
	// logical is the index of the source line this row displays, or
	// -1 for a row that is pure chrome (a blank separator, alignment
	// padding in the compare view).
	logical int
	// off is the rune offset within that source line at which this
	// row's first non-synthetic span begins; -1 when there is none.
	off int
	// kind tints the row in the compare view; diffUnchanged elsewhere.
	kind diffLineKind
}

// renderedDoc is what a formatter produces: the logical text, meaning
// the lines as they read before wrapping and without any styling, plus
// the visual rows they were laid out into.
//
// Keeping both means a search can run against the logical text, where
// positions are stable across resizes and a match is never split by a
// wrap, while the renderer still knows which row to light up.
type renderedDoc struct {
	logical []string
	visual  []visualLine
	// first[i] is the index in visual of the first row showing
	// logical line i, or -1 if the line produced no rows.
	first []int
}

// addLogical records a source line and returns its index. The rows
// that display it are added afterwards with addRow.
func (d *renderedDoc) addLogical(text string) int {
	d.logical = append(d.logical, text)
	d.first = append(d.first, -1)
	return len(d.logical) - 1
}

// addRow appends a row showing the given source line from rune offset
// off onwards.
func (d *renderedDoc) addRow(logical, off int, spans ...styledSpan) {
	if logical >= 0 && logical < len(d.first) && d.first[logical] < 0 {
		d.first[logical] = len(d.visual)
	}
	d.visual = append(d.visual, visualLine{
		spans: spans, logical: logical, off: off,
	})
}

// addChrome appends a row that shows no source text.
func (d *renderedDoc) addChrome(spans ...styledSpan) {
	d.addRow(-1, -1, spans...)
}

// addBlank appends an empty row.
func (d *renderedDoc) addBlank() {
	d.addChrome()
}

// addPad appends an empty row tinted as the given kind, used by the
// compare view to keep the two columns aligned.
func (d *renderedDoc) addPad(kind diffLineKind) {
	d.addChrome()
	d.visual[len(d.visual)-1].kind = kind
}

// kindAt is the compare-view tint of a row.
func (d *renderedDoc) kindAt(row int) diffLineKind {
	if d == nil || row < 0 || row >= len(d.visual) {
		return diffUnchanged
	}
	return d.visual[row].kind
}

// padToLen appends blank rows until the document is n rows long.
func (d *renderedDoc) padToLen(n int) {
	for d.Len() < n {
		d.addBlank()
	}
}

// padDocsToEqual pads the shorter of two documents so that both have
// the same number of rows.
func padDocsToEqual(a, b *renderedDoc) {
	n := max(a.Len(), b.Len())
	a.padToLen(n)
	b.padToLen(n)
}

// addWrapped appends the rows of an already-wrapped source line, which
// must have been added with addLogical. style is applied to the text of
// every segment and preStyle to the continuation arrow in front of it.
func (d *renderedDoc) addWrapped(
	logical int, segs []wrapSeg, style, preStyle lipgloss.Style,
	kind diffLineKind,
) {
	for _, seg := range segs {
		var spans []styledSpan
		if seg.pre != "" {
			spans = append(spans, chromeSpan(seg.pre, preStyle))
		}
		spans = append(spans, plainSpan(seg.text, style))
		d.addRow(logical, seg.off, spans...)
		d.visual[len(d.visual)-1].kind = kind
	}
}

// appendDoc appends another document, renumbering its indices.
func (d *renderedDoc) appendDoc(o *renderedDoc) {
	if o == nil {
		return
	}
	logicalBase := len(d.logical)
	visualBase := len(d.visual)

	d.logical = append(d.logical, o.logical...)
	for _, f := range o.first {
		if f < 0 {
			d.first = append(d.first, -1)
			continue
		}
		d.first = append(d.first, f+visualBase)
	}
	for _, v := range o.visual {
		if v.logical >= 0 {
			v.logical += logicalBase
		}
		d.visual = append(d.visual, v)
	}
}

// Len is the number of rows the document occupies on screen.
func (d *renderedDoc) Len() int {
	if d == nil {
		return 0
	}
	return len(d.visual)
}

// Line renders one row, without any search highlighting.
func (d *renderedDoc) Line(row int) string {
	if d == nil || row < 0 || row >= len(d.visual) {
		return ""
	}
	var b strings.Builder
	for _, sp := range d.visual[row].spans {
		b.WriteString(sp.style.Render(sp.text))
	}
	return b.String()
}

// String renders every row. Rows are newline-terminated rather than
// newline-separated, so that appending one document to another is the
// same as concatenating their strings.
func (d *renderedDoc) String() string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	for row := range d.visual {
		b.WriteString(d.Line(row))
		b.WriteByte('\n')
	}
	return b.String()
}

// rowOf returns the row displaying rune offset off of the given source
// line, or -1 if the line is not in the document. An offset past the
// end of the line resolves to its last row.
func (d *renderedDoc) rowOf(logical, off int) int {
	if d == nil || logical < 0 || logical >= len(d.first) {
		return -1
	}
	row := d.first[logical]
	if row < 0 {
		return -1
	}
	best := row
	for i := row; i < len(d.visual) && d.visual[i].logical == logical; i++ {
		if d.visual[i].off > off {
			break
		}
		best = i
	}
	return best
}

// docFromLines builds a document of unstyled lines, for placeholders
// such as "Fetching..." that are not worth running a formatter over.
func docFromLines(lines ...string) *renderedDoc {
	d := &renderedDoc{}
	for _, line := range lines {
		i := d.addLogical(line)
		d.addRow(i, 0, plainSpan(line, lipgloss.Style{}))
	}
	return d
}
