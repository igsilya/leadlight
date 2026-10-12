// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

func TestDocFromLinesIsUnstyled(t *testing.T) {
	d := docFromLines("Fetching...", "second")

	if d.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", d.Len())
	}
	if got := d.Line(0); got != "Fetching..." {
		t.Errorf("Line(0) = %q, want %q", got, "Fetching...")
	}
	if got := d.String(); got != "Fetching...\nsecond\n" {
		t.Errorf("String() = %q", got)
	}
	// A row out of range is empty rather than a panic.
	if got := d.Line(7); got != "" {
		t.Errorf("Line(7) = %q, want empty", got)
	}
}

func TestDocStringIsNewlineTerminated(t *testing.T) {
	// Newline-terminated rows are what make appendDoc the same as
	// string concatenation; newline-separated rows would not be.
	a := docFromLines("one", "two")
	b := docFromLines("three")

	want := a.String() + b.String()
	a.appendDoc(b)
	if got := a.String(); got != want {
		t.Errorf("appendDoc gave %q, concatenation gave %q", got, want)
	}
	if got := a.String(); got != "one\ntwo\nthree\n" {
		t.Errorf("String() = %q", got)
	}
}

func TestDocAppendRenumbersIndices(t *testing.T) {
	a := docFromLines("a0", "a1")
	b := docFromLines("b0", "b1")
	a.appendDoc(b)

	if len(a.logical) != 4 {
		t.Fatalf("logical count = %d, want 4", len(a.logical))
	}
	for row, want := range []string{"a0", "a1", "b0", "b1"} {
		if a.logical[a.visual[row].logical] != want {
			t.Errorf("row %d points at %q, want %q",
				row, a.logical[a.visual[row].logical], want)
		}
		if a.first[row] != row {
			t.Errorf("first[%d] = %d, want %d", row, a.first[row], row)
		}
	}
	// The appended document is untouched by the renumbering.
	if b.visual[0].logical != 0 {
		t.Errorf("source document was mutated: logical = %d",
			b.visual[0].logical)
	}
}

// wrappedDoc lays one source line out at the given width, the way the
// formatters do.
func wrappedDoc(line string, width int) *renderedDoc {
	d := &renderedDoc{}
	i := d.addLogical(line)
	d.addWrapped(i, wrapLineSegs(line, width),
		plainTextStyle, wrapIndicatorStyle, diffUnchanged)
	return d
}

func TestDocRowOfWrappedLine(t *testing.T) {
	line := "lorem ipsum dolor sit amet consectetur adipiscing elit sed"
	d := wrappedDoc(line, 20)

	if d.Len() < 3 {
		t.Fatalf("expected the line to wrap to 3+ rows, got %d", d.Len())
	}
	if d.first[0] != 0 {
		t.Errorf("first[0] = %d, want 0", d.first[0])
	}

	// Every offset in the line must resolve to the row that shows it.
	for off := 0; off < len([]rune(line)); off++ {
		row := d.rowOf(0, off)
		if row < 0 {
			t.Fatalf("offset %d resolved to no row", off)
		}
		v := d.visual[row]
		if off < v.off || off >= v.off+v.runeLen() {
			t.Errorf("offset %d landed on row %d spanning [%d,%d)",
				off, row, v.off, v.off+v.runeLen())
		}
	}

	// Offsets past the end clamp to the last row.
	if got, want := d.rowOf(0, 1<<20), d.Len()-1; got != want {
		t.Errorf("rowOf past end = %d, want %d", got, want)
	}
	if got := d.rowOf(3, 0); got != -1 {
		t.Errorf("rowOf for an unknown line = %d, want -1", got)
	}
}

func TestDocWrappedRowsCoverTheSourceLine(t *testing.T) {
	for _, line := range wrapTestCorpus() {
		for _, width := range []int{20, 40, 80} {
			d := wrappedDoc(line, width)

			// Concatenating the non-synthetic spans of every row
			// must reproduce the source line exactly.
			var b strings.Builder
			for _, v := range d.visual {
				for _, sp := range v.spans {
					if !sp.synthetic {
						b.WriteString(sp.text)
					}
				}
			}
			if got := b.String(); got != line {
				t.Errorf("width=%d: rows spell %q, source is %q",
					width, got, line)
			}
		}
	}
}

func TestDocLineConcatenatesSpans(t *testing.T) {
	d := &renderedDoc{}
	i := d.addLogical("hello world")
	d.addRow(i, 0,
		chromeSpan("> ", lipgloss.Style{}),
		plainSpan("hello world", lipgloss.Style{}),
	)

	if got := d.Line(0); got != "> hello world" {
		t.Errorf("Line(0) = %q", got)
	}
	if got := d.visual[0].runeLen(); got != len("hello world") {
		t.Errorf("runeLen() = %d, want %d", got, len("hello world"))
	}
}

func TestDocChromeRowsHaveNoSource(t *testing.T) {
	d := &renderedDoc{}
	d.addBlank()
	i := d.addLogical("text")
	d.addRow(i, 0, plainSpan("text", lipgloss.Style{}))
	d.addBlank()

	if d.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", d.Len())
	}
	for _, row := range []int{0, 2} {
		if d.visual[row].logical != -1 {
			t.Errorf("row %d logical = %d, want -1",
				row, d.visual[row].logical)
		}
		if d.Line(row) != "" {
			t.Errorf("row %d = %q, want empty", row, d.Line(row))
		}
	}
	if d.first[0] != 1 {
		t.Errorf("first[0] = %d, want 1", d.first[0])
	}
	if got := d.String(); got != "\ntext\n\n" {
		t.Errorf("String() = %q", got)
	}
}

// runeLen is the number of source runes a row displays. Only the
// tests need it; the renderer walks the spans directly.
func (v visualLine) runeLen() int {
	n := 0
	for _, sp := range v.spans {
		if !sp.synthetic {
			n += utf8.RuneCountInString(sp.text)
		}
	}
	return n
}
