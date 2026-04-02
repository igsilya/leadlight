package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := &Client{
		baseURL:    srv.URL,
		project:    "test-project",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}
	return c
}

func TestRateLimit(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			w.Write([]byte(`{}`))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
		minDelay:   50 * time.Millisecond,
	}

	ctx := context.Background()
	start := time.Now()
	c.getJSON(ctx, "/first", nil, &struct{}{})
	c.getJSON(ctx, "/second", nil, &struct{}{})
	elapsed := time.Since(start)

	if count.Load() != 2 {
		t.Errorf("count = %d, want 2", count.Load())
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 50ms", elapsed)
	}
}

func TestAuthToken(t *testing.T) {
	var gotAuth string
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte(`{}`))
		}))
	c.token = "abc123"

	c.getJSON(context.Background(), "/test", nil, &struct{}{})
	if gotAuth != "Token abc123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestAuthBasic(t *testing.T) {
	var gotUser, gotPass string
	var gotOK bool
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotPass, gotOK = r.BasicAuth()
			w.Write([]byte(`{}`))
		}))
	c.username = "lorem"
	c.password = "ipsum"

	c.getJSON(context.Background(), "/test", nil, &struct{}{})
	if !gotOK || gotUser != "lorem" || gotPass != "ipsum" {
		t.Errorf("basic auth = %q/%q/%v", gotUser, gotPass, gotOK)
	}
}

func TestAuthNone(t *testing.T) {
	var gotAuth string
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte(`{}`))
		}))

	c.getJSON(context.Background(), "/test", nil, &struct{}{})
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty", gotAuth)
	}
}

func TestGetJSON(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1,"name":"lorem"}`))
		}))

	var result struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	err := c.getJSON(context.Background(), "/test", nil, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != 1 || result.Name != "lorem" {
		t.Errorf("result = %+v", result)
	}
}

func TestGetJSON_HTTPError(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"detail":"not found"}`))
		}))

	err := c.getJSON(context.Background(), "/missing", nil, &struct{}{})
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestPagination(t *testing.T) {
	// Need server URL for Link headers; create server first
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			switch page {
			case "", "1":
				w.Header().Set("Link",
					fmt.Sprintf(
						`<%s/items/?page=2>; rel="next"`,
						srvURL))
				w.Write([]byte(`[{"id":1},{"id":2}]`))
			case "2":
				w.Header().Set("Link",
					fmt.Sprintf(
						`<%s/items/?page=3>; rel="next"`,
						srvURL))
				w.Write([]byte(`[{"id":3}]`))
			case "3":
				w.Write([]byte(`[{"id":4}]`))
			}
		}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	c := &Client{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	type item struct {
		ID int `json:"id"`
	}
	results, err := getAll[item](
		c, context.Background(), "/items/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("len = %d, want 4", len(results))
	}
	for i, want := range []int{1, 2, 3, 4} {
		if results[i].ID != want {
			t.Errorf("[%d].ID = %d, want %d", i, results[i].ID, want)
		}
	}
}

func TestPagination_NoLink(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`[{"id":1}]`))
		}))

	type item struct {
		ID int `json:"id"`
	}
	results, err := getAll[item](
		c, context.Background(), "/items/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
}

func TestParseLinkNext(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{
			`<https://pw.example.com/api/1.2/patches/?page=2>; rel="next"`,
			"https://pw.example.com/api/1.2/patches/?page=2",
		},
		{
			`<https://pw.example.com/?page=2>; rel="next", ` +
				`<https://pw.example.com/?page=5>; rel="last"`,
			"https://pw.example.com/?page=2",
		},
		{
			`<https://pw.example.com/?page=5>; rel="last"`,
			"",
		},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseLinkNext(tt.header)
		if got != tt.want {
			t.Errorf("parseLinkNext(%q) = %q, want %q",
				tt.header, got, tt.want)
		}
	}
}

func TestParseLinkLast(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{
			`<https://pw.example.com/?page=2>; rel="next", ` +
				`<https://pw.example.com/?page=15>; rel="last"`,
			"https://pw.example.com/?page=15",
		},
		{
			`<https://pw.example.com/?page=2>; rel="next"`,
			"",
		},
		{
			`<https://pw.example.com/?page=1>; rel="first", ` +
				`<https://pw.example.com/?page=3475>; rel="last"`,
			"https://pw.example.com/?page=3475",
		},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseLinkLast(tt.header)
		if got != tt.want {
			t.Errorf("parseLinkLast(%q) = %q, want %q",
				tt.header, got, tt.want)
		}
	}
}

func TestExtractPageCount(t *testing.T) {
	tests := []struct {
		url  string
		want int
	}{
		{"https://pw.example.com/api/1.3/series/?page=3475&per_page=10", 3475},
		{"https://pw.example.com/api/1.3/patches/?page=15", 15},
		{"https://pw.example.com/api/1.3/patches/", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := extractPageCount(tt.url)
		if got != tt.want {
			t.Errorf("extractPageCount(%q) = %d, want %d",
				tt.url, got, tt.want)
		}
	}
}

func TestGetProject(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/projects/test-proj/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode(Project{
				ProjectSummary: ProjectSummary{
					ID:   1,
					Name: "Lorem Project",
				},
				Maintainers: []User{
					{ID: 10, Username: "lorem"},
					{ID: 20, Username: "ipsum"},
				},
			})
		}))

	p, err := c.GetProject(context.Background(), "test-proj")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Lorem Project" {
		t.Errorf("Name = %q", p.Name)
	}
	if len(p.Maintainers) != 2 {
		t.Errorf("len(Maintainers) = %d", len(p.Maintainers))
	}
}

func TestGetPatches(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("project") != "test-project" {
				t.Errorf("project = %q", r.URL.Query().Get("project"))
			}
			states := r.URL.Query()["state"]
			if len(states) != 2 {
				t.Errorf("states = %v", states)
			}
			json.NewEncoder(w).Encode([]Patch{
				{ID: 1, Name: "Lorem patch", State: "new"},
				{ID: 2, Name: "Ipsum patch", State: "under-review"},
			})
		}))

	params := PatchListParams{
		State:   []string{"new", "under-review"},
		Project: "test-project",
	}
	patches, err := c.GetPatches(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("len = %d", len(patches))
	}
	if patches[0].Name != "Lorem patch" {
		t.Errorf("[0].Name = %q", patches[0].Name)
	}
}

func TestGetPatch(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/patches/100/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode(PatchDetail{
				Patch: Patch{
					ID:   100,
					Name: "Lorem detail",
				},
				Content: "Lorem ipsum dolor sit amet.",
				Diff:    "--- a/lorem\n+++ b/lorem",
			})
		}))

	pd, err := c.GetPatch(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if pd.Content != "Lorem ipsum dolor sit amet." {
		t.Errorf("Content = %q", pd.Content)
	}
}

func TestGetPatchComments(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/patches/100/comments/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode([]Comment{
				{ID: 1, Subject: "Re: lorem", Content: "Looks good."},
				{ID: 2, Subject: "Re: lorem", Content: "Agreed."},
			})
		}))

	comments, err := c.GetPatchComments(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("len = %d", len(comments))
	}
}

func TestGetCoverComments(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/covers/99/comments/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode([]Comment{
				{ID: 10, Subject: "Re: lorem cover", Content: "Acked-by: Dolor <dolor@amet.example>"},
			})
		}))

	comments, err := c.GetCoverComments(context.Background(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("len = %d", len(comments))
	}
	if comments[0].ID != 10 {
		t.Errorf("[0].ID = %d", comments[0].ID)
	}
}

func TestGetPatchChecks(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/patches/100/checks/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode([]Check{
				{ID: 1, State: "success", Context: "ci/build"},
				{ID: 2, State: "fail", Context: "ci/test"},
			})
		}))

	checks, err := c.GetPatchChecks(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Fatalf("len = %d", len(checks))
	}
	if checks[1].State != "fail" {
		t.Errorf("[1].State = %q", checks[1].State)
	}
}

func TestGetSeries(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/series/50/" {
				t.Errorf("path = %q", r.URL.Path)
			}
			json.NewEncoder(w).Encode(Series{
				ID:      50,
				Name:    "Lorem series",
				Total:   3,
				Patches: []PatchSummary{{ID: 1}, {ID: 2}, {ID: 3}},
			})
		}))

	s, err := c.GetSeries(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Lorem series" {
		t.Errorf("Name = %q", s.Name)
	}
	if len(s.Patches) != 3 {
		t.Errorf("len(Patches) = %d", len(s.Patches))
	}
}

func TestGetEvents(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("since") != "2026-03-10" {
				t.Errorf("since = %q", r.URL.Query().Get("since"))
			}
			w.Write([]byte(`[
				{
					"id": 1,
					"category": "patch-created",
					"project": ` + testProjectJSON + `,
					"date": "2026-03-10T12:00:00",
					"actor": null,
					"payload": {
						"patch": {
							"id": 100,
							"url": "",
							"web_url": "",
							"msgid": "",
							"list_archive_url": null,
							"date": "",
							"name": "Lorem",
							"mbox": ""
						}
					}
				}
			]`))
		}))

	params := EventListParams{
		Since:   "2026-03-10",
		Project: "test-project",
	}
	events, err := c.GetEvents(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("len = %d", len(events))
	}
	p, ok := events[0].Payload.(*PatchCreatedPayload)
	if !ok {
		t.Fatalf("Payload type = %T", events[0].Payload)
	}
	if p.Patch.ID != 100 {
		t.Errorf("Patch.ID = %d", p.Patch.ID)
	}
}

func TestUpdatePatch(t *testing.T) {
	var gotMethod string
	var gotBody map[string]interface{}
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			json.NewDecoder(r.Body).Decode(&gotBody)
			json.NewEncoder(w).Encode(PatchDetail{
				Patch: Patch{
					ID:    100,
					State: "accepted",
				},
			})
		}))
	c.token = "write-token"

	state := "accepted"
	result, err := c.UpdatePatch(
		context.Background(), 100,
		PatchUpdate{State: &state})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "PATCH" {
		t.Errorf("method = %q", gotMethod)
	}
	if gotBody["state"] != "accepted" {
		t.Errorf("body = %v", gotBody)
	}
	if result.State != "accepted" {
		t.Errorf("result.State = %q", result.State)
	}
}

func TestPatchUpdate_MarshalJSON(t *testing.T) {
	state := "new"
	delegate := 42

	tests := []struct {
		name string
		u    PatchUpdate
		want string
	}{
		{"state only",
			PatchUpdate{State: &state},
			`{"state":"new"}`},
		{"delegate set",
			PatchUpdate{Delegate: &delegate},
			`{"delegate":42}`},
		{"unset delegate",
			PatchUpdate{UnsetDelegate: true},
			`{"delegate":null}`},
		{"state + delegate",
			PatchUpdate{State: &state, Delegate: &delegate},
			`{"delegate":42,"state":"new"}`},
		{"empty",
			PatchUpdate{},
			`{}`},
	}
	for _, tt := range tests {
		data, err := json.Marshal(tt.u)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if string(data) != tt.want {
			t.Errorf("%s: got %s, want %s",
				tt.name, data, tt.want)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	c := testClient(t, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Second)
			w.Write([]byte(`{}`))
		}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.getJSON(ctx, "/slow", nil, &struct{}{})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestFixScheme_HttpToHttps(t *testing.T) {
	c := &Client{baseURL: "https://pw.example.com/api/1.2"}
	got := c.fixScheme(
		"http://pw.example.com/api/1.2/patches/?page=2")
	want := "https://pw.example.com/api/1.2/patches/?page=2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFixScheme_AlreadyHttps(t *testing.T) {
	c := &Client{baseURL: "https://pw.example.com/api/1.2"}
	got := c.fixScheme(
		"https://pw.example.com/api/1.2/patches/?page=2")
	want := "https://pw.example.com/api/1.2/patches/?page=2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFixScheme_HttpBaseStaysHttp(t *testing.T) {
	c := &Client{baseURL: "http://pw.example.com/api/1.2"}
	got := c.fixScheme(
		"http://pw.example.com/api/1.2/patches/?page=2")
	want := "http://pw.example.com/api/1.2/patches/?page=2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFixScheme_Empty(t *testing.T) {
	c := &Client{baseURL: "https://pw.example.com"}
	got := c.fixScheme("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFixScheme_InPagination(t *testing.T) {
	c := &Client{
		baseURL: "https://patchwork.example.com/api/1.2",
	}
	input := "http://patchwork.example.com/api/1.2/patches/?page=2"
	got := c.fixScheme(input)
	want := "https://patchwork.example.com/api/1.2/patches/?page=2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIsBotResponse(t *testing.T) {
	tests := []struct {
		name   string
		status int
		ct     string
		want   bool
	}{
		{"200 html = bot", 200, "text/html; charset=utf-8", true},
		{"200 json = ok", 200, "application/json", false},
		{"200 empty ct = ok", 200, "", false},
		{"403 html = bot", 403, "text/html", true},
		{"503 html = bot", 503, "text/html", true},
		{"500 html = server error", 500, "text/html", false},
		{"502 html = server error", 502, "text/html", false},
		{"504 html = server error", 504, "text/html", false},
		{"404 html = not bot", 404, "text/html", false},
		{"401 html = not bot", 401, "text/html", false},
		{"408 html = not bot", 408, "text/html", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.status,
				Header:     http.Header{},
			}
			if tt.ct != "" {
				resp.Header.Set("Content-Type", tt.ct)
			}
			got := isBotResponse(resp)
			if got != tt.want {
				t.Errorf("isBotResponse(status=%d, ct=%q) = %v, want %v",
					tt.status, tt.ct, got, tt.want)
			}
		})
	}
}

func TestDoRequest_BotDetection_Tier2_CurlWithUA(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
	// Server blocks Go (first request) but allows curl with same UA
	// (tier 2: different TLS fingerprint is enough).
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := reqCount.Add(1)
			if n == 1 {
				// First request (Go HTTP) — return bot page
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte("<html>challenge</html>"))
				return
			}
			// Subsequent requests (curl) — return JSON
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1}`))
		}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL,
		project:    "test",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	resp, err := c.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if transportMode(c.transport.Load()) != transportCurl {
		t.Errorf("transport = %d, want %d (transportCurl)",
			c.transport.Load(), transportCurl)
	}
}

func TestDoRequest_BotDetection_Tier3_CurlAnon(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
	// Server blocks any request with "leadlight" in the UA.
	// Only curl with its default UA (tier 3) gets through.
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			ua := r.Header.Get("User-Agent")
			if strings.Contains(ua, "leadlight") {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte("<html>challenge</html>"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1}`))
		}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL,
		project:    "test",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	resp, err := c.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if transportMode(c.transport.Load()) != transportCurlAnon {
		t.Errorf("transport = %d, want %d (transportCurlAnon)",
			c.transport.Load(), transportCurlAnon)
	}
}

func TestDoRequest_PermanentTransport_Curl(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1}`))
		}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL,
		project:    "test",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}
	c.transport.Store(int32(transportCurl))

	resp, err := c.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDoRequest_PermanentTransport_CurlAnon(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1}`))
		}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL,
		project:    "test",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}
	c.transport.Store(int32(transportCurlAnon))

	resp, err := c.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if strings.Contains(gotUA, "leadlight") {
		t.Errorf("anon transport should not send leadlight UA, got %q", gotUA)
	}
}

func TestDoRequest_NormalResponse_NoCurlFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":1}`))
		}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL,
		project:    "test",
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	resp, err := c.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if c.transport.Load() != int32(transportGo) {
		t.Error("transport should remain Go for normal responses")
	}
}
