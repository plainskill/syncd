package fanout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"syncd/internal/config"
	"syncd/internal/forgejo"
	"syncd/internal/gitops"
	"syncd/internal/hook"
	"syncd/internal/journal"
)

type Git interface {
	Ensure(remotes map[string]string) error
	Fetch(remote, ref string) error
	SHA(remote, ref string) (string, error)
	IsAncestor(ancestor, descendant string) (bool, error)
	Merge(ours, theirs, msg string) (sha string, conflict bool, err error)
	Push(remote, ref, sha string) error
	LSRemote(remote, glob string) (map[string]string, error)
}

type PRs interface {
	OpenOrUpdate(ownerRepo, head, base, title, body string) (int, error)
}

type Engine struct {
	Cfg *config.Config
	J   *journal.DB
	Log *slog.Logger

	NewGit func(config.Repo) Git
	NewPR  func(config.Repo) PRs

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	wake  chan struct{}
}

func NewEngine(cfg *config.Config, j *journal.DB, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{
		Cfg:   cfg,
		J:     j,
		Log:   log,
		locks: map[string]*sync.Mutex{},
		wake:  make(chan struct{}, 1),
	}
	e.NewGit = func(r config.Repo) Git {
		safe := strings.ReplaceAll(r.Name, "/", "-")
		return gitops.New(
			filepath.Join(cfg.HubRoot, safe+".git"),
			filepath.Join(cfg.HubRoot, "worktrees", safe),
			cfg.Bot.Name,
			cfg.Bot.Email,
		)
	}
	e.NewPR = func(r config.Repo) PRs {
		return forgejo.New(r.ForgejoAPI, r.ForgejoToken)
	}
	return e
}

func (e *Engine) Enqueue(ev hook.Event) error {
	if e.Cfg.IsBot(ev.Pusher) {
		e.Log.Info("skip bot echo", "pusher", ev.Pusher, "repo", ev.Repo, "sha", ev.SHA)
		return nil
	}
	if _, ok := e.Cfg.Repo(ev.Repo); !ok {
		e.Log.Info("skip unknown repo", "repo", ev.Repo)
		return nil
	}
	_, already, err := e.J.Enqueue(ev.Repo, ev.Ref, ev.Source, ev.SHA)
	if err != nil {
		return err
	}
	if already {
		return nil
	}
	e.Wake()
	return nil
}

func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(e.Cfg.ReconcileEvery)
	defer tick.Stop()
	e.Wake()
	for {
		e.drain()
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		case <-tick.C:
			e.Reconcile()
		}
	}
}

func (e *Engine) drain() {
	for {
		j, err := e.J.NextIncomplete(e.Cfg.MaxAttempts)
		if err != nil {
			e.Log.Error("journal next", "err", err)
			return
		}
		if j == nil {
			return
		}
		if err := e.Apply(j); err != nil {
			e.Log.Error("apply", "err", err, "repo", j.Repo, "ref", j.Ref, "sha", j.SHA)
			_ = e.J.BumpAttempt(j.ID, err.Error())
			if j.Attempts+1 >= e.Cfg.MaxAttempts {
				_ = e.J.MarkFailed(j.ID, err.Error())
			}
			return
		}
	}
}

func (e *Engine) lockRef(repo, ref string) func() {
	key := repo + "\x00" + ref
	e.mu.Lock()
	l, ok := e.locks[key]
	if !ok {
		l = &sync.Mutex{}
		e.locks[key] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (e *Engine) Apply(j *journal.Job) error {
	unlock := e.lockRef(j.Repo, j.Ref)
	defer unlock()

	repo, ok := e.Cfg.Repo(j.Repo)
	if !ok {
		return e.J.MarkDone(j.ID)
	}
	g := e.NewGit(repo)
	if err := g.Ensure(map[string]string{
		"forgejo": repo.Forgejo,
		"github":  repo.GitHub,
		"gitlawb": repo.GitLawb,
	}); err != nil {
		return err
	}

	if err := e.fetch(g, repo, j.Source, j.Ref); err != nil {
		return err
	}
	if err := e.fetch(g, repo, "forgejo", j.Ref); err != nil {
		return err
	}
	_ = e.J.MarkFetched(j.ID)

	fj, _ := g.SHA("forgejo", j.Ref)
	in := j.SHA

	if blk, err := e.J.Blocked(j.Repo, j.Ref); err != nil {
		return err
	} else if blk != nil {
		resolved, err := e.blockedResolved(g, fj, blk)
		if err != nil {
			return err
		}
		if resolved {
			e.Log.Info("conflict pr merged, unblocking", "repo", j.Repo, "ref", j.Ref, "pr", blk.PR)
			if err := e.J.Unblock(j.Repo, j.Ref); err != nil {
				return err
			}
			return e.pushTarget(j, repo, g, fj, true)
		}
		if j.Source != "forgejo" {
			return e.J.MarkConflict(j.ID, fmt.Sprintf("blocked on pr %d", blk.PR))
		}
	}

	fjAncIn, err := g.IsAncestor(fj, in)
	if err != nil {
		return err
	}
	inAncFj, err := g.IsAncestor(in, fj)
	if err != nil {
		return err
	}
	dec := Decide(fj, in, fjAncIn, inAncFj)
	e.Log.Info("decide", "repo", j.Repo, "ref", j.Ref, "source", j.Source, "decision", int(dec), "fj", short(fj), "in", short(in))

	switch dec {
	case DecSkip:
		return e.J.MarkDone(j.ID)
	case DecFF:
		return e.pushTarget(j, repo, g, in, false)
	case DecFanout:
		return e.pushTarget(j, repo, g, fj, true)
	case DecMerge:
		if IsTag(j.Ref) {
			return e.J.MarkFailed(j.ID, "refusing to move tag")
		}
		sha, conflict, err := g.Merge(fj, in, MergeMsg(j.Source, in))
		if err != nil {
			return err
		}
		if !conflict {
			return e.pushTarget(j, repo, g, sha, true)
		}
		return e.openConflictPR(j, repo, g, fj, in)
	default:
		return fmt.Errorf("unknown decision %d", dec)
	}
}

func (e *Engine) blockedResolved(g Git, fj string, blk *journal.Block) (bool, error) {
	if fj == "" {
		return false, nil
	}
	a, err := g.IsAncestor(blk.A, fj)
	if err != nil {
		return false, err
	}
	b, err := g.IsAncestor(blk.B, fj)
	if err != nil {
		return false, err
	}
	return a && b, nil
}

func (e *Engine) openConflictPR(j *journal.Job, repo config.Repo, g Git, fj, in string) error {
	br := ConflictBranch(j.Source, in)
	headRef := "refs/heads/" + br
	if err := g.Push("forgejo", headRef, in); err != nil {
		return fmt.Errorf("push conflict branch: %w", err)
	}
	base := stringsTrimHeads(j.Ref)
	title := fmt.Sprintf("sync: %s %s conflicts with Forgejo", j.Source, ShortSHA(in))
	body := fmt.Sprintf(
		"Git object conflict on `%s`.\n\n- Forgejo: `%s`\n- %s: `%s`\n\nResolve **on Forgejo** with a merge commit (do not squash).\nAfter merge, syncd will fan the result out to GitLawb and GitHub.",
		j.Ref, fj, j.Source, in,
	)
	n, err := e.NewPR(repo).OpenOrUpdate(j.Repo, br, base, title, body)
	if err != nil {
		_ = e.J.Block(j.Repo, j.Ref, 0, fj, in)
		_ = e.J.MarkConflict(j.ID, err.Error())
		return err
	}
	if err := e.J.Block(j.Repo, j.Ref, n, fj, in); err != nil {
		return err
	}
	e.Log.Info("opened conflict pr", "repo", j.Repo, "pr", n, "head", br)
	return e.J.MarkConflict(j.ID, fmt.Sprintf("pr %d", n))
}

func (e *Engine) pushTarget(j *journal.Job, repo config.Repo, g Git, target string, includeSource bool) error {
	if target == "" {
		return e.J.MarkDone(j.ID)
	}
	for _, remote := range PushOrder(j.Source, includeSource) {
		if j.Pushed(remote) {
			continue
		}
		if repo.RemoteURL(remote) == "" {
			_ = e.J.MarkPushed(j.ID, remote)
			continue
		}
		if err := g.Push(remote, j.Ref, target); err != nil {
			return fmt.Errorf("push %s: %w", remote, err)
		}
		if err := e.J.MarkPushed(j.ID, remote); err != nil {
			return err
		}
		e.Log.Info("pushed", "repo", j.Repo, "ref", j.Ref, "remote", remote, "sha", short(target))
	}
	return e.J.MarkDone(j.ID)
}

func (e *Engine) fetch(g Git, repo config.Repo, remote, ref string) error {
	if remote == "" || repo.RemoteURL(remote) == "" {
		return nil
	}
	err := g.Fetch(remote, ref)
	if errors.Is(err, gitops.ErrMissing) {
		return nil
	}
	return err
}

func (e *Engine) Reconcile() {
	for _, repo := range e.Cfg.Repos {
		g := e.NewGit(repo)
		if err := g.Ensure(map[string]string{
			"forgejo": repo.Forgejo,
			"github":  repo.GitHub,
			"gitlawb": repo.GitLawb,
		}); err != nil {
			e.Log.Error("reconcile ensure", "repo", repo.Name, "err", err)
			continue
		}
		for _, remote := range []string{"github", "forgejo", "gitlawb"} {
			if repo.RemoteURL(remote) == "" {
				continue
			}
			refs, err := g.LSRemote(remote, "refs/heads/*")
			if err != nil {
				e.Log.Error("ls-remote", "repo", repo.Name, "remote", remote, "err", err)
				continue
			}
			for ref, sha := range refs {
				_, _, err := e.J.Enqueue(repo.Name, ref, remote, sha)
				if err != nil {
					e.Log.Error("reconcile enqueue", "err", err)
				}
			}
		}
	}
	e.Wake()
}

func stringsTrimHeads(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

func short(sha string) string { return ShortSHA(sha) }
