// Команда dostup-test — живая проверка клиента internal/dostup против dostup.org.ua.
//
// Использование:
//
//	go run ./tools/dostup-test -email <email> -password <пароль>
//
// Проверяет: вход → поиск распорядителя → список «Мої запити».
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"info-bot-go/internal/dostup"
)

func main() {
	email := flag.String("email", "", "email аккаунта dostup.org.ua")
	password := flag.String("password", "", "пароль аккаунта")
	sessionFile := flag.String("session", ".dostup_session.json", "файл cookie-сессии")
	flag.Parse()

	if *email == "" || *password == "" {
		fmt.Println("Укажите -email и -password")
		os.Exit(1)
	}

	t0 := time.Now()
	client := dostup.New(*sessionFile)
	client.SetCredentials(*email, *password)

	// 1) вход
	if err := client.Login(*email, *password); err != nil {
		fmt.Println("❌ ВХОД:", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Вход выполнен (%.1f c)\n", time.Since(t0).Seconds())

	// 2) поиск распорядителя
	t1 := time.Now()
	bodies, err := client.SearchBodies("Державне бюро розслідувань")
	if err != nil {
		fmt.Println("❌ ПОИСК:", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Поиск «Державне бюро розслідувань»: %d найдено (%.1f c)\n", len(bodies), time.Since(t1).Seconds())
	for i, b := range bodies {
		if i >= 5 {
			break
		}
		fmt.Printf("   %d. %s → /body/%s\n", i+1, b.Name, b.Slug)
	}

	// 3) мои запросы
	t2 := time.Now()
	reqs, err := client.MyRequests()
	if err != nil {
		fmt.Println("❌ МОИ ЗАПРОСЫ:", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Мои запросы: %d шт. (%.1f c)\n", len(reqs), time.Since(t2).Seconds())
	for i, r := range reqs {
		if i >= 5 {
			break
		}
		fmt.Printf("   %d. %s\n      %s\n", i+1, r.Title, r.URL)
	}

	fmt.Printf("\n🎉 ВСЕ ПРОВЕРКИ ПРОЙДЕНЫ за %.1f c\n", time.Since(t0).Seconds())
}
