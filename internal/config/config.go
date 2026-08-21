package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen         string        `yaml:"listen"`
	HubRoot        string        `yaml:"hub_root"`
	SQLite         string        `yaml:"sqlite"`
	ReconcileEvery time.Duration `yaml:"reconcile_every"`
	MaxAttempts    int           `yaml:"max_attempts"`
	Bot            Bot           `yaml:"bot"`
	Repos          []Repo        `yaml:"repos"`
}

type Bot struct {
	Name    string `yaml:"name"`
	Email   string `yaml:"email"`
	GitHub  string `yaml:"github"`
	Forgejo string `yaml:"forgejo"`
	GitLawb string `yaml:"gitlawb"`
}

type Repo struct {
	Name          string  `yaml:"name"`
	DefaultBranch string  `yaml:"default_branch"`
	Forgejo       string  `yaml:"forgejo"`
	GitHub        string  `yaml:"github"`
	GitLawb       string  `yaml:"gitlawb"`
	ForgejoAPI    string  `yaml:"forgejo_api"`
	ForgejoToken  string  `yaml:"forgejo_token"`
	Secrets       Secrets `yaml:"secrets"`
}

type Secrets struct {
	GitHub  string `yaml:"github"`
	Forgejo string `yaml:"forgejo"`
	GitLawb string `yaml:"gitlawb"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded := os.ExpandEnv(string(raw))
	var c Config
	if err := yaml.Unmarshal([]byte(expanded), &c); err != nil {
		return nil, err
	}
	c.setDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) setDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:7744"
	}
	if c.ReconcileEvery == 0 {
		c.ReconcileEvery = 5 * time.Minute
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 8
	}
	if c.Bot.Name == "" {
		c.Bot.Name = "syncd"
	}
	if c.Bot.Email == "" {
		c.Bot.Email = "syncd@localhost"
	}
	if c.Bot.Forgejo == "" {
		c.Bot.Forgejo = c.Bot.Name
	}
	for i := range c.Repos {
		if c.Repos[i].DefaultBranch == "" {
			c.Repos[i].DefaultBranch = "main"
		}
	}
}

func (c *Config) validate() error {
	if c.HubRoot == "" {
		return fmt.Errorf("hub_root is required")
	}
	if c.SQLite == "" {
		return fmt.Errorf("sqlite is required")
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("at least one repo is required")
	}
	seen := map[string]struct{}{}
	for _, r := range c.Repos {
		if r.Name == "" || !strings.Contains(r.Name, "/") {
			return fmt.Errorf("repo name %q must be owner/name", r.Name)
		}
		if _, ok := seen[r.Name]; ok {
			return fmt.Errorf("duplicate repo %q", r.Name)
		}
		seen[r.Name] = struct{}{}
		if r.Forgejo == "" {
			return fmt.Errorf("repo %s: forgejo remote is required", r.Name)
		}
	}
	return nil
}

func (c *Config) Repo(name string) (Repo, bool) {
	for _, r := range c.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

func (c *Config) IsBot(pusher string) bool {
	p := strings.TrimSpace(strings.ToLower(pusher))
	if p == "" {
		return false
	}
	for _, id := range []string{c.Bot.Name, c.Bot.GitHub, c.Bot.Forgejo, c.Bot.GitLawb} {
		if id != "" && strings.ToLower(id) == p {
			return true
		}
	}
	return false
}

func (r Repo) DefaultRef() string {
	b := r.DefaultBranch
	if strings.HasPrefix(b, "refs/") {
		return b
	}
	return "refs/heads/" + b
}

func (r Repo) RemoteURL(name string) string {
	switch name {
	case "forgejo":
		return r.Forgejo
	case "github":
		return r.GitHub
	case "gitlawb":
		return r.GitLawb
	default:
		return ""
	}
}
