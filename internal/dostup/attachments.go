package dostup

// Вложения (attachments) входящих сообщений органа на публичной странице
// запроса. Проблема ТЗ №6: органы часто присылают ответ PDF-вложением
// (пример: МОЗ, RS.pdf), а тело письма — только подпись. Раньше розбир
// получал текст вида «3 Attachments RS.pdf 306K View Download З повагою…»
// и не видел сути ответа. Теперь:
//   - текст письма чистится от HTML-мусора блока вложений;
//   - вложения парсятся в структуру (имя + ссылка на скачивание);
//   - PDF можно скачать и извлечь текст (internal/pdftext).

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Attachment — файл, прикреплённый к входящему сообщению органа.
type Attachment struct {
	Name string `json:"name"` // имя файла: «RS.pdf»
	HRef string `json:"href"` // путь скачивания: /request/<slug>/response/<id>/attach/5/RS.pdf?...
}

// maxAttachments — предел количества распарсенных вложений на сообщение.
const maxAttachments = 12

var (
	// Блок одного вложения (li не вкладывается в li — регулярка безопасна).
	reAttachItem = regexp.MustCompile(`(?s)<li class="attachment"[^>]*>(.*?)</li>`)
	// Имя файла внутри блока.
	reAttachName = regexp.MustCompile(`(?s)<p class="attachment__name">\s*(.*?)\s*</p>`)
	// Ссылка «Download» — прямой путь к файлу.
	reAttachLink = regexp.MustCompile(`(?s)<a href="([^"]+)">\s*Download\s*</a>`)
	// Шапка блока вложений («N Attachments») и хвост «show more» — вырезаются целиком.
	reAttachHeader = regexp.MustCompile(`(?s)<div class="attachments__header">.*?</div>`)
	reAttachMore   = regexp.MustCompile(`(?s)<a href="#"[^>]*class="attachments__show-more"[^>]*></a>`)
)

// parseAttachments разбирает HTML-хвост последнего входящего сообщения
// и возвращает вложения. Чистая функция — покрыта тестом на живом HTML.
func parseAttachments(tail string) []Attachment {
	items := reAttachItem.FindAllStringSubmatch(tail, maxAttachments)
	if len(items) == 0 {
		return nil
	}
	var atts []Attachment
	for _, it := range items {
		a := Attachment{}
		if m := reAttachName.FindStringSubmatch(it[1]); m != nil {
			a.Name = htmlUnescape(strings.TrimSpace(reTag.ReplaceAllString(m[1], " ")))
		}
		if m := reAttachLink.FindStringSubmatch(it[1]); m != nil {
			a.HRef = strings.TrimSpace(m[1])
		}
		if a.Name != "" && a.HRef != "" {
			atts = append(atts, a)
		}
	}
	return atts
}

// stripAttachmentsHTML вырезает из HTML-фрагмента письма блок вложений
// (заголовок «N Attachments», карточки li с View/Download, show-more).
// Остаются только теги-обёртки — их снимает общий reTag при конверсации
// в текст. На выходе — чистое тело письма.
func stripAttachmentsHTML(html string) string {
	html = reAttachHeader.ReplaceAllString(html, "")
	html = reAttachItem.ReplaceAllString(html, "")
	html = reAttachMore.ReplaceAllString(html, "")
	return html
}

// attachmentMarker — компактная строка-маркер для текста/экстракта:
// подставляется вместо вырезанного мусора, чтобы AI и классификатор
// знали, что ответ содержит файлы («вкладенн» — маркер содержательности).
func attachmentMarker(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	names := make([]string, 0, len(atts))
	for _, a := range atts {
		names = append(names, a.Name)
	}
	return "[Вкладення: " + strings.Join(names, ", ") + "]"
}

// IsPDF — файл является PDF? .p7s (криптоподпись CAdES к PDF) — НЕ PDF
// для наших целей: подпись не содержит читаемого текста ответа.
func (a Attachment) IsPDF() bool {
	name := strings.ToLower(a.Name)
	if strings.HasSuffix(name, ".p7s") {
		return false
	}
	return strings.HasSuffix(name, ".pdf")
}

// PDFAttachments фильтрует PDF-вложения (без криптоподписей .p7s).
func PDFAttachments(atts []Attachment) []Attachment {
	var out []Attachment
	for _, a := range atts {
		if a.IsPDF() {
			out = append(out, a)
		}
	}
	return out
}

// GetRequestAttachments возвращает вложения последнего входящего сообщения
// органа на публичной странице запроса. Вызывается по действию пользователя
// (розбір), не в фоновых циклах — скачивание PDF делаем только по кнопке.
func (c *Client) GetRequestAttachments(slug string) ([]Attachment, error) {
	page, code, err := c.getFollow("/request/" + slug)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: страница запроса: HTTP %d", code)
	}
	return attachmentsFromPage(page), nil
}

// attachmentsFromPage — вложения последнего входящего сообщения страницы.
func attachmentsFromPage(page string) []Attachment {
	incoming := reIncoming.FindAllStringSubmatchIndex(page, -1)
	if len(incoming) == 0 {
		return nil
	}
	last := incoming[len(incoming)-1]
	return parseAttachments(page[last[0]:])
}

// DownloadAttachment скачивает файл вложения (до maxBytes) как бинарные
// данные. Ссылки вложений отдаются напрямую (200, application/pdf),
// но на всякий случай следуем редиректам вручную — как getFollow.
func (c *Client) DownloadAttachment(href string, maxBytes int64) ([]byte, error) {
	path := href
	if strings.HasPrefix(path, c.BaseURL) {
		path = strings.TrimPrefix(path, c.BaseURL)
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("dostup: неочікуваний формат посилання на вкладення: %q", href)
	}
	for i := 0; i < 6; i++ {
		req, err := http.NewRequest("GET", c.BaseURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept-Language", "uk,ru;q=0.9,en;q=0.8")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		loc := resp.Header.Get("Location")
		if resp.StatusCode >= 301 && resp.StatusCode <= 308 && loc != "" {
			resp.Body.Close()
			if strings.HasPrefix(loc, "/") {
				path = loc
			} else if strings.HasPrefix(loc, c.BaseURL) {
				path = strings.TrimPrefix(loc, c.BaseURL)
			} else {
				return nil, fmt.Errorf("dostup: зовнішній редирект вкладення: %q", loc)
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			return nil, fmt.Errorf("dostup: вкладення: HTTP %d", resp.StatusCode)
		}
		if maxBytes <= 0 {
			maxBytes = 15 << 20
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("dostup: вкладення завелике (>%d МБ)", maxBytes>>20)
		}
		return data, nil
	}
	return nil, fmt.Errorf("dostup: занадто багато редиректів вкладення")
}
