// Smoke-тест пилота стриминга: двухэтапный розбір (быстрый вердикт +
// документ, который печатается по мере генерации) против живого AI-прокси
// или прямого Gemini. Печатает тайминги этапов и число дельт стриминга.
//
// На VM (с продакшн-env):
//
//	set -a; source /home/archi/info-bot/.env; set +a
//	./smoke_analyze_stream
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"info-bot-go/internal/ai"
)

func main() {
	replyText := "Шановний громадянине! Ваш запит розглянуто. У наданні запитуваної інформації відмовлено, оскільки підготовка відповіді потребує значного часу та ресурсів. З повагою, канцелярія."
	organ := "Департамент освіти облдержадміністрації"
	subject := "Копії наказів про призначення стипендій"
	if len(os.Args) > 1 {
		replyText = os.Args[1]
	}

	var keys []string
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		keys = strings.Split(v, ",")
	}
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-3-flash-preview"
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
	fmt.Println("модель:", model, "| ключей:", len(keys))

	// Этап 1: быстрый вердикт.
	t0 := time.Now()
	a, err := rot.AnalyzeRefusalVerdict(organ, subject, replyText, nil)
	if err != nil {
		fmt.Println("ОШИБКА ВЕРДИКТА:", err)
		os.Exit(1)
	}
	verdictMs := time.Since(t0).Milliseconds()
	fmt.Printf("=== ВЕРДИКТ (за %d мс) ===\n", verdictMs)
	fmt.Println("Тип:", a.Type, "| Законність:", a.IsLegal, "| Наступний крок:", a.NextStep)
	fmt.Println("Коротко:", a.Summary)

	// Этап 2: документ со стримингом.
	t1 := time.Now()
	chunks := 0
	var firstGap time.Duration
	subj, body, derr := rot.AnalyzeRefusalDocument(a, organ, subject, replyText, func(delta string) {
		if chunks == 0 {
			firstGap = time.Since(t1)
		}
		chunks++
		if chunks <= 3 {
			fmt.Printf("  [дельта %d, +%v] %q\n", chunks, time.Since(t1).Round(10*time.Millisecond), delta)
		}
	})
	if derr != nil {
		fmt.Println("ОШИБКА ДОКУМЕНТА:", derr)
		os.Exit(1)
	}
	docMs := time.Since(t1).Milliseconds()
	fmt.Printf("=== ДОКУМЕНТ (за %d мс, дельт стриминга: %d) ===\n", docMs, chunks)
	fmt.Println("Перша дельта через:", firstGap.Round(10*time.Millisecond))
	fmt.Println("Тема:", subj)
	fmt.Printf("Тіло: %d символів\n", len([]rune(body)))
	fmt.Println("--- ПЕРВЫЕ 300 СИМВОЛОВ ---")
	r := []rune(body)
	if len(r) > 300 {
		r = r[:300]
	}
	fmt.Println(string(r))
	fmt.Printf("=== ИТОГО: вердикт %d мс + документ %d мс = %d мс ===\n", verdictMs, docMs, verdictMs+docMs)
}
