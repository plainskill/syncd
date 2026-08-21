package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFFMergeAndConflict(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	aWT := filepath.Join(root, "a")
	bWT := filepath.Join(root, "b")
	initRepo(t, aWT)
	writeCommit(t, aWT, "readme", "hello\n", "base")
	run(t, aWT, "git", "clone", "--bare", aWT, filepath.Join(root, "a.git"))
	run(t, aWT, "git", "clone", "--bare", aWT, filepath.Join(root, "b.git"))
	run(t, root, "git", "clone", filepath.Join(root, "a.git"), bWT)

	hub := New(filepath.Join(root, "hub.git"), filepath.Join(root, "wt"), "syncd", "syncd@localhost")
	if err := hub.Ensure(ctx, map[string]string{
		"github":  filepath.Join(root, "a.git"),
		"forgejo": filepath.Join(root, "b.git"),
	}); err != nil {
		t.Fatal(err)
	}

	// FF: extra commit on github (a)
	writeCommit(t, aWT, "readme", "hello\nff\n", "ff")
	run(t, aWT, "git", "push", filepath.Join(root, "a.git"), "HEAD:refs/heads/main")
	if err := hub.Fetch(ctx, "github", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Fetch(ctx, "forgejo", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	gh, _ := hub.SHA(ctx, "github", "refs/heads/main")
	fj, _ := hub.SHA(ctx, "forgejo", "refs/heads/main")
	anc, err := hub.IsAncestor(ctx, fj, gh)
	if err != nil || !anc {
		t.Fatalf("expected ff ancestry anc=%v err=%v", anc, err)
	}
	if err := hub.Push(ctx, "forgejo", "refs/heads/main", gh); err != nil {
		t.Fatal(err)
	}
	run(t, bWT, "git", "pull", "--ff-only")

	// diverged, mergeable: different files
	writeCommit(t, aWT, "only-a", "A\n", "ca")
	run(t, aWT, "git", "push", filepath.Join(root, "a.git"), "HEAD:refs/heads/main")
	writeCommit(t, bWT, "only-b", "B\n", "cb")
	run(t, bWT, "git", "push", filepath.Join(root, "b.git"), "HEAD:refs/heads/main")
	if err := hub.Fetch(ctx, "github", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Fetch(ctx, "forgejo", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	gh, _ = hub.SHA(ctx, "github", "refs/heads/main")
	fj, _ = hub.SHA(ctx, "forgejo", "refs/heads/main")
	sha, conflict, err := hub.Merge(ctx, fj, gh, "sync: merge github into main")
	if err != nil || conflict || sha == "" {
		t.Fatalf("mergeable: sha=%s conflict=%v err=%v", sha, conflict, err)
	}

	// content conflict
	root2 := t.TempDir()
	x := filepath.Join(root2, "x")
	initRepo(t, x)
	writeCommit(t, x, "f", "base\n", "base")
	run(t, x, "git", "clone", "--bare", x, filepath.Join(root2, "x.git"))
	run(t, x, "git", "clone", "--bare", x, filepath.Join(root2, "y.git"))
	y := filepath.Join(root2, "y")
	run(t, root2, "git", "clone", filepath.Join(root2, "y.git"), y)
	writeCommit(t, x, "f", "left\n", "left")
	run(t, x, "git", "push", filepath.Join(root2, "x.git"), "HEAD:refs/heads/main")
	writeCommit(t, y, "f", "right\n", "right")
	run(t, y, "git", "push", filepath.Join(root2, "y.git"), "HEAD:refs/heads/main")
	hub2 := New(filepath.Join(root2, "hub.git"), filepath.Join(root2, "wt"), "syncd", "syncd@localhost")
	if err := hub2.Ensure(ctx, map[string]string{
		"github":  filepath.Join(root2, "x.git"),
		"forgejo": filepath.Join(root2, "y.git"),
	}); err != nil {
		t.Fatal(err)
	}
	_ = hub2.Fetch(ctx, "github", "refs/heads/main")
	_ = hub2.Fetch(ctx, "forgejo", "refs/heads/main")
	gh, _ = hub2.SHA(ctx, "github", "refs/heads/main")
	fj, _ = hub2.SHA(ctx, "forgejo", "refs/heads/main")
	_, conflict, err = hub2.Merge(ctx, fj, gh, "sync")
	if err != nil {
		t.Fatal(err)
	}
	if !conflict {
		t.Fatal("expected content conflict")
	}
}

func TestGitOpTimeout(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, filepath.Join(dir, "src"))
	writeCommit(t, filepath.Join(dir, "src"), "f", "x\n", "c")
	hub := New(filepath.Join(dir, "hub.git"), filepath.Join(dir, "wt"), "syncd", "syncd@localhost")
	hub.Timeout = 200 * time.Millisecond
	if err := hub.Ensure(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	_, err := hub.git(context.Background(), "", "-c", "alias.hang=!sleep 5", "hang")
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("want timeout, got %v", err)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.name", "t")
	run(t, dir, "git", "config", "user.email", "t@t")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
}

func writeCommit(t *testing.T, dir, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", file)
	run(t, dir, "git", "-c", "commit.gpgsign=false", "commit", "-m", msg)
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %s\n%s", name, strings.Join(args, " "), err, out)
	}
}
