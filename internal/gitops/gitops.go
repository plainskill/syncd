package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const DefaultTimeout = 60 * time.Second

type Repo struct {
	Dir      string
	Worktree string
	BotName  string
	BotEmail string
	Timeout  time.Duration
}

func New(dir, worktree, botName, botEmail string) *Repo {
	if botName == "" {
		botName = "syncd"
	}
	if botEmail == "" {
		botEmail = "syncd@localhost"
	}
	return &Repo{Dir: dir, Worktree: worktree, BotName: botName, BotEmail: botEmail, Timeout: DefaultTimeout}
}

func (r *Repo) timeout() time.Duration {
	if r.Timeout <= 0 {
		return DefaultTimeout
	}
	return r.Timeout
}

func (r *Repo) Ensure(ctx context.Context, remotes map[string]string) error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "HEAD")); err != nil {
		cmd := exec.Command("git", "init", "--bare", r.Dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git init --bare: %s: %w", bytes.TrimSpace(out), err)
		}
	}
	for name, url := range remotes {
		if url == "" {
			continue
		}
		out, err := r.git(ctx, "", "remote", "get-url", name)
		if err != nil {
			if _, err := r.git(ctx, "", "remote", "add", name, url); err != nil {
				return err
			}
			continue
		}
		if strings.TrimSpace(out) != url {
			if _, err := r.git(ctx, "", "remote", "set-url", name, url); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repo) Fetch(ctx context.Context, remote, ref string) error {
	dst := tracking(remote, ref)
	_, err := r.git(ctx, "", "fetch", "--no-tags", remote, "+"+ref+":"+dst)
	if err != nil && isMissingRef(err) {
		return ErrMissing
	}
	return err
}

func (r *Repo) SHA(ctx context.Context, remote, ref string) (string, error) {
	out, err := r.git(ctx, "", "rev-parse", "--verify", "--quiet", tracking(remote, ref))
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	if ancestor == "" || descendant == "" {
		return false, nil
	}
	if ancestor == descendant {
		return true, nil
	}
	_, err := r.git(ctx, "", "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

func (r *Repo) Merge(ctx context.Context, ours, theirs, msg string) (sha string, conflict bool, err error) {
	if err := os.MkdirAll(filepath.Dir(r.Worktree), 0o700); err != nil {
		return "", false, err
	}
	_, _ = r.git(ctx, "", "worktree", "remove", "--force", r.Worktree)
	_ = os.RemoveAll(r.Worktree)
	if _, err := r.git(ctx, "", "worktree", "add", "--detach", r.Worktree, ours); err != nil {
		return "", false, err
	}
	defer func() {
		_, _ = r.git(ctx, "", "worktree", "remove", "--force", r.Worktree)
	}()
	_, err = r.git(ctx, r.Worktree,
		"-c", "commit.gpgsign=false",
		"-c", "user.name="+r.BotName,
		"-c", "user.email="+r.BotEmail,
		"merge", "--no-ff", "--no-edit", "-m", msg, theirs,
	)
	if err != nil {
		if mergeConflicted(r.Worktree) {
			_, _ = r.git(ctx, r.Worktree, "merge", "--abort")
			return "", true, nil
		}
		return "", false, err
	}
	out, err := r.git(ctx, r.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), false, nil
}

func (r *Repo) Push(ctx context.Context, remote, ref, sha string) error {
	if remote == "" || ref == "" || sha == "" {
		return fmt.Errorf("push: missing remote/ref/sha")
	}
	_, err := r.git(ctx, "", "push", remote, sha+":"+ref)
	return err
}

func (r *Repo) DeleteRef(ctx context.Context, remote, ref string) error {
	if remote == "" || ref == "" {
		return fmt.Errorf("delete: missing remote/ref")
	}
	_, err := r.git(ctx, "", "push", remote, ":"+ref)
	if err != nil && !isMissingRef(err) {
		return err
	}
	_, _ = r.git(ctx, "", "update-ref", "-d", tracking(remote, ref))
	return nil
}

func (r *Repo) LSRemote(ctx context.Context, remote, glob string) (map[string]string, error) {
	if glob == "" {
		glob = "refs/heads/*"
	}
	out, err := r.git(ctx, "", "ls-remote", "--refs", remote, glob)
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			sha, ref, ok = strings.Cut(line, " ")
		}
		if !ok {
			continue
		}
		refs[strings.TrimSpace(ref)] = strings.TrimSpace(sha)
	}
	return refs, nil
}

func tracking(remote, ref string) string {
	ref = strings.TrimPrefix(ref, "refs/")
	return "refs/remotes/" + remote + "/" + ref
}

func mergeConflicted(wt string) bool {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = wt
	out, err := cmd.Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func isMissingRef(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "couldn't find remote ref") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "couldn't find remote")
}

var ErrMissing = fmt.Errorf("missing ref")

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (r *Repo) git(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	cmd := exec.Command("git", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	env := filteredGitEnv()
	if dir != "" {
		cmd.Dir = dir
		cmd.Env = env
	} else {
		cmd.Env = append(env, "GIT_DIR="+r.Dir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), ctx.Err())
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
			return stdout.String(), fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
		}
		return stdout.String(), nil
	}
}

func filteredGitEnv() []string {
	out := make([]string, 0, 16)
	hasSSH := false
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") {
			continue
		}
		if strings.HasPrefix(e, "GIT_SSH_COMMAND=") {
			hasSSH = true
		}
		out = append(out, e)
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	if !hasSSH {
		out = append(out, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=5 -o ServerAliveCountMax=3")
	}
	return out
}
