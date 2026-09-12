package main

import (
	"fmt"
	"log"
	"os"

	"info-bot-go/internal/dostup"
)

func main() {
	slug := os.Args[1]
	if slug == "" {
		log.Fatal("usage: status-test <slug> [state]")
	}
	state := "successful"
	if len(os.Args) > 2 {
		state = os.Args[2]
	}
	client := dostup.New("/home/archi/info-bot/.dostup_session.json")
	if !client.IsLoggedIn() {
		log.Fatalf("не авторизован")
	}
	fmt.Printf("ставим статус %q на /request/%s ...\n", state, slug)
	if err := client.ReportStatus(slug, state); err != nil {
		fmt.Println("ОШИБКА:", err)
		os.Exit(1)
	}
	fmt.Println("OK: статус установлен/подтверждён")
}