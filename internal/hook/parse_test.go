package hook

import "testing"

func TestParseGitHub(t *testing.T) {
	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"ABCDEF",
		"pusher":{"name":"plainskill"},
		"sender":{"login":"plainskill"},
		"repository":{"full_name":"LibreLoom/LibreServ"}
	}`)
	ev, err := Parse("github", body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Repo != "LibreLoom/LibreServ" || ev.Ref != "refs/heads/main" || ev.SHA != "abcdef" {
		t.Fatalf("%+v", ev)
	}
	if ev.Pusher != "plainskill" || ev.Delete {
		t.Fatalf("pusher/delete %+v", ev)
	}
}

func TestParseDelete(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/x","after":"0000000000000000000000000000000000000000","repository":{"full_name":"o/r"}}`)
	ev, err := Parse("github", body)
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Delete {
		t.Fatal("expected delete")
	}
}

func TestParsePostReceive(t *testing.T) {
	body := []byte(`{"source":"forgejo","repo":"LibreLoom/LibreServ","ref":"refs/heads/main","after":"abc"}`)
	ev, err := Parse("", body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Source != "forgejo" || ev.Repo != "LibreLoom/LibreServ" || ev.SHA != "abc" {
		t.Fatalf("%+v", ev)
	}
}
