// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"database/sql"
	"fmt"
	"strings"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id                       INTEGER PRIMARY KEY,
    name                     TEXT NOT NULL,
    list_archive_url_format  TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS maintainers (
    id          INTEGER PRIMARY KEY,
    username    TEXT NOT NULL,
    first_name  TEXT,
    last_name   TEXT,
    email       TEXT,
    user_id     INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS series (
    id                INTEGER PRIMARY KEY,
    name              TEXT NOT NULL,
    date              TEXT NOT NULL,
    version           INTEGER,
    submitter         TEXT,
    submitter_email   TEXT,
    web_url           TEXT,
    mbox_url          TEXT,
    complete          INTEGER DEFAULT 0,
    total_patches      INTEGER DEFAULT 0,
    received_patches   INTEGER DEFAULT 0,
    detail_fetched     INTEGER DEFAULT 0,
    has_active_patch   INTEGER DEFAULT 0,
    updated_at         TEXT
);

CREATE TABLE IF NOT EXISTS patches (
    id                INTEGER PRIMARY KEY,
    series_id         INTEGER REFERENCES series(id),
    name              TEXT NOT NULL,
    date              TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT 'new',
    submitter         TEXT,
    submitter_email   TEXT,
    delegate_id       INTEGER DEFAULT 0,
    delegate          TEXT DEFAULT '',
    delegate_email    TEXT DEFAULT '',
    web_url           TEXT,
    msgid             TEXT,
    mbox_url          TEXT,
    commit_ref        TEXT,
    archived          INTEGER DEFAULT 0,
    checks_pass       INTEGER DEFAULT 0,
    checks_fail       INTEGER DEFAULT 0,
    checks_warn       INTEGER DEFAULT 0,
    -- Legacy counters: superseded by the tags and comments tables.
    -- Kept to avoid ALTER TABLE migrations on existing databases.
    comments_count    INTEGER DEFAULT 0,
    acked_by          INTEGER DEFAULT 0,
    fixes             INTEGER DEFAULT 0,
    reviewed_by       INTEGER DEFAULT 0,
    tested_by         INTEGER DEFAULT 0,
    content           TEXT DEFAULT '',
    diff              TEXT DEFAULT '',
    headers           TEXT DEFAULT '',
    prefixes          TEXT DEFAULT '',
    detail_fetched    INTEGER DEFAULT 0,
    comments_fetched  INTEGER DEFAULT 0,
    updated_at        TEXT
);

CREATE TABLE IF NOT EXISTS covers (
    id                INTEGER PRIMARY KEY,
    series_id         INTEGER REFERENCES series(id),
    name              TEXT,
    date              TEXT,
    submitter         TEXT,
    submitter_email   TEXT,
    msgid             TEXT,
    web_url           TEXT,
    mbox_url          TEXT,
    content           TEXT DEFAULT '',
    headers           TEXT DEFAULT '',
    detail_fetched    INTEGER DEFAULT 0,
    comments_fetched  INTEGER DEFAULT 0,
    has_active_patch  INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS checks (
    id          INTEGER PRIMARY KEY,
    patch_id    INTEGER REFERENCES patches(id),
    context     TEXT,
    state       TEXT,
    target_url  TEXT,
    date        TEXT
);

CREATE TABLE IF NOT EXISTS comments (
    id               INTEGER PRIMARY KEY,
    patch_id         INTEGER DEFAULT 0,
    cover_id         INTEGER DEFAULT 0,
    submitter        TEXT,
    date             TEXT,
    subject          TEXT,
    content          TEXT,
    msgid            TEXT,
    submitter_email  TEXT DEFAULT '',
    headers          TEXT DEFAULT '',
    web_url          TEXT DEFAULT '',
    list_archive_url TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS sync_state (
    key   TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS tags (
    patch_id   INTEGER DEFAULT 0,
    cover_id   INTEGER DEFAULT 0,
    comment_id INTEGER DEFAULT 0,
    source     TEXT NOT NULL,
    type       TEXT NOT NULL,
    identity   TEXT NOT NULL,
    UNIQUE(patch_id, cover_id, source, type, identity)
);
CREATE INDEX IF NOT EXISTS idx_checks_patch_context ON checks(patch_id, context, id);
CREATE INDEX IF NOT EXISTS idx_tags_patch ON tags(patch_id);
CREATE INDEX IF NOT EXISTS idx_tags_cover ON tags(cover_id);
CREATE INDEX IF NOT EXISTS idx_patches_series ON patches(series_id);
CREATE INDEX IF NOT EXISTS idx_comments_patch ON comments(patch_id);
CREATE INDEX IF NOT EXISTS idx_comments_cover ON comments(cover_id);
CREATE INDEX IF NOT EXISTS idx_covers_series ON covers(series_id);
`

// Reset checks_fetched for patches that have check records with empty
// descriptions. This triggers the background check loop to re-fetch
// the full check data including descriptions from the API.
const resetChecksWithoutDescriptions = `
UPDATE patches SET checks_fetched = 0
WHERE checks_fetched = 1
AND id IN (
  SELECT DISTINCT patch_id FROM checks
  WHERE COALESCE(description, '') = ''
);
`

// Recount check totals on every startup to repair inconsistencies from
// interrupted syncs or schema changes. Pending checks are excluded —
// they'll resolve to pass/fail/warning eventually.
// latestCheckSubquery filters to the most recent check per context.
// Patchwork creates a new check record (new ID) for each state change,
// so a context like "ai-review" may have both a "pending" and a "success"
// record. We only count the latest (highest ID) per context.
const latestCheckSubquery = `c.id = (
  SELECT MAX(c2.id) FROM checks c2
  WHERE c2.patch_id = c.patch_id AND c2.context = c.context)`

const recountChecks = `
UPDATE patches SET
  checks_pass = (
    SELECT COUNT(*) FROM checks c
    WHERE c.patch_id = patches.id AND c.state = 'success'
    AND ` + latestCheckSubquery + `),
  checks_fail = (
    SELECT COUNT(*) FROM checks c
    WHERE c.patch_id = patches.id AND c.state = 'fail'
    AND ` + latestCheckSubquery + `),
  checks_warn = (
    SELECT COUNT(*) FROM checks c
    WHERE c.patch_id = patches.id AND c.state = 'warning'
    AND ` + latestCheckSubquery + `)
WHERE id IN (
  SELECT DISTINCT patch_id FROM checks
);
`

var alterStatements = []string{
	`ALTER TABLE patches ADD COLUMN comments_fetched INTEGER DEFAULT 0`,
	`ALTER TABLE covers ADD COLUMN comments_fetched INTEGER DEFAULT 0`,
	`ALTER TABLE maintainers ADD COLUMN user_id INTEGER DEFAULT 0`,
	`ALTER TABLE comments ADD COLUMN submitter_email TEXT DEFAULT ''`,
	`ALTER TABLE comments ADD COLUMN headers TEXT DEFAULT ''`,
	`ALTER TABLE comments ADD COLUMN web_url TEXT DEFAULT ''`,
	`ALTER TABLE comments ADD COLUMN list_archive_url TEXT DEFAULT ''`,
	// Requires SQLite 3.25+ (2018). Fixes the naming: this column
	// stores warning count, not pending count.
	`ALTER TABLE patches RENAME COLUMN checks_pending TO checks_warn`,
	`ALTER TABLE checks ADD COLUMN description TEXT DEFAULT ''`,
	`ALTER TABLE patches ADD COLUMN checks_fetched INTEGER DEFAULT 0`,
	// Requires SQLite 3.35+ (2021). Frees space from cached mbox
	// content now that the mbox view is built from detail API data.
	`ALTER TABLE patches DROP COLUMN mbox_content`,
	`ALTER TABLE covers DROP COLUMN mbox_content`,
	`ALTER TABLE series ADD COLUMN detail_fetched INTEGER DEFAULT 0`,
	// Normalize NULLs to 0 for partial index compatibility.
	`UPDATE patches SET checks_fetched = 0 WHERE checks_fetched IS NULL`,
	`UPDATE patches SET detail_fetched = 0 WHERE detail_fetched IS NULL`,
	`UPDATE covers SET detail_fetched = 0 WHERE detail_fetched IS NULL`,
	// Pull request URL for filtering out pull request patches from
	// synthetic series creation.
	`ALTER TABLE patches ADD COLUMN pull_url TEXT DEFAULT ''`,
	// Denormalized active flag for fast cover/series priority lookups.
	`ALTER TABLE series ADD COLUMN has_active_patch INTEGER DEFAULT 0`,
	`ALTER TABLE covers ADD COLUMN has_active_patch INTEGER DEFAULT 0`,
	// Drop old simple partial indexes (replaced by compound covering indexes).
	`DROP INDEX IF EXISTS idx_patches_comments_unfetched`,
	`DROP INDEX IF EXISTS idx_patches_detail_unfetched`,
	`DROP INDEX IF EXISTS idx_patches_checks_unfetched`,
	`DROP INDEX IF EXISTS idx_covers_comments_unfetched`,
	`DROP INDEX IF EXISTS idx_covers_detail_unfetched`,
	`DROP INDEX IF EXISTS idx_series_detail_unfetched`,
	// Patches: active step (SEARCH by state, covering with series_id).
	`CREATE INDEX IF NOT EXISTS idx_patches_comments_active ON patches (state, id DESC, series_id) WHERE comments_fetched = 0 AND archived = 0`,
	`CREATE INDEX IF NOT EXISTS idx_patches_detail_active ON patches (state, id DESC, series_id) WHERE detail_fetched = 0 AND archived = 0`,
	`CREATE INDEX IF NOT EXISTS idx_patches_checks_active ON patches (state, id DESC, series_id) WHERE checks_fetched = 0 AND archived = 0`,
	// Patches: terminal step (covering scan, first entry matches).
	`CREATE INDEX IF NOT EXISTS idx_patches_comments_unfetched ON patches (id DESC, series_id) WHERE comments_fetched = 0`,
	`CREATE INDEX IF NOT EXISTS idx_patches_detail_unfetched ON patches (id DESC, series_id) WHERE detail_fetched = 0`,
	`CREATE INDEX IF NOT EXISTS idx_patches_checks_unfetched ON patches (id DESC, series_id) WHERE checks_fetched = 0`,
	// Active series lookup (for RecomputeActiveFlag).
	`CREATE INDEX IF NOT EXISTS idx_patches_active_series ON patches (state, series_id) WHERE archived = 0`,
	// Covers: by has_active_patch flag (SEARCH, no subquery).
	`CREATE INDEX IF NOT EXISTS idx_covers_detail_unfetched ON covers (has_active_patch, id DESC, series_id) WHERE detail_fetched = 0`,
	`CREATE INDEX IF NOT EXISTS idx_covers_comments_unfetched ON covers (has_active_patch, id DESC, series_id) WHERE comments_fetched = 0`,
	// Series: by has_active_patch flag.
	`CREATE INDEX IF NOT EXISTS idx_series_detail_unfetched ON series (has_active_patch, id DESC) WHERE detail_fetched = 0`,
}

func recomputeAllActiveFlags(db *sql.DB) {
	ph := make([]string, len(ActiveStates))
	args := make([]interface{}, len(ActiveStates))
	for i, s := range ActiveStates {
		ph[i] = "?"
		args[i] = s
	}
	inClause := strings.Join(ph, ",")
	db.Exec("UPDATE series SET has_active_patch = 0 WHERE has_active_patch != 0")
	db.Exec("UPDATE covers SET has_active_patch = 0 WHERE has_active_patch != 0")
	q := fmt.Sprintf(`UPDATE series SET has_active_patch = 1
		WHERE id IN (SELECT DISTINCT series_id FROM patches
		WHERE state IN (%s) AND archived = 0)`, inClause)
	db.Exec(q, args...)
	q = fmt.Sprintf(`UPDATE covers SET has_active_patch = 1
		WHERE series_id IN (SELECT DISTINCT series_id FROM patches
		WHERE state IN (%s) AND archived = 0)`, inClause)
	db.Exec(q, args...)
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	for _, stmt := range alterStatements {
		db.Exec(stmt) // ignore "duplicate column" errors
	}
	db.Exec(`DELETE FROM checks WHERE id NOT IN (
		SELECT MAX(id) FROM checks GROUP BY patch_id, context)`)
	_, err := db.Exec(recountChecks)
	if err != nil {
		return err
	}
	db.Exec(resetChecksWithoutDescriptions)
	recomputeAllActiveFlags(db)
	// Bump this version when comment schema changes require re-fetch
	const commentSchemaVersion = "2"
	var ver string
	db.QueryRow(
		"SELECT value FROM sync_state WHERE key = 'comment_schema'",
	).Scan(&ver)
	if ver != commentSchemaVersion {
		db.Exec("UPDATE patches SET comments_fetched = 0")
		db.Exec("UPDATE covers SET comments_fetched = 0")
		db.Exec(`INSERT OR REPLACE INTO sync_state
			(key, value) VALUES ('comment_schema', ?)`,
			commentSchemaVersion)
	}
	return nil
}
