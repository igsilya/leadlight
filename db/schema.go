package db

import "database/sql"

const schema = `
CREATE TABLE IF NOT EXISTS maintainers (
    id          INTEGER PRIMARY KEY,
    username    TEXT NOT NULL,
    first_name  TEXT,
    last_name   TEXT,
    email       TEXT
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
    total_patches     INTEGER DEFAULT 0,
    received_patches  INTEGER DEFAULT 0,
    updated_at        TEXT
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
    checks_pending    INTEGER DEFAULT 0,
    comments_count    INTEGER DEFAULT 0,
    acked_by          INTEGER DEFAULT 0,
    fixes             INTEGER DEFAULT 0,
    reviewed_by       INTEGER DEFAULT 0,
    tested_by         INTEGER DEFAULT 0,
    content           TEXT DEFAULT '',
    diff              TEXT DEFAULT '',
    headers           TEXT DEFAULT '',
    prefixes          TEXT DEFAULT '',
    mbox_content      TEXT DEFAULT '',
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
    mbox_content      TEXT DEFAULT '',
    detail_fetched    INTEGER DEFAULT 0
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
    id          INTEGER PRIMARY KEY,
    patch_id    INTEGER DEFAULT 0,
    cover_id    INTEGER DEFAULT 0,
    submitter   TEXT,
    date        TEXT,
    subject     TEXT,
    content     TEXT,
    msgid       TEXT
);

CREATE TABLE IF NOT EXISTS sync_state (
    key   TEXT PRIMARY KEY,
    value TEXT
);
`

const recountChecks = `
UPDATE patches SET
  checks_pass = (
    SELECT COUNT(*) FROM checks
    WHERE checks.patch_id = patches.id
    AND state = 'success'),
  checks_fail = (
    SELECT COUNT(*) FROM checks
    WHERE checks.patch_id = patches.id
    AND state = 'fail'),
  checks_pending = (
    SELECT COUNT(*) FROM checks
    WHERE checks.patch_id = patches.id
    AND state = 'pending')
  + (SELECT COUNT(*) FROM checks
    WHERE checks.patch_id = patches.id
    AND state = 'warning')
WHERE id IN (
  SELECT DISTINCT patch_id FROM checks
);
`

var alterStatements = []string{
	`ALTER TABLE patches ADD COLUMN comments_fetched INTEGER DEFAULT 0`,
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	for _, stmt := range alterStatements {
		db.Exec(stmt) // ignore "duplicate column" errors
	}
	_, err := db.Exec(recountChecks)
	return err
}
