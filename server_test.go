package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutesServeEmbeddedStaticAssets(t *testing.T) {
	store, err := NewConfigStore(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("create config store: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	res := httptest.NewRecorder()
	NewApp(store, NewManager()).Routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("GET /static/style.css status = %d, want %d", res.Code, http.StatusOK)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(body), "--mono") {
		t.Fatal("GET /static/style.css did not return the embedded stylesheet")
	}
}
