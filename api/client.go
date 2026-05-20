package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"leadlight/config"
)

const (
	defaultMinDelay = 5 * time.Second
	httpTimeout     = 60 * time.Second
	perPage         = "1024"
)

type transportMode int32

const (
	transportGo       transportMode = 0 // Go HTTP with leadlight UA
	transportCurl     transportMode = 1 // curl with leadlight UA
	transportCurlAnon transportMode = 2 // curl with default UA (avoids UA-based bot filters)
)

type Client struct {
	baseURL    string
	project    string
	token      string
	username   string
	password   string
	httpClient *http.Client
	minDelay   time.Duration
	mu         sync.Mutex
	lastReq    time.Time
	transport  atomic.Int32 // transportMode: set permanently after bot detection
}

func NewClient(cfg *config.Config) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.Server, "/"),
		project:  cfg.Project,
		token:    cfg.Token,
		username: cfg.Username,
		password: cfg.Password,
		httpClient: &http.Client{
			Timeout:   httpTimeout,
			Transport: transport,
		},
		minDelay: defaultMinDelay,
	}
}

func NewClientForTest(
	baseURL, project string,
	httpClient *http.Client,
	minDelay time.Duration,
) *Client {
	return &Client{
		baseURL:    baseURL,
		project:    project,
		httpClient: httpClient,
		minDelay:   minDelay,
	}
}

type PatchListParams struct {
	State    []string
	Project  string
	Since    string
	Before   string
	Order    string
	Archived string // "true", "false", "both", or "" (server default)
}

type SeriesListParams struct {
	Project string
	Since   string
	Before  string
	Order   string
}

type EventListParams struct {
	Since   string
	Project string
	Order   string
}

type PatchUpdate struct {
	State         *string
	Delegate      *int
	UnsetDelegate bool
}

func (u PatchUpdate) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{}
	if u.State != nil {
		m["state"] = *u.State
	}
	if u.UnsetDelegate {
		m["delegate"] = nil
	} else if u.Delegate != nil {
		m["delegate"] = *u.Delegate
	}
	return json.Marshal(m)
}

type rateLimitKey struct{}

// WithNoRateLimit returns a context that disables API rate
// limiting for all downstream requests.  Used for user-initiated
// operations (mbox fetch, delegate/state changes) where the
// 5-second delay between requests would degrade the UX.
func WithNoRateLimit(ctx context.Context) context.Context {
	return context.WithValue(ctx, rateLimitKey{}, true)
}

func (c *Client) shouldRateLimit(ctx context.Context) bool {
	v, _ := ctx.Value(rateLimitKey{}).(bool)
	return !v
}

// waitForRateLimit waits until enough time has passed since the last
// request, then reserves the next slot. The mutex is only held briefly
// to check/set the timestamp — never during sleep. This prevents
// blocking markRequestDone from other goroutines that have completed
// their HTTP calls.
func (c *Client) waitForRateLimit() {
	for {
		c.mu.Lock()
		if c.lastReq.IsZero() || time.Since(c.lastReq) >= c.minDelay {
			c.lastReq = time.Now()
			c.mu.Unlock()
			return
		}
		delay := c.minDelay - time.Since(c.lastReq)
		c.mu.Unlock()
		time.Sleep(delay)
	}
}

func (c *Client) markRequestDone() {
	c.mu.Lock()
	c.lastReq = time.Now()
	c.mu.Unlock()
}

func (c *Client) newRequest(
	ctx context.Context,
	method, rawURL string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "leadlight/1.0")
	req.Header.Set("Accept", "*/*")
	if c.token != "" {
		req.Header.Set("Authorization", "Token "+c.token)
	} else if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func isBotResponse(resp *http.Response) bool {
	// Bot protection returns HTML challenge pages on specific
	// status codes (200, 403, 503). Other status codes with HTML
	// are server errors, not bot protection.
	switch resp.StatusCode {
	case 200, 403, 503:
		ct := resp.Header.Get("Content-Type")
		return strings.Contains(ct, "text/html")
	}
	return false
}

// doCurlRequest performs an HTTP request via the curl binary.
// When skipUA is true, curl uses its own default User-Agent.
// Does not apply rate limiting — the caller is responsible.
func (c *Client) doCurlRequest(
	ctx context.Context, method, rawURL string, body io.Reader, skipUA bool,
) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	via := "curl"
	if skipUA {
		via = "curl-anon"
	}
	log.Printf("HTTP %s (%s) %s", method, via, rawURL)
	resp, err := execCurl(req, skipUA)
	if err != nil {
		log.Printf("HTTP %s (%s) -> error: %v %s", method, via, err, rawURL)
		return nil, err
	}
	log.Printf("HTTP %s (%s) -> %d %s", method, via, resp.StatusCode, rawURL)
	return resp, nil
}

func resetBody(body io.Reader) {
	if seeker, ok := body.(io.Seeker); ok {
		seeker.Seek(0, io.SeekStart)
	}
}

func (c *Client) doRequest(
	ctx context.Context,
	method, rawURL string,
	body io.Reader,
) (*http.Response, error) {
	if c.shouldRateLimit(ctx) {
		c.waitForRateLimit()
	}
	defer func() {
		if c.shouldRateLimit(ctx) {
			c.markRequestDone()
		}
	}()

	// If a previous request already determined the best transport,
	// use it directly without re-probing.
	switch transportMode(c.transport.Load()) {
	case transportCurl:
		return c.doCurlRequest(ctx, method, rawURL, body, false)
	case transportCurlAnon:
		return c.doCurlRequest(ctx, method, rawURL, body, true)
	}

	// Tier 1: Go HTTP with leadlight UA
	log.Printf("HTTP %s (go) %s", method, rawURL)
	req, err := c.newRequest(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("HTTP %s (go) -> error: %v %s", method, err, rawURL)
		return nil, err
	}
	log.Printf("HTTP %s (go) -> %d %s", method, resp.StatusCode, rawURL)
	if !isBotResponse(resp) {
		return resp, nil
	}

	// Tier 2: curl with leadlight UA (different TLS fingerprint)
	resp.Body.Close()
	log.Printf("HTTP %s -> bot protection detected, retrying with curl %s", method, rawURL)
	resetBody(body)
	curlResp, curlErr := c.doCurlRequest(ctx, method, rawURL, body, false)
	if curlErr == nil && !isBotResponse(curlResp) {
		c.transport.Store(int32(transportCurl))
		log.Printf("HTTP: permanently switched to curl for API requests")
		return curlResp, nil
	}
	if curlResp != nil {
		curlResp.Body.Close()
	}

	// Tier 3: curl with default UA (accepted by most bot filters)
	log.Printf("HTTP %s -> curl also blocked, retrying with default UA %s", method, rawURL)
	resetBody(body)
	anonResp, anonErr := c.doCurlRequest(ctx, method, rawURL, body, true)
	if anonErr == nil && !isBotResponse(anonResp) {
		c.transport.Store(int32(transportCurlAnon))
		log.Printf("HTTP: permanently switched to curl (default UA) for API requests")
		return anonResp, nil
	}
	if anonResp != nil {
		anonResp.Body.Close()
	}

	return nil, fmt.Errorf("bot protection: all transports blocked for %s", rawURL)
}

func (c *Client) doExternalRequest(
	ctx context.Context,
	method, rawURL string,
	body io.Reader,
) (*http.Response, error) {
	if c.shouldRateLimit(ctx) {
		c.waitForRateLimit()
	}
	req, err := c.newRequest(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	via := "curl"
	log.Printf("HTTP %s (%s) %s", method, via, rawURL)
	resp, err := execCurl(req, false)
	if c.shouldRateLimit(ctx) {
		c.markRequestDone()
	}
	if err != nil {
		via = "go-fallback"
		log.Printf("HTTP %s -> curl failed: %v, falling back to Go %s", method, err, rawURL)
		resp, err = c.httpClient.Do(req)
		if c.shouldRateLimit(ctx) {
			c.markRequestDone()
		}
	}
	if err != nil {
		log.Printf("HTTP %s (%s) -> error: %v %s", method, via, err, rawURL)
		return nil, err
	}
	log.Printf("HTTP %s (%s) -> %d %s", method, via, resp.StatusCode, rawURL)
	return resp, nil
}

func (c *Client) get(
	ctx context.Context,
	path string,
	params url.Values,
) (*http.Response, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return c.doRequest(ctx, http.MethodGet, u, nil)
}

func (c *Client) getJSON(
	ctx context.Context,
	path string,
	params url.Values,
	dest interface{},
) error {
	resp, err := c.get(ctx, path, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

func (c *Client) patchJSON(
	ctx context.Context,
	path string,
	body interface{},
	dest interface{},
) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	u := c.baseURL + path
	resp, err := c.doRequest(ctx, http.MethodPatch, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

func getAll[T any](
	c *Client,
	ctx context.Context,
	path string,
	params url.Values,
) ([]T, error) {
	var all []T

	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	for u != "" {
		resp, err := c.doRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return all, err
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return all, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
		}

		var page []T
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return all, err
		}

		all = append(all, page...)
		u = c.fixScheme(parseLinkNext(resp.Header.Get("Link")))
	}

	return all, nil
}

// Patchwork sometimes returns http:// URLs in Link headers
// even when accessed over https://. This preserves the
// original scheme from the configured base URL.
func (c *Client) fixScheme(u string) string {
	if u == "" {
		return u
	}
	if strings.HasPrefix(c.baseURL, "https://") &&
		strings.HasPrefix(u, "http://") {
		return "https://" + u[len("http://"):]
	}
	return u
}

func parseLinkRel(header, rel string) string {
	if header == "" {
		return ""
	}
	target := `rel="` + rel + `"`
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, target) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}

func parseLinkNext(header string) string { return parseLinkRel(header, "next") }
func parseLinkLast(header string) string { return parseLinkRel(header, "last") }

// extractPageCount parses the page number from a "last" Link URL.
// Returns 0 if the URL is empty or has no page parameter.
func extractPageCount(lastURL string) int {
	if lastURL == "" {
		return 0
	}
	u, err := url.Parse(lastURL)
	if err != nil {
		return 0
	}
	p := u.Query().Get("page")
	if p == "" {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

func (c *Client) GetProject(
	ctx context.Context,
	nameOrID string,
) (*Project, error) {
	var p Project
	path := fmt.Sprintf("/projects/%s/", nameOrID)
	if err := c.getJSON(ctx, path, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

type PageResult[T any] struct {
	Items      []T
	NextURL    string
	TotalPages int // 0 if unknown (no rel="last" in Link header)
}

func getPage[T any](
	c *Client, ctx context.Context, rawURL string,
) (*PageResult[T], error) {
	resp, err := c.doRequest(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var items []T
	err = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	link := resp.Header.Get("Link")
	next := c.fixScheme(parseLinkNext(link))
	total := extractPageCount(c.fixScheme(parseLinkLast(link)))
	return &PageResult[T]{Items: items, NextURL: next, TotalPages: total}, nil
}

func (c *Client) GetPatchesPage(
	ctx context.Context, pageURL string,
) (*PageResult[Patch], error) {
	return getPage[Patch](c, ctx, pageURL)
}

func (c *Client) BuildSeriesURL(params SeriesListParams) string {
	v := url.Values{}
	v.Set("per_page", perPage)
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
	}
	if params.Before != "" {
		v.Set("before", params.Before)
	}
	if params.Order != "" {
		v.Set("order", params.Order)
	}
	return c.baseURL + "/series/?" + v.Encode()
}

func (c *Client) GetSeriesPage(ctx context.Context, pageURL string) (*PageResult[Series], error) {
	return getPage[Series](c, ctx, pageURL)
}

func (c *Client) BuildPatchesURL(
	params PatchListParams,
) string {
	v := url.Values{}
	v.Set("per_page", perPage)
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	for _, s := range params.State {
		v.Add("state", s)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
	}
	if params.Before != "" {
		v.Set("before", params.Before)
	}
	if params.Order != "" {
		v.Set("order", params.Order)
	}
	if params.Archived != "" {
		v.Set("archived", params.Archived)
	}
	return c.baseURL + "/patches/?" + v.Encode()
}

func (c *Client) GetEventsPage(
	ctx context.Context, pageURL string,
) (*PageResult[Event], error) {
	return getPage[Event](c, ctx, pageURL)
}

func (c *Client) BuildEventsURL(
	params EventListParams,
) string {
	v := url.Values{}
	v.Set("per_page", perPage)
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
	}
	if params.Order != "" {
		v.Set("order", params.Order)
	}
	return c.baseURL + "/events/?" + v.Encode()
}

func (c *Client) GetPatches(
	ctx context.Context,
	params PatchListParams,
) ([]Patch, error) {
	v := url.Values{}
	v.Set("per_page", perPage)
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	for _, s := range params.State {
		v.Add("state", s)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
	}
	return getAll[Patch](c, ctx, "/patches/", v)
}

func (c *Client) GetPatch(
	ctx context.Context,
	id int,
) (*PatchDetail, error) {
	var pd PatchDetail
	path := fmt.Sprintf("/patches/%d/", id)
	if err := c.getJSON(ctx, path, nil, &pd); err != nil {
		return nil, err
	}
	return &pd, nil
}

func (c *Client) GetPatchComments(
	ctx context.Context,
	id int,
) ([]Comment, error) {
	path := fmt.Sprintf("/patches/%d/comments/", id)
	v := url.Values{}
	v.Set("per_page", perPage)
	return getAll[Comment](c, ctx, path, v)
}

func (c *Client) GetCoverComments(
	ctx context.Context,
	id int,
) ([]Comment, error) {
	path := fmt.Sprintf("/covers/%d/comments/", id)
	v := url.Values{}
	v.Set("per_page", perPage)
	return getAll[Comment](c, ctx, path, v)
}

func (c *Client) GetPatchChecks(
	ctx context.Context,
	id int,
) ([]Check, error) {
	path := fmt.Sprintf("/patches/%d/checks/", id)
	v := url.Values{}
	v.Set("per_page", perPage)
	return getAll[Check](c, ctx, path, v)
}

func (c *Client) GetSeries(
	ctx context.Context,
	id int,
) (*Series, error) {
	var s Series
	path := fmt.Sprintf("/series/%d/", id)
	if err := c.getJSON(ctx, path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetCover(
	ctx context.Context,
	id int,
) (*CoverDetail, error) {
	var cd CoverDetail
	path := fmt.Sprintf("/covers/%d/", id)
	if err := c.getJSON(ctx, path, nil, &cd); err != nil {
		return nil, err
	}
	return &cd, nil
}

func (c *Client) GetEvents(
	ctx context.Context,
	params EventListParams,
) ([]Event, error) {
	v := url.Values{}
	v.Set("per_page", perPage)
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
	}
	if params.Order != "" {
		v.Set("order", params.Order)
	}
	return getAll[Event](c, ctx, "/events/", v)
}

func (c *Client) UpdatePatch(
	ctx context.Context,
	id int,
	update PatchUpdate,
) (*PatchDetail, error) {
	var pd PatchDetail
	path := fmt.Sprintf("/patches/%d/", id)
	if err := c.patchJSON(ctx, path, update, &pd); err != nil {
		return nil, err
	}
	return &pd, nil
}

// LookupUserID resolves a username to a Patchwork user ID via
// the /users/ API.  The project API returns person/maintainer
// IDs which differ from user IDs used by the delegate API.
func (c *Client) LookupUserID(
	ctx context.Context, username string,
) (int, error) {
	v := url.Values{}
	v.Set("q", username)
	users, err := getAll[User](c, ctx, "/users/", v)
	if err != nil {
		return 0, fmt.Errorf("lookup user %q: %w", username, err)
	}
	for _, u := range users {
		if u.Username == username {
			return u.ID, nil
		}
	}
	return 0, fmt.Errorf("user %q not found", username)
}
