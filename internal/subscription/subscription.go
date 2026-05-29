// Package subscription downloads and stores Clash/mihomo subscription configs.
package subscription

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// userAgent makes providers serve Clash-format YAML rather than a raw node list.
const userAgent = "clash.meta/1.19 (mihomo-king)"

const maxBody = 32 << 20 // 32 MiB

// Result holds a freshly downloaded subscription.
type Result struct {
	Body     []byte
	UserInfo string // raw subscription-userinfo header, if any
}

// Download fetches a subscription URL as Clash/mihomo YAML.
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
	if !looksLikeClashConfig(body) {
		return nil, fmt.Errorf("response is not a Clash config (no 'proxies:' found); base64 node lists are not supported")
	}
	return &Result{Body: body, UserInfo: resp.Header.Get("subscription-userinfo")}, nil
}

// Store writes a subscription body to path, creating parent dirs.
func Store(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func looksLikeClashConfig(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "proxies:") || strings.Contains(s, "proxy-providers:")
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
