// Package geo downloads one Loyalsoldier geoip release and installs the files
// each managed core actually loads.
package geo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	v2geo "github.com/v2fly/geoip/plugin/v2ray"
	"google.golang.org/protobuf/proto"

	"mihomo-king/internal/config"
)

const (
	repo = "Loyalsoldier/geoip"

	// Source is the release the shared files come from.
	Source = "https://github.com/" + repo

	rawBase = "https://raw.githubusercontent.com/" + repo + "/release/"
	cdnBase = "https://cdn.jsdelivr.net/gh/" + repo + "@release/"

	maxFile = 64 << 20
)

// httpGet is the download function. Tests replace it.
var httpGet = get

// File is one shared geoip artifact and the name a core expects on disk.
type File struct {
	Name   string // filename in the Loyalsoldier release
	Dest   string // filename the core loads
	Format string
}

// Installed is one file written for a core.
type Installed struct {
	Core   string
	Format string
	Path   string
	Bytes  int
}

// Result is one completed download shared across the cores that can load it.
type Result struct {
	Source    string
	SHA256    string
	Files     []Installed
	Skipped   []string
	Restarted []string
}

// FilesFor reports the shared release files a core can load, in install order.
//
// mihomo looks for GeoIP.dat only when geodata-mode is on, and otherwise for
// Country.mmdb, geoip.db or geoip.metadb. An existing geoip.metadb wins that
// search, so the shared MaxMind file is installed under that name. sing-box
// has no shared database: each category is its own .srs, generated from the
// one downloaded geoip.dat. xray loads geoip.dat from XRAY_LOCATION_ASSET.
func FilesFor(tool string) ([]File, error) {
	switch tool {
	case config.ToolMihomo:
		return []File{
			{Name: "geoip.dat", Dest: "GeoIP.dat", Format: "dat"},
			{Name: "Country.mmdb", Dest: "geoip.metadb", Format: "mmdb"},
			{Name: "GeoLite2-ASN.mmdb", Dest: "ASN.mmdb", Format: "asn"},
		}, nil
	case config.ToolXray:
		return []File{{Name: "geoip.dat", Dest: "geoip.dat", Format: "dat"}}, nil
	case config.ToolSingBox:
		return []File{{Name: "geoip.dat", Dest: "geoip.dat", Format: "dat"}}, nil
	default:
		return nil, fmt.Errorf("unknown core %q", tool)
	}
}

// SingBoxCategories are the Loyalsoldier categories extracted into local
// rule-set files. The shared geoip.dat is the only download; sing-box cannot
// load that file, so each category becomes one .srs beside it.
func SingBoxCategories() []string {
	return []string{"cn", "private", "google", "telegram", "netflix", "cloudflare"}
}

// Install downloads the shared geoip release once and writes the files needed
// by tools. An empty tools list updates every managed core. restart reloads a
// core that is running and had a file replaced.
func Install(ctx context.Context, s *config.Settings, tools []string, restart bool, running func(string) bool, restartCore func(string) error) (*Result, error) {
	if len(tools) == 0 {
		tools = []string{config.ToolMihomo, config.ToolSingBox, config.ToolXray}
	}
	seen := map[string]bool{}
	var files []File
	var selected []string
	for _, tool := range tools {
		if seen[tool] {
			continue
		}
		needed, err := FilesFor(tool)
		if err != nil {
			return nil, err
		}
		seen[tool] = true
		selected = append(selected, tool)
		for _, file := range needed {
			if !containsFile(files, file.Name) {
				files = append(files, file)
			}
		}
	}

	cache, err := os.MkdirTemp("", "mihomo-king-geo-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(cache)

	bodies := map[string][]byte{}
	var sum string
	for _, file := range files {
		body, err := downloadVerified(ctx, cache, file.Name)
		if err != nil {
			return nil, err
		}
		bodies[file.Name] = body
		if file.Name == "geoip.dat" {
			sum = checksum(body)
		}
	}

	res := &Result{Source: Source, SHA256: sum}
	for _, tool := range selected {
		wrote, err := installCore(s, tool, bodies)
		if err != nil {
			return res, err
		}
		if len(wrote) == 0 {
			res.Skipped = append(res.Skipped, tool)
			continue
		}
		res.Files = append(res.Files, wrote...)
		if restart && running != nil && running(tool) && restartCore != nil {
			if err := restartCore(tool); err != nil {
				return res, fmt.Errorf("installed geoip, but restarting %s failed: %w", tool, err)
			}
			res.Restarted = append(res.Restarted, tool)
		}
	}
	return res, nil
}

func installCore(s *config.Settings, tool string, bodies map[string][]byte) ([]Installed, error) {
	files, err := FilesFor(tool)
	if err != nil {
		return nil, err
	}
	dir, err := assetDir(s, tool)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var wrote []Installed
	for _, file := range files {
		dest := filepath.Join(dir, file.Dest)
		changed, err := writeIfChanged(dest, bodies[file.Name])
		if err != nil {
			return wrote, err
		}
		if !changed {
			continue
		}
		wrote = append(wrote, Installed{Core: tool, Format: file.Format, Path: dest, Bytes: len(bodies[file.Name])})
	}
	if tool != config.ToolSingBox {
		return wrote, nil
	}
	sets, err := writeSingBoxSets(dir, bodies["geoip.dat"])
	if err != nil {
		return wrote, err
	}
	return append(wrote, sets...), nil
}

func writeSingBoxSets(dir string, dat []byte) ([]Installed, error) {
	lists, err := datCategories(dat, SingBoxCategories())
	if err != nil {
		return nil, err
	}
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		return nil, fmt.Errorf("sing-box is required to compile the shared geoip.dat into rule-sets: %w", err)
	}
	var wrote []Installed
	for _, category := range SingBoxCategories() {
		cidrs := lists[strings.ToUpper(category)]
		if len(cidrs) == 0 {
			return wrote, fmt.Errorf("shared geoip.dat has no %s list", category)
		}
		name := "geoip-" + category + ".srs"
		dest := filepath.Join(dir, name)
		body, err := compileRuleSet(bin, cidrs)
		if err != nil {
			return wrote, fmt.Errorf("compile %s: %w", name, err)
		}
		changed, err := writeIfChanged(dest, body)
		if err != nil {
			return wrote, err
		}
		if changed {
			wrote = append(wrote, Installed{Core: config.ToolSingBox, Format: "srs", Path: dest, Bytes: len(body)})
		}
	}
	return wrote, nil
}

func datCategories(dat []byte, categories []string) (map[string][]string, error) {
	var list v2geo.GeoIPList
	if err := proto.Unmarshal(dat, &list); err != nil {
		return nil, fmt.Errorf("parse geoip.dat: %w", err)
	}
	want := map[string]bool{}
	for _, category := range categories {
		want[strings.ToUpper(category)] = true
	}
	out := map[string][]string{}
	for _, entry := range list.Entry {
		code := strings.ToUpper(entry.GetCountryCode())
		if !want[code] {
			continue
		}
		cidrs := make([]string, 0, len(entry.GetCidr()))
		for _, cidr := range entry.GetCidr() {
			addr, ok := netip.AddrFromSlice(cidr.GetIp())
			if !ok {
				return nil, fmt.Errorf("geoip.dat %s has an invalid address", code)
			}
			bits := int(cidr.GetPrefix())
			if bits < 0 || bits > addr.BitLen() {
				return nil, fmt.Errorf("geoip.dat %s has an invalid prefix %d", code, bits)
			}
			cidrs = append(cidrs, netip.PrefixFrom(addr, bits).String())
		}
		out[code] = cidrs
	}
	return out, nil
}

func compileRuleSet(bin string, cidrs []string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "mihomo-king-srs-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "geoip.json")
	dest := filepath.Join(dir, "geoip.srs")
	doc, err := json.Marshal(map[string]any{
		"version": 2,
		"rules":   []any{map[string]any{"ip_cidr": cidrs}},
	})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(source, doc, 0o644); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "rule-set", "compile", source, "-o", dest)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(body, []byte("SRS")) {
		return nil, fmt.Errorf("sing-box did not write an SRS file")
	}
	return body, nil
}

func assetDir(s *config.Settings, tool string) (string, error) {
	switch tool {
	case config.ToolMihomo:
		if s.MihomoDir == "" {
			return "", fmt.Errorf("mihomo data dir is not configured")
		}
		return s.MihomoDir, nil
	case config.ToolXray:
		if s.XrayDir == "" {
			return "", fmt.Errorf("xray data dir is not configured")
		}
		return s.XrayDir, nil
	case config.ToolSingBox:
		if s.SingBoxDir == "" {
			return "", fmt.Errorf("sing-box data dir is not configured")
		}
		return s.SingBoxDir, nil
	default:
		return "", fmt.Errorf("unknown core %q", tool)
	}
}

func downloadVerified(ctx context.Context, cache, name string) ([]byte, error) {
	body, err := downloadFirst(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(cache, name), body, 0o644); err != nil {
		return nil, err
	}
	sum, err := downloadFirst(ctx, name+".sha256sum")
	if err != nil {
		return nil, fmt.Errorf("download %s checksum: %w", name, err)
	}
	want, err := parseSum(string(sum), name)
	if err != nil {
		return nil, err
	}
	got := checksum(body)
	if !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("%s checksum mismatch: got %s, want %s", name, got, want)
	}
	return body, nil
}

func downloadFirst(ctx context.Context, name string) ([]byte, error) {
	var last error
	for _, base := range []string{rawBase, cdnBase} {
		body, err := httpGet(ctx, base+name)
		if err == nil {
			return body, nil
		}
		last = err
	}
	return nil, fmt.Errorf("download %s: %w", name, last)
}

func get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mihomo-king")
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", rawURL, resp.Status)
	}
	if len(body) == 0 || len(body) > maxFile {
		return nil, fmt.Errorf("%s has an unexpected size", rawURL)
	}
	return body, nil
}

func parseSum(text, name string) (string, error) {
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) != name && !strings.Contains(line, name) {
			continue
		}
		sum := fields[0]
		if len(sum) != sha256.Size*2 || !isHex(sum) {
			return "", fmt.Errorf("%s checksum file is malformed", name)
		}
		return strings.ToLower(sum), nil
	}
	return "", fmt.Errorf("%s checksum file has no hash", name)
}

func checksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func writeIfChanged(dest string, body []byte) (bool, error) {
	if current, err := os.ReadFile(dest); err == nil && checksum(current) == checksum(body) {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+"-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return false, err
	}
	ok = true
	return true, nil
}

func containsFile(files []File, name string) bool {
	for _, file := range files {
		if file.Name == name {
			return true
		}
	}
	return false
}

// Note formats the result for the CLI and TUI.
func Note(res *Result) string {
	return FormatNote(res, fmt.Sprintf)
}

// FormatNote keeps result details shared by CLI and localized interfaces.
func FormatNote(res *Result, format func(string, ...any) string) string {
	var b strings.Builder
	b.WriteString(format("geoip from %s", res.Source))
	if res.SHA256 != "" {
		b.WriteString("\n" + format("sha256: %s", res.SHA256))
	}
	for _, file := range res.Files {
		b.WriteString("\n" + format("%s %s: %s (%d bytes)", file.Core, file.Format, file.Path, file.Bytes))
	}
	if len(res.Files) == 0 {
		b.WriteString("\n" + format("nothing changed"))
	}
	for _, tool := range res.Skipped {
		b.WriteString("\n" + format("%s unchanged", tool))
	}
	for _, tool := range res.Restarted {
		b.WriteString("\n" + format("%s restarted", tool))
	}
	return b.String()
}
