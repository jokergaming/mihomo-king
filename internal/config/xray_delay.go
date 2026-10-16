package config

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// XrayDelay tests latency of a single node in Xray mode.
// If the node is currently the active selected node and Xray is running,
// it tests HTTP GET through the mixed port. Otherwise, it tests direct TCP/TLS connect.
func (s *Settings) XrayDelay(node, testURL string, timeout time.Duration) (int, error) {
	if s.XrayNode == node {
		proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.MixedPort))
		if err == nil {
			client := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
				Timeout: timeout,
			}
			start := time.Now()
			resp, err := client.Get(testURL)
			if err == nil {
				_ = resp.Body.Close()
				return int(time.Since(start).Milliseconds()), nil
			}
		}
	}

	// Direct dial fallback/check
	proxy, err := s.findProxyNode(node)
	if err != nil {
		return 0, err
	}

	server := str(proxy["server"])
	port := intValue(proxy["port"])
	if server == "" || port == 0 {
		return 0, fmt.Errorf("node %s has invalid server/port", node)
	}

	addr := net.JoinHostPort(server, fmt.Sprintf("%d", port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if tlsVal, ok := boolValue(proxy["tls"]); ok && tlsVal {
		sni := str(proxy["servername"])
		if sni == "" {
			sni = str(proxy["sni"])
		}
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         sni,
			InsecureSkipVerify: true,
		})
		_ = tlsConn.SetDeadline(time.Now().Add(timeout))
		if err := tlsConn.Handshake(); err != nil {
			return 0, err
		}
	}

	return int(time.Since(start).Milliseconds()), nil
}

// XrayGroupDelay tests latency of all nodes in group concurrently.
func (s *Settings) XrayGroupDelay(group, testURL string, timeout time.Duration) (map[string]int, error) {
	groups, err := s.XrayGroups()
	if err != nil {
		return nil, err
	}
	var members []string
	for _, g := range groups {
		if g.Name == group {
			members = g.All
			break
		}
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("group %s has no testable members", group)
	}

	results := make(map[string]int, len(members))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)

	for _, member := range members {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			delay, err := s.XrayDelay(m, testURL, timeout)
			mu.Lock()
			if err != nil {
				results[m] = 0
			} else {
				results[m] = delay
			}
			mu.Unlock()
		}(member)
	}

	wg.Wait()
	return results, nil
}

func (s *Settings) findProxyNode(name string) (map[string]any, error) {
	subYAML, err := s.ActiveYAML()
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if len(subYAML) > 0 {
		if err := yaml.Unmarshal(subYAML, &root); err != nil {
			return nil, err
		}
	}
	if err := s.applyLocalNodes(root); err != nil {
		return nil, err
	}
	if err := s.applyChain(root); err != nil {
		return nil, err
	}

	for _, p := range mapSlice(root["proxies"]) {
		if str(p["name"]) == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("node %q not found", name)
}
