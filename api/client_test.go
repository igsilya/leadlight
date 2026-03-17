package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestGetMbox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("From: lorem@ipsum.example\n" +
				"Subject: Lorem\n\nlorem ipsum"))
		}))
	t.Cleanup(srv.Close)

	c := &Client{
		httpClient: srv.Client(),
		minDelay:   10 * time.Millisecond,
	}

	content, err := c.GetMbox(
		context.Background(), srv.URL+"/mbox/")
	if err != nil {
		t.Fatal(err)
	}
	if content == "" {
		t.Error("content is empty")
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
