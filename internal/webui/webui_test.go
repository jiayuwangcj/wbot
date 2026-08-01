package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func TestServesWatchlistPage(t *testing.T) {
	rec := serveGet(t, "/watchlist.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q; want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<title>wbot · Watchlist</title>") {
		t.Fatalf("watchlist missing title: %s", rec.Body)
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
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html"} {
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

func TestDataPageFormAndTables(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<form id="bars-form"`,
		`name="symbol"`,
		`name="timeframe"`,
		`name="from"`,
		`name="to"`,
		`id="bars-table"`,
		`id="runs-table"`,
		`id="bars-error"`,
		`id="runs-error"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}
}

func TestAppJSQueriesDataAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"fetch(", `"/v1/bars`, `"/v1/runs`} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestAdminPageSections(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<section id="status"`,
		`<section id="cluster"`,
		`<section id="config"`,
		`id="status-error"`,
		`id="cluster-error"`,
		`id="config-error"`,
		`id="config-table"`,
		`id="cluster-cards"`,
		`id="cluster-pipeline-runs-table"`,
		`id="cluster-pipeline-runs-empty"`,
		`id="cluster-coverage-table"`,
		`id="cluster-coverage-empty"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
}

func TestTableEmptyConvention(t *testing.T) {
	tableRe := regexp.MustCompile(`id="([a-z0-9-]+-table)"`)
	for _, path := range []string{"web/index.html", "web/admin.html", "web/watchlist.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		for _, m := range tableRe.FindAllStringSubmatch(html, -1) {
			emptyID := strings.Replace(m[1], "-table", "-empty", 1)
			if !strings.Contains(html, `id="`+emptyID+`"`) {
				t.Fatalf("%s: table %q has no matching empty element %q", path, m[1], emptyID)
			}
		}
	}
}

func TestAdminPageReadOnly(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<form") {
		t.Fatal("admin.html contains a form; admin page must be read-only")
	}
}

func TestWatchlistPageElements(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/watchlist.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<form id="watchlist-form"`,
		`name="symbol"`,
		`id="strategy-select"`,
		`id="param-fields"`,
		`id="watchlist-table"`,
		`id="watchlist-empty"`,
		`id="watchlist-error"`,
		`id="watchlist-form-error"`,
		`/ui/app.js`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("watchlist.html missing %q", want)
		}
	}
}

func TestWatchlistNavLinks(t *testing.T) {
	for _, path := range []string{"web/index.html", "web/admin.html"} {
		data, err := fs.ReadFile(webFiles, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "/ui/watchlist.html") {
			t.Fatalf("%s missing watchlist nav link", path)
		}
	}
}

func TestAppJSQueriesWatchlistAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`"/v1/watchlist`,
		`"/v1/strategies`,
		`method: "PUT"`,
		`method: "DELETE"`,
		"initWatchlistPage",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestAppJSDynamicParamForm(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`p.type === "choice"`,
		`p.type === "number"`,
		`"params." + p.name`,
		"p.choices",
		"p.default",
		"invalid number for ",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing schema-driven param form logic %q", want)
		}
	}
}

func TestAppJSQueriesAdminAPI(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{`"/v1/admin/status`, `"/v1/admin/cluster`, `"/v1/admin/config`} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestConfigMetadataOnly(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{"c.key", "c.group", "c.set", "c.updated_at"} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing config metadata access %q", want)
		}
	}
	if strings.Contains(js, "c.value") {
		t.Fatal("app.js renders config values (PRIVACY red line)")
	}
}
