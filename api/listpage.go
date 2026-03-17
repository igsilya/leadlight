package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type ListPagePatch struct {
	PatchID    int
	Name       string
	SeriesID   int
	SeriesName string
	AckedBy    int
	Fixes      int
	ReviewedBy int
	TestedBy   int
	ChecksPass int
	ChecksWarn int
	ChecksFail int
	Date       string
	Submitter  string
	Delegate   string
	State      string
}

type ListPageDelegate struct {
	ID       int
	Username string
}

type ListPage struct {
	Patches   []ListPagePatch
	Delegates []ListPageDelegate
	NextURL   string
}

func BuildListURL(
	baseURL string,
	project string,
	states []string,
) string {
	u := strings.TrimRight(baseURL, "/") +
		"/project/" + project + "/list/"
	if len(states) > 0 {
		params := make([]string, len(states))
		for i, s := range states {
			params[i] = "state=" + s
		}
		u += "?" + strings.Join(params, "&")
	}
	return u
}

func (c *Client) FetchListPage(
	ctx context.Context,
	pageURL string,
) (*ListPage, error) {
	c.waitForRateLimit()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf(
			"HTTP %d fetching list page", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Detect Anubis/non-Patchwork pages by checking for
	// the patch list table structure. An empty table with
	// <tbody> is valid (no patches); a page without any
	// table structure at all is not.
	if !bytes.Contains(body, []byte("<tbody>")) {
		return nil, fmt.Errorf(
			"page does not contain patch data")
	}

	return parseListPage(body)
}

func parseListPage(body []byte) (*ListPage, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	page := &ListPage{
		Patches:   parsePatchRows(doc),
		Delegates: parseDelegateOptions(doc),
		NextURL:   parseNextPageURL(doc),
	}
	return page, nil
}

var patchRowIDRe = regexp.MustCompile(`^patch_row:(\d+)$`)
var seriesIDRe = regexp.MustCompile(`series=(\d+)`)

// Patchwork 2.x: "N Acked-by / N Fixes / N Reviewed-by / N Tested-by"
// Patchwork 3.x: "N Acked-by / N Reviewed-by / N Tested-by" (no Fixes)
var tagTitleRe4 = regexp.MustCompile(
	`(\d+) Acked-by / (\d+) Fixes / (\d+) Reviewed-by / (\d+) Tested-by`)
var tagTitleRe3 = regexp.MustCompile(
	`(\d+) Acked-by / (\d+) Reviewed-by / (\d+) Tested-by`)

func parsePatchRows(doc *html.Node) []ListPagePatch {
	rows := findNodes(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return false
		}
		id := getAttr(n, "id")
		return patchRowIDRe.MatchString(id)
	})

	var patches []ListPagePatch
	for _, row := range rows {
		if p := parsePatchRow(row); p != nil {
			patches = append(patches, *p)
		}
	}
	return patches
}

func parsePatchRow(tr *html.Node) *ListPagePatch {
	id := getAttr(tr, "id")
	m := patchRowIDRe.FindStringSubmatch(id)
	if m == nil {
		return nil
	}
	patchID, _ := strconv.Atoi(m[1])
	p := &ListPagePatch{PatchID: patchID}

	cells := childElements(tr, "td")
	n := len(cells)

	if n > 0 {
		if a := findChild(cells[0], "a"); a != nil {
			p.Name = cleanText(textContent(a))
		}
	}

	if n > 1 {
		if a := findChild(cells[1], "a"); a != nil {
			p.SeriesName = cleanText(textContent(a))
			href := getAttr(a, "href")
			sm := seriesIDRe.FindStringSubmatch(href)
			if sm != nil {
				p.SeriesID, _ = strconv.Atoi(sm[1])
			}
		}
	}

	if n > 2 {
		if span := findChild(cells[2], "span"); span != nil {
			title := getAttr(span, "title")
			p.AckedBy, p.Fixes, p.ReviewedBy, p.TestedBy =
				parseTagTitle(title)
		}
	}

	if n > 3 {
		p.ChecksPass, p.ChecksWarn, p.ChecksFail =
			parseCheckSpans(cells[3])
	}

	if n > 4 {
		p.Date = cleanText(textContent(cells[4]))
	}

	if n > 5 {
		if a := findChild(cells[5], "a"); a != nil {
			p.Submitter = cleanText(textContent(a))
		} else {
			p.Submitter = cleanText(textContent(cells[5]))
		}
	}

	if n > 6 {
		p.Delegate = cleanText(textContent(cells[6]))
	}

	if n > 7 {
		p.State = cleanText(textContent(cells[7]))
	}

	return p
}

func parseTagTitle(title string) (a, f, r, te int) {
	if m := tagTitleRe4.FindStringSubmatch(title); m != nil {
		a, _ = strconv.Atoi(m[1])
		f, _ = strconv.Atoi(m[2])
		r, _ = strconv.Atoi(m[3])
		te, _ = strconv.Atoi(m[4])
		return
	}
	if m := tagTitleRe3.FindStringSubmatch(title); m != nil {
		a, _ = strconv.Atoi(m[1])
		r, _ = strconv.Atoi(m[2])
		te, _ = strconv.Atoi(m[3])
		return
	}
	return 0, 0, 0, 0
}

func parseCheckSpans(td *html.Node) (pass, warn, fail int) {
	// Find the outer span, then its child spans
	outer := findChild(td, "span")
	if outer == nil {
		return
	}
	spans := childElements(outer, "span")
	// Order: success, warning, fail
	for i, span := range spans {
		text := cleanText(textContent(span))
		n, err := strconv.Atoi(text)
		if err != nil {
			continue
		}
		switch i {
		case 0:
			pass = n
		case 1:
			warn = n
		case 2:
			fail = n
		}
	}
	return
}

func parseDelegateOptions(doc *html.Node) []ListPageDelegate {
	selects := findNodes(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode &&
			n.Data == "select" &&
			getAttr(n, "name") == "delegate"
	})
	if len(selects) == 0 {
		return nil
	}

	var delegates []ListPageDelegate
	options := childElements(selects[0], "option")
	for _, opt := range options {
		val := getAttr(opt, "value")
		id, err := strconv.Atoi(val)
		if err != nil {
			continue
		}
		username := cleanText(textContent(opt))
		if username == "" {
			continue
		}
		delegates = append(delegates, ListPageDelegate{
			ID:       id,
			Username: username,
		})
	}
	return delegates
}

func parseNextPageURL(doc *html.Node) string {
	spans := findNodes(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode &&
			n.Data == "span" &&
			hasClass(n, "next")
	})
	if len(spans) == 0 {
		return ""
	}
	if a := findChild(spans[0], "a"); a != nil {
		return getAttr(a, "href")
	}
	return ""
}

func findNodes(
	n *html.Node,
	match func(*html.Node) bool,
) []*html.Node {
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if match(node) {
			result = append(result, node)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return result
}

func findChild(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
		// Also search grandchildren for cases where
		// there's whitespace text nodes between elements
		if found := findChild(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func childElements(n *html.Node, tag string) []*html.Node {
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == tag {
				result = append(result, c)
			} else {
				walk(c)
			}
		}
	}
	walk(n)
	return result
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	classes := strings.Fields(getAttr(n, "class"))
	for _, c := range classes {
		if c == class {
			return true
		}
	}
	return false
}

func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
