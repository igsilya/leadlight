// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"leadlight/db"
)

const testMbox = `From: Lorem Ipsum <lorem@ipsum.example>
Subject: [PATCH v2 1/3] Fix race condition in ovsdb
Cc: dolor@amet.example,
 sit@amet.example
Date: Mon, 10 Mar 2026 12:00:00 +0000
Message-Id: <lorem-001@ipsum.example>
Content-Type: text/plain

This patch fixes the race condition that occurs when
multiple clients access the database simultaneously.

Signed-off-by: Lorem Ipsum <lorem@ipsum.example>
---
 lib/db.c | 10 +++++-----
 1 file changed, 5 insertions(+), 5 deletions(-)

diff --git a/lib/db.c b/lib/db.c
index abc123..def456 100644
--- a/lib/db.c
+++ b/lib/db.c
@@ -42,7 +42,7 @@
-    old_code();
+    new_code();
`

func TestFormatMbox_NotEmpty(t *testing.T) {
	p := ParsedMbox{
		Subject: "[PATCH v2 1/3] Fix race condition in ovsdb",
		From:    "Lorem Ipsum <lorem@ipsum.example>",
		To:      "dev@lorem.example",
		Date:    "Mon, 10 Mar 2026 12:00:00 +0000",
		Body:    "This fixes a race condition.\n\nThe issue occurs when...",
		Diff:    "diff --git a/lib/ovsdb.c b/lib/ovsdb.c\n--- a/lib/ovsdb.c\n+++ b/lib/ovsdb.c\n@@ -100,6 +100,7 @@\n some_function();\n+fix_race();\n other_function();",
	}
	formatted := FormatMbox(p, 120, false)
	if formatted == "" {
		t.Error("formatted is empty")
	}
}

func TestFormatComment(t *testing.T) {
	c := CommentInfo{
		Submitter: "Lorem Ipsum",
		Date:      "2026-03-12T09:00:00",
		Subject:   "Re: [PATCH] Lorem ipsum",
		Content:   "Looks good.\n\nAcked-by: Lorem <lorem@ipsum.example>",
	}
	formatted := FormatComment(c, 120, false, nil)
	if formatted == "" {
		t.Error("formatted is empty")
	}
	if !strings.Contains(formatted, "Lorem Ipsum") {
		t.Error("missing submitter")
	}
	if !strings.Contains(formatted, "Acked-by") {
		t.Error("missing content")
	}
}

func TestFormatComment_Empty(t *testing.T) {
	c := CommentInfo{}
	_ = FormatComment(c, 120, false, nil)
}

func TestReplaceControlChars(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"normal text", "normal text"},
		{" \f", " ^L"},
		{"a\vb\fc", "a^Kb^Lc"},
		{"\x00null", "^@null"},
		{"tab\there", "tab\there"},
		{"\nnewline", "\nnewline"},
		{"clean", "clean"},
	}
	for _, tt := range tests {
		got := replaceControlChars(tt.in)
		if got != tt.want {
			t.Errorf("replaceControlChars(%q) = %q, want %q",
				tt.in, got, tt.want)
		}
	}
}

func TestFormatMbox_FormFeed(t *testing.T) {
	p := ParsedMbox{
		Subject: "test",
		Body:    "body line",
		Diff:    "diff --git a/f b/f\n }\n \f\n+new code",
	}
	result := FormatMbox(p, 80, false)
	if !strings.Contains(result, "^L") {
		t.Error("form feed should render as ^L")
	}
	if strings.Contains(result, "\f") {
		t.Error("raw form feed should not be in output")
	}
}

func TestFormatComment_ControlChars(t *testing.T) {
	c := CommentInfo{
		Subject: "Re: test",
		Content: "looks good\x0c\nAcked-by: Lorem <lorem@ipsum.example>",
	}
	result := FormatComment(c, 80, false, nil)
	if !strings.Contains(result, "^L") {
		t.Error("form feed should render as ^L")
	}
}

func TestIsQuotedLine(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"> quoted text", true},
		{">> nested quote", true},
		{" > indented quote", true},
		{"not quoted", false},
		{"", false},
		{">", true},
	}
	for _, tt := range tests {
		got := isQuotedLine(tt.in)
		if got != tt.want {
			t.Errorf("isQuotedLine(%q) = %v, want %v",
				tt.in, got, tt.want)
		}
	}
}

func TestWrapLine_Short(t *testing.T) {
	lines := wrapLine("short line", 80)
	if len(lines) != 1 {
		t.Fatalf("len = %d, want 1", len(lines))
	}
	if lines[0] != "short line" {
		t.Errorf("got %q", lines[0])
	}
}

func TestWrapLine_ExactWidth(t *testing.T) {
	line := strings.Repeat("x", 40)
	lines := wrapLine(line, 40)
	if len(lines) != 1 {
		t.Fatalf("len = %d, want 1", len(lines))
	}
}

func TestWrapLine_Long(t *testing.T) {
	line := strings.Repeat("lorem ", 20) // 120 chars
	lines := wrapLine(line, 40)
	if len(lines) < 2 {
		t.Fatalf("len = %d, want >= 2", len(lines))
	}
	for i, l := range lines {
		w := len([]rune(l))
		if w > 40 {
			t.Errorf("line %d: %d runes > 40", i, w)
		}
	}
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "↳ ") {
			t.Errorf("continuation %d missing ↳ prefix: %q",
				i, lines[i])
		}
	}
}

func TestWrapLine_WordBreak(t *testing.T) {
	line := "lorem ipsum dolor sit amet consectetur"
	lines := wrapLine(line, 20)
	if len(lines) < 2 {
		t.Fatalf("len = %d, want >= 2", len(lines))
	}
	// First line should break at a space, not mid-word
	if strings.HasSuffix(lines[0], "conse") {
		t.Errorf("broke mid-word: %q", lines[0])
	}
}

func TestWrapLine_NoSpaces(t *testing.T) {
	line := strings.Repeat("x", 100)
	lines := wrapLine(line, 30)
	if len(lines) < 2 {
		t.Fatalf("len = %d, want >= 2", len(lines))
	}
	for i, l := range lines {
		if len([]rune(l)) > 30 {
			t.Errorf("line %d exceeds width: %d", i, len([]rune(l)))
		}
	}
}

func TestWrapLine_NarrowWidth(t *testing.T) {
	line := strings.Repeat("x", 20)
	lines := wrapLine(line, 4)
	for i, l := range lines {
		if len([]rune(l)) > 4 {
			t.Errorf("line %d exceeds width: %d", i, len([]rune(l)))
		}
	}
}

func TestWrapLine_Empty(t *testing.T) {
	lines := wrapLine("", 80)
	if len(lines) != 1 || lines[0] != "" {
		t.Errorf("got %v", lines)
	}
}

func TestFormatMbox_WrapsLongBodyLine(t *testing.T) {
	longLine := strings.Repeat("lorem ", 30)
	p := ParsedMbox{Subject: "test", Body: longLine}
	result := FormatMbox(p, 80, false)
	if strings.Contains(result, "…") {
		t.Error("body should wrap, not truncate")
	}
	if !strings.Contains(result, "↳") {
		t.Error("wrapped lines should have ↳ indicator")
	}
}

func TestFormatComment_WrapsLongContent(t *testing.T) {
	longLine := strings.Repeat("ipsum ", 30) // 180 chars
	c := CommentInfo{
		Subject: "Re: test",
		Content: longLine,
	}
	result := FormatComment(c, 80, false, nil)
	if strings.Contains(result, "…") {
		t.Error("comment should wrap, not truncate")
	}
	if !strings.Contains(result, "↳") {
		t.Error("wrapped lines should have ↳ indicator")
	}
}

func TestFormatDiff_WrapsLongLines(t *testing.T) {
	longDiffLine := "+" + strings.Repeat("x", 200)
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
		"@@ -1 +1 @@\n" + longDiffLine + "\n"
	result := formatDiff(diff, 80)
	if !strings.Contains(result, "↳") {
		t.Error("long diff line should wrap with ↳ continuation")
	}
	// All content should be present (no truncation)
	if strings.Contains(result, "…") {
		t.Error("diff should wrap, not truncate")
	}
}

func TestCollapseQuotedBlocks_SmallBlock(t *testing.T) {
	lines := []string{
		"> line 1",
		"> line 2",
		"> line 3",
		"reply here",
	}
	got := collapseQuotedBlocks(lines)
	if len(got) != len(lines) {
		t.Errorf("len = %d, want %d (small block unchanged)",
			len(got), len(lines))
	}
}

func TestCollapseQuotedBlocks_LargeBlock(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "my reply")
	got := collapseQuotedBlocks(lines)
	if len(got) >= len(lines) {
		t.Fatalf("should collapse: got %d, input %d",
			len(got), len(lines))
	}
	hasMarker := false
	for _, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Error("missing collapse marker")
	}
	if got[len(got)-1] != "my reply" {
		t.Errorf("last line = %q, want reply", got[len(got)-1])
	}
}

func TestCollapseQuotedBlocks_HunkDetection(t *testing.T) {
	lines := []string{
		"> commit message line 1",
		"> commit message line 2",
		"> commit message line 3",
		"> commit message line 4",
		"> commit message line 5",
		"> commit message line 6",
		"> commit message line 7",
		"> commit message line 8",
		"> commit message line 9",
		"> commit message line 10",
		"> @@ -100,6 +100,8 @@ func lorem()",
		"> +new code",
		"> +more code",
		">  context",
		"I think this needs a check here.",
	}
	got := collapseQuotedBlocks(lines)
	// Should keep the hunk header and lines after it
	foundHunk := false
	for _, l := range got {
		if strings.Contains(l, "@@ -100") {
			foundHunk = true
		}
	}
	if !foundHunk {
		t.Error("should keep last hunk header")
	}
}

func TestCollapseQuotedBlocks_NoHunkFallback(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines,
			fmt.Sprintf("> prose paragraph line %d", i))
	}
	lines = append(lines, "agreed")
	got := collapseQuotedBlocks(lines)
	// Should keep last 20 lines of the block as tail
	// Head=3, marker=1, tail=20 => 24 + "agreed" = 25
	if len(got) > 30 {
		t.Errorf("len = %d, want <= 30", len(got))
	}
	if got[len(got)-1] != "agreed" {
		t.Errorf("last = %q", got[len(got)-1])
	}
}

func TestCollapseQuotedBlocks_Mixed(t *testing.T) {
	var lines []string
	// Small quoted block — should not collapse
	lines = append(lines, "> small 1", "> small 2", "> small 3")
	lines = append(lines, "reply to small block")
	// Large quoted block — should collapse
	for i := 0; i < 25; i++ {
		lines = append(lines,
			fmt.Sprintf("> big block line %d", i))
	}
	lines = append(lines, "reply to big block")

	got := collapseQuotedBlocks(lines)
	// Small block lines should all be present
	found := 0
	for _, l := range got {
		if strings.HasPrefix(l, "> small") {
			found++
		}
	}
	if found != 3 {
		t.Errorf("small block lines = %d, want 3", found)
	}
	// Big block should be collapsed
	hasMarker := false
	for _, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Error("big block should have collapse marker")
	}
}

func TestCollapseQuotedBlocks_HeadTailOverlap(t *testing.T) {
	// Block of 9 lines: head=3 + tail=20 > 9, so no collapse
	var lines []string
	for i := 0; i < 9; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	got := collapseQuotedBlocks(lines)
	if len(got) != 9 {
		t.Errorf("len = %d, want 9 (overlap, no collapse)",
			len(got))
	}
}

func TestCollapseQuotedBlocks_DashDashClearer(t *testing.T) {
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> --")
	lines = append(lines, "> 2.53.0")
	lines = append(lines, "> lorem list footer")
	lines = append(lines, "Looks good.")
	got := collapseQuotedBlocks(lines)
	// -- is a clearer, not an anchor. Tail should use fallback
	// (last 20 lines), not start at --.
	markerIdx := -1
	for i, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			markerIdx = i
		}
	}
	if markerIdx < 0 {
		t.Fatal("missing collapse marker")
	}
	// Tail should NOT start at --
	if markerIdx+1 < len(got) && got[markerIdx+1] == "> --" {
		t.Error("-- should not be used as anchor (it's a clearer)")
	}
	if got[len(got)-1] != "Looks good." {
		t.Errorf("last = %q, want reply", got[len(got)-1])
	}
}

func TestCollapseQuotedBlocks_TripleDashClearer(t *testing.T) {
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> ---")
	lines = append(lines, ">  lorem.c | 3 +++")
	lines = append(lines, ">  1 file changed")
	lines = append(lines, "Acked.")
	got := collapseQuotedBlocks(lines)
	markerIdx := -1
	for i, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			markerIdx = i
		}
	}
	if markerIdx < 0 {
		t.Fatal("missing collapse marker")
	}
	// --- is a clearer, not an anchor
	if markerIdx+1 < len(got) && got[markerIdx+1] == "> ---" {
		t.Error("--- should not be used as anchor (it's a clearer)")
	}
}

func TestCollapseQuotedBlocks_TrailerLastLineSkipped(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> --") // last line of block
	lines = append(lines, "Reply.")
	got := collapseQuotedBlocks(lines)
	markerIdx := -1
	for i, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			markerIdx = i
		}
	}
	if markerIdx < 0 {
		t.Fatal("missing collapse marker")
	}
	// -- as last line should be skipped as anchor; fallback tail used
	if markerIdx+1 < len(got) && got[markerIdx+1] == "> --" {
		t.Error("'--' as last line should not be used as anchor")
	}
}

func TestCollapseQuotedBlocks_NoReplyShortTail(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	// No reply after — tail should be capped at collapseTailNoReply (8)
	got := collapseQuotedBlocks(lines)
	hasMarker := false
	for _, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Fatal("should have collapse marker")
	}
	// head(3) + marker(1) + tail(8) = 12
	if len(got) > 12 {
		t.Errorf("len = %d, want <= 12 (short tail for no-reply)",
			len(got))
	}
}

func TestCollapseQuotedBlocks_ReplyFullTail(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "my reply")
	got := collapseQuotedBlocks(lines)
	// head(3) + marker(1) + tail(20) = 24 + "my reply" = 25
	if len(got) > 26 {
		t.Errorf("len = %d, want <= 26 (full tail with reply)",
			len(got))
	}
	if len(got) < 20 {
		t.Errorf("len = %d, want >= 20 (tail should be ~20)",
			len(got))
	}
}

func TestCollapseQuotedBlocks_AnchorTailUncapped(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> @@ -1,3 +1,30 @@")
	for i := 0; i < 25; i++ {
		lines = append(lines,
			fmt.Sprintf("> +new code line %d", i))
	}
	lines = append(lines, "Review comment.")
	got := collapseQuotedBlocks(lines)
	// @@ anchor found — tail is uncapped, shows all 26 lines from @@
	foundHunk := false
	tailCount := 0
	inTail := false
	for _, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			inTail = true
			continue
		}
		if inTail && strings.HasPrefix(l, ">") {
			tailCount++
		}
		if strings.Contains(l, "@@ -1,3") {
			foundHunk = true
		}
	}
	if !foundHunk {
		t.Error("@@ anchor should be in tail")
	}
	if tailCount != 26 {
		t.Errorf("tail = %d lines, want 26 (uncapped)", tailCount)
	}
}

func TestCollapseQuotedBlocks_NoReplyIgnoresAnchor(t *testing.T) {
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> @@ -1,3 +1,6 @@")
	for i := 0; i < 10; i++ {
		lines = append(lines,
			fmt.Sprintf("> +code %d", i))
	}
	// No reply after — anchor should be ignored
	got := collapseQuotedBlocks(lines)
	markerIdx := -1
	for i, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			markerIdx = i
		}
	}
	if markerIdx < 0 {
		t.Fatal("missing collapse marker")
	}
	tailCount := 0
	for i := markerIdx + 1; i < len(got); i++ {
		tailCount++
	}
	if tailCount > collapseTailNoReply {
		t.Errorf("no-reply tail = %d, want <= %d",
			tailCount, collapseTailNoReply)
	}
}

func TestCollapseQuotedBlocks_ClearerStopsSearch(t *testing.T) {
	// @@ early in block, then -- later (not last line).
	// Backward search finds -- first → break → fallback used.
	// The @@ is NOT used as an anchor (tail doesn't start there).
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> @@ -1,3 +1,6 @@")
	lines = append(lines, "> +lorem ipsum")
	lines = append(lines, "> --")
	lines = append(lines, "> 2.53.0")
	for i := 0; i < 10; i++ {
		lines = append(lines, fmt.Sprintf("> reply line %d", i))
	}
	lines = append(lines, "My reply.")
	got := collapseQuotedBlocks(lines)
	markerIdx := -1
	for i, l := range got {
		if strings.Contains(l, "quoted lines hidden") {
			markerIdx = i
		}
	}
	if markerIdx < 0 {
		t.Fatal("missing collapse marker")
	}
	// Tail should NOT start at @@ (it's before the clearer)
	if markerIdx+1 < len(got) &&
		strings.Contains(got[markerIdx+1], "@@ -1,3") {
		t.Error("@@ before clearer should not start the tail")
	}
}

func TestCollapseQuotedBlocks_LastLineClearerSkipped(t *testing.T) {
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, fmt.Sprintf("> line %d", i))
	}
	lines = append(lines, "> @@ -1,3 +1,6 @@")
	lines = append(lines, "> +lorem ipsum")
	lines = append(lines, "> +dolor sit amet")
	lines = append(lines, "> --") // last line — skipped as clearer
	lines = append(lines, "Reply.")
	got := collapseQuotedBlocks(lines)
	// -- as last line skipped, @@ found → anchor used
	foundHunk := false
	for _, l := range got {
		if strings.Contains(l, "@@ -1,3") {
			foundHunk = true
		}
	}
	if !foundHunk {
		t.Error("@@ should be used as anchor when -- is last line")
	}
}

func TestFormatComment_CollapseQuotes(t *testing.T) {
	var content strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&content, "> quoted line %d\n", i)
	}
	content.WriteString("looks good\n")
	c := CommentInfo{
		Subject: "Re: test",
		Content: content.String(),
	}
	result := FormatComment(c, 80, true, nil)
	if !strings.Contains(result, "quoted lines hidden") {
		t.Error("should collapse with collapseQuotes=true")
	}
}

func TestFormatComment_ExpandQuotes(t *testing.T) {
	var content strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&content, "> quoted line %d\n", i)
	}
	content.WriteString("looks good\n")
	c := CommentInfo{
		Subject: "Re: test",
		Content: content.String(),
	}
	result := FormatComment(c, 80, false, nil)
	if strings.Contains(result, "quoted lines hidden") {
		t.Error("should not collapse with collapseQuotes=false")
	}
}

func TestWrapLogLine_Short(t *testing.T) {
	lines := wrapLogLine("short log line", 80)
	if len(lines) != 1 || lines[0] != "short log line" {
		t.Errorf("got %v", lines)
	}
}

func TestWrapLogLine_Empty(t *testing.T) {
	lines := wrapLogLine("", 80)
	if len(lines) != 1 || lines[0] != "" {
		t.Errorf("got %v", lines)
	}
}

func TestWrapLogLine_URLPreserved(t *testing.T) {
	url := "https://pw.example.com/api/1.2/events/" +
		"?order=date&per_page=1024&project=lorem" +
		"&since=2026-03-18T15%3A58%3A24"
	line := "2026/03/18 21:59:10 HTTP GET (go) -> 200 " + url
	lines := wrapLogLine(line, 133)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got %d lines", len(lines))
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, url) {
			found = true
		}
	}
	if !found {
		t.Error("URL should be intact on a single visual line")
	}
}

func TestWrapLogLine_URLExceedsWidth(t *testing.T) {
	url := "https://pw.example.com/" + strings.Repeat("x", 100)
	line := "GET " + url
	lines := wrapLogLine(line, 80)
	for i, l := range lines {
		if len([]rune(l)) > 80 {
			t.Errorf("line %d exceeds width: %d runes",
				i, len([]rune(l)))
		}
	}
}

func TestWrapLogLine_MultiWord(t *testing.T) {
	line := strings.Repeat("lorem ", 20)
	lines := wrapLogLine(strings.TrimSpace(line), 40)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got %d lines", len(lines))
	}
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "↳ ") {
			t.Errorf("line %d missing ↳ prefix: %q",
				i, lines[i])
		}
	}
	for i, l := range lines {
		if len([]rune(l)) > 40 {
			t.Errorf("line %d exceeds width: %d", i, len([]rune(l)))
		}
	}
}

func TestFormatDate_ISO(t *testing.T) {
	got := formatDate("2025-12-04T09:17:04")
	want := "Thu, 04 Dec 2025 09:17:04 +0000"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDate_RFC(t *testing.T) {
	in := "Wed, 18 Mar 2026 14:41:13 +0100"
	got := formatDate(in)
	if got != in {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestFormatDate_Invalid(t *testing.T) {
	got := formatDate("lorem ipsum")
	if got != "lorem ipsum" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestFormatComment_WithHeaders(t *testing.T) {
	c := CommentInfo{
		Subject:        "Re: Lorem ipsum",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
		Date:           "2025-12-04T09:17:04",
		Content:        "Looks good.",
		Headers: "Date: Thu, 04 Dec 2025 14:57:28 +0530\n" +
			"To: Dolor Amet <dolor@amet.example>\n" +
			"Cc: sit@amet.example\n",
		ListArchiveURL: "https://archive.example.com/lorem/123",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "lorem@ipsum.example") {
		t.Error("should show submitter email")
	}
	if !strings.Contains(result, "+0530") {
		t.Error("should show date from headers with timezone")
	}
	if !strings.Contains(result, "dolor@amet.example") {
		t.Error("should show To header")
	}
	if !strings.Contains(result, "sit@amet.example") {
		t.Error("should show Cc header")
	}
	if !strings.Contains(result, "archive.example.com") {
		t.Error("should show archive URL")
	}
}

func TestFormatComment_HeaderFallback(t *testing.T) {
	c := CommentInfo{
		Subject:   "Re: Lorem ipsum",
		Submitter: "Lorem Ipsum",
		Date:      "2025-12-04T09:17:04",
		Content:   "Looks good.",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "Lorem Ipsum") {
		t.Error("should fall back to Submitter")
	}
	if !strings.Contains(result, "Thu, 04 Dec 2025") {
		t.Error("should convert ISO date to RFC")
	}
}

func TestFormatComment_FromPrefersReplyTo(t *testing.T) {
	c := CommentInfo{
		Subject:        "Re: Lorem ipsum",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
		Date:           "2025-12-04T09:17:04",
		Content:        "Looks good.",
		Headers: "From: =?utf-8?q?Lorem_via_dev?= <dev@lorem.example>\n" +
			"Reply-To: Lorem Ipsum <lorem@ipsum.example>\n" +
			"Date: Thu, 04 Dec 2025 14:57:28 +0530\n",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "lorem@ipsum.example") {
		t.Error("should use Reply-To email")
	}
	if strings.Contains(result, "dev@lorem.example") {
		t.Error("should not use mangled list From")
	}
}

func TestFormatComment_FromFallsBackToSubmitter(t *testing.T) {
	// No Reply-To — API submitter preferred over raw From header
	// (which is often mangled by mailing lists).
	c := CommentInfo{
		Subject:        "Re: Lorem ipsum",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
		Date:           "2025-12-04T09:17:04",
		Content:        "Looks good.",
		Headers: "From: Lorem Ipsum <lorem@real.example>\n" +
			"Date: Thu, 04 Dec 2025 14:57:28 +0530\n",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "lorem@ipsum.example") {
		t.Error("should use API submitter over raw From")
	}
}

func TestFormatComment_FromLastResort(t *testing.T) {
	// No Reply-To, no API submitter — From header is the last resort.
	c := CommentInfo{
		Subject: "Re: Lorem ipsum",
		Date:    "2025-12-04T09:17:04",
		Content: "Looks good.",
		Headers: "From: Lorem Ipsum <lorem@last.example>\n" +
			"Date: Thu, 04 Dec 2025 14:57:28 +0530\n",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "lorem@last.example") {
		t.Error("should fall back to From header as last resort")
	}
}

func TestFormatComment_SubjectCompacted(t *testing.T) {
	c := CommentInfo{
		Subject: "Re: [dev] [PATCH v2 04/19] Lorem ipsum\n dolor sit amet",
		Date:    "2025-12-04T09:17:04",
		Content: "Looks good.",
	}
	result := FormatComment(c, 120, false, nil)
	if strings.Contains(result, "\n dolor") {
		t.Error("Subject should be compacted, fold not removed")
	}
	if !strings.Contains(result, "Lorem ipsum dolor sit amet") {
		t.Error("Subject should contain compacted text")
	}
}

func TestFormatComment_FromFallbackNoEmail(t *testing.T) {
	c := CommentInfo{
		Subject:   "Re: Lorem ipsum",
		Submitter: "Lorem Ipsum",
		Date:      "2025-12-04T09:17:04",
		Content:   "Looks good.",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "Lorem Ipsum") {
		t.Error("should show submitter name")
	}
	if strings.Contains(result, "<") {
		t.Error("should not have angle brackets without email")
	}
}

func TestFormatComment_DecodesHeaderMIME(t *testing.T) {
	c := CommentInfo{
		Subject:        "Re: Lorem",
		Submitter:      "Lorem",
		SubmitterEmail: "lorem@ipsum.example",
		Date:           "2025-12-04T09:17:04",
		Content:        "Looks good.",
		Headers: "To: =?utf-8?q?Dol=C3=B6r_Amet?= <dolor@amet.example>\n" +
			"Cc: =?utf-8?q?S=C3=ADt?= <sit@amet.example>\n" +
			"Date: Thu, 04 Dec 2025 14:57:28 +0530\n",
	}
	result := FormatComment(c, 120, false, nil)
	if !strings.Contains(result, "Dolör Amet") {
		t.Error("To header should be MIME decoded")
	}
	if !strings.Contains(result, "Sít") {
		t.Error("Cc header should be MIME decoded")
	}
}

func TestFormatMbox_ToHeader(t *testing.T) {
	p := ParsedMbox{
		Subject: "Lorem ipsum",
		From:    "Lorem <lorem@ipsum.example>",
		To:      "dolor@amet.example, sit@amet.example",
		Date:    "Wed, 18 Mar 2026 14:41:13 +0100",
		Body:    "Body text here.",
	}
	result := FormatMbox(p, 120, false)
	if !strings.Contains(result, "dolor@amet.example") {
		t.Error("should display To header")
	}
}

func TestFormatDiff_Colors(t *testing.T) {
	diff := "diff --git a/f b/f\n" +
		"--- a/f\n+++ b/f\n" +
		"@@ -1 +1 @@\n-old\n+new\n context\n"
	result := formatDiff(diff, 120)
	if result == "" {
		t.Error("formatted diff is empty")
	}
	if !strings.Contains(result, "old") {
		t.Error("missing old line")
	}
	if !strings.Contains(result, "new") {
		t.Error("missing new line")
	}
}

func TestCompactAndDecodeHeader_NoStrayNewlines(t *testing.T) {
	// Large Cc with RFC 2822 folds and MIME encoding.
	// After compact + decode, the result should have ZERO newlines.
	cc := "Lorem Ipsum <lorem@ex>,\n" +
		" list-a@lists.ex, \"Dolor S.\" <dolor@ex>,\n" +
		" =?utf-8?q?Amet_S=C3=A1nchez?= <amet@ex>,\n" +
		" \"Sit A.B. Consectetur\" <sit@ex>,\n" +
		" Adipiscing Elit <adipiscing@ex>,\n" +
		" =?utf-8?q?Sed_D=C3=B6?= <sed@ex>,\n" +
		" \"Tempor Incididunt \\(Corp\\)\" <tempor@ex>,\n" +
		" list-b@ex, \"Ut Enim\" <ut@ex>"

	compacted := compactHeader(cc)
	if strings.Contains(compacted, "\n") {
		t.Fatal("compacted Cc still has newlines")
	}

	decoded := decodeHeader(compacted)
	if strings.Contains(decoded, "\n") {
		t.Fatal("decoded Cc has stray newlines")
	}
	if !strings.Contains(decoded, "Sánchez") {
		t.Error("MIME-encoded name not decoded")
	}
	if !strings.Contains(decoded, "Dö") {
		t.Error("MIME-encoded umlaut not decoded")
	}
}

func TestFormatMbox_LineWidths(t *testing.T) {
	// Large Cc to stress-test line width calculation at multiple
	// terminal widths. If any line exceeds the target width,
	// lipgloss.Render with Width() re-wraps it, losing indentation.
	var addrs []string
	for i := 0; i < 15; i++ {
		addrs = append(addrs, fmt.Sprintf(
			"Lorem Ipsum%d <lorem%d@ipsum.example>", i, i))
	}
	cc := strings.Join(addrs, ", ")

	for _, width := range []int{80, 100, 120, 133, 150, 200} {
		p := ParsedMbox{
			Subject: "Lorem ipsum dolor sit amet",
			From:    "Lorem <lorem@ipsum.example>",
			To:      "dolor@ipsum.example",
			Cc:      cc,
			Date:    "Mon, 23 Mar 2026 12:00:19 -0400",
			Body:    "Lorem ipsum dolor sit amet.",
		}
		result := FormatMbox(p, width, false)
		lines := strings.Split(result, "\n")
		for i, line := range lines {
			vw := lipgloss.Width(line)
			if vw > width {
				t.Errorf("width=%d line %d visual width %d > %d",
					width, i, vw, width)
			}
		}
	}
}

func TestFormatMbox_CollapseLongCc(t *testing.T) {
	// Build a Cc with enough addresses to wrap to >3 lines at width 80
	var addrs []string
	for i := 0; i < 12; i++ {
		addrs = append(addrs, fmt.Sprintf("Lorem Ipsum%d <lorem%d@ipsum.example>", i, i))
	}
	cc := strings.Join(addrs, ", ")

	p := ParsedMbox{
		Subject: "Lorem ipsum",
		From:    "Lorem <lorem@ipsum.example>",
		Cc:      cc,
		Body:    "Body text.",
	}

	collapsed := FormatMbox(p, 80, true)
	if !strings.Contains(collapsed, "total (e to expand) ···") {
		t.Error("collapsed mbox should contain collapse marker")
	}
	// Only 3 Cc lines + 1 marker line, not all addresses
	lines := strings.Split(collapsed, "\n")
	ccLines := 0
	for _, l := range lines {
		if strings.Contains(l, "@ipsum.example") {
			ccLines++
		}
	}
	// 3 Cc lines + From line = 4 lines with the address pattern.
	// The marker line should NOT contain an address.
	if strings.Contains(collapsed, "lorem11@ipsum.example") {
		t.Error("last address should be hidden in collapsed mode")
	}

	expanded := FormatMbox(p, 80, false)
	if strings.Contains(expanded, "total (e to expand) ···") {
		t.Error("expanded mbox should not contain collapse marker")
	}
	if !strings.Contains(expanded, "lorem11@ipsum.example") {
		t.Error("all addresses should be visible in expanded mode")
	}
}

func TestFormatMbox_ShortCcNotCollapsed(t *testing.T) {
	// Cc that fits in <=3 lines should not be collapsed even when
	// collapseHeaders is true
	p := ParsedMbox{
		Subject: "Lorem ipsum",
		Cc:      "Lorem <lorem@ipsum.example>, Dolor <dolor@ipsum.example>",
		Body:    "Body text.",
	}
	result := FormatMbox(p, 80, true)
	if strings.Contains(result, "total (e to expand) ···") {
		t.Error("short Cc should not be collapsed")
	}
}

func TestFormatMbox_CollapseToHeader(t *testing.T) {
	// To header should also collapse when long enough
	var addrs []string
	for i := 0; i < 12; i++ {
		addrs = append(addrs, fmt.Sprintf("Dolor Amet%d <dolor%d@amet.example>", i, i))
	}
	to := strings.Join(addrs, ", ")

	p := ParsedMbox{
		Subject: "Lorem ipsum",
		To:      to,
		Body:    "Body text.",
	}

	collapsed := FormatMbox(p, 80, true)
	if !strings.Contains(collapsed, "total (e to expand) ···") {
		t.Error("collapsed To should contain collapse marker")
	}
	expanded := FormatMbox(p, 80, false)
	if strings.Contains(expanded, "total (e to expand) ···") {
		t.Error("expanded To should not contain collapse marker")
	}
}

func TestFormatMbox_SubjectNeverCollapsed(t *testing.T) {
	// Even a very long Subject should never be collapsed
	p := ParsedMbox{
		Subject: strings.Repeat("Lorem ipsum dolor sit amet ", 10),
		Body:    "Body text.",
	}
	result := FormatMbox(p, 60, true)
	if strings.Contains(result, "total (e to expand) ···") {
		t.Error("Subject should never be collapsed")
	}
}

func TestFormatComment_CollapseLongCc(t *testing.T) {
	var addrs []string
	for i := 0; i < 12; i++ {
		addrs = append(addrs, fmt.Sprintf("Lorem%d <lorem%d@ipsum.example>", i, i))
	}
	cc := strings.Join(addrs, ", ")

	c := CommentInfo{
		Subject: "Re: Lorem ipsum",
		Headers: "Subject: Re: Lorem ipsum\nCc: " + cc + "\n\n",
		Content: "Looks good.",
	}

	collapsed := FormatComment(c, 80, true, nil)
	if !strings.Contains(collapsed, "total (e to expand) ···") {
		t.Error("collapsed comment Cc should contain collapse marker")
	}

	expanded := FormatComment(c, 80, false, nil)
	if strings.Contains(expanded, "total (e to expand) ···") {
		t.Error("expanded comment Cc should not contain collapse marker")
	}
}

func TestFormatMbox_CollapseMarkerCount(t *testing.T) {
	// Verify the marker reports the correct total recipient count
	var addrs []string
	for i := 0; i < 20; i++ {
		addrs = append(addrs, fmt.Sprintf("Lorem Ipsum%d <lorem%d@ipsum.example>", i, i))
	}
	cc := strings.Join(addrs, ", ")

	p := ParsedMbox{Cc: cc}
	result := FormatMbox(p, 80, true)

	if !strings.Contains(result, "20 total (e to expand) ···") {
		t.Errorf("marker should say 20 total, got:\n%s", result)
	}
}

func TestFormatChecks_URLWrapsToNextLine(t *testing.T) {
	checks := []CheckInfo{{
		Context:   "ci/build",
		State:     "success",
		TargetURL: "https://example.com/builds/12345",
	}}
	// Width too narrow for URL on same line as context
	result := FormatChecks(checks, 40, false)
	lines := strings.Split(result, "\n")
	// URL should be on a separate indented line
	foundURL := false
	for _, l := range lines {
		stripped := stripAnsi(l)
		if strings.Contains(stripped, "https://") {
			foundURL = true
			// Should be on its own line (indented), not on the context line
			if strings.Contains(stripped, "ci/build") {
				t.Errorf("URL should be on a separate line from context: %q", stripped)
			}
			// Should be fully visible (fits within width on its own line)
			if !strings.Contains(stripped, "12345") {
				t.Errorf("URL should not be truncated: %q", stripped)
			}
		}
	}
	if !foundURL {
		t.Error("URL should be present in output")
	}
}

func TestFormatChecks_DescriptionWraps(t *testing.T) {
	checks := []CheckInfo{{
		Context:     "ci/test",
		State:       "fail",
		Description: "This is a very long description that should wrap to multiple lines instead of being truncated with an ellipsis character",
	}}
	result := FormatChecks(checks, 60, false)
	if strings.Contains(result, "…") {
		t.Error("description should wrap, not truncate")
	}
	// All content should be present
	if !strings.Contains(result, "ellipsis character") {
		t.Error("full description should be visible")
	}
	// All description lines should be indented
	for _, l := range strings.Split(result, "\n") {
		if l == "" || strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "Check") {
			continue
		}
		// Strip ANSI codes for checking indentation
		stripped := stripAnsi(l)
		if stripped != "" && !strings.HasPrefix(stripped, "  ") {
			t.Errorf("description line should be indented: %q", stripped)
		}
	}
}

func stripAnsi(s string) string {
	var buf strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		buf.WriteRune(r)
	}
	return buf.String()
}

func TestFormatFrom(t *testing.T) {
	tests := []struct{ name, email, want string }{
		{"Lorem", "lorem@ex", "Lorem <lorem@ex>"},
		{"", "bot@kernel.org", "bot@kernel.org"},
		{"Lorem", "", "Lorem"},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := formatFrom(tt.name, tt.email)
		if got != tt.want {
			t.Errorf("formatFrom(%q, %q) = %q, want %q",
				tt.name, tt.email, got, tt.want)
		}
	}
}

func TestFormatChecks_CollapseSuccess(t *testing.T) {
	var checks []CheckInfo
	for i := 0; i < 10; i++ {
		checks = append(checks, CheckInfo{
			Context: fmt.Sprintf("ci/test-%02d", i),
			State:   "success",
		})
	}
	result := FormatChecks(checks, 80, true)
	count := strings.Count(result, "✓")
	if count != 3 {
		t.Errorf("got %d success icons, want 3", count)
	}
	if !strings.Contains(result, "10 successful checks total") {
		t.Error("should show collapse marker with total count")
	}
}

func TestFormatChecks_CollapsePending(t *testing.T) {
	var checks []CheckInfo
	for i := 0; i < 5; i++ {
		checks = append(checks, CheckInfo{
			Context: fmt.Sprintf("ci/pending-%02d", i),
			State:   "pending",
		})
	}
	result := FormatChecks(checks, 80, true)
	if !strings.Contains(result, "5 checks pending") {
		t.Error("should show pending collapse marker")
	}
	if strings.Contains(result, "ci/pending-00") {
		t.Error("individual pending checks should be hidden")
	}
}

func TestFormatChecks_FailuresAlwaysShown(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/fail-1", State: "fail"},
		{Context: "ci/fail-2", State: "fail"},
		{Context: "ci/warn-1", State: "warning"},
	}
	for i := 0; i < 10; i++ {
		checks = append(checks, CheckInfo{
			Context: fmt.Sprintf("ci/ok-%02d", i),
			State:   "success",
		})
	}
	for i := 0; i < 3; i++ {
		checks = append(checks, CheckInfo{
			Context: fmt.Sprintf("ci/pend-%d", i),
			State:   "pending",
		})
	}
	result := FormatChecks(checks, 80, true)
	if !strings.Contains(result, "ci/fail-1") ||
		!strings.Contains(result, "ci/fail-2") {
		t.Error("failures should always be shown")
	}
	if !strings.Contains(result, "ci/warn-1") {
		t.Error("warnings should always be shown")
	}
	if !strings.Contains(result, "10 successful checks total") {
		t.Error("successes should be collapsed")
	}
	if !strings.Contains(result, "3 checks pending") {
		t.Error("pending should be collapsed")
	}
}

func TestFormatChecks_ExpandShowsAll(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/fail", State: "fail"},
	}
	for i := 0; i < 10; i++ {
		checks = append(checks, CheckInfo{
			Context: fmt.Sprintf("ci/ok-%02d", i),
			State:   "success",
		})
	}
	result := FormatChecks(checks, 80, false)
	count := strings.Count(result, "✓")
	if count != 10 {
		t.Errorf("got %d success icons, want 10", count)
	}
	if strings.Contains(result, "e to expand") {
		t.Error("expanded view should not have collapse markers")
	}
}

func TestFormatChecks_SortOrder(t *testing.T) {
	checks := []CheckInfo{
		{Context: "z-success", State: "success"},
		{Context: "a-fail", State: "fail"},
		{Context: "m-pending", State: "pending"},
		{Context: "b-warning", State: "warning"},
		{Context: "a-success", State: "success"},
	}
	result := FormatChecks(checks, 80, false)
	failIdx := strings.Index(result, "a-fail")
	warnIdx := strings.Index(result, "b-warning")
	succ1Idx := strings.Index(result, "a-success")
	succ2Idx := strings.Index(result, "z-success")
	pendIdx := strings.Index(result, "m-pending")
	if failIdx > warnIdx || warnIdx > succ1Idx ||
		succ1Idx > succ2Idx || succ2Idx > pendIdx {
		t.Errorf("wrong order: fail=%d warn=%d succ1=%d "+
			"succ2=%d pend=%d",
			failIdx, warnIdx, succ1Idx, succ2Idx, pendIdx)
	}
}

func TestExpandTabs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"\tcode", "        code"},
		{"x\tcode", "x       code"},
		{"+\tcode", "+       code"},
		{"-\tcode", "-       code"},
		// Context line (2 spaces) and +/- line (1 char) align at col 8
		{"  \tcode", "        code"},
		// Both produce "code" at column 8 — position-aware alignment
		{"+\t\t\toff", "+                       off"},
		{"  \t\t\toff", "                        off"},
		{"no tabs", "no tabs"},
		{"", ""},
	}
	for _, tt := range tests {
		got := expandTabs(tt.in, 8)
		if got != tt.want {
			t.Errorf("expandTabs(%q, 8) = %q, want %q",
				tt.in, got, tt.want)
		}
	}
}

func TestExpandTabs_DiffAlignment(t *testing.T) {
	// The key issue: in a diff, context lines have "  \t" (2 spaces
	// + tab) while +/- lines have "+\t" (1 char + tab). With
	// position-aware 8-col tabs, both should align code at column 8.
	ctx := expandTabs("  \tPPC_LI64(code);", 8)
	del := expandTabs("-\t/* Load percpu */", 8)
	add := expandTabs("+\tEMIT(code);", 8)

	// Find where the code text starts (after spaces)
	ctxStart := len(ctx) - len(strings.TrimLeft(ctx, " "))
	delStart := 1 + len(del[1:]) - len(strings.TrimLeft(del[1:], " "))
	addStart := 1 + len(add[1:]) - len(strings.TrimLeft(add[1:], " "))

	if ctxStart != delStart || ctxStart != addStart {
		t.Errorf("code should start at same column:\n"+
			"  ctx=%d: %q\n  del=%d: %q\n  add=%d: %q",
			ctxStart, ctx, delStart, del, addStart, add)
	}
}

func TestWrapHeaderValue_CommaTrailsCurrentLine(t *testing.T) {
	value := "Lorem <lorem@ipsum.example>, Dolor <dolor@ipsum.example>, " +
		"Amet <amet@ipsum.example>, Sit <sit@ipsum.example>"
	lines := wrapHeaderValue(value, 60)
	for i, line := range lines {
		if strings.HasPrefix(line, ",") {
			t.Errorf("line %d starts with comma: %q", i, line)
		}
		if i < len(lines)-1 && !strings.HasSuffix(line, ",") {
			t.Errorf("line %d should end with comma: %q", i, line)
		}
	}
	last := lines[len(lines)-1]
	if strings.HasSuffix(last, ",") {
		t.Errorf("last line should not end with comma: %q", last)
	}
}

func TestWrapHeaderValue_NoLeadingSpaceAfterComma(t *testing.T) {
	value := "Lorem <lorem@ex>, Ipsum <ipsum@ex>, " +
		"Dolor <dolor@ex>, Amet <amet@ex>"
	lines := wrapHeaderValue(value, 40)
	for i, line := range lines {
		if i > 0 && (strings.HasPrefix(line, " ") ||
			strings.HasPrefix(line, "\t")) {
			t.Errorf("line %d has leading whitespace: %q",
				i, line)
		}
	}
}

func TestWrapHeaderValue_LongCcList(t *testing.T) {
	var addrs []string
	for i := 0; i < 30; i++ {
		addrs = append(addrs,
			fmt.Sprintf("Lorem%d <lorem%d@example.com>", i, i))
	}
	value := strings.Join(addrs, ", ")
	lines := wrapHeaderValue(value, 111)
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(line, " ") {
			t.Errorf("line %d starts with space: %q", i, line)
		}
	}
}

func TestCompactHeader(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"LF fold", "a@ex,\n b@ex", "a@ex, b@ex"},
		{"tab fold", "a@ex,\n\tb@ex", "a@ex, b@ex"},
		{"CRLF fold", "a@ex,\r\n b@ex", "a@ex, b@ex"},
		{"multi fold", "a@ex,\n b@ex,\n c@ex", "a@ex, b@ex, c@ex"},
		{"no fold", "a@ex, b@ex", "a@ex, b@ex"},
		{"empty", "", ""},
		{"deep indent", "a@ex,\n         b@ex", "a@ex, b@ex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactHeader(tt.in)
			if got != tt.want {
				t.Errorf("compactHeader(%q) = %q, want %q",
					tt.in, got, tt.want)
			}
		})
	}
}

func TestHeaderString_String(t *testing.T) {
	headers := map[string]interface{}{"To": "dev@example.org"}
	got := headerString(headers, "To")
	if got != "dev@example.org" {
		t.Errorf("got %q", got)
	}
}

func TestHeaderString_Array(t *testing.T) {
	headers := map[string]interface{}{
		"Received": []interface{}{"first", "second"},
	}
	got := headerString(headers, "Received")
	if got != "first" {
		t.Errorf("got %q, want first element", got)
	}
}

func TestHeaderString_Missing(t *testing.T) {
	headers := map[string]interface{}{}
	got := headerString(headers, "To")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestQuoteDepth(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"", 0},
		{"lorem ipsum", 0},
		{"> lorem", 1},
		{"> > lorem", 2},
		{"> > > lorem", 3},
		{">lorem", 1},
		{">", 1},
		{"> >     lorem ns=$1", 2},
	}
	for _, tt := range tests {
		got := quoteDepth(tt.line)
		if got != tt.want {
			t.Errorf("quoteDepth(%q) = %d, want %d",
				tt.line, got, tt.want)
		}
	}
}

func TestStripQuoteMarkers(t *testing.T) {
	tests := []struct {
		line, wantPrefix, wantContent string
	}{
		{"lorem ipsum", "", "lorem ipsum"},
		{"> lorem", "> ", "lorem"},
		{"> > lorem", "> > ", "lorem"},
		{"> > > lorem", "> > > ", "lorem"},
		{">lorem", ">", "lorem"},
		{"> >     lorem ns=$1", "> > ", "    lorem ns=$1"},
		{"> > +lorem", "> > ", "+lorem"},
		{">> >>> lorem", ">> >>> ", "lorem"},
		{"", "", ""},
		{"> > ", "> > ", ""},
		{">", ">", ""},
	}
	for _, tt := range tests {
		prefix, content := stripQuoteMarkers(tt.line)
		if prefix != tt.wantPrefix || content != tt.wantContent {
			t.Errorf("stripQuoteMarkers(%q) = (%q, %q), want (%q, %q)",
				tt.line, prefix, content, tt.wantPrefix, tt.wantContent)
		}
	}
}

func TestBuildSourceLines(t *testing.T) {
	source := buildSourceLines(
		"Lorem ipsum\nDolor sit amet\n",
		"+consectetur\n-adipiscing\n",
		[]CommentInfo{
			{Content: "> Lorem ipsum\nViverra sagittis\n> > lacus vel"},
		},
	)
	// Patch content lines
	if !source["Lorem ipsum"] {
		t.Error("missing patch content line")
	}
	if !source["Dolor sit amet"] {
		t.Error("missing patch content line 2")
	}
	// Diff lines
	if !source["+consectetur"] {
		t.Error("missing diff line")
	}
	if !source["-adipiscing"] {
		t.Error("missing diff line 2")
	}
	// Comment lines stripped to depth 0
	if !source["Lorem ipsum"] {
		t.Error("missing stripped comment line")
	}
	if !source["Viverra sagittis"] {
		t.Error("missing unquoted comment line")
	}
	if !source["lacus vel"] {
		t.Error("missing stripped nested comment line")
	}
	// Empty lines excluded
	if source[""] {
		t.Error("empty line should not be in source")
	}
}

func TestRejoinQuoteContinuations(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]bool
		lines  []string
		want   []string
	}{
		{
			"basic single break",
			map[string]bool{
				"lorem ipsum dolor sit amet consectetur": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet",
				"consectetur",
			},
			[]string{
				"> lorem ipsum dolor sit amet consectetur",
			},
		},
		{
			"multi-line continuation",
			map[string]bool{
				"lorem ipsum dolor sit amet consectetur adipiscing": true,
			},
			[]string{
				"> lorem ipsum dolor sit",
				"amet consectetur",
				"adipiscing",
			},
			[]string{
				"> lorem ipsum dolor sit amet consectetur adipiscing",
			},
		},
		{
			"nested quote depth 2 to 0",
			map[string]bool{
				"+lorem ipsum dolor sit amet consectetur": true,
			},
			[]string{
				"> > +lorem ipsum dolor sit amet",
				"consectetur",
			},
			[]string{
				"> > +lorem ipsum dolor sit amet consectetur",
			},
		},
		{
			"depth 2 continuation at depth 1",
			map[string]bool{
				"+lorem ipsum dolor sit amet consectetur": true,
			},
			[]string{
				"> > +lorem ipsum dolor sit amet",
				"> consectetur",
			},
			[]string{
				"> > +lorem ipsum dolor sit amet consectetur",
			},
		},
		{
			"preserves content indentation",
			map[string]bool{
				"    lorem ns=$1 ipsum=$2 dolor=$3 amet=$4 sit=$5 consectetur=$6": true,
			},
			[]string{
				"> >     lorem ns=$1 ipsum=$2 dolor=$3 amet=$4",
				"sit=$5 consectetur=$6",
			},
			[]string{
				"> >     lorem ns=$1 ipsum=$2 dolor=$3 amet=$4 sit=$5 consectetur=$6",
			},
		},
		{
			"empty source leaves lines unchanged",
			nil,
			[]string{
				"> lorem ipsum",
				"dolor",
			},
			[]string{
				"> lorem ipsum",
				"dolor",
			},
		},
		{
			"mixed content: some rejoin some not",
			map[string]bool{
				"lorem ipsum dolor sit amet consectetur": true,
				"adipiscing elit sed do":                 true,
			},
			[]string{
				"> lorem ipsum dolor sit amet",
				"consectetur",
				"> adipiscing elit sed do",
				"Viverra sagittis lacus.",
			},
			[]string{
				"> lorem ipsum dolor sit amet consectetur",
				"> adipiscing elit sed do",
				"Viverra sagittis lacus.",
			},
		},
		{
			"non-breaking space in quoted content",
			map[string]bool{
				" lorem ipsum dolor sit amet consectetur": true,
			},
			[]string{
				">> >>>>\u00a0lorem ipsum dolor sit amet",
				">> >> consectetur",
			},
			[]string{
				">> >>>> lorem ipsum dolor sit amet consectetur",
			},
		},
		{
			"same depth rejoin",
			map[string]bool{
				"lorem ipsum dolor sit amet consectetur adipiscing": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet consectetur",
				"> adipiscing",
			},
			[]string{
				"> lorem ipsum dolor sit amet consectetur adipiscing",
			},
		},
		{
			"preserves original quote marker style",
			map[string]bool{
				"lorem ipsum dolor sit amet consectetur": true,
			},
			[]string{
				">> >>> lorem ipsum dolor sit amet",
				"consectetur",
			},
			[]string{
				">> >>> lorem ipsum dolor sit amet consectetur",
			},
		},
		{
			"mid-path break (no space join)",
			map[string]bool{
				"lorem/ipsum/dolor/sit/amet/consectetur": true,
			},
			[]string{
				"> lorem/ipsum/dolor/sit/amet/",
				"consectetur",
			},
			[]string{
				"> lorem/ipsum/dolor/sit/amet/consectetur",
			},
		},
		{
			"partial multi-line: second continuation no match",
			map[string]bool{
				"lorem ipsum dolor": true,
			},
			[]string{
				"> lorem ipsum",
				"dolor",
				"sit amet",
			},
			[]string{
				"> lorem ipsum dolor",
				"sit amet",
			},
		},
	}
	for _, tt := range tests {
		got := rejoinQuoteContinuations(tt.lines, tt.source)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n  got  %v\n  want %v",
				tt.name, got, tt.want)
		}
	}
}

func TestRejoinQuoteContinuations_NegativeCases(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]bool
		lines  []string
	}{
		{
			"reply after complete quoted line",
			map[string]bool{
				"lorem ipsum dolor sit amet.": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet.",
				"Viverra sagittis lacus vel.",
			},
		},
		{
			"reply after empty quoted line (paragraph break)",
			map[string]bool{
				"lorem ipsum": true,
			},
			[]string{
				"> lorem ipsum",
				">",
				"Dolor sit amet.",
			},
		},
		{
			"equal quote depth is normal quoting",
			map[string]bool{
				"lorem ipsum":    true,
				"dolor sit amet": true,
			},
			[]string{
				"> lorem ipsum",
				"> dolor sit amet",
			},
		},
		{
			"higher quote depth on next line",
			map[string]bool{
				"lorem ipsum dolor": true,
			},
			[]string{
				"> lorem ipsum",
				"> > dolor",
			},
		},
		{
			"empty continuation line",
			map[string]bool{
				"lorem ipsum": true,
			},
			[]string{
				"> lorem ipsum",
				"",
			},
		},
		{
			"same depth not a broken wrap",
			map[string]bool{
				"lorem ipsum": true,
				"dolor":       true,
			},
			[]string{
				"> lorem ipsum",
				"> dolor",
			},
		},
		{
			"both lines unquoted",
			map[string]bool{
				"lorem ipsum dolor": true,
			},
			[]string{
				"lorem ipsum",
				"dolor",
			},
		},
		{
			"single word false match risk",
			map[string]bool{
				"dolor":       true,
				"lorem ipsum": true,
			},
			[]string{
				"> lorem ipsum",
				"dolor sit amet.",
			},
		},
		{
			"attribution line after quote",
			map[string]bool{
				"lorem ipsum dolor sit amet": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet",
				"On Thu, Jan 1, 2026 at 10:00 Dolor wrote:",
			},
		},
		{
			"signature after quote",
			map[string]bool{
				"lorem ipsum dolor sit amet": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet",
				"-- ",
			},
		},
		{
			"empty quoted line followed by unquoted",
			map[string]bool{},
			[]string{
				">",
				"lorem",
			},
		},
		{
			"whitespace-only continuation",
			map[string]bool{
				"lorem ipsum": true,
			},
			[]string{
				"> lorem ipsum",
				"   ",
			},
		},
		{
			"partial source match not enough",
			map[string]bool{
				"lorem ipsum dolor": true,
			},
			[]string{
				"> lorem ipsum",
				"dolor sit amet",
			},
		},
		{
			"empty quoted line before reply (reply text in source)",
			map[string]bool{
				"Viverra sagittis lacus vel consectetur": true,
			},
			[]string{
				">",
				"Viverra sagittis lacus vel consectetur",
			},
		},
		{
			"empty nested quoted line before reply",
			map[string]bool{
				"Viverra sagittis": true,
			},
			[]string{
				"> >",
				"Viverra sagittis",
			},
		},
		{
			"alphanumeric boundary prevents no-space join",
			map[string]bool{
				"loremipsum": true,
			},
			[]string{
				"> lorem",
				"ipsum",
			},
		},
		{
			"same depth separate lines not merged",
			map[string]bool{
				"lorem ipsum dolor sit amet":  true,
				"consectetur adipiscing elit": true,
			},
			[]string{
				"> lorem ipsum dolor sit amet",
				"> consectetur adipiscing elit",
			},
		},
		{
			"accented last character prevents no-space join",
			map[string]bool{
				"caféipsum": true,
			},
			[]string{
				"> café",
				"ipsum",
			},
		},
		{
			"continuation between two unrelated quotes",
			map[string]bool{
				"lorem ipsum": true,
				"dolor amet":  true,
			},
			[]string{
				"> lorem ipsum",
				"viverra",
				"> dolor amet",
			},
		},
	}
	for _, tt := range tests {
		got := rejoinQuoteContinuations(tt.lines, tt.source)
		if !reflect.DeepEqual(got, tt.lines) {
			t.Errorf("%s: lines should not be modified\n  got  %v\n  want %v",
				tt.name, got, tt.lines)
		}
	}
}

func TestFromHeader_ReplyTo(t *testing.T) {
	headers := map[string]interface{}{
		"From":     "Lorem via dev <list@example.org>",
		"Reply-To": "Lorem Ipsum <lorem@example.com>",
	}
	got := fromHeader(headers)
	if got != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("got %q, want Reply-To value", got)
	}
}

func TestFromHeader_NoReplyTo(t *testing.T) {
	headers := map[string]interface{}{
		"From": "Lorem Ipsum <lorem@example.com>",
	}
	got := fromHeader(headers)
	if got != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("got %q, want From value", got)
	}
}

func TestFromHeader_MIMEEncoded(t *testing.T) {
	headers := map[string]interface{}{
		"Reply-To": "=?utf-8?q?Toke_H=C3=B8iland-J=C3=B8rgensen?= <toke@example.com>",
	}
	got := fromHeader(headers)
	if !strings.Contains(got, "Høiland") {
		t.Errorf("got %q, want decoded non-ASCII name", got)
	}
	if !strings.Contains(got, "toke@example.com") {
		t.Errorf("got %q, want email preserved", got)
	}
}

func TestGetHeader_JSON(t *testing.T) {
	headers := `{"From": "Lorem <lorem@example.com>", "To": "dev@example.org"}`
	if got := getHeader(headers, "From"); got != "Lorem <lorem@example.com>" {
		t.Errorf("From = %q", got)
	}
	if got := getHeader(headers, "To"); got != "dev@example.org" {
		t.Errorf("To = %q", got)
	}
	if got := getHeader(headers, "Cc"); got != "" {
		t.Errorf("Cc = %q, want empty", got)
	}
}

func TestGetHeader_Compact(t *testing.T) {
	headers := "From: Lorem <lorem@example.com>\nTo: dev@example.org\nCc: other@example.org\n"
	if got := getHeader(headers, "From"); got != "Lorem <lorem@example.com>" {
		t.Errorf("From = %q", got)
	}
	if got := getHeader(headers, "To"); got != "dev@example.org" {
		t.Errorf("To = %q", got)
	}
	if got := getHeader(headers, "Cc"); got != "other@example.org" {
		t.Errorf("Cc = %q", got)
	}
	if got := getHeader(headers, "Date"); got != "" {
		t.Errorf("Date = %q, want empty", got)
	}
}

func TestBuildParsedMboxFromPatch_CompactHeaders(t *testing.T) {
	headers := "Reply-To: Lorem Ipsum <lorem@example.com>\n" +
		"To: dev@example.org\n" +
		"Cc: other@example.org\n" +
		"Date: Mon, 13 Oct 2025 13:39:44 +0300\n"
	row := db.PatchRow{
		Name:    "[dev] Fix the widget",
		Content: "This fixes the widget.\n\nSigned-off-by: Lorem",
		Diff:    "--- a/widget.c\n+++ b/widget.c\n@@ -1 +1 @@",
		Headers: headers,
	}
	p := BuildParsedMboxFromPatch(row)
	if p.From != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("From = %q", p.From)
	}
	if p.To != "dev@example.org" {
		t.Errorf("To = %q", p.To)
	}
	if p.Cc != "other@example.org" {
		t.Errorf("Cc = %q", p.Cc)
	}
	if !strings.Contains(p.Date, "13 Oct 2025") {
		t.Errorf("Date = %q", p.Date)
	}
}

func TestBuildParsedMboxFromPatch(t *testing.T) {
	headers := `{
		"Reply-To": "Lorem Ipsum <lorem@example.com>",
		"To": "dev@example.org",
		"Cc": "other@example.org",
		"Date": "Mon, 13 Oct 2025 13:39:44 +0300"
	}`
	row := db.PatchRow{
		Name:    "[dev] Fix the widget",
		Content: "This fixes the widget.\n\nSigned-off-by: Lorem",
		Diff:    "--- a/widget.c\n+++ b/widget.c\n@@ -1 +1 @@",
		Headers: headers,
	}
	p := BuildParsedMboxFromPatch(row)
	if p.Subject != "[dev] Fix the widget" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if p.From != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("From = %q", p.From)
	}
	if p.To != "dev@example.org" {
		t.Errorf("To = %q", p.To)
	}
	if p.Cc != "other@example.org" {
		t.Errorf("Cc = %q", p.Cc)
	}
	if !strings.Contains(p.Date, "13 Oct 2025") {
		t.Errorf("Date = %q", p.Date)
	}
	if p.Body != row.Content {
		t.Errorf("Body = %q", p.Body)
	}
	if p.Diff != row.Diff {
		t.Errorf("Diff = %q", p.Diff)
	}
}

func TestBuildParsedMboxFromPatch_NoHeaders(t *testing.T) {
	row := db.PatchRow{
		Name:           "[dev] Fix the widget",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@example.com",
		Date:           "2025-10-13T13:39:44",
		Content:        "body text",
	}
	p := BuildParsedMboxFromPatch(row)
	if p.Subject != "[dev] Fix the widget" {
		t.Errorf("Subject = %q", p.Subject)
	}
	// Fallback to submitter fields when no headers
	if p.From != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("From = %q, want fallback to submitter", p.From)
	}
	if p.Date != "2025-10-13T13:39:44" {
		t.Errorf("Date = %q, want fallback to row.Date", p.Date)
	}
}

func TestBuildParsedMboxFromPatch_ListMangled(t *testing.T) {
	headers := `{
		"From": "Lorem via dev <dev@lorem.example>",
		"Reply-To": "Lorem Ipsum <lorem@real.com>"
	}`
	row := db.PatchRow{
		Name:    "test",
		Headers: headers,
	}
	p := BuildParsedMboxFromPatch(row)
	if !strings.Contains(p.From, "lorem@real.com") {
		t.Errorf("From = %q, should use Reply-To not mangled From",
			p.From)
	}
}

func TestBuildParsedMboxFromCover(t *testing.T) {
	headers := `{
		"Reply-To": "Lorem Ipsum <lorem@example.com>",
		"To": "dev@example.org",
		"Cc": "other@example.org",
		"Date": "Mon, 13 Oct 2025 13:39:44 +0300"
	}`
	row := db.CoverRow{
		Name:    "[dev] Cover letter",
		Content: "Overview of the series.",
		Headers: headers,
	}
	p := BuildParsedMboxFromCover(row)
	if p.Subject != "[dev] Cover letter" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if p.From != "Lorem Ipsum <lorem@example.com>" {
		t.Errorf("From = %q", p.From)
	}
	if p.Body != "Overview of the series." {
		t.Errorf("Body = %q", p.Body)
	}
}

func TestBuildParsedMboxFromPatch_FoldedHeaders(t *testing.T) {
	headers := `{
		"Subject": "[dev] [PATCH v2]\n Fix the widget",
		"Reply-To": "Lorem Ipsum <lorem@example.com>",
		"To": "a@ex,\n b@ex,\n c@ex",
		"Cc": "d@ex,\n\te@ex"
	}`
	row := db.PatchRow{
		Name:    "[PATCH v2] Fix the widget",
		Headers: headers,
		Content: "body",
	}
	p := BuildParsedMboxFromPatch(row)
	if strings.Contains(p.To, "\n") {
		t.Errorf("To should be compacted, got %q", p.To)
	}
	if strings.Contains(p.Cc, "\n") {
		t.Errorf("Cc should be compacted, got %q", p.Cc)
	}
	if strings.Contains(p.Subject, "\n") {
		t.Errorf("Subject should be compacted, got %q", p.Subject)
	}
}

func TestBuildParsedMboxFromPatch_SubjectFromHeaders(t *testing.T) {
	headers := `{
		"Subject": "[dev] [PATCH v2] Original subject"
	}`
	row := db.PatchRow{
		Name:    "[PATCH v2] Original subject",
		Headers: headers,
		Content: "body",
	}
	p := BuildParsedMboxFromPatch(row)
	if p.Subject != "[dev] [PATCH v2] Original subject" {
		t.Errorf("Subject = %q, want headers version", p.Subject)
	}
}

func TestUpgradeHTTP(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"http://pw.example.com/patch/1/", "https://pw.example.com/patch/1/"},
		{"https://pw.example.com/patch/1/", "https://pw.example.com/patch/1/"},
		{"", ""},
		{"ftp://example.com", "ftp://example.com"},
	}
	for _, tt := range tests {
		got := upgradeHTTP(tt.in)
		if got != tt.want {
			t.Errorf("upgradeHTTP(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildPatchURL_UpgradesHTTP(t *testing.T) {
	got := buildPatchURL("", "", "",
		"http://pw.example.com/patch/1/")
	want := "https://pw.example.com/patch/1/"
	if got != want {
		t.Errorf("buildPatchURL http = %q, want %q", got, want)
	}
}

func TestBuildPatchURL_LorePriority(t *testing.T) {
	got := buildPatchURL(
		"https://lore.example.com/dev",
		"https://archive.example.com/dev/{}",
		"<msg@lorem.example>",
		"https://pw.example.com/patch/1/")
	want := "https://lore.example.com/dev/msg@lorem.example/"
	if got != want {
		t.Errorf("buildPatchURL lore = %q, want %q", got, want)
	}
}

func TestBuildPatchURL_ArchiveFormat(t *testing.T) {
	got := buildPatchURL(
		"",
		"https://archive.example.com/dev/{}",
		"<msg@lorem.example>",
		"https://pw.example.com/patch/1/")
	want := "https://archive.example.com/dev/msg@lorem.example"
	if got != want {
		t.Errorf("buildPatchURL archive = %q, want %q", got, want)
	}
}

func TestBuildPatchURL_WebFallback(t *testing.T) {
	got := buildPatchURL("", "", "<msg@lorem.example>",
		"https://pw.example.com/patch/1/")
	if got != "https://pw.example.com/patch/1/" {
		t.Errorf("buildPatchURL web = %q", got)
	}
}

func TestBuildPatchURL_EmptyMsgID(t *testing.T) {
	got := buildPatchURL(
		"https://lore.example.com/dev",
		"https://archive.example.com/dev/{}",
		"",
		"https://pw.example.com/patch/1/")
	if got != "https://pw.example.com/patch/1/" {
		t.Errorf("buildPatchURL empty msgid = %q", got)
	}
}

func TestBuildPatchURL_LoreTrailingSlash(t *testing.T) {
	got := buildPatchURL(
		"https://lore.example.com/dev/",
		"", "<msg@lorem.example>", "")
	want := "https://lore.example.com/dev/msg@lorem.example/"
	if got != want {
		t.Errorf("buildPatchURL trailing slash = %q, want %q", got, want)
	}
}

func TestFormatChecks_AllFourStates(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/build", State: "success"},
		{Context: "ci/test", State: "fail"},
		{Context: "ci/style", State: "warning"},
		{Context: "ci/deploy", State: "pending"},
	}
	result := FormatChecks(checks, 80, false)
	if !strings.Contains(result, "✓") {
		t.Error("missing success icon ✓")
	}
	if !strings.Contains(result, "✗") {
		t.Error("missing fail icon ✗")
	}
	if !strings.Contains(result, "!") {
		t.Error("missing warning icon !")
	}
	if !strings.Contains(result, "?") {
		t.Error("missing pending icon ?")
	}
	if !strings.Contains(result, "ci/build") {
		t.Error("missing ci/build context")
	}
	if !strings.Contains(result, "ci/deploy") {
		t.Error("missing ci/deploy context")
	}
}

func TestFormatChecks_WarningAndPendingDistinct(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/warn", State: "warning"},
		{Context: "ci/pend", State: "pending"},
	}
	result := FormatChecks(checks, 80, false)
	lines := strings.Split(result, "\n")
	foundWarn, foundPend := false, false
	for _, line := range lines {
		if strings.Contains(line, "ci/warn") && strings.Contains(line, "!") {
			foundWarn = true
		}
		if strings.Contains(line, "ci/pend") && strings.Contains(line, "?") {
			foundPend = true
		}
	}
	if !foundWarn {
		t.Error("warning check should show ! icon")
	}
	if !foundPend {
		t.Error("pending check should show ? icon")
	}
}

func TestFormatChecks_WithDescription(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/build", State: "success",
			TargetURL:   "https://ci.example.com/123",
			Description: "All tests passed"},
	}
	result := FormatChecks(checks, 120, false)
	if !strings.Contains(result, "✓") {
		t.Error("missing success icon")
	}
	if !strings.Contains(result, "ci/build") {
		t.Error("missing context")
	}
	if !strings.Contains(result, "https://ci.example.com/123") {
		t.Error("missing URL")
	}
	if !strings.Contains(result, "All tests passed") {
		t.Error("missing description")
	}
	// Description should be on a separate indented line
	lines := strings.Split(result, "\n")
	foundDesc := false
	for _, line := range lines {
		if strings.Contains(line, "All tests passed") {
			if !strings.HasPrefix(line, "      ") {
				t.Error("description should have 6-space indent")
			}
			foundDesc = true
		}
	}
	if !foundDesc {
		t.Error("description not found on its own line")
	}
}

func TestFormatChecks_MultiLineDescription(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ynl", State: "success",
			Description: "Generated files up to date;\n" +
				"no warnings/errors;\n" +
				"no diff in generated;"},
	}
	result := FormatChecks(checks, 120, false)
	lines := strings.Split(result, "\n")
	descLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "      ") {
			descLines++
		}
	}
	if descLines != 3 {
		t.Errorf("got %d description lines, want 3", descLines)
	}
}

func TestFormatChecks_NoDescription(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/build", State: "success",
			TargetURL: "https://ci.example.com/123"},
	}
	result := FormatChecks(checks, 120, false)
	lines := strings.Split(result, "\n")
	// Should be: header, context, url, empty trailing
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 3 {
		t.Errorf("got %d non-empty lines, want 3 (header + context + url)",
			nonEmpty)
	}
}

func TestFormatChecks_URLAlwaysOnNextLine(t *testing.T) {
	checks := []CheckInfo{
		{Context: "build", State: "success",
			TargetURL:   "https://ci.example.com/build/123",
			Description: "Lorem ipsum"},
	}
	result := FormatChecks(checks, 120, false)
	lines := strings.Split(result, "\n")
	// Context and URL should be on separate lines
	contextLine := stripAnsi(lines[1])
	urlLine := stripAnsi(lines[2])
	if !strings.Contains(contextLine, "build") {
		t.Error("context line missing context name")
	}
	if strings.Contains(contextLine, "https://") {
		t.Error("URL should not be on the context line")
	}
	if !strings.Contains(urlLine, "https://ci.example.com") {
		t.Errorf("URL should be on the next line, got %q", urlLine)
	}
}
