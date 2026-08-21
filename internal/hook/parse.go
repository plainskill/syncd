package hook

import (
	"encoding/json"
	"strings"
)

const ZeroSHA = "0000000000000000000000000000000000000000"

type Event struct {
	Repo   string
	Ref    string
	Source string
	SHA    string
	Pusher string
	Delete bool
}

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Before     string `json:"before"`
	Deleted    bool   `json:"deleted"`
	Source     string `json:"source"`
	Repo       string `json:"repo"`
	Pusher     any    `json:"pusher"`
	Sender     any    `json:"sender"`
	Actor      any    `json:"actor"`
	Repository struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		Owner    struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
}

func Parse(source string, body []byte) (Event, error) {
	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, err
	}
	ev := Event{
		Source: source,
		Ref:    p.Ref,
		SHA:    strings.ToLower(strings.TrimSpace(p.After)),
		Delete: p.Deleted || isZero(p.After),
		Pusher: firstNonEmpty(
			actorLogin(p.Pusher),
			actorLogin(p.Sender),
			actorLogin(p.Actor),
		),
	}
	if p.Source != "" && ev.Source == "" {
		ev.Source = p.Source
	}
	ev.Repo = p.Repo
	if ev.Repo == "" {
		ev.Repo = p.Repository.FullName
	}
	if ev.Repo == "" && p.Repository.Owner.Login != "" && p.Repository.Name != "" {
		ev.Repo = p.Repository.Owner.Login + "/" + p.Repository.Name
	}
	if ev.Source == "" {
		ev.Source = source
	}
	return ev, nil
}

func isZero(sha string) bool {
	s := strings.TrimSpace(strings.ToLower(sha))
	return s == "" || s == ZeroSHA
}

func actorLogin(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case map[string]any:
		for _, k := range []string{"login", "username", "name", "did"} {
			if s, ok := t[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
