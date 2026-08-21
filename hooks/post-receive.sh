#!/bin/sh
# Install as hooks/post-receive in the Forgejo bare repo.
# Returns as soon as syncd has journaled the update; it does not wait on GitHub.
set -eu

SYNCD_URL="${SYNCD_URL:-http://127.0.0.1:7744/hook}"
SYNCD_REPO="${SYNCD_REPO:?SYNCD_REPO must be owner/name, e.g. LibreLoom/LibreServ}"
SYNCD_SECRET="${FJ_HOOK_SECRET:?FJ_HOOK_SECRET must be set; /hook rejects empty secrets}"
PUSHER="${GITEA_PUSHER_NAME:-${FORGEJO_PUSHER_NAME:-}}"

json_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

while read -r old new ref; do
	[ -n "$ref" ] || continue
	body=$(printf '{"source":"forgejo","repo":"%s","ref":"%s","after":"%s","before":"%s","pusher":"%s"}' \
		"$(json_escape "$SYNCD_REPO")" \
		"$(json_escape "$ref")" \
		"$(json_escape "$new")" \
		"$(json_escape "$old")" \
		"$(json_escape "$PUSHER")")
	curl -fsS -X POST \
		-H "Content-Type: application/json" \
		-H "X-Syncd-Secret: ${SYNCD_SECRET}" \
		--data-binary "$body" \
		"$SYNCD_URL" >/dev/null
done
