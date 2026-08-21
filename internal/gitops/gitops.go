package gitops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct {
	Dir      string
	Worktree string
	BotName  string
	BotEmail string
}

func New(dir, worktree, botName, botEmail string) *Repo {
	if botName == "" {
		botName = "syncd"
	}
	if botEmail == "" {
		botEmail = "syncd@localhost"
	}
	return &Repo{Dir: dir, Worktree: worktree, BotName: botName, BotEmail: botEmail}
}

func (r *Repo) Ensure(remotes map[string]string) error {
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
		out, err := r.git("", "remote", "get-url", name)
		if err != nil {
			if _, err := r.git("", "remote", "add", name, url); err != nil {
				return err
			}
			continue
		}
		if strings.TrimSpace(out) != url {
			if _, err := r.git("", "remote", "set-url", name, url); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repo) Fetch(remote, ref string) error {
	dst := tracking(remote, ref)
	_, err := r.git("", "fetch", "--no-tags", remote, "+"+ref+":"+dst)
	if err != nil && isMissingRef(err) {
		return ErrMissing
	}
	return err
}

func (r *Repo) SHA(remote, ref string) (string, error) {
	out, err := r.git("", "rev-parse", "--verify", "--quiet", tracking(remote, ref))
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) IsAncestor(ancestor, descendant string) (bool, error) {
	if ancestor == "" || descendant == "" {
		return false, nil
	}
	if ancestor == descendant {
		return true, nil
	}
	_, err := r.git("", "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

func (r *Repo) Merge(ours, theirs, msg string) (sha string, conflict bool, err error) {
	if err := os.MkdirAll(filepath.Dir(r.Worktree), 0o700); err != nil {
		return "", false, err
	}
	_, _ = r.git("", "worktree", "remove", "--force", r.Worktree)
	_ = os.RemoveAll(r.Worktree)
	if _, err := r.git("", "worktree", "add", "--detach", r.Worktree, ours); err != nil {
		return "", false, err
	}
	defer func() {
		_, _ = r.git("", "worktree", "remove", "--force", r.Worktree)
	}()
	_, err = r.git(r.Worktree,
		"-c", "commit.gpgsign=false",
		"-c", "user.name="+r.BotName,
		"-c", "user.email="+r.BotEmail,
		"merge", "--no-ff", "--no-edit", "-m", msg, theirs,
	)
	if err != nil {
		if mergeConflicted(r.Worktree) {
			_, _ = r.git(r.Worktree, "merge", "--abort")
			return "", true, nil
		}
		return "", false, err
	}
	out, err := r.git(r.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), false, nil
}

func (r *Repo) Push(remote, ref, sha string) error {
	if remote == "" || ref == "" || sha == "" {
		return fmt.Errorf("push: missing remote/ref/sha")
	}
	_, err := r.git("", "push", remote, sha+":"+ref)
	return err
}

func (r *Repo) LSRemote(remote, glob string) (map[string]string, error) {
	if glob == "" {
		glob = "refs/heads/*"
	}
	out, err := r.git("", "ls-remote", "--refs", remote, glob)
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
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func (r *Repo) git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
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
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
		return stdout.String(), fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return stdout.String(), nil
}

func filteredGitEnv() []string {
	out := make([]string, 0, 16)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") {
			continue
		}
		out = append(out, e)
	}
	return out
}
