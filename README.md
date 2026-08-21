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
| Own bot echo / already-done SHA | Drop. |
| Deleted ref | Ignore (no mirror deletes). |

A 5-minute `ls-remote` reconcile catches missed webhooks. SQLite + `fsync` is the crash log: the Forgejo `post-receive` hook returns as soon as the row is journaled.

## Run

```bash
cp config.example.yaml config.yaml   # edit remotes and secrets
go build -o syncd ./cmd/syncd
./syncd -config config.yaml
```

Hooks:

- GitHub `push` → `POST /hook/github` (`X-Hub-Signature-256`)
- GitLawb `push` → `POST /hook/gitlawb` (`X-Gitlawb-Signature-256`)
- Forgejo HTTP → `POST /hook/forgejo` (`X-Forgejo-Signature`)
- Forgejo same-host → `hooks/post-receive.sh` → `POST /hook`

`GET /healthz` for process checks. systemd unit: `deploy/syncd.service`.

Deploy next to Forgejo with **one** process. Empty `gitlawb` URL is fine until that remote exists; GitHub and GitLawb remotes are skipped when unset.

## Origin

On [cursor.com/codebase](https://cursor.com/codebase), **Sync from GitHub** the GitHub copy of the same repo. Origin’s GitHub mirror is the Origin adapter; syncd never talks to `origin.cursor.com`.
