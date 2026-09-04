// Package pdftext извлекает текст из PDF-файлов в чистом Go
// (CGO_ENABLED=0 — бинарник остаётся самодостаточным для продакшна).
//
// Нужен для ТЗ №6 «Розбір відмови»: органы регулярно присылают ответы
// в виде PDF-вложений (пример: МОЗ — «RS.pdf»), а текст письма содержит
// только подпись. Без извлечения текста из PDF бот не видел сути ответа.
package pdftext

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	pdflib "github.com/ledongthuc/pdf"
)

// ErrNoText — PDF не содержит текстового слоя (вероятно, это скан/фото).
// Обработчик по этому случаю просит пользователя прислать фото письма —
// его умеет читать мультимодальный AI-запрос.
var ErrNoText = errors.New("pdftext: у PDF немає текстового шару (ймовірно, це скан)")

// MaxBytes — верхний предел размера PDF, отдаваемого на извлечение
// (распаковка потоков ест память, а systemd-лимит бота — сотни МБ).
const MaxBytes = 15 << 20 // 15 МБ

// wsRe — схлопывает любые последовательности пробелов/переносов.
// Библиотека отдаёт текст «пословно» (слово\n \nслово) — для AI-анализа
// важен сам текст, а не вёрстка.
var wsRe = regexp.MustCompile(`\s+`)

// Extract возвращает текст PDF. maxRunes <= 0 — без ограничения длины.
// Пробелы и переносы схлопываются в один пробел: содержимое важнее вёрстки.
// Пустой или почти пустой результат → ErrNoText (скан без текстового слоя).
func Extract(data []byte, maxRunes int) (string, error) {
	if len(data) == 0 {
		return "", errors.New("pdftext: файл порожній")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", errors.New("pdftext: це не PDF-файл")
	}
	r, err := pdflib.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// Защищённые паролем/битые файлы попадают сюда — обёртываем
		// с понятным префиксом, не раскрывая внутренности ошибки наружу.
		return "", fmt.Errorf("pdftext: не вдалося відкрити PDF: %w", err)
	}
	rd, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("pdftext: не вдалося прочитати вміст: %w", err)
	}
	raw, err := io.ReadAll(rd)
	if err != nil {
		return "", fmt.Errorf("pdftext: читання потоку: %w", err)
	}
	text := strings.TrimSpace(wsRe.ReplaceAllString(string(raw), " "))
	if utf8.RuneCountInString(text) < 20 {
		return "", ErrNoText
	}
	if maxRunes > 0 {
		rn := []rune(text)
		if len(rn) > maxRunes {
			text = string(rn[:maxRunes]) + "…"
		}
	}
	return text, nil
}
