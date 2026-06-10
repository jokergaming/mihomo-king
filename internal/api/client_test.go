package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A PATCH that outlives the client timeout must not be reported as an error
// when the runtime state shows it was applied (the real-world "context
// deadline exceeded" on tun toggle).
func TestSetTunTimedOutButApplied(t *testing.T) {
	applied := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			applied = true
			time.Sleep(300 * time.Millisecond) // longer than the mutation timeout below
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			enable := "false"
			if applied {
				enable = "true"
			}
			w.Write([]byte(`{"mode":"rule","tun":{"enable":` + enable + `}}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	c.SetTimeouts(2*time.Second, 50*time.Millisecond)
	if err := c.SetTun(true); err != nil {
		t.Fatalf("SetTun should succeed via state verification, got: %v", err)
	}
}

// When the controller answers and the state disagrees, the original error
// must surface.
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
	c.SetTimeouts(2*time.Second, 50*time.Millisecond)
	if err := c.SetTun(true); err == nil {
		t.Fatal("SetTun should report the error when the state never reaches the target")
	}
}
