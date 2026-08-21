package forgejo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func New(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

type pr struct {
	Number int    `json:"number"`
	State  string `json:"state"`
}

func (c *Client) OpenOrUpdate(ownerRepo, head, base, title, body string) (int, error) {
	if c.Token == "" || c.Base == "" {
		return 0, fmt.Errorf("forgejo api token/base not configured")
	}
	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok {
		return 0, fmt.Errorf("bad repo %q", ownerRepo)
	}
	existing, err := c.findOpen(owner, repo, head)
	if err != nil {
		return 0, err
	}
	if existing != 0 {
		return existing, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"title": title,
		"head":  head,
		"base":  strings.TrimPrefix(base, "refs/heads/"),
		"body":  body,
	})
	req, err := http.NewRequest(http.MethodPost, c.Base+"/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/pulls", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return 0, fmt.Errorf("create pr: %s: %s", res.Status, b)
	}
	var created pr
	if err := json.Unmarshal(b, &created); err != nil {
		return 0, err
	}
	return created.Number, nil
}

func (c *Client) findOpen(owner, repo, head string) (int, error) {
	q := url.Values{"state": {"open"}, "limit": {"50"}}
	req, err := http.NewRequest(http.MethodGet, c.Base+"/api/v1/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/pulls?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "token "+c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return 0, fmt.Errorf("list prs: %s: %s", res.Status, b)
	}
	var prs []struct {
		Number int `json:"number"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(b, &prs); err != nil {
		return 0, err
	}
	head = strings.TrimPrefix(head, "refs/heads/")
	for _, p := range prs {
		if p.State == "open" && strings.TrimPrefix(p.Head.Ref, "refs/heads/") == head {
			return p.Number, nil
		}
	}
	return 0, nil
}
