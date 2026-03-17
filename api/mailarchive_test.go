package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildArchiveURL(t *testing.T) {
	got := BuildArchiveURL(
		"https://mail.example.org/pipermail/dev/",
		2026, time.March)
	want := "https://mail.example.org/pipermail/dev/" +
		"2026-March/date.html"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildArchiveURL_TrailingSlash(t *testing.T) {
	a := BuildArchiveURL(
		"https://mail.example.org/pipermail/dev",
		2026, time.January)
	b := BuildArchiveURL(
		"https://mail.example.org/pipermail/dev/",
		2026, time.January)
	if a != b {
		t.Errorf("trailing slash mismatch: %q vs %q", a, b)
	}
}

const testPipermailHTML = `<!DOCTYPE HTML>
<HTML><HEAD>
<title>The dev March 2026 Archive by date</title>
</HEAD><BODY>
<ul>
<LI><A HREF="430745.html">[ovs-dev] [PATCH net v3] Lorem ipsum dolor sit amet
</A><A NAME="430745">&nbsp;</A>
<I>Lorem Ipsum</I>

<LI><A HREF="430746.html">[ovs-dev] [PATCH 1/3] Consectetur adipiscing elit
</A><A NAME="430746">&nbsp;</A>
<I>Dolor Amet</I>

<LI><A HREF="430800.html">Re: [ovs-dev] [PATCH 1/3] Consectetur adipiscing elit
</A><A NAME="430800">&nbsp;</A>
<I>Sit Amet</I>

<LI><A HREF="430850.html">[ovs-dev] [syzbot ci] Re: Lorem ipsum dolor sit amet
</A><A NAME="430850">&nbsp;</A>
<I>Syzbot</I>

</ul>
</BODY></HTML>`

func TestParseArchiveMessages(t *testing.T) {
	msgs, err := parseArchiveMessages(
		[]byte(testPipermailHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("len = %d, want 4", len(msgs))
	}

	if msgs[0].Number != 430745 {
		t.Errorf("[0].Number = %d", msgs[0].Number)
	}
	want := "[ovs-dev] [PATCH net v3] Lorem ipsum dolor sit amet"
	if msgs[0].Subject != want {
		t.Errorf("[0].Subject = %q", msgs[0].Subject)
	}

	if msgs[2].Number != 430800 {
		t.Errorf("[2].Number = %d", msgs[2].Number)
	}

	if msgs[3].Number != 430850 {
		t.Errorf("[3].Number = %d", msgs[3].Number)
	}
}

func TestParseArchiveMessages_Empty(t *testing.T) {
	html := `<HTML><BODY><ul></ul></BODY></HTML>`
	msgs, err := parseArchiveMessages([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0", len(msgs))
	}
}

func TestExtractPatchCore(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"[ovs-dev] [PATCH v2 1/3] Fix something", "Fix something"},
		{"[ovs-dev,v2,1/3] Fix something", "Fix something"},
		{"[PATCH] Fix something", "Fix something"},
		{"Fix something", "Fix something"},
		{"[a] [b] [c] Fix", "Fix"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ExtractPatchCore(tt.input)
		if got != tt.want {
			t.Errorf("ExtractPatchCore(%q) = %q, want %q",
				tt.input, got, tt.want)
		}
	}
}

func TestSubjectMatch_Verbatim(t *testing.T) {
	tests := []struct {
		core    string
		subject string
		want    bool
	}{
		{
			"Fix race condition in ovsdb",
			"Re: [ovs-dev] [PATCH v2] Fix race condition in ovsdb",
			true,
		},
		{
			"Fix race condition in ovsdb",
			"[ovs-dev] Fix race condition in ovsdb",
			true,
		},
		{
			"Fix race condition",
			"Something completely different",
			false,
		},
		{"", "Re: something", false},
	}
	for _, tt := range tests {
		got := subjectMatch(tt.core, tt.subject)
		if got != tt.want {
			t.Errorf(
				"subjectMatch(%q, %q) = %v, want %v",
				tt.core, tt.subject, got, tt.want)
		}
	}
}

func TestSubjectMatch_ChineseReply(t *testing.T) {
	got := subjectMatch(
		"Fix race condition in ovsdb",
		"回复: [ovs-dev] Fix race condition in ovsdb")
	if !got {
		t.Error("should match Chinese reply prefix")
	}
}

func TestSubjectMatch_JapaneseReply(t *testing.T) {
	got := subjectMatch(
		"Fix race condition",
		"返信: [dev] Fix race condition")
	if !got {
		t.Error("should match Japanese reply prefix")
	}
}

func TestSubjectMatch_NestedReplyFwd(t *testing.T) {
	got := subjectMatch(
		"Fix race condition in ovsdb",
		"Re: Fwd: [ovs-dev] Fix race condition in ovsdb")
	if !got {
		t.Error("should match nested Re: Fwd:")
	}
}

func TestSubjectMatch_Was(t *testing.T) {
	got := subjectMatch(
		"Actual subject here",
		"Re: Fwd: Was: Actual subject here")
	if !got {
		t.Error("should match through Was:")
	}
}

func TestSubjectMatch_Truncated(t *testing.T) {
	got := subjectMatch(
		"Fix race condition in ovsdb server",
		"Re: [ovs-dev] Fix race condition in ovsdb serv")
	if !got {
		t.Error("should match truncated subject (>80%)")
	}
}

func TestSubjectMatch_TooShortTruncation(t *testing.T) {
	got := subjectMatch(
		"Fix race condition in ovsdb server",
		"Re: [ovs-dev] Fix race")
	if got {
		t.Error("should NOT match heavily truncated (<80%)")
	}
}

func TestSubjectMatch_CaseInsensitive(t *testing.T) {
	got := subjectMatch(
		"Fix Race Condition",
		"RE: [DEV] fix race condition")
	if !got {
		t.Error("should match case-insensitively")
	}
}

func TestMatchPatchSubjects(t *testing.T) {
	msgs := []ArchiveMessage{
		{Number: 100,
			Subject: "Re: [ovs-dev] [PATCH] Fix bug"},
		{Number: 101,
			Subject: "[ovs-dev] Unrelated topic"},
		{Number: 102,
			Subject: "Re: [ovs-dev] [PATCH v2] Add feature"},
	}

	patchNames := map[int]string{
		1000: "[ovs-dev,v1] Fix bug",
		1001: "[ovs-dev] [PATCH v2] Add feature",
		1002: "[ovs-dev] Something else entirely",
	}

	ids := MatchPatchSubjects(msgs, patchNames)
	got := map[int]bool{}
	for _, id := range ids {
		got[id] = true
	}

	if !got[1000] {
		t.Error("patch 1000 should match (Fix bug)")
	}
	if !got[1001] {
		t.Error("patch 1001 should match (Add feature)")
	}
	if got[1002] {
		t.Error("patch 1002 should not match")
	}
}

func TestMatchPatchSubjects_NoMatches(t *testing.T) {
	msgs := []ArchiveMessage{
		{Number: 100, Subject: "[dev] Totally different"},
	}
	patchNames := map[int]string{
		1000: "[dev] Some patch name",
	}
	ids := MatchPatchSubjects(msgs, patchNames)
	if len(ids) != 0 {
		t.Errorf("got %v, want empty", ids)
	}
}

func TestLongestCommonSubstring(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"abcdef", "xbcdey", 4},
		{"hello world", "hello world", 11},
		{"abc", "xyz", 0},
		{"", "abc", 0},
		{"fix race condition", "fix race cond", 13},
	}
	for _, tt := range tests {
		got := longestCommonSubstring(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("lcs(%q, %q) = %d, want %d",
				tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFetchArchiveMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(testPipermailHTML))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	msgs, err := c.FetchArchiveMessages(
		context.Background(), srv.URL+"/date.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Errorf("len = %d, want 4", len(msgs))
	}
}

func TestFetchArchiveMessages_Anubis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Making sure you're not a bot!"))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	_, err := c.FetchArchiveMessages(
		context.Background(), srv.URL+"/date.html")
	if err == nil {
		t.Error("expected error for Anubis page")
	}
}

func TestFilterNewMessages(t *testing.T) {
	msgs := []ArchiveMessage{
		{Number: 100, Subject: "Old"},
		{Number: 200, Subject: "Also old"},
		{Number: 300, Subject: "New one"},
		{Number: 400, Subject: "Newer"},
	}

	filtered := FilterNewMessages(msgs, 200)
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
	if filtered[0].Number != 300 {
		t.Errorf("[0].Number = %d", filtered[0].Number)
	}
	if filtered[1].Number != 400 {
		t.Errorf("[1].Number = %d", filtered[1].Number)
	}
}

func TestFilterNewMessages_AllNew(t *testing.T) {
	msgs := []ArchiveMessage{
		{Number: 100, Subject: "A"},
		{Number: 200, Subject: "B"},
	}
	filtered := FilterNewMessages(msgs, 0)
	if len(filtered) != 2 {
		t.Errorf("len = %d, want 2", len(filtered))
	}
}

func TestFilterNewMessages_NoneNew(t *testing.T) {
	msgs := []ArchiveMessage{
		{Number: 100, Subject: "A"},
	}
	filtered := FilterNewMessages(msgs, 500)
	if len(filtered) != 0 {
		t.Errorf("len = %d, want 0", len(filtered))
	}
}
