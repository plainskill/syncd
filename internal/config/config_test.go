package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadExpandsEnvAndDefaults(t *testing.T) {
	t.Setenv("FORGEJO_TOKEN", "tok")
	t.Setenv("GITHUB_HOOK_SECRET", "ghsec")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
hub_root: ` + filepath.Join(dir, "hubs") + `
sqlite: ` + filepath.Join(dir, "syncd.db") + `
repos:
  - name: LibreLoom/LibreServ
    forgejo: ssh://git@example/LibreLoom/LibreServ.git
    forgejo_token: ${FORGEJO_TOKEN}
    secrets:
      github: ${GITHUB_HOOK_SECRET}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:7744" {
		t.Fatalf("listen: %s", cfg.Listen)
	}
	if cfg.ReconcileEvery != 5*time.Minute {
		t.Fatalf("reconcile: %s", cfg.ReconcileEvery)
	}
	if cfg.Repos[0].ForgejoToken != "tok" {
		t.Fatalf("token not expanded")
	}
	if cfg.Repos[0].Secrets.GitHub != "ghsec" {
		t.Fatalf("secret not expanded")
	}
	if cfg.Repos[0].DefaultRef() != "refs/heads/main" {
		t.Fatalf("default ref: %s", cfg.Repos[0].DefaultRef())
	}
}

func TestIsBot(t *testing.T) {
	c := &Config{Bot: Bot{Name: "syncd", GitHub: "libreserv-sync[bot]", Forgejo: "syncd"}}
	if !c.IsBot("syncd") || !c.IsBot("LibreServ-Sync[bot]") {
		t.Fatal("expected bot match")
	}
	if c.IsBot("plainskill") || c.IsBot("") {
		t.Fatal("unexpected bot match")
	}
}
