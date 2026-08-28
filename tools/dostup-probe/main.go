// Пробник структуры страниц dostup.org.ua:
//   dostup-probe -email X -password Y -my      — список запросов портала (полный разбор)
//   dostup-probe -status <slug>                — статус публичной страницы запроса
package main

import (
        "encoding/json"
        "flag"
        "fmt"

        "info-bot-go/internal/dostup"
)

func main() {
        email := flag.String("email", "", "email аккаунта")
        password := flag.String("password", "", "пароль")
        session := flag.String("session", ".dostup_session.json", "файл сессии")
        my := flag.Bool("my", false, "список запросов портала (MyRequestsFull)")
        status := flag.String("status", "", "слаг запроса для проверки статуса")
        flag.Parse()

        c := dostup.New(*session)
        if *email != "" {
                c.SetCredentials(*email, *password)
        }

        if *status != "" {
                st, err := c.GetRequestStatus(*status)
                if err != nil {
                        fmt.Println("error:", err)
                        return
                }
                b, _ := json.MarshalIndent(st, "", "  ")
                fmt.Println(string(b))
                fmt.Println("LABEL:", dostup.StatusLabel(st.Status))
                fmt.Println("CLASSIFY:", dostup.KindLabel(dostup.ClassifyReply(st.ResponseExcerpt)))
                return
        }

        if *my {
                reqs, err := c.MyRequestsFull()
                if err != nil {
                        fmt.Println("error:", err)
                        return
                }
                b, _ := json.MarshalIndent(reqs, "", "  ")
                fmt.Println(string(b))
        }
}
