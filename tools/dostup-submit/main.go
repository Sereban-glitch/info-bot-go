// Команда dostup-submit — подача информационного запроса через dostup.org.ua
// тем же кодом (internal/dostup), который использует бот в продакшне.
//
// Режимы:
//
//	поиск распорядителей:   dostup-submit -email X -password Y -search "ДСА"
//	подача запроса:         dostup-submit -email X -password Y -slug <slug> -title "..." -body-file req.txt
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"info-bot-go/internal/dostup"
)

func main() {
	email := flag.String("email", "", "email аккаунта dostup.org.ua")
	password := flag.String("password", "", "пароль аккаунта")
	sessionFile := flag.String("session", ".dostup_session.json", "файл cookie-сессии")
	search := flag.String("search", "", "режим поиска: строка запроса")
	slug := flag.String("slug", "", "слаг распорядителя (для подачи)")
	title := flag.String("title", "", "тема запроса (до 150 символов)")
	bodyFile := flag.String("body-file", "", "файл с текстом запроса (UTF-8)")
	retries := flag.Int("retries", 2, "повторов при rate-limit (пауза 200 c)")
	flag.Parse()

	if *email == "" || *password == "" {
		fmt.Println("Укажите -email и -password")
		os.Exit(1)
	}

	client := dostup.New(*sessionFile)
	client.SetCredentials(*email, *password)

	// Режим поиска
	if *search != "" {
		if err := client.Login(*email, *password); err != nil {
			fmt.Println("❌ ВХОД:", err)
			os.Exit(1)
		}
		bodies, err := client.SearchBodies(*search)
		if err != nil {
			fmt.Println("❌ ПОИСК:", err)
			os.Exit(1)
		}
		fmt.Printf("Найдено %d:\n", len(bodies))
		for i, b := range bodies {
			if i >= 15 {
				break
			}
			fmt.Printf("  %s | %s\n", b.Slug, b.Name)
		}
		return
	}

	// Режим подачи
	if *slug == "" || *title == "" || *bodyFile == "" {
		fmt.Println("Для подачи укажите -slug, -title и -body-file")
		os.Exit(1)
	}
	raw, err := os.ReadFile(*bodyFile)
	if err != nil {
		fmt.Println("❌ Не читается файл текста:", err)
		os.Exit(1)
	}
	body := strings.TrimSpace(string(raw))
	titleClean := strings.TrimSpace(*title)
	if len(titleClean) > 150 {
		titleClean = titleClean[:147] + "..."
	}

	if err := client.Login(*email, *password); err != nil {
		fmt.Println("❌ ВХОД:", err)
		os.Exit(1)
	}

	var info *dostup.RequestInfo
	for attempt := 0; ; attempt++ {
		info, err = client.SubmitRequest(*slug, titleClean, body)
		if err == nil {
			break
		}
		if attempt < *retries && errors.Is(err, dostup.ErrRateLimited) {
			fmt.Printf("⏳ Rate limit, жду 200 с (попытка %d/%d)...\n", attempt+1, *retries)
			time.Sleep(200 * time.Second)
			continue
		}
		fmt.Println("❌ ПОДАЧА:", err)
		os.Exit(1)
	}

	fmt.Println("✅ ЗАПРОС ОПУБЛИКОВАН")
	fmt.Println("URL:", info.URL)
	fmt.Println("Slug:", info.Slug)
}
