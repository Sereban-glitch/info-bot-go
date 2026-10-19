package web

import (
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
}
