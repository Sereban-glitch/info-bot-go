// broadcast — разовая рассылка сообщения всем, кто нажал /start.
//
// Список получателей берём из сессий бота (.sessions_go/user-*.json —
// файл создаётся ровно в момент первого /start), текст — из файла (UTF-8).
// Владельца пропускаем (он и так знает) + защита от Telegram-флуда:
// пауза 1.5 с между сообщениями.
//
// Запуск на VM:
//	cd /home/archi/info-bot && set -a && . ./.env && set +a
//	./broadcast -file msg.txt            — разовая отправка
//	./broadcast -file msg.txt -dry       — показать, кому уйдёт, не отправляя
//	./broadcast -file msg.txt -include-admin  — включая владельца
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"
)

var (
	msgFile = flag.String("file", "", "файл с текстом сообщения (UTF-8)")
	dry     = flag.Bool("dry", false, "показать получателей без отправки")
	inclAdm = flag.Bool("include-admin", false, "включить владельца в рассылку")
	sessDir = flag.String("sessions", ".sessions_go", "каталог сессий бота")
)

var reUserFile = regexp.MustCompile(`^user-(\d+)\.json$`)

func main() {
	flag.Parse()
	if *msgFile == "" {
		fmt.Println("usage: broadcast -file сообщение.txt [-dry] [-include-admin]")
		os.Exit(2)
	}

	text, err := os.ReadFile(*msgFile)
	if err != nil {
		fmt.Println("читаю файл:", err)
		os.Exit(1)
	}
	msg := strings.TrimSpace(string(text))
	if msg == "" {
		fmt.Println("файл пуст")
		os.Exit(1)
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	admin := os.Getenv("ADMIN_ID")
	adminID, _ := strconv.ParseInt(admin, 10, 64)

	ids := userIDs(*sessDir)
	if len(ids) == 0 {
		fmt.Println("в каталоге сессий нет пользователей:", *sessDir)
		os.Exit(1)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	targets := ids[:0]
	for _, id := range ids {
		if !*inclAdm && id == adminID {
			continue
		}
		targets = append(targets, id)
	}

	fmt.Printf("Получателей: %d из %d (sessions: %s)\n", len(targets), len(ids), *sessDir)
	for _, id := range targets {
		fmt.Printf("  • %d%s\n", id, map[bool]string{true: " (владелец)", false: ""}[id == adminID])
	}
	if *dry {
		fmt.Println("Режим -dry: ничего не отправлено.")
		return
	}

	if token == "" {
		fmt.Println("TELEGRAM_BOT_TOKEN не задан")
		os.Exit(1)
	}
	b, err := tb.NewBot(tb.Settings{Token: token})
	if err != nil {
		fmt.Println("подключение к Telegram:", err)
		os.Exit(1)
	}
	fmt.Printf("Бот @%s отправляет сообщение (%d знаков)…\n", b.Me.Username, len([]rune(msg)))

	ok, fail := 0, 0
	for _, id := range targets {
		if _, err := b.Send(tb.ChatID(id), msg); err != nil {
			fail++
			fmt.Printf("  ❌ %d: %v\n", id, err)
		} else {
			ok++
			fmt.Printf("  ✅ %d\n", id)
		}
		time.Sleep(1500 * time.Millisecond)
	}
	fmt.Printf("Готово: доставлено %d, ошибок %d.\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

// userIDs — ID всех, кто начинал бота (файл user-<id>.json появляется
// при первом же /start — sessionMiddleware создаёт сессию до обработки).
func userIDs(dir string) []int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var ids []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := reUserFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
