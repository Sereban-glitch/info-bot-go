// Smoke-тест ТЗ №6 «Розбір відмови»: реальный запрос с промптом AI-юриста
// и печать структурированного вердикта. Повторяет настройки бота: сначала
// AI-прокси (если задан AI_PROXY_URL), затем прямой Gemini.
//
// Локальный запуск:
//
//	export PATH=$PATH:/home/z/tools/go/bin
//	source .env_smoke && export SMOKE_GEMINI_KEYS="$GEMINI_API_KEY"
//	go run scripts/smoke_analyze.go "текст ответа" "орган" "тема"
//
// На VM (с продакшн-env):
//
//	set -a; source /home/archi/info-bot/.env; set +a
//	./smoke_analyze "текст ответа" "орган" "тема"
package main

import (
	"fmt"
	"os"
	"strings"

	"info-bot-go/internal/ai"
)

func main() {
	replyText := "Шановний громадянине! Ваш запит розглянуто. У наданні запитуваної інформації відмовлено, оскільки підготовка відповіді потребує значного часу та ресурсів. З повагою, канцелярія."
	organ := "Департамент освіти облдержадміністрації"
	subject := "Копії наказів про призначення стипендій"
	if len(os.Args) > 1 {
		replyText = os.Args[1]
	}
	if len(os.Args) > 2 {
		organ = os.Args[2]
	}
	if len(os.Args) > 3 {
		subject = os.Args[3]
	}

	var keys []string
	if v := os.Getenv("SMOKE_GEMINI_KEYS"); v != "" {
		keys = strings.Split(v, ",")
	} else if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		keys = strings.Split(v, ",")
	}
	model := os.Getenv("SMOKE_GEMINI_MODEL")
	if model == "" {
		model = os.Getenv("GEMINI_MODEL")
	}
	if model == "" {
		model = "gemini-2.0-flash"
	}
	fallback := os.Getenv("GEMINI_FALLBACK_MODEL")

	rot := ai.NewRotator(keys, model, fallback)
	if os.Getenv("AI_PROXY_URL") != "" {
		rot.SetProxy(ai.ProxyConfig{
			URL:           os.Getenv("AI_PROXY_URL"),
			Key:           os.Getenv("AI_PROXY_KEY"),
			Model:         os.Getenv("AI_PROXY_MODEL"),
			FallbackModel: os.Getenv("AI_PROXY_FALLBACK_MODEL"),
			MediaModel:    os.Getenv("AI_PROXY_MEDIA_MODEL"),
		})
		fmt.Println("прокси:", os.Getenv("AI_PROXY_URL"), "модель:", os.Getenv("AI_PROXY_MODEL"))
	}
	if !rot.Available() {
		fmt.Println("нет доступных ключей")
		os.Exit(1)
	}
	fmt.Println("модель:", model, "| ключей:", len(keys))

	a, err := rot.AnalyzeRefusal(organ, subject, replyText, nil)
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		os.Exit(1)
	}

	fmt.Println("=== ВЕРДИКТ ===")
	fmt.Println("Тип:          ", a.Type)
	fmt.Println("Кратко:       ", a.Summary)
	fmt.Println("Законность:   ", a.IsLegal)
	fmt.Println("Обоснование:  ", a.LegalNotes)
	fmt.Printf("Нарушения:     %d\n", len(a.Violations))
	for _, v := range a.Violations {
		fmt.Printf("  • %s — %s\n", v.Article, v.Reason)
	}
	fmt.Println("Срок:         ", a.DeadlineOk)
	fmt.Println("Следующий шаг:", a.NextStep)
	fmt.Println("Рекомендация: ", a.Recommendation)
	fmt.Println("Тема док-та:  ", a.DraftSubject)
	fmt.Println("--- ТЕКСТ ДОКУМЕНТА ---")
	fmt.Println(a.DraftBody)
	fmt.Println("--- КОНЕЦ ---")
}
