package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"leadlight/config"
)

const defaultMinDelay = 5 * time.Second

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
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL:    strings.TrimRight(cfg.Server, "/"),
		project:    cfg.Project,
		token:      cfg.Token,
		username:   cfg.Username,
		password:   cfg.Password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		minDelay:   defaultMinDelay,
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
	State   []string
	Project string
	Since   string
}

type EventListParams struct {
	Since   string
	Project string
	Order   string
}

type PatchUpdate struct {
	State    *string `json:"state,omitempty"`
	Delegate *int    `json:"delegate,omitempty"`
}

func (c *Client) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.lastReq.IsZero() {
		elapsed := time.Since(c.lastReq)
		if elapsed < c.minDelay {
			time.Sleep(c.minDelay - elapsed)
		}
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
	req, err := http.NewRequestWithContext(
		ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
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

func (c *Client) doRequest(
	ctx context.Context,
	method, rawURL string,
	body io.Reader,
) (*http.Response, error) {
	c.waitForRateLimit()
	log.Printf("HTTP %s %s", method, rawURL)
	req, err := c.newRequest(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	c.markRequestDone()
	if err != nil {
		log.Printf("HTTP %s %s -> error: %v",
			method, rawURL, err)
		return nil, err
	}
	log.Printf("HTTP %s %s -> %d",
		method, rawURL, resp.StatusCode)
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
		return fmt.Errorf(
			"HTTP %d: %s", resp.StatusCode, body)
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
	resp, err := c.doRequest(
		ctx, http.MethodPatch, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf(
			"HTTP %d: %s", resp.StatusCode, respBody)
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
		resp, err := c.doRequest(
			ctx, http.MethodGet, u, nil)
		if err != nil {
			return all, err
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return all, fmt.Errorf(
				"HTTP %d: %s", resp.StatusCode, body)
		}

		var page []T
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return all, err
		}

		all = append(all, page...)
		u = c.fixScheme(
			parseLinkNext(resp.Header.Get("Link")))
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

func parseLinkNext(header string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
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
	Items   []T
	NextURL string
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
		return nil, fmt.Errorf(
			"HTTP %d: %s", resp.StatusCode, body)
	}
	var items []T
	err = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	next := c.fixScheme(
		parseLinkNext(resp.Header.Get("Link")))
	return &PageResult[T]{Items: items, NextURL: next}, nil
}

func (c *Client) GetPatchesPage(
	ctx context.Context, pageURL string,
) (*PageResult[Patch], error) {
	return getPage[Patch](c, ctx, pageURL)
}

func (c *Client) BuildPatchesURL(
	params PatchListParams,
) string {
	v := url.Values{}
	v.Set("per_page", "100")
	if params.Project != "" {
		v.Set("project", params.Project)
	}
	for _, s := range params.State {
		v.Add("state", s)
	}
	if params.Since != "" {
		v.Set("since", params.Since)
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
	v.Set("per_page", "100")
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
	v.Set("per_page", "100")
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
	v.Set("per_page", "100")
	return getAll[Comment](c, ctx, path, v)
}

func (c *Client) GetPatchChecks(
	ctx context.Context,
	id int,
) ([]Check, error) {
	path := fmt.Sprintf("/patches/%d/checks/", id)
	v := url.Values{}
	v.Set("per_page", "100")
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
	v.Set("per_page", "100")
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

func (c *Client) GetMbox(
	ctx context.Context,
	rawURL string,
) (string, error) {
	c.waitForRateLimit()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	c.markRequestDone()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
