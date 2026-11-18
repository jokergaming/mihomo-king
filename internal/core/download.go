// Package core downloads and installs mihomo, sing-box and xray release binaries.
package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"mihomo-king/internal/config"
	"mihomo-king/internal/mihomo"
)

const (
	ChannelRelease = config.Stable
	ChannelAlpha   = "alpha"

	maxArchive = 128 << 20
)

// githubAPI is the GitHub releases endpoint. Tests replace it.
var githubAPI = "https://api.github.com"

const MissingGitHubCLIMessage = "GitHub API rate limit reached. GitHub CLI (gh) was not found on PATH.\nDownload GitHub CLI from https://cli.github.com/\nAfter installing, run gh auth login --hostname github.com and retry.\n%v"

type MissingGitHubCLIError struct{ Cause error }

func (e *MissingGitHubCLIError) Error() string { return fmt.Sprintf(MissingGitHubCLIMessage, e.Cause) }
func (e *MissingGitHubCLIError) Unwrap() error { return e.Cause }

// Spec describes one managed core's GitHub releases.
type Spec struct {
	Tool     string
	Label    string
	Repo     string
	Endpoint string
}

// Release is the GitHub release chosen for a channel.
type Release struct {
	Tag      string
	Name     string
	URL      string // release page, so the user can read what changed
	Asset    string
	Download string
}

// Result is a completed install. Dest is the path that was already configured.
type Result struct {
	Backup    string
	Release   Release
	Dest      string
	Version   string
	Caps      bool
	Restarted bool
	Stopped   bool
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Name       string        `json:"name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	HTMLURL    string        `json:"html_url"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// SpecFor returns the release source for a managed tool.
func SpecFor(tool string) (Spec, error) {
	switch tool {
	case config.ToolMihomo:
		return Spec{Tool: config.ToolMihomo, Label: "mihomo", Repo: "MetaCubeX/mihomo"}, nil
	case config.ToolSingBox:
		return Spec{Tool: config.ToolSingBox, Label: "sing-box", Repo: "SagerNet/sing-box"}, nil
	case config.ToolXray:
		return Spec{Tool: config.ToolXray, Label: "xray", Repo: "XTLS/Xray-core"}, nil
	default:
		return Spec{}, fmt.Errorf("unknown core %q", tool)
	}
}

func ReleaseSpec(tool, source string) (Spec, error) {
	spec, err := SpecFor(tool)
	if err != nil {
		return Spec{}, err
	}
	spec.Endpoint = githubAPI + "/repos/" + spec.Repo + "/releases"
	if source == "" {
		return spec, nil
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return Spec{}, fmt.Errorf("expected an HTTPS GitHub release page or releases API URL")
	}
	if strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 3 && parts[2] == "releases" {
			parts = parts[:2]
		}
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return Spec{}, fmt.Errorf("expected https://github.com/OWNER/REPO/releases")
		}
		spec.Repo = strings.Join(parts, "/")
		spec.Endpoint = githubAPI + "/repos/" + spec.Repo + "/releases"
	} else {
		spec.Endpoint = u.String()
	}
	return spec, nil
}

// Install stages a downloaded core before changing the installed binary.
func Install(ctx context.Context, s *config.Settings, tool, channel, password string, restart bool) (*Result, error) {
	spec, err := ReleaseSpec(tool, s.CoreReleaseURLs[tool])
	if err != nil {
		return nil, err
	}
	rel, err := Latest(ctx, spec, channel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	body, err := get(ctx, rel.Download)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel.Asset, err)
	}
	bin, err := extractBinary(rel.Asset, body, spec.Label)
	if err != nil {
		return nil, err
	}
	return changeBinary(ctx, s, tool, channel, restart, func(stage string, res *Result) error {
		res.Release = rel
		mode := os.FileMode(0755)
		if info, err := os.Stat(res.Dest); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("not a regular binary: %s", res.Dest)
			}
			mode = info.Mode().Perm()
			if mode&0111 == 0 {
				mode |= 0100
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.WriteFile(stage, bin, mode); err != nil {
			return err
		}
		if runtime.GOOS == "linux" {
			return grantNetCaps(stage, password, res)
		}
		return nil
	})
}

// Latest picks the newest matching GitHub release and its platform asset.
func Latest(ctx context.Context, spec Spec, channel, goos, goarch string) (Release, error) {
	if !ValidChannel(channel) {
		return Release{}, fmt.Errorf("channel must be %s or %s", ChannelRelease, ChannelAlpha)
	}
	releases, err := fetchReleases(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	gh, err := pickRelease(releases, spec, channel)
	if err != nil {
		return Release{}, err
	}
	asset, err := pickAsset(gh.Assets, spec, gh.TagName, channel, goos, goarch)
	if err != nil {
		return Release{}, fmt.Errorf("%s %s: %w", spec.Label, gh.TagName, err)
	}
	return Release{
		Tag:      gh.TagName,
		Name:     cmp.Or(gh.Name, gh.TagName),
		URL:      cmp.Or(gh.HTMLURL, "https://github.com/"+spec.Repo+"/releases/tag/"+gh.TagName),
		Asset:    asset.Name,
		Download: asset.BrowserDownloadURL,
	}, nil
}

func fetchReleases(ctx context.Context, spec Spec) ([]githubRelease, error) {
	repo := spec.Repo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cmp.Or(spec.Endpoint, githubAPI+"/repos/"+repo+"/releases"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	q := req.URL.Query()
	if !q.Has("per_page") {
		q.Set("per_page", "30")
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", "mihomo-king")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list %s releases: %w", repo, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var detail struct{ Message string }
		_ = json.Unmarshal(body, &detail)
		failure := fmt.Errorf("list %s releases: %s", repo, resp.Status)
		if detail.Message != "" {
			failure = fmt.Errorf("%w: %s", failure, detail.Message)
		}
		if !githubQuotaExhausted(req.URL, resp) {
			return nil, failure
		}
		// gh handles its own credentials. Only pass official release-list paths.
		cmd := exec.CommandContext(ctx, "gh", "api", "--hostname", "github.com", req.URL.RequestURI())
		body, err = cmd.Output()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if reset, parseErr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil {
				failure = fmt.Errorf("%w (resets %s)", failure, time.Unix(reset, 0).Local().Format(time.RFC3339))
			}
			if errors.Is(err, exec.ErrNotFound) {
				return nil, &MissingGitHubCLIError{Cause: failure}
			}
			return nil, fmt.Errorf("%w; authenticated retry failed (%v). Check gh auth status --hostname github.com", failure, err)
		}
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse %s releases: %w", repo, err)
	}
	return releases, nil
}

var releaseListPath = regexp.MustCompile(`^/repos/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+/releases$`)

func githubQuotaExhausted(u *url.URL, resp *http.Response) bool {
	return (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0" &&
		u.Scheme == "https" && u.Host == "api.github.com" && u.User == nil &&
		releaseListPath.MatchString(u.EscapedPath()) && path.Clean(u.Path) == u.Path
}

func pickRelease(releases []githubRelease, spec Spec, channel string) (githubRelease, error) {
	if spec.Tool == config.ToolMihomo && channel == ChannelAlpha {
		for _, rel := range releases {
			if !rel.Draft && rel.TagName == "Prerelease-Alpha" {
				return rel, nil
			}
		}
		return githubRelease{}, fmt.Errorf("mihomo alpha release Prerelease-Alpha was not found")
	}
	for _, rel := range releases {
		if rel.Draft || rel.TagName == "" {
			continue
		}
		if channel == ChannelAlpha {
			if rel.Prerelease && isAlphaTag(spec, rel.TagName) {
				return rel, nil
			}
			continue
		}
		if !rel.Prerelease {
			return rel, nil
		}
	}
	return githubRelease{}, fmt.Errorf("no %s %s release found", spec.Label, channel)
}

func isAlphaTag(spec Spec, tag string) bool {
	if spec.Tool == config.ToolSingBox {
		return strings.Contains(strings.ToLower(tag), "alpha")
	}
	return true
}

func pickAsset(assets []githubAsset, spec Spec, tag, channel, goos, goarch string) (githubAsset, error) {
	if goos != "linux" && goos != "darwin" {
		return githubAsset{}, fmt.Errorf("%s downloads support linux and darwin, not %s", spec.Label, goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return githubAsset{}, fmt.Errorf("%s downloads support amd64 and arm64, not %s", spec.Label, goarch)
	}
	var match func(string) bool
	switch spec.Tool {
	case config.ToolMihomo:
		if channel == ChannelAlpha {
			// Alpha assets carry a rolling commit, e.g. mihomo-linux-amd64-alpha-91e85ee.gz.
			re := regexp.MustCompile("^mihomo-" + regexp.QuoteMeta(goos) + "-" + regexp.QuoteMeta(goarch) + `-alpha-[0-9a-f]+\.gz$`)
			match = re.MatchString
		} else {
			want := fmt.Sprintf("mihomo-%s-%s-%s.gz", goos, goarch, tag)
			match = func(name string) bool { return name == want }
		}
	case config.ToolSingBox:
		want := fmt.Sprintf("sing-box-%s-%s-%s.tar.gz", strings.TrimPrefix(tag, "v"), goos, goarch)
		match = func(name string) bool { return name == want }
	case config.ToolXray:
		arch := "arm64-v8a"
		if goarch == "amd64" {
			arch = "64"
		}
		want := fmt.Sprintf("Xray-%s-%s.zip", goos, arch)
		match = func(name string) bool { return name == want }
	default:
		return githubAsset{}, fmt.Errorf("unknown core %q", spec.Tool)
	}
	for _, asset := range assets {
		if asset.BrowserDownloadURL != "" && match(asset.Name) {
			return asset, nil
		}
	}
	return githubAsset{}, fmt.Errorf("no %s/%s asset was published", goos, goarch)
}

func get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mihomo-king")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArchive+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	if len(body) > maxArchive {
		return nil, fmt.Errorf("archive exceeds %d bytes", maxArchive)
	}
	return body, nil
}

func extractBinary(asset string, body []byte, want string) ([]byte, error) {
	switch {
	case strings.HasSuffix(asset, ".tar.gz"):
		return extractTarGz(body, want)
	case strings.HasSuffix(asset, ".zip"):
		return extractZip(body, want)
	case strings.HasSuffix(asset, ".gz"):
		return extractGzip(asset, body)
	default:
		return nil, fmt.Errorf("unsupported archive %s", asset)
	}
}

func extractGzip(asset string, body []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", asset, err)
	}
	defer r.Close()
	bin, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
	if err != nil {
		return nil, err
	}
	if len(bin) > maxArchive || !executable(bin) {
		return nil, fmt.Errorf("%s is not an executable", asset)
	}
	return bin, nil
}

func extractTarGz(body []byte, want string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxArchive))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive has no %s executable", want)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != want {
			continue
		}
		return readLimited(tr, want)
	}
}

func extractZip(body []byte, want string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		bin, err := readLimited(rc, want)
		rc.Close()
		return bin, err
	}
	return nil, fmt.Errorf("archive has no %s executable", want)
}

func readLimited(r io.Reader, want string) ([]byte, error) {
	bin, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
	if err != nil {
		return nil, err
	}
	if len(bin) > maxArchive {
		return nil, fmt.Errorf("%s exceeds %d bytes", want, maxArchive)
	}
	if !executable(bin) {
		return nil, fmt.Errorf("%s is not an executable", want)
	}
	return bin, nil
}

func executable(body []byte) bool {
	return len(body) >= 4 && (bytes.HasPrefix(body, []byte{0x7f, 'E', 'L', 'F'}) ||
		bytes.HasPrefix(body, []byte{0xfe, 0xed, 0xfa, 0xce}) ||
		bytes.HasPrefix(body, []byte{0xfe, 0xed, 0xfa, 0xcf}) ||
		bytes.HasPrefix(body, []byte{0xcf, 0xfa, 0xed, 0xfe}) ||
		bytes.HasPrefix(body, []byte{0xca, 0xfe, 0xba, 0xbe}))
}

func needsSudoPassword() bool { return os.Geteuid() != 0 }

func grantNetCaps(dest, password string, res *Result) error {
	if !needsSudoPassword() {
		if err := exec.Command("setcap", "cap_net_admin,cap_net_raw=ep", dest).Run(); err != nil {
			return fmt.Errorf("setcap %s: %w", dest, err)
		}
		res.Caps = true
		return nil
	}
	if password == "" {
		return fmt.Errorf("sudo password required to set cap_net_admin,cap_net_raw=ep on %s", dest)
	}
	if err := mihomo.GrantNetCaps(password, dest); err != nil {
		return err
	}
	has, ok := mihomo.HasNetCaps(dest)
	if ok && !has {
		return fmt.Errorf("setcap reported success but %s still lacks net caps", dest)
	}
	res.Caps = true
	return nil
}

// Channels is the user-facing channel list.
func Channels() []string { return []string{ChannelRelease, ChannelAlpha} }

// ValidChannel reports whether channel is supported.
func CanonicalChannel(channel string) string {
	if channel == "release" {
		return ChannelRelease
	}
	return channel
}

func ValidChannel(channel string) bool { return slices.Contains(Channels(), CanonicalChannel(channel)) }

// Note formats the post-install reminder, including the release page.
func Note(tool string, res *Result) string {
	return FormatNote(tool, res, fmt.Sprintf)
}

// FormatNote formats the same install result for CLI and localized interfaces.
func FormatNote(tool string, res *Result, format func(string, ...any) string) string {
	note := format("installed %s %s at %s", tool, res.Release.Tag, res.Dest)
	if res.Backup != "" {
		note = format("restored %s backup %s at %s", tool, res.Backup, res.Dest)
	}
	if res.Version != "" {
		note += " (" + res.Version + ")"
	}
	if res.Restarted {
		note += "\n" + format("%s restarted", tool)
	}
	if res.Stopped {
		note += "\n" + format("%s left stopped", tool)
	}
	if res.Caps {
		note += "\n" + format("caps: cap_net_admin,cap_net_raw=ep")
	}
	if res.Release.URL != "" {
		note += "\n" + format("release: %s", res.Release.URL) + "\n" + format("open the release page to see what changed")
	}
	return note
}
