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
	resp, err := c.doExternalRequest(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d fetching archive", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if bytes.Contains(body, []byte("not a bot")) {
		return nil, fmt.Errorf("archive is not accessible")
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

// Matches "v2", "V10", "PATCHv2" etc. — no word boundary before v
// because "PATCHv2" is a common format in the wild.
var versionInBracketRe = regexp.MustCompile(`(?i)v(\d+)\b`)

// extractVersion extracts a version token (e.g., "v2") from a patch
// subject. Skips to the first '[' (ignoring any reply prefix in any
// language), then scans consecutive bracket groups. Stops as soon as
// non-bracket text is encountered — brackets buried in (was: ...) or
// quoted text are ignored, and the caller falls back to matching all
// versions to avoid false negatives.
func extractVersion(subject string) string {
	s := strings.TrimSpace(subject)
	i := strings.Index(s, "[")
	if i < 0 {
		return ""
	}
	// If there's a '(' before the first '[', the bracket is likely
	// inside a parenthetical like "(was: [PATCH v2] ...)" — don't
	// trust it for version extraction.
	if strings.Contains(s[:i], "(") {
		return ""
	}
	s = s[i:]
	for strings.HasPrefix(s, "[") {
		close := strings.Index(s, "]")
		if close < 0 {
			break
		}
		bracket := s[1:close]
		if m := versionInBracketRe.FindString(bracket); m != "" {
			return strings.ToLower(m)
		}
		s = strings.TrimSpace(s[close+1:])
	}
	return ""
}

// versionsMatch checks if an archive message version matches a patch
// version. Unversioned patches are implicitly v1.
func versionsMatch(msgVersion, patchVersion string) bool {
	if msgVersion == patchVersion {
		return true
	}
	if msgVersion == "v1" && patchVersion == "" {
		return true
	}
	if patchVersion == "v1" && msgVersion == "" {
		return true
	}
	return false
}

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
	type patchInfo struct {
		core    string
		version string
	}
	patches := map[int]patchInfo{}
	for id, name := range patchNames {
		c := ExtractPatchCore(name)
		if c != "" {
			patches[id] = patchInfo{
				core:    c,
				version: extractVersion(name),
			}
		}
	}

	matched := map[int]bool{}
	for _, msg := range msgs {
		msgVersion := extractVersion(msg.Subject)
		for id, p := range patches {
			if !subjectMatch(p.core, msg.Subject) {
				continue
			}
			// If the archive message has no extractable version,
			// match all versions to avoid false negatives.
			if msgVersion == "" ||
				versionsMatch(msgVersion, p.version) {
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
