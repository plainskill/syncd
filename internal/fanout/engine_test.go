package fanout

import (
	"context"
	"path/filepath"
	"testing"

	"syncd/internal/config"
	"syncd/internal/hook"
	"syncd/internal/journal"
)

type fakeGit struct {
	sha      map[string]string
	pushes   []string
	deletes  []string
	conflict bool
	mergeSHA string
}

func (f *fakeGit) Ensure(context.Context, map[string]string) error { return nil }
func (f *fakeGit) Fetch(context.Context, string, string) error     { return nil }
func (f *fakeGit) SHA(_ context.Context, remote, _ string) (string, error) {
	return f.sha[remote], nil
}
func (f *fakeGit) IsAncestor(_ context.Context, a, b string) (bool, error) {
	if a == "" || b == "" {
		return false, nil
	}
	if a == b {
		return true, nil
	}
	if f.sha["anc:"+a+">"+b] == "1" {
		return true, nil
	}
	return false, nil
}
func (f *fakeGit) Merge(context.Context, string, string, string) (string, bool, error) {
	return f.mergeSHA, f.conflict, nil
}
func (f *fakeGit) Push(_ context.Context, remote, ref, sha string) error {
	f.pushes = append(f.pushes, remote+" "+ref+" "+sha)
	f.sha[remote] = sha
	return nil
}
func (f *fakeGit) LSRemote(context.Context, string, string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeGit) DeleteRef(_ context.Context, remote, ref string) error {
	f.deletes = append(f.deletes, remote+" "+ref)
	delete(f.sha, remote)
	return nil
}

func TestDeleteMirrorsBranch(t *testing.T) {
	g := &fakeGit{sha: map[string]string{"forgejo": "aaa", "gitlawb": "aaa"}}
	e, _ := testEngine(t, g, &fakePR{})
	e.Delete(context.Background(), hook.Event{Repo: "o/r", Ref: "refs/heads/feat", Source: "github", Before: "aaa", Delete: true})
	if len(g.deletes) != 2 || g.deletes[0] != "forgejo refs/heads/feat" || g.deletes[1] != "gitlawb refs/heads/feat" {
		t.Fatalf("deletes %v", g.deletes)
	}
}

func TestDeleteSkipsMovedRemote(t *testing.T) {
	g := &fakeGit{sha: map[string]string{"forgejo": "bbb", "gitlawb": "aaa"}}
	e, _ := testEngine(t, g, &fakePR{})
	e.Delete(context.Background(), hook.Event{Repo: "o/r", Ref: "refs/heads/feat", Source: "github", Before: "aaa", Delete: true})
	if len(g.deletes) != 1 || g.deletes[0] != "gitlawb refs/heads/feat" {
		t.Fatalf("deletes %v", g.deletes)
	}
}

func TestDeleteProtectsRefs(t *testing.T) {
	for _, ref := range []string{"refs/heads/main", "refs/heads/sync/github/abc1234", "refs/tags/v1"} {
		g := &fakeGit{sha: map[string]string{"forgejo": "aaa"}}
		e, _ := testEngine(t, g, &fakePR{})
		e.Delete(context.Background(), hook.Event{Repo: "o/r", Ref: ref, Source: "github", Before: "aaa", Delete: true})
		if len(g.deletes) != 0 {
			t.Fatalf("%s: deletes %v", ref, g.deletes)
		}
	}
}

type fakePR struct{ n int }

func (f *fakePR) OpenOrUpdate(string, string, string, string, string) (int, error) {
	if f.n == 0 {
		f.n = 7
	}
	return f.n, nil
}

func testEngine(t *testing.T, g *fakeGit, pr *fakePR) (*Engine, *journal.DB) {
	t.Helper()
	db, err := journal.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &config.Config{
		HubRoot:     t.TempDir(),
		MaxAttempts: 8,
		Bot:         config.Bot{Name: "syncd", GitHub: "sync-bot"},
		Repos: []config.Repo{{
			Name:          "o/r",
			DefaultBranch: "main",
			Forgejo:       "fj",
			GitHub:        "gh",
			GitLawb:       "gl",
		}},
	}
	e := NewEngine(cfg, db, nil)
	e.NewGit = func(config.Repo) Git { return g }
	e.NewPR = func(config.Repo) PRs { return pr }
	return e, db
}

func TestEnqueueSkipsBot(t *testing.T) {
	g := &fakeGit{sha: map[string]string{}}
	e, _ := testEngine(t, g, &fakePR{})
	if err := e.Enqueue(hook.Event{Repo: "o/r", Ref: "refs/heads/main", Source: "github", SHA: "aaa", Pusher: "sync-bot"}); err != nil {
		t.Fatal(err)
	}
	j, err := e.J.NextIncomplete(8)
	if err != nil || j != nil {
		t.Fatalf("expected no job, got %+v err=%v", j, err)
	}
}

func TestApplyFFGitHubToForgejoThenGitLawb(t *testing.T) {
	g := &fakeGit{sha: map[string]string{"forgejo": "aaa", "anc:aaa>bbb": "1"}}
	e, db := testEngine(t, g, &fakePR{})
	j, already, err := db.Enqueue("o/r", "refs/heads/main", "github", "bbb")
	if err != nil || already {
		t.Fatalf("enqueue %v %v", already, err)
	}
	if err := e.Apply(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if len(g.pushes) < 2 || g.pushes[0] != "forgejo refs/heads/main bbb" || g.pushes[1] != "gitlawb refs/heads/main bbb" {
		t.Fatalf("pushes %v", g.pushes)
	}
	got, _ := db.Get(j.ID)
	if got.State != journal.StateDone {
		t.Fatalf("state %s", got.State)
	}
}

func TestApplyConflictOpensPR(t *testing.T) {
	g := &fakeGit{sha: map[string]string{"forgejo": "aaa"}, conflict: true}
	pr := &fakePR{}
	e, db := testEngine(t, g, pr)
	j, _, err := db.Enqueue("o/r", "refs/heads/main", "github", "ccc")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Get(j.ID)
	if got.State != journal.StateConflict {
		t.Fatalf("state %s", got.State)
	}
	blk, err := db.Blocked("o/r", "refs/heads/main")
	if err != nil || blk == nil || blk.PR != 7 {
		t.Fatalf("block %+v err=%v", blk, err)
	}
	if len(g.pushes) != 1 || g.pushes[0] != "forgejo refs/heads/sync/github/ccc ccc" {
		t.Fatalf("pushes %v", g.pushes)
	}
}

func TestApplyMergeableDivergence(t *testing.T) {
	g := &fakeGit{sha: map[string]string{"forgejo": "aaa"}, mergeSHA: "mmm"}
	e, db := testEngine(t, g, &fakePR{})
	j, _, err := db.Enqueue("o/r", "refs/heads/main", "github", "ccc")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if g.pushes[0] != "forgejo refs/heads/main mmm" {
		t.Fatalf("first push should be merge onto fj: %v", g.pushes)
	}
	_, already, err := db.Enqueue("o/r", "refs/heads/main", "forgejo", "mmm")
	if err != nil || !already {
		t.Fatalf("forgejo echo of merge sha should be seen, already=%v err=%v", already, err)
	}
}
