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

func readDist(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(webFiles, "web/dist/"+name)
	if err != nil {
		t.Fatalf("read dist/%s: %v", name, err)
	}
	return string(data)
}

func TestServesFiveReactPages(t *testing.T) {
	tests := []struct {
		path  string
		title string
	}{
		{path: "/", title: "<title>wbot · Dashboard</title>"},
		{path: "/watchlist.html", title: "<title>wbot · 策略</title>"},
		{path: "/results.html", title: "<title>wbot · 回测</title>"},
		{path: "/data.html", title: "<title>wbot · 数据</title>"},
		{path: "/admin.html", title: "<title>wbot · Admin</title>"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := serveGet(t, tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("content-type = %q; want text/html", ct)
			}
			for _, want := range []string{tt.title, `<html lang="zh">`, `name="viewport"`, `/ui/style.css`, `/ui/favicon.svg`, `id="root"`, `type="module"`} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Fatalf("page missing %q: %s", want, rec.Body)
				}
			}
		})
	}
}

func TestServesStaticAssets(t *testing.T) {
	tests := []struct {
		path string
		ct   string
	}{
		{path: "/style.css", ct: "text/css"},
		{path: "/favicon.svg", ct: "image/svg+xml"},
	}
	for _, tt := range tests {
		rec := serveGet(t, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; want 200", tt.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tt.ct) {
			t.Fatalf("%s: content-type = %q; want %s", tt.path, ct, tt.ct)
		}
	}
}

func TestCacheRevalidation(t *testing.T) {
	rec := serveGet(t, "/style.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache-control = %q; want no-cache", rec.Header().Get("Cache-Control"))
	}
	lastModified := rec.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified")
	}
	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	req.Header.Set("If-Modified-Since", lastModified)
	conditional := httptest.NewRecorder()
	FileServer().ServeHTTP(conditional, req)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 {
		t.Fatalf("conditional response = %d with %d bytes; want 304 and empty body", conditional.Code, conditional.Body.Len())
	}
}

func TestFaviconLinkedOnAllPages(t *testing.T) {
	for _, page := range []string{"index.html", "watchlist.html", "results.html", "data.html", "admin.html"} {
		if !strings.Contains(readDist(t, page), `<link rel="icon" href="/ui/favicon.svg" type="image/svg+xml" />`) {
			t.Fatalf("%s missing favicon link", page)
		}
	}
}

func TestMissingFileIs404(t *testing.T) {
	if rec := serveGet(t, "/nope.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", rec.Code)
	}
}

func TestDistAssetInventory(t *testing.T) {
	var paths []string
	err := fs.WalkDir(webFiles, "web/dist", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"web/dist/index.html", "web/dist/watchlist.html", "web/dist/results.html", "web/dist/data.html", "web/dist/admin.html", "web/dist/style.css", "web/dist/favicon.svg", "web/dist/assets/"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("dist asset inventory missing %q: %s", want, joined)
		}
	}
	for _, gone := range []string{"web/dist/app.js", "web/dist/vendor/"} {
		if strings.Contains(joined, gone) {
			t.Fatalf("old frontend asset still embedded: %q", gone)
		}
	}
}

func TestDashboardBundleContract(t *testing.T) {
	var bundle strings.Builder
	err := fs.WalkDir(webFiles, "web/dist/assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			return err
		}
		bundle.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	js := bundle.String()
	for _, want := range []string{"/v1/futu/account?env=", "/v1/account/snapshots?env=", "/v1/futu/orders?env=", "/v1/runs?limit=", "PAPER", "REAL", "暂无历史快照"} {
		if !strings.Contains(js, want) {
			t.Fatalf("dashboard bundle missing %q", want)
		}
	}
	if strings.Contains(js, "/v1/futu/quote") {
		t.Fatal("dashboard bundle calls forbidden quote endpoint")
	}
}

func TestDistPagesHaveNoRuntimeExternalURLs(t *testing.T) {
	for _, page := range []string{"index.html", "watchlist.html", "results.html", "data.html", "admin.html", "style.css"} {
		content := strings.ReplaceAll(readDist(t, page), "https://t.me/BotFather", "")
		for _, marker := range []string{"http://", "https://"} {
			if strings.Contains(content, marker) {
				t.Fatalf("dist/%s contains runtime URL marker %q", page, marker)
			}
		}
	}
}
