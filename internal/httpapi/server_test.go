package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"private-release/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return httptest.NewServer(NewServer(st, "secret").Router())
}

func doReq(t *testing.T, method, url, key, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("X-Admin-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestEndToEnd(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Admin key required.
	code, _ := doReq(t, "POST", srv.URL+"/projects/", "wrong",
		`{"id":"demo","lower":0,"upper":10,"bins":2,"epsilon_budget":"1"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", code)
	}

	code, _ = doReq(t, "POST", srv.URL+"/projects/", "secret",
		`{"id":"demo","lower":0,"upper":10,"bins":2,"epsilon_budget":"1"}`)
	if code != http.StatusCreated {
		t.Fatalf("want 201, got %d", code)
	}

	// Immutable: re-create rejected.
	code, _ = doReq(t, "POST", srv.URL+"/projects/", "secret",
		`{"id":"demo","lower":0,"upper":10,"bins":2,"epsilon_budget":"1"}`)
	if code != http.StatusConflict {
		t.Fatalf("want 409, got %d", code)
	}

	// Contributions; duplicate rejected.
	code, _ = doReq(t, "POST", srv.URL+"/projects/demo/contributions", "secret", `{"user_id":"u1","value":3}`)
	if code != http.StatusCreated {
		t.Fatalf("want 201, got %d", code)
	}
	code, _ = doReq(t, "POST", srv.URL+"/projects/demo/contributions", "secret", `{"user_id":"u1","value":4}`)
	if code != http.StatusConflict {
		t.Fatalf("want 409 for duplicate, got %d", code)
	}
	doReq(t, "POST", srv.URL+"/projects/demo/contributions", "secret", `{"user_id":"u2","value":99}`)

	// Public releases.
	code, body := doReq(t, "GET", srv.URL+"/projects/demo/count?epsilon=0.5", "", "")
	if code != http.StatusOK {
		t.Fatalf("count: want 200, got %d (%v)", code, body)
	}
	priv := body["privacy"].(map[string]any)
	if priv["epsilon_spent"] != "0.500000000000" {
		t.Fatalf("spent: %v", priv)
	}

	// Reuse: same op + equal epsilon.
	code, body = doReq(t, "GET", srv.URL+"/projects/demo/count?epsilon=0.50", "", "")
	if code != http.StatusOK || body["privacy"].(map[string]any)["reused"] != true {
		t.Fatalf("reuse: %d %v", code, body)
	}

	// Histogram returns all bins including empty ones.
	code, body = doReq(t, "GET", srv.URL+"/projects/demo/histogram?epsilon=0.25", "", "")
	if code != http.StatusOK {
		t.Fatalf("hist: %d %v", code, body)
	}
	if len(body["bins"].([]any)) != 2 {
		t.Fatalf("want 2 bins: %v", body)
	}

	// Budget exhaustion: spent 0.75, asking 0.5 more exceeds 1.
	code, body = doReq(t, "GET", srv.URL+"/projects/demo/sum?epsilon=0.5", "", "")
	if code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %v", code, body)
	}
	if body["error"] == "" {
		t.Fatal("expected clear error message")
	}

	// Invalid epsilon rejected.
	code, _ = doReq(t, "GET", srv.URL+"/projects/demo/count?epsilon=-1", "", "")
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
	code, _ = doReq(t, "GET", srv.URL+"/projects/demo/count?epsilon=abc", "", "")
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
}
