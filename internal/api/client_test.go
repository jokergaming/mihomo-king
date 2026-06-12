package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A PATCH that outlives the client timeout must not be reported as an error
// when the runtime state shows it was applied (the real-world "context
// deadline exceeded" on tun toggle).
func TestSetTunTimedOutButApplied(t *testing.T) {
	var mu sync.Mutex
	applied := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			mu.Lock()
			applied = true
			mu.Unlock()
			time.Sleep(300 * time.Millisecond) // longer than the mutation timeout below
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			mu.Lock()
			enable := "false"
			if applied {
				enable = "true"
			}
			mu.Unlock()
			w.Write([]byte(`{"mode":"rule","tun":{"enable":` + enable + `}}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	c.SetTimeouts(2*time.Second, 50*time.Millisecond, 5*time.Second)
	if err := c.SetTun(true); err != nil {
		t.Fatalf("SetTun should succeed via state verification, got: %v", err)
	}
}

func TestDelayEndpoints(t *testing.T) {
	const testURL = "http://www.gstatic.com/generate_204"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q, want bearer secret", got)
		}
		if got := r.URL.Query().Get("url"); got != testURL {
			t.Fatalf("url query = %q, want %q", got, testURL)
		}
		if got := r.URL.Query().Get("timeout"); got != "5000" {
			t.Fatalf("timeout query = %q, want 5000", got)
		}

		switch r.URL.Path {
		case "/proxies/HK 1/delay":
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 123})
		case "/group/Proxy/delay":
			_ = json.NewEncoder(w).Encode(map[string]int{"HK 1": 123, "JP": 456})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	delay, err := c.Delay("HK 1", testURL, 5000)
	if err != nil {
		t.Fatalf("Delay returned error: %v", err)
	}
	if delay != 123 {
		t.Fatalf("Delay = %d, want 123", delay)
	}

	delays, err := c.GroupDelay("Proxy", testURL, 5000)
	if err != nil {
		t.Fatalf("GroupDelay returned error: %v", err)
	}
	if delays["HK 1"] != 123 || delays["JP"] != 456 {
		t.Fatalf("GroupDelay = %#v, want HK 1=123 and JP=456", delays)
	}
}

// mihomo applies the patch slowly: early state reads still show the old value
// before the new one lands. Verification must poll through the stale reads
// instead of giving up on the first disagreement.
func TestSetTunAppliedLate(t *testing.T) {
	var mu sync.Mutex
	var patchedAt time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			mu.Lock()
			patchedAt = time.Now()
			mu.Unlock()
			time.Sleep(300 * time.Millisecond) // response lost to the timeout
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			mu.Lock()
			enable := "false"
			if !patchedAt.IsZero() && time.Since(patchedAt) > 2500*time.Millisecond {
				enable = "true" // applied, but only visible a while later
			}
			mu.Unlock()
			w.Write([]byte(`{"mode":"rule","tun":{"enable":` + enable + `}}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	c.SetTimeouts(2*time.Second, 50*time.Millisecond, 8*time.Second)
	if err := c.SetTun(true); err != nil {
		t.Fatalf("SetTun should survive stale reads during apply, got: %v", err)
	}
}

// When the state never reaches the target inside the verify window, the
// original error must surface.
func TestSetTunRealFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			w.Write([]byte(`{"mode":"rule","tun":{"enable":false}}`)) // never applied
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	c.SetTimeouts(2*time.Second, 50*time.Millisecond, 2*time.Second)
	if err := c.SetTun(true); err == nil {
		t.Fatal("SetTun should report the error when the state never reaches the target")
	}
}
