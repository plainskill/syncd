# syncd

Webhook-driven git object hub for **Forgejo**, **GitHub**, and **GitLawb**. Cursor Origin is not a peer — it follows GitHub via Origin’s native GitHub sync.

Pushes are journaled, then applied **one ref at a time**. GitHub → Forgejo → GitLawb in the common case. Forgejo is canonical for issues and conflict PRs.

## Behavior

| Situation | Action |
|---|---|
| Fast-forward | Push immediately. GitHub sources hit Forgejo first, then GitLawb. |
| Same commit already on Forgejo | Fill remotes that lack it. |
| Diverged, clean merge | Merge commit (no rebase, no squash), land on Forgejo, fan out. |
| Content conflict | Push `sync/<source>/<sha>` to Forgejo and open a PR. Do not move `main`. After that PR is **merge-committed**, fan out the result. |
| Own bot echo / already-seen SHA | Drop. Fan-out target SHAs are recorded so Forgejo post-receive of a merge commit does not re-push. |
| Deleted branch | Delete on the other remotes, but only where their tip still equals the deleted SHA. Never the default branch, `sync/*`, or tags. Webhook-driven only (reconcile does not delete). |

A 5-minute `ls-remote` reconcile catches missed webhooks (same enqueue path as HTTP). SQLite + `fsync` is the crash log: the Forgejo `post-receive` hook returns as soon as the row is journaled. Git fetch/push/ls-remote have a 60s deadline; a hung remote fails that job and the queue continues.

Hooks require a shared secret. Empty `FJ_HOOK_SECRET` / GitHub / GitLawb secrets are 401, including on localhost. `post-receive.sh` sends `GITEA_PUSHER_NAME` so bot echo can match. Only one process may run: syncd exclusive-flocks `$hub_root/syncd.lock`.

`GET /healthz` for process checks. systemd unit: `deploy/syncd.service` (`UMask=0077`, config file 0600).

## Run

```bash
cp config.example.yaml config.yaml   # edit remotes and secrets
go build -o syncd ./cmd/syncd
./syncd -config config.yaml
```

Hooks:

- GitHub `push` → `POST /hook/github` (`X-Hub-Signature-256`)
- GitLawb `push` → `POST /hook/gitlawb` (`X-Gitlawb-Signature-256`)
- Forgejo HTTP webhook → `POST /hook/forgejo` (`X-Forgejo-Signature`)
- Forgejo same-host (bare-metal) → `hooks/post-receive.sh` → `POST /hook`

Containerized Forgejo: the filesystem `post-receive.sh` path does NOT work —
Forgejo chains its own internal hook and the container usually lacks `curl`
(the script silently exits 127). Registers a Forgejo **push webhook** instead
(`POST /hook/forgejo`, secret = `FJ_HOOK_SECRET`); that is the working path.

Deploy next to Forgejo. Empty `gitlawb` URL is fine until that remote exists; GitHub and GitLawb remotes are skipped when unset.

## Origin

On [cursor.com/codebase](https://cursor.com/codebase), **Sync from GitHub** the GitHub copy of the same repo. Origin’s GitHub mirror is the Origin adapter; syncd never talks to `origin.cursor.com`.
