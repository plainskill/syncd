package hook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestGitHubSignature(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	hdr := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !GitHubSignatureOK("secret", body, hdr) {
		t.Fatal("expected ok")
	}
	if GitHubSignatureOK("secret", body, "sha256=deadbeef") {
		t.Fatal("expected fail")
	}
	if GitHubSignatureOK("secret", body, "") {
		t.Fatal("empty header")
	}
}

func TestForgejoAndGitLawbHex(t *testing.T) {
	body := []byte("payload")
	mac := hmac.New(sha256.New, []byte("s"))
	mac.Write(body)
	hexSig := hex.EncodeToString(mac.Sum(nil))
	if !ForgejoSignatureOK("s", body, hexSig) {
		t.Fatal("forgejo hex")
	}
	if !GitLawbSignatureOK("s", body, "sha256="+hexSig) {
		t.Fatal("gitlawb prefixed")
	}
}

func TestSyncdSecret(t *testing.T) {
	if SyncdSecretOK("", "anything") || SyncdSecretOK("abc", "") {
		t.Fatal("empty secret must fail closed")
	}
	if !SyncdSecretOK("abc", "abc") || SyncdSecretOK("abc", "nope") {
		t.Fatal("shared secret")
	}
}
