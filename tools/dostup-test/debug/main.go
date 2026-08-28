package main

// Отладка: куда редиректит GET /profile/sign_in из Go-клиента.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
)

func main() {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 60e9,
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	req, _ := http.NewRequest("GET", "https://dostup.org.ua/profile/sign_in", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept-Language", "uk,ru;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Println("Status:", resp.Status)
	fmt.Println("Location:", resp.Header.Get("Location"))
	fmt.Println("Body length:", len(b))
	if len(b) > 0 {
		fmt.Println("Body start:", string(b[:min(300, len(b))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
