package db

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn    *sql.DB
	writeMu sync.Mutex // SQLite allows concurrent reads but only one writer
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// WAL mode allows the TUI to read while the syncer writes concurrently.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

type SeriesRow struct {
	ID              int
	Name            string
	Date            string
	Version         int
	Submitter       string
	SubmitterEmail  string
	WebURL          string
	MboxURL         string
	Complete        bool
	TotalPatches    int
	ReceivedPatches int
	UpdatedAt       string
}

type PatchRow struct {
	ID             int
	SeriesID       int
	Name           string
	Date           string
	State          string
	Submitter      string
	SubmitterEmail string
	DelegateID     int
	Delegate       string
	DelegateEmail  string
	WebURL         string
	MsgID          string
	MboxURL        string
	CommitRef      string
	Archived       bool
	ChecksPass     int
	ChecksFail     int
	ChecksWarn     int
	Content        string
	Diff           string
	Headers        string
	Prefixes       string
	DetailFetched  bool
	UpdatedAt      string
}

type CheckRow struct {
	ID          int
	PatchID     int
	Context     string
	State       string
	TargetURL   string
	Description string
	Date        string
}

type CommentRow struct {
	ID             int
	PatchID        int
	CoverID        int
	Submitter      string
	SubmitterEmail string
	Date           string
	Subject        string
	Content        string
	MsgID          string
	Headers        string
	WebURL         string
	ListArchiveURL string
}

type CoverRow struct {
	ID             int
	SeriesID       int
	Name           string
	Date           string
	Submitter      string
	SubmitterEmail string
	MsgID          string
	WebURL         string
	MboxURL        string
	Content        string
	Headers        string
	DetailFetched  bool
}

type MaintainerRow struct {
	ID        int
	Username  string
	FirstName string
	LastName  string
	Email     string
}

func (d *DB) SaveSeries(s SeriesRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO series (id, name, date, version,
			submitter, submitter_email,
			web_url, mbox_url, complete,
			total_patches, received_patches, detail_fetched)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,1)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			date = excluded.date,
			version = excluded.version,
			submitter = excluded.submitter,
			submitter_email = excluded.submitter_email,
			web_url = excluded.web_url,
			mbox_url = excluded.mbox_url,
			complete = excluded.complete,
			total_patches = excluded.total_patches,
			received_patches = excluded.received_patches,
			detail_fetched = 1`,
		s.ID, s.Name, s.Date, s.Version,
		s.Submitter, s.SubmitterEmail,
		s.WebURL, s.MboxURL, boolToInt(s.Complete),
		s.TotalPatches, s.ReceivedPatches)
	return err
}

func (d *DB) SaveSeriesSummary(
	id int, name, date string, version int,
) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO series (id, name, date, version)
		VALUES (?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name = CASE WHEN excluded.name != ''
				THEN excluded.name ELSE series.name END,
			date = CASE WHEN excluded.date != ''
				THEN excluded.date ELSE series.date END,
			version = excluded.version`,
		id, name, date, version)
	return err
}

func (d *DB) SavePatch(p PatchRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	// Merge-on-conflict: preserve existing non-empty values when the
	// incoming data has zero/empty. Different sync sources provide
	// different fields (events vs list pages vs API detail).
	_, err := d.conn.Exec(`
		INSERT INTO patches (id, series_id, name, date,
			state, submitter, submitter_email,
			delegate_id, delegate, delegate_email,
			web_url, msgid, mbox_url, commit_ref, archived)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			series_id = CASE WHEN excluded.series_id != 0
				THEN excluded.series_id
				ELSE patches.series_id END,
			name = excluded.name,
			date = excluded.date,
			state = excluded.state,
			submitter = excluded.submitter,
			submitter_email = excluded.submitter_email,
			delegate_id = excluded.delegate_id,
			delegate = excluded.delegate,
			delegate_email = excluded.delegate_email,
			web_url = excluded.web_url,
			msgid = CASE WHEN excluded.msgid != ''
				THEN excluded.msgid ELSE patches.msgid END,
			mbox_url = CASE WHEN excluded.mbox_url != ''
				THEN excluded.mbox_url
				ELSE patches.mbox_url END,
			commit_ref = excluded.commit_ref,
			archived = excluded.archived`,
		p.ID, p.SeriesID, p.Name, p.Date,
		p.State, p.Submitter, p.SubmitterEmail,
		p.DelegateID, p.Delegate, p.DelegateEmail,
		p.WebURL, p.MsgID, p.MboxURL,
		p.CommitRef, boolToInt(p.Archived))
	return err
}

func (d *DB) SavePatchSummary(
	id, seriesID int,
	name, date, msgid, mboxURL, webURL string,
) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO patches (id, series_id, name, date,
			state, msgid, mbox_url, web_url,
			submitter, submitter_email)
		VALUES (?,?,?,?,'new',?,?,?,'','')
		ON CONFLICT(id) DO UPDATE SET
			series_id = CASE WHEN excluded.series_id != 0
				THEN excluded.series_id
				ELSE patches.series_id END,
			name = CASE WHEN excluded.name != ''
				THEN excluded.name ELSE patches.name END,
			date = CASE WHEN excluded.date != ''
				THEN excluded.date ELSE patches.date END,
			msgid = CASE WHEN excluded.msgid != ''
				THEN excluded.msgid ELSE patches.msgid END,
			mbox_url = CASE WHEN excluded.mbox_url != ''
				THEN excluded.mbox_url
				ELSE patches.mbox_url END,
			web_url = CASE WHEN excluded.web_url != ''
				THEN excluded.web_url
				ELSE patches.web_url END`,
		id, seriesID, name, date, msgid, mboxURL, webURL)
	return err
}

func (d *DB) UpdatePatchState(patchID int, state string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		`UPDATE patches SET state = ? WHERE id = ?`,
		state, patchID)
	return err
}

func (d *DB) UpdatePatchDelegate(
	patchID, delegateID int,
	name, email string,
) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE patches
		SET delegate_id = ?, delegate = ?, delegate_email = ?
		WHERE id = ?`,
		delegateID, name, email, patchID)
	return err
}

func (d *DB) UpdatePatchDetail(
	patchID int,
	content, diff, headers, prefixes string,
) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE patches
		SET content = ?, diff = ?, headers = ?,
			prefixes = ?, detail_fetched = 1
		WHERE id = ?`,
		content, diff, headers, prefixes, patchID)
	return err
}

func (d *DB) UpdatePatchChecks(
	patchID, pass, fail, warn int,
) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE patches
		SET checks_pass = ?, checks_fail = ?, checks_warn = ?
		WHERE id = ?`,
		pass, fail, warn, patchID)
	return err
}

func (d *DB) UpdateSeriesPatches(seriesID int, submitter, email string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE patches SET submitter = ?, submitter_email = ?
		WHERE series_id = ? AND submitter = ''`,
		submitter, email, seriesID)
	return err
}

// SaveCheck inserts or updates a check record. Merge-on-conflict:
// same pattern as SavePatch — prefer non-empty incoming values,
// preserve existing data when incoming is empty.
func (d *DB) SaveCheck(c CheckRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO checks
			(id, patch_id, context, state, target_url, description, date)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			state = CASE WHEN excluded.state != ''
				THEN excluded.state ELSE checks.state END,
			target_url = CASE WHEN excluded.target_url != ''
				THEN excluded.target_url ELSE checks.target_url END,
			description = CASE WHEN excluded.description != ''
				THEN excluded.description ELSE checks.description END,
			date = CASE WHEN excluded.date != ''
				THEN excluded.date ELSE checks.date END`,
		c.ID, c.PatchID, c.Context,
		c.State, c.TargetURL, c.Description, c.Date)
	return err
}

func (d *DB) GetPatchesNeedingChecks(limit int) []FetchRef {
	ph, args := activeStateFilter()
	q1 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 1
		FROM patches
		WHERE checks_fetched = 0
			AND state IN (%s) AND archived = 0
		ORDER BY id DESC LIMIT ?`, ph)
	refs := scanFetchRefs(d.conn.Query(q1, append(args, limit)...))
	if len(refs) >= limit {
		return refs
	}
	q2 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 0
		FROM patches
		WHERE checks_fetched = 0
			AND NOT (state IN (%s) AND archived = 0)
		ORDER BY id DESC LIMIT ?`, ph)
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, append(args, limit-len(refs))...))...)
}

func (d *DB) MarkChecksFetched(patchID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE patches SET checks_fetched = 1 WHERE id = ?",
		patchID)
	return err
}

func (d *DB) ResetChecksFetched(patchID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE patches SET checks_fetched = 0 WHERE id = ?",
		patchID)
	return err
}

// RunRecountChecks runs the batch recount of all check counters.
// Exposed for testing — normally runs automatically during migration.
func (d *DB) RunRecountChecks() {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(recountChecks)
}

// RunResetChecksWithoutDescriptions resets checks_fetched for patches
// with description-less checks. Exposed for testing — normally runs
// automatically during migration.
func (d *DB) RunResetChecksWithoutDescriptions() {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(resetChecksWithoutDescriptions)
}

// RecountPatchChecks recounts check totals for a single patch.
// Only the latest check per context is counted — Patchwork creates
// new records for state changes, so older states are superseded.
func (d *DB) RecountPatchChecks(patchID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE patches SET
			checks_pass = (
				SELECT COUNT(*) FROM checks c
				WHERE c.patch_id = ? AND c.state = 'success'
				AND c.id = (SELECT MAX(c2.id) FROM checks c2
					WHERE c2.patch_id = c.patch_id
					AND c2.context = c.context)),
			checks_fail = (
				SELECT COUNT(*) FROM checks c
				WHERE c.patch_id = ? AND c.state = 'fail'
				AND c.id = (SELECT MAX(c2.id) FROM checks c2
					WHERE c2.patch_id = c.patch_id
					AND c2.context = c.context)),
			checks_warn = (
				SELECT COUNT(*) FROM checks c
				WHERE c.patch_id = ? AND c.state = 'warning'
				AND c.id = (SELECT MAX(c2.id) FROM checks c2
					WHERE c2.patch_id = c.patch_id
					AND c2.context = c.context))
		WHERE id = ?`,
		patchID, patchID, patchID, patchID)
	return err
}

func (d *DB) InsertComment(c CommentRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO comments
			(id, patch_id, cover_id, submitter,
			 submitter_email, date, subject,
			 content, msgid, headers,
			 web_url, list_archive_url)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.PatchID, c.CoverID, c.Submitter,
		c.SubmitterEmail, c.Date, c.Subject,
		c.Content, c.MsgID, c.Headers,
		c.WebURL, c.ListArchiveURL)
	return err
}

func (d *DB) SaveCover(c CoverRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO covers (id, series_id, name, date,
			submitter, submitter_email, msgid,
			web_url, mbox_url)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			series_id = CASE WHEN excluded.series_id != 0
				THEN excluded.series_id
				ELSE covers.series_id END,
			name = excluded.name,
			date = excluded.date,
			submitter = excluded.submitter,
			submitter_email = excluded.submitter_email,
			msgid = CASE WHEN excluded.msgid != ''
				THEN excluded.msgid ELSE covers.msgid END,
			web_url = excluded.web_url,
			mbox_url = CASE WHEN excluded.mbox_url != ''
				THEN excluded.mbox_url
				ELSE covers.mbox_url END`,
		c.ID, c.SeriesID, c.Name, c.Date,
		c.Submitter, c.SubmitterEmail,
		c.MsgID, c.WebURL, c.MboxURL)
	return err
}

func (d *DB) UpdateCoverDetail(coverID int, content, headers string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		UPDATE covers
		SET content = ?, headers = ?, detail_fetched = 1
		WHERE id = ?`,
		content, headers, coverID)
	return err
}

func (d *DB) SaveMaintainers(maintainers []MaintainerRow) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Preserve cached user_id values before replacing
	cachedIDs := map[string]int{}
	rows, _ := tx.Query(
		"SELECT username, user_id FROM maintainers WHERE user_id > 0")
	if rows != nil {
		for rows.Next() {
			var u string
			var uid int
			rows.Scan(&u, &uid)
			cachedIDs[u] = uid
		}
		rows.Close()
	}

	if _, err := tx.Exec("DELETE FROM maintainers"); err != nil {
		return err
	}
	for _, m := range maintainers {
		uid := cachedIDs[m.Username]
		if _, err := tx.Exec(`
			INSERT INTO maintainers
				(id, username, first_name, last_name, email, user_id)
			VALUES (?,?,?,?,?,?)`,
			m.ID, m.Username, m.FirstName,
			m.LastName, m.Email, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) SetSyncState(key, value string) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(`
		INSERT INTO sync_state (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
}

func (d *DB) GetSyncState(key string) string {
	var value string
	d.conn.QueryRow(
		"SELECT value FROM sync_state WHERE key = ?",
		key).Scan(&value)
	return value
}

func (d *DB) GetActiveSeries(states []string) []SeriesRow {
	if len(states) == 0 {
		return nil
	}
	placeholders := make([]string, len(states))
	args := make([]interface{}, len(states))
	for i, s := range states {
		placeholders[i] = "?"
		args[i] = s
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT s.id, s.name, s.date, s.version,
			s.submitter, s.submitter_email,
			s.web_url, s.mbox_url, s.complete,
			s.total_patches, s.received_patches,
			COALESCE(s.updated_at, '')
		FROM series s
		JOIN patches p ON p.series_id = s.id
		WHERE p.state IN (%s) AND p.archived = 0
		ORDER BY s.date DESC`,
		strings.Join(placeholders, ","))

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []SeriesRow
	for rows.Next() {
		var r SeriesRow
		rows.Scan(
			&r.ID, &r.Name, &r.Date, &r.Version,
			&r.Submitter, &r.SubmitterEmail,
			&r.WebURL, &r.MboxURL, &r.Complete,
			&r.TotalPatches, &r.ReceivedPatches,
			&r.UpdatedAt)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetSeriesTotalPatches(seriesID int) int {
	var total int
	d.conn.QueryRow(
		"SELECT COALESCE(total_patches, 0) FROM series WHERE id = ?",
		seriesID).Scan(&total)
	return total
}

func (d *DB) GetAllSeries() []SeriesRow {
	rows, err := d.conn.Query(`
		SELECT DISTINCT s.id, s.name, s.date, s.version,
			s.submitter, s.submitter_email,
			s.web_url, s.mbox_url, s.complete,
			s.total_patches, s.received_patches,
			COALESCE(s.updated_at, '')
		FROM series s
		JOIN patches p ON p.series_id = s.id
		ORDER BY s.date DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []SeriesRow
	for rows.Next() {
		var r SeriesRow
		rows.Scan(
			&r.ID, &r.Name, &r.Date, &r.Version,
			&r.Submitter, &r.SubmitterEmail,
			&r.WebURL, &r.MboxURL, &r.Complete,
			&r.TotalPatches, &r.ReceivedPatches,
			&r.UpdatedAt)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetPatchesForSeries(seriesID int) []PatchRow {
	rows, err := d.conn.Query(patchSelectSQL+
		` WHERE series_id = ? ORDER BY date ASC`,
		seriesID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanPatches(rows)
}

func (d *DB) GetPatch(patchID int) (*PatchRow, error) {
	row := d.conn.QueryRow(
		patchSelectSQL+` WHERE id = ?`, patchID)
	var r PatchRow
	err := scanPatchRow(row, &r)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

const patchSelectSQL = `
	SELECT id, COALESCE(series_id, 0), name, date, state,
		COALESCE(submitter, ''),
		COALESCE(submitter_email, ''),
		COALESCE(delegate_id, 0),
		COALESCE(delegate, ''),
		COALESCE(delegate_email, ''),
		COALESCE(web_url, ''),
		COALESCE(msgid, ''),
		COALESCE(mbox_url, ''),
		COALESCE(commit_ref, ''),
		COALESCE(archived, 0),
		COALESCE(checks_pass, 0),
		COALESCE(checks_fail, 0),
		COALESCE(checks_warn, 0),
		COALESCE(content, ''),
		COALESCE(diff, ''),
		COALESCE(headers, ''),
		COALESCE(prefixes, ''),
		detail_fetched,
		COALESCE(updated_at, '')
	FROM patches`

func scanPatchRow(
	row interface{ Scan(...interface{}) error },
	r *PatchRow,
) error {
	return row.Scan(
		&r.ID, &r.SeriesID, &r.Name, &r.Date,
		&r.State, &r.Submitter, &r.SubmitterEmail,
		&r.DelegateID, &r.Delegate, &r.DelegateEmail,
		&r.WebURL, &r.MsgID, &r.MboxURL, &r.CommitRef,
		&r.Archived, &r.ChecksPass, &r.ChecksFail,
		&r.ChecksWarn,
		&r.Content, &r.Diff, &r.Headers, &r.Prefixes,
		&r.DetailFetched, &r.UpdatedAt)
}

func scanPatches(rows *sql.Rows) []PatchRow {
	var result []PatchRow
	for rows.Next() {
		var r PatchRow
		scanPatchRow(rows, &r)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetMaintainers() []MaintainerRow {
	rows, err := d.conn.Query(`
		SELECT id, username, first_name, last_name, email
		FROM maintainers ORDER BY username`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []MaintainerRow
	for rows.Next() {
		var r MaintainerRow
		rows.Scan(&r.ID, &r.Username,
			&r.FirstName, &r.LastName, &r.Email)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetMaintainerUserID(username string) int {
	var uid int
	d.conn.QueryRow(
		`SELECT COALESCE(user_id, 0) FROM maintainers
		 WHERE username = ?`, username).Scan(&uid)
	return uid
}

func (d *DB) SetMaintainerUserID(username string, userID int) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(
		"UPDATE maintainers SET user_id = ? WHERE username = ?",
		userID, username)
}

func (d *DB) ClearMaintainerUserID(username string) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(
		"UPDATE maintainers SET user_id = 0 WHERE username = ?",
		username)
}

func (d *DB) GetDelegateDisplayNames() map[string]string {
	rows, err := d.conn.Query(`
		SELECT username, first_name FROM maintainers
		WHERE first_name != ''`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := map[string]string{}
	for rows.Next() {
		var username, firstName string
		rows.Scan(&username, &firstName)
		result[username] = firstName
	}
	return result
}

// FetchRef identifies an item that needs fetching, along with its
// parent series. Used by the syncer to track which items are being
// fetched and to show per-row spinners in the TUI.
// ActiveStates defines which patch states are considered "active"
// for background fetch prioritization. Patches in these states
// (and not archived) are fetched before terminal-state patches.
var ActiveStates = []string{
	"new", "under-review", "needs-ack", "awaiting-upstream",
}

func activeStateFilter() (string, []interface{}) {
	ph := make([]string, len(ActiveStates))
	args := make([]interface{}, len(ActiveStates))
	for i, s := range ActiveStates {
		ph[i] = "?"
		args[i] = s
	}
	return strings.Join(ph, ","), args
}

func (d *DB) RecomputeActiveFlag(seriesID int) {
	ph, args := activeStateFilter()
	var has int
	d.conn.QueryRow(fmt.Sprintf(
		`SELECT EXISTS (SELECT 1 FROM patches
		 WHERE series_id = ? AND archived = 0 AND state IN (%s))`, ph),
		append([]interface{}{seriesID}, args...)...).Scan(&has)
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec("UPDATE series SET has_active_patch = ? WHERE id = ?",
		has, seriesID)
	d.conn.Exec("UPDATE covers SET has_active_patch = ? WHERE series_id = ?",
		has, seriesID)
}

func (d *DB) RecomputeAllActiveFlags() {
	ph, args := activeStateFilter()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec("UPDATE series SET has_active_patch = 0 WHERE has_active_patch != 0")
	d.conn.Exec("UPDATE covers SET has_active_patch = 0 WHERE has_active_patch != 0")
	q := fmt.Sprintf(`UPDATE series SET has_active_patch = 1
		WHERE id IN (SELECT DISTINCT series_id FROM patches
		WHERE state IN (%s) AND archived = 0)`, ph)
	d.conn.Exec(q, args...)
	q = fmt.Sprintf(`UPDATE covers SET has_active_patch = 1
		WHERE series_id IN (SELECT DISTINCT series_id FROM patches
		WHERE state IN (%s) AND archived = 0)`, ph)
	d.conn.Exec(q, args...)
}

type FetchRef struct {
	ID       int  // patch or cover ID
	SeriesID int  // parent series ID (0 if unknown)
	IsActive bool // true if in an active state (e.g., new, under-review)
}

func scanFetchRefs(rows *sql.Rows, err error) []FetchRef {
	if err != nil {
		return nil
	}
	defer rows.Close()
	var refs []FetchRef
	for rows.Next() {
		var ref FetchRef
		var active int
		rows.Scan(&ref.ID, &ref.SeriesID, &active)
		ref.IsActive = active == 1
		refs = append(refs, ref)
	}
	return refs
}

func (d *DB) GetPatchesNeedingDetail(limit int) []FetchRef {
	ph, args := activeStateFilter()
	q1 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 1
		FROM patches
		WHERE detail_fetched = 0
			AND state IN (%s) AND archived = 0
		ORDER BY id DESC LIMIT ?`, ph)
	refs := scanFetchRefs(d.conn.Query(q1, append(args, limit)...))
	if len(refs) >= limit {
		return refs
	}
	q2 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 0
		FROM patches
		WHERE detail_fetched = 0
			AND NOT (state IN (%s) AND archived = 0)
		ORDER BY id DESC LIMIT ?`, ph)
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, append(args, limit-len(refs))...))...)
}

func (d *DB) GetCoversNeedingDetail(limit int) []FetchRef {
	q1 := `SELECT id, series_id, 1 FROM covers
		WHERE detail_fetched = 0 AND has_active_patch = 1
		ORDER BY id DESC LIMIT ?`
	refs := scanFetchRefs(d.conn.Query(q1, limit))
	if len(refs) >= limit {
		return refs
	}
	q2 := `SELECT id, series_id, 0 FROM covers
		WHERE detail_fetched = 0 AND has_active_patch = 0
		ORDER BY id DESC LIMIT ?`
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, limit-len(refs)))...)
}

func (d *DB) GetSeriesNeedingDetail(limit int) []FetchRef {
	q1 := `SELECT id, id, 1 FROM series
		WHERE detail_fetched = 0 AND has_active_patch = 1
		ORDER BY id DESC LIMIT ?`
	refs := scanFetchRefs(d.conn.Query(q1, limit))
	if len(refs) >= limit {
		return refs
	}
	q2 := `SELECT id, id, 0 FROM series
		WHERE detail_fetched = 0 AND has_active_patch = 0
		ORDER BY id DESC LIMIT ?`
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, limit-len(refs)))...)
}

type TagRow struct {
	PatchID   int
	CoverID   int
	CommentID int
	Source    string
	Type      string
	Identity  string
}

func (d *DB) ClearTags(patchID, coverID int, source string) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.Exec(`DELETE FROM tags
		WHERE patch_id = ? AND cover_id = ? AND source = ?`,
		patchID, coverID, source)
}

func (d *DB) SaveTags(
	patchID, coverID, commentID int,
	source string, tags map[string]map[string]bool,
) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	for tagType, identities := range tags {
		for identity := range identities {
			d.conn.Exec(`INSERT OR IGNORE INTO tags
				(patch_id, cover_id, comment_id,
				 source, type, identity)
				VALUES (?,?,?,?,?,?)`,
				patchID, coverID, commentID,
				source, tagType, identity)
		}
	}
}

func (d *DB) GetTagsForSeries(seriesID int) []TagRow {
	rows, err := d.conn.Query(`
		SELECT patch_id, cover_id, comment_id,
			source, type, identity
		FROM tags
		WHERE patch_id IN
			(SELECT id FROM patches WHERE series_id = ?)
		   OR cover_id IN
			(SELECT id FROM covers WHERE series_id = ?)`,
		seriesID, seriesID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []TagRow
	for rows.Next() {
		var r TagRow
		rows.Scan(&r.PatchID, &r.CoverID, &r.CommentID,
			&r.Source, &r.Type, &r.Identity)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetCommentCountForSeries(seriesID int) int {
	var count int
	d.conn.QueryRow(`
		SELECT COUNT(*) FROM comments
		WHERE patch_id IN
			(SELECT id FROM patches WHERE series_id = ?)
		   OR cover_id IN
			(SELECT id FROM covers WHERE series_id = ?)`,
		seriesID, seriesID).Scan(&count)
	return count
}

// seriesIDSubquery returns a SQL subquery that selects series IDs
// matching the current view filter.  When showAll is true, all
// series that have at least one patch are included.  When false,
// only series that have at least one patch in the given states
// are included.  The subquery is used by the batch methods below
// to avoid passing large ID lists as parameters — the number of
// query parameters is always constant (just the state values).
func (d *DB) seriesIDSubquery(
	showAll bool, states []string,
) (string, []interface{}) {
	if showAll {
		return `SELECT DISTINCT s.id FROM series s
			JOIN patches p ON p.series_id = s.id`, nil
	}
	parts := make([]string, len(states))
	args := make([]interface{}, len(states))
	for i, s := range states {
		parts[i] = "?"
		args[i] = s
	}
	return fmt.Sprintf(`SELECT DISTINCT s.id FROM series s
		JOIN patches p ON p.series_id = s.id
		WHERE p.state IN (%s) AND p.archived = 0`,
		strings.Join(parts, ",")), args
}

// GetAllPatchesBatch fetches all patches for all matching series
// in a single query, returning them grouped by series_id.  Note
// that ALL patches for a matching series are returned, even if
// only some of the patches match the state filter — the filter
// determines which series are included, not which patches.
func (d *DB) GetAllPatchesBatch(
	showAll bool, states []string,
) map[int][]PatchRow {
	sub, args := d.seriesIDSubquery(showAll, states)
	query := patchSelectSQL +
		` WHERE series_id IN (` + sub + `) ORDER BY series_id, id`
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int][]PatchRow{}
	for rows.Next() {
		var r PatchRow
		scanPatchRow(rows, &r)
		result[r.SeriesID] = append(result[r.SeriesID], r)
	}
	return result
}

// GetTagsBatch fetches all tags for all matching series in a
// single query.  Tags can be on patches (patch_id > 0) or on
// covers (cover_id > 0).  The LEFT JOINs with patches and
// covers resolve the series_id for each tag.  The subquery
// args are passed twice — once for the patches join filter and
// once for the covers join filter.
func (d *DB) GetTagsBatch(
	showAll bool, states []string,
) map[int][]TagRow {
	sub, subArgs := d.seriesIDSubquery(showAll, states)
	query := `SELECT t.patch_id, t.cover_id, t.comment_id,
		t.source, t.type, t.identity,
		COALESCE(p.series_id, c.series_id) as series_id
		FROM tags t
		LEFT JOIN patches p ON t.patch_id = p.id
			AND t.patch_id > 0
		LEFT JOIN covers c ON t.cover_id = c.id
			AND t.cover_id > 0
		WHERE p.series_id IN (` + sub + `)
		   OR c.series_id IN (` + sub + `)`
	args := append(subArgs, subArgs...)
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int][]TagRow{}
	for rows.Next() {
		var r TagRow
		var seriesID int
		rows.Scan(&r.PatchID, &r.CoverID, &r.CommentID,
			&r.Source, &r.Type, &r.Identity, &seriesID)
		result[seriesID] = append(result[seriesID], r)
	}
	return result
}

// GetCommentCountsBatch counts comments for all matching series
// in a single query.  Comments can be on patches or covers, so
// we use UNION ALL to combine both sources, then GROUP BY
// series_id.  Series with no comments are not in the result
// map — the caller gets zero from the Go map's default value.
// The subquery args are passed twice (patches + covers).
func (d *DB) GetCommentCountsBatch(
	showAll bool, states []string,
) map[int]int {
	sub, subArgs := d.seriesIDSubquery(showAll, states)
	query := `SELECT sub.series_id, COUNT(*) FROM (
		SELECT p.series_id FROM comments c
		JOIN patches p ON c.patch_id = p.id
		WHERE p.series_id IN (` + sub + `)
		UNION ALL
		SELECT cv.series_id FROM comments c
		JOIN covers cv ON c.cover_id = cv.id
		WHERE cv.series_id IN (` + sub + `)
	) sub GROUP BY sub.series_id`
	args := append(subArgs, subArgs...)
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int]int{}
	for rows.Next() {
		var seriesID, count int
		rows.Scan(&seriesID, &count)
		result[seriesID] = count
	}
	return result
}

// GetPatchCommentCountsBatch counts comments per patch for all
// patches in matching series. Returns map[patchID]count.
func (d *DB) GetPatchCommentCountsBatch(
	showAll bool, states []string,
) map[int]int {
	sub, subArgs := d.seriesIDSubquery(showAll, states)
	query := `SELECT c.patch_id, COUNT(*) FROM comments c
		JOIN patches p ON c.patch_id = p.id
		WHERE p.series_id IN (` + sub + `)
		GROUP BY c.patch_id`
	rows, err := d.conn.Query(query, subArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int]int{}
	for rows.Next() {
		var patchID, count int
		rows.Scan(&patchID, &count)
		result[patchID] = count
	}
	return result
}

// GetCommentSubmittersBatch returns unique submitter names per series,
// ordered by first appearance. Includes both patch and cover comments.
func (d *DB) GetCommentSubmittersBatch(
	showAll bool, states []string,
) map[int][]string {
	sub, subArgs := d.seriesIDSubquery(showAll, states)
	query := `SELECT sub.series_id, sub.submitter FROM (
		SELECT p.series_id, c.submitter, c.date FROM comments c
		JOIN patches p ON c.patch_id = p.id WHERE p.series_id IN (` + sub + `)
		UNION ALL
		SELECT cv.series_id, c.submitter, c.date FROM comments c
		JOIN covers cv ON c.cover_id = cv.id WHERE cv.series_id IN (` + sub + `)
	) sub ORDER BY sub.series_id, sub.date`
	args := append(subArgs, subArgs...)
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int][]string{}
	seen := map[int]map[string]bool{}
	for rows.Next() {
		var seriesID int
		var submitter string
		rows.Scan(&seriesID, &submitter)
		if seen[seriesID] == nil {
			seen[seriesID] = map[string]bool{}
		}
		if !seen[seriesID][submitter] {
			seen[seriesID][submitter] = true
			result[seriesID] = append(result[seriesID], submitter)
		}
	}
	return result
}

// GetPatchCommentSubmittersBatch returns unique submitter names per patch,
// ordered by first appearance. Only patch comments (not cover).
func (d *DB) GetPatchCommentSubmittersBatch(
	showAll bool, states []string,
) map[int][]string {
	sub, subArgs := d.seriesIDSubquery(showAll, states)
	query := `SELECT c.patch_id, c.submitter FROM comments c
		JOIN patches p ON c.patch_id = p.id
		WHERE p.series_id IN (` + sub + `)
		ORDER BY c.patch_id, c.date`
	rows, err := d.conn.Query(query, subArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int][]string{}
	seen := map[int]map[string]bool{}
	for rows.Next() {
		var patchID int
		var submitter string
		rows.Scan(&patchID, &submitter)
		if seen[patchID] == nil {
			seen[patchID] = map[string]bool{}
		}
		if !seen[patchID][submitter] {
			seen[patchID][submitter] = true
			result[patchID] = append(result[patchID], submitter)
		}
	}
	return result
}

func (d *DB) GetPatchIDsWithComments() []int {
	return d.getIDList(
		"SELECT DISTINCT patch_id FROM comments WHERE patch_id > 0")
}

func (d *DB) GetCoverIDsWithComments() []int {
	return d.getIDList(
		"SELECT DISTINCT cover_id FROM comments WHERE cover_id > 0")
}

func (d *DB) getIDList(query string) []int {
	rows, err := d.conn.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func (d *DB) GetPatchContent() map[int]string {
	return d.getContentMap(
		"SELECT id, content FROM patches WHERE content != ''")
}

func (d *DB) GetCoverContent() map[int]string {
	return d.getContentMap(
		"SELECT id, content FROM covers WHERE content != ''")
}

func (d *DB) getContentMap(query string) map[int]string {
	rows, err := d.conn.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[int]string{}
	for rows.Next() {
		var id int
		var content string
		rows.Scan(&id, &content)
		result[id] = content
	}
	return result
}

func (d *DB) GetAllPatchNames() map[int]string {
	return d.getAllNames("patches")
}

func (d *DB) GetAllCoverNames() map[int]string {
	return d.getAllNames("covers")
}

func (d *DB) getAllNames(table string) map[int]string {
	rows, err := d.conn.Query(
		"SELECT id, name FROM " + table)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := map[int]string{}
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		result[id] = name
	}
	return result
}

func (d *DB) GetOldestPatchDate() string {
	var date string
	d.conn.QueryRow(
		"SELECT COALESCE(MIN(date), '') FROM patches WHERE archived = 0",
	).Scan(&date)
	return date
}

func (d *DB) GetPatchesNeedingComments(limit int) []FetchRef {
	ph, args := activeStateFilter()
	q1 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 1
		FROM patches
		WHERE comments_fetched = 0
			AND state IN (%s) AND archived = 0
		ORDER BY id DESC LIMIT ?`, ph)
	refs := scanFetchRefs(d.conn.Query(q1, append(args, limit)...))
	if len(refs) >= limit {
		return refs
	}
	q2 := fmt.Sprintf(`
		SELECT id, COALESCE(series_id, 0), 0
		FROM patches
		WHERE comments_fetched = 0
			AND NOT (state IN (%s) AND archived = 0)
		ORDER BY id DESC LIMIT ?`, ph)
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, append(args, limit-len(refs))...))...)
}

func (d *DB) MarkCommentsFetched(patchID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE patches SET comments_fetched = 1 WHERE id = ?",
		patchID)
	return err
}

func (d *DB) ResetCommentsFetched(patchID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE patches SET comments_fetched = 0 WHERE id = ?",
		patchID)
	return err
}

// GetChecksForPatch returns the latest check per context for a patch.
// Patchwork creates new records for state changes, so we only return
// the most recent (highest ID) per context.
func (d *DB) GetChecksForPatch(patchID int) []CheckRow {
	rows, err := d.conn.Query(`
		SELECT id, patch_id, context, state,
			COALESCE(target_url, ''),
			COALESCE(description, ''),
			COALESCE(date, '')
		FROM checks
		WHERE patch_id = ?
			AND id = (SELECT MAX(c2.id) FROM checks c2
				WHERE c2.patch_id = checks.patch_id
				AND c2.context = checks.context)
		ORDER BY context`, patchID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CheckRow
	for rows.Next() {
		var r CheckRow
		rows.Scan(&r.ID, &r.PatchID, &r.Context,
			&r.State, &r.TargetURL, &r.Description, &r.Date)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetComments(patchID int) []CommentRow {
	rows, err := d.conn.Query(`
		SELECT id, patch_id, cover_id, submitter,
			COALESCE(submitter_email, ''),
			date, subject, content, msgid,
			COALESCE(headers, ''),
			COALESCE(web_url, ''),
			COALESCE(list_archive_url, '')
		FROM comments WHERE patch_id = ?
		ORDER BY date`, patchID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CommentRow
	for rows.Next() {
		var r CommentRow
		rows.Scan(&r.ID, &r.PatchID, &r.CoverID,
			&r.Submitter, &r.SubmitterEmail,
			&r.Date, &r.Subject, &r.Content, &r.MsgID,
			&r.Headers, &r.WebURL, &r.ListArchiveURL)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetCommentsForCover(coverID int) []CommentRow {
	rows, err := d.conn.Query(`
		SELECT id, patch_id, cover_id, submitter,
			COALESCE(submitter_email, ''),
			date, subject, content, msgid,
			COALESCE(headers, ''),
			COALESCE(web_url, ''),
			COALESCE(list_archive_url, '')
		FROM comments WHERE cover_id = ?
		ORDER BY date`, coverID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CommentRow
	for rows.Next() {
		var r CommentRow
		rows.Scan(&r.ID, &r.PatchID, &r.CoverID,
			&r.Submitter, &r.SubmitterEmail,
			&r.Date, &r.Subject, &r.Content, &r.MsgID,
			&r.Headers, &r.WebURL, &r.ListArchiveURL)
		result = append(result, r)
	}
	return result
}

func (d *DB) GetCoversNeedingComments(limit int) []FetchRef {
	q1 := `SELECT id, series_id, 1 FROM covers
		WHERE comments_fetched = 0 AND has_active_patch = 1
		ORDER BY id DESC LIMIT ?`
	refs := scanFetchRefs(d.conn.Query(q1, limit))
	if len(refs) >= limit {
		return refs
	}
	q2 := `SELECT id, series_id, 0 FROM covers
		WHERE comments_fetched = 0 AND has_active_patch = 0
		ORDER BY id DESC LIMIT ?`
	return append(refs,
		scanFetchRefs(d.conn.Query(q2, limit-len(refs)))...)
}

func (d *DB) MarkCoverCommentsFetched(coverID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE covers SET comments_fetched = 1 WHERE id = ?",
		coverID)
	return err
}

func (d *DB) ResetCoverCommentsFetched(coverID int) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		"UPDATE covers SET comments_fetched = 0 WHERE id = ?",
		coverID)
	return err
}

func (d *DB) NeedsPatchDetail(patchID int) bool {
	var fetched int
	err := d.conn.QueryRow(
		"SELECT detail_fetched FROM patches WHERE id = ?",
		patchID).Scan(&fetched)
	return err == nil && fetched == 0
}

func (d *DB) NeedsCoverDetail(coverID int) bool {
	var fetched int
	err := d.conn.QueryRow(
		"SELECT detail_fetched FROM covers WHERE id = ?",
		coverID).Scan(&fetched)
	return err == nil && fetched == 0
}

func (d *DB) NeedsPatchChecks(patchID int) bool {
	var fetched int
	err := d.conn.QueryRow(
		"SELECT checks_fetched FROM patches WHERE id = ?",
		patchID).Scan(&fetched)
	return err == nil && fetched == 0
}

func (d *DB) NeedsPatchComments(patchID int) bool {
	var fetched int
	err := d.conn.QueryRow(
		"SELECT comments_fetched FROM patches WHERE id = ?",
		patchID).Scan(&fetched)
	return err == nil && fetched == 0
}

func (d *DB) NeedsCoverComments(coverID int) bool {
	var fetched int
	err := d.conn.QueryRow(
		"SELECT comments_fetched FROM covers WHERE id = ?",
		coverID).Scan(&fetched)
	return err == nil && fetched == 0
}

func (d *DB) GetCover(seriesID int) (*CoverRow, error) {
	row := d.conn.QueryRow(`
		SELECT id, series_id, name, date,
			submitter, submitter_email, msgid,
			web_url, mbox_url, content, headers,
			detail_fetched
		FROM covers WHERE series_id = ?`, seriesID)
	var r CoverRow
	err := row.Scan(
		&r.ID, &r.SeriesID, &r.Name, &r.Date,
		&r.Submitter, &r.SubmitterEmail, &r.MsgID,
		&r.WebURL, &r.MboxURL, &r.Content, &r.Headers,
		&r.DetailFetched)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
