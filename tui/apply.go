// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"leadlight/db"
	"leadlight/gitops"
	"leadlight/status"
)

// patchNumber extracts the patch sequence number from a name like
// "[PATCH v2 3/5] subject". Returns 0 for cover letters (0/N) or
// names without a recognizable N/M pattern.
var patchNumRe = regexp.MustCompile(`\b(\d+)/\d+\]`)

func patchNumber(name string) int {
	m := patchNumRe.FindStringSubmatch(name)
	if len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// applyPatch holds the data needed to construct an mbox message
// for git am.
type applyPatch struct {
	From    string
	Date    string
	Subject string
	MsgID   string
	Body    string // commit message with injected tags
	Diff    string
}

// injectTags inserts comment-sourced review tags into a commit
// message body. Fixes go at the top of the trailer block (before
// all existing tags). Acked-by/Reviewed-by/Tested-by go before
// the first Signed-off-by/Co-authored-by/Co-developed-by.
// If removeSignoff is non-empty, any Signed-off-by matching that
// identity is removed (to avoid duplicates when git am -s is used).
// splitBodyAtSeparator splits a commit message body at the "---"
// separator line. The Patchwork API includes the diffstat after
// "---" as part of the Content field. Tags must be injected into
// the commit message part only, before the separator.
func splitBodyAtSeparator(body string) (msg, rest string) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(lines[:i], "\n"),
				"\n" + strings.Join(lines[i:], "\n")
		}
	}
	return body, ""
}

func injectTags(body string, commentTags []db.TagRow, originalTags []db.TagRow, removeSignoff string) string {
	// Split at "---" separator — tags go into the commit message
	// part only, before the diffstat.
	msg, rest := splitBodyAtSeparator(body)

	// Build set of existing original tags for dedup
	existing := map[string]bool{}
	for _, t := range originalTags {
		existing[t.Type+":"+t.Identity] = true
	}

	// Collect new tags, grouped by type
	var fixes, acked, reviewed, tested []string
	for _, t := range commentTags {
		key := t.Type + ":" + t.Identity
		if existing[key] {
			continue
		}
		existing[key] = true // prevent duplicates within comment tags
		line := tagLine(t.Type, t.Identity)
		switch t.Type {
		case "fixes":
			fixes = append(fixes, line)
		case "acked":
			acked = append(acked, line)
		case "reviewed":
			reviewed = append(reviewed, line)
		case "tested":
			tested = append(tested, line)
		}
	}

	artTags := concat(acked, reviewed, tested)
	if len(fixes) == 0 && len(artTags) == 0 && removeSignoff == "" {
		return body
	}

	lines := strings.Split(msg, "\n")

	// Remove trailing blank lines to find the trailer block
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	// Find the trailer block: contiguous tag-like lines at the end
	trailerStart := end
	for trailerStart > 0 && isTrailerLine(lines[trailerStart-1]) {
		trailerStart--
	}

	// Find the signoff insertion point within the trailer: before
	// the first Signed-off-by / Co-authored-by / Co-developed-by
	signoffIdx := end
	for i := trailerStart; i < end; i++ {
		if isSignoffLine(lines[i]) {
			signoffIdx = i
			break
		}
	}

	// Remove matching signoff if requested
	if removeSignoff != "" {
		filtered := make([]string, 0, len(lines))
		for i, line := range lines {
			if i >= trailerStart && i < end &&
				strings.EqualFold(strings.TrimSpace(line), removeSignoff) {
				// Adjust indices
				if i < signoffIdx {
					signoffIdx--
				}
				end--
				continue
			}
			filtered = append(filtered, line)
		}
		lines = filtered
	}

	// Insert A/R/T tags before the signoff point
	if len(artTags) > 0 {
		lines = insertLines(lines, signoffIdx, artTags)
		// Adjust trailerStart is unchanged (we're inserting within
		// the trailer block)
	}

	// Insert Fixes at the top of the trailer block
	if len(fixes) > 0 {
		lines = insertLines(lines, trailerStart, fixes)
	}

	return strings.Join(lines, "\n") + rest
}

func tagLine(tagType, identity string) string {
	switch tagType {
	case "fixes":
		return "Fixes: " + identity
	case "acked":
		return "Acked-by: " + identity
	case "reviewed":
		return "Reviewed-by: " + identity
	case "tested":
		return "Tested-by: " + identity
	default:
		return tagType + ": " + identity
	}
}

func isTrailerLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	trailerPrefixes := []string{
		"Fixes:", "Acked-by:", "Reviewed-by:", "Tested-by:",
		"Signed-off-by:", "Co-authored-by:", "Co-developed-by:",
		"Cc:", "Link:", "Suggested-by:", "Reported-by:",
	}
	for _, p := range trailerPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isSignoffLine(line string) bool {
	s := strings.TrimSpace(line)
	return strings.HasPrefix(s, "Signed-off-by:") ||
		strings.HasPrefix(s, "Co-authored-by:") ||
		strings.HasPrefix(s, "Co-developed-by:")
}

func insertLines(lines []string, at int, insert []string) []string {
	result := make([]string, 0, len(lines)+len(insert))
	result = append(result, lines[:at]...)
	result = append(result, insert...)
	result = append(result, lines[at:]...)
	return result
}

func concat(slices ...[]string) []string {
	var result []string
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}

// startApplyConfirm enters the confirmation prompt for applying patches.
// For a series row, it collects all patches in the series ordered by N/M.
// For a sub-row, it applies just that single patch.
func (m *Model) startApplyConfirm(seriesID int, patchIDs []int, name string) {
	if !gitops.IsRepo() {
		m.Status.SetTimed(status.Info,
			"Cannot apply: not a git repository", 5*time.Second)
		return
	}
	dirty, err := gitops.IsDirty()
	if err != nil {
		m.Status.SetTimed(status.Info,
			"Cannot apply: "+err.Error(), 5*time.Second)
		return
	}
	if dirty {
		m.Status.SetTimed(status.Info,
			"Cannot apply: uncommitted changes, commit or stash first",
			5*time.Second)
		return
	}

	m.applySeriesID = seriesID
	m.applyPatchIDs = patchIDs
	m.applyName = name
	m.applySelectedOption = 0
	m.applyCoverID = 0
	if cover, _ := m.db.GetCover(seriesID); cover != nil {
		m.applyCoverID = cover.ID
	}
	m.applyState = applyConfirm
}

// startApplyFetching triggers async fetches for all needed data
// and transitions to the fetching state.
func (m *Model) startApplyFetching() {
	m.applyState = applyFetching
	m.applyStartTime = time.Now()
	if !m.logConsole {
		m.logConsole = true
		m.applyOpenedLog = true
	}

	pending := 0
	for _, pid := range m.applyPatchIDs {
		if m.db.NeedsPatchDetail(pid) {
			if m.FetchPatchDetail != nil {
				m.FetchPatchDetail(pid)
			}
			pending++
		}
		if m.db.NeedsPatchComments(pid) {
			if m.FetchPatchComments != nil {
				m.FetchPatchComments(pid)
			}
			pending++
		}
	}
	if m.applyCoverID != 0 && m.db.NeedsCoverComments(m.applyCoverID) {
		if m.FetchCoverComments != nil {
			m.FetchCoverComments(m.applyCoverID)
		}
		pending++
	}

	log.Printf("[apply] Preparing to apply %d patches from %q",
		len(m.applyPatchIDs), m.applyName)

	if pending == 0 {
		m.applyState = applyRunning
		log.Printf("[apply] All data available, constructing mbox...")
	} else {
		log.Printf("[apply] Fetching data (%d requests pending)...", pending)
	}
}

// allApplyDataReady checks if all patches have their detail and
// comments fetched, and cover comments if applicable.
func (m *Model) allApplyDataReady() bool {
	for _, pid := range m.applyPatchIDs {
		if m.db.NeedsPatchDetail(pid) || m.db.NeedsPatchComments(pid) {
			return false
		}
	}
	if m.applyCoverID != 0 && m.db.NeedsCoverComments(m.applyCoverID) {
		return false
	}
	return true
}

// runApply constructs the mbox and runs git am as a tea.Cmd.
// Called when all data is ready.
func (m *Model) runApply() tea.Cmd {
	data := m.collectApplyData()
	signoff := m.Signoff

	return func() tea.Msg {
		mboxContent, err := constructApplyMbox(data)
		if err != nil {
			return applyResultMsg{err: err}
		}

		tmpFile, err := os.CreateTemp("", "leadlight-*.mbox")
		if err != nil {
			return applyResultMsg{err: fmt.Errorf("create temp file: %w", err)}
		}
		tmpPath := tmpFile.Name()
		if _, err := tmpFile.WriteString(mboxContent); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return applyResultMsg{err: fmt.Errorf("write mbox: %w", err)}
		}
		tmpFile.Close()

		output, err := gitops.Am(tmpPath, signoff)
		if err != nil {
			return applyResultMsg{output: output, err: err, tmpFile: tmpPath}
		}
		os.Remove(tmpPath)
		return applyResultMsg{output: output}
	}
}

// collectApplyData reads all patch data from the DB and constructs
// the applyPatch slice with tags injected. Must be called from the
// main goroutine (before starting the tea.Cmd).
func (m *Model) collectApplyData() []applyPatch {
	var removeSignoff string
	if m.Signoff {
		removeSignoff = gitops.Signoff()
	}

	// Collect cover comment tags (apply to all patches)
	var coverTags []db.TagRow
	if m.applyCoverID != 0 {
		allCoverTags := m.db.GetTagsForCover(m.applyCoverID)
		for _, t := range allCoverTags {
			if t.Source == "comment" {
				coverTags = append(coverTags, t)
			}
		}
	}

	// Sort patches by N/M order
	type patchInfo struct {
		id    int
		order int
	}
	var ordered []patchInfo
	for _, pid := range m.applyPatchIDs {
		row, err := m.db.GetPatch(pid)
		if err != nil || row == nil {
			continue
		}
		ordered = append(ordered, patchInfo{id: pid, order: patchNumber(row.Name)})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})

	var patches []applyPatch
	for _, pi := range ordered {
		row, _ := m.db.GetPatch(pi.id)
		if row == nil {
			continue
		}

		// Collect tags for this patch
		patchTags := m.db.GetTagsForPatch(pi.id)
		var original, comment []db.TagRow
		for _, t := range patchTags {
			if t.Source == "original" {
				original = append(original, t)
			} else if t.Source == "comment" {
				comment = append(comment, t)
			}
		}
		// Combine patch comment tags + cover comment tags
		allComment := append(comment, coverTags...)

		body := injectTags(row.Content, allComment, original, removeSignoff)

		from := formatFrom(row.Submitter, row.SubmitterEmail)

		patches = append(patches, applyPatch{
			From:    from,
			Date:    row.Date,
			Subject: row.Name,
			MsgID:   row.MsgID,
			Body:    body,
			Diff:    row.Diff,
		})
	}
	return patches
}

func (m *Model) exitApplyMode() {
	m.applyState = applyIdle
	if m.applyOpenedLog {
		m.logConsole = false
		m.applyOpenedLog = false
	}
}

func (m *Model) doAbortGitAm() {
	output, err := gitops.AmAbort()
	for _, line := range strings.Split(output, "\n") {
		if line != "" {
			log.Printf("[apply] %s", line)
		}
	}
	if err != nil {
		log.Printf("[apply] Abort failed: %v", err)
	} else {
		log.Printf("[apply] Reverted successfully")
	}
}

func (m *Model) applyDoRevert() {
	m.doAbortGitAm()
	m.applyDoneMsg = "Reverted."
	m.applyState = applyDone
}

func (m *Model) applyDoKeep() {
	log.Printf("[apply] Left in conflicted state for manual resolution")
	log.Printf("[apply] Use 'git am --continue' or 'git am --abort'")
	m.applyDoneMsg = "Kept for manual resolution."
	m.applyState = applyDone
}

// handleApplyKey handles key presses during the apply flow.
func (m *Model) handleApplyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// For all states except confirm, pass unhandled keys to the
	// log handler for scrolling, saving, etc.
	if m.applyState != applyConfirm && m.logConsole {
		switch key {
		case "enter", "q", "esc", "1", "2",
			"left", "right", "h", "l", "tab":
			// Fall through to apply-specific handling
		default:
			return m.handleLogKey(msg)
		}
	}

	switch m.applyState {
	case applyConfirm:
		switch key {
		case "1":
			m.startApplyFetching()
			if m.applyState == applyRunning {
				return m, m.runApply()
			}
		case "2", "esc", "q":
			m.exitApplyMode()
		case "left", "right", "h", "l", "tab":
			m.applySelectedOption = 1 - m.applySelectedOption
		case "enter":
			if m.applySelectedOption == 0 {
				m.startApplyFetching()
				if m.applyState == applyRunning {
					return m, m.runApply()
				}
			} else {
				m.exitApplyMode()
			}
		}
	case applyFetching:
		if key == "q" || key == "esc" || key == "enter" || key == "1" {
			log.Printf("[apply] Cancelled")
			m.exitApplyMode()
		}
	case applyRunning:
		// Can't cancel git am
	case applyConflict:
		switch key {
		case "1":
			m.applyDoRevert()
		case "2":
			m.applyDoKeep()
		case "left", "right", "h", "l", "tab":
			m.applySelectedOption = 1 - m.applySelectedOption
		case "enter":
			if m.applySelectedOption == 0 {
				m.applyDoRevert()
			} else {
				m.applyDoKeep()
			}
		case "esc", "q":
			m.applyDoRevert()
		}
	case applyDone:
		switch key {
		case "1", "enter", "q", "esc":
			m.exitApplyMode()
		}
	}
	return m, nil
}

// constructApplyMbox builds a multi-message mbox string for git am
// from a list of patches in the correct application order.
func constructApplyMbox(patches []applyPatch) (string, error) {
	var buf strings.Builder
	for _, p := range patches {
		if strings.TrimSpace(p.Diff) == "" {
			return "", fmt.Errorf("patch %q has no diff", p.Subject)
		}
		msgID := p.MsgID
		if msgID == "" {
			msgID = fmt.Sprintf("<leadlight-%d@generated>",
				buf.Len()) // unique enough
		}
		buf.WriteString("From " + msgID + " Mon Jan  1 00:00:00 2001\n")
		buf.WriteString("From: " + p.From + "\n")
		buf.WriteString("Date: " + p.Date + "\n")
		buf.WriteString("Subject: " + p.Subject + "\n")
		buf.WriteString("Message-Id: " + msgID + "\n")
		buf.WriteString("\n")
		buf.WriteString(p.Body)
		if !strings.HasSuffix(p.Body, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("---\n")
		buf.WriteString(p.Diff)
		if !strings.HasSuffix(p.Diff, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}
	return buf.String(), nil
}
