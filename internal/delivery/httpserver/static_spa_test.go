package httpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandler_servesLegalPages(t *testing.T) {
	dir := t.TempDir()
	for name, file := range staticHTMLPages {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("<!DOCTYPE html><title>"+name+"</title>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!DOCTYPE html><title>app</title>"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newStaticHandler(dir)
	for path := range staticHTMLPages {
		req := httptest.NewRequest(http.MethodGet, "/"+path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<title>"+path+"</title>") {
			t.Fatalf("%s: unexpected body %q", path, rec.Body.String())
		}
	}
}
