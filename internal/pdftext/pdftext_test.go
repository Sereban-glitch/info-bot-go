package pdftext

import (
        "errors"
        "os"
        "strings"
        "testing"
        "unicode/utf8"
)

func loadTestdata(t *testing.T, name string) []byte {
        t.Helper()
        data, err := os.ReadFile("testdata/" + name)
        if err != nil {
                t.Fatalf("read testdata/%s: %v", name, err)
        }
        return data
}

// TestExtractLiveRS — ЖИВОЙ PDF из реального ответа МОЗ (запит
// pro_nadannia_statistichnoyi_info, 03.09.2026): две страницы
// украинского текста. Главный кейс фичи: кириллица должна извлекаться
// читаемо, без «квадратиков» и потерянных букв.
func TestExtractLiveRS(t *testing.T) {
        text, err := Extract(loadTestdata(t, "RS.pdf"), 0)
        if err != nil {
                t.Fatalf("Extract: %v", err)
        }
        if n := utf8.RuneCountInString(text); n < 3000 {
                t.Errorf("ожидали ≥3000 знаков живого текста, получили %d", n)
        }
        // Ключевые фразы живого письма (шапка + суть + подпись).
        for _, want := range []string{
                "ЦЕНТР ГРОМАДСЬКОГО ЗДОРОВ’Я",
                "МІНІСТЕРСТВА ОХОРОНИ ЗДОРОВ’Я",
                "Гаршину Сергію",
                "повідомляє наступне",
        } {
                if !strings.Contains(text, want) {
                        t.Errorf("в тексте нет фрагмента %q", want)
                }
        }
}

// TestExtractMaxRunes — ограничение длины по РУНАМ (кириллица = 2 байта,
// байтовая нарезка ломала бы UTF-8 на середине буквы).
func TestExtractMaxRunes(t *testing.T) {
        text, err := Extract(loadTestdata(t, "RS.pdf"), 500)
        if err != nil {
                t.Fatalf("Extract: %v", err)
        }
        rn := []rune(strings.TrimSuffix(text, "…"))
        if len(rn) > 500 {
                t.Errorf("лимит рун превышен: %d", len(rn))
        }
        if !utf8.ValidString(text) {
                t.Errorf("невалидный UTF-8 после усечения")
        }
}

// TestExtractMini — структурный smoke: минимальный PDF с латиницей.
func TestExtractMini(t *testing.T) {
        text, err := Extract(loadTestdata(t, "mini.pdf"), 0)
        if err != nil {
                t.Fatalf("Extract: %v", err)
        }
        if !strings.Contains(text, "Test PDF text for extraction smoke check 12345") {
                t.Errorf("текст мини-PDF не извлечён: %q", text)
        }
}

// TestExtractNoTextLayer — настоящий «скан»: валидный PDF со страницей
// без текста (только векторная графика) → ErrNoText. Обработчик по этому
// случаю просит фото письма — его читает мультимодальный AI-запрос.
func TestExtractNoTextLayer(t *testing.T) {
        _, err := Extract(loadTestdata(t, "scan_like.pdf"), 0)
        if !errors.Is(err, ErrNoText) {
                t.Errorf("ожидали ErrNoText, получили %v", err)
        }
}

// TestExtractNotPDF — защита от «PDF», которым не являются.
func TestExtractNotPDF(t *testing.T) {
        if _, err := Extract([]byte(" Просто текст, совсем не PDF "), 0); err == nil {
                t.Errorf("ожидали ошибку для не-PDF данных")
        }
        if _, err := Extract(nil, 0); err == nil {
                t.Errorf("ожидали ошибку для пустого файла")
        }
}
