package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type ArchiveMessage struct {
	Number  int
	Subject string
}

func BuildArchiveURL(baseURL string, year int, month time.Month) string {
	return fmt.Sprintf("%s%d-%s/date.html",
		strings.TrimRight(baseURL, "/")+"/",
		year, month.String())
}

func (c *Client) FetchArchiveMessages(ctx context.Context, pageURL string) ([]ArchiveMessage, error) {
	c.waitForRateLimit()
	log.Printf("ARCHIVE %s", pageURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	c.markRequestDone()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf(
			"HTTP %d fetching archive", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if bytes.Contains(body, []byte("not a bot")) {
		return nil, fmt.Errorf(
			"blocked by bot protection (Anubis)")
	}

	return parseArchiveMessages(body)
}

var archiveMsgRe = regexp.MustCompile(`(?i)<LI><A HREF="(\d+)\.html">([^<]+)`)

func parseArchiveMessages(body []byte) ([]ArchiveMessage, error) {
	matches := archiveMsgRe.FindAllSubmatch(body, -1)
	msgs := make([]ArchiveMessage, 0, len(matches))
	for _, m := range matches {
		num, err := strconv.Atoi(string(m[1]))
		if err != nil {
			continue
		}
		subject := cleanArchiveSubject(string(m[2]))
		msgs = append(msgs, ArchiveMessage{
			Number:  num,
			Subject: subject,
		})
	}
	return msgs, nil
}

func cleanArchiveSubject(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(textContent(doc))
}

var bracketPrefixRe = regexp.MustCompile(`^\[[^\]]*\]\s*`)

func ExtractPatchCore(s string) string {
	for {
		stripped := bracketPrefixRe.ReplaceAllString(s, "")
		if stripped == s {
			break
		}
		s = stripped
	}
	return strings.TrimSpace(s)
}

const lcsMatchThreshold = 80

func subjectMatch(patchCore, emailSubject string) bool {
	pc := strings.ToLower(patchCore)
	es := strings.ToLower(emailSubject)
	if pc == "" {
		return false
	}
	if strings.Contains(es, pc) {
		return true
	}
	lcs := longestCommonSubstring(pc, es)
	return lcs*100/len(pc) >= lcsMatchThreshold
}

func longestCommonSubstring(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	maxLen := 0
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > maxLen {
					maxLen = curr[j]
				}
			} else {
				curr[j] = 0
			}
		}
		prev, curr = curr, prev
		for k := range curr {
			curr[k] = 0
		}
	}
	return maxLen
}

func MatchPatchSubjects(msgs []ArchiveMessage, patchNames map[int]string) []int {
	cores := map[int]string{}
	for id, name := range patchNames {
		c := ExtractPatchCore(name)
		if c != "" {
			cores[id] = c
		}
	}

	matched := map[int]bool{}
	for _, msg := range msgs {
		for id, core := range cores {
			if subjectMatch(core, msg.Subject) {
				matched[id] = true
			}
		}
	}

	ids := make([]int, 0, len(matched))
	for id := range matched {
		ids = append(ids, id)
	}
	return ids
}

func FilterNewMessages(msgs []ArchiveMessage, lastSeen int) []ArchiveMessage {
	var result []ArchiveMessage
	for _, m := range msgs {
		if m.Number > lastSeen {
			result = append(result, m)
		}
	}
	return result
}
