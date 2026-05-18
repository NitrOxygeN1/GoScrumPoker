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

	h := newStaticHandler(dir, "")
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

func TestStaticHandler_injectsMeetCloudProjectNumber(t *testing.T) {
	dir := t.TempDir()
	const projectNumber = "1234567890"
	if err := os.WriteFile(
		filepath.Join(dir, "index.html"),
		[]byte(`<!DOCTYPE html><html><head><title>app</title></head><body><div id="root"></div></body></html>`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	h := newStaticHandler(dir, projectNumber)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	want := `<meta name="gsp-cloud-project-number" content="` + projectNumber + `">`
	if !strings.Contains(body, want) {
		t.Fatalf("body missing meta tag %q: %q", want, body)
	}
	if strings.Index(body, want) > strings.Index(body, "</head>") {
		t.Fatalf("meta tag injected after </head>: %q", body)
	}
}

func TestStaticHandler_omitsMetaWhenProjectNumberEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "index.html"),
		[]byte(`<!DOCTYPE html><html><head><title>app</title></head><body></body></html>`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	h := newStaticHandler(dir, "")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "gsp-cloud-project-number") {
		t.Fatalf("meta tag must not be present when project number is empty: %q", rec.Body.String())
	}
}

func TestStaticHandler_escapesMeetCloudProjectNumber(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "index.html"),
		[]byte(`<!DOCTYPE html><html><head></head><body></body></html>`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Defensive: even though Google project numbers are digits-only, a misconfigured
	// env var must not allow HTML injection into index.html.
	h := newStaticHandler(dir, `"><script>alert(1)</script><meta x="`)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("script tag was not escaped: %q", body)
	}
}
