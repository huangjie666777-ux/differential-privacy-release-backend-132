package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"private-release/internal/service"
	"private-release/internal/store"
)

func TestCoreHTTPFlow(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	router := NewRouter(service.New(db), "secret")

	body := `{"id":"p1","lower":0,"upper":10,"edges":[0,5,10],"total_epsilon":"1.5","records":[{"user_id":"u1","value":4},{"user_id":"u2","value":7}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/", bytes.NewBufferString(body))
	req.Header.Set("X-Admin-Key", "secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	for _, payload := range []string{
		`{"operation":"count","epsilon":"0.5"}`,
		`{"operation":"sum","epsilon":"0.5"}`,
		`{"operation":"histogram","epsilon":"0.5"}`,
	} {
		req = httptest.NewRequest(http.MethodPost, "/v1/projects/p1/queries", bytes.NewBufferString(payload))
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %s status=%d body=%s", payload, rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/p1/queries", bytes.NewBufferString(`{"operation":"count","epsilon":"0.01"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("over-budget status = %d body=%s", rec.Code, rec.Body.String())
	}
}
