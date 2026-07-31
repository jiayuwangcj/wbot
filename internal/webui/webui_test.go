package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveGet(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	FileServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesIndex(t *testing.T) {
	rec := serveGet(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q; want text/html", ct)
	}
	for _, want := range []string{"<title>wbot · Data</title>", `name="viewport"`, "/ui/style.css", "/ui/admin.html", "/ui/app.js"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("index missing %q: %s", want, rec.Body)
		}
	}
}

func TestServesAdminPlaceholder(t *testing.T) {
	rec := serveGet(t, "/admin.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · Admin</title>") {
		t.Fatalf("admin missing title: %s", rec.Body)
	}
}

func TestServesStaticAssets(t *testing.T) {
	tests := []struct {
		path string
		ct   string
	}{
		{"/style.css", "text/css"},
		{"/app.js", "text/javascript"},
	}
	for _, tt := range tests {
		rec := serveGet(t, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200 (body %s)", tt.path, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ct) {
			t.Fatalf("%s: content-type = %q; want %s prefix", tt.path, ct, tt.ct)
		}
	}
}

func TestMissingFileIs404(t *testing.T) {
	rec := serveGet(t, "/nope.txt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestNoExternalURLs(t *testing.T) {
	var files []string
	fs.WalkDir(webFiles, "web", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	for _, path := range files {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, banned := range []string{"http://", "https://", "//"} {
			if strings.Contains(string(data), banned) {
				t.Fatalf("%s contains banned external URL marker %q", path, banned)
			}
		}
	}
}

func TestViewportMetaOnAllPages(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), `name="viewport"`) {
			t.Fatalf("%s missing viewport meta", path)
		}
	}
}

func TestResponsiveBreakpoints(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, want := range []string{"@media (min-width: 1024px)", "@media (max-width: 767px)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("style.css missing %q", want)
		}
	}
}
