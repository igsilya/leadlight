package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildListURL_WithStates(t *testing.T) {
	got := BuildListURL(
		"https://pw.example.com",
		"lorem-project",
		[]string{"new", "under-review"},
	)
	want := "https://pw.example.com" +
		"/project/lorem-project/list/" +
		"?state=new&state=under-review"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildListURL_NoStates(t *testing.T) {
	got := BuildListURL(
		"https://pw.example.com",
		"lorem-project",
		nil,
	)
	want := "https://pw.example.com" +
		"/project/lorem-project/list/"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildListURL_SingleState(t *testing.T) {
	got := BuildListURL(
		"https://pw.example.com",
		"lorem",
		[]string{"accepted"},
	)
	want := "https://pw.example.com" +
		"/project/lorem/list/?state=accepted"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

const testPatchRowHTML = `<html><body><table><tbody>
<tr id="patch_row:12345">
 <td>
  <a href="/project/lorem/patch/abc123/">
   [dev] Lorem ipsum dolor sit amet
  </a>
 </td>
 <td>
  <a href="?series=678">
   [dev] Lorem ipsum series
  </a>
 </td>
 <td class="text-nowrap">
  <span title="0 Acked-by / 0 Fixes / 0 Reviewed-by / 0 Tested-by">
   - - - -
  </span>
 </td>
 <td class="text-nowrap">
  <span title="Success / Warning / Fail">
   <span class="patchlistchecks ">-</span>
   <span class="patchlistchecks ">-</span>
   <span class="patchlistchecks ">-</span>
  </span>
 </td>
 <td class="text-nowrap">2026-03-10</td>
 <td><a href="?submitter=100">Lorem Ipsum</a></td>
 <td></td>
 <td>New</td>
</tr>
<tr id="patch_row:12346">
 <td>
  <a href="/project/lorem/patch/def456/">
   [dev,2/3] Consectetur adipiscing elit
  </a>
 </td>
 <td>
  <a href="?series=679">
   [dev] Dolor amet series
  </a>
 </td>
 <td class="text-nowrap">
  <span title="1 Acked-by / 0 Fixes / 2 Reviewed-by / 0 Tested-by">
   1 - 2 -
  </span>
 </td>
 <td class="text-nowrap">
  <span title="Success / Warning / Fail">
   <span class="patchlistchecks success">3</span>
   <span class="patchlistchecks ">-</span>
   <span class="patchlistchecks fail">1</span>
  </span>
 </td>
 <td class="text-nowrap">2026-03-09</td>
 <td><a href="?submitter=200">Dolor Amet</a></td>
 <td>sitamet</td>
 <td>Under Review</td>
</tr>
</tbody></table></body></html>`

func TestParsePatchRows(t *testing.T) {
	page, err := parseListPage([]byte(testPatchRowHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Patches) != 2 {
		t.Fatalf("len = %d, want 2", len(page.Patches))
	}

	p := page.Patches[0]
	if p.PatchID != 12345 {
		t.Errorf("PatchID = %d", p.PatchID)
	}
	if p.Name != "[dev] Lorem ipsum dolor sit amet" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.SeriesID != 678 {
		t.Errorf("SeriesID = %d", p.SeriesID)
	}
	if p.SeriesName != "[dev] Lorem ipsum series" {
		t.Errorf("SeriesName = %q", p.SeriesName)
	}
	if p.Date != "2026-03-10" {
		t.Errorf("Date = %q", p.Date)
	}
	if p.Submitter != "Lorem Ipsum" {
		t.Errorf("Submitter = %q", p.Submitter)
	}
	if p.State != "New" {
		t.Errorf("State = %q", p.State)
	}
	if p.Delegate != "" {
		t.Errorf("Delegate = %q, want empty", p.Delegate)
	}
}

func TestParsePatchRows_WithChecks(t *testing.T) {
	page, err := parseListPage([]byte(testPatchRowHTML))
	if err != nil {
		t.Fatal(err)
	}

	p := page.Patches[1]
	if p.ChecksPass != 3 {
		t.Errorf("ChecksPass = %d", p.ChecksPass)
	}
	if p.ChecksWarn != 0 {
		t.Errorf("ChecksWarn = %d", p.ChecksWarn)
	}
	if p.ChecksFail != 1 {
		t.Errorf("ChecksFail = %d", p.ChecksFail)
	}

	// First row has all dashes
	p0 := page.Patches[0]
	if p0.ChecksPass != 0 || p0.ChecksWarn != 0 || p0.ChecksFail != 0 {
		t.Errorf("row 0 checks = %d/%d/%d",
			p0.ChecksPass, p0.ChecksWarn, p0.ChecksFail)
	}
}

func TestParsePatchRows_WithDelegate(t *testing.T) {
	page, err := parseListPage([]byte(testPatchRowHTML))
	if err != nil {
		t.Fatal(err)
	}

	if page.Patches[1].Delegate != "sitamet" {
		t.Errorf("Delegate = %q", page.Patches[1].Delegate)
	}
}

func TestParsePatchRows_PartialColumns(t *testing.T) {
	html := `<html><body><table><tbody>
<tr id="patch_row:99999">
 <td>
  <a href="/project/lorem/patch/abc/">
   [dev] Partial row with fewer columns
  </a>
 </td>
 <td>
  <a href="?series=111">Partial series</a>
 </td>
</tr>
</tbody></table></body></html>`
	page, err := parseListPage([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Patches) != 1 {
		t.Fatalf("len = %d, want 1", len(page.Patches))
	}
	p := page.Patches[0]
	if p.PatchID != 99999 {
		t.Errorf("PatchID = %d", p.PatchID)
	}
	if p.Name != "[dev] Partial row with fewer columns" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.SeriesID != 111 {
		t.Errorf("SeriesID = %d", p.SeriesID)
	}
	// Fields beyond available columns should be zero/empty
	if p.State != "" {
		t.Errorf("State = %q, want empty", p.State)
	}
	if p.Date != "" {
		t.Errorf("Date = %q, want empty", p.Date)
	}
}

func TestParsePatchRows_ExtraColumns(t *testing.T) {
	html := `<html><body><table><tbody>
<tr id="patch_row:88888">
 <td><a href="/p/">Lorem patch</a></td>
 <td><a href="?series=222">Lorem series</a></td>
 <td class="text-nowrap"><span title="1 Acked-by / 0 Reviewed-by / 0 Tested-by">1 - -</span></td>
 <td class="text-nowrap"><span title="S/W/F"><span class="patchlistchecks success">2</span><span class="patchlistchecks ">-</span><span class="patchlistchecks ">-</span></span></td>
 <td class="text-nowrap">2026-03-10</td>
 <td><a href="?s=1">Lorem</a></td>
 <td>ipsum</td>
 <td>New</td>
 <td>Extra future column</td>
 <td>Another extra</td>
</tr>
</tbody></table></body></html>`
	page, err := parseListPage([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Patches) != 1 {
		t.Fatalf("len = %d", len(page.Patches))
	}
	p := page.Patches[0]
	if p.State != "New" {
		t.Errorf("State = %q", p.State)
	}
}

const testDelegateHTML = `<html><body>
<select name="delegate" class="form-control">
 <option selected value="">------</option>
 <option value="Nobody">Nobody</option>
 <option value="42">lorem</option>
 <option value="55">ipsum</option>
 <option value="78">dolor</option>
</select>
</body></html>`

func TestParseDelegateOptions(t *testing.T) {
	page, err := parseListPage([]byte(testDelegateHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Delegates) != 3 {
		t.Fatalf("len = %d, want 3", len(page.Delegates))
	}
	if page.Delegates[0].ID != 42 {
		t.Errorf("[0].ID = %d", page.Delegates[0].ID)
	}
	if page.Delegates[0].Username != "lorem" {
		t.Errorf("[0].Username = %q", page.Delegates[0].Username)
	}
	if page.Delegates[2].ID != 78 {
		t.Errorf("[2].ID = %d", page.Delegates[2].ID)
	}
}

func TestParseDelegateOptions_SkipsSpecial(t *testing.T) {
	html := `<html><body>
<select name="delegate">
 <option selected value="">------</option>
 <option value="Nobody">Nobody</option>
</select>
</body></html>`
	page, err := parseListPage([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Delegates) != 0 {
		t.Errorf("len = %d, want 0", len(page.Delegates))
	}
}

const testNextPageHTML = `<html><body>
<span class="next">
 <a href="/project/lorem/list/?state=*&amp;page=2"
  title="Next Page">&raquo;</a>
</span>
</body></html>`

func TestParseNextPageURL(t *testing.T) {
	page, err := parseListPage([]byte(testNextPageHTML))
	if err != nil {
		t.Fatal(err)
	}
	want := "/project/lorem/list/?state=*&page=2"
	if page.NextURL != want {
		t.Errorf("NextURL = %q, want %q", page.NextURL, want)
	}
}

func TestParseNextPageURL_NoNext(t *testing.T) {
	html := `<html><body><p>no pagination</p></body></html>`
	page, err := parseListPage([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if page.NextURL != "" {
		t.Errorf("NextURL = %q, want empty", page.NextURL)
	}
}

func TestFetchListPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(testPatchRowHTML))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	page, err := c.FetchListPage(
		context.Background(), srv.URL+"/list/")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Patches) != 2 {
		t.Errorf("len(Patches) = %d", len(page.Patches))
	}
	if page.Patches[0].PatchID != 12345 {
		t.Errorf("[0].PatchID = %d", page.Patches[0].PatchID)
	}
}

func TestFetchListPage_NotHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Making sure you're not a bot!"))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	_, err := c.FetchListPage(
		context.Background(), srv.URL+"/list/")
	if err == nil {
		t.Error("expected error for non-patchwork page")
	}
}

func TestFetchListPage_EmptyTable(t *testing.T) {
	html := `<html><body>
<table><tbody></tbody></table>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(html))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	page, err := c.FetchListPage(
		context.Background(), srv.URL+"/list/")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Patches) != 0 {
		t.Errorf("len(Patches) = %d, want 0",
			len(page.Patches))
	}
}
