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

func TestParseForgejoDeleteEvent(t *testing.T) {
	body := []byte(`{"ref":"feat/x","ref_type":"branch","pusher_type":"user","repository":{"full_name":"o/r"},"sender":{"login":"max"}}`)
	ev, err := Parse("forgejo", body)
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Delete || ev.Ref != "refs/heads/feat/x" || ev.Repo != "o/r" || ev.Pusher != "max" {
		t.Fatalf("%+v", ev)
	}
}

func TestParsePostReceive(t *testing.T) {
	body := []byte(`{"source":"forgejo","repo":"LibreLoom/LibreServ","ref":"refs/heads/main","after":"abc","pusher":"syncd"}`)
	ev, err := Parse("", body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Source != "forgejo" || ev.Repo != "LibreLoom/LibreServ" || ev.SHA != "abc" || ev.Pusher != "syncd" {
		t.Fatalf("%+v", ev)
	}
}
