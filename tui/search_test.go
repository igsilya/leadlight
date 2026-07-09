// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"testing"
)

func TestMultipleOccurrencesOnSameLine(t *testing.T) {
	m := &Model{
		viewportLines: []string{
			"foo bar foo baz foo",
			"no matches here",
			"another foo here",
		},
	}

	// Search for "foo"
	m.searchText = "foo"
	m.updateViewportSearchMatches()

	// Should find 4 total matches: 3 on line 0, 1 on line 2
	if len(m.searchMatches) != 4 {
		t.Errorf("Expected 4 matches, got %d", len(m.searchMatches))
	}

	// Check that the first 3 matches are on line 0
	for i := 0; i < 3; i++ {
		if m.searchMatches[i].lineIdx != 0 {
			t.Errorf("Match %d should be on line 0, got line %d", i, m.searchMatches[i].lineIdx)
		}
	}

	// Check that they have different positions
	if m.searchMatches[0].start == m.searchMatches[1].start {
		t.Error("First and second matches should have different positions")
	}
	if m.searchMatches[1].start == m.searchMatches[2].start {
		t.Error("Second and third matches should have different positions")
	}

	// Fourth match should be on line 2
	if m.searchMatches[3].lineIdx != 2 {
		t.Errorf("Match 3 should be on line 2, got line %d", m.searchMatches[3].lineIdx)
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
