package web

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestMiniAppGeneratedAssetEmbeddedAndServed(t *testing.T) {
	html, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`src="(/assets/index-status-[a-f0-9]+\.js)"`).FindSubmatch(html)
	if len(match) != 2 {
		t.Fatal("generated bundle entrypoint missing")
	}
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, string(match[1]), nil)
	rec := httptest.NewRecorder()
	http.FileServer(http.FS(static)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", rec.Code)
	}
	for _, text := range []string{"Відповідь без оцінки", "Успішних: ", "Відповіді без відмов", "щонайменше 5 запитами"} {
		if !strings.Contains(rec.Body.String(), text) {
			t.Errorf("embedded asset missing %q", text)
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(rec.Body.Bytes()))
	wantPath := "/assets/index-status-" + hash[:12] + ".js"
	if string(match[1]) != wantPath {
		t.Fatalf("JavaScript URL does not match content hash: got %s, want %s", match[1], wantPath)
	}
}

func TestMiniAppBlueThemeEmbeddedAndServed(t *testing.T) {
	html, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`href="(/assets/blue-theme-[a-f0-9]+\.css)"`).FindAllSubmatch(html, -1)
	if len(matches) != 1 {
		t.Fatalf("expected one generated blue theme, got %d", len(matches))
	}
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	http.FileServer(http.FS(static)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, string(matches[0][1]), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("blue theme status = %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("blue theme Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(rec.Body.Bytes()))
	if want := "/assets/blue-theme-" + hash[:12] + ".css"; string(matches[0][1]) != want {
		t.Errorf("blue theme URL does not match content hash: got %s, want %s", matches[0][1], want)
	}
	for _, selector := range []string{".sz-home", ".sz-next-action", ".sz-request-card", ".sz-detail", ".sz-timeline"} {
		if !strings.Contains(rec.Body.String(), selector) {
			t.Errorf("embedded blue theme missing %q", selector)
		}
	}
}

func TestMiniAppAllLocalEntrypointAssetsExist(t *testing.T) {
	html, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"?]+)"`).FindAllSubmatch(html, -1)
	if len(refs) < 3 {
		t.Fatalf("expected app JavaScript, base CSS, and blue theme references; got %d", len(refs))
	}
	for _, ref := range refs {
		asset, err := staticFiles.ReadFile("static" + string(ref[1]))
		if err != nil {
			t.Errorf("entrypoint asset %q is not embedded: %v", ref[1], err)
		} else if len(asset) == 0 {
			t.Errorf("entrypoint asset %q is empty", ref[1])
		}
	}
}
