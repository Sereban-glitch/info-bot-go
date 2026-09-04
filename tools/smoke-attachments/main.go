// Smoke-тест извлечения текста из PDF-вложений портала (ТЗ №6+):
// публичная страница запроса → вложения последнего ответа органа →
// скачивание PDF → извлечение текста. Проверяет всю цепочку фичи
// «розбир читает RS.pdf» против живого портала.
//
// Запуск:
//
//	go run tools/smoke-attachments/main.go <slug>
//	go run tools/smoke-attachments/main.go pro_nadannia_statistichnoyi_info
package main

import (
	"fmt"
	"os"
	"unicode/utf8"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/pdftext"
)

func main() {
	slug := "pro_nadannia_statistichnoyi_info"
	if len(os.Args) > 1 {
		slug = os.Args[1]
	}
	c := dostup.New("")

	fmt.Printf("=== 1. Текст последнего ответа (без мусора вложений) ===\n")
	text, err := c.GetRequestResponseText(slug, 600)
	if err != nil {
		fmt.Printf("GetRequestResponseText: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%.600s\n\n", text)

	fmt.Printf("=== 2. Вложения последнего ответа ===\n")
	atts, err := c.GetRequestAttachments(slug)
	if err != nil {
		fmt.Printf("GetRequestAttachments: %v\n", err)
		os.Exit(1)
	}
	for _, a := range atts {
		mark := "— не PDF (пропускаем)"
		if a.IsPDF() {
			mark = "→ PDF, читаем"
		}
		fmt.Printf("• %-18s %s\n", a.Name, mark)
	}

	pdfs := dostup.PDFAttachments(atts)
	if len(pdfs) == 0 {
		fmt.Println("\nPDF-вложений нет — цепочка заканчивается здесь.")
		return
	}

	a := pdfs[0]
	fmt.Printf("\n=== 3. Скачивание %s ===\n", a.Name)
	data, err := c.DownloadAttachment(a.HRef, 10<<20)
	if err != nil {
		fmt.Printf("DownloadAttachment: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %d байт\n", len(data))

	fmt.Printf("\n=== 4. Извлечение текста ===\n")
	txt, err := pdftext.Extract(data, 0)
	if err != nil {
		fmt.Printf("Extract: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %d рун текста\n", utf8.RuneCountInString(txt))
	fmt.Printf("Начало: %.300s\n", txt)
	fmt.Printf("Конец:  %.200s\n", txt[utf8.RuneCountInString(txt)-200:])
	fmt.Println("\nSMOKE OK")
}
