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
