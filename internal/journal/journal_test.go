package journal

import (
	"path/filepath"
	"testing"
)

func TestEnqueueIdempotentAndDoneSkip(t *testing.T) {
	db := openT(t)
	j, already, err := db.Enqueue("o/r", "refs/heads/main", "github", "abc")
	if err != nil || already || j == nil {
		t.Fatalf("first: job=%v already=%v err=%v", j, already, err)
	}
	_, already, err = db.Enqueue("o/r", "refs/heads/main", "github", "abc")
	if err != nil || !already {
		t.Fatalf("dup: already=%v err=%v", already, err)
	}
	if err := db.MarkDone(j.ID); err != nil {
		t.Fatal(err)
	}
	_, already, err = db.Enqueue("o/r", "refs/heads/main", "forgejo", "abc")
	if err != nil || !already {
		t.Fatalf("echo from other source should skip, already=%v err=%v", already, err)
	}
}

func TestNextIncompleteOrderAndAttempts(t *testing.T) {
	db := openT(t)
	a, _, _ := db.Enqueue("o/r", "refs/heads/main", "github", "aaa")
	b, _, _ := db.Enqueue("o/r", "refs/heads/other", "github", "bbb")
	got, err := db.NextIncomplete(8)
	if err != nil || got.ID != a.ID {
		t.Fatalf("want %d got %+v err=%v", a.ID, got, err)
	}
	if err := db.MarkDone(a.ID); err != nil {
		t.Fatal(err)
	}
	got, err = db.NextIncomplete(8)
	if err != nil || got.ID != b.ID {
		t.Fatalf("want %d got %+v err=%v", b.ID, got, err)
	}
}

func TestRememberSkipsFanoutTarget(t *testing.T) {
	db := openT(t)
	if err := db.Remember("o/r", "refs/heads/main", "mmm"); err != nil {
		t.Fatal(err)
	}
	_, already, err := db.Enqueue("o/r", "refs/heads/main", "forgejo", "mmm")
	if err != nil || !already {
		t.Fatalf("seen target sha should skip, already=%v err=%v", already, err)
	}
}

func TestBlockRoundTrip(t *testing.T) {
	db := openT(t)
	if err := db.Block("o/r", "refs/heads/main", 12, "aaa", "bbb"); err != nil {
		t.Fatal(err)
	}
	b, err := db.Blocked("o/r", "refs/heads/main")
	if err != nil || b == nil || b.PR != 12 || b.A != "aaa" || b.B != "bbb" {
		t.Fatalf("got %+v err=%v", b, err)
	}
	if err := db.Unblock("o/r", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	b, err = db.Blocked("o/r", "refs/heads/main")
	if err != nil || b != nil {
		t.Fatalf("unblocked: %+v err=%v", b, err)
	}
}

func openT(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
