// smoke-sendpdf — живой тест новой функции sendPDFDocument: отправляет
// PDF-файл в Telegram как документ с HTML-caption (ровно тот же вызов
// telebot, что и в боте). Запускается на VM, где .env содержит токен:
//
//      TELEGRAM_BOT_TOKEN=... ADMIN_ID=... ./smoke-sendpdf /path/file.pdf
//
// Выход: печатает ID отправленного сообщения или ошибку. Чат — ADMIN_ID
// (владелец = первый тестер фичи).
package main

import (
        "bytes"
        "flag"
        "fmt"
        "os"
        "strconv"

        tb "gopkg.in/telebot.v3"
)

func main() {
        flag.Parse()
        path := flag.Arg(0)
        if path == "" {
                fmt.Println("usage: smoke-sendpdf <file.pdf>")
                os.Exit(2)
        }
        data, err := os.ReadFile(path)
        if err != nil {
                fmt.Println("read:", err)
                os.Exit(1)
        }

        token := os.Getenv("TELEGRAM_BOT_TOKEN")
        chat := os.Getenv("ADMIN_ID")
        if token == "" || chat == "" {
                fmt.Println("TELEGRAM_BOT_TOKEN / ADMIN_ID не заданы")
                os.Exit(1)
        }
        chatID, _ := strconv.ParseInt(chat, 10, 64)

        b, err := tb.NewBot(tb.Settings{Token: token})
        if err != nil {
                fmt.Println("bot:", err)
                os.Exit(1)
        }
        me := b.Me
        fmt.Printf("bot: @%s, file: %s (%d байт)\n", me.Username, path, len(data))

        // Тот же вызов, что в handlers.sendPDFDocument.
        doc := &tb.Document{
                File:     tb.FromReader(bytes.NewReader(data)),
                FileName: "RS.pdf",
                MIME:     "application/pdf",
                Caption:  "🧪 Тест фичі: «📎 <b>Оригінальний PDF з відповіді органу</b>\n<i>файл без змін — як надіслав розпорядник</i>»",
        }
        msg, err := b.Send(tb.ChatID(chatID), doc, tb.ModeHTML)
        if err != nil {
                fmt.Println("SEND FAILED:", err)
                os.Exit(1)
        }
        fmt.Printf("SENT OK: message_id=%d, document=%q, caption delivered\n", msg.ID, msg.Document.FileName)
}
