package hook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMuxGitHubOKAndReject(t *testing.T) {
	var got Event
	body := []byte(`{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"o/r"}}`)
	mac := hmac.New(sha256.New, []byte("s"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	mux := Mux(func(ev Event) error { got = ev; return nil }, func(source, repo string) string {
		if source == "github" && repo == "o/r" {
			return "s"
		}
		return ""
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/hook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if got.SHA != "abc" || got.Source != "github" {
		t.Fatalf("%+v", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/hook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", rec.Code)
	}
}

func TestMuxHealth(t *testing.T) {
	mux := Mux(func(Event) error { return nil }, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestMuxRejectsEmptySecret(t *testing.T) {
	body := []byte(`{"source":"forgejo","repo":"o/r","ref":"refs/heads/main","after":"abc"}`)
	mux := Mux(func(Event) error { return nil }, func(string, string) string { return "" }, nil)
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/hook/github", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("github empty secret want 401 got %d", rec.Code)
	}
}
