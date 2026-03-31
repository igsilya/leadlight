package tui

import (
	"strings"
	"testing"

	"leadlight/db"
)

func TestPatchNumber(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"[PATCH 1/3] fix thing", 1},
		{"[PATCH 3/3] last patch", 3},
		{"[PATCH v2 2/5] second patch", 2},
		{"[PATCH v10 10/15] tenth", 10},
		{"[PATCH 0/3] cover letter", 0},
		{"[PATCH] single patch no number", 0},
		{"just a subject line", 0},
		{"[RFC PATCH 1/2] rfc patch", 1},
		{"[PATCH net-next v3 4/7] net: fix", 4},
	}
	for _, tt := range tests {
		got := patchNumber(tt.name)
		if got != tt.want {
			t.Errorf("patchNumber(%q) = %d, want %d",
				tt.name, got, tt.want)
		}
	}
}

func TestInjectTags_BeforeSignoff(t *testing.T) {
	body := "Fix the broken thing.\n\nSigned-off-by: Author <a@ex>\n"
	comment := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	got := injectTags(body, comment, nil, "")
	// Reviewed-by should appear before Signed-off-by
	rIdx := strings.Index(got, "Reviewed-by:")
	sIdx := strings.Index(got, "Signed-off-by:")
	if rIdx < 0 || sIdx < 0 || rIdx > sIdx {
		t.Errorf("Reviewed-by should be before Signed-off-by:\n%s", got)
	}
}

func TestInjectTags_BeforeCoAuthored(t *testing.T) {
	body := "Fix thing.\n\nSigned-off-by: Author <a@ex>\nCo-developed-by: Helper <h@ex>\n"
	comment := []db.TagRow{
		{Type: "acked-by", Identity: "Acker <ack@ex>"},
	}
	got := injectTags(body, comment, nil, "")
	aIdx := strings.Index(got, "Acked-by:")
	sIdx := strings.Index(got, "Signed-off-by:")
	cIdx := strings.Index(got, "Co-developed-by:")
	if aIdx < 0 || sIdx < 0 || cIdx < 0 || aIdx > sIdx {
		t.Errorf("Acked-by should be before Signed-off-by:\n%s", got)
	}
	if aIdx > cIdx {
		t.Errorf("Acked-by should be before Co-developed-by:\n%s", got)
	}
}

func TestInjectTags_FixesOnTop(t *testing.T) {
	body := "Fix thing.\n\nAcked-by: Existing <e@ex>\nSigned-off-by: Author <a@ex>\n"
	comment := []db.TagRow{
		{Type: "fixes", Identity: "abcdef12345 (\"broken commit\")"},
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	got := injectTags(body, comment, nil, "")
	fIdx := strings.Index(got, "Fixes:")
	aIdx := strings.Index(got, "Acked-by:")
	rIdx := strings.Index(got, "Reviewed-by:")
	sIdx := strings.Index(got, "Signed-off-by:")
	if fIdx < 0 || aIdx < 0 || rIdx < 0 || sIdx < 0 {
		t.Fatalf("missing tags:\n%s", got)
	}
	if fIdx > aIdx {
		t.Errorf("Fixes should be before Acked-by:\n%s", got)
	}
	if rIdx > sIdx {
		t.Errorf("Reviewed-by should be before Signed-off-by:\n%s", got)
	}
}

func TestInjectTags_Dedup(t *testing.T) {
	body := "Fix thing.\n\nReviewed-by: Reviewer <r@ex>\nSigned-off-by: Author <a@ex>\n"
	comment := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	original := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	got := injectTags(body, comment, original, "")
	count := strings.Count(got, "Reviewed-by:")
	if count != 1 {
		t.Errorf("should not duplicate existing tag, got %d occurrences:\n%s",
			count, got)
	}
}

func TestInjectTags_Order(t *testing.T) {
	body := "Fix thing.\n\nSigned-off-by: Author <a@ex>\n"
	comment := []db.TagRow{
		{Type: "tested-by", Identity: "Tester <t@ex>"},
		{Type: "acked-by", Identity: "Acker <ack@ex>"},
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
		{Type: "fixes", Identity: "abcdef (\"broken\")"},
	}
	got := injectTags(body, comment, nil, "")
	fIdx := strings.Index(got, "Fixes:")
	aIdx := strings.Index(got, "Acked-by:")
	rIdx := strings.Index(got, "Reviewed-by:")
	tIdx := strings.Index(got, "Tested-by:")
	sIdx := strings.Index(got, "Signed-off-by:")
	if fIdx > aIdx || aIdx > rIdx || rIdx > tIdx || tIdx > sIdx {
		t.Errorf("wrong order (want Fixes < Acked < Reviewed < Tested < Signoff):\n%s", got)
	}
}

func TestInjectTags_NoTrailers(t *testing.T) {
	body := "Fix thing.\n\nSome more explanation.\n"
	comment := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	got := injectTags(body, comment, nil, "")
	if !strings.Contains(got, "Reviewed-by: Reviewer <r@ex>") {
		t.Errorf("should add tag:\n%s", got)
	}
}

func TestInjectTags_RemoveUserSignoff(t *testing.T) {
	body := "Fix thing.\n\nReviewed-by: Reviewer <r@ex>\nSigned-off-by: Author <a@ex>\nSigned-off-by: Other <o@ex>\n"
	got := injectTags(body, nil, nil, "Signed-off-by: Author <a@ex>")
	if strings.Contains(got, "Signed-off-by: Author <a@ex>") {
		t.Errorf("should remove matching signoff:\n%s", got)
	}
	if !strings.Contains(got, "Signed-off-by: Other <o@ex>") {
		t.Errorf("should keep non-matching signoff:\n%s", got)
	}
}

func TestInjectTags_CoverTags(t *testing.T) {
	body := "Fix thing.\n\nSigned-off-by: Author <a@ex>\n"
	// Cover comment tags should be applied to individual patches
	coverTags := []db.TagRow{
		{Type: "acked-by", Identity: "Maintainer <m@ex>"},
	}
	patchTags := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
	}
	allTags := append(coverTags, patchTags...)
	got := injectTags(body, allTags, nil, "")
	if !strings.Contains(got, "Acked-by: Maintainer <m@ex>") {
		t.Errorf("should include cover tag:\n%s", got)
	}
	if !strings.Contains(got, "Reviewed-by: Reviewer <r@ex>") {
		t.Errorf("should include patch tag:\n%s", got)
	}
}

func TestInjectTags_DedupWithinComments(t *testing.T) {
	body := "Fix thing.\n\nSigned-off-by: Author <a@ex>\n"
	comment := []db.TagRow{
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"},
		{Type: "reviewed-by", Identity: "Reviewer <r@ex>"}, // dup
	}
	got := injectTags(body, comment, nil, "")
	count := strings.Count(got, "Reviewed-by:")
	if count != 1 {
		t.Errorf("should dedup within comment tags, got %d:\n%s",
			count, got)
	}
}

func TestConstructApplyMbox_Single(t *testing.T) {
	patches := []applyPatch{{
		From:    "Author <a@ex>",
		Date:    "2026-03-10T12:00:00",
		Subject: "[PATCH] Fix thing",
		MsgID:   "<123@ex>",
		Body:    "Fix the broken thing.\n\nSigned-off-by: Author <a@ex>\n",
		Diff:    " file.c | 2 +-\n\ndiff --git a/file.c b/file.c\n",
	}}
	got, err := constructApplyMbox(patches)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "From <123@ex>") {
		t.Errorf("should start with From line:\n%s", got)
	}
	if !strings.Contains(got, "From: Author <a@ex>") {
		t.Errorf("should have From header:\n%s", got)
	}
	if !strings.Contains(got, "Subject: [PATCH] Fix thing") {
		t.Errorf("should have Subject header:\n%s", got)
	}
	if !strings.Contains(got, "---\n") {
		t.Errorf("should have --- separator:\n%s", got)
	}
	if !strings.Contains(got, "diff --git") {
		t.Errorf("should contain the diff:\n%s", got)
	}
}

func TestConstructApplyMbox_Series(t *testing.T) {
	patches := []applyPatch{
		{
			From: "Author <a@ex>", Date: "2026-03-10",
			Subject: "[PATCH 1/2] First", MsgID: "<1@ex>",
			Body: "First change.\n", Diff: "diff --git a/1\n",
		},
		{
			From: "Author <a@ex>", Date: "2026-03-10",
			Subject: "[PATCH 2/2] Second", MsgID: "<2@ex>",
			Body: "Second change.\n", Diff: "diff --git a/2\n",
		},
	}
	got, err := constructApplyMbox(patches)
	if err != nil {
		t.Fatal(err)
	}
	// Should have two From lines
	count := strings.Count(got, "From <")
	if count != 2 {
		t.Errorf("should have 2 From lines, got %d:\n%s", count, got)
	}
	// First patch should come before second
	idx1 := strings.Index(got, "[PATCH 1/2]")
	idx2 := strings.Index(got, "[PATCH 2/2]")
	if idx1 > idx2 {
		t.Errorf("patches should be in order:\n%s", got)
	}
}

func TestConstructApplyMbox_GenerateMsgID(t *testing.T) {
	patches := []applyPatch{{
		From: "Author <a@ex>", Date: "2026-03-10",
		Subject: "[PATCH] Fix", MsgID: "",
		Body: "Fix.\n", Diff: "diff --git a/f\n",
	}}
	got, err := constructApplyMbox(patches)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Message-Id: <leadlight-") {
		t.Errorf("should generate Message-Id:\n%s", got)
	}
}

func TestConstructApplyMbox_EmptyDiff(t *testing.T) {
	patches := []applyPatch{{
		From: "Author <a@ex>", Date: "2026-03-10",
		Subject: "[PATCH] Fix", MsgID: "<1@ex>",
		Body: "Fix.\n", Diff: "",
	}}
	_, err := constructApplyMbox(patches)
	if err == nil {
		t.Error("should error on empty diff")
	}
}
