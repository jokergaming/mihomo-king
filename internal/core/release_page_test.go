package core

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestSingBoxLargeReleasePage(t *testing.T) {
	notes := strings.Repeat("x", 300<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "[")
		for range 29 {
			fmt.Fprintf(w, `{"tag_name":"v1.15.0-alpha.13","prerelease":true,"body":"%s"},`, notes)
		}
		io.WriteString(w, `{"tag_name":"v1.14.4","assets":[{"name":"sing-box-1.14.4-linux-amd64.tar.gz","browser_download_url":"https://example.test/stable"}]}]`)
	}))
	defer srv.Close()
	spec, err := SpecFor("sing-box")
	if err != nil {
		t.Fatal(err)
	}
	spec.Endpoint = srv.URL
	rel, err := Latest(t.Context(), spec, ChannelRelease, "linux", "amd64")
	if err != nil || rel.Tag != "v1.14.4" {
		t.Fatalf("stable release at end of large page: %+v, %v", rel, err)
	}
}

func TestDecodeReleasePageRejectsIncompleteAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`[{"tag_name":"v1"}`, `[] {}`, `{}`} {
		if _, err := decodeReleasePage(strings.NewReader(body)); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}
