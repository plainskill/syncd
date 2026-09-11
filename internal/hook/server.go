package hook

import (
	"io"
	"log/slog"
	"net/http"
)

type Enqueuer func(Event) error

func Mux(enqueue Enqueuer, secretFor func(source, repo string) string, log *slog.Logger) *http.ServeMux {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("POST /hook", handle("", "X-Syncd-Secret", nil, enqueue, secretFor, log))
	mux.Handle("POST /hook/github", handle("github", "X-Hub-Signature-256", GitHubSignatureOK, enqueue, secretFor, log))
	mux.Handle("POST /hook/forgejo", handle("forgejo", "X-Forgejo-Signature", ForgejoSignatureOK, enqueue, secretFor, log))
	mux.Handle("POST /hook/gitlawb", handle("gitlawb", "X-Gitlawb-Signature-256", GitLawbSignatureOK, enqueue, secretFor, log))
	return mux
}

func handle(source, header string, verify func(string, []byte, string) bool, enqueue Enqueuer, secretFor func(source, repo string) string, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		ev, err := Parse(source, body)
		if err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if source != "" {
			ev.Source = source
		}
		sec := ""
		if secretFor != nil {
			sec = secretFor(ev.Source, ev.Repo)
		}
		if header == "X-Syncd-Secret" {
			if !SyncdSecretOK(sec, r.Header.Get("X-Syncd-Secret")) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if verify != nil {
			if sec == "" || !verify(sec, body, r.Header.Get(header)) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if ev.Delete || ev.SHA == "" || ev.SHA == ZeroSHA {
			ev.Delete = true
			ev.SHA = ""
		}
		if ev.Repo == "" || ev.Ref == "" {
			http.Error(w, "missing repo/ref", http.StatusBadRequest)
			return
		}
		if err := enqueue(ev); err != nil {
			log.Error("enqueue", "err", err, "repo", ev.Repo, "ref", ev.Ref)
			http.Error(w, "enqueue failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
