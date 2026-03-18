package tui

import (
	"strings"
	"testing"
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

func TestParseMbox_Headers(t *testing.T) {
	p := ParseMbox(testMbox)
	if p.From != "Lorem Ipsum <lorem@ipsum.example>" {
		t.Errorf("From = %q", p.From)
	}
	if p.Subject != "[PATCH v2 1/3] Fix race condition in ovsdb" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if p.Date != "Mon, 10 Mar 2026 12:00:00 +0000" {
		t.Errorf("Date = %q", p.Date)
	}
}

func TestParseMbox_CcMultiline(t *testing.T) {
	p := ParseMbox(testMbox)
	if !strings.Contains(p.Cc, "dolor@amet.example") {
		t.Errorf("Cc = %q, missing dolor", p.Cc)
	}
	if !strings.Contains(p.Cc, "sit@amet.example") {
		t.Errorf("Cc = %q, missing sit", p.Cc)
	}
}

func TestParseMbox_Body(t *testing.T) {
	p := ParseMbox(testMbox)
	if !strings.Contains(p.Body, "race condition") {
		t.Errorf("Body missing content: %q", p.Body)
	}
	if !strings.Contains(p.Body, "Signed-off-by") {
		t.Errorf("Body missing Signed-off-by: %q", p.Body)
	}
}

func TestParseMbox_Diff(t *testing.T) {
	p := ParseMbox(testMbox)
	if !strings.Contains(p.Diff, "diff --git") {
		t.Errorf("Diff missing header: %q", p.Diff)
	}
	if !strings.Contains(p.Diff, "+    new_code()") {
		t.Errorf("Diff missing + line: %q", p.Diff)
	}
	if !strings.Contains(p.Diff, "-    old_code()") {
		t.Errorf("Diff missing - line: %q", p.Diff)
	}
}

func TestParseMbox_NoBody(t *testing.T) {
	raw := "From: lorem@ipsum.example\n" +
		"Subject: empty\n\n"
	p := ParseMbox(raw)
	if p.Body != "" {
		t.Errorf("Body = %q, want empty", p.Body)
	}
}

func TestParseMbox_NoDiff(t *testing.T) {
	raw := "Subject: no diff\n\nJust a message body.\n"
	p := ParseMbox(raw)
	if p.Body != "Just a message body." {
		t.Errorf("Body = %q", p.Body)
	}
	if p.Diff != "" {
		t.Errorf("Diff = %q, want empty", p.Diff)
	}
}

func TestParseMbox_EncodedHeaders(t *testing.T) {
	raw := "From: =?utf-8?q?Lor=C3=A9m_Ips=C3=BAm?= <lorem@ipsum.example>\n" +
		"Subject: =?utf-8?q?[PATCH]_Dol=C3=B3r_amet?=\n" +
		"Cc: =?utf-8?b?Q29uc8OpY3TDqXR1cg==?= <consect@ipsum.example>\n\n"
	p := ParseMbox(raw)
	if !strings.Contains(p.From, "Lorém") {
		t.Errorf("From = %q, want decoded", p.From)
	}
	if !strings.Contains(p.Subject, "Dolór") {
		t.Errorf("Subject = %q, want decoded", p.Subject)
	}
	if !strings.Contains(p.Cc, "Conséctétur") {
		t.Errorf("Cc = %q, want decoded", p.Cc)
	}
}

func TestParseMbox_PlainHeaders(t *testing.T) {
	raw := "From: Lorem <lorem@ipsum.example>\n" +
		"Subject: [PATCH] Plain ASCII\n\n"
	p := ParseMbox(raw)
	if p.From != "Lorem <lorem@ipsum.example>" {
		t.Errorf("From = %q", p.From)
	}
	if p.Subject != "[PATCH] Plain ASCII" {
		t.Errorf("Subject = %q", p.Subject)
	}
}

func TestFormatMbox_NotEmpty(t *testing.T) {
	p := ParseMbox(testMbox)
	formatted := FormatMbox(p, 120)
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
	formatted := FormatComment(c, 120)
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
	// Empty comment with no fields should not panic
	_ = FormatComment(c, 120)
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
	raw := "Subject: test\n\n" +
		"body line\n" +
		"diff --git a/f b/f\n" +
		" }\n \f\n+new code\n"
	p := ParseMbox(raw)
	result := FormatMbox(p, 80)
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
	result := FormatComment(c, 80)
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
	longLine := strings.Repeat("lorem ", 30) // 180 chars
	raw := "Subject: test\n\n" + longLine
	p := ParseMbox(raw)
	result := FormatMbox(p, 80)
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
	result := FormatComment(c, 80)
	if strings.Contains(result, "…") {
		t.Error("comment should wrap, not truncate")
	}
	if !strings.Contains(result, "↳") {
		t.Error("wrapped lines should have ↳ indicator")
	}
}

func TestFormatDiff_StillTruncates(t *testing.T) {
	longDiffLine := "+" + strings.Repeat("x", 200)
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
		"@@ -1 +1 @@\n" + longDiffLine + "\n"
	result := formatDiff(diff, 80)
	if strings.Contains(result, "↳") {
		t.Error("diff should truncate, not wrap")
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
