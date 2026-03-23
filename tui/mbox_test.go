package tui

import (
	"fmt"
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
	formatted := FormatComment(c, 120, false)
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
	_ = FormatComment(c, 120, false)
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
	result := FormatComment(c, 80, false)
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
	result := FormatComment(c, 80, false)
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
	result := FormatComment(c, 80, true)
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
	result := FormatComment(c, 80, false)
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
		"?order=date&per_page=100&project=lorem" +
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
	result := FormatComment(c, 120, false)
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
	result := FormatComment(c, 120, false)
	if !strings.Contains(result, "Lorem Ipsum") {
		t.Error("should fall back to Submitter")
	}
	if !strings.Contains(result, "Thu, 04 Dec 2025") {
		t.Error("should convert ISO date to RFC")
	}
}

func TestFormatComment_FromUsesSubmitterEmail(t *testing.T) {
	c := CommentInfo{
		Subject:        "Re: Lorem ipsum",
		Submitter:      "Lorem Ipsum",
		SubmitterEmail: "lorem@ipsum.example",
		Date:           "2025-12-04T09:17:04",
		Content:        "Looks good.",
		Headers: "From: =?utf-8?q?Lorem_via_dev?= <dev@lorem.example>\n" +
			"Date: Thu, 04 Dec 2025 14:57:28 +0530\n",
	}
	result := FormatComment(c, 120, false)
	if !strings.Contains(result, "lorem@ipsum.example") {
		t.Error("should use submitter email, not mangled headers.From")
	}
	if strings.Contains(result, "dev@lorem.example") {
		t.Error("should not use mangled list From")
	}
}

func TestFormatComment_FromFallbackNoEmail(t *testing.T) {
	c := CommentInfo{
		Subject:   "Re: Lorem ipsum",
		Submitter: "Lorem Ipsum",
		Date:      "2025-12-04T09:17:04",
		Content:   "Looks good.",
	}
	result := FormatComment(c, 120, false)
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
	result := FormatComment(c, 120, false)
	if !strings.Contains(result, "Dolör Amet") {
		t.Error("To header should be MIME decoded")
	}
	if !strings.Contains(result, "Sít") {
		t.Error("Cc header should be MIME decoded")
	}
}

func TestFormatMbox_ToHeader(t *testing.T) {
	raw := "Subject: Lorem ipsum\n" +
		"From: Lorem <lorem@ipsum.example>\n" +
		"To: dolor@amet.example, sit@amet.example\n" +
		"Date: Wed, 18 Mar 2026 14:41:13 +0100\n\n" +
		"Body text here."
	p := ParseMbox(raw)
	if p.To == "" {
		t.Fatal("ParseMbox should extract To header")
	}
	result := FormatMbox(p, 120)
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

func TestFormatChecks_AllFourStates(t *testing.T) {
	checks := []CheckInfo{
		{Context: "ci/build", State: "success"},
		{Context: "ci/test", State: "fail"},
		{Context: "ci/style", State: "warning"},
		{Context: "ci/deploy", State: "pending"},
	}
	result := FormatChecks(checks, 80)
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
	result := FormatChecks(checks, 80)
	// Warning should show ! and pending should show ?
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
