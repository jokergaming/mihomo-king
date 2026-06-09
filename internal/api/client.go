// Package api is a thin client for mihomo's RESTful API (external-controller).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrUnauthorized means the controller rejected the configured secret.
var ErrUnauthorized = errors.New("controller unauthorized")

// Client talks to a mihomo external-controller.
type Client struct {
	base   string
	secret string
	http   *http.Client
}

// New builds a client for controller (host:port or full URL) and bearer secret.
func New(controller, secret string) *Client {
	base := controller
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		base:   strings.TrimRight(base, "/"),
		secret: secret,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Proxy is a single proxy or group entry from GET /proxies.
type Proxy struct {
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

// Group is a user-selectable proxy group.
type Group struct {
	Name string
	Now  string
	All  []string
}

// Configs is the subset of GET /configs we display.
type Configs struct {
	Mode string `json:"mode"`
	Port int    `json:"mixed-port"`
	Tun  struct {
		Enable bool `json:"enable"`
	} `json:"tun"`
}

// Version returns the running mihomo version (and doubles as a liveness check).
func (c *Client) Version() (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get("/version", &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// Configs returns the live runtime config.
func (c *Client) Configs() (*Configs, error) {
	var cfg Configs
	if err := c.get("/configs", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Groups returns the selectable (Selector) groups, sorted by name.
func (c *Client) Groups() ([]Group, error) {
	var resp struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.get("/proxies", &resp); err != nil {
		return nil, err
	}
	var groups []Group
	for name, p := range resp.Proxies {
		if p.Type == "Selector" {
			groups = append(groups, Group{Name: name, Now: p.Now, All: p.All})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

// SelectNode picks node within a Selector group (PUT /proxies/{group}).
func (c *Client) SelectNode(group, node string) error {
	return c.send(http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": node})
}

// SetTun toggles TUN live without a restart (PATCH /configs).
func (c *Client) SetTun(enable bool) error {
	return c.send(http.MethodPatch, "/configs", map[string]any{"tun": map[string]any{"enable": enable}})
}

// ReloadConfig reloads mihomo from a config file path (PUT /configs?force=true).
func (c *Client) ReloadConfig(path string) error {
	return c.send(http.MethodPut, "/configs?force=true", map[string]string{"path": path})
}

func (c *Client) get(path string, out any) error {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(path, resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) send(method, path string, body any) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return statusErr(path, resp)
	}
	return nil
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func statusErr(path string, resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
	detail := strings.TrimSpace(string(msg))
	if resp.StatusCode == http.StatusUnauthorized {
		if detail != "" {
			return fmt.Errorf("%w: %s: %s", ErrUnauthorized, path, detail)
		}
		return fmt.Errorf("%w: %s", ErrUnauthorized, path)
	}
	if detail != "" {
		return fmt.Errorf("%s: %s: %s", path, resp.Status, detail)
	}
	return fmt.Errorf("%s: %s", path, resp.Status)
}
