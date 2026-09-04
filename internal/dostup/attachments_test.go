package dostup

import (
	"os"
	"strings"
	"testing"
)

func loadTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return string(data)
}

// TestParseAttachmentsLive — ЖИВАЯ страница запроса МОЗ
// (pro_nadannia_statistichnoyi_info, ответ 03.09.2026, incoming-1352):
// 3 вложения — картинка, PDF и криптоподпись. Парсер должен вытащить
// все три с корректными ссылками скачивания.
func TestParseAttachmentsLive(t *testing.T) {
	page := loadTestdata(t, "request_moz_attachments.html")
	atts := attachmentsFromPage(page)
	if len(atts) != 3 {
		t.Fatalf("ожидали 3 вложения, получили %d: %+v", len(atts), atts)
	}
	want := []Attachment{
		{Name: "image.png.jpg", HRef: "/request/pro_nadannia_statistichnoyi_info/response/1352/attach/4/image.png.jpg?cookie_passthrough=1"},
		{Name: "RS.pdf", HRef: "/request/pro_nadannia_statistichnoyi_info/response/1352/attach/5/RS.pdf?cookie_passthrough=1"},
		{Name: "RS.pdf.p7s", HRef: "/request/pro_nadannia_statistichnoyi_info/response/1352/attach/6/RS.pdf.p7s?cookie_passthrough=1"},
	}
	for i, w := range want {
		if atts[i] != w {
			t.Errorf("вложение %d: получили %+v, ожидали %+v", i, atts[i], w)
		}
	}
}

// TestPDFAttachmentsFilter — PDF отбирается, криптоподпись .p7s и
// картинка — нет (в подписи нет читаемого текста ответа).
func TestPDFAttachmentsFilter(t *testing.T) {
	atts := []Attachment{
		{Name: "image.png.jpg", HRef: "/x/image.png.jpg"},
		{Name: "RS.pdf", HRef: "/x/RS.pdf"},
		{Name: "RS.pdf.p7s", HRef: "/x/RS.pdf.p7s"},
		{Name: "звіт.PDF", HRef: "/x/report.PDF"},
	}
	pdfs := PDFAttachments(atts)
	if len(pdfs) != 2 {
		t.Fatalf("ожидали 2 PDF, получили %d: %+v", len(pdfs), pdfs)
	}
	if pdfs[0].Name != "RS.pdf" || pdfs[1].Name != "звіт.PDF" {
		t.Errorf("фильтр PDF работает неверно: %+v", pdfs)
	}
}

// TestCleanCorrespondenceLive — главный кейс фичи: раньше текст письма
// для AI содержал мусор «3 Attachments … View Download», а сути не было
// видно. Теперь блок вложений вырезается, вместо него — маркер, тело
// письма остаётся читаемым.
func TestCleanCorrespondenceLive(t *testing.T) {
	page := loadTestdata(t, "request_moz_attachments.html")
	incoming := reIncoming.FindAllStringSubmatchIndex(page, -1)
	if len(incoming) == 0 {
		t.Fatal("на живой странице не найдено входящее сообщение")
	}
	last := incoming[len(incoming)-1]
	tail := page[last[0]:]
	m := reCorrText.FindStringSubmatch(tail)
	if m == nil {
		t.Fatal("не найден текст письма (reCorrText)")
	}
	text := cleanCorrespondenceHTML(m[1])

	// Мусора больше нет.
	for _, bad := range []string{"Attachments", "Download", "View", "attachment"} {
		if strings.Contains(text, bad) {
			t.Errorf("в чистом тексте остался мусор вложений: %q (текст: %.200s)", bad, text)
		}
	}
	// Маркер вложений на месте.
	if !strings.Contains(text, "[Вкладення: image.png.jpg, RS.pdf, RS.pdf.p7s]") {
		t.Errorf("нет маркера вложений: %.300s", text)
	}
	// Тело письма (подпись) сохранилось.
	if !strings.Contains(text, "щодо надання інформації") {
		t.Errorf("потеряно тело письма: %.300s", text)
	}
	if !strings.Contains(text, "Центр громадського здоров") {
		t.Errorf("потеряна подпись письма: %.300s", text)
	}
}

// TestClassifyAttachmentMarker — письмо-«пустышка» (только подпись + PDF)
// с маркером вложений классифицируется как ответ по существу, а не
// авто-подтверждение: файл в ответе — не «ваш лист отримано».
func TestClassifyAttachmentMarker(t *testing.T) {
	if got := ClassifyReply("[Вкладення: RS.pdf]"); got != ReplySubstantive {
		t.Errorf("маркер вложений должен давать ReplySubstantive, получили %v", got)
	}
}
