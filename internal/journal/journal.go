package journal

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StateQueued   = "queued"
	StateFetched  = "fetched"
	StatePushing  = "pushing"
	StateDone     = "done"
	StateConflict = "conflict"
	StateFailed   = "failed"
)

type Job struct {
	ID        int64
	Repo      string
	Ref       string
	Source    string
	SHA       string
	State     string
	PushedFj  bool
	PushedGL  bool
	PushedGH  bool
	Attempts  int
	Error     string
	CreatedAt int64
}

type Block struct {
	Repo string
	Ref  string
	PR   int
	A    string
	B    string
}

type DB struct {
	sql *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return &DB{sql: conn}, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return err
	}
	for _, step := range []struct {
		version int
		sql     string
	}{
		{1, `
CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL,
  ref TEXT NOT NULL,
  source TEXT NOT NULL,
  sha TEXT NOT NULL,
  state TEXT NOT NULL,
  pushed_fj INTEGER NOT NULL DEFAULT 0,
  pushed_gl INTEGER NOT NULL DEFAULT 0,
  pushed_gh INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  created_at INTEGER NOT NULL,
  UNIQUE(repo, ref, source, sha)
);
CREATE INDEX IF NOT EXISTS jobs_incomplete ON jobs(state, id);
CREATE TABLE IF NOT EXISTS blocked (
  repo TEXT NOT NULL,
  ref TEXT NOT NULL,
  pr INTEGER NOT NULL,
  a TEXT NOT NULL,
  b TEXT NOT NULL,
  PRIMARY KEY (repo, ref)
);`},
		{2, `
CREATE TABLE IF NOT EXISTS seen (
  repo TEXT NOT NULL,
  ref TEXT NOT NULL,
  sha TEXT NOT NULL,
  PRIMARY KEY (repo, ref, sha)
);`},
	} {
		if v >= step.version {
			continue
		}
		if _, err := db.Exec(step.sql); err != nil {
			return fmt.Errorf("migrate to v%d: %w", step.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, step.version); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) Close() error { return d.sql.Close() }

// Enqueue inserts a job. already=true if this exact event was already journaled
// or a done job already recorded this repo/ref/sha from any source.
func (d *DB) Enqueue(repo, ref, source, sha string) (job *Job, already bool, err error) {
	if sha == "" {
		return nil, true, nil
	}
	seen, err := d.Seen(repo, ref, sha)
	if err != nil {
		return nil, false, err
	}
	if seen {
		return nil, true, nil
	}
	var n int
	err = d.sql.QueryRow(
		`SELECT COUNT(*) FROM jobs WHERE repo=? AND ref=? AND sha=? AND state=?`,
		repo, ref, sha, StateDone,
	).Scan(&n)
	if err != nil {
		return nil, false, err
	}
	if n > 0 {
		return nil, true, nil
	}
	now := time.Now().Unix()
	res, err := d.sql.Exec(
		`INSERT OR IGNORE INTO jobs (repo, ref, source, sha, state, created_at) VALUES (?,?,?,?,?,?)`,
		repo, ref, source, sha, StateQueued, now,
	)
	if err != nil {
		return nil, false, err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if aff == 0 {
		return nil, true, nil
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	j, err := d.Get(id)
	return j, false, err
}

func (d *DB) Get(id int64) (*Job, error) {
	j := &Job{}
	var pf, pg, ph int
	err := d.sql.QueryRow(
		`SELECT id, repo, ref, source, sha, state, pushed_fj, pushed_gl, pushed_gh, attempts, IFNULL(error,''), created_at
		 FROM jobs WHERE id=?`, id,
	).Scan(&j.ID, &j.Repo, &j.Ref, &j.Source, &j.SHA, &j.State, &pf, &pg, &ph, &j.Attempts, &j.Error, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	j.PushedFj, j.PushedGL, j.PushedGH = pf == 1, pg == 1, ph == 1
	return j, nil
}

func (d *DB) NextIncomplete(maxAttempts int) (*Job, error) {
	row := d.sql.QueryRow(
		`SELECT id FROM jobs
		 WHERE state IN (?, ?, ?) AND attempts < ?
		 ORDER BY id ASC LIMIT 1`,
		StateQueued, StateFetched, StatePushing, maxAttempts,
	)
	var id int64
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.Get(id)
}

func (d *DB) SetState(id int64, state, errMsg string) error {
	_, err := d.sql.Exec(`UPDATE jobs SET state=?, error=? WHERE id=?`, state, errMsg, id)
	return err
}

func (d *DB) BumpAttempt(id int64, errMsg string) error {
	_, err := d.sql.Exec(`UPDATE jobs SET attempts=attempts+1, error=? WHERE id=?`, errMsg, id)
	return err
}

func (d *DB) MarkFetched(id int64) error {
	_, err := d.sql.Exec(`UPDATE jobs SET state=? WHERE id=?`, StateFetched, id)
	return err
}

func (d *DB) MarkPushed(id int64, remote string) error {
	col := remoteColumn(remote)
	if col == "" {
		return fmt.Errorf("unknown remote %q", remote)
	}
	_, err := d.sql.Exec(`UPDATE jobs SET `+col+`=1, state=? WHERE id=?`, StatePushing, id)
	return err
}

func (d *DB) MarkDone(id int64) error {
	j, err := d.Get(id)
	if err != nil {
		return err
	}
	if err := d.Remember(j.Repo, j.Ref, j.SHA); err != nil {
		return err
	}
	_, err = d.sql.Exec(`UPDATE jobs SET state=? WHERE id=?`, StateDone, id)
	return err
}

func (d *DB) Remember(repo, ref, sha string) error {
	if sha == "" {
		return nil
	}
	_, err := d.sql.Exec(`INSERT OR IGNORE INTO seen (repo, ref, sha) VALUES (?,?,?)`, repo, ref, sha)
	return err
}

func (d *DB) Seen(repo, ref, sha string) (bool, error) {
	if sha == "" {
		return false, nil
	}
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM seen WHERE repo=? AND ref=? AND sha=?`, repo, ref, sha).Scan(&n)
	return n > 0, err
}

func (d *DB) MarkConflict(id int64, msg string) error {
	_, err := d.sql.Exec(`UPDATE jobs SET state=?, error=? WHERE id=?`, StateConflict, msg, id)
	return err
}

func (d *DB) MarkFailed(id int64, msg string) error {
	_, err := d.sql.Exec(`UPDATE jobs SET state=?, error=? WHERE id=?`, StateFailed, msg, id)
	return err
}

func remoteColumn(remote string) string {
	switch remote {
	case "forgejo":
		return "pushed_fj"
	case "gitlawb":
		return "pushed_gl"
	case "github":
		return "pushed_gh"
	default:
		return ""
	}
}

func (j *Job) Pushed(remote string) bool {
	switch remote {
	case "forgejo":
		return j.PushedFj
	case "gitlawb":
		return j.PushedGL
	case "github":
		return j.PushedGH
	default:
		return false
	}
}

func (d *DB) Block(repo, ref string, pr int, a, b string) error {
	_, err := d.sql.Exec(
		`INSERT INTO blocked (repo, ref, pr, a, b) VALUES (?,?,?,?,?)
		 ON CONFLICT(repo, ref) DO UPDATE SET pr=excluded.pr, a=excluded.a, b=excluded.b`,
		repo, ref, pr, a, b,
	)
	return err
}

func (d *DB) Unblock(repo, ref string) error {
	_, err := d.sql.Exec(`DELETE FROM blocked WHERE repo=? AND ref=?`, repo, ref)
	return err
}

func (d *DB) Blocked(repo, ref string) (*Block, error) {
	b := &Block{}
	err := d.sql.QueryRow(
		`SELECT repo, ref, pr, a, b FROM blocked WHERE repo=? AND ref=?`, repo, ref,
	).Scan(&b.Repo, &b.Ref, &b.PR, &b.A, &b.B)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}
