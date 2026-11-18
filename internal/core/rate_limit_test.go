package core

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReleaseRateLimitRetry(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "gh"), []byte(`#!/bin/sh
[ "$#" = 4 ] && [ "$1" = api ] && [ "$2" = --hostname ] && [ "$3" = github.com ] && [ "$4" = '/repos/MetaCubeX/mihomo/releases?per_page=30' ] || exit 1
echo '[{"tag_name":"v1.2.3"}]'
`), 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	remaining := "0"
	http.DefaultClient = &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("X-RateLimit-Remaining", remaining)
		return &http.Response{StatusCode: 403, Status: "403 Forbidden", Header: header, Body: io.NopCloser(strings.NewReader(`{"message":"test explanation"}`))}, nil
	})}
	spec, err := ReleaseSpec("mihomo", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetchReleases(t.Context(), spec)
	if err != nil || len(got) != 1 || got[0].TagName != "v1.2.3" {
		t.Fatalf("authenticated retry: %v, %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	_, err = fetchReleases(t.Context(), spec)
	if err == nil || !strings.Contains(err.Error(), "https://cli.github.com/") || !strings.Contains(err.Error(), "gh auth login") || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("missing gh guidance: %v", err)
	}
	for _, tc := range []struct{ host, remaining string }{{"api.github.com", "10"}, {"mirror.example", "0"}} {
		spec.Endpoint = "https://" + tc.host + "/repos/MetaCubeX/mihomo/releases"
		remaining = tc.remaining
		_, err := fetchReleases(t.Context(), spec)
		if err == nil || !strings.Contains(err.Error(), "test explanation") || strings.Contains(err.Error(), "retry") {
			t.Fatalf("unexpected retry or missing diagnostic: %v", err)
		}
	}
}
