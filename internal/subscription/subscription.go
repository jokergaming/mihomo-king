// Package subscription downloads, imports and stores subscriptions. Every
// accepted format is normalized to Clash/mihomo YAML before it is stored.
package subscription

import (
	"cmp"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"mihomo-king/internal/config"
)

// userAgent makes providers serve Clash-format YAML rather than a raw node list.
const userAgent = "clash.meta/1.19 (mihomo-king)"

const maxBody = 32 << 20 // 32 MiB

// defaultGroup is the select group generated for subscriptions that carry
// nodes but no proxy groups (node lists, bare proxy lists).
const defaultGroup = "PROXY"

// reservedNames are proxy/group names mihomo or this tool already use; nodes
// converted from links are renamed rather than collide with them.
var reservedNames = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL", defaultGroup, config.LocalGroupName}

// Result holds a fetched subscription, normalized to Clash/mihomo YAML.
type Result struct {
	Body     []byte
	UserInfo string         // raw subscription-userinfo header, if any
	Proxies  int            // inline proxies in Body
	Skipped  map[string]int // node links that could not be converted, by scheme
}

// Download fetches a subscription URL in any format Parse accepts.
func Download(rawURL string) (*Result, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription returned %s", resp.Status)
	}
	res, err := Parse(body)
	if err != nil {
		return nil, err
	}
	res.UserInfo = resp.Header.Get("subscription-userinfo")
	return res, nil
}

// ReadFile imports a subscription file from disk in any format Parse accepts.
func ReadFile(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("%s is larger than %d MiB", path, maxBody>>20)
	}
	return Parse(body)
}

// IsLocalPath reports whether a subscription source typed by the user names a
// file rather than a URL.
func IsLocalPath(source string) bool {
	source = unquote(source)
	for _, prefix := range []string{"/", "~", ".", "file://"} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

// ResolvePath turns a typed file source (~/x, file:///x, a relative path, a
// path quoted by terminal drag-and-drop) into an absolute path, so the file
// can be re-imported later from any working directory.
func ResolvePath(source string) (string, error) {
	path := unquote(source)
	if strings.HasPrefix(path, "file://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		path = u.Path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	if path == "" {
		return "", fmt.Errorf("file path required")
	}
	return filepath.Abs(path)
}

// unquote trims space and one pair of surrounding quotes.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return s
}

// Store writes a subscription body to path, creating parent dirs.
func Store(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// Parse normalizes a subscription body to Clash/mihomo YAML. It accepts a
// Clash/mihomo config, a base64-encoded node list (v2rayN style), or plain
// node links one per line. Node lists become proxies plus a PROXY select
// group routing everything; Clash configs are kept as they are.
func Parse(body []byte) (*Result, error) {
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if text == "" {
		return nil, fmt.Errorf("subscription is empty")
	}
	if res, ok, err := parseClash(text); ok {
		return res, err
	}
	if decoded, ok := decodeBase64(text); ok {
		text = strings.TrimSpace(decoded)
		if res, ok, err := parseClash(text); ok {
			return res, err
		}
	}
	return parseLinks(text)
}

// parseClash handles a Clash/mihomo config. ok is false when text is not one,
// so the caller can try the node-list formats.
func parseClash(text string) (res *Result, ok bool, err error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		if strings.Contains(text, "proxies:") || strings.Contains(text, "proxy-providers:") {
			return nil, true, fmt.Errorf("parse clash config: %w", err)
		}
		return nil, false, nil
	}
	_, hasProxies := root["proxies"]
	_, hasProviders := root["proxy-providers"]
	if !hasProxies && !hasProviders {
		return nil, false, nil
	}
	proxies, _ := root["proxies"].([]any)
	providers, _ := root["proxy-providers"].(map[string]any)
	if len(proxies) == 0 && len(providers) == 0 {
		return nil, true, fmt.Errorf("subscription has no proxies")
	}

	res = &Result{Body: []byte(text + "\n"), Proxies: len(proxies)}
	if groups, _ := root["proxy-groups"].([]any); len(groups) > 0 {
		return res, true, nil
	}
	// A bare proxy list (proxy-provider style) has nothing to select with.
	var names []string
	for _, proxy := range proxies {
		if m, ok := proxy.(map[string]any); ok {
			if name, _ := m["name"].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return res, true, nil
	}
	addDefaultGroup(root, names)
	if res.Body, err = yaml.Marshal(root); err != nil {
		return nil, true, err
	}
	return res, true, nil
}

// parseLinks converts node links, one per line, into a Clash config. Lines that
// are not links are ignored; links that cannot be converted are counted in
// Result.Skipped.
func parseLinks(text string) (*Result, error) {
	taken := map[string]bool{}
	for _, name := range reservedNames {
		taken[name] = true
	}
	var proxies []any
	var names []string
	skipped := map[string]int{}
	var firstErr error
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		scheme, ok := linkScheme(line)
		if !ok {
			continue
		}
		proxy, err := config.ParseProxyLink(line)
		if err != nil {
			skipped[scheme]++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		name, _ := proxy["name"].(string)
		server, _ := proxy["server"].(string)
		name = uniqueName(cmp.Or(strings.TrimSpace(name), scheme+"-"+server), taken)
		proxy["name"] = name
		proxies = append(proxies, proxy)
		names = append(names, name)
	}
	if len(proxies) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no supported node links: %w", firstErr)
		}
		return nil, fmt.Errorf("unrecognized subscription format (want Clash YAML, a base64 node list, or node links)")
	}

	root := map[string]any{"proxies": proxies}
	addDefaultGroup(root, names)
	body, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	res := &Result{Body: body, Proxies: len(proxies)}
	if len(skipped) > 0 {
		res.Skipped = skipped
	}
	return res, nil
}

// addDefaultGroup gives a config without proxy groups a select group over all
// its proxies and, if it has no rules, routes everything through that group.
func addDefaultGroup(root map[string]any, names []string) {
	taken := map[string]bool{}
	for _, name := range names {
		taken[name] = true
	}
	group := uniqueName(defaultGroup, taken)
	root["proxy-groups"] = []any{map[string]any{"name": group, "type": "select", "proxies": names}}
	if rules, _ := root["rules"].([]any); len(rules) == 0 {
		root["rules"] = []string{"MATCH," + group}
	}
}

// uniqueName returns name, or name with a " 2", " 3"… suffix when it is taken,
// and marks the result taken. Providers often reuse node names; mihomo
// rejects duplicates.
func uniqueName(name string, taken map[string]bool) string {
	unique := name
	for i := 2; taken[unique]; i++ {
		unique = fmt.Sprintf("%s %d", name, i)
	}
	taken[unique] = true
	return unique
}

// linkScheme returns the lowercased scheme of a "scheme://…" line.
func linkScheme(line string) (string, bool) {
	scheme, _, ok := strings.Cut(line, "://")
	if !ok || scheme == "" {
		return "", false
	}
	for _, r := range scheme {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '+' || r == '-' || r == '.') {
			return "", false
		}
	}
	return strings.ToLower(scheme), true
}

// decodeBase64 decodes a base64 subscription body. Providers differ in
// alphabet and padding, and some wrap the text across lines.
func decodeBase64(text string) (string, bool) {
	compact := strings.Join(strings.Fields(text), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(compact); err == nil && utf8.Valid(decoded) {
			return string(decoded), true
		}
	}
	return "", false
}

// Summary describes the result for a status line, e.g.
// "42 proxies · skipped 3 links (ssr)".
func (r *Result) Summary() string {
	var parts []string
	if r.Proxies > 0 {
		parts = append(parts, plural(r.Proxies, "proxy", "proxies"))
	}
	if len(r.Skipped) > 0 {
		total := 0
		for _, n := range r.Skipped {
			total += n
		}
		schemes := strings.Join(slices.Sorted(maps.Keys(r.Skipped)), ", ")
		parts = append(parts, fmt.Sprintf("skipped %s (%s)", plural(total, "link", "links"), schemes))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// FormatUserInfo turns a raw subscription-userinfo header into a short human
// string like "12.3GiB / 100.0GiB, expires 2026-07-01". Returns "" if empty.
func FormatUserInfo(raw string) string {
	if raw == "" {
		return ""
	}
	fields := map[string]int64{}
	for _, part := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64); err == nil {
			fields[strings.TrimSpace(kv[0])] = n
		}
	}

	var b strings.Builder
	used := fields["upload"] + fields["download"]
	switch {
	case fields["total"] > 0:
		fmt.Fprintf(&b, "%s / %s", humanBytes(used), humanBytes(fields["total"]))
	case used > 0:
		fmt.Fprintf(&b, "used %s", humanBytes(used))
	}
	if exp := fields["expire"]; exp > 0 {
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "expires %s", time.Unix(exp, 0).Format("2006-01-02"))
	}
	return b.String()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
