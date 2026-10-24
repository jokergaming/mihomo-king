package geo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mihomo-king/internal/config"
)

func TestInstallSharesOneDownload(t *testing.T) {
	dat := []byte("geoip-dat-body")
	mmdb := []byte("country-mmdb-body")
	asn := []byte("asn-mmdb-body")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch filepath.Base(r.URL.Path) {
		case "geoip.dat":
			_, _ = w.Write(dat)
		case "Country.mmdb":
			_, _ = w.Write(mmdb)
		case "GeoLite2-ASN.mmdb":
			_, _ = w.Write(asn)
		default:
			if strings.HasSuffix(r.URL.Path, ".sha256sum") {
				name := strings.TrimSuffix(filepath.Base(r.URL.Path), ".sha256sum")
				_, _ = w.Write([]byte(sumLine(bodyFor(name, dat, mmdb, asn), name)))
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	orig := httpGet
	httpGet = localGet(orig, srv.URL)
	t.Cleanup(func() { httpGet = orig })

	dir := t.TempDir()
	s := &config.Settings{
		MihomoDir:  filepath.Join(dir, "mihomo"),
		SingBoxDir: filepath.Join(dir, "sing-box"),
		XrayDir:    filepath.Join(dir, "xray"),
	}
	res, err := Install(t.Context(), s, []string{config.ToolMihomo, config.ToolXray}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA256 != checksum(dat) {
		t.Fatalf("sha256 = %s", res.SHA256)
	}
	assertFile(t, filepath.Join(s.MihomoDir, "GeoIP.dat"), dat)
	assertFile(t, filepath.Join(s.MihomoDir, "geoip.metadb"), mmdb)
	assertFile(t, filepath.Join(s.MihomoDir, "ASN.mmdb"), asn)
	assertFile(t, filepath.Join(s.XrayDir, "geoip.dat"), dat)
	if _, err := os.Stat(filepath.Join(s.SingBoxDir, "geoip.dat")); !os.IsNotExist(err) {
		t.Fatalf("sing-box was updated although it was not requested: %v", err)
	}

	again, err := Install(t.Context(), s, []string{config.ToolMihomo}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Files) != 0 || len(again.Skipped) != 1 {
		t.Fatalf("second install = %+v, want unchanged", again)
	}
	if hits == 0 {
		t.Fatal("download was not requested")
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256sum") {
			_, _ = w.Write([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  geoip.dat\n"))
			return
		}
		_, _ = w.Write([]byte("not-the-hashed-body"))
	}))
	defer srv.Close()
	orig := httpGet
	httpGet = localGet(orig, srv.URL)
	t.Cleanup(func() { httpGet = orig })

	s := &config.Settings{XrayDir: t.TempDir()}
	if _, err := Install(t.Context(), s, []string{config.ToolXray}, false, nil, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}

func TestParseSum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	got, err := parseSum(sum+"  geoip.dat\n", "geoip.dat")
	if err != nil || got != sum {
		t.Fatalf("parseSum = %s, %v", got, err)
	}
	if _, err := parseSum("short  geoip.dat\n", "geoip.dat"); err == nil {
		t.Fatal("accepted a short checksum")
	}
}

func localGet(orig func(context.Context, string) ([]byte, error), base string) func(context.Context, string) ([]byte, error) {
	return func(ctx context.Context, rawURL string) ([]byte, error) {
		name := rawURL
		switch {
		case strings.HasPrefix(rawURL, rawBase):
			name = strings.TrimPrefix(rawURL, rawBase)
		case strings.HasPrefix(rawURL, cdnBase):
			name = strings.TrimPrefix(rawURL, cdnBase)
		}
		return orig(ctx, base+"/"+name)
	}
}

func bodyFor(name string, dat, mmdb, asn []byte) []byte {
	switch name {
	case "Country.mmdb":
		return mmdb
	case "GeoLite2-ASN.mmdb":
		return asn
	default:
		return dat
	}
}

func sumLine(body []byte, name string) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
