package dostup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipCorrespondenceTextUnicodeBoundaries(t *testing.T) {
	text := "AҐрЇє漢字🙂🚀е\u0301[ email address ]"
	runes := []rune(text)
	for limit := 1; limit <= len(runes)+1; limit++ {
		got := clipCorrespondenceText(text, limit)
		want := text
		if limit < len(runes) {
			want = string(runes[:limit]) + "…"
		}
		if got != want || !utf8.ValidString(got) {
			t.Errorf("limit %d: got %q, want %q (valid UTF-8: %v)", limit, got, want, utf8.ValidString(got))
		}
		encoded, err := json.Marshal(got)
		if err != nil || strings.Contains(string(encoded), "�") || strings.Contains(string(encoded), `\ufffd`) {
			t.Errorf("limit %d: damaged JSON %s, err %v", limit, encoded, err)
		}
	}
	for _, limit := range []int{-1, 0} {
		if got := clipCorrespondenceText(text, limit); got != text {
			t.Errorf("unlimited %d changed text: %q", limit, got)
		}
	}
	if got := clipCorrespondenceText("", 1); got != "" {
		t.Errorf("empty text: %q", got)
	}
}

func TestCleanCorrespondenceExcerptStructure(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"plain CSV", "Назва;Адреса\r\nСуд;[ email address ]\rІнший;Київ", "Назва;Адреса\nСуд;[ email address ]\nІнший;Київ"},
		{"explicit HTML breaks", `<p>Назва;Адреса<br>Суд;[ email address ]<BR />Інший;Київ</p>`, "Назва;Адреса\nСуд;[ email address ]\nІнший;Київ"},
		{"preformatted CSV", "<pre>Назва;Адреса\n\"Суд; Київ\";\"[ email address ]\"\n</pre>", "Назва;Адреса\n\"Суд; Київ\";\"[ email address ]\""},
		{"quoted CSV newline", "<pre>Назва;Опис\nСуд;\"Перший\nДругий\"</pre>", "Назва;Опис\nСуд;\"Перший\nДругий\""},
		{"paragraphs", "<div><p>Перший <b>абзац</b>.</p>\n<p>Другий абзац.</p></div>", "Перший абзац.\nДругий абзац."},
		{"HTML source wrapping is not a row", "<p>Назва;Адреса\nСуд;[ email address ]</p>", "Назва;Адреса Суд;[ email address ]"},
		{"HTML table row breaks only", "<table><tr><th>Назва</th><th>Адреса</th></tr><tr><td>Суд</td><td>[ email address ]</td></tr></table>", "Назва Адреса\nСуд [ email address ]"},
		{"inline tags do not create spaces", "<p>До<b>ступ</b> &amp; &#x1F642; &lt;tag&gt;</p>", "Доступ & 🙂 <tag>"},
		{"encoded CSV quotes", "<pre>&quot;Назва&quot;;&quot;Адреса&quot;\n&#34;Суд&#34;;&#34;[ email address ]&#34;</pre>", "\"Назва\";\"Адреса\"\n\"Суд\";\"[ email address ]\""},
		{"escaped markup remains literal", `<p>&lt;img src=x onerror=alert(1)&gt;</p>`, `<img src=x onerror=alert(1)>`},
		{"flat fragment stays flat", `<p>Назва;Адреса;Суд;[ email address ];Гр�</p>`, "Назва;Адреса;Суд;[ email address ];Гр�"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanCorrespondenceHTML(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequestExcerptEndpointsClipRunes(t *testing.T) {
	// At the old 400-byte boundary this text cut a Ukrainian character in half.
	// Only this synthetic local server is used; no portal or stored record is touched.
	body := "A" + strings.Repeat("Ґ", 398) + "🙂" + "Кінець"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/request/synthetic-excerpt" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `<div id="request_status" class="request-status-message request-status-message--successful"></div><div id="incoming-42"><span class="correspondence__header__author">Synthetic</span><div class="correspondence_text"><p>%s</p><p class="event_actions">actions</p></div></div>`, body)
	}))
	defer srv.Close()
	client := newTestClient(srv.URL)
	status, err := client.GetRequestStatus("synthetic-excerpt")
	if err != nil {
		t.Fatal(err)
	}
	want := "A" + strings.Repeat("Ґ", 398) + "🙂…"
	if status.ResponseExcerpt != want || !utf8.ValidString(status.ResponseExcerpt) {
		t.Fatalf("status excerpt clipped incorrectly: %q", status.ResponseExcerpt)
	}
	for _, limit := range []int{0, 1, 2, 399, 400, 401, 1000} {
		got, err := client.GetRequestResponseText("synthetic-excerpt", limit)
		if err != nil {
			t.Fatal(err)
		}
		if want := clipCorrespondenceText(body, limit); got != want || !utf8.ValidString(got) {
			t.Errorf("response limit %d: got %q, want %q", limit, got, want)
		}
	}
}
