package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"mihomo-king/internal/config"
)

func TestPickReleaseAndAsset(t *testing.T) {
	releases := []githubRelease{
		{TagName: "Prerelease-Alpha", Prerelease: true, HTMLURL: "https://example.test/alpha", Assets: []githubAsset{
			{Name: "mihomo-linux-amd64-alpha-91e85ee.gz", BrowserDownloadURL: "https://example.test/alpha.gz"},
			{Name: "mihomo-linux-amd64-v3-alpha-91e85ee.gz", BrowserDownloadURL: "https://example.test/v3.gz"},
		}},
		{TagName: "v1.19.33", HTMLURL: "https://example.test/stable", Assets: []githubAsset{
			{Name: "mihomo-linux-amd64-v1.19.33.gz", BrowserDownloadURL: "https://example.test/stable.gz"},
			{Name: "mihomo-linux-amd64-v1-v1.19.33.gz", BrowserDownloadURL: "https://example.test/v1.gz"},
		}},
		{TagName: "v1.15.0-alpha.13", Prerelease: true, HTMLURL: "https://example.test/sb-alpha", Assets: []githubAsset{
			{Name: "sing-box-1.15.0-alpha.13-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/sb-alpha.tgz"},
			{Name: "sing-box-1.15.0-alpha.13-linux-amd64-glibc.tar.gz", BrowserDownloadURL: "https://example.test/sb-glibc.tgz"},
		}},
		{TagName: "v1.14.4", HTMLURL: "https://example.test/sb", Assets: []githubAsset{
			{Name: "sing-box-1.14.4-linux-arm64.tar.gz", BrowserDownloadURL: "https://example.test/sb.tgz"},
		}},
		{TagName: "v26.9.30", Prerelease: true, HTMLURL: "https://example.test/xray-alpha", Assets: []githubAsset{
			{Name: "Xray-linux-64.zip", BrowserDownloadURL: "https://example.test/xray-alpha.zip"},
		}},
		{TagName: "v26.10.21", HTMLURL: "https://example.test/xray", Assets: []githubAsset{
			{Name: "Xray-darwin-arm64-v8a.zip", BrowserDownloadURL: "https://example.test/xray.zip"},
		}},
	}

	mihomoReleases := releases[:2]
	singReleases := releases[2:4]
	xrayReleases := releases[4:]
	mihomo, _ := SpecFor(config.ToolMihomo)
	got, err := releaseFrom(mihomoReleases, mihomo, ChannelAlpha, "linux", "amd64")
	if err != nil || got.Asset != "mihomo-linux-amd64-alpha-91e85ee.gz" || got.URL != "https://example.test/alpha" {
		t.Fatalf("mihomo alpha = %+v, %v", got, err)
	}
	got, err = releaseFrom(mihomoReleases, mihomo, ChannelRelease, "linux", "amd64")
	if err != nil || got.Asset != "mihomo-linux-amd64-v1.19.33.gz" {
		t.Fatalf("mihomo release = %+v, %v", got, err)
	}

	sing, _ := SpecFor(config.ToolSingBox)
	got, err = releaseFrom(singReleases, sing, ChannelAlpha, "linux", "amd64")
	if err != nil || got.Tag != "v1.15.0-alpha.13" || got.Asset != "sing-box-1.15.0-alpha.13-linux-amd64.tar.gz" {
		t.Fatalf("sing-box alpha = %+v, %v", got, err)
	}
	got, err = releaseFrom(singReleases, sing, ChannelRelease, "linux", "arm64")
	if err != nil || got.Asset != "sing-box-1.14.4-linux-arm64.tar.gz" {
		t.Fatalf("sing-box release = %+v, %v", got, err)
	}

	xray, _ := SpecFor(config.ToolXray)
	got, err = releaseFrom(xrayReleases, xray, ChannelAlpha, "linux", "amd64")
	if err != nil || got.Tag != "v26.9.30" || got.Asset != "Xray-linux-64.zip" {
		t.Fatalf("xray alpha = %+v, %v", got, err)
	}
	got, err = releaseFrom(xrayReleases, xray, ChannelRelease, "darwin", "arm64")
	if err != nil || got.Asset != "Xray-darwin-arm64-v8a.zip" || got.URL != "https://example.test/xray" {
		t.Fatalf("xray release = %+v, %v", got, err)
	}
}

func TestInstallKeepsConfiguredPath(t *testing.T) {
	elf := []byte{0x7f, 'E', 'L', 'F', 1, 2, 3}
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		switch r.URL.Path {
		case "/repos/MetaCubeX/mihomo/releases":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":"v1.2.3","html_url":"https://github.com/MetaCubeX/mihomo/releases/tag/v1.2.3","assets":[{"name":"mihomo-linux-amd64-v1.2.3.gz","browser_download_url":"http://` + r.Host + `/mihomo.gz"}]}]`))
		case "/mihomo.gz":
			_, _ = w.Write(gzipBytes(t, elf))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	orig := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = orig })

	dir := t.TempDir()
	dest := filepath.Join(dir, "bin", "mihomo")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &config.Settings{MihomoBin: dest}
	spec, err := SpecFor(config.ToolMihomo)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := Latest(t.Context(), spec, ChannelRelease, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if rel.URL != "https://github.com/MetaCubeX/mihomo/releases/tag/v1.2.3" || rel.Asset != "mihomo-linux-amd64-v1.2.3.gz" {
		t.Fatalf("release = %+v", rel)
	}
	body, err := get(t.Context(), rel.Download)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := extractBinary(rel.Asset, body, spec.Label)
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(installMust(t, s, config.ToolMihomo), bin); err != nil {
		t.Fatal(err)
	}
	if s.MihomoBin != dest {
		t.Fatalf("configured path changed to %s", s.MihomoBin)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, elf) {
		t.Fatalf("installed %q, %v", got, err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, %v", info, err)
	}
	if gotUA != "mihomo-king" {
		t.Fatalf("user agent = %q", gotUA)
	}
}

func TestGrantNetCapsRequiresPasswordWhenNotRoot(t *testing.T) {
	if !needsSudoPassword() {
		t.Skip("running as root, setcap does not ask for a password")
	}
	res := &Result{}
	err := grantNetCaps(filepath.Join(t.TempDir(), "mihomo"), "", res)
	if err == nil {
		t.Fatal("empty password was accepted")
	}
	if res.Caps {
		t.Fatal("caps were marked set without a password")
	}
}

func TestExtractArchives(t *testing.T) {
	elf := []byte{0x7f, 'E', 'L', 'F', 'x'}
	tarGz := tarGzBytes(t, "sing-box-1.0-linux-amd64/sing-box", elf)
	got, err := extractBinary("sing-box-1.0-linux-amd64.tar.gz", tarGz, "sing-box")
	if err != nil || !bytes.Equal(got, elf) {
		t.Fatalf("tar = %q, %v", got, err)
	}
	zipped := zipBytes(t, "xray", elf)
	got, err = extractBinary("Xray-linux-64.zip", zipped, "xray")
	if err != nil || !bytes.Equal(got, elf) {
		t.Fatalf("zip = %q, %v", got, err)
	}
	if _, err := extractBinary("notes.gz", gzipBytes(t, []byte("not a binary")), "mihomo"); err == nil {
		t.Fatal("non-executable gzip was accepted")
	}
}

func TestInstallPathRequiresExistingCommand(t *testing.T) {
	s := &config.Settings{MihomoBin: "definitely-not-a-core-binary"}
	if _, err := installPath(s, config.ToolMihomo); err == nil {
		t.Fatal("bare missing command was accepted")
	}
}

func releaseFrom(releases []githubRelease, spec Spec, channel, goos, goarch string) (Release, error) {
	gh, err := pickRelease(releases, spec, channel)
	if err != nil {
		return Release{}, err
	}
	asset, err := pickAsset(gh.Assets, spec, gh.TagName, channel, goos, goarch)
	if err != nil {
		return Release{}, err
	}
	return Release{Tag: gh.TagName, URL: gh.HTMLURL, Asset: asset.Name, Download: asset.BrowserDownloadURL}, nil
}

func installMust(t *testing.T, s *config.Settings, tool string) string {
	t.Helper()
	path, err := installPath(s, tool)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tarGzBytes(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzipBytes(t, raw.Bytes())
}

func zipBytes(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
