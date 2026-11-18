package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckUpdate(t *testing.T) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	for _, tc := range []struct {
		tool, line, tag, asset, want string
		alpha                        bool
	}{
		{"mihomo", "Mihomo Meta v1.9.9 linux amd64", "v1.10.0", "mihomo-" + platform + "-v1.10.0.gz", "v1.10.0", false},
		{"mihomo", "Mihomo Meta v1.10.1 linux amd64", "v1.10.0", "mihomo-" + platform + "-v1.10.0.gz", "", false},
		{"mihomo", "Mihomo Meta alpha-g91e85ee linux amd64", "Prerelease-Alpha", "mihomo-" + platform + "-alpha-91e85ee.gz", "", true},
		{"mihomo", "Mihomo Meta alpha-123abcd linux amd64", "Prerelease-Alpha", "mihomo-" + platform + "-alpha-91e85ee.gz", "alpha-91e85ee", true},
		{"sing-box", "sing-box version 1.15.0-alpha.9", "v1.15.0-alpha.13", "sing-box-1.15.0-alpha.13-" + platform + ".tar.gz", "v1.15.0-alpha.13", true},
		{"xray", "Xray 26.9.3 (Xray, Penetrates Everything.)", "v26.9.3", "Xray-" + runtime.GOOS + "-" + map[string]string{"amd64": "64", "arm64": "arm64-v8a"}[runtime.GOARCH] + ".zip", "", false},
	} {
		t.Run(tc.line, func(t *testing.T) {
			spec, err := SpecFor(tc.tool)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/example/"+spec.Tool+"/releases" {
					t.Errorf("unexpected download: %s", r.URL)
				}
				json.NewEncoder(w).Encode([]githubRelease{{TagName: tc.tag, Prerelease: tc.alpha, Assets: []githubAsset{{Name: tc.asset, BrowserDownloadURL: "https://example.invalid/asset"}}}})
			}))
			defer server.Close()
			original := githubAPI
			githubAPI = server.URL
			defer func() { githubAPI = original }()
			bin := filepath.Join(t.TempDir(), tc.tool)
			arg := "version"
			if tc.tool == "mihomo" {
				arg = "-v"
			}
			if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$1\" = '"+arg+"' ] || exit 1\necho '"+tc.line+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			channel := ChannelRelease
			if tc.alpha {
				channel = ChannelAlpha
			}
			got, err := CheckUpdate(t.Context(), tc.tool, bin, channel, "https://github.com/example/"+spec.Tool+"/releases")
			if err != nil || got.Latest != tc.want {
				t.Fatalf("CheckUpdate = %+v, %v", got, err)
			}
		})
	}
}
