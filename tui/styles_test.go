// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRgbHex(t *testing.T) {
	tests := []struct {
		c    rgb
		want string
	}{
		{rgb{0, 0, 0}, "#000000"},
		{rgb{255, 255, 255}, "#ffffff"},
		{rgb{0x55, 0x4d, 0x00}, "#554d00"},
		{rgb{0x15, 0x50, 0x20}, "#155020"},
	}
	for _, tt := range tests {
		got := tt.c.hex()
		if got != tt.want {
			t.Errorf("rgb%v.hex() = %q, want %q",
				tt.c, got, tt.want)
		}
	}
}

func TestRgbLerp(t *testing.T) {
	black := rgb{0, 0, 0}
	white := rgb{255, 255, 255}

	if got := black.lerp(white, 0); got != black {
		t.Errorf("lerp(0) = %v, want %v", got, black)
	}
	if got := black.lerp(white, 1); got != white {
		t.Errorf("lerp(1) = %v, want %v", got, white)
	}

	mid := black.lerp(white, 0.5)
	if mid.r != 127 || mid.g != 127 || mid.b != 127 {
		t.Errorf("lerp(0.5) = %v, want ~{127,127,127}", mid)
	}

	lorem := rgb{100, 0, 200}
	dolor := rgb{200, 100, 0}
	q := lorem.lerp(dolor, 0.25)
	if q.r != 125 || q.g != 25 || q.b != 150 {
		t.Errorf("lerp(0.25) = %v, want {125,25,150}", q)
	}
}

func TestBuildStyles_BothThemes(t *testing.T) {
	for key := range darkTheme.BgColors {
		if _, ok := lightTheme.BgColors[key]; !ok {
			t.Errorf("light theme missing bgColor %q", key)
		}
	}
	for key := range lightTheme.BgColors {
		if _, ok := darkTheme.BgColors[key]; !ok {
			t.Errorf("dark theme missing bgColor %q", key)
		}
	}
}

func TestSetTheme(t *testing.T) {
	prev := activeTheme
	defer buildStyles(prev)

	SetTheme("dark")
	if activeTheme != &darkTheme {
		t.Error("expected dark theme")
	}
	SetTheme("light")
	if activeTheme != &lightTheme {
		t.Error("expected light theme")
	}
}

func TestTruncate_ASCII(t *testing.T) {
	s := "lorem ipsum dolor sit amet"
	got := truncate(s, 10)
	if got != "lorem ip… " {
		t.Errorf("truncate(%q, 10) = %q", s, got)
	}
}

func TestTruncate_FullWidth(t *testing.T) {
	// 3 Hangul chars = 6 display columns
	s := "가나다lorem"
	got := truncate(s, 6)
	// 6 columns: 2 Hangul (4 cols) + "… " (2 cols) = 6
	if got != "가나… " {
		t.Errorf("truncate(%q, 6) = %q", s, got)
	}
}

func TestTruncate_FullWidth_NoTruncation(t *testing.T) {
	// 3 Hangul chars = 6 display columns, width = 8 → no truncation
	s := "가나다"
	got := truncate(s, 8)
	if got != s {
		t.Errorf("truncate(%q, 8) = %q, want unchanged", s, got)
	}
}

func TestRenderCell_CJK_ExactWidth(t *testing.T) {
	// 5 Hangul = 10 display cols, render into 9-col cell.
	// Lipgloss MaxWidth may leave a 1-col gap; renderCell must pad.
	s := "가나다라마"
	cell := renderCell(s, 9)
	if w := lipgloss.Width(cell); w != 9 {
		t.Errorf("renderCell width = %d, want 9", w)
	}
}

func TestTruncate_MixedWidth(t *testing.T) {
	// "ab" + Hangul + "cd" = 2+2+2 = 6 display columns
	s := "ab가cd"
	got := truncate(s, 5)
	// width-2=3: "ab" is 2 cols, Hangul would push to 4 > 3
	// Truncate at "ab" + "… " = 4 cols
	if got != "ab… " {
		t.Errorf("truncate(%q, 5) = %q", s, got)
	}
}
