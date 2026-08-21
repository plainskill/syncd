package hook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func GitHubSignatureOK(secret string, body []byte, header string) bool {
	return hmacHeaderOK(secret, body, header, "sha256=")
}

func GitLawbSignatureOK(secret string, body []byte, header string) bool {
	if hmacHeaderOK(secret, body, header, "sha256=") {
		return true
	}
	return hmacHexOK(secret, body, header)
}

func ForgejoSignatureOK(secret string, body []byte, header string) bool {
	if header == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(header), "sha256=") {
		return hmacHeaderOK(secret, body, header, "sha256=")
	}
	return hmacHexOK(secret, body, header)
}

func SyncdSecretOK(secret, got string) bool {
	if secret == "" || got == "" {
		return false
	}
	return hmac.Equal([]byte(secret), []byte(got))
}

func hmacHeaderOK(secret string, body []byte, header, prefix string) bool {
	if secret == "" || header == "" {
		return false
	}
	h := strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(h), strings.ToLower(prefix)) {
		return false
	}
	return hmacHexOK(secret, body, h[len(prefix):])
}

func hmacHexOK(secret string, body []byte, gotHex string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	got := strings.TrimSpace(strings.ToLower(gotHex))
	return hmac.Equal([]byte(want), []byte(got))
}
