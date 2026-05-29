package subscription

import (
	"strings"
	"testing"
)

func TestFormatUserInfo(t *testing.T) {
	if got := FormatUserInfo(""); got != "" {
		t.Errorf("empty header = %q, want empty", got)
	}
	// 1 GiB total, an expiry timestamp.
	got := FormatUserInfo("upload=0; download=0; total=1073741824; expire=1751328000")
	if !strings.Contains(got, "GiB") {
		t.Errorf("got %q, want a GiB total", got)
	}
	if !strings.Contains(got, "expires") {
		t.Errorf("got %q, want an expiry", got)
	}
}

func TestLooksLikeClashConfig(t *testing.T) {
	if !looksLikeClashConfig([]byte("proxies:\n  - {}\n")) {
		t.Error("config with proxies: should be recognized")
	}
	if looksLikeClashConfig([]byte("dGhpcyBpcyBiYXNlNjQK")) {
		t.Error("base64 blob should not be recognized as a clash config")
	}
}
